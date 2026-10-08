package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/pagecache"
	"proofreader/internal/repository"
	"proofreader/pkg/markdown"
	"proofreader/pkg/storage"
)

// fakeApparatusStore отдаёт ровно ту форму, что отдаёт настоящий репозиторий:
// Plan и Remove возвращают один и тот же план, а не разные. Ошибка «тома нет»
// приходит из Plan — так же, как из apparatusPlan.
type fakeApparatusStore struct {
	plan      models.ApparatusPlan
	planErr   error
	removed   []int64
	removeErr error
}

func (f *fakeApparatusStore) Plan(_ context.Context, _ int64) (models.ApparatusPlan, error) {
	return f.plan, f.planErr
}

func (f *fakeApparatusStore) Remove(_ context.Context, workID int64) (models.ApparatusPlan, error) {
	if f.removeErr != nil {
		return models.ApparatusPlan{}, f.removeErr
	}
	f.removed = append(f.removed, workID)
	return f.plan, nil
}

func apparatusHandler(t *testing.T, store *fakeApparatusStore) (*WorkHandler, storage.Storage, *pagecache.Store) {
	t.Helper()
	files := storage.NewMemoryStorage()
	cache := pagecache.New(t.TempDir())
	return &WorkHandler{
		renderer:   markdown.NewRenderer(),
		store:      files,
		presignTTL: time.Minute,
		cache:      NewServingCache(cache, &fakeCrawlerCaches{}),
		apparatus:  store,
	}, files, cache
}

func fullPlan() models.ApparatusPlan {
	return models.ApparatusPlan{
		WorkID:    45,
		WorkTitle: "Л. С. Выготский. Собрание сочинений. Том 1",
		Chapters: []models.ApparatusChapter{
			{ID: 3225, Title: "Комментарии", StartPage: 459, EndPage: 472},
			{ID: 3226, Title: "Именной указатель", StartPage: 473, EndPage: 476},
		},
		ChapterCount: 5,
		PageCount:    27,
		ConceptCount: 12,
		Children:     []models.ApparatusChild{{ID: 46, Title: "Передние листы", Role: models.WorkRoleFrontMatter, Pages: 4}},
		StoragePaths: []string{"works/45/pages/page_459.png", "works/45/pages/page_460.png"},
	}
}

func TestApparatusPlanIsReadOnly(t *testing.T) {
	store := &fakeApparatusStore{plan: fullPlan()}
	h, _, _ := apparatusHandler(t, store)

	req := httptest.NewRequest(http.MethodGet, "/api/works/45/apparatus", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "45"})
	rec := httptest.NewRecorder()
	h.ApparatusPlan(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("план ответил %d, ожидалось %d", rec.Code, http.StatusOK)
	}
	if len(store.removed) != 0 {
		t.Errorf("чтение плана сняло аппарат тома %v", store.removed)
	}

	var got models.ApparatusPlan
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("разбор плана: %v", err)
	}
	if got.PageCount != 27 || got.ChapterCount != 5 || got.ConceptCount != 12 {
		t.Errorf("план отдал %+v, ожидались числа из фикстуры", got)
	}
	if len(got.Chapters) != 2 || got.Chapters[0].Title != "Комментарии" {
		t.Errorf("главы плана: %+v", got.Chapters)
	}
	if len(got.Children) != 1 {
		t.Errorf("служебные работы плана: %+v", got.Children)
	}
}

// Ключи хранилища наружу не отдаются: читателю плана нужен счёт, а не список
// объектов бакета.
func TestApparatusPlanHidesStorageKeys(t *testing.T) {
	h, _, _ := apparatusHandler(t, &fakeApparatusStore{plan: fullPlan()})
	req := httptest.NewRequest(http.MethodGet, "/api/works/45/apparatus", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "45"})
	rec := httptest.NewRecorder()
	h.ApparatusPlan(rec, req)

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("разбор плана: %v", err)
	}
	for key := range raw {
		if key == "storage_paths" || key == "StoragePaths" {
			t.Errorf("план отдал ключи хранилища наружу: %v", raw[key])
		}
	}
}

// Главное правило этого маршрута: пустой план — отказ, а не тихий успех.
// «Снято ноль» на неразмеченном томе выглядит как снятие и им не является.
func TestDeleteApparatusRefusesEmptyPlan(t *testing.T) {
	store := &fakeApparatusStore{plan: models.ApparatusPlan{WorkID: 145, WorkTitle: "Том 4"}}
	h, _, _ := apparatusHandler(t, store)

	req := httptest.NewRequest(http.MethodDelete, "/api/works/145/apparatus", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "145"})
	rec := httptest.NewRecorder()
	h.DeleteApparatus(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("пустой план ответил %d, ожидалось %d", rec.Code, http.StatusConflict)
	}
	if len(store.removed) != 0 {
		t.Errorf("на пустом плане всё же был снос: %v", store.removed)
	}
}

func TestDeleteApparatusRemovesPreviewsAndChildFiles(t *testing.T) {
	plan := fullPlan()
	store := &fakeApparatusStore{plan: plan}
	h, files, cache := apparatusHandler(t, store)

	ctx := context.Background()
	for _, key := range plan.StoragePaths {
		if err := files.Put(ctx, key, strings.NewReader("png"), -1, "image/png"); err != nil {
			t.Fatalf("Put %s: %v", key, err)
		}
	}
	// Полоса тела тома под тем же префиксом — она обязана уцелеть.
	body := "works/45/pages/page_100.png"
	if err := files.Put(ctx, body, strings.NewReader("png"), -1, "image/png"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	childFile := "works/46/pages/page_1.png"
	if err := files.Put(ctx, childFile, strings.NewReader("png"), -1, "image/png"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	volumeCache := pagecache.Key{WorkID: 45, Start: 1, End: 10}
	childCache := pagecache.Key{WorkID: 46, Start: 1, End: 4}
	for _, k := range []pagecache.Key{volumeCache, childCache} {
		if err := cache.Put(k, []byte(`{"pages":[]}`)); err != nil {
			t.Fatalf("Put в кэш: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/works/45/apparatus", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "45"})
	rec := httptest.NewRecorder()
	h.DeleteApparatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("снятие ответило %d, ожидалось %d", rec.Code, http.StatusOK)
	}
	if len(store.removed) != 1 || store.removed[0] != 45 {
		t.Errorf("снято %v, ожидался один том 45", store.removed)
	}
	for _, key := range plan.StoragePaths {
		if _, err := files.Get(ctx, key); err == nil {
			t.Errorf("превью аппарата %s пережило снятие", key)
		}
	}
	if _, err := files.Get(ctx, body); err != nil {
		t.Errorf("снятие аппарата унесло превью полосы тела тома: %v", err)
	}
	if _, err := files.Get(ctx, childFile); err == nil {
		t.Error("файлы служебной работы пережили снятие")
	}
	if _, _, ok := cache.Open(volumeCache); ok {
		t.Error("кэш тома пережил снятие аппарата")
	}
	if _, _, ok := cache.Open(childCache); ok {
		t.Error("кэш служебной работы пережил снятие аппарата")
	}
}

// Ошибка «тома нет» приходит сигналом repository.ErrWorkNotFound, обёрнутым в
// текст, — ровно так, как её отдаёт apparatusPlan. Фейк, возвращающий вместо
// сигнала просто строку «work 999 not found», проверял бы форму, которой
// репозиторий не отдаёт.
func TestDeleteApparatusOfMissingWorkIsNotFound(t *testing.T) {
	store := &fakeApparatusStore{
		planErr: fmt.Errorf("work 999: %w", repository.ErrWorkNotFound),
	}
	h, _, _ := apparatusHandler(t, store)

	req := httptest.NewRequest(http.MethodDelete, "/api/works/999/apparatus", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "999"})
	rec := httptest.NewRecorder()
	h.DeleteApparatus(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("несуществующий том ответил %d, ожидалось %d", rec.Code, http.StatusNotFound)
	}
}

// Обратная половина, и она важнее: мигнувшая база НЕ должна отвечать «тома
// нет». Это единственный диагноз, после которого оператор перестаёт искать, и
// получить его во время срочного снятия из-за исчерпанного пула значит
// остановить снятие не там.
func TestApparatusPlanFailureIsNotReportedAsMissingWork(t *testing.T) {
	store := &fakeApparatusStore{
		planErr: errors.New("failed to count apparatus chapters: conn busy"),
	}
	h, _, _ := apparatusHandler(t, store)

	for _, call := range []struct {
		name string
		fn   func(http.ResponseWriter, *http.Request)
	}{
		{"план", h.ApparatusPlan},
		{"снятие", h.DeleteApparatus},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/works/45/apparatus", nil)
		req = mux.SetURLVars(req, map[string]string{"id": "45"})
		rec := httptest.NewRecorder()
		call.fn(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s при сбое базы ответил %d, ожидалось %d",
				call.name, rec.Code, http.StatusInternalServerError)
		}
	}
}

// Отказ хранилища приходит ПОСЛЕ коммита базы, и выйти на нём нельзя мимо
// сброса кэшей: иначе снятый аппарат отдавался бы готовыми главами до часа, а
// карточкой og:image до суток — ровно та утечка, ради которой ServingCache и
// заведён. Повтора не будет: план теперь пуст, и повторный вызов ответит 409.
func TestDeleteApparatusDropsCachesEvenWhenStorageFails(t *testing.T) {
	plan := fullPlan()
	store := &fakeApparatusStore{plan: plan}
	cache := pagecache.New(t.TempDir())
	crawler := &fakeCrawlerCaches{}
	h := &WorkHandler{
		renderer:   markdown.NewRenderer(),
		store:      failingStorage{storage.NewMemoryStorage()},
		presignTTL: time.Minute,
		cache:      NewServingCache(cache, crawler),
		apparatus:  store,
	}

	key := pagecache.Key{WorkID: 45, Start: 1, End: 10}
	if err := cache.Put(key, []byte(`{"pages":[]}`)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/works/45/apparatus", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "45"})
	rec := httptest.NewRecorder()
	h.DeleteApparatus(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("отказ хранилища ответил %d, ожидалось %d", rec.Code, http.StatusInternalServerError)
	}
	if _, _, ok := cache.Open(key); ok {
		t.Error("файловый кэш пережил снятие с отказавшим хранилищем")
	}
	if crawler.calls == 0 {
		t.Error("краулерский кэш не сброшен при отказе хранилища")
	}
}

// failingStorage роняет только удаление: всё остальное ведёт себя как обычно.
type failingStorage struct{ storage.Storage }

func (failingStorage) Delete(context.Context, string) error {
	return errors.New("seaweedfs: connection reset")
}

func (failingStorage) DeletePrefix(context.Context, string) error {
	return errors.New("seaweedfs: connection reset")
}

func TestApparatusRoutesRejectBadID(t *testing.T) {
	h, _, _ := apparatusHandler(t, &fakeApparatusStore{plan: fullPlan()})
	for _, call := range []struct {
		name string
		fn   func(http.ResponseWriter, *http.Request)
	}{
		{"план", h.ApparatusPlan},
		{"снятие", h.DeleteApparatus},
	} {
		req := httptest.NewRequest(http.MethodDelete, "/api/works/шестнадцать/apparatus", nil)
		req = mux.SetURLVars(req, map[string]string{"id": "шестнадцать"})
		rec := httptest.NewRecorder()
		call.fn(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s на нечисловом id ответил %d, ожидалось %d",
				call.name, rec.Code, http.StatusBadRequest)
		}
	}
}

// Отказ репозитория на снятии — 500, и хранилище при этом не трогается:
// объекты сносятся ПОСЛЕ успешного коммита, иначе упавшая транзакция оставила
// бы том со строками в базе и без превью.
func TestDeleteApparatusKeepsFilesWhenRemovalFails(t *testing.T) {
	plan := fullPlan()
	store := &fakeApparatusStore{plan: plan, removeErr: errors.New("deadlock detected")}
	h, files, _ := apparatusHandler(t, store)

	ctx := context.Background()
	for _, key := range plan.StoragePaths {
		if err := files.Put(ctx, key, strings.NewReader("png"), -1, "image/png"); err != nil {
			t.Fatalf("Put %s: %v", key, err)
		}
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/works/45/apparatus", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "45"})
	rec := httptest.NewRecorder()
	h.DeleteApparatus(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("упавшее снятие ответило %d, ожидалось %d", rec.Code, http.StatusInternalServerError)
	}
	for _, key := range plan.StoragePaths {
		if _, err := files.Get(ctx, key); err != nil {
			t.Errorf("превью %s снесено, хотя снятие не удалось: %v", key, err)
		}
	}
}
