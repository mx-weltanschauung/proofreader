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

	"proofreader/internal/models"
	"proofreader/internal/pagecache"
	"proofreader/pkg/storage"
)

// newTestRouter builds a Router the same way createTestRouter() (router_test.go)
// does: real authService, nil concrete handlers. This package's test harness has
// no DB, so work/page/chapter/document/category handlers cannot be exercised
// end-to-end here — see the comment on serveWithRecover below for how we still
// assert the auth/role boundary without a live handler.
func newTestRouter(t *testing.T) *Router {
	t.Helper()
	return createTestRouter()
}

// tokenForRole mints a JWT for a synthetic user with the given role, using the
// same JWT config as createTestRouter() (shared "test-secret").
func tokenForRole(t *testing.T, role models.UserRole) string {
	t.Helper()
	rt := createTestRouter()
	token, err := rt.authService.GenerateToken(&models.User{
		ID:    1,
		Email: "test-user@example.com",
		Role:  role,
	})
	if err != nil {
		t.Fatalf("failed to generate token for role %q: %v", role, err)
	}
	return token
}

func editorToken(t *testing.T) string {
	t.Helper()
	return tokenForRole(t, models.RoleEditor)
}

func adminToken(t *testing.T) string {
	t.Helper()
	return tokenForRole(t, models.RoleAdministrator)
}

// serveWithRecover runs the request through the full router and returns the
// resulting status code. Because this package's handlers are wired with a nil
// *pgxpool.Pool-backed repo (no DB in this test harness — see newTestRouter),
// any request that gets PAST the auth/role middleware and reaches a concrete
// handler will panic on a nil pointer dereference inside that handler.
//
// We recover from that panic here and treat it as success for the purpose of
// this test: reaching the handler proves the request cleared the middleware
// gate we're testing. httptest.ResponseRecorder defaults its Code to 200 when
// WriteHeader is never called, so a recovered panic reads as "200-ish, not
// 401/403" to the assertions below — which is exactly the signal we want
// ("not gated"), without being able to assert a genuine 200 response body.
func serveWithRecover(t *testing.T, r http.Handler, req *http.Request) int {
	t.Helper()
	rec := httptest.NewRecorder()
	func() {
		defer func() {
			_ = recover() // nil-handler panic == request passed the middleware gate
		}()
		r.ServeHTTP(rec, req)
	}()
	return rec.Code
}

// TestAccessMatrix asserts the authorization boundary decided by middleware,
// before any (nil, DB-less) concrete handler runs:
//   - public GET reads must not be auth-gated (401) for guests
//   - mutations, including the page-edit "hole", must 401 for guests
//   - page versions stay behind plain auth (401 for guests)
//   - the page-edit "hole" is closed: a non-editor/admin authenticated role
//     must be 403'd by RequireRole, not allowed through
//   - an editor token clears the page-edit role gate
//   - admin-only routes (/api/users, Task 7) are gated by RequireRole(administrator):
//     401 for guests, 403 for editors, and cleared (not 401/403) for admins
func TestAccessMatrix(t *testing.T) {
	rt := newTestRouter(t)
	r := rt.Setup()

	t.Run("guest public reads are not auth-gated", func(t *testing.T) {
		cases := []struct{ method, path string }{
			{"GET", "/api/works"},
			{"GET", "/api/works/1"},
			{"GET", "/api/works/1/notes"},
			{"GET", "/api/works/1/pages"},
			{"GET", "/api/works/1/pages/1"},
			{"GET", "/api/works/1/pages/1/render"},
			{"GET", "/api/works/1/pages/by-number/1"},
			{"GET", "/api/works/1/page-map"},
			{"GET", "/api/works/1/chapters"},
			{"GET", "/api/works/1/chapters/1"},
			{"GET", "/api/works/1/chapters/1/pages"},
			{"GET", "/api/documents"},
			{"GET", "/api/documents/1"},
			{"GET", "/api/documents/1/view"},
			{"GET", "/api/categories"},
			{"GET", "/api/categories/1"},
			{"GET", "/api/collections"},
			{"GET", "/api/collections/materializm"},
			{"GET", "/api/collections/materializm/items/1/pages"},
			{"GET", "/api/works/1/download"},
			{"GET", "/api/works/1/chapters/1/download"},
			{"GET", "/api/collections/materializm/download"},
			{"GET", "/api/shelf"},
			{"GET", "/api/search?q=test"},
			{"GET", "/api/search/pages?q=test&work_id=1"},
			{"POST", "/api/feedback"},
		}
		for _, c := range cases {
			t.Run(c.method+" "+c.path, func(t *testing.T) {
				req := httptest.NewRequest(c.method, c.path, nil)
				status := serveWithRecover(t, r, req)
				if status == http.StatusUnauthorized {
					t.Fatalf("%s %s = 401, want the request to pass the public read layer (no auth gate)", c.method, c.path)
				}
			})
		}
	})

	t.Run("guest mutations and page edit are 401", func(t *testing.T) {
		cases := []struct{ method, path string }{
			{"POST", "/api/works"},
			{"PUT", "/api/works/1"},
			{"DELETE", "/api/works/1"},
			// Снятие аппарата и его план. План читает, но публичным не
			// становится: он перечисляет поимённо то, что собираются снять.
			{"GET", "/api/works/1/apparatus"},
			{"DELETE", "/api/works/1/apparatus"},
			{"POST", "/api/works/1/upload"},
			{"POST", "/api/editions/1/index/import"},
			{"POST", "/api/works/1/create-pages"},
			{"POST", "/api/works/1/categories"},
			{"DELETE", "/api/works/1/categories/1"},
			{"PUT", "/api/works/1/pages/1"}, // the edit hole
			{"POST", "/api/works/1/pages/1/versions/1/restore"},
			{"GET", "/api/works/1/pages/1/versions"},
			{"GET", "/api/works/1/pages/1/versions/1"},
			{"POST", "/api/works/1/chapters"},
			{"PUT", "/api/works/1/chapters/1"},
			{"DELETE", "/api/works/1/chapters/1"},
			{"PATCH", "/api/works/1/chapters/1/move"},
			{"POST", "/api/documents"},
			{"PUT", "/api/documents/1"},
			{"DELETE", "/api/documents/1"},
			{"POST", "/api/categories"},
			{"PUT", "/api/categories/1"},
			{"DELETE", "/api/categories/1"},
			{"POST", "/api/collections"},
			{"POST", "/api/collections/materializm/items"},
			{"PUT", "/api/collections/materializm/items/1"},
			{"DELETE", "/api/collections/materializm/items/1"},
			{"PATCH", "/api/collections/materializm/items/1/move"},
			{"GET", "/api/auth/me"},
		}
		for _, c := range cases {
			t.Run(c.method+" "+c.path, func(t *testing.T) {
				req := httptest.NewRequest(c.method, c.path, nil)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				if rec.Code != http.StatusUnauthorized {
					t.Fatalf("%s %s = %d, want %d", c.method, c.path, rec.Code, http.StatusUnauthorized)
				}
			})
		}
	})

	t.Run("non-staff authenticated role cannot edit page (hole closed)", func(t *testing.T) {
		// The two real roles in this system are administrator and editor
		// (self-registration and the third "proofreader" role were removed
		// in T2/T3). To prove RequireRole actually rejects any role other
		// than editor/admin - not just unauthenticated requests - we mint a
		// token with a synthetic non-staff role. Before the restructure this
		// route had no RequireRole gate at all, so any authenticated user
		// (any role) reached the handler; after, it must be 403.
		req := httptest.NewRequest("PUT", "/api/works/1/pages/1", nil)
		req.Header.Set("Authorization", "Bearer "+tokenForRole(t, models.UserRole("proofreader")))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("PUT /api/works/1/pages/1 with non-staff role = %d, want %d", rec.Code, http.StatusForbidden)
		}
	})

	t.Run("non-staff authenticated role cannot import index (hole closed)", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/editions/1/index/import", nil)
		req.Header.Set("Authorization", "Bearer "+tokenForRole(t, models.UserRole("proofreader")))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("POST /api/editions/1/index/import with non-staff role = %d, want %d", rec.Code, http.StatusForbidden)
		}
	})

	t.Run("editor can edit page", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/api/works/1/pages/1", nil)
		req.Header.Set("Authorization", "Bearer "+editorToken(t))
		status := serveWithRecover(t, r, req)
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			t.Fatalf("PUT /api/works/1/pages/1 with editor token = %d, want to clear the auth+role gate (not 401/403)", status)
		}
	})

	t.Run("guest cannot list users", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/users", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("GET /api/users with no token = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("editor cannot list users", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/users", nil)
		req.Header.Set("Authorization", "Bearer "+editorToken(t))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("GET /api/users with editor token = %d, want %d", rec.Code, http.StatusForbidden)
		}
	})

	t.Run("admin can list users", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/users", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken(t))
		status := serveWithRecover(t, r, req)
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			t.Fatalf("GET /api/users with admin token = %d, want to clear the auth+role gate (not 401/403)", status)
		}
	})

	t.Run("export is administrator-only", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/export", nil)
		if status := serveWithRecover(t, r, req); status != http.StatusUnauthorized {
			t.Errorf("guest GET /api/export = %d, want 401", status)
		}

		req = httptest.NewRequest("GET", "/api/export", nil)
		req.Header.Set("Authorization", "Bearer "+editorToken(t))
		if status := serveWithRecover(t, r, req); status != http.StatusForbidden {
			t.Errorf("editor GET /api/export = %d, want 403", status)
		}

		req = httptest.NewRequest("GET", "/api/export", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken(t))
		if status := serveWithRecover(t, r, req); status == http.StatusUnauthorized || status == http.StatusForbidden {
			t.Errorf("admin GET /api/export = %d, want the request to clear the role gate", status)
		}
	})

	// Разбор писем виден только владельцу читальни: письма читателей — не
	// редакторская зона, поэтому все четыре маршрута administrator-only, в
	// отличие от публичного POST /api/feedback выше.
	t.Run("feedback admin routes are administrator-only", func(t *testing.T) {
		cases := []struct{ method, path string }{
			{"GET", "/api/feedback"},
			{"GET", "/api/feedback/unread-count"},
			{"PATCH", "/api/feedback/1"},
			{"DELETE", "/api/feedback/1"},
		}
		for _, c := range cases {
			t.Run(c.method+" "+c.path, func(t *testing.T) {
				req := httptest.NewRequest(c.method, c.path, nil)
				if status := serveWithRecover(t, r, req); status != http.StatusUnauthorized {
					t.Errorf("guest %s %s = %d, want 401", c.method, c.path, status)
				}

				req = httptest.NewRequest(c.method, c.path, nil)
				req.Header.Set("Authorization", "Bearer "+editorToken(t))
				if status := serveWithRecover(t, r, req); status != http.StatusForbidden {
					t.Errorf("editor %s %s = %d, want 403", c.method, c.path, status)
				}

				req = httptest.NewRequest(c.method, c.path, nil)
				req.Header.Set("Authorization", "Bearer "+adminToken(t))
				if status := serveWithRecover(t, r, req); status == http.StatusUnauthorized || status == http.StatusForbidden {
					t.Errorf("admin %s %s = %d, want the request to clear the role gate", c.method, c.path, status)
				}
			})
		}
	})

	// Читатели и их подборки — личные данные, поэтому administrator-only, а
	// не staff. Разница между этими двумя уровнями — ровно ответ РЕДАКТОРУ, и
	// сторожевая таблица routeAccess её не ловит: TestStaffRoutesRejectReaderToken
	// и TestRequireRoleRejectsRoleOutsideEnum фильтруют accessStaff и accessAdmin
	// вместе и ждут 403, а подроутер staff отвечает 403 и читательскому токену,
	// и роли вне перечня. Замена admin.HandleFunc на staff.HandleFunc в
	// router.go оставляла весь набор зелёным, отдавая список читателей каждому
	// редактору, — эта проверка и заведена мутацией, которая так прошла.
	t.Run("reader admin routes are administrator-only", func(t *testing.T) {
		cases := []struct{ method, path string }{
			{"GET", "/api/readers"},
			{"GET", "/api/readers/chtec/collections"},
		}
		for _, c := range cases {
			t.Run(c.method+" "+c.path, func(t *testing.T) {
				req := httptest.NewRequest(c.method, c.path, nil)
				if status := serveWithRecover(t, r, req); status != http.StatusUnauthorized {
					t.Errorf("guest %s %s = %d, want 401", c.method, c.path, status)
				}

				req = httptest.NewRequest(c.method, c.path, nil)
				req.Header.Set("Authorization", "Bearer "+editorToken(t))
				if status := serveWithRecover(t, r, req); status != http.StatusForbidden {
					t.Errorf("editor %s %s = %d, want 403", c.method, c.path, status)
				}

				req = httptest.NewRequest(c.method, c.path, nil)
				req.Header.Set("Authorization", "Bearer "+adminToken(t))
				if status := serveWithRecover(t, r, req); status == http.StatusUnauthorized || status == http.StatusForbidden {
					t.Errorf("admin %s %s = %d, want the request to clear the role gate", c.method, c.path, status)
				}
			})
		}
	})

	// Очередь модерации предложений — работа редактора (см. router.go:
	// staff, не admin), поэтому в отличие от /api/users и /api/feedback
	// здесь editor обязан пройти, а не получить 403.
	t.Run("suggestion queue routes are staff-only, editor included", func(t *testing.T) {
		cases := []struct{ method, path string }{
			{"GET", "/api/suggestions"},
			{"GET", "/api/suggestions/1"},
		}
		for _, c := range cases {
			t.Run(c.method+" "+c.path, func(t *testing.T) {
				req := httptest.NewRequest(c.method, c.path, nil)
				if status := serveWithRecover(t, r, req); status != http.StatusUnauthorized {
					t.Errorf("guest %s %s = %d, want 401", c.method, c.path, status)
				}

				req = httptest.NewRequest(c.method, c.path, nil)
				req.Header.Set("Authorization", "Bearer "+tokenForRole(t, models.UserRole("proofreader")))
				if status := serveWithRecover(t, r, req); status != http.StatusForbidden {
					t.Errorf("non-staff %s %s = %d, want 403", c.method, c.path, status)
				}

				req = httptest.NewRequest(c.method, c.path, nil)
				req.Header.Set("Authorization", "Bearer "+editorToken(t))
				if status := serveWithRecover(t, r, req); status == http.StatusUnauthorized || status == http.StatusForbidden {
					t.Errorf("editor %s %s = %d, want the request to clear the role gate", c.method, c.path, status)
				}
			})
		}
	})

	// Регистрация staff "/suggestions/{id}" не должна перехватывать
	// "/suggestions/mine" (обе объявлены на одном префиксе в разных
	// подроутерах — reader и staff, см. router.go). Раньше это проверялось
	// только curl'ом на временном сервере, и проверка исчезла вместе с
	// сессией; здесь то же самое свойство держится прогоном через настоящий
	// rt.Setup().
	//
	// Гость без токена больше не различает два маршрута: оба подроутера
	// требуют AuthMiddleware, и оба ответят 401. Различает читательский
	// токен: reader пропускает роль reader дальше в Mine (200, пустой список
	// у только что выпущенного читателя), а staff отбивает её RequireRole
	// (403) до того, как Get вообще увидит "mine" как id. Поэтому 403 у
	// читателя — верный признак того, что "mine" перехвачен staff'ом
	// "/suggestions/{id}", а не то, что чего-то не хватает в запросе.
	t.Run("reader suggestions/mine is not swallowed by the staff {id} route", func(t *testing.T) {
		// serveWithRecover не годится здесь: recover() маскирует и настоящий
		// провал маршрутизации (staff перехватил "mine" как {id}, дошёл до
		// Get с nil-репозиторием и тоже паникует), и правильный путь (reader
		// дошёл до Mine) — оба читались бы одинаково "200-ish". Различить
		// 200 от 403 можно только с живым обработчиком, поэтому здесь, как и
		// в TestRouterDispatchesGetByNumberThroughMux, в роутер подставлен
		// настоящий PageSuggestionHandler поверх fakeSuggestionStore.
		rt2 := newTestRouter(t)
		rt2.pageSuggestionHandler = NewPageSuggestionHandler(&fakeSuggestionStore{}, nil, nil, nil, nil, "test-secret", false)
		r2 := rt2.Setup()

		req := httptest.NewRequest("GET", "/api/suggestions/mine", nil)
		req.Header.Set("Authorization", "Bearer "+readerToken(t))
		rec := httptest.NewRecorder()
		r2.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/suggestions/mine с читательским токеном = %d, want %d "+
				"(403 означает, что staff /suggestions/{id} перехватил литеральную строку "+
				"\"mine\" и отбил роль reader; 401 — что запрос вовсе не дошёл до reader)",
				rec.Code, http.StatusOK)
		}

		// Пустой список — Mine честно ответил на claims читателя без единой
		// правки, а не Get, у которого "mine" не парсится как числовой id.
		if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
			t.Fatalf("тело ответа = %q, want []", got)
		}

		// Гость без токена вообще не должен пройти AuthMiddleware ни на одном
		// из двух подроутеров.
		req = httptest.NewRequest("GET", "/api/suggestions/mine", nil)
		rec = httptest.NewRecorder()
		r2.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("GET /api/suggestions/mine без токена = %d, want 401", rec.Code)
		}
	})
}

// Сброс одной главы — редактору: устаревший текст после правки бьёт именно
// по нему. Сброс всего и сводка — только администратору: на боевом это два
// ядра под полным перерендером.
func TestCachePurgeRoleBoundary(t *testing.T) {
	rt := newTestRouter(t)
	r := rt.Setup()

	cases := []struct {
		name   string
		method string
		path   string
		token  string
		want   int
	}{
		{"глава без токена", http.MethodDelete, "/api/works/47/chapters/5/cache", "", http.StatusUnauthorized},
		{"глава редактором", http.MethodDelete, "/api/works/47/chapters/5/cache", editorToken(t), http.StatusOK},
		{"всё редактором", http.MethodDelete, "/api/cache", editorToken(t), http.StatusForbidden},
		{"всё админом", http.MethodDelete, "/api/cache", adminToken(t), http.StatusOK},
		{"сводка редактором", http.MethodGet, "/api/cache/stats", editorToken(t), http.StatusForbidden},
		{"сводка админом", http.MethodGet, "/api/cache/stats", adminToken(t), http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			if got := serveWithRecover(t, r, req); got != tc.want {
				t.Errorf("код %d, ожидался %d", got, tc.want)
			}
		})
	}
}

// TestRouterDispatchesGetByNumberThroughMux serves a real request through
// rt.Setup(), unlike page_handler_test.go's GetByNumber tests which all call
// the handler directly via mux.SetURLVars and so never prove the route is
// registered at all, nor that router.go's path-variable names ("workId",
// "pageNumber") actually match what the handler reads out of mux.Vars. A real
// PageHandler backed by fakePageStore is wired into the router so the whole
// path — route match, var extraction, handler, JSON response — is exercised.
func TestRouterDispatchesGetByNumberThroughMux(t *testing.T) {
	var gotWorkID int64
	var gotPageNumber int

	ph := &PageHandler{
		pageRepo: &fakePageStore{
			getByWorkAndPageNumberFn: func(_ context.Context, workID int64, pageNumber int) (*models.Page, error) {
				if pageNumber <= 0 {
					return nil, errors.New("page not found")
				}
				gotWorkID, gotPageNumber = workID, pageNumber
				return &models.Page{ID: 1882, WorkID: workID, PageNumber: pageNumber}, nil
			},
		},
		store:      storage.NewMemoryStorage(),
		presignTTL: time.Minute,
	}

	rt := newTestRouter(t)
	rt.pageHandler = ph
	r := rt.Setup()

	t.Run("resolves a real request end to end", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/works/3/pages/by-number/245", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if gotWorkID != 3 || gotPageNumber != 245 {
			t.Errorf("store received (workID=%d, pageNumber=%d), want (3, 245) - "+
				"router.go's path-variable names may no longer match what the handler reads",
				gotWorkID, gotPageNumber)
		}

		var got models.Page
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("bad JSON: %v; body: %s", err, rec.Body.String())
		}
		if got.PageNumber != 245 {
			t.Errorf("response page_number = %d, want 245", got.PageNumber)
		}
	})

	// Not asserted anywhere else: Atoi("-5") succeeds (-5), the repo finds no
	// row, and the handler falls through to its generic 404 - never an ID
	// fallback. Same route/handler, so it belongs alongside the case above.
	t.Run("negative page number is a clean 404, never an ID fallback", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/works/3/pages/by-number/-5", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
		}
	})
}

// TestCollectionDownloadRouteWinsOverGenericNicknameSlug serves a real
// request through rt.Setup() to prove router.go's registration order holds:
// GET /collections/{slug}/download and GET /collections/{nickname}/{slug}
// are both three path segments and both GET, so mux would resolve whichever
// is registered first. A nickname literally equal to "чтец" with a trailing
// "/download" must still reach DownloadHandler.Collection, not
// CollectionHandler.Get("чтец", "download") — real handlers are wired in
// because serveWithRecover can't tell the two outcomes apart (neither panics
// nor returns 401/403).
func TestCollectionDownloadRouteWinsOverGenericNicknameSlug(t *testing.T) {
	owner := int64(701)
	store := &fakeCollectionStore{
		collections: []*models.Collection{
			// Сотрудническая подборка (пустой ник) со слагом "чтец" — цель
			// первого запроса ниже через .../download.
			{
				ID: 1, Title: "Чтец", Slug: "чтец",
				PublishedAt: ptrTime(time.Now().Add(-time.Hour)),
			},
			// Читательская подборка с ником "чтец" и тем же слагом — цель
			// второго запроса, через общий маршрут /{nickname}/{slug}.
			{
				ID: 2, Title: "Подборка читателя чтец", Slug: "чтец",
				OwnerID: &owner, AuthorNickname: "чтец",
				PublishedAt: ptrTime(time.Now().Add(-time.Hour)),
			},
		},
	}

	rt := newTestRouter(t)
	rt.collectionHandler = NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)
	rt.downloadHandler = NewDownloadHandler(NewDownloadSource(nil, nil, nil, nil, store, nil, "https://example.org"))
	r := rt.Setup()

	req := httptest.NewRequest(http.MethodGet, "/api/collections/чтец/download?format=md", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200 (маршрут скачивания); тело: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type %q похож на ответ Get, а не на файл скачивания — "+
			"/download перехвачен общим /{nickname}/{slug}", ct)
	}

	// Дополнительная проверка того же порядка: подборка с ником "чтец" и
	// слагом, отличным от "download", обязана по-прежнему резолвиться через
	// общий маршрут — специфичные пути не должны перехватывать всё подряд.
	req = httptest.NewRequest(http.MethodGet, "/api/collections/чтец/чтец", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200 (общий маршрут подборки); тело: %s", rec.Code, rec.Body.String())
	}
	var got models.Collection
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("bad JSON: %v; body: %s", err, rec.Body.String())
	}
	if got.ID != 2 {
		t.Errorf("id = %d, want 2 (читательская подборка с ником \"чтец\")", got.ID)
	}
}

// TestCollectionsMineRouteNotSwallowedByGenericSlug proves the subrouter
// creation order in router.go actually holds at the mux level: reader (which
// carries /collections/mine) is created BEFORE public (which carries the
// generic /collections/{slug}) — see the comment at reader's declaration —
// so a real request for "mine" must reach CollectionHandler.Mine and not be
// parsed as Get("", "mine"). A real handler backed by fakeCollectionStore is
// wired in, same technique as the suggestions/mine counterpart in
// TestAccessMatrix, because serveWithRecover cannot tell "reached Mine" apart
// from "reached Get and panicked on nil deps" — here neither panics, so the
// two outcomes must be told apart by status/body.
func TestCollectionsMineRouteNotSwallowedByGenericSlug(t *testing.T) {
	owner := int64(1) // tokenForRole mints User{ID: 1}, see readerToken/tokenForRole.
	store := &fakeCollectionStore{
		collections: []*models.Collection{
			{ID: 30, Title: "Моя", Slug: "moya", OwnerID: &owner, AuthorNickname: "кто-то"},
		},
	}
	rt := newTestRouter(t)
	rt.collectionHandler = NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)
	r := rt.Setup()

	req := httptest.NewRequest(http.MethodGet, "/api/collections/mine", nil)
	req.Header.Set("Authorization", "Bearer "+readerToken(t))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/collections/mine с читательским токеном = %d, want 200 "+
			"(404 означало бы, что общий /collections/{slug} перехватил литеральную строку "+
			"\"mine\" как слаг и не нашёл такую подборку); тело: %s", rec.Code, rec.Body.String())
	}
	var got []*models.Collection
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("bad JSON: %v; тело: %s", err, rec.Body.String())
	}
	if len(got) != 1 || got[0].ID != 30 {
		t.Fatalf("тело = %s, ожидался список из одной подборки владельца (id=30)", rec.Body.String())
	}

	// Гость без токена — 401 от AuthMiddleware на reader, ещё до
	// CollectionHandler.Mine; ручная проверка claims внутри самого
	// обработчика остаётся страховкой на случай переезда маршрута, но здесь
	// уже не участвует.
	req = httptest.NewRequest(http.MethodGet, "/api/collections/mine", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/collections/mine без токена = %d, want 401", rec.Code)
	}
}

// Литеральный "/documents/review" не должен достаться публичному
// "/documents/{id}": подроутер staffEarly объявлен ДО public. Проверяется
// ПОВЕДЕНИЕМ маршрутизатора, а не чтением исходника.
func TestDocumentsReviewRouteNotSwallowedByGenericID(t *testing.T) {
	r := newTestRouter(t).Setup()
	req := httptest.NewRequest("GET", "/api/documents/review", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	// Без токена — 401 от стражника очереди. Перехвати путь публичный
	// /documents/{id}, ParseInt("review") дал бы 400.
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("очередь перехвачена публичным маршрутом: код %d", rr.Code)
	}
}

// Зеркало: "/documents/mine" не должен достаться тому же публичному
// шаблону. Отдельным тестом, а не строкой в предыдущем: маршруты висят на
// РАЗНЫХ подроутерах (reader и staffEarly), и один зелёный ничего не
// говорит о втором.
func TestDocumentsMineRouteNotSwallowedByGenericID(t *testing.T) {
	r := newTestRouter(t).Setup()
	req := httptest.NewRequest("GET", "/api/documents/mine", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("«моё» перехвачено публичным маршрутом: код %d", rr.Code)
	}
}
