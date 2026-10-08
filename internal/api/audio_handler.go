package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/audio"
	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/pkg/book"
	"proofreader/pkg/storage"
)

// audioURLTTL — срок подписанной ссылки на звук. Не S3_PRESIGN_TTL (30 мин):
// <audio> дозапрашивает файл диапазонами по тому же адресу, и слушатель,
// простоявший на паузе дольше срока, получил бы отказ на перемотке.
const audioURLTTL = 12 * time.Hour

// uploadURLTTL — срок ссылки на заливку одного файла.
const uploadURLTTL = 30 * time.Minute

type AudioTrackStore interface {
	ListTracks(ctx context.Context, workID int64) ([]models.AudioTrack, error)
	StaleTracks(ctx context.Context, workID int64) ([]models.AudioTrack, error)
	Track(ctx context.Context, id int64) (models.AudioTrack, error)
	RegisterTracks(ctx context.Context, workID int64, tracks []models.AudioTrack) ([]models.AudioTrack, []models.AudioTrack, error)
	KeyReferenced(ctx context.Context, key string) (bool, error)
}
type AudioQueueStore interface {
	Enqueue(ctx context.Context, workID int64, chapterID *int64, by *int64) (models.AudioQueueItem, bool, error)
	ListOpenQueue(ctx context.Context) ([]models.AudioQueueItem, error)
	StaleSummary(ctx context.Context) ([]models.AudioStaleWork, error)
	CancelQueueItem(ctx context.Context, id int64) error
	RetryQueueItem(ctx context.Context, id int64) (models.AudioQueueItem, error)
	Claim(ctx context.Context, ids []int64, reclaim bool) ([]models.AudioQueueItem, error)
	FinishQueueItem(ctx context.Context, id int64, claimedAt time.Time, status, errText string, counts map[string]int) error
}
type AudioRecordingStore interface {
	CreateRecording(ctx context.Context, rec *models.AudioRecording) error
	ListRecordings(ctx context.Context, workID int64) ([]models.AudioRecording, error)
	Recording(ctx context.Context, id int64) (models.AudioRecording, error)
	UpdateRecording(ctx context.Context, id int64, reader *string, position *int) (models.AudioRecording, error)
	DeleteRecording(ctx context.Context, id int64) (string, error)
}
type AudioChapterStore interface {
	GetByID(ctx context.Context, id int64) (*models.Chapter, error)
	FindByPage(ctx context.Context, workID int64, pageNumber int) (*models.Chapter, error)
}

// AudioHandler — озвучка: чтение дорожек и записей, редирект на звук,
// очередь для worker'а, заливка и регистрация. store — АУДИОбакет, не
// основной (спека аудиокниг 29.09, «Хранилище»).
type AudioHandler struct {
	tracks   AudioTrackStore
	queue    AudioQueueStore
	recs     AudioRecordingStore
	chapters AudioChapterStore
	store    storage.Storage
}

func NewAudioHandler(tracks AudioTrackStore, queue AudioQueueStore, recs AudioRecordingStore,
	chapters AudioChapterStore, store storage.Storage) *AudioHandler {
	return &AudioHandler{tracks: tracks, queue: queue, recs: recs, chapters: chapters, store: store}
}

func pathID(w http.ResponseWriter, r *http.Request, name, what string) (int64, bool) {
	id, err := strconv.ParseInt(mux.Vars(r)[name], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Неверный идентификатор: "+what)
		return 0, false
	}
	return id, true
}

// mapAudioErr переводит ошибку хранилища в ответ.
func mapAudioErr(w http.ResponseWriter, err error, notFound string) {
	switch {
	case errors.Is(err, repository.ErrAudioNotFound):
		writeError(w, http.StatusNotFound, notFound)
	case errors.Is(err, repository.ErrAudioConflict):
		writeError(w, http.StatusConflict, "Заявка в другом состоянии — обновите список")
	default:
		log.Printf("озвучка: %v", err)
		writeError(w, http.StatusInternalServerError, "Ошибка озвучки на сервере")
	}
}

type trackView struct {
	models.AudioTrack
	URL string `json:"url"`
}

type recordingView struct {
	models.AudioRecording
	URL string `json:"url"`
}

// WorkAudio — GET /works/{id}/audio: дорожки тома по началу и записи
// человека. Адреса относительные: фронт и worker ходят в тот же хост.
func (h *AudioHandler) WorkAudio(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id", "тома")
	if !ok {
		return
	}
	tracks, err := h.tracks.ListTracks(r.Context(), id)
	if err != nil {
		mapAudioErr(w, err, "")
		return
	}
	recs, err := h.recs.ListRecordings(r.Context(), id)
	if err != nil {
		mapAudioErr(w, err, "")
		return
	}
	out := struct {
		Tracks     []trackView     `json:"tracks"`
		Recordings []recordingView `json:"recordings"`
	}{Tracks: []trackView{}, Recordings: []recordingView{}}
	for _, t := range tracks {
		out.Tracks = append(out.Tracks, trackView{t, audio.TrackURL("", t.ID)})
	}
	for _, x := range recs {
		out.Recordings = append(out.Recordings, recordingView{x, audio.RecordingURL("", x.ID)})
	}
	writeJSONStatus(w, http.StatusOK, out)
}

// audioFile — объект звука и имя, под которым его сохранит «скачать».
type audioFile struct {
	key   string
	ascii string // запаска filename= для браузеров без filename*
	human string
	ext   string
}

// Точка в конце имени дала бы «..opus»: у заголовка («Товар.») и у
// инициалов чтеца («Иванов И. И.»).
func trimTitle(s string) string { return strings.TrimRight(strings.TrimSpace(s), ". ") }

func trackFile(t models.AudioTrack) audioFile {
	return audioFile{key: t.S3Key, ascii: fmt.Sprintf("track-%d", t.ID), human: trimTitle(t.Title), ext: "opus"}
}

// Номер записи в имени обязателен: у главы бывает несколько записей, и без
// него вторая сохранялась бы поверх первой.
func recordingFile(x models.AudioRecording) audioFile {
	human := fmt.Sprintf("%s. Запись %d", trimTitle(x.ChapterTitle), x.Position)
	if x.Reader != "" {
		human += " — " + x.Reader
	}
	// Расширение — из ключа: его ставит RecordingKey по типу при заливке.
	ext := strings.TrimPrefix(path.Ext(x.S3Key), ".")
	return audioFile{key: x.S3Key, ascii: fmt.Sprintf("recording-%d", x.ID), human: trimTitle(human), ext: ext}
}

// redirect — 302 на подписанную ссылку; строки нет — 410: адрес дорожки
// стабилен, и снятый том обязан пропасть по нему сразу. ?download=1 —
// ссылка с attachment: хранилище на чужом домене, и атрибут download у
// <a> браузер там игнорирует — без заголовка файл заиграл бы во вкладке.
func (h *AudioHandler) redirect(w http.ResponseWriter, r *http.Request, file func(context.Context, int64) (audioFile, error)) {
	id, ok := pathID(w, r, "id", "дорожки")
	if !ok {
		return
	}
	f, err := file(r.Context(), id)
	if errors.Is(err, repository.ErrAudioNotFound) {
		writeError(w, http.StatusGone, "Записи больше нет")
		return
	}
	if err != nil {
		mapAudioErr(w, err, "")
		return
	}
	var url string
	if r.URL.Query().Get("download") == "1" {
		url, err = h.store.PresignGetAttachment(r.Context(), f.key, audioURLTTL,
			book.ContentDisposition(f.ascii, f.human, f.ext))
	} else {
		url, err = h.store.PresignGet(r.Context(), f.key, audioURLTTL)
	}
	if err != nil {
		log.Printf("ссылка на звук %s: %v", f.key, err)
		writeError(w, http.StatusInternalServerError, "Не удалось выдать ссылку на звук")
		return
	}
	// Подписанная ссылка истекает — кэшировать редирект нельзя.
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, url, http.StatusFound)
}

func (h *AudioHandler) TrackRedirect(w http.ResponseWriter, r *http.Request) {
	h.redirect(w, r, func(ctx context.Context, id int64) (audioFile, error) {
		t, err := h.tracks.Track(ctx, id)
		return trackFile(t), err
	})
}

func (h *AudioHandler) RecordingRedirect(w http.ResponseWriter, r *http.Request) {
	h.redirect(w, r, func(ctx context.Context, id int64) (audioFile, error) {
		x, err := h.recs.Recording(ctx, id)
		return recordingFile(x), err
	})
}
