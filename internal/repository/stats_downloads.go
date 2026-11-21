package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type DownloadSummaryRow struct {
	Kind   string `json:"kind"`
	Format string `json:"format"`
	Views  int    `json:"views"`
}

type DownloadTopRow struct {
	Kind     string `json:"kind"`
	WorkID   int64  `json:"work_id"`
	EntityID int64  `json:"entity_id"`
	Format   string `json:"format"`
	Title    string `json:"title"`
	Missing  bool   `json:"missing"`
	Views    int    `json:"views"`
}

type DownloadStats struct {
	Summary []DownloadSummaryRow `json:"summary"`
	Top     []DownloadTopRow     `json:"top"`
}

// Downloads читает историческую свёртку и несвёрнутые сутки через ту же
// границу, что и остальные экраны статистики. Подборки имеют только формат,
// поэтому в топ конкретных объектов не входят.
func (r *StatsRepository) Downloads(ctx context.Context, from, today time.Time, limit int) (*DownloadStats, error) {
	result := &DownloadStats{Summary: []DownloadSummaryRow{}, Top: []DownloadTopRow{}}
	rows, err := r.pool.Query(ctx, dailyUnion+`
		SELECT kind, slug_key, sum(views)::int FROM v
		 WHERE channel='download' GROUP BY kind, slug_key ORDER BY 3 DESC, kind, slug_key`, from, today)
	if err != nil {
		return nil, fmt.Errorf("итоги выдач: %w", err)
	}
	result.Summary, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (DownloadSummaryRow, error) {
		var s DownloadSummaryRow
		err := row.Scan(&s.Kind, &s.Format, &s.Views)
		return s, err
	})
	if err != nil {
		return nil, fmt.Errorf("итоги выдач: %w", err)
	}
	rows, err = r.pool.Query(ctx, dailyUnion+`
		, grouped AS (
			SELECT kind, work_id, entity_id, slug_key, sum(views)::int AS views
			  FROM v WHERE channel='download' AND kind IN ('work','chapter')
			 GROUP BY kind, work_id, entity_id, slug_key
			 ORDER BY views DESC, kind, work_id, entity_id, slug_key LIMIT $3
		)
		SELECT g.kind, g.work_id, g.entity_id, g.slug_key,
		       coalesce(CASE WHEN g.kind='chapter' THEN c.title ELSE w.title END, ''),
		       CASE WHEN g.kind='chapter' THEN c.id IS NULL ELSE w.id IS NULL END,
		       g.views
		  FROM grouped g
		  LEFT JOIN works w ON w.id=g.work_id
		  LEFT JOIN chapters c ON g.kind='chapter' AND c.id=g.entity_id AND c.work_id=g.work_id
		 ORDER BY g.views DESC, g.kind, g.work_id, g.entity_id, g.slug_key`, from, today, limit)
	if err != nil {
		return nil, fmt.Errorf("топ выдач: %w", err)
	}
	result.Top, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (DownloadTopRow, error) {
		var v DownloadTopRow
		err := row.Scan(&v.Kind, &v.WorkID, &v.EntityID, &v.Format, &v.Title, &v.Missing, &v.Views)
		return v, err
	})
	if err != nil {
		return nil, fmt.Errorf("топ выдач: %w", err)
	}
	return result, nil
}
