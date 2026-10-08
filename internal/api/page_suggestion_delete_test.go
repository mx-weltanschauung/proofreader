package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"proofreader/internal/repository"
)

func (f *fakeSuggestionStore) Delete(ctx context.Context, id int64) error {
	f.deletedIDs = append(f.deletedIDs, id)
	return f.deleteErr
}

func (f *fakeSuggestionStore) DeleteRejected(ctx context.Context) (int64, error) {
	f.purgeCalled = true
	return f.purgeCount, f.purgeErr
}

// deleteRig регистрирует оба маршрута удаления в том же порядке, что и
// router.go: литерал выше шаблона.
func deleteRig(t *testing.T, store *fakeSuggestionStore) http.Handler {
	t.Helper()
	h := NewPageSuggestionHandler(store, pageStoreWith(nil, nil), &fakePageVersions{},
		&fakeFragmentStore{}, &fakeDocumentCutStore{}, "соль", false)
	r := mux.NewRouter()
	r.HandleFunc("/api/suggestions/rejected", h.DeleteRejected).Methods("DELETE")
	r.HandleFunc("/api/suggestions/{id}", h.Delete).Methods("DELETE")
	return r
}

func TestDeleteRejectedSuggestionRemovesIt(t *testing.T) {
	store := &fakeSuggestionStore{}
	r := deleteRig(t, store)

	req := httptest.NewRequest(http.MethodDelete, "/api/suggestions/3", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
	}
	if len(store.deletedIDs) != 1 || store.deletedIDs[0] != 3 {
		t.Fatalf("снесено не то: %v", store.deletedIDs)
	}
}

// Удалять можно только отклонённое: новое ещё ждёт разбора, принятое — след
// того, откуда на полосе взялась правка. Отказ приходит из хранилища одним
// запросом (условие по статусу в WHERE), обработчик переводит его в 409.
func TestDeleteUndecidedSuggestionConflicts(t *testing.T) {
	store := &fakeSuggestionStore{deleteErr: repository.ErrSuggestionNotDeletable}
	r := deleteRig(t, store)

	req := httptest.NewRequest(http.MethodDelete, "/api/suggestions/3", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("ожидался 409, получено %d (%s)", rec.Code, rec.Body.String())
	}
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("тело ошибки не JSON: %s", rec.Body.String())
	}
	if payload.Message == "" {
		t.Fatal("в теле ошибки нет message — редактор увидит запасную фразу")
	}
}

func TestPurgeRejectedReportsCount(t *testing.T) {
	store := &fakeSuggestionStore{purgeCount: 4}
	r := deleteRig(t, store)

	req := httptest.NewRequest(http.MethodDelete, "/api/suggestions/rejected", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
	}
	if !store.purgeCalled {
		t.Fatal("массовая чистка не дошла до хранилища")
	}
	if len(store.deletedIDs) != 0 {
		t.Fatalf("чистка ушла в удаление одного: %v", store.deletedIDs)
	}
	var payload struct {
		Deleted int64 `json:"deleted"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if payload.Deleted != 4 {
		t.Fatalf("снесено 4, отвечено %d", payload.Deleted)
	}
}
