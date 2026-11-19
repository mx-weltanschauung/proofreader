package repository_test

import (
	"context"
	"errors"
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

func TestNewWorkInHiddenEditionInheritsScansAtCreation(t *testing.T) {
	pool := repository.SharedTestPool(t)
	router, authService, _ := closedSweepRouter(t, pool)
	ctx := context.Background()
	var editionID int64
	if err := pool.QueryRow(ctx, `INSERT INTO editions (title, slug, scans_hidden)
		VALUES ('Скрытое издание', 'hidden-at-create', true) RETURNING id`).Scan(&editionID); err != nil {
		t.Fatal(err)
	}
	editor, err := authService.GenerateToken(&models.User{ID: 1, Role: models.RoleEditor, Email: "editor@example.org"})
	if err != nil {
		t.Fatal(err)
	}
	created := sweepSend(t, router, "POST", "/api/works", editor,
		fmt.Sprintf(`{"title":"Новый том","status":"draft","edition_id":%d,"volume_number":1,"require_inherited_scans":true}`, editionID))
	if created.code != 201 || !strings.Contains(created.text, `"scans_hidden":false`) {
		t.Fatalf("новый том должен создаться без собственного флага: %d %s", created.code, created.text)
	}
	var workID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM works WHERE title = 'Новый том'`).Scan(&workID); err != nil {
		t.Fatal(err)
	}
	if work := sweepDo(t, router, fmt.Sprintf("/api/works/%d", workID), editor); work.code != 200 || !strings.Contains(work.text, `"scans_hidden_by":"edition"`) {
		t.Fatalf("созданный том не наследует скрытие: %d %s", work.code, work.text)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO pages (work_id, page_number, content_markdown, preview_path)
		VALUES ($1, 1, 'Открытый текст', 'works/hidden-at-create/page.png')`, workID); err != nil {
		t.Fatal(err)
	}
	pagePath := fmt.Sprintf("/api/works/%d/pages/by-number/1", workID)
	if page := sweepDo(t, router, pagePath, ""); page.code != 200 || !strings.Contains(page.text, "Открытый текст") || strings.Contains(page.text, "page.png") {
		t.Fatalf("созданный том раскрыл скан: %d %s", page.code, page.text)
	}
	if _, err := pool.Exec(ctx, `UPDATE editions SET scans_hidden = false WHERE id = $1`, editionID); err != nil {
		t.Fatal(err)
	}
	if page := sweepDo(t, router, pagePath, ""); page.code != 200 || !strings.Contains(page.text, "page.png") {
		t.Fatalf("после открытия издания скан должен открыться: %d %s", page.code, page.text)
	}
	rejected := sweepSend(t, router, "POST", "/api/works", editor,
		fmt.Sprintf(`{"title":"Недопустимый том","status":"draft","edition_id":%d,"volume_number":2,"require_inherited_scans":true}`, editionID))
	if rejected.code != 409 {
		t.Fatalf("создание без ожидаемого наследования должно быть отклонено: %d %s", rejected.code, rejected.text)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM works WHERE title = 'Недопустимый том'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("отклонённая работа осталась в базе: count=%d err=%v", count, err)
	}
}

func TestScansHiddenStructuralRecheckAfterStaleRead(t *testing.T) {
	pool := repository.SharedTestPool(t)
	ctx := context.Background()
	var editionID, workID, chapterID, parentID int64
	if err := pool.QueryRow(ctx, `INSERT INTO editions (title, slug) VALUES ('Ограниченное издание', 'scans-structural-stale') RETURNING id`).Scan(&editionID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO works (title, author, language, country, file_path, owner_id, edition_id)
		VALUES ('Том', 'Автор', 'ru', 'ru', 'works/stale.pdf', 1, $1) RETURNING id`, editionID).Scan(&workID); err != nil {
		t.Fatal(err)
	}
	works := repository.NewWorkRepository(pool)
	chapters := repository.NewChapterRepository(pool)
	closed := repository.NewClosedRepository(pool)
	staleWork, err := works.GetByID(ctx, workID)
	if err != nil {
		t.Fatal(err)
	}
	actor := models.ClosureActor{Name: "администратор"}
	if _, err := closed.SetEditionScansHidden(ctx, editionID, true, actor, "письмо"); err != nil {
		t.Fatal(err)
	}
	staleWork.EditionID = nil
	if err := works.Update(ctx, staleWork); !errors.Is(err, repository.ErrScansHiddenStructure) {
		t.Fatalf("stale work move: %v", err)
	}
	if _, err := closed.SetWorkScansHidden(ctx, workID, true, actor, "сохранить ограничение"); err != nil {
		t.Fatal(err)
	}
	if err := works.Update(ctx, staleWork); err != nil {
		t.Fatalf("own hidden work can move without exposing scans: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO chapters (work_id, title, type, order_number, start_page, end_page)
		VALUES ($1, 'Скрытая глава', 'chapter', 1, 1, 2) RETURNING id`, workID).Scan(&chapterID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO chapters (work_id, title, type, order_number, start_page, end_page)
		VALUES ($1, 'Родитель', 'chapter', 2, 3, 4) RETURNING id`, workID).Scan(&parentID); err != nil {
		t.Fatal(err)
	}
	staleChapter, err := chapters.GetByID(ctx, chapterID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := closed.SetChapterScansHidden(ctx, chapterID, true, actor, "письмо"); err != nil {
		t.Fatal(err)
	}
	staleChapter.EndPage = 3
	if err := chapters.Update(ctx, staleChapter); !errors.Is(err, repository.ErrScansHiddenStructure) {
		t.Fatalf("stale chapter range: %v", err)
	}
	if err := chapters.Move(ctx, chapterID, &parentID, 1); !errors.Is(err, repository.ErrScansHiddenStructure) {
		t.Fatalf("stale chapter move: %v", err)
	}
	if err := chapters.Delete(ctx, chapterID); !errors.Is(err, repository.ErrScansHiddenStructure) {
		t.Fatalf("stale chapter delete: %v", err)
	}
}

// The same public page and work routes must respect every parent level. Full
// closure can be switched independently without clearing the scan restriction.
func TestScansHiddenSweepParentsAndFullClosure(t *testing.T) {
	pool := repository.SharedTestPool(t)
	router, authService, _ := closedSweepRouter(t, pool)
	ctx := context.Background()
	var editionID, journalID, volumeID, issueID, issueChildID, issueGrandchildID int64
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
	newChild := func(parent int64) int64 {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO works (title, author, language, country, file_path, owner_id, role, parent_work_id)
			VALUES ('Служебная работа со сканами', 'Автор', 'ru', 'ru', 'works/scans-sweep/child.pdf', 1, 'front_matter', $1) RETURNING id`, parent).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	issueChildID = newChild(issueID)
	issueGrandchildID = newChild(issueChildID)
	for _, work := range []int64{volumeID, issueID, issueChildID, issueGrandchildID} {
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
		if item.path == editionScans {
			deletePath := fmt.Sprintf("/api/editions/%d", editionID)
			res := sweepSend(t, router, "DELETE", deletePath, editor, "")
			if res.code != 409 {
				t.Errorf("editor removed scan-restricted edition: %d %s", res.code, res.text)
			}
			for _, listPath := range []string{fmt.Sprintf("/api/editions/%d/works", editionID), "/api/shelf"} {
				list := sweepDo(t, router, listPath, "")
				if list.code != 200 || strings.Contains(list.text, "volume.pdf") || !strings.Contains(list.text, `"scans_hidden_by":"edition"`) {
					t.Errorf("restricted edition summary leaked or lost flag: %s: %d %s", listPath, list.code, list.text)
				}
			}
		}
		check(item.work, "", false, true)
		check(item.work, editor, false, true)
		check(item.work, admin, true, true)
		if item.path == journalScans {
			check(issueChildID, "", false, true)
			check(issueGrandchildID, "", false, true)
		}
		set(item.path, false, "scans_hidden")
		check(item.work, "", true, true)
		if item.path == journalScans {
			check(issueChildID, "", true, true)
			check(issueGrandchildID, "", true, true)
		}
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
