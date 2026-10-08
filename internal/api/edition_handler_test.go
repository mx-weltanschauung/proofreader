package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

type fakeEditionStore struct {
	editions  []*models.Edition
	works     []*models.Work
	summaries []*models.VolumeSummary
	created   *models.Edition
	createErr error
	deletedID int64
	deleted   bool
}

func (f *fakeEditionStore) List(ctx context.Context) ([]*models.Edition, error) {
	return f.editions, nil
}
func (f *fakeEditionStore) GetByID(ctx context.Context, id int64) (*models.Edition, error) {
	for _, e := range f.editions {
		if e.ID == id {
			return e, nil
		}
	}
	return nil, context.Canceled
}
func (f *fakeEditionStore) Create(ctx context.Context, e *models.Edition) error {
	if f.createErr != nil {
		return f.createErr
	}
	if e.ID == 0 {
		e.ID = 42
	}
	f.created = e
	return nil
}
func (f *fakeEditionStore) Update(ctx context.Context, e *models.Edition) error { return nil }
func (f *fakeEditionStore) Delete(ctx context.Context, id int64) error {
	f.deleted = true
	f.deletedID = id
	return nil
}
func (f *fakeEditionStore) ListWorks(ctx context.Context, editionID int64) ([]*models.Work, error) {
	return f.works, nil
}

func (f *fakeEditionStore) ListWorkSummaries(ctx context.Context, editionID int64) ([]*models.VolumeSummary, error) {
	return f.summaries, nil
}

func TestEditionHandlerCreate(t *testing.T) {
	store := &fakeEditionStore{}
	h := NewEditionHandler(store)

	body := bytes.NewBufferString(`{"title":"Сочинения, 2-е изд.","slug":"mae-2"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/editions", body)
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("код %d, ожидался 201; тело: %s", rec.Code, rec.Body.String())
	}
	if store.created == nil || store.created.Slug != "mae-2" {
		t.Fatalf("в стор не пришло собрание: %+v", store.created)
	}
}

// Плановый объём собрания доезжает от формы до хранилища. Без этого поле в
// базе есть, а вписать его человеку нечем: заводится оно только вручную,
// с титульного листа издания.
func TestEditionHandlerCreateKeepsVolumesPlanned(t *testing.T) {
	store := &fakeEditionStore{}
	h := NewEditionHandler(store)

	body := bytes.NewBufferString(`{"title":"Сочинения","slug":"mae-2","volumes_planned":50}`)
	req := httptest.NewRequest(http.MethodPost, "/api/editions", body)
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("код %d, ожидался 201; тело: %s", rec.Code, rec.Body.String())
	}
	if store.created == nil || store.created.VolumesPlanned == nil {
		t.Fatalf("плановый объём не доехал до хранилища: %+v", store.created)
	}
	if *store.created.VolumesPlanned != 50 {
		t.Fatalf("VolumesPlanned = %d, ожидалось 50", *store.created.VolumesPlanned)
	}
}

// Собрание без известного плана заводится пустым полем, а не нулём: подпись
// полки различает «45 из 55» и просто «45 томов».
func TestEditionHandlerCreateWithoutPlanLeavesItEmpty(t *testing.T) {
	store := &fakeEditionStore{}
	h := NewEditionHandler(store)

	body := bytes.NewBufferString(`{"title":"Сочинения","slug":"mae-2"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/editions", body)
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if store.created == nil {
		t.Fatalf("в стор не пришло собрание")
	}
	if store.created.VolumesPlanned != nil {
		t.Fatalf("VolumesPlanned = %d, ожидалась пустота", *store.created.VolumesPlanned)
	}
}

// Правка собрания сохраняет план так же, как и заведение: у половины корпуса
// он вписывается позже, когда том уже залит.
func TestEditionHandlerUpdateKeepsVolumesPlanned(t *testing.T) {
	store := &fakeEditionStore{editions: []*models.Edition{{ID: 7, Title: "Сочинения", Slug: "s"}}}
	h := NewEditionHandler(store)

	body := bytes.NewBufferString(`{"title":"Сочинения","slug":"s","volumes_planned":24}`)
	req := httptest.NewRequest(http.MethodPut, "/api/editions/7", body)
	req = mux.SetURLVars(req, map[string]string{"id": "7"})
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200; тело: %s", rec.Code, rec.Body.String())
	}
	got := store.editions[0]
	if got.VolumesPlanned == nil || *got.VolumesPlanned != 24 {
		t.Fatalf("VolumesPlanned = %v, ожидалось 24", got.VolumesPlanned)
	}
}

func TestEditionHandlerListWorksReturnsSummaries(t *testing.T) {
	num := 3
	store := &fakeEditionStore{summaries: []*models.VolumeSummary{{
		Work:          models.Work{ID: 3, Title: "Том 3", VolumeNumber: &num},
		PagesTotal:    586,
		PagesByStatus: map[string]int{"вычитана": 70, "не_вычитана": 516},
		ChaptersTotal: 3,
		TopChapters: []models.TopChapter{
			{Title: "Немецкая идеология", Pages: 538, Share: 0.918},
			{Title: "Тезисы о Фейербахе", Pages: 4, Share: 0.007},
		},
	}}}
	h := NewEditionHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/editions/1/works", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "1"})
	rec := httptest.NewRecorder()
	h.ListWorks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200; тело: %s", rec.Code, rec.Body.String())
	}

	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("ответ не разобрался: %v; тело: %s", err, rec.Body.String())
	}
	if len(got) != 1 {
		t.Fatalf("томов в ответе %d, ожидался 1", len(got))
	}
	// Work встроен, поэтому его поля лежат на верхнем уровне — старая форма
	// ответа сохраняется, к ней только добавляются агрегаты.
	if got[0]["title"] != "Том 3" {
		t.Errorf("title = %v", got[0]["title"])
	}
	if got[0]["pages_total"] != float64(586) {
		t.Errorf("pages_total = %v", got[0]["pages_total"])
	}
	byStatus, ok := got[0]["pages_by_status"].(map[string]any)
	if !ok || byStatus["вычитана"] != float64(70) {
		t.Errorf("pages_by_status = %v", got[0]["pages_by_status"])
	}
	tops, ok := got[0]["top_chapters"].([]any)
	if !ok || len(tops) != 2 {
		t.Fatalf("top_chapters = %v", got[0]["top_chapters"])
	}
	first, ok := tops[0].(map[string]any)
	if !ok || first["title"] != "Немецкая идеология" {
		t.Errorf("первая работа = %v", tops[0])
	}
}

// Том без страниц и без глав — обычное состояние сразу после загрузки PDF.
// Ответ обязан быть валидным, а не пустым объектом или null.
func TestEditionHandlerListWorksEmptyVolume(t *testing.T) {
	store := &fakeEditionStore{summaries: []*models.VolumeSummary{{
		Work:          models.Work{ID: 40, Title: "Предметный указатель"},
		PagesTotal:    0,
		PagesByStatus: map[string]int{},
		ChaptersTotal: 0,
		TopChapters:   nil,
	}}}
	h := NewEditionHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/editions/1/works", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "1"})
	rec := httptest.NewRecorder()
	h.ListWorks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", rec.Code)
	}
	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("ответ не разобрался: %v", err)
	}
	if _, present := got[0]["top_chapters"]; present {
		t.Errorf("top_chapters должен отсутствовать у тома без глав, а он есть: %v", got[0])
	}
}

func TestEditionHandler_Create_EmptyTitle(t *testing.T) {
	store := &fakeEditionStore{}
	h := NewEditionHandler(store)

	body := bytes.NewBufferString(`{"title":"","slug":"x"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/editions", body)
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("код %d, ожидался %d", rec.Code, http.StatusBadRequest)
	}
	if store.created != nil {
		t.Errorf("собрание не должно было попасть в стор: %+v", store.created)
	}
}

func TestEditionHandler_Create_InvalidJSON(t *testing.T) {
	h := NewEditionHandler(nil)

	req := httptest.NewRequest(http.MethodPost, "/api/editions", bytes.NewBufferString("invalid json"))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("код %d, ожидался %d", rec.Code, http.StatusBadRequest)
	}
}

func TestEditionHandler_Update_InvalidID(t *testing.T) {
	h := NewEditionHandler(nil)

	req := httptest.NewRequest(http.MethodPut, "/api/editions/invalid", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "invalid"})
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("код %d, ожидался %d", rec.Code, http.StatusBadRequest)
	}
}

func TestEditionHandler_Update_EmptyTitle(t *testing.T) {
	store := &fakeEditionStore{editions: []*models.Edition{
		{ID: 3, Title: "Старое", Slug: "old"},
	}}
	h := NewEditionHandler(store)

	body := bytes.NewBufferString(`{"title":"","slug":"x"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/editions/3", body)
	req = mux.SetURLVars(req, map[string]string{"id": "3"})
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("код %d, ожидался %d", rec.Code, http.StatusBadRequest)
	}
}

func TestEditionHandler_Update_NotFound(t *testing.T) {
	store := &fakeEditionStore{}
	h := NewEditionHandler(store)

	body := bytes.NewBufferString(`{"title":"Новое","slug":"new"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/editions/99", body)
	req = mux.SetURLVars(req, map[string]string{"id": "99"})
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("код %d, ожидался %d", rec.Code, http.StatusNotFound)
	}
}

func TestEditionHandler_Delete(t *testing.T) {
	store := &fakeEditionStore{}
	h := NewEditionHandler(store)

	req := httptest.NewRequest(http.MethodDelete, "/api/editions/7", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "7"})
	rec := httptest.NewRecorder()
	h.Delete(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("код %d, ожидался %d", rec.Code, http.StatusNoContent)
	}
	if !store.deleted || store.deletedID != 7 {
		t.Errorf("стор не вызван с id=7: deleted=%v deletedID=%d", store.deleted, store.deletedID)
	}
}

func TestEditionHandler_List(t *testing.T) {
	store := &fakeEditionStore{editions: []*models.Edition{
		{ID: 1, Title: "Сочинения", Slug: "mae"},
	}}
	h := NewEditionHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/editions", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался %d", rec.Code, http.StatusOK)
	}
	var got []*models.Edition
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("ответ не разобран: %v", err)
	}
	if len(got) != 1 || got[0].Slug != "mae" {
		t.Fatalf("ожидалось одно собрание slug=mae, получено %+v", got)
	}
}

func TestEditionHandler_List_EmptyIsArray(t *testing.T) {
	// Тот же баг, что чинился для ListConcepts: пустая выборка приходит из
	// репозитория nil-слайсом, а encoding/json пишет null — фронт вызвал бы
	// .map на null и упал бы.
	store := &fakeEditionStore{editions: nil}
	h := NewEditionHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/editions", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("тело %q, ожидалось []", got)
	}
}

func TestEditionHandlerGet(t *testing.T) {
	store := &fakeEditionStore{editions: []*models.Edition{
		{ID: 1, Title: "Сочинения, 2-е изд.", Slug: "mae-2"},
	}}
	h := NewEditionHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/editions/1", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "1"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200; тело: %s", rec.Code, rec.Body.String())
	}
	var got models.Edition
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("не разобрать ответ: %v", err)
	}
	if got.Slug != "mae-2" {
		t.Fatalf("slug %q, ожидался mae-2", got.Slug)
	}
}

func TestEditionHandlerGetMissing(t *testing.T) {
	// Фейк отвечает на ненайденное ошибкой, живой репозиторий — тоже
	// (pgx.ErrNoRows). Для клиента это 404, а не 500.
	h := NewEditionHandler(&fakeEditionStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/editions/404", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "404"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("код %d, ожидался 404", rec.Code)
	}
}

// Адресный слаг доезжает от формы до хранилища так же, как slug и
// volumes_planned: без этого поле в базе есть, а заполнить его нечем.
func TestCreateEditionKeepsURLSlug(t *testing.T) {
	store := &fakeEditionStore{}
	h := NewEditionHandler(store)

	body := `{"title":"В. И. Ленин. ПСС","slug":"lenin-pss-5","url_slug":"lenin"}`
	req := httptest.NewRequest(http.MethodPost, "/api/editions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("код %d, тело: %s", rec.Code, rec.Body.String())
	}
	if store.created == nil || store.created.URLSlug != "lenin" {
		t.Fatalf("url_slug не доехал до хранилища: %+v", store.created)
	}
}

func TestEditionHandlerCreatePassesExplicitID(t *testing.T) {
	store := &fakeEditionStore{}
	h := NewEditionHandler(store)
	req := httptest.NewRequest(http.MethodPost, "/api/editions",
		bytes.NewBufferString(`{"id":9,"title":"Горький","slug":"gorky"}`))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusCreated || store.created == nil || store.created.ID != 9 {
		t.Fatalf("код %d, в стор ушло %+v; ждали 201 и id 9", rec.Code, store.created)
	}
}

func TestEditionHandlerCreateTakenIDIsConflict(t *testing.T) {
	store := &fakeEditionStore{createErr: fmt.Errorf("failed to create edition: %w", repository.ErrIDTaken)}
	h := NewEditionHandler(store)
	req := httptest.NewRequest(http.MethodPost, "/api/editions",
		bytes.NewBufferString(`{"id":9,"title":"Горький","slug":"gorky"}`))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("код %d, ждали 409", rec.Code)
	}
}

func TestEditionHandlerCreateNegativeIDIsBadRequest(t *testing.T) {
	store := &fakeEditionStore{}
	h := NewEditionHandler(store)
	req := httptest.NewRequest(http.MethodPost, "/api/editions",
		bytes.NewBufferString(`{"id":-1,"title":"Горький","slug":"gorky"}`))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusBadRequest || store.created != nil {
		t.Fatalf("код %d, в стор ушло %+v; ждали 400", rec.Code, store.created)
	}
}
