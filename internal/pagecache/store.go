// Package pagecache хранит готовые тела ответов читальни, адресуемые
// диапазоном полос.
//
// В файле лежит уже сжатое gzip тело — ровно то, что уехало бы читателю.
// Поэтому попадание не стоит ни рендера, ни кодирования JSON, ни сжатия:
// на самой длинной главе корпуса (742 полосы) это 645 мс, из которых база
// занимает 7.
package pagecache

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// TTL — сколько живёт готовое тело. Машинная вычитка правит полосы
	// каждый день; без срока годности кэш отдавал бы вчерашний текст, пока
	// его не вытеснит объёмом. То же значение и та же причина, что у
	// seo.cacheTTL.
	TTL = time.Hour

	// gzipLevel — тот же уровень, что у middleware.Gzip. В файл ложится
	// ровно то, что и так уехало бы читателю, поэтому промах стоит столько
	// же, сколько запрос стоит сегодня. Уровни 6 и 9 дают −6.8% и −8.9% к
	// размеру за 0.49 с и 0.95 с против 0.19 с — за это платил бы первый
	// читатель.
	gzipLevel = 5

	dirPerm  = 0o755
	filePerm = 0o644

	// stampName — файл с коммитом сборки, которая наполнила каталог. Purge его
	// не трогает: иначе следующий старт счёл бы пустой кэш чужим и стирал бы
	// уже пустое.
	stampName = "COMMIT"
)

// Key — адрес записи: диапазон полос работы.
//
// Не id главы: главу и элемент подборки, накрывающие один диапазон, надо
// хранить одним файлом, а сдвиг границ главы обязан давать другой ключ сам
// собой — иначе после сдвига границ (`PUT /works/{workId}/chapters/{id}`,
// правящий StartPage/EndPage, или перестройка глав конвейером) читатель до
// конца часа получал бы чужой кусок тома. `/chapters/{id}/move` границ не
// трогает — он правит только parent_id и order_number.
type Key struct {
	WorkID int64
	Start  int
	End    int
}

func (k Key) workDir() string {
	return fmt.Sprintf("w%d", k.WorkID)
}

func (k Key) rel() string {
	return filepath.Join(k.workDir(), fmt.Sprintf("%d-%d.json.gz", k.Start, k.End))
}

// Store — кэш готовых сжатых тел на диске.
//
// Пустой каталог означает «кэш выключен»: все операции превращаются в тишину.
// Так собран сервер без PAGE_CACHE_DIR — и так идут тесты обработчиков, не
// желающие файлов на диске.
type Store struct {
	dir string

	// maxBytes — потолок каталога. Держится полем, а не константой, чтобы
	// уборку по объёму можно было проверить тестом, не занимая четверть
	// гигабайта на диске: тест ставит своё маленькое значение.
	maxBytes int64
}

// New создаёт хранилище. Пустой dir даёт выключенное хранилище, а не nil:
// вызывающему не приходится проверять указатель на каждой строке.
func New(dir string) *Store {
	return &Store{dir: dir, maxBytes: MaxBytes}
}

// Enabled говорит, пишет ли хранилище хоть что-нибудь.
func (s *Store) Enabled() bool {
	return s != nil && s.dir != ""
}

// Open отдаёт готовые gzip-байты и их размер. Третье значение — было ли
// попадание; просроченный файл попаданием не считается.
func (s *Store) Open(k Key) (io.ReadCloser, int64, bool) {
	if !s.Enabled() {
		return nil, 0, false
	}

	f, err := os.Open(filepath.Join(s.dir, k.rel()))
	if err != nil {
		return nil, 0, false
	}

	info, err := f.Stat()
	if err != nil || time.Since(info.ModTime()) > TTL {
		f.Close()
		return nil, 0, false
	}
	return f, info.Size(), true
}

// Put сжимает тело и кладёт его под ключ.
//
// Через временный файл, fsync и Rename: недописанного файла читатель увидеть
// не должен. Синк платится один раз на промах, который и так стоит сотни
// миллисекунд рендера — на этом фоне он незаметен. Без него после потери
// питания на диске мог остаться файл с усечённым телом, который читатель,
// принимающий gzip, получил бы сквозным путём как исправный: сервер там
// поток не разжимает и порчи не видит.
func (s *Store) Put(k Key, raw []byte) error {
	if !s.Enabled() {
		return nil
	}

	dir := filepath.Join(s.dir, k.workDir())
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("каталог %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "*.tmp")
	if err != nil {
		return fmt.Errorf("временный файл в %s: %w", dir, err)
	}
	// После удачного Rename файла по этому имени уже нет, и Remove просто
	// ничего не находит.
	defer os.Remove(tmp.Name())

	zw, err := gzip.NewWriterLevel(tmp, gzipLevel)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("gzip: %w", err)
	}
	if _, err := zw.Write(raw); err != nil {
		zw.Close()
		tmp.Close()
		return fmt.Errorf("запись тела: %w", err)
	}
	if err := zw.Close(); err != nil {
		tmp.Close()
		return fmt.Errorf("закрытие gzip: %w", err)
	}
	// Синк содержимого до переименования: после падения файл либо
	// отсутствует, либо содержит целое тело — каталог синкать не нужно.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("синк временного файла: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("закрытие временного файла: %w", err)
	}
	if err := os.Chmod(tmp.Name(), filePerm); err != nil {
		return fmt.Errorf("права на %s: %w", tmp.Name(), err)
	}
	return os.Rename(tmp.Name(), filepath.Join(s.dir, k.rel()))
}

// Init готовит каталог к работе и стирает содержимое, оставшееся от другой
// сборки.
//
// Срок годности от изменившегося рендерера не спасает: час читатель получал
// бы вёрстку прежней версии, а при изменившейся форме ответа фронт на ней
// просто сломался бы. Отметка коммита закрывает это на старте, до первого
// запроса. Откат на прежний коммит — тоже расхождение, тоже стирает.
func (s *Store) Init(commit string) error {
	if !s.Enabled() {
		return nil
	}
	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		return fmt.Errorf("каталог кэша %s: %w", s.dir, err)
	}

	stamp := filepath.Join(s.dir, stampName)
	if old, err := os.ReadFile(stamp); err == nil && strings.TrimSpace(string(old)) == commit {
		return nil
	}

	if _, _, err := s.Purge(); err != nil {
		return fmt.Errorf("очистка кэша чужой сборки: %w", err)
	}
	return os.WriteFile(stamp, []byte(commit), filePerm)
}

// MaxBytes — потолок каталога. Весь корпус в gzip — около 60 МБ (195 МБ
// markdown → ~270 МБ в вёрстке с JSON-экранированием → /4.6), да и часовой
// срок годности сам держит объём: больше, чем прочитали за час, в кэше
// лежать не может. Потолок — страховка от переполнения диска, а не рабочий
// режим; срабатывание пишется в лог как признак неверно подобранного числа.
const MaxBytes = 256 << 20

// Stats — сводка для страницы /admin/cache. Кэш на диске невидим, и без
// сводки пустой PAGE_CACHE_DIR на боевом ничем себя не выдаст.
type Stats struct {
	Enabled          bool  `json:"enabled"`
	Files            int   `json:"files"`
	Bytes            int64 `json:"bytes"`
	OldestAgeSeconds int64 `json:"oldest_age_seconds"`
}

// entry — файл кэша, каким его видит уборка.
type entry struct {
	path    string
	size    int64
	modTime time.Time
	temp    bool
}

// walk обходит каталоги работ. Отметка коммита и всё, что лежит вне w*, в
// обход не попадает.
func (s *Store) walk() ([]entry, error) {
	if !s.Enabled() {
		return nil, nil
	}
	dirs, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var found []entry
	for _, d := range dirs {
		if !d.IsDir() || !strings.HasPrefix(d.Name(), "w") {
			continue
		}
		sub := filepath.Join(s.dir, d.Name())
		files, err := os.ReadDir(sub)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			found = append(found, entry{
				path:    filepath.Join(sub, f.Name()),
				size:    info.Size(),
				modTime: info.ModTime(),
				temp:    strings.HasSuffix(f.Name(), ".tmp"),
			})
		}
	}
	return found, nil
}

// Delete убирает один диапазон. Первое значение — было ли что удалять.
func (s *Store) Delete(k Key) (bool, error) {
	if !s.Enabled() {
		return false, nil
	}
	err := os.Remove(filepath.Join(s.dir, k.rel()))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Purge сносит все каталоги работ и отчитывается, сколько файлов и байт
// унесло. Отметку коммита не трогает.
func (s *Store) Purge() (int, int64, error) {
	found, err := s.walk()
	if err != nil {
		return 0, 0, err
	}

	files, bytes := 0, int64(0)
	for _, e := range found {
		if e.temp {
			continue
		}
		files++
		bytes += e.size
	}

	dirs, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return files, bytes, nil
		}
		return files, bytes, err
	}
	for _, d := range dirs {
		if !d.IsDir() || !strings.HasPrefix(d.Name(), "w") {
			continue
		}
		if err := os.RemoveAll(filepath.Join(s.dir, d.Name())); err != nil {
			return files, bytes, err
		}
	}
	return files, bytes, nil
}

// PurgeWork сносит кэш одного тома целиком и отчитывается, сколько файлов и
// байт унесло.
//
// Заведён под снятие тома: после удаления строки работы адресного сброса её
// глав не остаётся — CacheHandler.PurgeChapter резолвит границы главы из базы,
// а строки уже нет, и снятый том жил бы в кэше до истечения часа. Ключ здесь
// не диапазон, а работа: каталог w<id> и есть весь её кэш.
//
// Сравнение имени каталога — точное, а не по префиксу: w4 и w47 лежат рядом.
func (s *Store) PurgeWork(workID int64) (int, int64, error) {
	if !s.Enabled() {
		return 0, 0, nil
	}

	dir := filepath.Join(s.dir, Key{WorkID: workID}.workDir())
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}

	files, bytes := 0, int64(0)
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files++
		bytes += info.Size()
	}

	if err := os.RemoveAll(dir); err != nil {
		return files, bytes, err
	}
	return files, bytes, nil
}

// Stats считает каталог. Хвосты .tmp в счёт не идут: это мусор уборки, а не
// содержимое кэша.
func (s *Store) Stats() (Stats, error) {
	st := Stats{Enabled: s.Enabled()}
	found, err := s.walk()
	if err != nil {
		return st, err
	}

	var oldest time.Time
	for _, e := range found {
		if e.temp {
			continue
		}
		st.Files++
		st.Bytes += e.size
		if oldest.IsZero() || e.modTime.Before(oldest) {
			oldest = e.modTime
		}
	}
	if !oldest.IsZero() {
		st.OldestAgeSeconds = int64(time.Since(oldest).Seconds())
	}
	return st, nil
}

// Sweep убирает просроченное и хвосты, а затем, если объём выше потолка,
// вытесняет самое старое. Первое значение — сколько унесено по сроку,
// второе — сколько вытеснено по объёму.
func (s *Store) Sweep() (int, int, error) {
	found, err := s.walk()
	if err != nil {
		return 0, 0, err
	}

	expired := 0
	var alive []entry
	for _, e := range found {
		if time.Since(e.modTime) > TTL {
			if err := os.Remove(e.path); err == nil {
				expired++
			}
			continue
		}
		if !e.temp {
			alive = append(alive, e)
		}
	}

	total := int64(0)
	for _, e := range alive {
		total += e.size
	}
	if total <= s.maxBytes {
		return expired, 0, nil
	}

	sort.Slice(alive, func(i, j int) bool { return alive[i].modTime.Before(alive[j].modTime) })
	evicted := 0
	for _, e := range alive {
		if total <= s.maxBytes {
			break
		}
		if err := os.Remove(e.path); err != nil {
			continue
		}
		total -= e.size
		evicted++
	}
	return expired, evicted, nil
}
