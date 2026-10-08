package repository

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/stats"
)

// StatsRepository — посещаемость: запись пачками, соль суток, свёртка, чистка
// и чтение для /admin/stats.
type StatsRepository struct {
	pool *pgxpool.Pool
}

func NewStatsRepository(pool *pgxpool.Pool) *StatsRepository {
	return &StatsRepository{pool: pool}
}

var pageViewColumns = []string{"day", "ts", "channel", "kind", "work_id", "entity_id",
	"slug_key", "agent", "visitor", "ref_host", "query", "hits", "device"}

// InsertViews пишет пачку одним COPY.
func (r *StatsRepository) InsertViews(ctx context.Context, rows []stats.Row) error {
	if len(rows) == 0 {
		return nil
	}
	_, err := r.pool.CopyFrom(ctx, pgx.Identifier{"page_views"}, pageViewColumns,
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			v := rows[i]
			return []any{v.Day, v.TS, v.Channel, v.Kind, v.WorkID, v.EntityID,
				v.SlugKey, v.Agent, v.Visitor, v.RefHost, v.Query, v.Hits, v.Device}, nil
		}))
	if err != nil {
		return fmt.Errorf("запись просмотров: %w", err)
	}
	return nil
}

// DaySalt — соль суток; заводится при первом обращении. Два процесса,
// пришедшие разом, получат одну и ту же: проигравший вставку перечитывает.
func (r *StatsRepository) DaySalt(ctx context.Context, day time.Time) ([]byte, error) {
	fresh := make([]byte, 32)
	if _, err := rand.Read(fresh); err != nil {
		return nil, err
	}
	if _, err := r.pool.Exec(ctx,
		`INSERT INTO visit_salts (day, salt) VALUES ($1, $2) ON CONFLICT (day) DO NOTHING`,
		day, fresh); err != nil {
		return nil, fmt.Errorf("соль суток: %w", err)
	}
	var salt []byte
	if err := r.pool.QueryRow(ctx, `SELECT salt FROM visit_salts WHERE day = $1`, day).Scan(&salt); err != nil {
		return nil, fmt.Errorf("соль суток: %w", err)
	}
	return salt, nil
}

// Rollup сворачивает законченные сутки (day < today), начиная с последних
// уже свёрнутых включительно: просмотр, слитый через секунды после
// полуночи, дописывается во вчерашние сутки при следующем прогоне.
// Идемпотентна: ON CONFLICT перезаписывает счёт тем же числом.
func (r *StatsRepository) Rollup(ctx context.Context, today time.Time) error {
	const from = `coalesce((SELECT max(day) FROM page_views_daily_totals), '-infinity'::date)`
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO page_views_daily (day, channel, kind, work_id, entity_id, slug_key, agent, ref_host, query,
		                              views, visitors, zero_hits)
		SELECT day, channel, kind, work_id, entity_id, slug_key, agent, ref_host, query,
		       count(*), count(DISTINCT visitor) FILTER (WHERE visitor <> ''),
		       count(*) FILTER (WHERE hits = 0)
		  FROM page_views
		 WHERE day < $1 AND day >= `+from+`
		 GROUP BY day, channel, kind, work_id, entity_id, slug_key, agent, ref_host, query
		ON CONFLICT (day, channel, kind, work_id, entity_id, slug_key, agent, ref_host, query)
		DO UPDATE SET views = EXCLUDED.views, visitors = EXCLUDED.visitors, zero_hits = EXCLUDED.zero_hits`,
		today); err != nil {
		return fmt.Errorf("свёртка по сущностям: %w", err)
	}
	// Устройства — ДО итогов: from читает max(day) из page_views_daily_totals,
	// и вставка итогов сдвинула бы его на последние сутки — устройства прочих
	// суток этого прогона пропали бы молча.
	if _, err := tx.Exec(ctx, `
		INSERT INTO page_views_daily_devices (day, device, views, visitors)
		SELECT day, device, count(*), count(DISTINCT visitor) FILTER (WHERE visitor <> '')
		  FROM page_views
		 WHERE channel = 'spa' AND day < $1 AND day >= `+from+`
		 GROUP BY day, device
		ON CONFLICT (day, device) DO UPDATE SET views = EXCLUDED.views, visitors = EXCLUDED.visitors`,
		today); err != nil {
		return fmt.Errorf("свёртка устройств: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO page_views_daily_totals (day, channel, views, visitors)
		SELECT day, channel, count(*), count(DISTINCT visitor) FILTER (WHERE visitor <> '')
		  FROM page_views
		 WHERE day < $1 AND day >= `+from+`
		 GROUP BY day, channel
		ON CONFLICT (day, channel) DO UPDATE SET views = EXCLUDED.views, visitors = EXCLUDED.visitors`,
		today); err != nil {
		return fmt.Errorf("свёртка итогов: %w", err)
	}
	return tx.Commit(ctx)
}

// Prune удаляет сырые просмотры до rawBefore и соли до saltBefore.
func (r *StatsRepository) Prune(ctx context.Context, rawBefore, saltBefore time.Time) error {
	if _, err := r.pool.Exec(ctx, `DELETE FROM page_views WHERE day < $1`, rawBefore); err != nil {
		return fmt.Errorf("чистка просмотров: %w", err)
	}
	if _, err := r.pool.Exec(ctx, `DELETE FROM visit_salts WHERE day < $1`, saltBefore); err != nil {
		return fmt.Errorf("чистка солей: %w", err)
	}
	return nil
}

type TrafficRow struct {
	Day      time.Time `json:"day"`
	Channel  string    `json:"channel"`
	Views    int       `json:"views"`
	Visitors int       `json:"visitors"`
}

type TopRow struct {
	WorkID    int64  `json:"work_id"`
	EntityID  int64  `json:"entity_id"`
	SlugKey   string `json:"slug_key"`
	Title     string `json:"title"`
	WorkTitle string `json:"work_title"`
	Missing   bool   `json:"missing"`
	Views     int    `json:"views"`
	Visitors  int    `json:"visitors"` // посетителе-дни
}

type CountRow struct {
	Key      string `json:"key"`
	Views    int    `json:"views"`
	Visitors int    `json:"visitors"`
}

type SearchRow struct {
	Query    string `json:"query"`
	Count    int    `json:"count"`
	ZeroHits int    `json:"zero_hits"`
}

type SearchStats struct {
	Frequent []SearchRow `json:"frequent"`
	Zero     []SearchRow `json:"zero"`
}

type CrawlerRow struct {
	Day   time.Time `json:"day"`
	Agent string    `json:"agent"`
	Views int       `json:"views"`
}

type DeviceRow struct {
	Day      time.Time `json:"day"`
	Device   string    `json:"device"`
	Views    int       `json:"views"`
	Visitors int       `json:"visitors"`
}

// rolledDay — последние свёрнутые сутки. Сутки либо целиком в свёртке
// (<= rolledDay), либо читаются сырыми (> rolledDay): Rollup пересчитывает
// последние свёрнутые включительно, поэтому пересечения нет. Так вчерашний
// день не пропадает между полуночью и ближайшей уборкой.
// Свёртка за сегодня (day >= $2) не считается: сегодня всегда сырое.
const rolledDay = `coalesce((SELECT max(day) FROM page_views_daily_totals WHERE day < $2), '-infinity'::date)`

// dailyUnion — свёртка за [from, rolledDay] плюс сырые строки после неё до
// today включительно, той же формы. $1 = from, $2 = today.
const dailyUnion = `
	WITH v AS (
		SELECT day, channel, kind, work_id, entity_id, slug_key, agent, ref_host, query, views, visitors, zero_hits
		  FROM page_views_daily WHERE day >= $1 AND day <= ` + rolledDay + `
		UNION ALL
		SELECT day, channel, kind, work_id, entity_id, slug_key, agent, ref_host, query,
		       count(*)::int, (count(DISTINCT visitor) FILTER (WHERE visitor <> ''))::int,
		       (count(*) FILTER (WHERE hits = 0))::int
		  FROM page_views WHERE day >= $1 AND day > `+rolledDay+` AND day <= $2
		 GROUP BY day, channel, kind, work_id, entity_id, slug_key, agent, ref_host, query
	)`

func (r *StatsRepository) Traffic(ctx context.Context, from, today time.Time) ([]TrafficRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT day, channel, views, visitors FROM page_views_daily_totals
		 WHERE day >= $1 AND day <= `+rolledDay+`
		UNION ALL
		SELECT day, channel, count(*)::int, (count(DISTINCT visitor) FILTER (WHERE visitor <> ''))::int
		  FROM page_views WHERE day >= $1 AND day > `+rolledDay+` AND day <= $2 GROUP BY day, channel
		ORDER BY 1, 2`, from, today)
	if err != nil {
		return nil, fmt.Errorf("посещаемость по дням: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (TrafficRow, error) {
		var t TrafficRow
		err := row.Scan(&t.Day, &t.Channel, &t.Views, &t.Visitors)
		return t, err
	})
}

// Top — сущности вида kind по убыванию просмотров, с названиями. Строка,
// сущности которой больше нет (том снят, разбор удалён), остаётся с
// missing: иначе снятое пропадало бы из истории молча.
func (r *StatsRepository) Top(ctx context.Context, channel, kind string, from, today time.Time, limit int) ([]TopRow, error) {
	rows, err := r.pool.Query(ctx, dailyUnion+`
		, t AS (
			SELECT work_id, entity_id, slug_key, sum(views)::int AS views, sum(visitors)::int AS visitors
			  FROM v WHERE channel = $3 AND kind = $4
			 GROUP BY work_id, entity_id, slug_key
			 ORDER BY views DESC, work_id, entity_id, slug_key LIMIT $5
		)
		SELECT t.work_id, t.entity_id, t.slug_key, t.views, t.visitors,
		       coalesce(CASE $4
		           WHEN 'chapter'    THEN c.title
		           WHEN 'edition'    THEN e.title
		           WHEN 'concept'    THEN ic.title
		           WHEN 'document'   THEN d.title
		           WHEN 'collection' THEN col.title
		           ELSE w.title END, '') AS title,
		       coalesce(w.title, '') AS work_title,
		       CASE $4
		           WHEN 'chapter'    THEN c.id IS NULL
		           WHEN 'edition'    THEN e.id IS NULL
		           WHEN 'concept'    THEN ic.id IS NULL
		           WHEN 'document'   THEN d.id IS NULL
		           WHEN 'collection' THEN col.id IS NULL
		           WHEN 'work'       THEN w.id IS NULL
		           WHEN 'page'       THEN w.id IS NULL
		           WHEN 'read'       THEN w.id IS NULL
		           ELSE false END AS missing
		  FROM t
		  LEFT JOIN works w           ON w.id = t.work_id AND t.work_id <> 0
		  LEFT JOIN chapters c        ON $4 = 'chapter' AND c.id = t.entity_id
		  LEFT JOIN editions e        ON $4 = 'edition' AND e.id = t.entity_id
		  LEFT JOIN index_concepts ic ON $4 = 'concept' AND ic.slug = t.slug_key
		  LEFT JOIN documents d       ON $4 = 'document' AND
		       CASE WHEN d.author_nickname = '' THEN d.slug ELSE d.author_nickname || '/' || d.slug END = t.slug_key
		  LEFT JOIN collections col   ON $4 = 'collection' AND
		       CASE WHEN col.author_nickname = '' THEN col.slug ELSE col.author_nickname || '/' || col.slug END = t.slug_key
		 ORDER BY t.views DESC, t.work_id, t.entity_id, t.slug_key`, from, today, channel, kind, limit)
	if err != nil {
		return nil, fmt.Errorf("топ %s: %w", kind, err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (TopRow, error) {
		var t TopRow
		err := row.Scan(&t.WorkID, &t.EntityID, &t.SlugKey, &t.Views, &t.Visitors, &t.Title, &t.WorkTitle, &t.Missing)
		return t, err
	})
}

func (r *StatsRepository) Referrers(ctx context.Context, from, today time.Time, limit int) ([]CountRow, error) {
	rows, err := r.pool.Query(ctx, dailyUnion+`
		SELECT ref_host, sum(views)::int, sum(visitors)::int FROM v
		 WHERE channel = 'spa' AND ref_host <> ''
		 GROUP BY ref_host ORDER BY 2 DESC LIMIT $3`, from, today, limit)
	if err != nil {
		return nil, fmt.Errorf("источники: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (CountRow, error) {
		var c CountRow
		err := row.Scan(&c.Key, &c.Views, &c.Visitors)
		return c, err
	})
}

func (r *StatsRepository) Searches(ctx context.Context, from, today time.Time, limit int) (*SearchStats, error) {
	scan := func(q string) ([]SearchRow, error) {
		rows, err := r.pool.Query(ctx, dailyUnion+q, from, today, limit)
		if err != nil {
			return nil, fmt.Errorf("поисковые запросы: %w", err)
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (SearchRow, error) {
			var s SearchRow
			err := row.Scan(&s.Query, &s.Count, &s.ZeroHits)
			return s, err
		})
	}
	const base = `SELECT query, sum(views)::int, sum(zero_hits)::int FROM v
		 WHERE kind = 'search' AND channel = 'search' AND agent = '' AND query <> '' GROUP BY query`
	freq, err := scan(base + ` ORDER BY 2 DESC LIMIT $3`)
	if err != nil {
		return nil, err
	}
	zero, err := scan(base + ` HAVING sum(zero_hits) > 0 ORDER BY 3 DESC LIMIT $3`)
	if err != nil {
		return nil, err
	}
	return &SearchStats{Frequent: freq, Zero: zero}, nil
}

func (r *StatsRepository) Crawlers(ctx context.Context, from, today time.Time) ([]CrawlerRow, error) {
	rows, err := r.pool.Query(ctx, dailyUnion+`
		SELECT day, agent, sum(views)::int FROM v
		 WHERE channel IN ('seo', 'md') AND agent <> ''
		 GROUP BY day, agent ORDER BY day, 3 DESC`, from, today)
	if err != nil {
		return nil, fmt.Errorf("краулеры: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (CrawlerRow, error) {
		var c CrawlerRow
		err := row.Scan(&c.Day, &c.Agent, &c.Views)
		return c, err
	})
}

// Devices — канал spa по суткам и классу ширины окна: свёртка до последних
// свёрнутых суток, дальше сырые строки — тот же приём, что у Traffic.
func (r *StatsRepository) Devices(ctx context.Context, from, today time.Time) ([]DeviceRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT day, device, views, visitors FROM page_views_daily_devices
		 WHERE day >= $1 AND day <= `+rolledDay+`
		UNION ALL
		SELECT day, device, count(*)::int, (count(DISTINCT visitor) FILTER (WHERE visitor <> ''))::int
		  FROM page_views WHERE channel = 'spa' AND day >= $1 AND day > `+rolledDay+` AND day <= $2
		 GROUP BY day, device
		ORDER BY 1, 2`, from, today)
	if err != nil {
		return nil, fmt.Errorf("устройства: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (DeviceRow, error) {
		var d DeviceRow
		err := row.Scan(&d.Day, &d.Device, &d.Views, &d.Visitors)
		return d, err
	})
}
