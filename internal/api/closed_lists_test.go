package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"proofreader/internal/models"
	"proofreader/pkg/markdown"
	"proofreader/pkg/storage"
)

// listsFixture — настоящий роутер над подставными хранилищами списков.
// Закрыты: работа 7 (сама), издание 20, журнал pzm; свободная работа 31.
type listsFixture struct {
	handler  http.Handler
	journals *fakeJournalStore
	persons  *fakePersons
}

func newListsFixture(t *testing.T) *listsFixture {
	t.Helper()
	works := &fakeWorkRepo{list: []*models.Work{
		{ID: 7, Title: "Закрытая", ClosedBy: models.ClosedByWork},
		{ID: 8, Title: "Открытая"},
		{ID: 9, Title: "Закрыта изданием", ClosedBy: models.ClosedByEdition},
	}}
	wh := NewWorkHandler(works, nil, markdown.NewRenderer(), storage.NewMemoryStorage(), time.Minute, nil, nil)

	ed20, ed21 := int64(20), int64(21)
	editions := &fakeEditionStore{
		editions: []*models.Edition{
			{ID: 20, Title: "Закрытое", Closed: true},
			{ID: 21, Title: "Открытое"},
		},
		summaries: []*models.VolumeSummary{
			summaryOf(7, &ed21, models.ClosedByWork),
			summaryOf(8, &ed21, ""),
			summaryOf(9, &ed20, models.ClosedByEdition),
		},
	}
	eh := NewEditionHandler(editions)

	closed := &fakeClosedStore{works: map[int64]string{7: models.ClosedByWork, 9: models.ClosedByEdition, 31: models.ClosedByWork}}
	shelf := NewShelfHandler(&shelfEditionsOver{editions}, &fakeShelfWorks{works: []models.ShelfWork{
		{ID: 31, Title: "Закрытая свободная"}, {ID: 32, Title: "Открытая свободная"},
	}}).WithClosed(closed)

	hl := NewHighlightHandler(&fakeHighlightStore{
		edition: &models.Edition{ID: 21, Title: "Открытое"},
		list: []models.EditionHighlight{
			{ChapterID: 1, WorkID: 7, Label: "из закрытого"},
			{ChapterID: 2, WorkID: 8, Label: "из открытого"},
		},
	}).WithClosed(closed)

	journals := &fakeJournalStore{
		journal: &models.Journal{ID: 3, Slug: "pzm", Title: "ПЗМ", Closed: true},
		list: []models.JournalSummary{
			{Journal: models.Journal{ID: 3, Slug: "pzm", Title: "ПЗМ", Closed: true}, IssuesTotal: 5},
			{Journal: models.Journal{ID: 4, Slug: "bol", Title: "Большевик"}, IssuesTotal: 2},
		},
	}
	persons := &fakePersons{}
	// Подставной журнал полки — тот же список, что у журналов.
	shelf = shelf.WithJournals(fakeShelfJournals{list: journals.list})

	rt := createTestRouter()
	r := NewRouter(nil, wh, nil, nil, nil, nil, nil, nil, nil, nil, eh, shelf, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		rt.cacheHandler, nil, rt.authService).
		WithClosed(closed).WithHighlights(hl).
		WithJournals(NewJournalHandler(journals), NewPersonHandler(persons, persons, nil))
	return &listsFixture{handler: r.Setup(), journals: journals, persons: persons}
}

func summaryOf(id int64, edition *int64, closedBy string) *models.VolumeSummary {
	s := &models.VolumeSummary{}
	s.ID = id
	s.EditionID = edition
	s.ClosedBy = closedBy
	return s
}

// shelfEditionsOver — ShelfEditions поверх fakeEditionStore.
type shelfEditionsOver struct{ f *fakeEditionStore }

func (s *shelfEditionsOver) List(ctx context.Context) ([]*models.Edition, error) {
	return s.f.List(ctx)
}
func (s *shelfEditionsOver) ListAllWorkSummaries(context.Context) ([]*models.VolumeSummary, error) {
	return s.f.summaries, nil
}

func (f *listsFixture) body(t *testing.T, path, token string, wantCode int) []byte {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != wantCode {
		t.Fatalf("%s (токен %q): код %d, ждали %d: %s", path, token, rec.Code, wantCode, rec.Body.String())
	}
	return rec.Body.Bytes()
}

func idsOf(t *testing.T, raw []byte, key string) []int64 {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("разбор %s: %v", raw, err)
	}
	var out []int64
	for _, row := range rows {
		out = append(out, int64(row[key].(float64)))
	}
	return out
}

func sameIDs(got []int64, want ...int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestWorksListHidesClosed(t *testing.T) {
	f := newListsFixture(t)
	for _, token := range []string{"", readerToken(t)} {
		if got := idsOf(t, f.body(t, "/api/works", token, 200), "id"); !sameIDs(got, 8) {
			t.Errorf("токен %q: работы %v, ждали [8]", token, got)
		}
	}
	if got := idsOf(t, f.body(t, "/api/works", editorToken(t), 200), "id"); !sameIDs(got, 7, 8, 9) {
		t.Errorf("редактор: работы %v, ждали [7 8 9]", got)
	}
}

func TestShelfHidesClosed(t *testing.T) {
	f := newListsFixture(t)
	type shelfOut struct {
		Editions []struct {
			Edition struct{ ID int64 }   `json:"edition"`
			Volumes []struct{ ID int64 } `json:"volumes"`
		} `json:"editions"`
		LooseWorks []struct{ ID int64 }    `json:"loose_works"`
		Journals   []struct{ Slug string } `json:"journals"`
	}
	read := func(token string) (editions, volumes, loose []int64, journals []string) {
		var s shelfOut
		if err := json.Unmarshal(f.body(t, "/api/shelf", token, 200), &s); err != nil {
			t.Fatal(err)
		}
		for _, e := range s.Editions {
			editions = append(editions, e.Edition.ID)
			for _, v := range e.Volumes {
				volumes = append(volumes, v.ID)
			}
		}
		for _, l := range s.LooseWorks {
			loose = append(loose, l.ID)
		}
		for _, j := range s.Journals {
			journals = append(journals, j.Slug)
		}
		return
	}
	e, v, l, j := read("")
	if !sameIDs(e, 21) || !sameIDs(v, 8) || !sameIDs(l, 32) || len(j) != 1 || j[0] != "bol" {
		t.Errorf("аноним: издания %v, тома %v, свободные %v, журналы %v", e, v, l, j)
	}
	e, v, l, j = read(editorToken(t))
	if !sameIDs(e, 20, 21) || !sameIDs(v, 9, 7, 8) || !sameIDs(l, 31, 32) || len(j) != 2 {
		t.Errorf("редактор: издания %v, тома %v, свободные %v, журналы %v", e, v, l, j)
	}
}

func TestEditionListAndGetHideClosed(t *testing.T) {
	f := newListsFixture(t)
	if got := idsOf(t, f.body(t, "/api/editions", "", 200), "id"); !sameIDs(got, 21) {
		t.Errorf("аноним: издания %v", got)
	}
	if got := idsOf(t, f.body(t, "/api/editions", editorToken(t), 200), "id"); !sameIDs(got, 20, 21) {
		t.Errorf("редактор: издания %v", got)
	}
}

func TestEditionGetClosedIs404(t *testing.T) {
	f := newListsFixture(t)
	for _, token := range []string{"", readerToken(t)} {
		f.body(t, "/api/editions/20", token, 404)
		f.body(t, "/api/editions/21", token, 200)
	}
	f.body(t, "/api/editions/20", editorToken(t), 200)
}

func TestEditionWorksHideClosed(t *testing.T) {
	f := newListsFixture(t)
	if got := idsOf(t, f.body(t, "/api/editions/21/works", "", 200), "id"); !sameIDs(got, 8) {
		t.Errorf("аноним: тома %v, ждали [8]", got)
	}
	if got := idsOf(t, f.body(t, "/api/editions/21/works", editorToken(t), 200), "id"); !sameIDs(got, 7, 8, 9) {
		t.Errorf("редактор: тома %v", got)
	}
	// Закрытое издание: не-сотруднику 404, сотруднику — его тома.
	f.body(t, "/api/editions/20/works", "", 404)
	f.body(t, "/api/editions/20/works", editorToken(t), 200)
}

func TestHighlightsHideClosed(t *testing.T) {
	f := newListsFixture(t)
	if got := idsOf(t, f.body(t, "/api/editions/21/highlights", "", 200), "work_id"); !sameIDs(got, 8) {
		t.Errorf("аноним: избранное %v, ждали [8]", got)
	}
	if got := idsOf(t, f.body(t, "/api/editions/21/highlights", editorToken(t), 200), "work_id"); !sameIDs(got, 7, 8) {
		t.Errorf("редактор: избранное %v", got)
	}
}

func TestHighlightsOfClosedEditionIs404(t *testing.T) {
	h := NewHighlightHandler(&fakeHighlightStore{edition: &models.Edition{ID: 20, Closed: true}})
	rt := createTestRouter()
	r := NewRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		rt.cacheHandler, nil, rt.authService).WithHighlights(h)
	handler := r.Setup()
	if code := gateGet(handler, "/api/editions/20/highlights", ""); code != 404 {
		t.Errorf("аноним: код %d, ждали 404", code)
	}
	if code := gateGet(handler, "/api/editions/20/highlights", editorToken(t)); code != 200 {
		t.Errorf("редактор: код %d, ждали 200", code)
	}
}

func TestJournalListAndDetailHideClosed(t *testing.T) {
	f := newListsFixture(t)
	if got := idsOf(t, f.body(t, "/api/journals", "", 200), "id"); !sameIDs(got, 4) {
		t.Errorf("аноним: журналы %v", got)
	}
	if got := idsOf(t, f.body(t, "/api/journals", editorToken(t), 200), "id"); !sameIDs(got, 3, 4) {
		t.Errorf("редактор: журналы %v", got)
	}
}

func TestClosedJournalIs404(t *testing.T) {
	f := newListsFixture(t)
	f.body(t, "/api/journals/pzm", "", 404)
	f.body(t, "/api/journals/pzm", readerToken(t), 404)
	f.body(t, "/api/journals/pzm", editorToken(t), 200)
}

func TestJournalDetailPassesStaffFlag(t *testing.T) {
	f := newListsFixture(t)
	f.journals.journal.Closed = false
	f.body(t, "/api/journals/pzm", "", 200)
	if f.journals.includeClosed {
		t.Error("аноним: includeClosed == true")
	}
	f.body(t, "/api/journals/pzm", editorToken(t), 200)
	if !f.journals.includeClosed {
		t.Error("редактор: includeClosed == false")
	}
}

func TestPersonAllClosedIs404(t *testing.T) {
	f := newListsFixture(t)
	f.persons.detail = &models.PersonDetail{Person: &models.Person{ID: 1, Slug: "ivanov"}}
	f.persons.detailClosed = true
	f.body(t, "/api/persons/ivanov", "", 404)
	f.body(t, "/api/persons/ivanov", editorToken(t), 200)
}

func TestPersonPartlyClosedShowsOpenOnly(t *testing.T) {
	f := newListsFixture(t)
	f.persons.detail = &models.PersonDetail{Person: &models.Person{ID: 1, Slug: "ivanov"}}
	f.body(t, "/api/persons/ivanov", "", 200)
	if f.persons.includeClosed {
		t.Error("аноним: includeClosed == true")
	}
	f.body(t, "/api/persons/ivanov", editorToken(t), 200)
	if !f.persons.includeClosed {
		t.Error("редактор: includeClosed == false")
	}
}
