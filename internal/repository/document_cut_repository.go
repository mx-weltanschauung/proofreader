package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

// DocumentCutRepository handles document-cut (вклейка) data access.
type DocumentCutRepository struct {
	pool *pgxpool.Pool
}

// NewDocumentCutRepository creates a new document-cut repository.
func NewDocumentCutRepository(pool *pgxpool.Pool) *DocumentCutRepository {
	return &DocumentCutRepository{pool: pool}
}

const documentCutColumns = `
	id, document_id, work_id,
	start_page_id, start_offset, end_page_id, end_offset,
	head_quote, tail_quote, start_hash, end_hash, status, source_title, created_at
`

// pageColumns/scanPages — свой скан полос вклейки: PageRepository не выносит
// сканирование отдельной функцией, оно встроено в каждый его метод.
const pageColumns = `
	id, work_id, page_number, preview_path, content_markdown, status, chapter_id, created_at, updated_at
`

func scanPages(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]*models.Page, error) {
	var out []*models.Page
	for rows.Next() {
		var p models.Page
		if err := rows.Scan(
			&p.ID, &p.WorkID, &p.PageNumber, &p.PreviewPath, &p.ContentMarkdown,
			&p.Status, &p.ChapterID, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan page: %w", err)
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

// nullableID — 0 в Anchor значит «полосы нет» (см. DocumentCut.Broken()); в
// столбец обязана идти NULL, а не несуществующий id 0.
func nullableID(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}

// scanCuts сканирует document_cuts. work_id/start_page_id/end_page_id в базе
// nullable: полосы читаются во временные *int64 и при NULL остаются нулевым
// значением в Anchor — тем же, на которое смотрит Broken().
func scanCuts(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]*models.DocumentCut, error) {
	var out []*models.DocumentCut
	for rows.Next() {
		var c models.DocumentCut
		var startPageID, endPageID *int64
		if err := rows.Scan(
			&c.ID, &c.DocumentID, &c.WorkID,
			&startPageID, &c.StartOffset, &endPageID, &c.EndOffset,
			&c.HeadQuote, &c.TailQuote, &c.StartHash, &c.EndHash,
			&c.Status, &c.SourceTitle, &c.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan document cut: %w", err)
		}
		if startPageID != nil {
			c.StartPageID = *startPageID
		}
		if endPageID != nil {
			c.EndPageID = *endPageID
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

// ByDocument returns all cuts of one document, in insertion order.
func (r *DocumentCutRepository) ByDocument(ctx context.Context, documentID int64) ([]*models.DocumentCut, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+documentCutColumns+`
		FROM document_cuts
		WHERE document_id = $1
		ORDER BY id
	`, documentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list cuts of document %d: %w", documentID, err)
	}
	defer rows.Close()
	return scanCuts(rows)
}

// ByIDs returns the cuts of one document among the given ids.
//
// Проверка принадлежности, а не удобство: чужую вклейку по id достать
// нельзя, документ обязан совпасть.
func (r *DocumentCutRepository) ByIDs(ctx context.Context, documentID int64, ids []int64) ([]*models.DocumentCut, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+documentCutColumns+`
		FROM document_cuts
		WHERE document_id = $1 AND id = ANY($2)
		ORDER BY id
	`, documentID, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to list cuts %v of document %d: %w", ids, documentID, err)
	}
	defer rows.Close()
	return scanCuts(rows)
}

// ByPage returns the cuts that start or end on the given page.
//
// Обе стороны, а не только начало: вклейка через границу страниц держится
// за одну полосу головной цитатой, за другую — хвостовой, и правка любой из
// них требует переякоривания.
func (r *DocumentCutRepository) ByPage(ctx context.Context, pageID int64) ([]*models.DocumentCut, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+documentCutColumns+`
		FROM document_cuts
		WHERE start_page_id = $1 OR end_page_id = $1
		ORDER BY id
	`, pageID)
	if err != nil {
		return nil, fmt.Errorf("failed to list cuts by page %d: %w", pageID, err)
	}
	defer rows.Close()
	return scanCuts(rows)
}

// Create inserts a new cut and fills its id and created_at.
func (r *DocumentCutRepository) Create(ctx context.Context, cut *models.DocumentCut) error {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO document_cuts
			(document_id, work_id, start_page_id, start_offset, end_page_id, end_offset,
			 head_quote, tail_quote, start_hash, end_hash, status, source_title)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, created_at
	`,
		cut.DocumentID, cut.WorkID,
		nullableID(cut.StartPageID), cut.StartOffset,
		nullableID(cut.EndPageID), cut.EndOffset,
		cut.HeadQuote, cut.TailQuote, cut.StartHash, cut.EndHash,
		cut.Status, cut.SourceTitle,
	).Scan(&cut.ID, &cut.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create document cut: %w", err)
	}
	return nil
}

// DeleteUnreferenced сносит вклейки разбора, которых больше не называет его
// тело. Пустой keep сносит все — законный случай: автор убрал последнюю (тело
// без единого тега <cut> и даёт keep == nil из сборки мусора).
//
// NOT (id = ANY($2)) на пустом СРЕЗЕ даёт true, что и нужно; но nil-срез pgx
// кодирует в параметр-массив как NULL, а не как пустой массив — тогда
// id = ANY(NULL) даёт NULL, NOT (NULL) тоже NULL, и условие WHERE не
// выполняется ни для одной строки: «сносит все» тихо превращается в «не
// сносит ничего». Поэтому nil приводится к пустому срезу явно, до запроса —
// это не перестраховка, а единственный способ отличить «сносить всё» от
// «неизвестно что сносить» на уровне SQL.
func (r *DocumentCutRepository) DeleteUnreferenced(ctx context.Context, documentID int64, keep []int64) error {
	if keep == nil {
		keep = []int64{}
	}
	if _, err := r.pool.Exec(ctx, `
		DELETE FROM document_cuts
		WHERE document_id = $1 AND NOT (id = ANY($2))
	`, documentID, keep); err != nil {
		return fmt.Errorf("failed to prune cuts of document %d: %w", documentID, err)
	}
	return nil
}

// UpdateAnchor moves a cut to new offsets after its page was edited.
func (r *DocumentCutRepository) UpdateAnchor(ctx context.Context, id int64, startOffset, endOffset int, startHash, endHash string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE document_cuts
		SET start_offset = $2, end_offset = $3, start_hash = $4, end_hash = $5
		WHERE id = $1
	`, id, startOffset, endOffset, startHash, endHash)
	if err != nil {
		return fmt.Errorf("failed to update document cut anchor: %w", err)
	}
	return nil
}

// SetStatus changes a cut's status.
func (r *DocumentCutRepository) SetStatus(ctx context.Context, id int64, status string) error {
	if _, err := r.pool.Exec(ctx, `UPDATE document_cuts SET status = $2 WHERE id = $1`, id, status); err != nil {
		return fmt.Errorf("failed to set document cut status: %w", err)
	}
	return nil
}

// PagesOfCut отдаёт полосы вклейки от начальной до конечной включительно, в
// порядке чтения. Битая вклейка отдаёт пустой срез без ошибки — показывать
// её будет снимок подписи.
func (r *DocumentCutRepository) PagesOfCut(ctx context.Context, cut *models.DocumentCut) ([]*models.Page, error) {
	if cut.Broken() {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		WITH bounds AS (
			SELECT
				(SELECT page_number FROM pages WHERE id = $2) AS lo,
				(SELECT page_number FROM pages WHERE id = $3) AS hi
		)
		SELECT `+pageColumns+`
		FROM pages, bounds
		WHERE work_id = $1
		  AND page_number BETWEEN LEAST(bounds.lo, bounds.hi) AND GREATEST(bounds.lo, bounds.hi)
		ORDER BY page_number
	`, *cut.WorkID, cut.StartPageID, cut.EndPageID)
	if err != nil {
		return nil, fmt.Errorf("failed to list pages of cut %d: %w", cut.ID, err)
	}
	defer rows.Close()
	return scanPages(rows)
}
