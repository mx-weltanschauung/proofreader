package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/auth"
	"proofreader/internal/config"
	"proofreader/internal/metrics"
	"proofreader/internal/models"
	"proofreader/internal/pagecache"
	"proofreader/internal/repository"
	"proofreader/internal/stats"
)

func TestSettingsNewServerChangesLiveSuggestionLimit(t *testing.T) {
	dsn := os.Getenv("PROOFREADER_SETTINGS_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set PROOFREADER_SETTINGS_TEST_DB_URL to a separate throwaway DB")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `UPDATE site_settings SET suggestions_per_hour = 5 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(ctx, `UPDATE site_settings SET suggestions_per_hour = 5 WHERE id = 1`); err != nil {
			t.Errorf("restore suggestion limit: %v", err)
		}
	}()

	email := fmt.Sprintf("suggestion-limit-%d@test.local", time.Now().UnixNano())
	var userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, role) VALUES ($1, 'x', 'reader') RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Errorf("delete test user: %v", err)
		}
	}()
	work := &models.Work{Title: "Проверка предела", Author: "Проба", Language: "ru", Country: "ru", Status: models.WorkStatusDraft, Role: models.WorkRoleVolume, NumberingStyle: models.NumberingArabic, OwnerID: userID}
	if err := repository.NewWorkRepository(pool).Create(ctx, work, models.ClosureActor{}, ""); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(ctx, `DELETE FROM works WHERE id = $1`, work.ID); err != nil {
			t.Errorf("delete test work: %v", err)
		}
	}()
	page := &models.Page{WorkID: work.ID, PageNumber: 1, ContentMarkdown: testPageText, Status: models.PageStatusNotProofread}
	if err := repository.NewPageRepository(pool).Create(ctx, page); err != nil {
		t.Fatal(err)
	}

	jwt := config.JWTConfig{Secret: "settings-integration", Expiration: time.Hour, ReaderExpiration: time.Hour}
	cfg := &config.Config{JWT: jwt, Server: config.ServerConfig{PublicBaseURL: "http://localhost"}, S3: config.S3Config{PresignTTL: time.Minute}}
	router := NewServer(ServerDeps{Config: cfg, Pool: pool, PageCache: pagecache.New(""), Stats: stats.NewRecorder(repository.NewStatsRepository(pool)), Metrics: metrics.New()})
	authService := auth.NewService(&jwt)
	adminToken, err := authService.GenerateToken(&models.User{ID: userID, Role: models.RoleAdministrator})
	if err != nil {
		t.Fatal(err)
	}
	readerToken, err := authService.GenerateToken(&models.User{ID: userID, Role: models.RoleReader})
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, token string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	if rec := request(http.MethodGet, "/api/admin/settings", adminToken, nil); rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"suggestions_per_hour":5`)) {
		t.Fatalf("initial GET = %d %s", rec.Code, rec.Body.String())
	}
	patch := func(value int) {
		t.Helper()
		rec := request(http.MethodPatch, "/api/admin/settings", adminToken, map[string]int{"suggestions_per_hour": value})
		if rec.Code != http.StatusOK {
			t.Fatalf("PATCH %d = %d %s", value, rec.Code, rec.Body.String())
		}
	}
	submit := func() int {
		t.Helper()
		rec := request(http.MethodPost, fmt.Sprintf("/api/works/%d/pages/%d/suggestions", work.ID, page.ID), readerToken, validBody())
		return rec.Code
	}
	patch(1)
	if got := submit(); got != http.StatusCreated {
		t.Fatalf("first submission = %d", got)
	}
	if got := submit(); got != http.StatusTooManyRequests {
		t.Fatalf("limit 1 = %d", got)
	}
	patch(2)
	if got := submit(); got != http.StatusCreated {
		t.Fatalf("limit 2 = %d", got)
	}
	if got := submit(); got != http.StatusTooManyRequests {
		t.Fatalf("third submission = %d", got)
	}
}
