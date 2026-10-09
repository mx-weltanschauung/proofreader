package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"proofreader/internal/auth"
	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/internal/repository"
)

var _ JournalStore = (*repository.JournalRepository)(nil)

type fakeJournalStore struct {
	journal      *models.Journal
	createErr    error
	issueErr     error
	createdIssue *models.JournalIssue
	createdWork  *models.Work
	updatedTitle string
	issue        *models.JournalIssue
}

func (f *fakeJournalStore) List(context.Context) ([]models.JournalSummary, error) {
	return []models.JournalSummary{}, nil
}
func (f *fakeJournalStore) GetByID(_ context.Context, id int64) (*models.Journal, error) {
	if f.journal == nil || f.journal.ID != id {
		return nil, repository.ErrJournalNotFound
	}
	return f.journal, nil
}
func (f *fakeJournalStore) Detail(_ context.Context, slug string) (*models.JournalDetail, error) {
	if f.journal == nil || f.journal.Slug != slug {
		return nil, repository.ErrJournalNotFound
	}
	return &models.JournalDetail{Journal: f.journal, Years: []models.JournalYear{}}, nil
}
func (f *fakeJournalStore) Create(_ context.Context, j *models.Journal) error {
	if f.createErr != nil {
		return f.createErr
	}
	if j.ID == 0 {
		j.ID = 1
	}
	f.journal = j
	return nil
}
func (f *fakeJournalStore) Update(_ context.Context, j *models.Journal) error {
	f.journal = j
	return nil
}
func (f *fakeJournalStore) CreateIssue(_ context.Context, i *models.JournalIssue, w *models.Work) error {
	if f.issueErr != nil {
		return f.issueErr
	}
	f.createdIssue, f.createdWork = i, w
	if w.ID == 0 {
		w.ID = 77
	}
	i.WorkID = w.ID
	return nil
}
func (f *fakeJournalStore) GetIssue(_ context.Context, id int64) (*models.JournalIssue, error) {
	if f.issue == nil || f.issue.ID != id {
		return nil, repository.ErrIssueNotFound
	}
	return f.issue, nil
}
func (f *fakeJournalStore) UpdateIssue(_ context.Context, i *models.JournalIssue, title string) error {
	f.issue, f.updatedTitle = i, title
	return nil
}

// journalRequest — запрос редактора: claims кладутся тем же приёмом, что в
// createWorkAsEditor (work_handler_test.go).
func journalRequest(method, path, body string, vars map[string]string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = mux.SetURLVars(req, vars)
	return req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey,
		&auth.Claims{UserID: 1, Role: models.RoleEditor}))
}

func TestJournalCreateIssueBuildsWorkTitleAndLabel(t *testing.T) {
	store := &fakeJournalStore{journal: &models.Journal{ID: 3, Slug: "pzm", Title: "Под знаменем марксизма"}}
	rec := httptest.NewRecorder()
	NewJournalHandler(store).CreateIssue(rec, journalRequest(http.MethodPost, "/api/journals/3/issues",
		`{"year": 1925, "number_from": 5, "number_to": 6, "months": "май—июнь"}`, map[string]string{"id": "3"}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	if store.createdWork.Title != "Под знаменем марксизма, 1925, № 5—6" || store.createdIssue.Label != "5—6" {
		t.Fatalf("работа %q, номер %+v", store.createdWork.Title, store.createdIssue)
	}
	if store.createdWork.OwnerID != 1 || store.createdWork.Status != models.WorkStatusDraft {
		t.Fatalf("владелец/статус: %+v", store.createdWork)
	}
}

func TestJournalCreateIssueSingleNumberDefaultsTo(t *testing.T) {
	store := &fakeJournalStore{journal: &models.Journal{ID: 3, Title: "Большевик"}}
	rec := httptest.NewRecorder()
	NewJournalHandler(store).CreateIssue(rec, journalRequest(http.MethodPost, "/", `{"year": 1928, "number_from": 20}`,
		map[string]string{"id": "3"}))
	if rec.Code != http.StatusCreated || store.createdIssue.NumberTo != 20 || store.createdIssue.Label != "20" {
		t.Fatalf("код %d, номер %+v", rec.Code, store.createdIssue)
	}
}

func TestJournalCreateIssueRejects(t *testing.T) {
	for _, c := range []struct {
		name, body string
		store      *fakeJournalStore
		want       int
	}{
		{"год вне диапазона", `{"year": 1700, "number_from": 1}`, &fakeJournalStore{journal: &models.Journal{ID: 3}}, http.StatusBadRequest},
		{"номер до", `{"year": 1925, "number_from": 6, "number_to": 5}`, &fakeJournalStore{journal: &models.Journal{ID: 3}}, http.StatusBadRequest},
		{"нет журнала", `{"year": 1925, "number_from": 1}`, &fakeJournalStore{}, http.StatusNotFound},
		{"номер занят", `{"year": 1925, "number_from": 1}`, &fakeJournalStore{journal: &models.Journal{ID: 3},
			issueErr: fmt.Errorf("x: %w", repository.ErrIssueTaken)}, http.StatusConflict},
		{"id занят", `{"year": 1925, "number_from": 1, "work_id": 5}`, &fakeJournalStore{journal: &models.Journal{ID: 3},
			issueErr: fmt.Errorf("x: %w", repository.ErrIDTaken)}, http.StatusConflict},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			NewJournalHandler(c.store).CreateIssue(rec, journalRequest(http.MethodPost, "/", c.body, map[string]string{"id": "3"}))
			if rec.Code != c.want {
				t.Fatalf("код %d, ждали %d: %s", rec.Code, c.want, rec.Body.String())
			}
		})
	}
}

func TestJournalCreateRejectsBadSlugAndTakenSlug(t *testing.T) {
	rec := httptest.NewRecorder()
	NewJournalHandler(&fakeJournalStore{}).Create(rec, journalRequest(http.MethodPost, "/", `{"slug": "ПЗМ", "title": "x"}`, nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("кириллица в слаге: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	store := &fakeJournalStore{createErr: fmt.Errorf("x: %w", repository.ErrJournalSlugTaken)}
	NewJournalHandler(store).Create(rec, journalRequest(http.MethodPost, "/", `{"slug": "pzm", "title": "x"}`, nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("занятый слаг: %d", rec.Code)
	}
}

func TestJournalGetUnknownIs404(t *testing.T) {
	rec := httptest.NewRecorder()
	NewJournalHandler(&fakeJournalStore{}).Get(rec, journalRequest(http.MethodGet, "/", "", map[string]string{"slug": "net"}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("код %d", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["message"] == "" {
		t.Fatalf("отказ не {message}: %q", rec.Body.String())
	}
}

func TestJournalUpdateIssueRetitlesWork(t *testing.T) {
	store := &fakeJournalStore{
		journal: &models.Journal{ID: 3, Title: "Большевик"},
		issue:   &models.JournalIssue{ID: 9, JournalID: 3, Year: 1927, NumberFrom: 19, NumberTo: 19, Label: "19"},
	}
	rec := httptest.NewRecorder()
	NewJournalHandler(store).UpdateIssue(rec, journalRequest(http.MethodPut, "/", `{"year": 1927, "number_from": 19, "number_to": 20}`,
		map[string]string{"id": "9"}))
	if rec.Code != http.StatusOK || store.updatedTitle != "Большевик, 1927, № 19—20" {
		t.Fatalf("код %d, заглавие %q", rec.Code, store.updatedTitle)
	}
}
