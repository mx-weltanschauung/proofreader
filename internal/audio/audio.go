// Package audio — чистые правила озвучки: отпечаток текста полос, ключи
// объектов в аудиобакете и плейлист. Без базы и хранилища, чтобы правила
// проверялись без них (спека аудиокниг 29.09).
package audio

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"proofreader/internal/models"
)

// PageSeparator склеивает тексты полос. Нулевого байта в тексте полосы нет,
// поэтому граница полос не спутается с текстом.
const PageSeparator = "\n\x00\n"

// PagesSHA256 — отпечаток сырого content_markdown полос диапазона по
// возрастанию номера. Считается одинаково здесь и в tools/tts/jobs.py
// (общая фикстура testdata/pages_sha256.json): бэкенд проверяет свежесть
// звука, не зная нормализации синтезатора.
func PagesSHA256(texts []string) string {
	sum := sha256.Sum256([]byte(strings.Join(texts, PageSeparator)))
	return hex.EncodeToString(sum[:])
}

// ValidSHA256 — 64 знака нижнего регистра [0-9a-f]. Проверка обязательна до
// TrackKey: хэш входит в ключ объекта, и «../» в нём был бы путём.
func ValidSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// WorkPrefix — всё, что лежит в аудиобакете от тома.
func WorkPrefix(workID int64) string { return fmt.Sprintf("works/%d/", workID) }

// TrackKey — ключ синтезированной дорожки. Меняется с рецептом, диапазоном,
// текстом полос И заголовком (он произносится, worker переозвучивает
// переименованную главу): переозвучка не перезаписывает объект, который
// сейчас слушают, и кэш браузера не держит старый звук под старым адресом.
func TrackKey(workID int64, recipeSHA, pagesSHA string, start, end int, title string) string {
	t := sha256.Sum256([]byte(title))
	return fmt.Sprintf("works/%d/%s/%d-%d-%s-%s.opus", workID, recipeSHA[:8], start, end, pagesSHA[:8],
		hex.EncodeToString(t[:4]))
}

var (
	trackKeyRe = regexp.MustCompile(`^works/(\d+)/[0-9a-f]{8}/\d+-\d+-[0-9a-f]{8}-[0-9a-f]{8}\.opus$`)
	recKeyRe   = regexp.MustCompile(`^works/(\d+)/rec/\d+/[0-9A-Za-z_-]+\.([a-z0-9]+)$`)
)

// IsAudioKey — ключ имеет форму, которую заводит озвучка тома workID:
// синтезированная дорожка или запись человека с допустимым расширением.
// Страховка удаления: чужая форма (превью, PDF) не проходит, даже если
// хранилище подключено не то.
func IsAudioKey(workID int64, key string) bool {
	if strings.Contains(key, "..") {
		return false
	}
	id := fmt.Sprint(workID)
	if m := trackKeyRe.FindStringSubmatch(key); m != nil {
		return m[1] == id
	}
	if m := recKeyRe.FindStringSubmatch(key); m != nil && m[1] == id {
		for _, ext := range models.RecordingContentTypes {
			if ext == m[2] {
				return true
			}
		}
	}
	return false
}

// RecordingKey — ключ записи человека.
func RecordingKey(workID, chapterID int64, token, ext string) string {
	return fmt.Sprintf("works/%d/rec/%d/%s.%s", workID, chapterID, token, ext)
}

func TrackURL(base string, id int64) string     { return fmt.Sprintf("%s/api/audio/%d.opus", base, id) }
func RecordingURL(base string, id int64) string { return fmt.Sprintf("%s/api/audio/rec/%d", base, id) }

// Entry — строка плейлиста.
type Entry struct {
	Title      string
	DurationMS int64
	URL        string
}

// Playlist — записи человека и синтез для диапазона [start, end]; нули —
// весь том. Порядок — по началу диапазона; на одном начале запись человека
// идёт раньше синтеза (решение владельца 30.09: слушатель видит обе, ручную
// первой), записи главы — вместе и по position. Глава и её первая подглава
// начинаются на одной полосе: объемлющая (конец дальше) идёт первой, как при
// обходе дерева сверху вниз, а при совпавшем диапазоне главы разводит номер —
// иначе их записи перемешивались по position.
//
// Запись человека попадает в плейлист главы, если её глава лежит внутри
// диапазона (сама глава и её подглавы); синтез — если пересекается с ним.
func Playlist(tracks []models.AudioTrack, recs []models.AudioRecording, start, end int, base string) []Entry {
	whole := start == 0 && end == 0
	type item struct {
		start, kind, end int
		chapter          int64
		pos              int
		e                Entry
	}
	perChapter := map[int64]int{}
	for _, r := range recs {
		perChapter[r.ChapterID]++
	}
	var items []item
	for _, r := range recs {
		if !whole && (r.ChapterStartPage < start || r.ChapterEndPage > end) {
			continue
		}
		title := r.ChapterTitle
		if r.Reader != "" {
			title += " — читает " + r.Reader
		}
		if n := perChapter[r.ChapterID]; n > 1 {
			title += fmt.Sprintf(" (%d из %d)", r.Position, n)
		}
		items = append(items, item{r.ChapterStartPage, 0, r.ChapterEndPage, r.ChapterID, r.Position,
			Entry{title, r.DurationMS, RecordingURL(base, r.ID)}})
	}
	for _, t := range tracks {
		if !whole && (t.StartPage > end || t.EndPage < start) {
			continue
		}
		items = append(items, item{t.StartPage, 1, t.EndPage, 0, 0, Entry{t.Title, t.DurationMS, TrackURL(base, t.ID)}})
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.start != b.start {
			return a.start < b.start
		}
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		if a.end != b.end {
			return a.end > b.end
		}
		if a.chapter != b.chapter {
			return a.chapter < b.chapter
		}
		return a.pos < b.pos
	})
	out := make([]Entry, len(items))
	for i, it := range items {
		out[i] = it.e
	}
	return out
}

// WriteM3U пишет расширенный плейлист. Перевод строки в заголовке ломал бы
// формат (строка после #EXTINF — адрес), поэтому сводится к пробелу.
func WriteM3U(w io.Writer, entries []Entry) error {
	if _, err := io.WriteString(w, "#EXTM3U\n"); err != nil {
		return err
	}
	for _, e := range entries {
		title := strings.Join(strings.Fields(e.Title), " ")
		if _, err := fmt.Fprintf(w, "#EXTINF:%d,%s\n%s\n", (e.DurationMS+500)/1000, title, e.URL); err != nil {
			return err
		}
	}
	return nil
}
