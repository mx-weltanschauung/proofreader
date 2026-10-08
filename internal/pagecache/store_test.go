package pagecache

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var raw = []byte(`{"pages":[{"page_number":1,"html":"<p>текст</p>","blank":false}],"footnotes_html":""}`)

func TestPutThenOpenGivesBackSameBytes(t *testing.T) {
	s := New(t.TempDir())
	key := Key{WorkID: 47, Start: 1, End: 742}

	if err := s.Put(key, raw); err != nil {
		t.Fatalf("Put: %v", err)
	}

	rc, size, ok := s.Open(key)
	if !ok {
		t.Fatal("Open: промах сразу после записи")
	}
	defer rc.Close()
	if size <= 0 {
		t.Errorf("size = %d, ожидался размер файла на диске", size)
	}

	zr, err := gzip.NewReader(rc)
	if err != nil {
		t.Fatalf("файл не читается как gzip: %v", err)
	}
	got, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if string(got) != string(raw) {
		t.Errorf("вернулось %q, ожидалось %q", got, raw)
	}
}

// Раскладка каталога — часть договора: по ней ходит уборка и по ней человек
// ищет файл руками на боевом.
func TestPutUsesRangeAddressedPath(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	if err := s.Put(Key{WorkID: 55, Start: 10, End: 231}, raw); err != nil {
		t.Fatalf("Put: %v", err)
	}

	want := filepath.Join(dir, "w55", "10-231.json.gz")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("ожидался файл %s: %v", want, err)
	}
}

// Час прошёл — попадания нет. Проверяется переводом mtime назад, а не
// ожиданием: часовой тест никто не станет прогонять.
func TestOpenMissesExpiredFile(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	key := Key{WorkID: 1, Start: 1, End: 2}

	if err := s.Put(key, raw); err != nil {
		t.Fatalf("Put: %v", err)
	}
	path := filepath.Join(dir, "w1", "1-2.json.gz")
	old := time.Now().Add(-TTL - time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	if _, _, ok := s.Open(key); ok {
		t.Error("просроченный файл отдан как попадание")
	}
}

func TestOpenMissesUnknownKey(t *testing.T) {
	s := New(t.TempDir())
	if _, _, ok := s.Open(Key{WorkID: 9, Start: 1, End: 1}); ok {
		t.Error("попадание по ключу, которого не писали")
	}
}

// Выключенное хранилище — не ошибка и не паника, а тишина: так собран
// сервер без PAGE_CACHE_DIR и так идут тесты обработчиков.
func TestDisabledStoreIsSilent(t *testing.T) {
	s := New("")
	if s.Enabled() {
		t.Error("Enabled() = true у хранилища без каталога")
	}
	if err := s.Put(Key{WorkID: 1, Start: 1, End: 1}, raw); err != nil {
		t.Errorf("Put на выключенном хранилище вернул ошибку: %v", err)
	}
	if _, _, ok := s.Open(Key{WorkID: 1, Start: 1, End: 1}); ok {
		t.Error("попадание на выключенном хранилище")
	}
}

// Недописанный файл читателю не виден: запись идёт во временный файл и
// въезжает на место одним Rename.
func TestPutLeavesNoTempFileBehind(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	if err := s.Put(Key{WorkID: 3, Start: 5, End: 6}, raw); err != nil {
		t.Fatalf("Put: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(dir, "w3"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "5-6.json.gz" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("в каталоге %v, ожидался ровно 5-6.json.gz", names)
	}
}

// Изменённый рендерер или изменённая форма ответа не должны доехать до
// читателя: смена коммита стирает всё, что лежало от прежней сборки.
func TestInitWipesCacheFromAnotherCommit(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	key := Key{WorkID: 47, Start: 1, End: 742}

	if err := s.Init("aaaa111"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Put(key, raw); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := s.Init("bbbb222"); err != nil {
		t.Fatalf("повторный Init: %v", err)
	}
	if _, _, ok := s.Open(key); ok {
		t.Error("файл от прежней сборки пережил смену коммита")
	}
}

// Перезапуск на том же коммите кэш сохраняет — иначе перезагрузка сервера
// оставляла бы читальню с холодным кэшем без всякой причины.
func TestInitKeepsCacheOnSameCommit(t *testing.T) {
	dir := t.TempDir()
	key := Key{WorkID: 47, Start: 1, End: 742}

	first := New(dir)
	if err := first.Init("aaaa111"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := first.Put(key, raw); err != nil {
		t.Fatalf("Put: %v", err)
	}

	second := New(dir)
	if err := second.Init("aaaa111"); err != nil {
		t.Fatalf("Init после перезапуска: %v", err)
	}
	if _, _, ok := second.Open(key); !ok {
		t.Error("перезапуск на том же коммите потерял кэш")
	}
}

func TestInitOnDisabledStoreDoesNothing(t *testing.T) {
	if err := New("").Init("aaaa111"); err != nil {
		t.Errorf("Init на выключенном хранилище вернул ошибку: %v", err)
	}
}

func TestDeleteRemovesOnlyItsOwnRange(t *testing.T) {
	s := New(t.TempDir())
	mine := Key{WorkID: 47, Start: 1, End: 100}
	neighbour := Key{WorkID: 47, Start: 101, End: 200}

	if err := s.Put(mine, raw); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(neighbour, raw); err != nil {
		t.Fatalf("Put соседа: %v", err)
	}

	removed, err := s.Delete(mine)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !removed {
		t.Error("Delete сказал, что удалять было нечего")
	}
	if _, _, ok := s.Open(mine); ok {
		t.Error("файл диапазона остался после Delete")
	}
	if _, _, ok := s.Open(neighbour); !ok {
		t.Error("Delete задел соседний диапазон")
	}
}

func TestDeleteOfMissingKeyIsNotAnError(t *testing.T) {
	s := New(t.TempDir())
	removed, err := s.Delete(Key{WorkID: 1, Start: 1, End: 1})
	if err != nil {
		t.Errorf("Delete несуществующего вернул ошибку: %v", err)
	}
	if removed {
		t.Error("Delete отчитался об удалении того, чего не было")
	}
}

func TestPurgeCountsAndKeepsStamp(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if err := s.Init("aaaa111"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Put(Key{WorkID: 1, Start: 1, End: 2}, raw); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(Key{WorkID: 2, Start: 1, End: 2}, raw); err != nil {
		t.Fatalf("Put: %v", err)
	}

	files, bytes, err := s.Purge()
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if files != 2 {
		t.Errorf("files = %d, ожидалось 2", files)
	}
	if bytes <= 0 {
		t.Errorf("bytes = %d, ожидался положительный объём", bytes)
	}
	if _, err := os.Stat(filepath.Join(dir, stampName)); err != nil {
		t.Errorf("Purge снёс отметку коммита: %v", err)
	}
}

func TestStatsCountsFilesAndOldest(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if err := s.Put(Key{WorkID: 1, Start: 1, End: 2}, raw); err != nil {
		t.Fatalf("Put: %v", err)
	}
	old := time.Now().Add(-30 * time.Minute)
	if err := os.Chtimes(filepath.Join(dir, "w1", "1-2.json.gz"), old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	st, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if !st.Enabled {
		t.Error("Enabled = false у включённого хранилища")
	}
	if st.Files != 1 {
		t.Errorf("Files = %d, ожидалось 1", st.Files)
	}
	if st.Bytes <= 0 {
		t.Errorf("Bytes = %d, ожидался положительный объём", st.Bytes)
	}
	if st.OldestAgeSeconds < 1700 || st.OldestAgeSeconds > 1900 {
		t.Errorf("OldestAgeSeconds = %d, ожидалось около 1800", st.OldestAgeSeconds)
	}
}

// Сводка на выключенном кэше — не ошибка: страница /admin/cache должна
// показать «выключен», а не 500.
func TestStatsOnDisabledStoreIsZeroAndFine(t *testing.T) {
	st, err := New("").Stats()
	if err != nil {
		t.Fatalf("Stats на выключенном хранилище: %v", err)
	}
	if st.Enabled || st.Files != 0 || st.Bytes != 0 {
		t.Errorf("сводка выключенного хранилища = %+v, ожидались нули", st)
	}
}

func TestSweepRemovesExpiredAndTempLeftovers(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	fresh := Key{WorkID: 1, Start: 1, End: 2}
	stale := Key{WorkID: 1, Start: 3, End: 4}

	for _, k := range []Key{fresh, stale} {
		if err := s.Put(k, raw); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	old := time.Now().Add(-TTL - time.Minute)
	if err := os.Chtimes(filepath.Join(dir, "w1", "3-4.json.gz"), old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	// Хвост от записи, упавшей на полпути.
	leftover := filepath.Join(dir, "w1", "abcdef.tmp")
	if err := os.WriteFile(leftover, []byte("огрызок"), 0o644); err != nil {
		t.Fatalf("подготовка хвоста: %v", err)
	}
	if err := os.Chtimes(leftover, old, old); err != nil {
		t.Fatalf("Chtimes хвоста: %v", err)
	}

	expired, _, err := s.Sweep()
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if expired != 2 {
		t.Errorf("expired = %d, ожидалось 2 (просроченный файл и хвост)", expired)
	}
	if _, _, ok := s.Open(fresh); !ok {
		t.Error("Sweep унёс свежий файл")
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Error("хвост .tmp пережил уборку")
	}
}

// Вытеснение по объёму — единственная защита от переполнения диска, и до
// этого теста ветка Sweep после уборки просроченного ни разу не исполнялась:
// потолок в 256 МБ константой тест на диске не поставить. maxBytes как поле
// снимает это без изменения поведения снаружи.
func TestSweepEvictsOldestWhenOverMaxBytes(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	oldest := Key{WorkID: 9, Start: 1, End: 2}
	middle := Key{WorkID: 9, Start: 3, End: 4}
	newest := Key{WorkID: 9, Start: 5, End: 6}

	for _, k := range []Key{oldest, middle, newest} {
		if err := s.Put(k, raw); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	pathOf := func(k Key) string {
		return filepath.Join(dir, k.workDir(), fmt.Sprintf("%d-%d.json.gz", k.Start, k.End))
	}
	info, err := os.Stat(pathOf(oldest))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	size := info.Size()

	now := time.Now()
	for k, at := range map[Key]time.Time{
		oldest: now.Add(-10 * time.Minute),
		middle: now.Add(-5 * time.Minute),
		newest: now,
	} {
		if err := os.Chtimes(pathOf(k), at, at); err != nil {
			t.Fatalf("Chtimes: %v", err)
		}
	}

	// Потолок ниже суммарного объёма трёх файлов, но не ниже двух: вытеснить
	// должен ровно самый старый.
	s.maxBytes = size * 2

	expired, evicted, err := s.Sweep()
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if expired != 0 {
		t.Errorf("expired = %d, ожидалось 0 (все файлы в пределах TTL)", expired)
	}
	if evicted != 1 {
		t.Errorf("evicted = %d, ожидалось 1 (реально исчез ровно один файл)", evicted)
	}
	if _, _, ok := s.Open(oldest); ok {
		t.Error("самый старый файл пережил вытеснение по объёму")
	}
	if _, _, ok := s.Open(middle); !ok {
		t.Error("вытеснен средний файл вместо самого старого")
	}
	if _, _, ok := s.Open(newest); !ok {
		t.Error("вытеснен самый свежий файл вместо самого старого")
	}

	st, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.Bytes > s.maxBytes {
		t.Errorf("после уборки Bytes = %d, ожидался не больше потолка %d", st.Bytes, s.maxBytes)
	}
}
