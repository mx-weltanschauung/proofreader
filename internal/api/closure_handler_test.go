package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/auth"
	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/internal/pagecache"
)

// asRole подставляет в запрос claims с нужной ролью.
func asRole(req *http.Request, role models.UserRole) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey,
		&auth.Claims{UserID: 1, Role: role}))
}

// fakeChapterSource — главы одним деревом; GetByID ищет по всему дереву.
type fakeChapterSource []*models.Chapter

func (f fakeChapterSource) GetByID(_ context.Context, id int64) (*models.Chapter, error) {
	var find func(list []*models.Chapter) *models.Chapter
	find = func(list []*models.Chapter) *models.Chapter {
		for _, c := range list {
			if c.ID == id {
				return c
			}
			if got := find(c.Children); got != nil {
				return got
			}
		}
		return nil
	}
	if c := find(f); c != nil {
		return c, nil
	}
	return nil, errors.New("not found")
}

func (f fakeChapterSource) ListByWorkHierarchical(_ context.Context, workID int64) ([]*models.Chapter, error) {
	var out []*models.Chapter
	for _, c := range f {
		if c.WorkID == workID {
			out = append(out, c)
		}
	}
	return out, nil
}

func chapterPut(h http.HandlerFunc, workID, id, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"workId": workID, "id": id})
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestCloseRequiresReason(t *testing.T) {
	w := &fakeClosedWriter{works: map[int64][]int64{7: {7}}, chapters: map[int64][]int64{5: {7}}}
	h := NewClosedHandler(w, NewServingCache(nil, &fakeCrawlerCaches{})).
		WithChapters(fakeChapterSource{{ID: 5, WorkID: 7}})
	for _, body := range []string{
		`{"closed": true}`, `{"closed": true, "reason": ""}`, `{"closed": true, "reason": "  \t "}`,
	} {
		if rec := closedPut(h.Work, "7", body); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Укажите причину") {
			t.Errorf("работа, тело %s: код %d, %s", body, rec.Code, rec.Body.String())
		}
		if rec := chapterPut(h.Chapter, "7", "5", body); rec.Code != http.StatusBadRequest {
			t.Errorf("глава, тело %s: код %d", body, rec.Code)
		}
	}
	if w.calls != 0 {
		t.Errorf("в хранилище сходили %d раз при пустой причине", w.calls)
	}
	// Присланная причина доезжает до хранилища очищенной от пробелов.
	if rec := chapterPut(h.Chapter, "7", "5", `{"closed": true, "reason": " письмо "}`); rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	if w.gotReason != "письмо" {
		t.Errorf("причина %q", w.gotReason)
	}
}

func TestCloseChapterOfOtherWork404(t *testing.T) {
	w := &fakeClosedWriter{chapters: map[int64][]int64{5: {7}}}
	h := NewClosedHandler(w, NewServingCache(nil, &fakeCrawlerCaches{})).
		WithChapters(fakeChapterSource{{ID: 5, WorkID: 7}})
	if rec := chapterPut(h.Chapter, "8", "5", `{"closed": true, "reason": "р"}`); rec.Code != http.StatusNotFound {
		t.Errorf("глава чужой работы: код %d, ждали 404", rec.Code)
	}
	if rec := chapterPut(h.Chapter, "7", "99", `{"closed": true, "reason": "р"}`); rec.Code != http.StatusNotFound {
		t.Errorf("нет главы: код %d, ждали 404", rec.Code)
	}
	if w.calls != 0 {
		t.Error("признак записан для чужой главы")
	}
}

func TestCloseChapterDropsWorkCache(t *testing.T) {
	store := pagecache.New(t.TempDir())
	k := putCachedFile(t, store, 7)
	other := putCachedFile(t, store, 9)
	w := &fakeClosedWriter{chapters: map[int64][]int64{5: {7}}}
	h := NewClosedHandler(w, NewServingCache(store, &fakeCrawlerCaches{})).
		WithChapters(fakeChapterSource{{ID: 5, WorkID: 7}})
	if rec := chapterPut(h.Chapter, "7", "5", `{"closed": true, "reason": "письмо"}`); rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	if _, _, ok := store.Open(k); ok {
		t.Error("файл работы остался в кэше")
	}
	if _, _, ok := store.Open(other); !ok {
		t.Error("сброс задел чужую работу")
	}
}

func TestEditorCannotCreateClosed(t *testing.T) {
	repo := &fakeWorkRepo{}
	rec := httptest.NewRecorder()
	NewWorkHandler(repo, nil, nil, nil, 0, nil, nil).Create(rec,
		journalRequest(http.MethodPost, "/api/works", `{"title":"Т","closed":true}`, nil))
	if rec.Code != http.StatusForbidden || repo.created != nil {
		t.Errorf("работа: код %d, создано %+v", rec.Code, repo.created)
	}
	es := &fakeEditionStore{}
	rec = httptest.NewRecorder()
	NewEditionHandler(es).Create(rec, asRole(httptest.NewRequest(http.MethodPost, "/api/editions",
		strings.NewReader(`{"title":"С","slug":"s","closed":true}`)), models.RoleEditor))
	if rec.Code != http.StatusForbidden || es.created != nil {
		t.Errorf("издание: код %d", rec.Code)
	}
	js := &fakeJournalStore{}
	rec = httptest.NewRecorder()
	NewJournalHandler(js).Create(rec, journalRequest(http.MethodPost, "/api/journals",
		`{"slug":"p","title":"П","closed":true}`, nil))
	if rec.Code != http.StatusForbidden || js.journal != nil {
		t.Errorf("журнал: код %d", rec.Code)
	}
	js = &fakeJournalStore{journal: &models.Journal{ID: 3, Title: "П"}}
	rec = httptest.NewRecorder()
	NewJournalHandler(js).CreateIssue(rec, journalRequest(http.MethodPost, "/api/journals/3/issues",
		`{"year":1925,"number_from":5,"closed":true}`, map[string]string{"id": "3"}))
	if rec.Code != http.StatusForbidden || js.createdWork != nil {
		t.Errorf("номер: код %d", rec.Code)
	}
	// Открытое редактор заводит как прежде.
	repo = &fakeWorkRepo{}
	rec = httptest.NewRecorder()
	NewWorkHandler(repo, nil, nil, nil, 0, nil, nil).Create(rec,
		journalRequest(http.MethodPost, "/api/works", `{"title":"Т"}`, nil))
	if repo.created == nil {
		t.Errorf("открытая работа не создана: код %d", rec.Code)
	}
}

func TestAdminCreateClosedDefaultReason(t *testing.T) {
	for _, c := range []struct{ body, want string }{
		{`{"title":"Т","closed":true}`, "закрыто при создании"},
		{`{"title":"Т","closed":true,"closed_reason":" жалоба "}`, "жалоба"},
	} {
		repo := &fakeWorkRepo{}
		NewWorkHandler(repo, nil, nil, nil, 0, nil, nil).Create(httptest.NewRecorder(),
			asRole(journalRequest(http.MethodPost, "/api/works", c.body, nil), models.RoleAdministrator))
		if repo.created == nil || repo.createdReason != c.want {
			t.Errorf("тело %s: причина %q, ждали %q", c.body, repo.createdReason, c.want)
		}
	}
}

type fakeClosureLog struct {
	latest        *models.Closure
	limit, offset int
}

func (f *fakeClosureLog) LatestClosure(context.Context, string, int64) (*models.Closure, error) {
	return f.latest, nil
}
func (f *fakeClosureLog) ListClosures(_ context.Context, limit, offset int) ([]models.Closure, error) {
	f.limit, f.offset = limit, offset
	return nil, nil
}

func TestLatestClosureHidesReasonFromEditor(t *testing.T) {
	log := &fakeClosureLog{latest: &models.Closure{Closed: true, Reason: "письмо правообладателя",
		At: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)}}
	h := NewClosedHandler(nil, nil).WithLog(log)
	get := func(role models.UserRole, url string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.Latest(rec, asRole(httptest.NewRequest(http.MethodGet, url, nil), role))
		return rec
	}
	var resp latestClosureResponse
	rec := get(models.RoleEditor, "/api/closures/latest?kind=chapter&id=5")
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if rec.Code != 200 || !resp.Closed || resp.Reason != "" || resp.At != "2026-10-23T12:00:00Z" {
		t.Errorf("редактор: код %d, %+v", rec.Code, resp)
	}
	resp = latestClosureResponse{}
	rec = get(models.RoleAdministrator, "/api/closures/latest?kind=chapter&id=5")
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Reason != "письмо правообладателя" {
		t.Errorf("администратор: %+v", resp)
	}
	log.latest = nil
	if rec := get(models.RoleAdministrator, "/api/closures/latest?kind=work&id=5"); rec.Code != 404 {
		t.Errorf("нет записей: код %d, ждали 404", rec.Code)
	}
	if rec := get(models.RoleAdministrator, "/api/closures/latest?kind=x&id=5"); rec.Code != 400 {
		t.Errorf("неверный вид: код %d", rec.Code)
	}
}

func TestClosuresListValidatesPaging(t *testing.T) {
	log := &fakeClosureLog{}
	h := NewClosedHandler(nil, nil).WithLog(log)
	for q, want := range map[string]int{
		"": 200, "?limit=10&offset=20": 200, "?limit=-1": 400, "?limit=0": 400, "?limit=abc": 400,
		"?offset=-5": 400, "?offset=x": 400,
	} {
		rec := httptest.NewRecorder()
		h.List(rec, httptest.NewRequest(http.MethodGet, "/api/admin/closures"+q, nil))
		if rec.Code != want {
			t.Errorf("%q: код %d, ждали %d", q, rec.Code, want)
		}
	}
	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest(http.MethodGet, "/api/admin/closures", nil))
	if log.limit != 100 || log.offset != 0 || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("умолчание: limit %d offset %d тело %s", log.limit, log.offset, rec.Body.String())
	}
	h.List(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/admin/closures?limit=100000", nil))
	if log.limit != 500 {
		t.Errorf("потолок: limit %d, ждали 500", log.limit)
	}
}

func TestClosedPlanCountsSharedPages(t *testing.T) {
	src := fakeChapterSource{
		{ID: 1, WorkID: 7, Title: "Левая", StartPage: 30, EndPage: 36},
		{ID: 2, WorkID: 7, Title: "Закрываемая", StartPage: 36, EndPage: 39, Children: []*models.Chapter{
			{ID: 3, WorkID: 7, Title: "Подглава", StartPage: 37, EndPage: 38},
		}},
		{ID: 4, WorkID: 7, Title: "Правая", StartPage: 39, EndPage: 47},
		{ID: 5, WorkID: 8, Title: "Чужая работа", StartPage: 36, EndPage: 39},
	}
	h := NewClosedHandler(nil, nil).WithChapters(src)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "7", "id": "2"})
	rec := httptest.NewRecorder()
	h.Plan(rec, req)
	if rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	var plan closePlan
	if err := json.Unmarshal(rec.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Chapters != 2 || plan.Pages != 4 || len(plan.Shared) != 2 {
		t.Fatalf("план %+v", plan)
	}
	if plan.Shared[0].PageNumber != 36 || strings.Join(plan.Shared[0].Neighbors, ",") != "Левая" ||
		plan.Shared[1].PageNumber != 39 || strings.Join(plan.Shared[1].Neighbors, ",") != "Правая" {
		t.Errorf("общие полосы %+v", plan.Shared)
	}
	// Глава другой работы — 404; без общих полос shared — [], а не null.
	req = mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/x", nil), map[string]string{"workId": "8", "id": "2"})
	rec = httptest.NewRecorder()
	h.Plan(rec, req)
	if rec.Code != 404 {
		t.Errorf("чужая работа: код %d", rec.Code)
	}
	req = mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/x", nil), map[string]string{"workId": "7", "id": "3"})
	rec = httptest.NewRecorder()
	h.Plan(rec, req)
	if !strings.Contains(rec.Body.String(), `"shared":[]`) {
		t.Errorf("подглава без соседей: %s", rec.Body.String())
	}
}
