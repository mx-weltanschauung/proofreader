package repository

import (
	"context"
	"testing"
	"time"

	"proofreader/internal/stats"
)

func visibleRow(d string, workID int64, page int, visitor string) stats.VisiblePageRow {
	return stats.VisiblePageRow{Day: day(d), TS: day(d).Add(12 * time.Hour), WorkID: workID, PageNumber: page, Visitor: visitor}
}

func seedVisibleChapter(t *testing.T) (*StatsRepository, int64, int64) {
	t.Helper()
	pool := testPool(t)
	ctx := context.Background()
	seedWork(t, pool, 49, "Том")
	_, err := pool.Exec(ctx, `INSERT INTO chapters (id, work_id, title, type, order_number, start_page, end_page, parent_id)
		VALUES (101, 49, 'Часть', 'part', 1, 1, 3, NULL), (102, 49, 'Глава', 'chapter', 1, 1, 3, 101)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO pages (work_id, page_number, chapter_id) VALUES (49,1,102),(49,2,102),(49,3,102)`)
	if err != nil {
		t.Fatal(err)
	}
	return NewStatsRepository(pool), 49, 102
}

func TestVisiblePagesRollupAndRead(t *testing.T) {
	r, workID, chapterID := seedVisibleChapter(t)
	ctx := context.Background()
	rows := []stats.VisiblePageRow{
		visibleRow("2026-09-28", workID, 1, "a"), visibleRow("2026-09-28", workID, 1, "a"),
		visibleRow("2026-09-28", workID, 1, "b"), visibleRow("2026-09-28", workID, 2, "a"),
	}
	if err := r.InsertVisiblePages(ctx, rows); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := r.Rollup(ctx, day("2026-09-29")); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []int64{chapterID, 101} {
		got, err := r.VisiblePages(ctx, workID, id, day("2026-09-01"), day("2026-09-29"))
		if err != nil {
			t.Fatal(err)
		}
		if got.CollectingSince == nil || !got.CollectingSince.Equal(day("2026-09-28")) || len(got.Pages) != 3 ||
			got.Pages[0].Visitors != 2 || got.Pages[1].Visitors != 1 || got.Pages[2].Visitors != 0 {
			t.Fatalf("chapter %d: %+v", id, got)
		}
	}
}

func TestVisiblePagesYesterdayAndPrune(t *testing.T) {
	r, workID, chapterID := seedVisibleChapter(t)
	ctx := context.Background()
	if err := r.InsertVisiblePages(ctx, []stats.VisiblePageRow{
		visibleRow("2026-06-01", workID, 1, "a"), visibleRow("2026-09-28", workID, 1, "a"),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := r.VisiblePages(ctx, workID, chapterID, day("2026-06-01"), day("2026-09-29"))
	if err != nil || got.Pages[0].Visitors != 2 {
		t.Fatalf("unrolled yesterday: %+v, %v", got, err)
	}
	if err := r.Rollup(ctx, day("2026-09-29")); err != nil {
		t.Fatal(err)
	}
	if err := r.Prune(ctx, day("2026-07-01"), day("2026-09-29")); err != nil {
		t.Fatal(err)
	}
	got, err = r.VisiblePages(ctx, workID, chapterID, day("2026-06-01"), day("2026-09-29"))
	if err != nil || got.Pages[0].Visitors != 2 {
		t.Fatalf("after prune: %+v, %v", got, err)
	}
}

func TestVisiblePagesIgnoresMissingPage(t *testing.T) {
	r, workID, chapterID := seedVisibleChapter(t)
	ctx := context.Background()
	if err := r.InsertVisiblePages(ctx, []stats.VisiblePageRow{visibleRow("2026-09-28", workID, 99, "a")}); err != nil {
		t.Fatal(err)
	}
	if err := r.Rollup(ctx, day("2026-09-29")); err != nil {
		t.Fatal(err)
	}
	got, err := r.VisiblePages(ctx, workID, chapterID, day("2026-09-01"), day("2026-09-29"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range got.Pages {
		if p.Visitors != 0 {
			t.Fatalf("invalid event counted: %+v", got)
		}
	}
}
