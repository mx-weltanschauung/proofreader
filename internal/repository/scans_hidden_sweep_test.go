package repository_test

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"testing"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

// TestScansHiddenSweep uses the production NewServer wiring and a disposable DB.
// The shared page belongs to both an open and a restricted chapter.
func TestScansHiddenSweep(t *testing.T) {
	pool := repository.SharedTestPool(t)
	router, authService, _ := closedSweepRouter(t, pool)
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	ctx := context.Background()
	var workID, pageID, chapterID int64
	if err := pool.QueryRow(ctx, `INSERT INTO works (title, author, language, country, file_path, owner_id)
		VALUES ('Открытый текст', 'Автор', 'ru', 'ru', 'works/1001/original/secret.pdf', 1) RETURNING id`).Scan(&workID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO pages (work_id, page_number, content_markdown, preview_path)
		VALUES ($1, 2, 'ТЕКСТ-ДОСТУПЕН', $2) RETURNING id`, workID, fmt.Sprintf("works/%d/pages/secret.png", workID)).Scan(&pageID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO chapters (work_id, title, type, order_number, start_page, end_page)
		VALUES ($1, 'Соседка', 'chapter', 1, 1, 2) RETURNING id`, workID).Scan(new(int64)); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO chapters (work_id, title, type, order_number, start_page, end_page)
		VALUES ($1, 'Скрытая речь', 'chapter', 2, 2, 2) RETURNING id`, workID).Scan(&chapterID); err != nil {
		t.Fatal(err)
	}
	admin, err := authService.GenerateToken(&models.User{ID: 1, Role: models.RoleAdministrator, Email: "admin@example.org"})
	if err != nil {
		t.Fatal(err)
	}
	editor, err := authService.GenerateToken(&models.User{ID: 1, Role: models.RoleEditor, Email: "editor@example.org"})
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/works/%d/chapters/%d/scans-hidden", workID, chapterID)
	if res := sweepSend(t, router, "PUT", path, admin, `{"scans_hidden":true,"reason":"письмо"}`); res.code != 200 {
		t.Fatalf("set scans: %d %s", res.code, res.text)
	}
	for role, token := range map[string]string{"anonymous": "", "editor": editor, "administrator": admin} {
		for _, p := range []string{
			fmt.Sprintf("/api/works/%d/pages/%d", workID, pageID),
			fmt.Sprintf("/api/works/%d/pages/by-number/2", workID),
			fmt.Sprintf("/api/works/%d/pages", workID),
		} {
			res := sweepDo(t, router, p, token)
			if res.code != 200 || !strings.Contains(res.text, "ТЕКСТ-ДОСТУПЕН") {
				t.Errorf("%s %s: text missing: %d %s", role, p, res.code, res.text)
			}
			containsScan := strings.Contains(res.text, "secret.png")
			if containsScan != (role == "administrator") {
				t.Errorf("%s %s: scan visibility wrong: %s", role, p, res.text)
			}
		}
		res := sweepDo(t, router, fmt.Sprintf("/api/works/%d", workID), token)
		containsOriginal := strings.Contains(res.text, "secret.pdf")
		if containsOriginal != (role == "administrator") {
			t.Errorf("%s original visibility wrong: %s", role, res.text)
		}
	}
	if res := sweepSend(t, router, "PUT", path, admin, `{"scans_hidden":false,"reason":"открыть"}`); res.code != 200 {
		t.Fatalf("open scans: %d %s", res.code, res.text)
	}
	if res := sweepDo(t, router, fmt.Sprintf("/api/works/%d/pages/%d", workID, pageID), editor); !strings.Contains(res.text, "secret.png") {
		t.Errorf("scan did not reopen: %s", res.text)
	}
}
