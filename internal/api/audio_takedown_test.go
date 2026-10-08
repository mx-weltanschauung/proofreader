package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/pagecache"
	"proofreader/pkg/markdown"
	"proofreader/pkg/storage"
)

// TestWorkDeleteWipesAudioPrefix: снос тома уносит всё под works/{id}/ в
// аудиобакете; соседний том с общим началом номера не задет.
func TestWorkDeleteWipesAudioPrefix(t *testing.T) {
	ctx := context.Background()
	audioMem := storage.NewMemoryStorage()
	_ = audioMem.Put(ctx, "works/47/r/1-2-p.opus", strings.NewReader("a"), -1, "")
	_ = audioMem.Put(ctx, "works/470/r/1-2-p.opus", strings.NewReader("b"), -1, "")
	repo := &fakeWorkRepo{}
	h := NewWorkHandler(repo, nil, markdown.NewRenderer(), storage.NewMemoryStorage(),
		time.Minute, nil, nil).WithAudioStore(audioMem)
	rec := httptest.NewRecorder()
	h.Delete(rec, mux.SetURLVars(httptest.NewRequest("DELETE", "/api/works/47", nil), map[string]string{"id": "47"}))
	if rec.Code != http.StatusNoContent || len(repo.deleted) != 1 {
		t.Fatalf("%d, удалено %v", rec.Code, repo.deleted)
	}
	if _, _, err := audioMem.Head(ctx, "works/47/r/1-2-p.opus"); err == nil {
		t.Error("звук снятого тома остался")
	}
	if _, _, err := audioMem.Head(ctx, "works/470/r/1-2-p.opus"); err != nil {
		t.Error("задет звук тома 470")
	}
}

// TestApparatusDeleteRemovesHumanRecordingObjects: снятие аппарата удаляет
// объекты записей его глав поштучно, синтез тома не трогает.
func TestApparatusDeleteRemovesHumanRecordingObjects(t *testing.T) {
	ctx := context.Background()
	plan := fullPlan()
	key := fmt.Sprintf("works/%d/rec/9/x.mp3", plan.WorkID)
	synthKey := fmt.Sprintf("works/%d/r/1-2-p.opus", plan.WorkID)
	plan.AudioPaths = []string{key}
	store := &fakeApparatusStore{plan: plan}
	h := &WorkHandler{
		renderer: markdown.NewRenderer(), store: storage.NewMemoryStorage(), presignTTL: time.Minute,
		cache: NewServingCache(pagecache.New(t.TempDir()), &fakeCrawlerCaches{}), apparatus: store,
	}
	audioMem := storage.NewMemoryStorage()
	_ = audioMem.Put(ctx, key, strings.NewReader("x"), -1, "")
	_ = audioMem.Put(ctx, synthKey, strings.NewReader("y"), -1, "")
	h.WithAudioStore(audioMem)

	rec := httptest.NewRecorder()
	h.DeleteApparatus(rec, mux.SetURLVars(httptest.NewRequest("DELETE", "/", nil),
		map[string]string{"id": fmt.Sprint(plan.WorkID)}))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if _, _, err := audioMem.Head(ctx, key); err == nil {
		t.Error("запись аппарата осталась")
	}
	if _, _, err := audioMem.Head(ctx, synthKey); err != nil {
		t.Error("снятие аппарата унесло синтез")
	}
}

// TestApparatusDeleteRemovesOverlappingTrackObjects: дорожки синтеза,
// задевшие полосы аппарата, уходят поштучно; прочий синтез тома — нет.
func TestApparatusDeleteRemovesOverlappingTrackObjects(t *testing.T) {
	ctx := context.Background()
	plan := fullPlan()
	gone := fmt.Sprintf("works/%d/r/7-9-p.opus", plan.WorkID)
	kept := fmt.Sprintf("works/%d/r/1-2-p.opus", plan.WorkID)
	plan.TrackPaths = []string{gone}
	store := &fakeApparatusStore{plan: plan}
	h := &WorkHandler{
		renderer: markdown.NewRenderer(), store: storage.NewMemoryStorage(), presignTTL: time.Minute,
		cache: NewServingCache(pagecache.New(t.TempDir()), &fakeCrawlerCaches{}), apparatus: store,
	}
	audioMem := storage.NewMemoryStorage()
	_ = audioMem.Put(ctx, gone, strings.NewReader("x"), -1, "")
	_ = audioMem.Put(ctx, kept, strings.NewReader("y"), -1, "")
	h.WithAudioStore(audioMem)

	rec := httptest.NewRecorder()
	h.DeleteApparatus(rec, mux.SetURLVars(httptest.NewRequest("DELETE", "/", nil),
		map[string]string{"id": fmt.Sprint(plan.WorkID)}))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if _, _, err := audioMem.Head(ctx, gone); err == nil {
		t.Error("дорожка на полосах аппарата осталась")
	}
	if _, _, err := audioMem.Head(ctx, kept); err != nil {
		t.Error("снятие аппарата унесло дорожку тела")
	}
}

type fakeRecordingCounter int

func (n fakeRecordingCounter) CountInChapterSubtree(context.Context, int64) (int, error) {
	return int(n), nil
}

// TestChapterDeleteRefusedWhileHumanRecordingAttached (Review Focus 5).
func TestChapterDeleteRefusedWhileHumanRecordingAttached(t *testing.T) {
	deleted := 0
	store := &fakeChapterStore{deleteFn: func(context.Context, int64) error { deleted++; return nil }}
	req := func() *http.Request {
		return mux.SetURLVars(httptest.NewRequest("DELETE", "/", nil), map[string]string{"workId": "47", "id": "5"})
	}
	h := NewChapterHandler(store, nil, markdown.NewRenderer(), NewRangeCache(pagecache.New(""))).
		WithRecordings(fakeRecordingCounter(2))
	rec := httptest.NewRecorder()
	h.Delete(rec, req())
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "запис") || deleted != 0 {
		t.Errorf("%d %q удалений %d", rec.Code, rec.Body, deleted)
	}
	h.WithRecordings(fakeRecordingCounter(0))
	rec = httptest.NewRecorder()
	h.Delete(rec, req())
	if rec.Code != http.StatusNoContent || deleted != 1 {
		t.Errorf("без записей: %d, удалений %d", rec.Code, deleted)
	}
}
