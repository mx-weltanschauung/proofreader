package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/pagecache"
	"proofreader/pkg/markdown"
	"proofreader/pkg/storage"
)

// deletableWorkRepo — минимальный WorkRepo для сноса: список детей и само
// удаление. Остальные методы не зовутся и оставлены паникующими нарочно — если
// снос начнёт ходить куда-то ещё, тест скажет об этом, а не промолчит.
type deletableWorkRepo struct {
	WorkRepo
	children []*models.Work
	deleted  []int64
}

func (r *deletableWorkRepo) ListChildren(_ context.Context, _ int64) ([]*models.Work, error) {
	return r.children, nil
}

func (r *deletableWorkRepo) Delete(_ context.Context, id int64) error {
	r.deleted = append(r.deleted, id)
	return nil
}

// fakeCrawlerCaches считает вызовы сброса краулерской половины.
type fakeCrawlerCaches struct{ calls int }

func (f *fakeCrawlerCaches) Purge() (int, int) {
	f.calls++
	return 1, 1
}

// Снос тома обязан уносить и его кэш отдачи. До этой проверки он этого не
// делал: DELETE /works/{id} правил базу и S3, а готовые главы оставались
// лежать файлами на диске до истечения часа, карточка og:image — до суток.
// Догнать их после сноса нечем — PurgeChapter резолвит границы главы из базы.
func TestDeleteWorkDropsItsServingCache(t *testing.T) {
	dir := t.TempDir()
	cache := pagecache.New(dir)
	doomed := pagecache.Key{WorkID: 7, Start: 1, End: 10}
	neighbour := pagecache.Key{WorkID: 8, Start: 1, End: 10}
	for _, k := range []pagecache.Key{doomed, neighbour} {
		if err := cache.Put(k, []byte(`{"pages":[]}`)); err != nil {
			t.Fatalf("Put %+v: %v", k, err)
		}
	}

	crawler := &fakeCrawlerCaches{}
	repo := &deletableWorkRepo{}
	h := &WorkHandler{
		workRepo:   repo,
		renderer:   markdown.NewRenderer(),
		store:      storage.NewMemoryStorage(),
		presignTTL: time.Minute,
		cache:      NewServingCache(cache, crawler),
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/works/7", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "7"})
	rec := httptest.NewRecorder()
	h.Delete(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("Delete ответил %d, ожидалось %d", rec.Code, http.StatusNoContent)
	}
	if _, _, ok := cache.Open(doomed); ok {
		t.Error("файловый кэш снятого тома пережил снос")
	}
	if _, _, ok := cache.Open(neighbour); !ok {
		t.Error("снос тома 7 унёс кэш тома 8")
	}
	if crawler.calls != 1 {
		t.Errorf("краулерский кэш сброшен %d раз, ожидался 1", crawler.calls)
	}
}

// Снос без кэшей (так собран сервер с пустым PAGE_CACHE_DIR и так идут
// остальные тесты обработчика) не должен падать.
func TestDeleteWorkWithoutCacheStillDeletes(t *testing.T) {
	repo := &deletableWorkRepo{}
	h := &WorkHandler{
		workRepo:   repo,
		renderer:   markdown.NewRenderer(),
		store:      storage.NewMemoryStorage(),
		presignTTL: time.Minute,
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/works/7", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "7"})
	rec := httptest.NewRecorder()
	h.Delete(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("Delete ответил %d, ожидалось %d", rec.Code, http.StatusNoContent)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != 7 {
		t.Errorf("удалено %v, ожидался один том 7", repo.deleted)
	}
}

// Служебный ребёнок уезжает каскадом в базе, но его кэш — отдельный каталог,
// и без явного сброса он пережил бы снос родителя.
func TestDeleteWorkDropsChildServingCache(t *testing.T) {
	cache := pagecache.New(t.TempDir())
	child := pagecache.Key{WorkID: 71, Start: 1, End: 4}
	if err := cache.Put(child, []byte(`{"pages":[]}`)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	repo := &deletableWorkRepo{children: []*models.Work{{ID: 71, Role: models.WorkRoleFrontMatter}}}
	h := &WorkHandler{
		workRepo:   repo,
		renderer:   markdown.NewRenderer(),
		store:      storage.NewMemoryStorage(),
		presignTTL: time.Minute,
		cache:      NewServingCache(cache, &fakeCrawlerCaches{}),
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/works/7", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "7"})
	rec := httptest.NewRecorder()
	h.Delete(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("Delete ответил %d, ожидалось %d", rec.Code, http.StatusNoContent)
	}
	if _, _, ok := cache.Open(child); ok {
		t.Error("кэш служебного ребёнка пережил снос родителя")
	}
}
