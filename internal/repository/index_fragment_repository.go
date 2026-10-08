package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

// IndexFragmentRepository handles concept fragment (cut) data access.
type IndexFragmentRepository struct {
	pool *pgxpool.Pool
}

// NewIndexFragmentRepository creates a new fragment repository.
func NewIndexFragmentRepository(pool *pgxpool.Pool) *IndexFragmentRepository {
	return &IndexFragmentRepository{pool: pool}
}

const fragmentColumns = `
	id, reference_id, order_number,
	start_page_id, start_offset, end_page_id, end_offset,
	head_quote, tail_quote, start_hash, end_hash, status
`

func scanFragments(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]*models.IndexFragment, error) {
	var out []*models.IndexFragment
	for rows.Next() {
		var f models.IndexFragment
		if err := rows.Scan(
			&f.ID, &f.ReferenceID, &f.OrderNumber,
			&f.StartPageID, &f.StartOffset, &f.EndPageID, &f.EndOffset,
			&f.HeadQuote, &f.TailQuote, &f.StartHash, &f.EndHash, &f.Status,
		); err != nil {
			return nil, fmt.Errorf("failed to scan fragment: %w", err)
		}
		out = append(out, &f)
	}
	return out, rows.Err()
}

// ByReferences returns the fragments of the given references, grouped by
// reference and ordered inside each group.
//
// Пустой список адресов возвращает пустую карту без запроса: порция потока
// может целиком состоять из ненарезанных адресов.
func (r *IndexFragmentRepository) ByReferences(ctx context.Context, referenceIDs []int64) (map[int64][]*models.IndexFragment, error) {
	out := map[int64][]*models.IndexFragment{}
	if len(referenceIDs) == 0 {
		return out, nil
	}

	rows, err := r.pool.Query(ctx, `
		SELECT `+fragmentColumns+`
		FROM index_fragments
		WHERE reference_id = ANY($1)
		ORDER BY reference_id, order_number
	`, referenceIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to list fragments by references: %w", err)
	}
	defer rows.Close()

	list, err := scanFragments(rows)
	if err != nil {
		return nil, err
	}
	for _, f := range list {
		out[f.ReferenceID] = append(out[f.ReferenceID], f)
	}
	return out, nil
}

// ByPage returns the fragments that start or end on the given page.
//
// Обе стороны, а не только начало: вырезка через границу страниц держится
// за 730-ю головной цитатой и за 731-ю хвостовой, и правка любой из них
// требует переякоривания.
func (r *IndexFragmentRepository) ByPage(ctx context.Context, pageID int64) ([]*models.IndexFragment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+fragmentColumns+`
		FROM index_fragments
		WHERE start_page_id = $1 OR end_page_id = $1
		ORDER BY id
	`, pageID)
	if err != nil {
		return nil, fmt.Errorf("failed to list fragments by page: %w", err)
	}
	defer rows.Close()

	return scanFragments(rows)
}

// ReplaceForReference replaces the whole set of cuts of one address.
//
// Замена целиком, а не досыл: скилл гоняется повторно, и правка человека
// приходит полным набором. order_number проставляется по порядку в срезе.
func (r *IndexFragmentRepository) ReplaceForReference(ctx context.Context, referenceID int64, fragments []*models.IndexFragment) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin fragment replace: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM index_fragments WHERE reference_id = $1`, referenceID); err != nil {
		return fmt.Errorf("failed to clear fragments: %w", err)
	}

	for i, f := range fragments {
		if _, err := tx.Exec(ctx, `
			INSERT INTO index_fragments
				(reference_id, order_number, start_page_id, start_offset,
				 end_page_id, end_offset, head_quote, tail_quote,
				 start_hash, end_hash, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`, referenceID, i+1, f.StartPageID, f.StartOffset,
			f.EndPageID, f.EndOffset, f.HeadQuote, f.TailQuote,
			f.StartHash, f.EndHash, f.Status); err != nil {
			return fmt.Errorf("failed to insert fragment: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit fragment replace: %w", err)
	}
	return nil
}

// UpdateAnchor moves a fragment to new offsets after its page was edited.
func (r *IndexFragmentRepository) UpdateAnchor(ctx context.Context, id int64, startOffset, endOffset int, startHash, endHash string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE index_fragments
		SET start_offset = $2, end_offset = $3, start_hash = $4, end_hash = $5
		WHERE id = $1
	`, id, startOffset, endOffset, startHash, endHash)
	if err != nil {
		return fmt.Errorf("failed to update fragment anchor: %w", err)
	}
	return nil
}

// SetStatus changes a fragment's status.
func (r *IndexFragmentRepository) SetStatus(ctx context.Context, id int64, status string) error {
	if _, err := r.pool.Exec(ctx, `UPDATE index_fragments SET status = $2 WHERE id = $1`, id, status); err != nil {
		return fmt.Errorf("failed to set fragment status: %w", err)
	}
	return nil
}
