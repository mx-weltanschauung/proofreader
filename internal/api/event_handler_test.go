package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"proofreader/internal/auth"
	"proofreader/internal/models"
	"proofreader/internal/repository"
)

// fakeEventStore — подделка; не названные методы паникуют (интерфейс nil).
type fakeEventStore struct {
	EventStore
	orgs   []models.EventOrganizationDetail
	detail *models.EventDetail
	links  []models.ChapterEventLink
	linkEr error
}

func (f *fakeEventStore) Organizations(context.Context) ([]models.EventOrganizationDetail, error) {
	return f.orgs, nil
}

func (f *fakeEventStore) OrganizationDetail(_ context.Context, slug string) (*models.EventOrganizationDetail, error) {
	for i := range f.orgs {
		if f.orgs[i].Slug == slug {
			return &f.orgs[i], nil
		}
	}
	return nil, repository.ErrEventOrgNotFound
}

func (f *fakeEventStore) EventDetail(_ context.Context, slug string) (*models.EventDetail, error) {
	if f.detail == nil || f.detail.Slug != slug {
		return nil, repository.ErrEventNotFound
	}
	return f.detail, nil
}

func (f *fakeEventStore) ChapterEvents(context.Context, int64, int64) ([]models.ChapterEventLink, error) {
	return f.links, f.linkEr
}

func closedOnlyEvent() *models.EventDetail {
	return &models.EventDetail{
		Event:        models.Event{Slug: "i-sezd", Title: "I съезд"},
		Organization: models.EventOrganization{Slug: "org-a", Title: "Организация А"},
		Sections: []models.EventSection{{Title: "П", Entries: []models.EventEntry{
			{Label: "Доклад", Targets: []models.EventTarget{tgt(9, "")}},
		}}},
	}
}

func closedOnlyOrgs() []models.EventOrganizationDetail {
	return []models.EventOrganizationDetail{{
		EventOrganization: models.EventOrganization{ID: 1, Slug: "org-a", Title: "Организация А"},
		Kinds: []models.EventKindEvents{{EventKind: models.EventKind{Title: "Съезды"}, Events: []models.EventSummary{
			{Slug: "i-sezd", Title: "I съезд", MaterialsTotal: 1, TargetWorkIDs: []int64{9}},
		}}},
	}}
}

func eventHandlerFor(store EventStore) *EventHandler {
	return NewEventHandler(store, &fakeClosedStore{works: map[int64]string{9: models.ClosedByWork}})
}

func TestEventGetAllClosedIs404ForAnonymousAndReader(t *testing.T) {
	h := eventHandlerFor(&fakeEventStore{detail: closedOnlyEvent()})
	vars := map[string]string{"slug": "org-a", "eventSlug": "i-sezd"}
	for name, claims := range map[string]*auth.Claims{"аноним": nil, "читатель": readerClaims(5, "чтец")} {
		rec := doGetVars(t, h.GetEvent, "/api/organizations/org-a/i-sezd", vars, claims)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	rec := doGetVars(t, h.GetEvent, "/api/organizations/org-a/i-sezd", vars, editorClaims())
	if rec.Code != http.StatusOK {
		t.Fatalf("редактор: %d %s", rec.Code, rec.Body)
	}
	var d models.EventDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil || !d.Sections[0].Entries[0].Targets[0].Closed {
		t.Errorf("пометка Closed у редактора: %v %s", err, rec.Body)
	}
}

func TestEventGetWrongOrganizationIs404(t *testing.T) {
	h := eventHandlerFor(&fakeEventStore{detail: closedOnlyEvent()})
	rec := doGetVars(t, h.GetEvent, "/x", map[string]string{"slug": "org-b", "eventSlug": "i-sezd"}, editorClaims())
	if rec.Code != http.StatusNotFound {
		t.Errorf("%d", rec.Code)
	}
}

func TestOrganizationGetAllClosedIs404(t *testing.T) {
	h := eventHandlerFor(&fakeEventStore{orgs: closedOnlyOrgs()})
	vars := map[string]string{"slug": "org-a"}
	if rec := doGetVars(t, h.GetOrganization, "/x", vars, nil); rec.Code != http.StatusNotFound {
		t.Errorf("аноним: %d", rec.Code)
	}
	if rec := doGetVars(t, h.GetOrganization, "/x", vars, editorClaims()); rec.Code != http.StatusOK {
		t.Errorf("редактор: %d", rec.Code)
	}
}

func TestShelfOrganizationsHideAllClosed(t *testing.T) {
	build := func() *ShelfHandler {
		return NewShelfHandler(&fakeShelfEditions{}, &fakeShelfWorks{}).
			WithClosed(&fakeClosedStore{works: map[int64]string{9: models.ClosedByWork}}).
			WithEvents(&fakeEventStore{orgs: closedOnlyOrgs()})
	}
	get := func(claims *auth.Claims) models.Shelf {
		req := httptest.NewRequest(http.MethodGet, "/api/shelf", nil)
		rec := httptest.NewRecorder()
		withClaims(claims, build().Get)(rec, req)
		var s models.Shelf
		if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
		if rec.Code == 200 && !json.Valid(rec.Body.Bytes()) {
			t.Fatal("не json")
		}
		return s
	}
	anon := get(nil)
	if anon.Organizations == nil || len(anon.Organizations) != 0 {
		t.Errorf("аноним: %+v", anon.Organizations)
	}
	ed := get(editorClaims())
	if len(ed.Organizations) != 1 || ed.Organizations[0].EventsTotal != 1 || ed.Organizations[0].Slug != "org-a" {
		t.Errorf("редактор: %+v", ed.Organizations)
	}
	// без хранилища событий — всё равно массив
	req := httptest.NewRequest(http.MethodGet, "/api/shelf", nil)
	rec := httptest.NewRecorder()
	NewShelfHandler(&fakeShelfEditions{}, &fakeShelfWorks{}).Get(rec, req)
	if !contains(rec.Body.String(), `"organizations":[]`) {
		t.Errorf("нет пустого массива: %s", rec.Body)
	}
}

func TestChapterEventsOfAnotherWorkIs404(t *testing.T) {
	h := eventHandlerFor(&fakeEventStore{linkEr: repository.ErrChapterNotInWork})
	rec := doGetVars(t, h.ChapterEvents, "/x", map[string]string{"workId": "1", "id": "2"}, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("%d", rec.Code)
	}
}

func TestChapterEventsListsLinks(t *testing.T) {
	links := []models.ChapterEventLink{{OrganizationSlug: "org-a", EventSlug: "i-sezd", SectionTitle: "П"}}
	h := eventHandlerFor(&fakeEventStore{links: links})
	rec := doGetVars(t, h.ChapterEvents, "/x", map[string]string{"workId": "1", "id": "2"}, nil)
	var got []models.ChapterEventLink
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &got) != nil || len(got) != 1 || got[0].EventSlug != "i-sezd" {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
	h = eventHandlerFor(&fakeEventStore{})
	rec = doGetVars(t, h.ChapterEvents, "/x", map[string]string{"workId": "1", "id": "2"}, nil)
	if rec.Body.String() == "null\n" || !contains(rec.Body.String(), "[]") {
		t.Errorf("пусто должно быть []: %s", rec.Body)
	}
}
