package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

var (
	_ PersonStore = (*repository.PersonRepository)(nil)
	_ CreditStore = (*repository.PersonRepository)(nil)
)

type fakePersons struct {
	replaced  []models.CreditInput
	chapterID int64
	credits   map[int64][]models.ArticleCredit
	merged    [2]int64
	replErr   error
	createErr error
}

func (f *fakePersons) Create(_ context.Context, p *models.Person) error {
	if f.createErr != nil {
		return f.createErr
	}
	p.ID, p.Slug = 1, "x"
	return nil
}
func (f *fakePersons) Update(context.Context, *models.Person) error { return nil }
func (f *fakePersons) GetByID(_ context.Context, id int64) (*models.Person, error) {
	return &models.Person{ID: id}, nil
}
func (f *fakePersons) Detail(_ context.Context, slug string) (*models.PersonDetail, error) {
	return nil, repository.ErrPersonNotFound
}
func (f *fakePersons) Search(context.Context, string, int) ([]models.Person, error) {
	return []models.Person{}, nil
}
func (f *fakePersons) Merge(_ context.Context, into, from int64) error {
	if into == from {
		return repository.ErrMergeSelf
	}
	f.merged = [2]int64{into, from}
	return nil
}
func (f *fakePersons) ReplaceCredits(_ context.Context, ch int64, in []models.CreditInput) error {
	if f.replErr != nil {
		return f.replErr
	}
	f.chapterID, f.replaced = ch, in
	return nil
}
func (f *fakePersons) ListCreditsByWork(context.Context, int64) (map[int64][]models.ArticleCredit, error) {
	return f.credits, nil
}

type fakeChapterOne struct {
	ChapterStore
	ch *models.Chapter
}

// GetByID отвечает на отсутствующую главу так же, как ChapterRepository.GetByID
// (chapter_repository.go: fmt.Errorf("chapter not found")).
func (f fakeChapterOne) GetByID(_ context.Context, id int64) (*models.Chapter, error) {
	if f.ch == nil || f.ch.ID != id {
		return nil, fmt.Errorf("chapter not found")
	}
	return f.ch, nil
}

func creditsReq(body string, workID, chapterID string) *http.Request {
	return journalRequest(http.MethodPut, "/", body, map[string]string{"workId": workID, "id": chapterID})
}

func TestReplaceCreditsTrimsAndStores(t *testing.T) {
	persons := &fakePersons{}
	h := NewPersonHandler(persons, persons, fakeChapterOne{ch: &models.Chapter{ID: 5, WorkID: 2}})
	rec := httptest.NewRecorder()
	h.ReplaceCredits(rec, creditsReq(`[{"role": "author", "printed": "  Гр. Баммель "}]`, "2", "5"))
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	if persons.chapterID != 5 || len(persons.replaced) != 1 || persons.replaced[0].Printed != "Гр. Баммель" {
		t.Fatalf("в хранилище: %d %+v", persons.chapterID, persons.replaced)
	}
}

func TestReplaceCreditsRejects(t *testing.T) {
	for _, c := range []struct {
		name, body, work string
		want             int
	}{
		{"null", `null`, "2", http.StatusBadRequest},
		{"роль", `[{"role": "editor", "printed": "X"}]`, "2", http.StatusBadRequest},
		{"пустая подпись", `[{"role": "author", "printed": "  "}]`, "2", http.StatusBadRequest},
		{"чужая работа", `[]`, "3", http.StatusNotFound},
	} {
		t.Run(c.name, func(t *testing.T) {
			persons := &fakePersons{}
			h := NewPersonHandler(persons, persons, fakeChapterOne{ch: &models.Chapter{ID: 5, WorkID: 2}})
			rec := httptest.NewRecorder()
			h.ReplaceCredits(rec, creditsReq(c.body, c.work, "5"))
			if rec.Code != c.want {
				t.Fatalf("код %d, ждали %d", rec.Code, c.want)
			}
		})
	}
	persons := &fakePersons{replErr: repository.ErrPersonNotFound}
	h := NewPersonHandler(persons, persons, fakeChapterOne{ch: &models.Chapter{ID: 5, WorkID: 2}})
	rec := httptest.NewRecorder()
	h.ReplaceCredits(rec, creditsReq(`[{"role": "author", "printed": "X", "person_id": 99}]`, "2", "5"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("несуществующий человек: %d", rec.Code)
	}
}

func TestPersonMergeSelfIs400(t *testing.T) {
	persons := &fakePersons{}
	rec := httptest.NewRecorder()
	NewPersonHandler(persons, persons, nil).Merge(rec, journalRequest(http.MethodPost, "/", `{"from": 4}`, map[string]string{"id": "4"}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("код %d", rec.Code)
	}
}

func TestPersonGetUnknownIs404(t *testing.T) {
	persons := &fakePersons{}
	rec := httptest.NewRecorder()
	NewPersonHandler(persons, persons, nil).Get(rec, journalRequest(http.MethodGet, "/", "", map[string]string{"slug": "net"}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("код %d", rec.Code)
	}
}

// Явный id человека занят — 409 с сообщением, как у журнала и номера.
func TestPersonCreateTakenIDIs409(t *testing.T) {
	persons := &fakePersons{createErr: fmt.Errorf("persons: %w", repository.ErrIDTaken)}
	rec := httptest.NewRecorder()
	NewPersonHandler(persons, persons, nil).Create(rec,
		journalRequest(http.MethodPost, "/", `{"id": 7171, "name": "Я. Захер"}`, nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("ответ не writeError: %q", ct)
	}
}
