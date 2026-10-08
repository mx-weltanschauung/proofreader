package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/pagecache"
)

type fakeChapterGetter struct {
	chapter *models.Chapter
	err     error
}

func (f *fakeChapterGetter) GetByID(ctx context.Context, id int64) (*models.Chapter, error) {
	return f.chapter, f.err
}

func purgeChapterRequest() *http.Request {
	req := httptest.NewRequest(http.MethodDelete, "/api/works/47/chapters/5/cache", nil)
	return mux.SetURLVars(req, map[string]string{"workId": "47", "id": "5"})
}

func TestPurgeChapterRemovesItsRangeOnly(t *testing.T) {
	store := pagecache.New(t.TempDir())
	mine := pagecache.Key{WorkID: 47, Start: 1, End: 100}
	neighbour := pagecache.Key{WorkID: 47, Start: 101, End: 200}
	body := []byte(`{"pages":[]}`)
	for _, k := range []pagecache.Key{mine, neighbour} {
		if err := store.Put(k, body); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	h := NewCacheHandler(store, &fakeChapterGetter{
		chapter: &models.Chapter{ID: 5, WorkID: 47, StartPage: 1, EndPage: 100},
	})

	rec := httptest.NewRecorder()
	h.PurgeChapter(rec, purgeChapterRequest())

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", rec.Code)
	}
	var got struct {
		Removed int `json:"removed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("тело не разбирается: %v", err)
	}
	if got.Removed != 1 {
		t.Errorf("removed = %d, ожидалась единица", got.Removed)
	}
	if _, _, ok := store.Open(mine); ok {
		t.Error("файл главы пережил сброс")
	}
	if _, _, ok := store.Open(neighbour); !ok {
		t.Error("сброс задел соседний диапазон")
	}
}

// Глава чужого тома — 400, а не тихий сброс чужого диапазона.
func TestPurgeChapterRejectsForeignWork(t *testing.T) {
	h := NewCacheHandler(pagecache.New(t.TempDir()), &fakeChapterGetter{
		chapter: &models.Chapter{ID: 5, WorkID: 99, StartPage: 1, EndPage: 100},
	})

	rec := httptest.NewRecorder()
	h.PurgeChapter(rec, purgeChapterRequest())

	if rec.Code != http.StatusBadRequest {
		t.Errorf("код %d, ожидался 400", rec.Code)
	}
}

func TestPurgeAllReportsWhatItRemoved(t *testing.T) {
	store := pagecache.New(t.TempDir())
	body := []byte(`{"pages":[]}`)
	for _, k := range []pagecache.Key{{WorkID: 1, Start: 1, End: 2}, {WorkID: 2, Start: 1, End: 2}} {
		if err := store.Put(k, body); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	h := NewCacheHandler(store, &fakeChapterGetter{})

	rec := httptest.NewRecorder()
	h.PurgeAll(rec, httptest.NewRequest(http.MethodDelete, "/api/cache", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", rec.Code)
	}
	var got struct {
		Removed int   `json:"removed"`
		Bytes   int64 `json:"bytes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("тело не разбирается: %v", err)
	}
	if got.Removed != 2 {
		t.Errorf("removed = %d, ожидалось 2", got.Removed)
	}
	if got.Bytes <= 0 {
		t.Errorf("bytes = %d, ожидался положительный объём", got.Bytes)
	}
}

// Выключенный кэш обязан отвечать сводкой «выключен», а не пятисоткой:
// иначе администратор не отличит поломку от незаданного PAGE_CACHE_DIR.
func TestStatsOnDisabledCacheAnswersDisabled(t *testing.T) {
	h := NewCacheHandler(pagecache.New(""), &fakeChapterGetter{})

	rec := httptest.NewRecorder()
	h.Stats(rec, httptest.NewRequest(http.MethodGet, "/api/cache/stats", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", rec.Code)
	}
	var got pagecache.Stats
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("тело не разбирается: %v", err)
	}
	if got.Enabled {
		t.Error("enabled = true у выключенного кэша")
	}
}
