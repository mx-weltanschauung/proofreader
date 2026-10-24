package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/pagecache"
	"proofreader/internal/repository"
)

type fakeClosedWriter struct {
	works, editions, journals map[int64][]int64
	gotID                     int64
	gotClosed                 bool
}

func (f *fakeClosedWriter) do(m map[int64][]int64, id int64, closed bool) ([]int64, error) {
	f.gotID, f.gotClosed = id, closed
	ids, ok := m[id]
	if !ok {
		return nil, repository.ErrClosedTargetNotFound
	}
	return ids, nil
}
func (f *fakeClosedWriter) SetWorkClosed(_ context.Context, id int64, c bool, _ models.ClosureActor, _ string) ([]int64, error) {
	return f.do(f.works, id, c)
}
func (f *fakeClosedWriter) SetEditionClosed(_ context.Context, id int64, c bool, _ models.ClosureActor, _ string) ([]int64, error) {
	return f.do(f.editions, id, c)
}
func (f *fakeClosedWriter) SetJournalClosed(_ context.Context, id int64, c bool, _ models.ClosureActor, _ string) ([]int64, error) {
	return f.do(f.journals, id, c)
}

func closedPut(h http.HandlerFunc, id, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, "/api/x/"+id+"/closed", bytes.NewBufferString(body))
	req = mux.SetURLVars(req, map[string]string{"id": id})
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

// putCachedFile кладёт в файловый кэш один файл работы.
func putCachedFile(t *testing.T, s *pagecache.Store, work int64) pagecache.Key {
	t.Helper()
	k := pagecache.Key{WorkID: work, Start: 1, End: 2}
	if err := s.Put(k, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestCloseEditionDropsEveryWork(t *testing.T) {
	dir := t.TempDir()
	store := pagecache.New(dir)
	keys := []pagecache.Key{putCachedFile(t, store, 1), putCachedFile(t, store, 2), putCachedFile(t, store, 3)}
	other := putCachedFile(t, store, 9)
	crawler := &fakeCrawlerCaches{}
	h := NewClosedHandler(&fakeClosedWriter{editions: map[int64][]int64{5: {1, 2, 3}}}, NewServingCache(store, crawler))

	rec := closedPut(h.Edition, "5", `{"closed": true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	var resp closedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != 5 || !resp.Closed || len(resp.Affected) != 3 {
		t.Fatalf("ответ %+v", resp)
	}
	for _, k := range keys {
		if _, _, ok := store.Open(k); ok {
			t.Errorf("файл работы %d остался в кэше", k.WorkID)
		}
	}
	if _, _, ok := store.Open(other); !ok {
		t.Error("сброс задел чужую работу")
	}
	if crawler.calls != 3 {
		t.Errorf("сбросов краулерского кэша %d, ждали 3", crawler.calls)
	}
}

func TestOpenWorkDropsCacheToo(t *testing.T) {
	store := pagecache.New(t.TempDir())
	k := putCachedFile(t, store, 7)
	crawler := &fakeCrawlerCaches{}
	w := &fakeClosedWriter{works: map[int64][]int64{7: {7}}}
	h := NewClosedHandler(w, NewServingCache(store, crawler))

	if rec := closedPut(h.Work, "7", `{"closed": false}`); rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	if w.gotClosed {
		t.Error("в хранилище ушло closed=true")
	}
	if _, _, ok := store.Open(k); ok {
		t.Error("при открытии файл работы остался в кэше")
	}
	if crawler.calls != 1 {
		t.Errorf("сбросов краулерского кэша %d, ждали 1", crawler.calls)
	}
}

// Издание без работ и журнал без номеров: затронутых работ нет, но у них
// своя страница и карточка в краулерском кэше — он сбрасывается всё равно.
func TestToggleWithNoAffectedWorksStillPurgesCrawler(t *testing.T) {
	crawler := &fakeCrawlerCaches{}
	h := NewClosedHandler(&fakeClosedWriter{
		editions: map[int64][]int64{5: {}}, journals: map[int64][]int64{6: nil},
	}, NewServingCache(pagecache.New(t.TempDir()), crawler))

	for _, c := range []struct {
		h    http.HandlerFunc
		id   string
		want int
	}{{h.Edition, "5", 1}, {h.Journal, "6", 2}} {
		rec := closedPut(c.h, c.id, `{"closed": true}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
		}
		if crawler.calls != c.want {
			t.Fatalf("сбросов %d, ждали %d", crawler.calls, c.want)
		}
		var resp closedResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Affected == nil {
			t.Error("affected должен быть [], а не null")
		}
	}
}

func TestClosedUnknownTargetAndBadBody(t *testing.T) {
	crawler := &fakeCrawlerCaches{}
	h := NewClosedHandler(&fakeClosedWriter{}, NewServingCache(nil, crawler))
	if rec := closedPut(h.Work, "99", `{"closed": true}`); rec.Code != http.StatusNotFound {
		t.Errorf("неизвестная цель: код %d, ждали 404", rec.Code)
	}
	for _, body := range []string{``, `{}`, `{"closed": "да"}`} {
		if rec := closedPut(h.Work, "7", body); rec.Code != http.StatusBadRequest {
			t.Errorf("тело %q: код %d, ждали 400", body, rec.Code)
		}
	}
	if rec := closedPut(h.Work, "abc", `{"closed": true}`); rec.Code != http.StatusBadRequest {
		t.Errorf("id abc: код %d, ждали 400", rec.Code)
	}
	if crawler.calls != 0 {
		t.Error("при отказе кэш сбрасывать не надо")
	}
}

func TestClosedRoutesAreStaffOnly(t *testing.T) {
	rt := createTestRouter()
	w := &fakeClosedWriter{works: map[int64][]int64{7: {7}}, editions: map[int64][]int64{7: {7}}, journals: map[int64][]int64{7: {7}}}
	r := NewRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		rt.cacheHandler, nil, rt.authService).WithClosedWriter(NewClosedHandler(w, nil)).Setup()
	for _, path := range []string{"/api/works/7/closed", "/api/editions/7/closed", "/api/journals/7/closed"} {
		for tok, want := range map[string]int{"": 401, readerToken(t): 403, editorToken(t): 200, adminToken(t): 200} {
			req := httptest.NewRequest(http.MethodPut, path, bytes.NewBufferString(`{"closed": true}`))
			if tok != "" {
				req.Header.Set("Authorization", "Bearer "+tok)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != want {
				t.Errorf("%s токен %q: код %d, ждали %d", path, tok, rec.Code, want)
			}
		}
	}
}

func TestCreateWorkWithClosed(t *testing.T) {
	for _, c := range []struct {
		body string
		want bool
	}{{`{"title":"Т","closed":true}`, true}, {`{"title":"Т"}`, false}} {
		repo := &fakeWorkRepo{}
		h := NewWorkHandler(repo, nil, nil, nil, 0, nil, nil)
		h.Create(httptest.NewRecorder(), journalRequest(http.MethodPost, "/api/works", c.body, nil))
		if repo.created == nil || repo.created.Closed != c.want {
			t.Errorf("тело %s: Closed = %v, ждали %v", c.body, repo.created, c.want)
		}
	}
}

func TestCreateEditionWithClosed(t *testing.T) {
	store := &fakeEditionStore{}
	req := httptest.NewRequest(http.MethodPost, "/api/editions", bytes.NewBufferString(`{"title":"С","slug":"s","closed":true}`))
	NewEditionHandler(store).Create(httptest.NewRecorder(), req)
	if store.created == nil || !store.created.Closed {
		t.Fatalf("создано %+v, ждали Closed", store.created)
	}
}

func TestCreateJournalAndIssueWithClosed(t *testing.T) {
	store := &fakeJournalStore{}
	rec := httptest.NewRecorder()
	NewJournalHandler(store).Create(rec, journalRequest(http.MethodPost, "/api/journals",
		`{"slug":"pzm","title":"ПЗМ","closed":true}`, nil))
	if rec.Code != http.StatusCreated || !store.journal.Closed {
		t.Fatalf("журнал: код %d, %+v", rec.Code, store.journal)
	}
	store = &fakeJournalStore{journal: &models.Journal{ID: 3, Title: "ПЗМ"}}
	rec = httptest.NewRecorder()
	NewJournalHandler(store).CreateIssue(rec, journalRequest(http.MethodPost, "/api/journals/3/issues",
		`{"year":1925,"number_from":5,"closed":true}`, map[string]string{"id": "3"}))
	if rec.Code != http.StatusCreated || !store.createdWork.Closed {
		t.Fatalf("номер: код %d, работа %+v", rec.Code, store.createdWork)
	}
}

// Общие Update признак не пишут, а в ответе обязаны отдавать настоящий.
func TestUpdateResponsesCarryStoredClosed(t *testing.T) {
	decode := func(rec *httptest.ResponseRecorder) (v struct{ Closed bool }) {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &v)
		return
	}

	es := &fakeEditionStore{editions: []*models.Edition{{ID: 7, Title: "С", Slug: "s", Closed: true}}}
	req := httptest.NewRequest(http.MethodPut, "/api/editions/7", bytes.NewBufferString(`{"title":"Н","slug":"s","closed":false}`))
	rec := httptest.NewRecorder()
	NewEditionHandler(es).Update(rec, mux.SetURLVars(req, map[string]string{"id": "7"}))
	if !decode(rec).Closed {
		t.Error("издание: ответ сказал closed=false при закрытом")
	}

	js := &fakeJournalStore{journal: &models.Journal{ID: 3, Slug: "pzm", Title: "ПЗМ", Closed: true}}
	rec = httptest.NewRecorder()
	NewJournalHandler(js).Update(rec, journalRequest(http.MethodPut, "/api/journals/3",
		`{"slug":"pzm","title":"Новое","closed":false}`, map[string]string{"id": "3"}))
	if !decode(rec).Closed || !js.journal.Closed {
		t.Error("журнал: признак потерян")
	}

	wr := &fakeWorkRepo{closed: true}
	req = httptest.NewRequest(http.MethodPut, "/api/works/4", bytes.NewBufferString(`{"title":"Н","closed":false}`))
	rec = httptest.NewRecorder()
	NewWorkHandler(wr, nil, nil, nil, 0, nil, nil).Update(rec, mux.SetURLVars(req, map[string]string{"id": "4"}))
	if !decode(rec).Closed {
		t.Error("работа: ответ сказал closed=false при закрытой")
	}
}
