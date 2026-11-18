package repository

import (
	"context"
	"testing"

	"proofreader/internal/models"
)

// Если наследование исчезнет хотя бы на одном пути, ссылка на скан может
// выдаваться без действия администратора.
func TestScansHiddenInheritance(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	works := NewWorkRepository(pool)
	editions := NewEditionRepository(pool)
	closed := NewClosedRepository(pool)

	self := newVolume(t, works, "сам том")
	child := newChild(t, works, self, "передние листы")
	ed := &models.Edition{Title: "собрание", Slug: "scan-ed"}
	if err := editions.Create(ctx, ed, models.ClosureActor{}, ""); err != nil {
		t.Fatal(err)
	}
	inEdition := newVolume(t, works, "том собрания")
	if _, err := pool.Exec(ctx, `UPDATE works SET edition_id = $2 WHERE id = $1`, inEdition.ID, ed.ID); err != nil {
		t.Fatal(err)
	}
	journal := newJournal(t, NewJournalRepository(pool), "scan-journal")
	_, issue := newIssue(t, NewJournalRepository(pool), journal, 1925, 1, 1)
	open := newVolume(t, works, "открытый том")

	if _, err := pool.Exec(ctx, `UPDATE works SET scans_hidden = true WHERE id = $1`, self.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE editions SET scans_hidden = true WHERE id = $1`, ed.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE journals SET scans_hidden = true WHERE id = $1`, journal.ID); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		work *models.Work
		want string
	}{
		{self, "work"}, {child, "parent"}, {inEdition, "edition"},
		{issue, "journal"}, {open, ""},
	} {
		got, err := closed.ScansHiddenBy(ctx, c.work.ID)
		if err != nil || got != c.want {
			t.Errorf("work %d: ScansHiddenBy=%q, err=%v; want %q", c.work.ID, got, err, c.want)
		}
	}
	ids, err := closed.WorksWithHiddenScans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []*models.Work{self, child, inEdition, issue} {
		if !ids[w.ID] {
			t.Errorf("work %d omitted from WorksWithHiddenScans", w.ID)
		}
	}
	if ids[open.ID] {
		t.Errorf("open work %d included in WorksWithHiddenScans", open.ID)
	}
}

func TestScansHiddenSpansSharedPage(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	w := newVolume(t, NewWorkRepository(pool), "том с общей полосой")
	var parentID, childID int64
	if err := pool.QueryRow(ctx, `INSERT INTO chapters
		(work_id, title, type, order_number, start_page, end_page, scans_hidden)
		VALUES ($1, 'раздел', 'chapter', 1, 1, 5, true) RETURNING id`, w.ID).Scan(&parentID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO chapters
		(work_id, parent_id, title, type, order_number, start_page, end_page)
		VALUES ($1, $2, 'подглава', 'chapter', 1, 2, 3) RETURNING id`, w.ID, parentID).Scan(&childID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO chapters
		(work_id, title, type, order_number, start_page, end_page)
		VALUES ($1, 'сосед', 'chapter', 2, 5, 8)`, w.ID); err != nil {
		t.Fatal(err)
	}
	spans, err := NewClosedRepository(pool).ScansHiddenSpans(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) == 0 || spans[0].Start != 1 || spans[0].End != 5 {
		t.Fatalf("ScansHiddenSpans=%v; want 1..5 including shared page 5", spans)
	}
	ch, err := NewChapterRepository(pool).GetByID(ctx, childID)
	if err != nil || ch.ScansHiddenBy != "parent" {
		t.Fatalf("child ScansHiddenBy=%q err=%v; want parent", ch.ScansHiddenBy, err)
	}
	ids, err := NewClosedRepository(pool).WorksWithHiddenScans(ctx)
	if err != nil || !ids[w.ID] {
		t.Fatalf("hidden chapter work missing from originals set: %v, %v", ids, err)
	}
}
