package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

type ReadLaterEntry struct {
	WorkID    int64  `json:"work_id"`
	ChapterID *int64 `json:"chapter_id,omitempty"`
	Title     string `json:"title"`
	Source    string `json:"source"`
}

type ReadLaterRepository struct {
	pool *pgxpool.Pool
}

func NewReadLaterRepository(pool *pgxpool.Pool) *ReadLaterRepository {
	return &ReadLaterRepository{pool: pool}
}

// Глава хранится по своему id. Текущий том, журнал и их признаки
// видимости вычисляются при запросе, в том числе после переноса главы.
const readLaterVisible = `
 AND ($2 OR (
  NOT EXISTS (SELECT 1 FROM closed_works cw WHERE cw.id=w.id)
  AND NOT EXISTS (SELECT 1 FROM closed_chapters cc WHERE cc.id=c.id)
 ))`

func (r *ReadLaterRepository) List(ctx context.Context, ownerID int64, includeClosed bool) ([]ReadLaterEntry, error) {
	rows, err := r.pool.Query(ctx, `
  SELECT w.id, c.id, COALESCE(c.title,w.title),
   CASE WHEN c.id IS NOT NULL THEN w.title ELSE COALESCE(w.author,'') END,
   COALESCE(e.title,''),
   CASE WHEN e.volume_numbering <> 'order' AND w.volume_number IS NOT NULL
    THEN concat_ws(', ', 'т. ' || w.volume_number, NULLIF(w.volume_part,'')) ELSE '' END,
   COALESCE(j.title,''), COALESCE(ji.year,0), COALESCE(ji.label,'')
  FROM read_later saved
  LEFT JOIN chapters c ON c.id=saved.chapter_id
  JOIN works w ON w.id=COALESCE(saved.work_id,c.work_id)
  LEFT JOIN editions e ON e.id=w.edition_id
  LEFT JOIN journal_issues ji ON ji.work_id=w.id
  LEFT JOIN journals j ON j.id=ji.journal_id
  WHERE saved.owner_id=$1`+readLaterVisible+`
  ORDER BY saved.created_at DESC,saved.id DESC`, ownerID, includeClosed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]ReadLaterEntry, 0)
	for rows.Next() {
		var entry ReadLaterEntry
		var workContext, edition, volume, journal, issue string
		var year int
		if err := rows.Scan(&entry.WorkID, &entry.ChapterID, &entry.Title, &workContext,
			&edition, &volume, &journal, &year, &issue); err != nil {
			return nil, err
		}
		if year != 0 {
			entry.Source = models.IssueTitle(journal, year, issue)
		} else {
			parts := make([]string, 0, 3)
			for _, text := range []string{workContext, edition, volume} {
				if text != "" && text != entry.Title {
					parts = append(parts, text)
				}
			}
			entry.Source = strings.Join(parts, " · ")
			if entry.Source == "" {
				entry.Source = "Том"
			}
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (r *ReadLaterRepository) Save(ctx context.Context, ownerID int64, includeClosed, chapter bool, id int64) (bool, error) {
	var available bool
	err := r.pool.QueryRow(ctx, `
  WITH target AS (
   SELECT CASE WHEN NOT $3 THEN w.id END AS work_id, c.id AS chapter_id
   FROM works w
   LEFT JOIN chapters c ON $3 AND c.id=$4 AND c.work_id=w.id
   WHERE (CASE WHEN $3 THEN c.id ELSE w.id END)=$4`+readLaterVisible+`
  ), saved AS (
   INSERT INTO read_later (owner_id,work_id,chapter_id)
   SELECT $1,work_id,chapter_id FROM target
   ON CONFLICT (owner_id,work_id,chapter_id) DO NOTHING
  )
  SELECT EXISTS (SELECT 1 FROM target)`, ownerID, includeClosed, chapter, id).Scan(&available)
	// Удаление источника могло завершиться между снимком запроса и проверкой FK.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" &&
		(pgErr.ConstraintName == "read_later_work_id_fkey" || pgErr.ConstraintName == "read_later_chapter_id_fkey") {
		return false, nil
	}
	return available, err
}

func (r *ReadLaterRepository) Remove(ctx context.Context, ownerID int64, chapter bool, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM read_later
  WHERE owner_id=$1 AND (CASE WHEN $2 THEN chapter_id ELSE work_id END)=$3`, ownerID, chapter, id)
	return err
}
