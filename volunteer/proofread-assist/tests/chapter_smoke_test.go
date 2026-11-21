package smoke_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"proofreader/internal/api"
	"proofreader/internal/auth"
	"proofreader/internal/config"
	"proofreader/internal/metrics"
	"proofreader/internal/models"
	"proofreader/internal/pagecache"
	"proofreader/internal/repository"
	"proofreader/internal/stats"
	"proofreader/pkg/storage"
)

func TestPackagedChapterReview(t *testing.T) {
	binary := os.Getenv("PROOFREAD_ASSIST_BINARY")
	if binary == "" {
		t.Skip("set PROOFREAD_ASSIST_BINARY to run the packaged chapter smoke")
	}
	dsn := os.Getenv("PROOFREADER_TEST_DB_URL")
	if dsn == "" {
		t.Fatal("a migrated disposable PROOFREADER_TEST_DB_URL is required")
	}
	ctx := t.Context()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	one := func(sql string, args ...any) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	owner := one("INSERT INTO users(email,password_hash,role) VALUES($1,'x','reader') RETURNING id", fmt.Sprintf("review-%d@test.invalid", time.Now().UnixNano()))
	cleanup := func(sql string, id int64) {
		if _, err := pool.Exec(ctx, sql, id); err != nil {
			t.Error(err)
		}
	}
	defer cleanup("DELETE FROM users WHERE id=$1", owner)
	work := one("INSERT INTO works(title,file_path,owner_id) VALUES('Smoke','/book',$1) RETURNING id", owner)
	defer cleanup("DELETE FROM works WHERE id=$1", work)
	chapter := one("INSERT INTO chapters(work_id,title,type,order_number,start_page,end_page) VALUES($1,'Review','chapter',1,1,2) RETURNING id", work)
	for i, text := range []string{"кот кот", "без ошибок"} {
		one("INSERT INTO pages(work_id,page_number,content_markdown,preview_path) VALUES($1,$2,$3,'scan.png') RETURNING id", work, i+1, text)
	}
	scans := storage.NewMemoryStorage()
	if err := scans.Put(ctx, "scan.png", strings.NewReader("png"), 3, "image/png"); err != nil {
		t.Fatal(err)
	}
	jwt := config.JWTConfig{Secret: "chapter-smoke", ReaderExpiration: time.Hour}
	token, err := auth.NewService(&jwt).GenerateToken(&models.User{ID: owner, Role: models.RoleReader})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{JWT: jwt, S3: config.S3Config{PresignTTL: time.Minute}}
	router := api.NewServer(api.ServerDeps{Config: cfg, Pool: pool, Scans: scans, PageCache: pagecache.New(""), Stats: stats.NewRecorder(repository.NewStatsRepository(pool)), Metrics: metrics.New()})
	const modelKey = "smoke-model-credential"
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if strings.Contains(fmt.Sprint(r.URL, r.Header, string(body)), modelKey) {
			t.Error("site received model key")
			w.WriteHeader(400)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		router.ServeHTTP(w, r)
	}))
	defer site.Close()
	cmd := exec.CommandContext(ctx, "python3", "chapter_smoke.py", binary, site.URL, token, fmt.Sprint(work), fmt.Sprint(chapter))
	cmd.Env = append(os.Environ(), "PROOFREAD_ASSIST_HOME="+t.TempDir(), "OPENAI_API_KEY="+modelKey)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("chapter review: %v\n%s", err, out)
	}
	var count int
	var text, status string
	if err := pool.QueryRow(ctx, `SELECT count(*),coalesce(min(s.proposed_markdown),''),coalesce(min(s.status::text),'') FROM page_suggestions s JOIN pages p ON s.page_id=p.id WHERE p.work_id=$1`, work).Scan(&count, &text, &status); err != nil || count != 1 || text != "кит кот" || status != string(models.SuggestionNew) {
		t.Fatalf("moderation: count=%d text=%q status=%q err=%v", count, text, status, err)
	}
}
