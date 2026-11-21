package repository

import (
	"context"
	"testing"

	"proofreader/internal/stats"
)

func downloadRow(d, kind, format string, workID, entityID int64) stats.Row {
	r := row(d, stats.ChannelDownload, kind, workID, entityID, "visitor")
	r.SlugKey = format
	return r
}

func summaryCount(data *DownloadStats, kind, format string) int {
	for _, r := range data.Summary {
		if r.Kind == kind && r.Format == format {
			return r.Views
		}
	}
	return 0
}

func TestDownloadsIncludesRolledAndRaw(t *testing.T) {
	pool := testPool(t)
	r := NewStatsRepository(pool)
	ctx := context.Background()
	seedWork(t, pool, 49, "Том")
	if _, err := pool.Exec(ctx, `INSERT INTO chapters (id,work_id,title,type,order_number,start_page,end_page) VALUES (101,49,'Глава','chapter',1,1,3)`); err != nil {
		t.Fatal(err)
	}
	if err := r.InsertViews(ctx, []stats.Row{
		downloadRow("2026-09-28", "work", "epub", 49, 0),
		downloadRow("2026-09-28", "work", "epub", 49, 0),
		downloadRow("2026-09-29", "work", "pdf", 49, 0),
		downloadRow("2026-09-29", "chapter", "fb2", 49, 101),
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Rollup(ctx, day("2026-09-29")); err != nil {
		t.Fatal(err)
	}
	data, err := r.Downloads(ctx, day("2026-09-28"), day("2026-09-29"), 50)
	if err != nil {
		t.Fatal(err)
	}
	if summaryCount(data, "work", "epub") != 2 || summaryCount(data, "work", "pdf") != 1 || summaryCount(data, "chapter", "fb2") != 1 {
		t.Fatalf("summary=%+v", data.Summary)
	}
	if len(data.Top) != 3 || data.Top[0].Title != "Том" {
		t.Fatalf("top=%+v", data.Top)
	}
}

func TestDownloadsCollectionOnlyInSummary(t *testing.T) {
	r := NewStatsRepository(testPool(t))
	ctx := context.Background()
	if err := r.InsertViews(ctx, []stats.Row{downloadRow("2026-09-29", "collection", "epub", 0, 0)}); err != nil {
		t.Fatal(err)
	}
	data, err := r.Downloads(ctx, day("2026-09-01"), day("2026-09-29"), 50)
	if err != nil {
		t.Fatal(err)
	}
	if summaryCount(data, "collection", "epub") != 1 || len(data.Top) != 0 {
		t.Fatalf("data=%+v", data)
	}
}

func TestDownloadsArchiveAndUnknown(t *testing.T) {
	r := NewStatsRepository(testPool(t))
	ctx := context.Background()
	if err := r.InsertViews(ctx, []stats.Row{
		downloadRow("2026-09-29", "archive", "", 0, 0),
		downloadRow("2026-09-29", "work", "strange", 777, 0),
	}); err != nil {
		t.Fatal(err)
	}
	data, err := r.Downloads(ctx, day("2026-09-01"), day("2026-09-29"), 50)
	if err != nil {
		t.Fatal(err)
	}
	if summaryCount(data, "archive", "") != 1 || summaryCount(data, "work", "strange") != 1 || len(data.Top) != 1 || !data.Top[0].Missing {
		t.Fatalf("data=%+v", data)
	}
}
