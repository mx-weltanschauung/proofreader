package repository

import (
	"context"
	"errors"
	"fmt"

	"proofreader/internal/models"
	"proofreader/pkg/slug"
)

// ErrHighlightForeignChapter — в списке избранного глава не из этого собрания
// (или её нет вовсе). Иначе витрину Маркса можно было бы набить главами Ленина.
var ErrHighlightForeignChapter = errors.New("chapter does not belong to the edition")

// highlightsQueryTemplate: таблица works под своим именем, а не алиасом, —
// workSlugColumns написан против `works.`. Координаты тома берутся из тех же
// колонок слага: у служебных передних листов они наследуются от родителя.
// Условие works.edition_id = h.edition_id — то же, что проверка при записи:
// том, переехавший в другое собрание, свою витрину в старом больше не держит.
const highlightsQueryTemplate = `
SELECT h.edition_id, h.chapter_id, c.title, works.id, works.title, works.role,
       works.precedes_volume, h.label, ` + workSlugColumns + `
FROM edition_highlights h
JOIN chapters c ON c.id = h.chapter_id
JOIN works ON works.id = c.work_id AND works.edition_id = h.edition_id
%s
ORDER BY h.edition_id, h.position`

var (
	listHighlightsQuery    = fmt.Sprintf(highlightsQueryTemplate, "WHERE h.edition_id = $1")
	listAllHighlightsQuery = fmt.Sprintf(highlightsQueryTemplate, "")
)

// ListHighlights — избранное одного собрания по порядку.
func (r *EditionRepository) ListHighlights(
	ctx context.Context, editionID int64,
) ([]models.EditionHighlight, error) {
	return r.scanHighlights(ctx, listHighlightsQuery, editionID)
}

// ListAllHighlights — избранное всех собраний разом, по собранию и порядку:
// главной оно нужно целиком, одним запросом.
func (r *EditionRepository) ListAllHighlights(ctx context.Context) ([]models.EditionHighlight, error) {
	return r.scanHighlights(ctx, listAllHighlightsQuery)
}

func (r *EditionRepository) scanHighlights(
	ctx context.Context, query string, args ...any,
) ([]models.EditionHighlight, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list edition highlights: %w", err)
	}
	defer rows.Close()

	out := make([]models.EditionHighlight, 0)
	for rows.Next() {
		var h models.EditionHighlight
		var workTitle, role, slugEdition string
		var precedes *int
		if err := rows.Scan(
			&h.EditionID, &h.ChapterID, &h.ChapterTitle, &h.WorkID, &workTitle, &role,
			&precedes, &h.Label, &slugEdition, &h.VolumeNumber, &h.VolumePart,
		); err != nil {
			return nil, fmt.Errorf("failed to scan edition highlight: %w", err)
		}
		h.ChapterSlug = slug.Chapter(h.ChapterTitle)
		h.WorkSlug = workSlugFrom(role, slugEdition, h.VolumeNumber, h.VolumePart, precedes, workTitle)
		out = append(out, h)
	}
	return out, rows.Err()
}

// ReplaceHighlights заменяет избранное собрания списком целиком: перестановка,
// добавление и удаление — один и тот же вызов. Строка собрания запирается
// первой: два одновременных сохранения иначе оба удаляли бы и оба вставляли
// одну главу, и второе падало бы на первичном ключе. Проверка принадлежности
// глав идёт до записи и роняет весь список — частичной витрины не бывает.
// Повторы и длину подписи проверяет обработчик.
func (r *EditionRepository) ReplaceHighlights(
	ctx context.Context, editionID int64, items []models.HighlightInput,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin highlights transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT 1 FROM editions WHERE id = $1 FOR UPDATE`, editionID); err != nil {
		return fmt.Errorf("failed to lock edition: %w", err)
	}

	if len(items) > 0 {
		ids := make([]int64, len(items))
		for i, item := range items {
			ids[i] = item.ChapterID
		}
		var own int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM chapters c
			JOIN works w ON w.id = c.work_id
			WHERE c.id = ANY($1) AND w.edition_id = $2`, ids, editionID,
		).Scan(&own); err != nil {
			return fmt.Errorf("failed to check highlight chapters: %w", err)
		}
		if own != len(ids) {
			return ErrHighlightForeignChapter
		}
	}

	if _, err := tx.Exec(ctx, `DELETE FROM edition_highlights WHERE edition_id = $1`, editionID); err != nil {
		return fmt.Errorf("failed to clear highlights: %w", err)
	}
	for i, item := range items {
		if _, err := tx.Exec(ctx, `
			INSERT INTO edition_highlights (edition_id, chapter_id, position, label)
			VALUES ($1, $2, $3, $4)`, editionID, item.ChapterID, i, item.Label,
		); err != nil {
			return fmt.Errorf("failed to insert highlight: %w", err)
		}
	}
	return tx.Commit(ctx)
}
