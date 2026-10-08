package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"proofreader/internal/auth"
	"proofreader/internal/config"
	"proofreader/internal/models"
	"proofreader/internal/pagecache"
)

func createTestRouter() *Router {
	cfg := &config.JWTConfig{
		Secret:            "test-secret",
		Expiration:        time.Hour,
		RefreshExpiration: 24 * time.Hour,
		// Без этого поля тест-хелпер молча выпускает читателю токен с нулевым
		// сроком жизни (tokenTTL(RoleReader) возвращает cfg.ReaderExpiration
		// как есть) — тот истекает раньше, чем ValidateToken успевает на него
		// посмотреть, и любая проверка с читательским токеном видит не 403, а
		// 401 "истёк токен", то есть проверяет не то, что заявлено.
		ReaderExpiration: 90 * 24 * time.Hour,
	}
	authService := auth.NewService(cfg)

	// Не nil: тест границы ролей ждёт от редактора настоящих 200 на сбросе
	// главы, а с nil-обработчиком случилась бы паника, которая засчиталась
	// бы через восстановление — то есть тест проверял бы совсем не то.
	cacheHandler := NewCacheHandler(pagecache.New(""), &fakeChapterGetter{
		chapter: &models.Chapter{ID: 5, WorkID: 47, StartPage: 1, EndPage: 2},
	})
	return NewRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, cacheHandler, nil, authService)
}

func TestNewRouter(t *testing.T) {
	router := createTestRouter()
	if router == nil {
		t.Fatal("Expected router to be created")
	}
}

func TestRouterSetup(t *testing.T) {
	router := createTestRouter()
	muxRouter := router.Setup()

	if muxRouter == nil {
		t.Fatal("Expected mux router to be created")
	}
}

func TestHealthEndpoint(t *testing.T) {
	router := createTestRouter()
	muxRouter := router.Setup()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	muxRouter.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Health endpoint status = %d, want %d", rec.Code, http.StatusOK)
	}

	if rec.Body.String() != "OK" {
		t.Errorf("Health endpoint body = %s, want OK", rec.Body.String())
	}
}

func TestCORSHeaders(t *testing.T) {
	router := createTestRouter()
	muxRouter := router.Setup()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	muxRouter.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("Expected Access-Control-Allow-Origin header")
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("Expected Access-Control-Allow-Methods header")
	}
}

func TestUnauthenticatedRoutes(t *testing.T) {
	router := createTestRouter()
	muxRouter := router.Setup()

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/health"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()

			muxRouter.ServeHTTP(rec, req)

			if rec.Code == http.StatusUnauthorized {
				t.Errorf("Route %s %s should not require auth", tt.method, tt.path)
			}
		})
	}
}

func TestAuthenticatedRoutesRequireToken(t *testing.T) {
	router := createTestRouter()
	muxRouter := router.Setup()

	// /api/works, /api/categories, /api/documents (GET) are now public reads
	// (see router_access_test.go / TestAccessMatrix) - only /auth/me still
	// requires a token unconditionally.
	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/auth/me"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()

			muxRouter.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("Route %s %s status = %d, want %d (should require auth)",
					tt.method, tt.path, rec.Code, http.StatusUnauthorized)
			}
		})
	}
}
