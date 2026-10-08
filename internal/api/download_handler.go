package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/middleware"
	"proofreader/pkg/book"
)

// downloadWriteDeadline — сколько времени даётся на отдачу файла. Штатные 15
// секунд WriteTimeout обрезают том на 907 страниц у медленного клиента, а
// нулевое время сняло бы ограничение совсем и оставило висящие соединения.
const downloadWriteDeadline = 10 * time.Minute

// DownloadHandler отдаёт главу, работу или подборка файлом.
type DownloadHandler struct {
	source *DownloadSource
	audio  *AudioPlaylist
}

// WithAudio включает ?format=m3u. Плейлист — не book.Writer: он читает
// audio_tracks, а не собирает книгу, поэтому ветка стоит до ForFormat.
func (h *DownloadHandler) WithAudio(p *AudioPlaylist) *DownloadHandler {
	h.audio = p
	return h
}

const formatM3U = "m3u"

func (h *DownloadHandler) m3u(w http.ResponseWriter, r *http.Request, workID, chapterID int64) {
	if h.audio == nil {
		http.Error(w, "Звука в читальне нет", http.StatusNotFound)
		return
	}
	h.audio.serve(r.Context(), w, workID, chapterID)
}

func NewDownloadHandler(source *DownloadSource) *DownloadHandler {
	return &DownloadHandler{source: source}
}

// Work отдаёт том целиком.
func (h *DownloadHandler) Work(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		http.Error(w, "Неверный идентификатор работы", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("format") == formatM3U {
		h.m3u(w, r, id, 0)
		return
	}
	h.serve(w, r, fmt.Sprintf("work-%d", id), func(ctx context.Context) (*book.Book, error) {
		return h.source.Work(ctx, id)
	})
}

// Chapter отдаёт одну главу вместе с её поддеревом.
func (h *DownloadHandler) Chapter(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workID, err := strconv.ParseInt(vars["workId"], 10, 64)
	if err != nil {
		http.Error(w, "Неверный идентификатор работы", http.StatusBadRequest)
		return
	}
	chapterID, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Неверный идентификатор главы", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("format") == formatM3U {
		h.m3u(w, r, workID, chapterID)
		return
	}
	h.serve(w, r, fmt.Sprintf("chapter-%d", chapterID), func(ctx context.Context) (*book.Book, error) {
		return h.source.Chapter(ctx, workID, chapterID)
	})
}

// Collection отдаёт подборка.
//
// Гейт видимости (черновик/снятая с публикации — не сотруднику) стоит здесь,
// а не внутри DownloadSource.Collection: источник собирает книгу без claims
// вовсе, а без явной проверки до h.serve снятая с публикации подборка
// оставалась бы доступна по адресу скачивания — ссылка получается из
// разосланной дописыванием одного сегмента, угадывать нечего.
func (h *DownloadHandler) Collection(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("format") == formatM3U {
		http.Error(w, "Плейлист есть у тома и у главы, у подборки его нет", http.StatusBadRequest)
		return
	}
	nickname, slug := collectionKey(r)
	if slug == "" {
		http.Error(w, "Не указан подборка", http.StatusBadRequest)
		return
	}

	collection, err := h.source.collections.GetByAuthorSlug(r.Context(), nickname, slug)
	if err != nil || collection == nil {
		http.Error(w, "Не найдено", http.StatusNotFound)
		return
	}
	claims, _ := middleware.GetUserFromContext(r.Context())
	if status, message, ok := collectionVisibleTo(claims, collection); !ok {
		http.Error(w, message, status)
		return
	}

	h.serve(w, r, "collection-"+asciiSlug(slug), func(ctx context.Context) (*book.Book, error) {
		return h.source.Collection(ctx, nickname, slug)
	})
}

// serve — общая часть всех трёх маршрутов: разбор формата, сборка книги,
// ETag и отдача.
func (h *DownloadHandler) serve(
	w http.ResponseWriter, r *http.Request, asciiBase string,
	build func(context.Context) (*book.Book, error),
) {
	format := r.URL.Query().Get("format")
	writer, ok := book.ForFormat(format)
	if !ok {
		http.Error(w,
			fmt.Sprintf("Неизвестный формат %q. Доступны: %s", format, strings.Join(append(book.Formats(), formatM3U), ", ")),
			http.StatusBadRequest)
		return
	}

	b, err := build(r.Context())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.Error(w, "Не найдено", http.StatusNotFound)
			return
		}
		log.Printf("выгрузка %s: %v", asciiBase, err)
		http.Error(w, "Не удалось собрать файл", http.StatusInternalServerError)
		return
	}

	// Выгрузка побайтово воспроизводима, поэтому метка кэша складывается из
	// сущности, формата и CacheKey — max(Modified, UpdatedAt сущностей,
	// формирующих файл). Modified один описывает только текст страниц:
	// переименование главы, её перенос, смещение печатной нумерации или
	// правка состава подборки меняют байты файла, не трогая ни одной
	// страницы, и Modified в одиночку эти правки не увидел бы (см.
	// DownloadSource.Work/Chapter/Collection в download_source.go).
	etag := downloadETag(asciiBase, format, b.Meta.CacheKey)
	w.Header().Set("ETag", etag)
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", writer.ContentType())
	w.Header().Set("Content-Disposition",
		book.ContentDisposition(asciiBase, b.Meta.Title, writer.Ext()))

	// Дедлайн раздвигается тем же приёмом, что в ExportHandler: см.
	// responseWriter.Unwrap в internal/middleware/logging.go — без него
	// контроллер не доберётся до настоящего соединения.
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(downloadWriteDeadline)); err != nil {
		log.Printf("выгрузка %s: не удалось раздвинуть дедлайн записи: %v", asciiBase, err)
	}

	if err := writer.Write(w, b); err != nil {
		// Байты уже на проводе, статусом эту ошибку не сообщить: клиент
		// получит оборванный файл.
		log.Printf("выгрузка %s прервана: %v", asciiBase, err)
	}
}

func downloadETag(asciiBase, format string, modified time.Time) string {
	sum := sha256.Sum256([]byte(asciiBase + "|" + format + "|" + modified.UTC().Format(time.RFC3339)))
	return `"` + hex.EncodeToString(sum[:8]) + `"`
}

// asciiSlug оставляет от адреса подборки только то, что можно положить в
// ASCII-запаску имени файла.
func asciiSlug(slug string) string {
	var b strings.Builder
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		default:
			b.WriteRune('-')
		}
	}
	if b.Len() == 0 {
		return "collection"
	}
	return b.String()
}
