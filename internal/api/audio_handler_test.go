package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/auth"
	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/pkg/storage"
)

type fakeAudio struct {
	tracks    []models.AudioTrack
	recs      []models.AudioRecording
	queue     []models.AudioQueueItem
	enqueued  []*int64
	claimArgs struct {
		ids     []int64
		reclaim bool
	}
	finished   map[int64]string
	claimedAt  time.Time
	registered []models.AudioTrack
	freed      []models.AudioTrack
	referenced map[string]bool
	err        error
}

func (f *fakeAudio) ListTracks(context.Context, int64) ([]models.AudioTrack, error) {
	return f.tracks, f.err
}
func (f *fakeAudio) StaleTracks(context.Context, int64) ([]models.AudioTrack, error) {
	var out []models.AudioTrack
	for _, t := range f.tracks {
		if t.Stale {
			out = append(out, t)
		}
	}
	return out, f.err
}
func (f *fakeAudio) Track(_ context.Context, id int64) (models.AudioTrack, error) {
	for _, t := range f.tracks {
		if t.ID == id {
			return t, nil
		}
	}
	return models.AudioTrack{}, repository.ErrAudioNotFound
}
func (f *fakeAudio) RegisterTracks(_ context.Context, _ int64, ts []models.AudioTrack) ([]models.AudioTrack, []models.AudioTrack, error) {
	f.registered = ts
	for i := range ts {
		ts[i].ID = int64(100 + i)
	}
	return ts, f.freed, f.err
}
func (f *fakeAudio) KeyReferenced(_ context.Context, key string) (bool, error) {
	return f.referenced[key], nil
}
func (f *fakeAudio) Enqueue(_ context.Context, w int64, c *int64, _ *int64) (models.AudioQueueItem, bool, error) {
	f.enqueued = append(f.enqueued, c)
	return models.AudioQueueItem{ID: int64(len(f.enqueued)), WorkID: w, ChapterID: c, Status: models.AudioQueued}, true, f.err
}
func (f *fakeAudio) ListOpenQueue(context.Context) ([]models.AudioQueueItem, error) {
	return f.queue, f.err
}
func (f *fakeAudio) StaleSummary(context.Context) ([]models.AudioStaleWork, error) { return nil, f.err }
func (f *fakeAudio) CancelQueueItem(context.Context, int64) error                  { return f.err }
func (f *fakeAudio) RetryQueueItem(_ context.Context, id int64) (models.AudioQueueItem, error) {
	return models.AudioQueueItem{ID: id}, f.err
}
func (f *fakeAudio) Claim(_ context.Context, ids []int64, reclaim bool) ([]models.AudioQueueItem, error) {
	f.claimArgs.ids, f.claimArgs.reclaim = ids, reclaim
	return f.queue, f.err
}
func (f *fakeAudio) FinishQueueItem(_ context.Context, id int64, claimedAt time.Time, status, _ string, _ map[string]int) error {
	f.claimedAt = claimedAt
	if f.finished == nil {
		f.finished = map[int64]string{}
	}
	f.finished[id] = status
	return f.err
}
func (f *fakeAudio) CreateRecording(_ context.Context, rec *models.AudioRecording) error {
	rec.ID, rec.Position = 55, 1
	f.recs = append(f.recs, *rec)
	return f.err
}
func (f *fakeAudio) ListRecordings(context.Context, int64) ([]models.AudioRecording, error) {
	return f.recs, f.err
}
func (f *fakeAudio) Recording(_ context.Context, id int64) (models.AudioRecording, error) {
	for _, r := range f.recs {
		if r.ID == id {
			return r, nil
		}
	}
	return models.AudioRecording{}, repository.ErrAudioNotFound
}
func (f *fakeAudio) UpdateRecording(_ context.Context, id int64, reader *string, pos *int) (models.AudioRecording, error) {
	return models.AudioRecording{ID: id}, f.err
}
func (f *fakeAudio) DeleteRecording(_ context.Context, id int64) (string, error) {
	r, err := f.Recording(context.Background(), id)
	return r.S3Key, err
}

type fakeAudioChapters struct{ byID map[int64]*models.Chapter }

func (f fakeAudioChapters) GetByID(_ context.Context, id int64) (*models.Chapter, error) {
	if c, ok := f.byID[id]; ok {
		return c, nil
	}
	return nil, errors.New("chapter not found")
}
func (f fakeAudioChapters) FindByPage(_ context.Context, workID int64, page int) (*models.Chapter, error) {
	var best *models.Chapter
	for _, c := range f.byID {
		if c.WorkID == workID && c.StartPage <= page && page <= c.EndPage &&
			(best == nil || c.EndPage-c.StartPage < best.EndPage-best.StartPage) {
			best = c
		}
	}
	return best, nil
}

func newAudioTestHandler() (*AudioHandler, *fakeAudio, *storage.MemoryStorage) {
	f := &fakeAudio{}
	mem := storage.NewMemoryStorage()
	ch := fakeAudioChapters{byID: map[int64]*models.Chapter{
		5: {ID: 5, WorkID: 47, Title: "Глава первая", StartPage: 10, EndPage: 20},
		6: {ID: 6, WorkID: 48, Title: "чужая", StartPage: 1, EndPage: 2},
	}}
	return NewAudioHandler(f, f, f, ch, mem), f, mem
}

func audioRequest(method, url string, body any, vars map[string]string) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, url, &buf)
	req = mux.SetURLVars(req, vars)
	claims := &auth.Claims{UserID: 1, Role: models.RoleEditor}
	return req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, claims))
}

func TestTrackRedirectSignsLongLivedURL(t *testing.T) {
	h, f, _ := newAudioTestHandler()
	f.tracks = []models.AudioTrack{{ID: 3, S3Key: "works/47/r/1-2-p.opus"}}
	rec := httptest.NewRecorder()
	h.TrackRedirect(rec, audioRequest("GET", "/api/audio/3.opus", nil, map[string]string{"id": "3"}))
	if rec.Code != http.StatusFound {
		t.Fatalf("код %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	// Срок ссылки ≥ 12 ч: <audio> дозапрашивает файл диапазонами, пауза не должна
	// ломать перемотку (MemoryStorage пишет срок в адрес).
	if !strings.Contains(loc, "works/47/r/1-2-p.opus") || !strings.Contains(loc, "ttl="+(12*time.Hour).String()) {
		t.Errorf("Location = %q", loc)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
}

// curl -I и часть проигрывателей спрашивают звук HEAD'ом раньше GET: без
// него роутер отвечал 405, и проигрыватель считал адрес мёртвым.
func TestAudioRedirectsAnswerHEADThroughRouter(t *testing.T) {
	h, f, _ := newAudioTestHandler()
	f.tracks = []models.AudioTrack{{ID: 3, S3Key: "works/47/r/1-2-p.opus"}}
	f.recs = []models.AudioRecording{{ID: 7, S3Key: "works/47/rec/5/x.mp3"}}
	r := newTestRouter(t).WithAudio(h).Setup()
	for _, url := range []string{"/api/audio/3.opus", "/api/audio/rec/7"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, url, nil))
		if rec.Code != http.StatusFound || rec.Header().Get("Location") == "" {
			t.Errorf("HEAD %s: %d, Location %q", url, rec.Code, rec.Header().Get("Location"))
		}
	}
}

func TestTrackRedirectMissingIsGone(t *testing.T) {
	h, _, _ := newAudioTestHandler()
	rec := httptest.NewRecorder()
	h.TrackRedirect(rec, audioRequest("GET", "/api/audio/9.opus", nil, map[string]string{"id": "9"}))
	if rec.Code != http.StatusGone {
		t.Errorf("код %d, ждали 410", rec.Code)
	}
}

func TestWorkAudioListsTracksAndRecordingsWithURLs(t *testing.T) {
	h, f, _ := newAudioTestHandler()
	f.tracks = []models.AudioTrack{{ID: 3, Title: "Глава", StartPage: 1, EndPage: 2, Stale: true,
		RecipeSHA256: "r", PagesSHA256: "p", S3Key: "секрет", MD5: "md5секрет"}}
	f.recs = []models.AudioRecording{{ID: 7, ChapterID: 5, Reader: "Иванов", S3Key: "секрет"}}
	rec := httptest.NewRecorder()
	h.WorkAudio(rec, audioRequest("GET", "/api/works/47/audio", nil, map[string]string{"id": "47"}))
	body := rec.Body.String()
	for _, want := range []string{`"url":"/api/audio/3.opus"`, `"url":"/api/audio/rec/7"`, `"stale":true`,
		`"recipe_sha256":"r"`, `"pages_sha256":"p"`, `"reader":"Иванов"`} {
		if !strings.Contains(body, want) {
			t.Errorf("нет %s в %s", want, body)
		}
	}
	if strings.Contains(body, "секрет") || strings.Contains(body, "md5") {
		t.Errorf("ключ хранилища утёк наружу: %s", body)
	}
}

func TestEnqueueRefusesForeignChapter(t *testing.T) {
	h, f, _ := newAudioTestHandler()
	rec := httptest.NewRecorder()
	h.Enqueue(rec, audioRequest("POST", "/api/works/47/audio/queue", map[string]any{"chapter_id": 6},
		map[string]string{"id": "47"}))
	if rec.Code != http.StatusNotFound || len(f.enqueued) != 0 {
		t.Errorf("глава чужого тома: %d, поставлено %d", rec.Code, len(f.enqueued))
	}
	rec = httptest.NewRecorder()
	h.Enqueue(rec, audioRequest("POST", "/api/works/47/audio/queue", map[string]any{"chapter_id": 5},
		map[string]string{"id": "47"}))
	if rec.Code != http.StatusCreated || len(f.enqueued) != 1 || *f.enqueued[0] != 5 {
		t.Errorf("своя глава: %d %v", rec.Code, f.enqueued)
	}
}

func TestRequeueStaleQueuesNarrowestChapterOncePerChapter(t *testing.T) {
	h, f, _ := newAudioTestHandler()
	f.tracks = []models.AudioTrack{
		{ID: 1, StartPage: 10, EndPage: 12, Stale: true},
		{ID: 2, StartPage: 13, EndPage: 20, Stale: true}, // та же глава 5
		{ID: 3, StartPage: 30, EndPage: 31, Stale: true}, // ничем не накрыта — весь том
		{ID: 4, StartPage: 1, EndPage: 9},                // свежая
	}
	rec := httptest.NewRecorder()
	h.RequeueStale(rec, audioRequest("POST", "/api/works/47/audio/requeue-stale", nil, map[string]string{"id": "47"}))
	if rec.Code != http.StatusOK || len(f.enqueued) != 2 {
		t.Fatalf("код %d, заявок %d", rec.Code, len(f.enqueued))
	}
	if f.enqueued[0] == nil || *f.enqueued[0] != 5 || f.enqueued[1] != nil {
		t.Errorf("заявки: %v", f.enqueued)
	}
}

// Слипание уносит титул перед первой главой в её дорожку: начало дорожки вне
// глав (у 114 томов из 229 полосы до первой главы есть), конец — в главе.
// Такая дорожка обязана встать заявкой на главу своего конца, а не на весь том.
func TestRequeueStaleFallsBackToEndPageChapter(t *testing.T) {
	h, f, _ := newAudioTestHandler()
	f.tracks = []models.AudioTrack{{ID: 1, StartPage: 8, EndPage: 12, Stale: true}}
	rec := httptest.NewRecorder()
	h.RequeueStale(rec, audioRequest("POST", "/api/works/47/audio/requeue-stale", nil, map[string]string{"id": "47"}))
	if rec.Code != http.StatusOK || len(f.enqueued) != 1 {
		t.Fatalf("код %d, заявок %d", rec.Code, len(f.enqueued))
	}
	if f.enqueued[0] == nil || *f.enqueued[0] != 5 {
		t.Errorf("заявка %v, ждали главу 5 по концу дорожки", f.enqueued[0])
	}
}

func TestClaimPassesIDsAndReclaim(t *testing.T) {
	h, f, _ := newAudioTestHandler()
	rec := httptest.NewRecorder()
	h.Claim(rec, audioRequest("POST", "/api/audio/queue/claim", map[string]any{"ids": []int{4, 9}, "reclaim": true}, nil))
	if rec.Code != http.StatusOK || len(f.claimArgs.ids) != 2 || !f.claimArgs.reclaim {
		t.Errorf("код %d, аргументы %+v", rec.Code, f.claimArgs)
	}
	rec = httptest.NewRecorder()
	h.Claim(rec, audioRequest("POST", "/api/audio/queue/claim", map[string]any{}, nil))
	if f.claimArgs.ids != nil {
		t.Errorf("без ids обязано уйти nil (все), ушло %v", f.claimArgs.ids)
	}
}

const claimedT = "2026-09-30T10:00:00.123456Z"

func TestResultValidatesStatus(t *testing.T) {
	h, f, _ := newAudioTestHandler()
	for _, c := range []struct {
		body map[string]any
		code int
	}{
		{map[string]any{"status": "готово", "claimed_at": claimedT}, http.StatusNoContent},
		{map[string]any{"status": "в_очереди", "claimed_at": claimedT}, http.StatusNoContent},
		{map[string]any{"status": "ошибка", "claimed_at": claimedT}, http.StatusBadRequest}, // без текста
		{map[string]any{"status": "ошибка", "error": "упало", "claimed_at": claimedT}, http.StatusNoContent},
		{map[string]any{"status": "синтезируется", "claimed_at": claimedT}, http.StatusBadRequest},
		// Тикет 01: без отметки забора итог не принимается — сверять нечего.
		{map[string]any{"status": "готово"}, http.StatusBadRequest},
	} {
		rec := httptest.NewRecorder()
		h.Result(rec, audioRequest("POST", "/api/audio/queue/3/result", c.body, map[string]string{"id": "3"}))
		if rec.Code != c.code {
			t.Errorf("%v: код %d, ждали %d", c.body, rec.Code, c.code)
		}
	}
	// Отметка доходит до хранилища с точностью до микросекунды: Postgres
	// сравнивает её на равенство.
	if want, _ := time.Parse(time.RFC3339Nano, claimedT); !f.claimedAt.Equal(want) {
		t.Errorf("отметка забора в хранилище: %v, ждали %v", f.claimedAt, want)
	}
	f.err = repository.ErrAudioConflict
	rec := httptest.NewRecorder()
	h.Result(rec, audioRequest("POST", "/api/audio/queue/3/result", map[string]any{"status": "готово", "claimed_at": claimedT},
		map[string]string{"id": "3"}))
	if rec.Code != http.StatusConflict {
		t.Errorf("конфликт: %d", rec.Code)
	}
}

func TestRecordingRedirectSignsLongLivedURL(t *testing.T) {
	h, f, _ := newAudioTestHandler()
	f.recs = []models.AudioRecording{{ID: 7, S3Key: "works/47/rec/7.mp3"}}
	f.tracks = []models.AudioTrack{{ID: 7, S3Key: "works/47/r/track.opus"}}
	rec := httptest.NewRecorder()
	h.RecordingRedirect(rec, audioRequest("GET", "/api/audio/rec/7", nil, map[string]string{"id": "7"}))
	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusFound || !strings.Contains(loc, "works/47/rec/7.mp3") ||
		!strings.Contains(loc, "ttl="+(12*time.Hour).String()) {
		t.Errorf("код %d, Location = %q", rec.Code, loc)
	}
}

// downloadName — имя файла, которое браузер возьмёт из подписанной ссылки:
// MemoryStorage кладёт Content-Disposition в параметр disposition, а
// mime.ParseMediaType раскодирует filename* (RFC 8187) в "filename".
func downloadName(t *testing.T, loc string) string {
	t.Helper()
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	disp := u.Query().Get("disposition")
	if disp == "" {
		t.Fatalf("ссылка без Content-Disposition: %q", loc)
	}
	kind, params, err := mime.ParseMediaType(disp)
	if err != nil || kind != "attachment" {
		t.Fatalf("Content-Disposition %q: %q %v", disp, kind, err)
	}
	return params["filename"]
}

// «Скачать» — та же дорожка, подписанная с attachment: ссылка ведёт на чужой
// домен хранилища, и без заголовка браузер играл бы файл во вкладке.
func TestTrackDownloadNamesFileByTitle(t *testing.T) {
	h, f, _ := newAudioTestHandler()
	f.tracks = []models.AudioTrack{{ID: 3, Title: "Товар. Часть первая.", S3Key: "works/47/r/1-2-p.opus"}}
	rec := httptest.NewRecorder()
	h.TrackRedirect(rec, audioRequest("GET", "/api/audio/3.opus?download=1", nil, map[string]string{"id": "3"}))
	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusFound || !strings.Contains(loc, "works/47/r/1-2-p.opus") ||
		!strings.Contains(loc, "ttl="+(12*time.Hour).String()) {
		t.Fatalf("код %d, Location = %q", rec.Code, loc)
	}
	if got := downloadName(t, loc); got != "Товар. Часть первая.opus" {
		t.Errorf("имя файла %q", got)
	}
}

// Без ?download=1 ссылка прежняя: плеер и плейлист получают звук для игры.
func TestAudioRedirectWithoutDownloadHasNoDisposition(t *testing.T) {
	h, f, _ := newAudioTestHandler()
	f.tracks = []models.AudioTrack{{ID: 3, Title: "Товар", S3Key: "works/47/r/1-2-p.opus"}}
	f.recs = []models.AudioRecording{{ID: 7, ChapterTitle: "Товар", Position: 1, S3Key: "works/47/rec/5/x.mp3"}}
	for _, c := range []struct {
		url string
		run func(http.ResponseWriter, *http.Request)
	}{
		{"/api/audio/3.opus", h.TrackRedirect},
		{"/api/audio/rec/7?download=0", h.RecordingRedirect},
	} {
		id := "3"
		if strings.Contains(c.url, "rec") {
			id = "7"
		}
		rec := httptest.NewRecorder()
		c.run(rec, audioRequest("GET", c.url, nil, map[string]string{"id": id}))
		if loc := rec.Header().Get("Location"); rec.Code != http.StatusFound || strings.Contains(loc, "disposition") {
			t.Errorf("%s: код %d, Location = %q", c.url, rec.Code, loc)
		}
	}
}

func TestRecordingDownloadNamesFileByChapterAndReader(t *testing.T) {
	h, f, _ := newAudioTestHandler()
	f.recs = []models.AudioRecording{
		{ID: 7, ChapterTitle: "Товар.", Position: 2, Reader: "Иванов", ContentType: "audio/mpeg", S3Key: "works/47/rec/5/ab.mp3"},
		{ID: 8, ChapterTitle: "Товар", Position: 3, ContentType: "audio/flac", S3Key: "works/47/rec/5/cd.flac"},
		// Инициалы с точкой в конце давали «И. И..ogg».
		{ID: 9, ChapterTitle: "Товар", Position: 4, Reader: "Иванов И. И.", ContentType: "audio/ogg", S3Key: "works/47/rec/5/ef.ogg"},
	}
	for id, want := range map[string]string{
		"7": "Товар. Запись 2 — Иванов.mp3",
		"8": "Товар. Запись 3.flac",
		"9": "Товар. Запись 4 — Иванов И. И.ogg",
	} {
		rec := httptest.NewRecorder()
		h.RecordingRedirect(rec, audioRequest("GET", "/api/audio/rec/"+id+"?download=1", nil, map[string]string{"id": id}))
		if rec.Code != http.StatusFound {
			t.Fatalf("запись %s: код %d", id, rec.Code)
		}
		if got := downloadName(t, rec.Header().Get("Location")); got != want {
			t.Errorf("запись %s: имя файла %q, ждали %q", id, got, want)
		}
	}
}

func TestRecordingRedirectMissingIsGone(t *testing.T) {
	h, _, _ := newAudioTestHandler()
	rec := httptest.NewRecorder()
	h.RecordingRedirect(rec, audioRequest("GET", "/api/audio/rec/9", nil, map[string]string{"id": "9"}))
	if rec.Code != http.StatusGone {
		t.Errorf("код %d, ждали 410", rec.Code)
	}
}
