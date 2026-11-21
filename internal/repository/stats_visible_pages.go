package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"proofreader/internal/stats"
)

var ErrVisibleChapterNotFound = errors.New("chapter not found in work")

type VisiblePagePoint struct {
	PageNumber int `json:"page_number"`
	Visitors   int `json:"visitors"`
}

type VisiblePageStats struct {
	CollectingSince *time.Time         `json:"collecting_since"`
	Pages           []VisiblePagePoint `json:"pages"`
}

func (r *StatsRepository) InsertVisiblePages(ctx context.Context, rows []stats.VisiblePageRow) error {
	if len(rows) == 0 {
		return nil
	}
	_, err := r.pool.CopyFrom(ctx, pgx.Identifier{"visible_page_views"},
		[]string{"day", "ts", "work_id", "page_number", "visitor"},
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			v := rows[i]
			return []any{v.Day, v.TS, v.WorkID, v.PageNumber, v.Visitor}, nil
		}))
	if err != nil {
		return fmt.Errorf("запись видимых полос: %w", err)
	}
	return nil
}

// rollupVisiblePages вызывается внутри общей транзакции с остальной свёрткой.
func rollupVisiblePages(ctx context.Context, tx pgx.Tx, today time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO visible_page_views_daily (day, work_id, page_number, visitors)
		SELECT v.day, v.work_id, v.page_number, count(DISTINCT v.visitor)::int
		  FROM visible_page_views v
		  JOIN pages p ON p.work_id = v.work_id AND p.page_number = v.page_number
		 WHERE v.day < $1 AND v.day >= coalesce(
		       (SELECT max(day) FROM visible_page_views_daily), '-infinity'::date)
		   AND v.visitor <> ''
		 GROUP BY v.day, v.work_id, v.page_number
		ON CONFLICT (day, work_id, page_number) DO UPDATE SET visitors = EXCLUDED.visitors`, today)
	if err != nil {
		return fmt.Errorf("свёртка видимых полос: %w", err)
	}
	return nil
}

func (r *StatsRepository) VisiblePages(ctx context.Context, workID, chapterID int64, from, today time.Time) (*VisiblePageStats, error) {
	var start, end int
	if err := r.pool.QueryRow(ctx, `SELECT start_page, end_page FROM chapters WHERE id=$1 AND work_id=$2`, chapterID, workID).Scan(&start, &end); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrVisibleChapterNotFound
		}
		return nil, fmt.Errorf("глава для статистики: %w", err)
	}
	result := &VisiblePageStats{Pages: []VisiblePagePoint{}}
	// Отсчёт сбора по всему тому: отсутствие событий в выбранной главе
	// после запуска не означает, что сбор ещё не включён.
	if err := r.pool.QueryRow(ctx, `
		SELECT min(day) FROM (
			SELECT d.day FROM visible_page_views_daily d JOIN pages p ON p.work_id=d.work_id AND p.page_number=d.page_number WHERE d.work_id=$1
			UNION ALL
			SELECT v.day FROM visible_page_views v JOIN pages p ON p.work_id=v.work_id AND p.page_number=v.page_number WHERE v.work_id=$1 AND v.visitor<>''
		) valid`, workID).Scan(&result.CollectingSince); err != nil {
		return nil, fmt.Errorf("начало сбора полос: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
		WITH rolled AS (
			SELECT coalesce(max(day), '-infinity'::date) AS day FROM visible_page_views_daily WHERE day < $2
		), counts AS (
			SELECT d.page_number, sum(d.visitors)::int AS visitors
			  FROM visible_page_views_daily d, rolled
			 WHERE d.work_id=$3 AND d.day >= $1 AND d.day <= rolled.day
			 GROUP BY d.page_number
			 UNION ALL
			 SELECT v.page_number, count(DISTINCT (v.day, v.visitor))::int
			  FROM visible_page_views v, rolled
			 WHERE v.work_id=$3 AND v.day >= $1 AND v.day > rolled.day AND v.day <= $2 AND v.visitor<>''
			 GROUP BY v.page_number
		)
		SELECT p.page_number, coalesce(sum(c.visitors),0)::int
		  FROM pages p LEFT JOIN counts c ON c.page_number=p.page_number
		 WHERE p.work_id=$3 AND p.page_number BETWEEN $4 AND $5
		 GROUP BY p.page_number ORDER BY p.page_number`, from, today, workID, start, end)
	if err != nil {
		return nil, fmt.Errorf("распределение полос: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p VisiblePagePoint
		if err := rows.Scan(&p.PageNumber, &p.Visitors); err != nil {
			return nil, err
		}
		result.Pages = append(result.Pages, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
