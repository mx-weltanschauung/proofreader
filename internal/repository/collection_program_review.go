package repository

import (
	"context"
	"errors"
	"hash/fnv"
	"time"

	"github.com/jackc/pgx/v5"

	"proofreader/internal/models"
)

var (
	ErrCollectionProgramReviewRequired = errors.New("collection program requires review")
	ErrCollectionProgramPending        = errors.New("collection program is pending")
	ErrCollectionProgramPublished      = errors.New("collection program is published")
	ErrCollectionProgramNotDraft       = errors.New("collection is not a reader program draft")
	ErrCollectionProgramNotPending     = errors.New("this program revision is not pending")
)

type collectionProgramState struct {
	revision  int64
	reader    bool
	published bool
	status    string
}

// Правки, публикация и рассмотрение используют одну блокировку строки.
func lockCollectionProgram(ctx context.Context, tx pgx.Tx, id int64) (collectionProgramState, error) {
	var p collectionProgramState
	err := tx.QueryRow(ctx, `
		SELECT program_revision, author_nickname <> '', published_at IS NOT NULL, program_review_status
		FROM collections WHERE id = $1 FOR NO KEY UPDATE`, id).
		Scan(&p.revision, &p.reader, &p.published, &p.status)
	return p, err
}

// Проверяем наличие занятий уже под блокировкой, отдельным запросом.
func collectionHasMeetings(ctx context.Context, tx pgx.Tx, id int64) (bool, error) {
	var has bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM collection_meetings WHERE collection_id = $1)`, id).Scan(&has)
	return has, err
}

func lockCollectionComposition(ctx context.Context, tx pgx.Tx, id int64, addingProgram bool) (int64, error) {
	p, err := lockCollectionProgram(ctx, tx, id)
	if err != nil {
		return 0, err
	}
	if !p.reader {
		return p.revision, nil
	}
	if p.status == models.CollectionProgramPending {
		return 0, ErrCollectionProgramPending
	}
	if p.published {
		hasMeetings, err := collectionHasMeetings(ctx, tx, id)
		if err != nil {
			return 0, err
		}
		if hasMeetings || addingProgram {
			return 0, ErrCollectionProgramPublished
		}
	}
	return p.revision, nil
}

// Отметку адреса блокируем до строки подборки, чтобы счёт и запись шли вместе.
func lockCollectionPublication(ctx context.Context, tx pgx.Tx, prefix, ipHash string) error {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(prefix + ipHash))
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(hasher.Sum64()))
	return err
}

func (r *CollectionRepository) SubmitProgram(
	ctx context.Context, id, revision int64, ipHash string, limit int, since time.Time,
) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockCollectionPublication(ctx, tx, "collection_programs:", ipHash); err != nil {
		return false, err
	}
	p, err := lockCollectionProgram(ctx, tx, id)
	if err != nil {
		return false, err
	}
	if p.status == models.CollectionProgramPending {
		return false, ErrCollectionProgramPending
	}
	hasMeetings, err := collectionHasMeetings(ctx, tx, id)
	if err != nil {
		return false, err
	}
	if !p.reader || p.published || !hasMeetings {
		return false, ErrCollectionProgramNotDraft
	}
	if p.revision != revision {
		return false, ErrCollectionCompositionChanged
	}
	var recent int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM collection_program_submissions
		WHERE ip_hash = $1 AND submitted_at > $2`, ipHash, since).Scan(&recent); err != nil {
		return false, err
	}
	if recent >= limit {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO collection_program_submissions (ip_hash) VALUES ($1)`, ipHash); err != nil {
		return false, err
	}

	if _, err := tx.Exec(ctx, `UPDATE collections
		SET program_review_status = 'pending', program_reject_reason = '',
		    program_submitted_at = now(), program_submit_ip_hash = $2, program_revision = program_revision + 1
		WHERE id = $1`, id, ipHash); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (r *CollectionRepository) ProgramsForReview(ctx context.Context) ([]models.CollectionProgramReviewEntry, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, title, slug, author_nickname, program_revision, program_submitted_at
		FROM collections
		WHERE program_review_status = 'pending' AND owner_id IS NOT NULL
		ORDER BY program_submitted_at, id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[models.CollectionProgramReviewEntry])
}

// ResolveProgram публикует при пустой причине или возвращает программу автору.
// Условие на версию исключает решение после отзыва или повторной подачи.
func (r *CollectionRepository) ResolveProgram(ctx context.Context, id, revision int64, reason string) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE collections
		SET program_review_status = CASE WHEN $3 = '' THEN '' ELSE 'rejected' END,
		    program_reject_reason = $3,
		    published_at = CASE WHEN $3 = '' THEN now() ELSE NULL END,
		    publish_ip_hash = CASE WHEN $3 = '' THEN program_submit_ip_hash ELSE publish_ip_hash END,
		    program_revision = program_revision + 1
		WHERE id = $1 AND program_revision = $2 AND program_review_status = 'pending'
		  AND owner_id IS NOT NULL AND author_nickname <> ''`, id, revision, reason)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrCollectionProgramNotPending
	}
	return nil
}
