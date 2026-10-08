package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"proofreader/internal/auth"
	"proofreader/internal/config"
	"proofreader/internal/models"
)

func createTestAuthService() *auth.Service {
	cfg := &config.JWTConfig{
		Secret:            "test-secret-key",
		Expiration:        time.Hour,
		RefreshExpiration: 24 * time.Hour,
	}
	return auth.NewService(cfg)
}

func TestCORS(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := CORS(handler)

	t.Run("regular request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Error("Expected Access-Control-Allow-Origin header")
		}
		if rec.Header().Get("Access-Control-Allow-Methods") == "" {
			t.Error("Expected Access-Control-Allow-Methods header")
		}
		if rec.Header().Get("Access-Control-Allow-Headers") == "" {
			t.Error("Expected Access-Control-Allow-Headers header")
		}
		if rec.Code != http.StatusOK {
			t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("OPTIONS request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/test", nil)
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Errorf("Status = %d, want %d", rec.Code, http.StatusNoContent)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Error("Expected Access-Control-Allow-Origin header")
		}
	})
}

func TestLogging(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	wrapped := Logging(handler)

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusCreated)
	}
}

func TestResponseWriter(t *testing.T) {
	t.Run("default status", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rw := &responseWriter{
			ResponseWriter: rec,
			statusCode:     http.StatusOK,
		}

		rw.Write([]byte("test"))

		if rw.statusCode != http.StatusOK {
			t.Errorf("statusCode = %d, want %d", rw.statusCode, http.StatusOK)
		}
	})

	t.Run("custom status", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rw := &responseWriter{
			ResponseWriter: rec,
			statusCode:     http.StatusOK,
		}

		rw.WriteHeader(http.StatusNotFound)

		if rw.statusCode != http.StatusNotFound {
			t.Errorf("statusCode = %d, want %d", rw.statusCode, http.StatusNotFound)
		}
	})
}

func TestAuthMiddleware(t *testing.T) {
	authService := createTestAuthService()

	user := &models.User{
		ID:    1,
		Email: "test@example.com",
		Role:  models.RoleEditor,
	}

	token, _ := authService.GenerateToken(user)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(UserContextKey).(*auth.Claims)
		if !ok {
			t.Error("Expected claims in context")
			return
		}
		if claims.UserID != user.ID {
			t.Errorf("UserID = %d, want %d", claims.UserID, user.ID)
		}
		w.WriteHeader(http.StatusOK)
	})

	wrapped := AuthMiddleware(authService)(handler)

	t.Run("valid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("missing authorization header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("invalid authorization format", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "InvalidFormat")
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer invalid.token.here")
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("wrong prefix", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Basic "+token)
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})
}

func TestRequireRole(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name         string
		userRole     models.UserRole
		allowedRoles []models.UserRole
		expectedCode int
	}{
		{
			name:         "admin allowed for admin-only",
			userRole:     models.RoleAdministrator,
			allowedRoles: []models.UserRole{models.RoleAdministrator},
			expectedCode: http.StatusOK,
		},
		{
			name:         "editor allowed for editor-only",
			userRole:     models.RoleEditor,
			allowedRoles: []models.UserRole{models.RoleEditor},
			expectedCode: http.StatusOK,
		},
		{
			name:         "editor not allowed for admin-only",
			userRole:     models.RoleEditor,
			allowedRoles: []models.UserRole{models.RoleAdministrator},
			expectedCode: http.StatusForbidden,
		},
		{
			name:         "admin allowed for editor+admin",
			userRole:     models.RoleAdministrator,
			allowedRoles: []models.UserRole{models.RoleEditor, models.RoleAdministrator},
			expectedCode: http.StatusOK,
		},
		{
			name:         "editor allowed for editor+admin",
			userRole:     models.RoleEditor,
			allowedRoles: []models.UserRole{models.RoleEditor, models.RoleAdministrator},
			expectedCode: http.StatusOK,
		},
		{
			name:         "unknown role not allowed for editor+admin",
			userRole:     models.UserRole("viewer"),
			allowedRoles: []models.UserRole{models.RoleEditor, models.RoleAdministrator},
			expectedCode: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := &auth.Claims{
				UserID: 1,
				Email:  "test@example.com",
				Role:   tt.userRole,
			}

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			ctx := context.WithValue(req.Context(), UserContextKey, claims)
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()

			wrapped := RequireRole(tt.allowedRoles...)(handler)
			wrapped.ServeHTTP(rec, req)

			if rec.Code != tt.expectedCode {
				t.Errorf("Status = %d, want %d", rec.Code, tt.expectedCode)
			}
		})
	}

	t.Run("no user in context", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		wrapped := RequireRole(models.RoleAdministrator)(handler)
		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})
}

func TestOptionalAuth(t *testing.T) {
	authService := createTestAuthService()

	user := &models.User{
		ID:    1,
		Email: "test@example.com",
		Role:  models.RoleEditor,
	}

	token, _ := authService.GenerateToken(user)

	t.Run("with valid token", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(UserContextKey).(*auth.Claims)
			if !ok || claims == nil {
				t.Error("Expected claims in context with valid token")
				return
			}
			if claims.UserID != user.ID {
				t.Errorf("UserID = %d, want %d", claims.UserID, user.ID)
			}
			w.WriteHeader(http.StatusOK)
		})

		wrapped := OptionalAuth(authService)(handler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("without token", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := r.Context().Value(UserContextKey)
			if claims != nil {
				t.Error("Expected no claims in context without token")
			}
			w.WriteHeader(http.StatusOK)
		})

		wrapped := OptionalAuth(authService)(handler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("with invalid token", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := r.Context().Value(UserContextKey)
			if claims != nil {
				t.Error("Expected no claims in context with invalid token")
			}
			w.WriteHeader(http.StatusOK)
		})

		wrapped := OptionalAuth(authService)(handler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer invalid.token")
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
		}
	})
}

func TestGetUserFromContext(t *testing.T) {
	t.Run("with claims", func(t *testing.T) {
		claims := &auth.Claims{
			UserID: 123,
			Email:  "user@test.com",
			Role:   models.RoleEditor,
		}

		ctx := context.WithValue(context.Background(), UserContextKey, claims)

		got, ok := GetUserFromContext(ctx)
		if !ok {
			t.Error("Expected to get user from context")
		}
		if got.UserID != claims.UserID {
			t.Errorf("UserID = %d, want %d", got.UserID, claims.UserID)
		}
		if got.Email != claims.Email {
			t.Errorf("Email = %s, want %s", got.Email, claims.Email)
		}
	})

	t.Run("without claims", func(t *testing.T) {
		ctx := context.Background()

		_, ok := GetUserFromContext(ctx)
		if ok {
			t.Error("Expected not to get user from empty context")
		}
	})

	t.Run("with wrong type", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), UserContextKey, "wrong type")

		_, ok := GetUserFromContext(ctx)
		if ok {
			t.Error("Expected not to get user when wrong type in context")
		}
	})
}

func TestResponseWriterUnwrap(t *testing.T) {
	rec := httptest.NewRecorder()
	wrapped := &responseWriter{ResponseWriter: rec, statusCode: http.StatusOK}

	unwrapper, ok := interface{}(wrapped).(interface{ Unwrap() http.ResponseWriter })
	if !ok {
		t.Fatal("responseWriter must expose Unwrap() so http.ResponseController can reach the real writer")
	}
	if unwrapper.Unwrap() != http.ResponseWriter(rec) {
		t.Error("Unwrap() must return the wrapped writer")
	}
}

// deadlineWriter is a minimal http.ResponseWriter that also implements the
// SetWriteDeadline/SetReadDeadline interfaces http.ResponseController looks
// for. httptest.ResponseRecorder implements neither, so it can't be used to
// tell whether a deadline call actually reached the underlying connection —
// only that Unwrap() exists and returns something.
type deadlineWriter struct {
	http.ResponseWriter
	writeDeadlines []time.Time
}

func (w *deadlineWriter) SetWriteDeadline(t time.Time) error {
	w.writeDeadlines = append(w.writeDeadlines, t)
	return nil
}

func (w *deadlineWriter) SetReadDeadline(t time.Time) error {
	return nil
}

// TestLoggingSetWriteDeadlineReachesUnderlyingWriter is the regression test
// for the actual failure the Unwrap() fix addresses: a handler running behind
// middleware.Logging clearing its write deadline via http.ResponseController
// must reach the real connection, or a large export silently keeps the
// server's 15s WriteTimeout and gets cut off mid-stream.
func TestLoggingSetWriteDeadlineReachesUnderlyingWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	dw := &deadlineWriter{ResponseWriter: rec}

	handler := Logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := http.NewResponseController(w).SetWriteDeadline(time.Time{}); err != nil {
			t.Errorf("SetWriteDeadline through Logging middleware: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/export", nil)
	handler.ServeHTTP(dw, req)

	if len(dw.writeDeadlines) != 1 {
		t.Fatalf("SetWriteDeadline calls reaching the underlying writer = %d, want 1", len(dw.writeDeadlines))
	}
	if !dw.writeDeadlines[0].IsZero() {
		t.Errorf("write deadline reaching the underlying writer = %v, want zero (cleared)", dw.writeDeadlines[0])
	}
}

// Дедлайн записи обязан доезжать до соединения через ВСЮ цепочку стражников,
// а не через один Logging.
//
// Боевой порядок (router.go): CORS → Logging → Gzip, и Gzip стоит ближе всех
// к обработчику, — то есть при «Accept-Encoding: gzip» обработчику достаётся
// *gzipResponseWriter, а Unwrap у Logging остаётся снаружи и до него дело не
// доходит. Заголовок этот шлёт каждый клиент ввоза: requests ставит «gzip,
// deflate» сам, а tools/ocr_ingest/proofreader_client.py и есть единственный
// клиент ввоза указателя. Без Unwrap у gzipResponseWriter контроллер отвечает
// ErrNotSupported, снятие дедлайна тихо превращается в строку в журнале, и
// ленинский ввоз остаётся под 15-секундным WriteTimeout, от которого его
// спасали. Тест соседа выше этого не видит: он собирает цепочку из одного
// стражника, то есть стоит НИЖЕ шва, на котором всё ломается.
func TestGzipSetWriteDeadlineReachesUnderlyingWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	dw := &deadlineWriter{ResponseWriter: rec}

	handler := Logging(Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := http.NewResponseController(w).SetWriteDeadline(time.Time{}); err != nil {
			t.Errorf("SetWriteDeadline через цепочку Logging→Gzip: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest(http.MethodPost, "/api/editions/9/index/import", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	handler.ServeHTTP(dw, req)

	if len(dw.writeDeadlines) != 1 {
		t.Fatalf("вызовов SetWriteDeadline, доехавших до соединения = %d, want 1", len(dw.writeDeadlines))
	}
	if !dw.writeDeadlines[0].IsZero() {
		t.Errorf("до соединения доехал дедлайн %v, хотели нулевой (снятый)", dw.writeDeadlines[0])
	}
}
