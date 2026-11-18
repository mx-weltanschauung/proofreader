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
		chapterPages := sweepDo(t, router, fmt.Sprintf("/api/works/%d/chapters/%d/pages", workID, chapterID), token)
		if chapterPages.code != 200 || !strings.Contains(chapterPages.text, "ТЕКСТ-ДОСТУПЕН") || strings.Contains(chapterPages.text, "secret.png") {
			t.Errorf("%s chapter reading: %d %s", role, chapterPages.code, chapterPages.text)
		}
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

// The same public page and work routes must respect every parent level. Full
// closure can be switched independently without clearing the scan restriction.
func TestScansHiddenSweepParentsAndFullClosure(t *testing.T) {
	pool := repository.SharedTestPool(t)
	router, authService, _ := closedSweepRouter(t, pool)
	ctx := context.Background()
	var editionID, journalID, volumeID, issueID int64
	for _, item := range []struct {
		query string
		id    *int64
	}{
		{`INSERT INTO editions (title, slug) VALUES ('Сканы: издание', 'scans-sweep-edition') RETURNING id`, &editionID},
		{`INSERT INTO journals (slug, title) VALUES ('scans-sweep-journal', 'Сканы: журнал') RETURNING id`, &journalID},
	} {
		if err := pool.QueryRow(ctx, item.query).Scan(item.id); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `INSERT INTO works (title, author, language, country, file_path, owner_id, edition_id, volume_number)
		VALUES ('Том со сканами', 'Автор', 'ru', 'ru', 'works/scans-sweep/volume.pdf', 1, $1, 1) RETURNING id`, editionID).Scan(&volumeID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO works (title, author, language, country, file_path, owner_id, role)
		VALUES ('Номер со сканами', 'Автор', 'ru', 'ru', 'works/scans-sweep/issue.pdf', 1, 'journal_issue') RETURNING id`).Scan(&issueID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO journal_issues (journal_id, year, number_from, number_to, label, work_id)
		VALUES ($1, 1928, 1, 1, '1', $2)`, journalID, issueID); err != nil {
		t.Fatal(err)
	}
	for _, work := range []int64{volumeID, issueID} {
		if _, err := pool.Exec(ctx, `INSERT INTO pages (work_id, page_number, content_markdown, preview_path)
			VALUES ($1, 1, 'ТЕКСТ-СКВОЗНОЙ', 'works/scans-sweep/secret.png')`, work); err != nil {
			t.Fatal(err)
		}
	}
	admin, err := authService.GenerateToken(&models.User{ID: 1, Role: models.RoleAdministrator, Email: "admin@example.org"})
	if err != nil {
		t.Fatal(err)
	}
	editor, err := authService.GenerateToken(&models.User{ID: 1, Role: models.RoleEditor, Email: "editor@example.org"})
	if err != nil {
		t.Fatal(err)
	}
	set := func(path string, hidden bool, field string) {
		t.Helper()
		body := fmt.Sprintf(`{"%s":%t,"reason":"проверка сканов"}`, field, hidden)
		res := sweepSend(t, router, "PUT", path, admin, body)
		if res.code != 200 {
			t.Fatalf("PUT %s: %d %s", path, res.code, res.text)
		}
	}
	check := func(work int64, token string, wantScan, wantText bool) {
		t.Helper()
		page := sweepDo(t, router, fmt.Sprintf("/api/works/%d/pages/by-number/1", work), token)
		if wantText {
			if page.code != 200 || !strings.Contains(page.text, "ТЕКСТ-СКВОЗНОЙ") {
				t.Errorf("work %d: text missing: %d %s", work, page.code, page.text)
			}
		} else if page.code != 404 {
			t.Errorf("work %d: expected closed page: %d %s", work, page.code, page.text)
		}
		if got := strings.Contains(page.text, "secret.png"); got != wantScan {
			t.Errorf("work %d: scan visibility = %t, want %t: %s", work, got, wantScan, page.text)
		}
		original := sweepDo(t, router, fmt.Sprintf("/api/works/%d", work), token)
		if got := strings.Contains(original.text, ".pdf"); got != wantScan {
			t.Errorf("work %d: original visibility = %t, want %t: %s", work, got, wantScan, original.text)
		}
	}
	volumeScans := fmt.Sprintf("/api/works/%d/scans-hidden", volumeID)
	editionScans := fmt.Sprintf("/api/editions/%d/scans-hidden", editionID)
	journalScans := fmt.Sprintf("/api/journals/%d/scans-hidden", journalID)
	issueScans := fmt.Sprintf("/api/works/%d/scans-hidden", issueID)
	for _, item := range []struct {
		path string
		work int64
	}{
		{volumeScans, volumeID}, {editionScans, volumeID},
		{journalScans, issueID}, {issueScans, issueID},
	} {
		check(item.work, "", true, true)
		set(item.path, true, "scans_hidden")
		check(item.work, "", false, true)
		check(item.work, editor, false, true)
		check(item.work, admin, true, true)
		set(item.path, false, "scans_hidden")
		check(item.work, "", true, true)
	}
	set(volumeScans, true, "scans_hidden")
	full := fmt.Sprintf("/api/works/%d/closed", volumeID)
	set(full, true, "closed")
	check(volumeID, "", false, false)
	check(volumeID, editor, false, true)
	set(full, false, "closed")
	check(volumeID, "", false, true)
	set(volumeScans, false, "scans_hidden")
	check(volumeID, "", true, true)

	audit := sweepDo(t, router, "/api/admin/closures", admin)
	if audit.code != 200 || !strings.Contains(audit.text, `"scope":"scans"`) || !strings.Contains(audit.text, `"scope":"full"`) {
		t.Errorf("audit does not distinguish restrictions: %d %s", audit.code, audit.text)
	}
}
