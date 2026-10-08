package repository

import (
	"context"
	"testing"

	"proofreader/internal/stats"
)

// Вчера не должно пропадать после полуночи, пока уборка ещё не свернула его:
// свёрнуто только 27-е, 28-е и 29-е читаются сырыми.
func TestStatsTrafficKeepsUnrolledYesterday(t *testing.T) {
	pool := testPool(t)
	repo := NewStatsRepository(pool)
	ctx := context.Background()
	repo.InsertViews(ctx, []stats.Row{
		row("2026-09-27", stats.ChannelSPA, "work", 1, 0, "a"),
		row("2026-09-28", stats.ChannelSPA, "work", 1, 0, "a"),
		row("2026-09-28", stats.ChannelSPA, "work", 1, 0, "b"),
		row("2026-09-29", stats.ChannelSPA, "work", 1, 0, "a"),
	})
	if err := repo.Rollup(ctx, day("2026-09-28")); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.Traffic(ctx, day("2026-09-01"), day("2026-09-29"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]int{"2026-09-27": {1, 1}, "2026-09-28": {2, 2}, "2026-09-29": {1, 1}}
	if len(rows) != 3 {
		t.Fatalf("строк %d: %+v", len(rows), rows)
	}
	for _, r := range rows {
		w, ok := want[r.Day.Format("2006-01-02")]
		if !ok || r.Views != w[0] || r.Visitors != w[1] {
			t.Fatalf("строка %+v, ожидалось %v", r, w)
		}
	}
	top, err := repo.Top(ctx, stats.ChannelSPA, "work", day("2026-09-01"), day("2026-09-29"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 1 || top[0].Views != 4 {
		t.Fatalf("топ должен видеть все трое суток: %+v", top)
	}
}

// Запросы агентов (MCP) не попадают в списки читательских поисков.
func TestStatsSearchesExcludeAgents(t *testing.T) {
	pool := testPool(t)
	repo := NewStatsRepository(pool)
	ctx := context.Background()
	zero := 0
	repo.InsertViews(ctx, []stats.Row{
		{Day: day("2026-09-29"), TS: day("2026-09-29"), Channel: stats.ChannelSearch, Kind: "search",
			Query: "от агента", Agent: "mcp", Hits: &zero},
	})
	s, err := repo.Searches(ctx, day("2026-09-01"), day("2026-09-29"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Frequent) != 0 || len(s.Zero) != 0 {
		t.Fatalf("агентский запрос попал в списки: %+v", s)
	}
}
