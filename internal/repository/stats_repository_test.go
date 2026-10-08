package repository

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/stats"
)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func row(d, channel, kind string, workID, entityID int64, visitor string) stats.Row {
	return stats.Row{Day: day(d), TS: day(d).Add(12 * time.Hour), Channel: channel, Kind: kind,
		WorkID: workID, EntityID: entityID, Visitor: visitor}
}

func TestStatsDaySaltStableAndDistinct(t *testing.T) {
	repo := NewStatsRepository(testPool(t))
	ctx := context.Background()
	a1, err := repo.DaySalt(ctx, day("2026-09-28"))
	if err != nil {
		t.Fatal(err)
	}
	a2, _ := repo.DaySalt(ctx, day("2026-09-28"))
	b, _ := repo.DaySalt(ctx, day("2026-09-29"))
	if len(a1) != 32 || !bytes.Equal(a1, a2) {
		t.Fatalf("соль суток нестабильна: %x / %x", a1, a2)
	}
	if bytes.Equal(a1, b) {
		t.Fatal("у разных суток одна соль")
	}
}

func TestStatsRollupIdempotentAndSkipsToday(t *testing.T) {
	pool := testPool(t)
	repo := NewStatsRepository(pool)
	ctx := context.Background()
	zero, one := 0, 3
	rows := []stats.Row{
		row("2026-09-27", stats.ChannelSPA, "chapter", 49, 10125, "a"),
		row("2026-09-27", stats.ChannelSPA, "chapter", 49, 10125, "a"),
		row("2026-09-27", stats.ChannelSPA, "chapter", 49, 10125, "b"),
		row("2026-09-27", stats.ChannelSPA, "work", 49, 0, "a"),
		{Day: day("2026-09-27"), TS: day("2026-09-27"), Channel: stats.ChannelSearch, Kind: "search", Query: "прибавочная", Hits: &zero, Visitor: "a"},
		{Day: day("2026-09-27"), TS: day("2026-09-27"), Channel: stats.ChannelSearch, Kind: "search", Query: "прибавочная", Hits: &one, Visitor: "b"},
		row("2026-09-29", stats.ChannelSPA, "chapter", 49, 10125, "c"), // «сегодня»
	}
	if err := repo.InsertViews(ctx, rows); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // второй прогон не должен ничего удвоить
		if err := repo.Rollup(ctx, day("2026-09-29")); err != nil {
			t.Fatal(err)
		}
	}
	var views, visitors int
	if err := pool.QueryRow(ctx, `SELECT views, visitors FROM page_views_daily
		WHERE day='2026-09-27' AND kind='chapter' AND entity_id=10125`).Scan(&views, &visitors); err != nil {
		t.Fatal(err)
	}
	if views != 3 || visitors != 2 {
		t.Fatalf("глава: views=%d visitors=%d, ожидалось 3/2", views, visitors)
	}
	if err := pool.QueryRow(ctx, `SELECT views, visitors FROM page_views_daily_totals
		WHERE day='2026-09-27' AND channel='spa'`).Scan(&views, &visitors); err != nil {
		t.Fatal(err)
	}
	if views != 4 || visitors != 2 {
		t.Fatalf("итог spa: views=%d visitors=%d, ожидалось 4/2", views, visitors)
	}
	var zeroHits int
	if err := pool.QueryRow(ctx, `SELECT zero_hits FROM page_views_daily
		WHERE day='2026-09-27' AND query='прибавочная'`).Scan(&zeroHits); err != nil {
		t.Fatal(err)
	}
	if zeroHits != 1 {
		t.Fatalf("zero_hits=%d, ожидался 1", zeroHits)
	}
	var today int
	pool.QueryRow(ctx, `SELECT count(*) FROM page_views_daily WHERE day='2026-09-29'`).Scan(&today)
	if today != 0 {
		t.Fatal("свёртка захватила незаконченные сутки")
	}
}

func TestStatsPruneKeepsRollup(t *testing.T) {
	pool := testPool(t)
	repo := NewStatsRepository(pool)
	ctx := context.Background()
	repo.InsertViews(ctx, []stats.Row{
		row("2026-06-01", stats.ChannelSPA, "work", 1, 0, "a"),
		row("2026-09-28", stats.ChannelSPA, "work", 1, 0, "a"),
	})
	repo.DaySalt(ctx, day("2026-09-28"))
	repo.DaySalt(ctx, day("2026-09-29"))
	if err := repo.Rollup(ctx, day("2026-09-29")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Prune(ctx, day("2026-07-01"), day("2026-09-29")); err != nil {
		t.Fatal(err)
	}
	var raw, rolled, salts int
	pool.QueryRow(ctx, `SELECT count(*) FROM page_views`).Scan(&raw)
	pool.QueryRow(ctx, `SELECT count(*) FROM page_views_daily`).Scan(&rolled)
	pool.QueryRow(ctx, `SELECT count(*) FROM visit_salts`).Scan(&salts)
	if raw != 1 || rolled != 2 || salts != 1 {
		t.Fatalf("raw=%d rolled=%d salts=%d, ожидалось 1/2/1", raw, rolled, salts)
	}
}

// seedWork — том без родителя и издания; форма INSERT — как в
// chapter_find_by_page_test.go. Владельца (id = 1) заводит testPool.
func seedWork(t *testing.T, pool *pgxpool.Pool, id int64, title string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO works (id, title, author, file_path, owner_id, role)
		VALUES ($1, $2, 'Автор', 'f', 1, 'volume')`, id, title); err != nil {
		t.Fatalf("подготовка работы: %v", err)
	}
}

// Review Focus 2: сегодня из сырых + прошлое из свёртки без двойного счёта.
func TestStatsTrafficJoinsTodayWithoutDoubleCount(t *testing.T) {
	pool := testPool(t)
	repo := NewStatsRepository(pool)
	ctx := context.Background()
	repo.InsertViews(ctx, []stats.Row{
		row("2026-09-28", stats.ChannelSPA, "work", 1, 0, "a"),
		row("2026-09-28", stats.ChannelSPA, "work", 1, 0, "b"),
		row("2026-09-29", stats.ChannelSPA, "work", 1, 0, "a"),
	})
	if err := repo.Rollup(ctx, day("2026-09-29")); err != nil {
		t.Fatal(err)
	}
	// Свёртка сегодняшних суток пуста по построению; строка-подлог проверяет,
	// что чтение её и не смотрит: иначе сегодня считалось бы дважды.
	if _, err := pool.Exec(ctx, `INSERT INTO page_views_daily_totals (day, channel, views, visitors)
		VALUES ('2026-09-29', 'spa', 100, 100)`); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.Traffic(ctx, day("2026-09-01"), day("2026-09-29"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]TrafficRow{}
	for _, r := range rows {
		got[r.Day.Format("2006-01-02")+r.Channel] = r
	}
	if y := got["2026-09-28spa"]; y.Views != 2 || y.Visitors != 2 {
		t.Fatalf("вчера: %+v", y)
	}
	if d := got["2026-09-29spa"]; d.Views != 1 || d.Visitors != 1 {
		t.Fatalf("сегодня: %+v", d)
	}
	if len(rows) != 2 {
		t.Fatalf("строк %d: %+v", len(rows), rows)
	}
}

// Review Focus 5: снятый том даёт строку с missing, а не ошибку.
func TestStatsTopResolvesTitlesAndMissing(t *testing.T) {
	pool := testPool(t)
	repo := NewStatsRepository(pool)
	ctx := context.Background()
	seedWork(t, pool, 10, "Что делать?")
	repo.InsertViews(ctx, []stats.Row{
		row("2026-09-29", stats.ChannelSPA, "work", 10, 0, "a"),
		row("2026-09-29", stats.ChannelSPA, "work", 10, 0, "b"),
		row("2026-09-29", stats.ChannelSPA, "work", 999, 0, "a"), // снят
	})
	top, err := repo.Top(ctx, stats.ChannelSPA, "work", day("2026-09-01"), day("2026-09-29"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 2 || top[0].WorkID != 10 || top[0].Title != "Что делать?" || top[0].Views != 2 || top[0].Missing {
		t.Fatalf("первая строка: %+v", top)
	}
	if !top[1].Missing || top[1].WorkID != 999 {
		t.Fatalf("снятый том: %+v", top[1])
	}
}

func TestStatsSearchesSplitsZero(t *testing.T) {
	pool := testPool(t)
	repo := NewStatsRepository(pool)
	ctx := context.Background()
	zero, some := 0, 5
	q := func(text string, hits *int) stats.Row {
		return stats.Row{Day: day("2026-09-29"), TS: day("2026-09-29"), Channel: stats.ChannelSearch,
			Kind: "search", Query: text, Hits: hits, Visitor: "a"}
	}
	repo.InsertViews(ctx, []stats.Row{q("ленин", &some), q("ленин", &some), q("хрестоматия", &zero)})
	// Подлог в свёртке за сегодня: чтение обязано его не видеть (см. dailyUnion).
	if _, err := pool.Exec(ctx, `INSERT INTO page_views_daily (day, channel, kind, query, views, visitors, zero_hits)
		VALUES ('2026-09-29', 'search', 'search', 'ленин', 100, 100, 0)`); err != nil {
		t.Fatal(err)
	}
	s, err := repo.Searches(ctx, day("2026-09-01"), day("2026-09-29"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Frequent) != 2 || s.Frequent[0].Query != "ленин" || s.Frequent[0].Count != 2 {
		t.Fatalf("частые: %+v", s.Frequent)
	}
	if len(s.Zero) != 1 || s.Zero[0].Query != "хрестоматия" || s.Zero[0].ZeroHits != 1 {
		t.Fatalf("нулевые: %+v", s.Zero)
	}
}

func spaDevice(d, device, visitor string) stats.Row {
	r := row(d, stats.ChannelSPA, "chapter", 49, 10125, visitor)
	r.Device = device
	return r
}

// Двое суток за один прогон: граница свёртки читает max(day) итогов, и
// вставка устройств ПОСЛЕ итогов взяла бы только последние сутки.
func TestStatsRollupDevicesCoversEveryDayAndOnlySPA(t *testing.T) {
	pool := testPool(t)
	repo := NewStatsRepository(pool)
	ctx := context.Background()
	md := row("2026-09-27", stats.ChannelMD, "chapter", 49, 10125, "z")
	md.Device = stats.DeviceWide // серверный канал не должен попасть в устройства
	rows := []stats.Row{
		spaDevice("2026-09-26", stats.DeviceNarrow, "a"),
		spaDevice("2026-09-27", stats.DeviceNarrow, "a"),
		spaDevice("2026-09-27", stats.DeviceNarrow, "a"),
		spaDevice("2026-09-27", stats.DeviceNarrow, "b"),
		spaDevice("2026-09-27", stats.DeviceWide, "c"),
		spaDevice("2026-09-27", "", "d"),
		md,
	}
	if err := repo.InsertViews(ctx, rows); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := repo.Rollup(ctx, day("2026-09-29")); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string][2]int{}
	r, err := pool.Query(ctx, `SELECT day::text || ' ' || device, views, visitors FROM page_views_daily_devices`)
	if err != nil {
		t.Fatal(err)
	}
	for r.Next() {
		var k string
		var v, u int
		if err := r.Scan(&k, &v, &u); err != nil {
			t.Fatal(err)
		}
		got[k] = [2]int{v, u}
	}
	want := map[string][2]int{
		"2026-09-26 narrow": {1, 1},
		"2026-09-27 narrow": {3, 2},
		"2026-09-27 wide":   {1, 1},
		"2026-09-27 ":       {1, 1},
	}
	if len(got) != len(want) {
		t.Fatalf("свёртка устройств: %v, ждали %v", got, want)
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%q: %v, ждали %v", k, got[k], w)
		}
	}
}

// Сегодня и не свёрнутые ещё сутки читаются из сырых строк, свёрнутые — из
// свёртки, без пересечения.
func TestStatsDevicesJoinsRollupWithRaw(t *testing.T) {
	pool := testPool(t)
	repo := NewStatsRepository(pool)
	ctx := context.Background()
	if err := repo.InsertViews(ctx, []stats.Row{
		spaDevice("2026-09-27", stats.DeviceNarrow, "a"),
		spaDevice("2026-09-27", stats.DeviceNarrow, "b"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Rollup(ctx, day("2026-09-28")); err != nil {
		t.Fatal(err)
	}
	if err := repo.InsertViews(ctx, []stats.Row{
		spaDevice("2026-09-29", stats.DeviceWide, "c"), // «сегодня», не свёрнуто
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.Devices(ctx, day("2026-09-20"), day("2026-09-29"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("строк %d: %+v", len(rows), rows)
	}
	if rows[0].Day.Format("2006-01-02") != "2026-09-27" || rows[0].Device != stats.DeviceNarrow ||
		rows[0].Views != 2 || rows[0].Visitors != 2 {
		t.Errorf("свёрнутые сутки: %+v", rows[0])
	}
	if rows[1].Day.Format("2006-01-02") != "2026-09-29" || rows[1].Device != stats.DeviceWide ||
		rows[1].Views != 1 || rows[1].Visitors != 1 {
		t.Errorf("сегодня из сырых: %+v", rows[1])
	}
}
