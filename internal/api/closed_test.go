package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"proofreader/internal/middleware"
	"proofreader/internal/models"
)

// fakeClosedStore — закрытые работы заданы множеством; calls считает обращения.
type fakeClosedStore struct {
	works map[int64]string
	err   error
	calls int
}

func (f *fakeClosedStore) ClosedBy(_ context.Context, id int64) (string, error) {
	f.calls++
	return f.works[id], f.err
}

func (f *fakeClosedStore) ClosedIDs(context.Context) (map[int64]bool, error) {
	f.calls++
	out := map[int64]bool{}
	for id := range f.works {
		out[id] = true
	}
	return out, f.err
}

func (f *fakeClosedStore) ClosedEditionIDs(context.Context) (map[int64]bool, error) {
	f.calls++
	return map[int64]bool{}, f.err
}

// gateRouter — один маршрут тома за OptionalAuth и closedGate, в том же
// порядке, что у подроутера public. Возвращает роутер и счётчик вызовов
// обработчика.
func gateRouter(t *testing.T, store ClosedStore, pattern string) (*mux.Router, *int) {
	t.Helper()
	called := 0
	r := mux.NewRouter()
	api := r.PathPrefix("/api").Subrouter()
	api.Use(middleware.OptionalAuth(createTestRouter().authService))
	api.Use(closedGate(store))
	api.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}).Methods("GET")
	return r, &called
}

func gateGet(r http.Handler, path, token string) int {
	req := httptest.NewRequest("GET", path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code
}

func TestClosedGateHidesFromAnonymousAndReader(t *testing.T) {
	store := &fakeClosedStore{works: map[int64]string{7: models.ClosedByWork}}
	r, called := gateRouter(t, store, "/works/{workId}/pages")

	for name, token := range map[string]string{"аноним": "", "читатель": readerToken(t)} {
		*called = 0
		if code := gateGet(r, "/api/works/7/pages", token); code != http.StatusNotFound {
			t.Errorf("%s: закрытая работа: код %d, ждали 404", name, code)
		}
		if *called != 0 {
			t.Errorf("%s: обработчик вызван для закрытой работы", name)
		}
		if code := gateGet(r, "/api/works/8/pages", token); code != http.StatusOK {
			t.Errorf("%s: открытая работа: код %d, ждали 200", name, code)
		}
	}
	for name, token := range map[string]string{"редактор": editorToken(t), "админ": adminToken(t)} {
		if code := gateGet(r, "/api/works/7/pages", token); code != http.StatusOK {
			t.Errorf("%s: закрытая работа: код %d, ждали 200", name, code)
		}
	}
}

func TestClosedGateMatchesIdVariableAndRegexTemplates(t *testing.T) {
	store := &fakeClosedStore{works: map[int64]string{7: models.ClosedByEdition}}
	for _, pattern := range []string{"/works/{id}/audio", "/works/{workId:[0-9]+}/pages", "/works/{id:[0-9]+}"} {
		r, called := gateRouter(t, store, pattern)
		path := "/api/works/7/pages"
		switch pattern {
		case "/works/{id}/audio":
			path = "/api/works/7/audio"
		case "/works/{id:[0-9]+}":
			path = "/api/works/7"
		}
		if code := gateGet(r, path, ""); code != http.StatusNotFound || *called != 0 {
			t.Errorf("%s: код %d, вызовов %d; ждали 404 и 0", pattern, code, *called)
		}
	}
}

func TestClosedGateIgnoresOtherRoutes(t *testing.T) {
	store := &fakeClosedStore{works: map[int64]string{7: models.ClosedByWork}}
	r, called := gateRouter(t, store, "/editions/{id}")
	if code := gateGet(r, "/api/editions/7", ""); code != http.StatusOK || *called != 1 {
		t.Errorf("код %d, вызовов %d; ждали 200 и 1", code, *called)
	}
	if store.calls != 0 {
		t.Errorf("хранилище зовётся на чужом маршруте: %d", store.calls)
	}
}

func TestClosedGatePassesUnparseableIdAndNilStore(t *testing.T) {
	store := &fakeClosedStore{works: map[int64]string{7: models.ClosedByWork}}
	r, called := gateRouter(t, store, "/works/{workId}/pages")
	if code := gateGet(r, "/api/works/abc/pages", ""); code != http.StatusOK || *called != 1 {
		t.Errorf("нечисловой id: код %d, вызовов %d; ждали 200 и 1", code, *called)
	}
	r, called = gateRouter(t, nil, "/works/{workId}/pages")
	if code := gateGet(r, "/api/works/7/pages", ""); code != http.StatusOK || *called != 1 {
		t.Errorf("nil-хранилище: код %d, вызовов %d; ждали 200 и 1", code, *called)
	}
}

func TestClosedGateStoreErrorIs500(t *testing.T) {
	store := &fakeClosedStore{err: errors.New("база недоступна")}
	r, called := gateRouter(t, store, "/works/{workId}/pages")
	if code := gateGet(r, "/api/works/7/pages", ""); code != http.StatusInternalServerError || *called != 0 {
		t.Errorf("код %d, вызовов %d; ждали 500 и 0", code, *called)
	}
}

func TestClosedSetForStaffIsNil(t *testing.T) {
	store := &fakeClosedStore{works: map[int64]string{7: models.ClosedByWork}}
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+editorToken(t))
	var got ClosedSet
	var gotErr error
	h := middleware.OptionalAuth(createTestRouter().authService)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, gotErr = closedSetFor(r, store)
	}))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if gotErr != nil || got != nil || store.calls != 0 {
		t.Fatalf("сотруднику: set=%v err=%v calls=%d; ждали nil, nil, 0", got, gotErr, store.calls)
	}
}

func TestClosedSetForReaderAndNilSafety(t *testing.T) {
	store := &fakeClosedStore{works: map[int64]string{7: models.ClosedByWork}}
	req := httptest.NewRequest("GET", "/x", nil)
	set, err := closedSetFor(req, store)
	if err != nil || !set.Has(7) || set.Has(8) {
		t.Fatalf("аноним: set=%v err=%v", set, err)
	}
	if set, err = closedSetFor(req, nil); err != nil || set != nil {
		t.Fatalf("nil-хранилище: set=%v err=%v", set, err)
	}
	var none ClosedSet
	if none.Has(7) {
		t.Fatal("nil-множество ничего не закрывает")
	}
}
