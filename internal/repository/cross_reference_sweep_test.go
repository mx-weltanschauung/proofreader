package repository_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"proofreader/internal/auth"
	"proofreader/internal/config"
	"proofreader/internal/models"
	"proofreader/internal/repository"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestVerifiedCrossReferences(t *testing.T) {
	pool := repository.SharedTestPool(t)
	var f *closedSweep
	router, _, _ := closedSweepRouter(t, pool, func(cfg *config.Config, authService *auth.Service) {
		f = seedClosedSweep(t, pool, authService)
		cfg.Server.CrossReferenceEditionID = f.openEd
	})
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	one := func(sql string, args ...any) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	const source = "См. т. 3, с. 47; см. т. 3, с. 48; см. т. 3, с. 50. Сноска[^s1].\n\n[^s1]: См. Сочинения, 4 изд., том 3, стр. 47."
	exec(`UPDATE editions SET title = 'Собрание, 5-е изд.' WHERE id = $1`, f.openEd)
	exec(`UPDATE pages SET content_markdown = $2 WHERE id = $1`, f.pages[f.open], source)
	exec(`UPDATE works SET page_offset = 45 WHERE id = $1`, f.ch.work)
	nextWork := func(edition int64, part string) int64 {
		return one(`INSERT INTO works (title, author, file_path, owner_id, edition_id, volume_number, volume_part, page_offset)
			VALUES ('Другой том', 'Автор', 'original.pdf', 1, $1, 3, $2, 40) RETURNING id`, edition, part)
	}
	b := nextWork(f.openEd, "2")
	otherEdition := nextWork(f.closedEd, "")
	exec(`INSERT INTO pages (work_id, page_number, content_markdown) VALUES ($1, 7, 'Часть 2'), ($2, 7, 'Другое издание')`, b, otherEdition)

	queue := fmt.Sprintf("/api/editions/%d/works/%d/cross-references", f.openEd, f.open)
	pageRender := fmt.Sprintf("/api/works/%d/pages/%d/render", f.open, f.pages[f.open])
	chapterPages := fmt.Sprintf("/api/works/%d/chapters/%d/pages", f.open, f.chapters[f.open])
	window := fmt.Sprintf("/api/works/%d/reading", f.open)
	// Прогретый ответ главы без ссылок. Подтверждение должно сработать сразу.
	if res := sweepDo(t, router, chapterPages, f.readerTok); res.code != 200 || strings.Contains(res.text, "bibliographic-reference") {
		t.Fatalf("prime chapter cache: %+v", res)
	}
	scan := func(wantAdded string) {
		t.Helper()
		res := sweepSend(t, router, http.MethodPost, queue+"/scan", f.editorTok, "")
		if res.code != 200 || !strings.Contains(res.text, `"detected":4`) || !strings.Contains(res.text, `"added":`+wantAdded) {
			t.Fatalf("scan: %+v", res)
		}
	}
	list := func(status string) []models.CrossReferenceCandidate {
		t.Helper()
		res := sweepSend(t, router, http.MethodGet, queue+"?status="+status, f.editorTok, "")
		var data struct {
			Items []models.CrossReferenceCandidate
		}
		if res.code != 200 || json.Unmarshal([]byte(res.text), &data) != nil {
			t.Fatalf("list: %+v", res)
		}
		return data.Items
	}
	decide := func(c models.CrossReferenceCandidate, body string, status int) {
		t.Helper()
		res := sweepSend(t, router, http.MethodPatch, queue+"/"+itoa(c.ID), f.editorTok, body)
		if res.code != status {
			t.Fatalf("decision for %d: %+v", c.ID, res)
		}
	}
	scan("4")
	if res := sweepDo(t, router, queue, f.readerTok); res.code != http.StatusForbidden {
		t.Fatalf("reader accessed staff queue: %+v", res)
	}
	otherQueue := fmt.Sprintf("/api/editions/%d/works/%d/cross-references", f.closedEd, otherEdition)
	if res := sweepSend(t, router, http.MethodPost, otherQueue+"/scan", f.editorTok, ""); res.code != http.StatusNotFound {
		t.Fatalf("scanned an inactive edition: %+v", res)
	}
	editionDetail := fmt.Sprintf("/api/editions/%d", f.openEd)
	if res := sweepDo(t, router, editionDetail, f.editorTok); !strings.Contains(res.text, `"cross_references_enabled":true`) {
		t.Fatalf("active edition does not advertise its queue: %+v", res)
	}
	items := list("pending")
	if len(items) != 4 {
		t.Fatalf("four candidates expected: %+v", items)
	}
	ambiguous, rejected, missing, mismatch := items[0], items[1], items[2], items[3]
	if ambiguous.Issue != "ambiguous" || len(ambiguous.Targets) != 2 ||
		ambiguous.Targets[0].PageID != f.ch.pages[2] || ambiguous.Targets[0].PrintedPage != 47 ||
		missing.Issue != "missing" || len(missing.Targets) != 0 ||
		mismatch.Issue != "edition_mismatch" || len(mismatch.Targets) != 0 {
		t.Fatalf("wrong resolution: %+v", items)
	}
	confirm := func(id int64) string {
		return fmt.Sprintf(`{"decision":"confirmed","target_page_id":%d}`, id)
	}
	decide(ambiguous, confirm(one("SELECT id FROM pages WHERE work_id = $1", otherEdition)), 409)
	decide(mismatch, confirm(f.ch.pages[2]), 409)
	decide(ambiguous, confirm(f.ch.pages[2]), 204)
	decide(rejected, `{"decision":"rejected"}`, 204)
	scan("0")
	if len(list("pending")) != 2 || len(list("confirmed")) != 1 || len(list("rejected")) != 1 {
		t.Fatal("scan changed reviewed candidates")
	}
	linkPath := "/api/cross-references/" + itoa(ambiguous.ID) + "/open"
	checkLink := func(visible bool) {
		t.Helper()
		for _, path := range []string{pageRender, chapterPages, window} {
			res := sweepDo(t, router, path, f.readerTok)
			if res.code != 200 || strings.Contains(res.text, linkPath) != visible ||
				strings.Count(res.text, "bibliographic-reference") > 1 ||
				(!visible && strings.Contains(res.text, "bibliographic-reference")) {
				t.Fatalf("%s: visible=%t: %+v", path, visible, res)
			}
		}
		res := sweepDo(t, router, linkPath, f.readerTok)
		if visible {
			want := fmt.Sprintf("/works/%d/pages/2?verified_page_id=%d", f.ch.work, f.ch.pages[2])
			if res.code != http.StatusFound || res.loc != want {
				t.Fatalf("redirect: %+v; want %s", res, want)
			}
		} else if res.code != http.StatusNotFound || res.loc != "" {
			t.Fatalf("unavailable link leaked its destination: %+v", res)
		}
	}
	checkLink(true)

	exec(`UPDATE editions SET title = 'Исправленное заглавие. 5-е издание' WHERE id = $1`, f.openEd)
	scan("0")
	checkLink(true)
	exec(`UPDATE editions SET title = 'Исправленное заглавие. 6-е издание' WHERE id = $1`, f.openEd)
	checkLink(false)
	exec(`UPDATE editions SET title = 'Исправленное заглавие. 5-е издание' WHERE id = $1`, f.openEd)
	checkLink(true)

	// Скрытие скана сохраняет переход к тексту. Скан той же полосы читателю
	// и редактору не выдаёт штатный обработчик.
	exec(`UPDATE works SET scans_hidden = true WHERE id = $1`, f.ch.work)
	target := fmt.Sprintf("/api/works/%d/pages/by-number/2", f.ch.work)
	for _, token := range []string{f.readerTok, f.editorTok} {
		res := sweepDo(t, router, target, token)
		if res.code != 200 || strings.Contains(res.text, `"preview_url"`) || strings.Contains(res.text, `"preview_path":"works/`) {
			t.Fatalf("scan visibility: %+v", res)
		}
	}
	checkLink(true)

	exec(`UPDATE chapters SET closed = true WHERE id = $1`, f.ch.closed)
	checkLink(false)
	exec(`UPDATE chapters SET closed = false WHERE id = $1`, f.ch.closed)
	checkLink(true)
	exec(`UPDATE works SET closed = true WHERE id = $1`, f.ch.work)
	checkLink(false)
	exec(`UPDATE works SET closed = false, page_offset = 44 WHERE id = $1`, f.ch.work)
	checkLink(false)
	exec(`UPDATE works SET page_offset = 45 WHERE id = $1`, f.ch.work)

	// Правка с теми же словами не делает прежнее решение действительным.
	exec(`UPDATE pages SET content_markdown = $2 WHERE id = $1`, f.pages[f.open], "Другой контекст. "+source)
	checkLink(false)
	decide(missing, `{"decision":"rejected"}`, 409)
	if rows := list("confirmed"); len(rows) != 1 || rows[0].Issue != "stale" {
		t.Fatalf("stale source assignment: %+v", rows)
	}
	exec(`UPDATE pages SET content_markdown = $2 WHERE id = $1`, f.pages[f.open], source)
	checkLink(true)

	exec(`DELETE FROM pages WHERE id = $1`, f.ch.pages[2])
	exec(`INSERT INTO pages (work_id, page_number, content_markdown) VALUES ($1, 2, 'Новая полоса')`, f.ch.work)
	checkLink(false)
	if rows := list("confirmed"); len(rows) != 1 || rows[0].Issue != "stale" || rows[0].TargetPageID != nil {
		t.Fatalf("lost target assignment: %+v", rows)
	}
}

func seedSweepCrossReferences(t *testing.T, pool *pgxpool.Pool, f *closedSweep) {
	t.Helper()
	ctx := context.Background()
	source := f.pages[f.open]
	quotes := [3]string{"т. 3, с. 1", "т. 2, с. 1", "т. 3, с. 2"}
	if _, err := pool.Exec(ctx, `UPDATE pages SET content_markdown = content_markdown || $2 WHERE id = $1`,
		source, "\n\n"+strings.Join(quotes[:], "; ")+"."); err != nil {
		t.Fatal(err)
	}
	for i, target := range [3]int64{f.ch.pages[1], f.pages[f.self], f.ch.pages[2]} {
		err := pool.QueryRow(ctx, `
			INSERT INTO cross_references (edition_id, edition_designation, source_page_id, source_digest,
				note, ordinal, quote, context, volume, printed_page, edition_number, status, target_page_id)
			SELECT e.id, 0, sp.id, sha256(convert_to(sp.content_markdown, 'UTF8')), '', $3, $4, $4,
			       w.volume_number, tp.page_number + w.page_offset, 0, 'confirmed', tp.id
			FROM pages sp, pages tp JOIN works w ON w.id = tp.work_id JOIN editions e ON e.id = w.edition_id
			WHERE sp.id = $1 AND tp.id = $2
			RETURNING id
		`, source, target, i+1, quotes[i]).Scan(&f.readingRefs[i])
		if err != nil {
			t.Fatal(err)
		}
	}
}
