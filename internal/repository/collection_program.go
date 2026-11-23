package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"proofreader/internal/models"
)

// ErrCollectionCompositionChanged означает, что список для записи программы
// устарел или не содержит каждый текущий элемент подборки ровно один раз.
var ErrCollectionCompositionChanged = errors.New("collection composition changed")

// Все операции, меняющие порядок или набор элементов, берут этот замок
// прежде, чем трогать строки состава.
func lockCollectionComposition(ctx context.Context, tx pgx.Tx, id int64) (int64, error) {
	var revision int64
	err := tx.QueryRow(ctx, `
		SELECT program_revision FROM collections WHERE id = $1 FOR NO KEY UPDATE`, id).Scan(&revision)
	return revision, err
}

func advanceCollectionRevision(ctx context.Context, tx pgx.Tx, id int64) error {
	_, err := tx.Exec(ctx, `UPDATE collections SET program_revision = program_revision + 1 WHERE id = $1`, id)
	return err
}

type CollectionComposition struct {
	Revision int64
	Meetings []models.CollectionMeeting
	Items    []ItemRow
}

// Composition читает занятия и состав из одного снимка, чтобы перестановка
// занятий между запросами не связала текст с чужим занятием.
func (r *CollectionRepository) Composition(ctx context.Context, id int64) (CollectionComposition, error) {
	var result CollectionComposition
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	err = tx.QueryRow(ctx, `
		SELECT program_revision, (
			SELECT jsonb_agg(row_to_json(m) ORDER BY m.position)
			FROM (SELECT position, title, question FROM collection_meetings WHERE collection_id = $1) m
		)
		FROM collections WHERE id = $1`, id).Scan(&result.Revision, &result.Meetings)
	if err != nil {
		return result, err
	}
	result.Items, err = queryCollectionItemRows(ctx, tx, id)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

// SaveProgram записывает занятия, их состав и общий порядок одним действием.
// ID элементов остаются прежними. Тексты вне занятий идут после программы.
func (r *CollectionRepository) SaveProgram(ctx context.Context, id, expectedRevision int64, meetings []models.CollectionMeetingInput, unassigned []int64) error {
	titles := make([]string, 0, len(meetings))
	questions := make([]string, 0, len(meetings))
	itemIDs := make([]int64, 0)
	positions := make([]int32, 0)
	for i, meeting := range meetings {
		titles = append(titles, meeting.Title)
		questions = append(questions, meeting.Question)
		for _, itemID := range meeting.ItemIDs {
			itemIDs = append(itemIDs, itemID)
			positions = append(positions, int32(i+1))
		}
	}
	for _, itemID := range unassigned {
		itemIDs = append(itemIDs, itemID)
		positions = append(positions, 0)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	revision, err := lockCollectionComposition(ctx, tx, id)
	if err != nil {
		return err
	}
	if revision != expectedRevision {
		return ErrCollectionCompositionChanged
	}

	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM collection_items WHERE collection_id = $1`, id).Scan(&count); err != nil {
		return err
	}
	if count != len(itemIDs) {
		return ErrCollectionCompositionChanged
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO collection_meetings (collection_id, position, title, question)
		SELECT $1, position, title, question
		FROM unnest($2::text[], $3::text[]) WITH ORDINALITY AS m(title, question, position)
		ON CONFLICT (collection_id, position) DO UPDATE
		    SET title = EXCLUDED.title, question = EXCLUDED.question`, id, titles, questions); err != nil {
		return err
	}

	result, err := tx.Exec(ctx, `
		UPDATE collection_items ci
		SET meeting_position = NULLIF(m.position, 0), order_number = m.reading_order
		FROM unnest($2::bigint[], $3::int[]) WITH ORDINALITY AS m(item_id, position, reading_order)
		WHERE ci.collection_id = $1 AND ci.id = m.item_id`, id, itemIDs, positions)
	if err != nil {
		return err
	}
	// UPDATE затрагивает каждую строку максимум раз, даже при повторах во входе.
	if result.RowsAffected() != int64(count) {
		return ErrCollectionCompositionChanged
	}

	if _, err := tx.Exec(ctx, `DELETE FROM collection_meetings WHERE collection_id = $1 AND position > $2`, id, len(meetings)); err != nil {
		return err
	}
	if err := advanceCollectionRevision(ctx, tx, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
