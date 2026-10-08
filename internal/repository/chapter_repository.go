package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
	"proofreader/pkg/slug"
)

// ChapterRepository handles chapter data access
type ChapterRepository struct {
	pool *pgxpool.Pool
}

// NewChapterRepository creates a new chapter repository
func NewChapterRepository(pool *pgxpool.Pool) *ChapterRepository {
	return &ChapterRepository{pool: pool}
}

// chapterColumns — колонки главы в порядке, который читает scanChapter.
const chapterColumns = `id, work_id, parent_id, title, type, order_number,
	start_page, end_page, is_apparatus, created_at, updated_at`

// scanChapter — единственный разбор строки главы. Заведён вместо пяти копий
// подряд: слаг считается здесь, и любая забытая копия оставила бы главу без
// хвоста адреса молча.
func scanChapter(row rowScanner) (*models.Chapter, error) {
	var c models.Chapter
	err := row.Scan(
		&c.ID, &c.WorkID, &c.ParentID, &c.Title, &c.Type, &c.OrderNumber,
		&c.StartPage, &c.EndPage, &c.IsApparatus, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	c.Slug = slug.Chapter(c.Title)
	return &c, nil
}

// Create creates a new chapter.
//
// Незаполненный IsApparatus (false) означает «определи сам»: заголовок идёт в
// models.IsApparatusTitle вместе с ответом на вопрос, верхнего ли глава уровня
// (часть правил у подглавы не работает — см. apparatusTopTitles), а подглава
// вдобавок наследует признак родителя — раздел внутри «Примечаний» тоже
// аппарат, как бы он ни назывался. Поставить
// аппаратной главе false осознанно можно правкой: Update пишет значение как
// есть, и ручная поправка спорного случая переживает всё остальное.
func (r *ChapterRepository) Create(ctx context.Context, chapter *models.Chapter) error {
	if !chapter.IsApparatus {
		chapter.IsApparatus = models.IsApparatusTitle(chapter.Title, chapter.ParentID == nil)
	}
	if !chapter.IsApparatus && chapter.ParentID != nil {
		var parentApparatus bool
		err := r.pool.QueryRow(ctx,
			`SELECT is_apparatus FROM chapters WHERE id = $1`, *chapter.ParentID,
		).Scan(&parentApparatus)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("failed to read parent chapter: %w", err)
		}
		chapter.IsApparatus = parentApparatus
	}

	query := `
		INSERT INTO chapters (work_id, parent_id, title, type, order_number, start_page, end_page, is_apparatus)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at
	`

	err := r.pool.QueryRow(
		ctx, query,
		chapter.WorkID, chapter.ParentID, chapter.Title, chapter.Type, chapter.OrderNumber, chapter.StartPage, chapter.EndPage, chapter.IsApparatus,
	).Scan(&chapter.ID, &chapter.CreatedAt, &chapter.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create chapter: %w", err)
	}

	return nil
}

// GetByID retrieves a chapter by ID
func (r *ChapterRepository) GetByID(ctx context.Context, id int64) (*models.Chapter, error) {
	query := `SELECT ` + chapterColumns + `
		FROM chapters
		WHERE id = $1
	`

	chapter, err := scanChapter(r.pool.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("chapter not found")
		}
		return nil, fmt.Errorf("failed to get chapter: %w", err)
	}

	return chapter, nil
}

// FindByPage — самая узкая глава, накрывающая полосу, или nil, если такой нет.
//
// Полоса не знает своей главы: chapters.id в pages есть (ON DELETE SET NULL),
// но конвейер его не заполняет ни одной полосе — главы живут диапазонами
// start_page/end_page и перестраиваются целиком при каждом прогоне тома,
// поэтому хранимая ссылка поехала бы вразнос. Принадлежность считается в
// момент запроса.
//
// Правило выбора повторяет pageHitChapterJoin (search_repository.go), и
// повторяет намеренно: главы вложены (произведение → подглавы), полосу
// накрывают несколько сразу, и обеим выдачам — поиску и canonical — нужна
// одна и та же, самая глубокая. Порядок «сначала узкий диапазон, потом id»
// разводит и полный повтор границ у главы с единственной подглавой.
//
// nil без ошибки — штатный ответ, а не умолчание об ошибке: передние листы,
// дыры между работами тома и полосы за концом последней главы не накрыты
// ничем, и вызывающий обязан иметь для этого ветку.
func (r *ChapterRepository) FindByPage(
	ctx context.Context, workID int64, pageNumber int,
) (*models.Chapter, error) {
	query := `SELECT ` + chapterColumns + `
		FROM chapters
		WHERE work_id = $1 AND $2 BETWEEN start_page AND end_page
		ORDER BY end_page - start_page, id
		LIMIT 1
	`

	chapter, err := scanChapter(r.pool.QueryRow(ctx, query, workID, pageNumber))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find chapter by page: %w", err)
	}

	return chapter, nil
}

// ListByWork retrieves all chapters for a work (flat list)
func (r *ChapterRepository) ListByWork(ctx context.Context, workID int64) ([]*models.Chapter, error) {
	query := `SELECT ` + chapterColumns + `
		FROM chapters
		WHERE work_id = $1
		ORDER BY order_number
	`

	rows, err := r.pool.Query(ctx, query, workID)
	if err != nil {
		return nil, fmt.Errorf("failed to list chapters: %w", err)
	}
	defer rows.Close()

	var chapters []*models.Chapter
	for rows.Next() {
		chapter, err := scanChapter(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan chapter: %w", err)
		}
		chapters = append(chapters, chapter)
	}

	return chapters, nil
}

// ListByWorkHierarchical retrieves all chapters for a work as a tree structure
func (r *ChapterRepository) ListByWorkHierarchical(ctx context.Context, workID int64) ([]*models.Chapter, error) {
	chapters, err := r.ListByWork(ctx, workID)
	if err != nil {
		return nil, err
	}

	return buildChapterTree(chapters), nil
}

// buildChapterTree converts a flat list of chapters into a tree structure
func buildChapterTree(chapters []*models.Chapter) []*models.Chapter {
	chapterMap := make(map[int64]*models.Chapter)
	var roots []*models.Chapter

	for _, chapter := range chapters {
		chapter.Children = []*models.Chapter{}
		chapterMap[chapter.ID] = chapter
	}

	for _, chapter := range chapters {
		if chapter.ParentID == nil {
			roots = append(roots, chapter)
		} else {
			parent, exists := chapterMap[*chapter.ParentID]
			if exists {
				parent.Children = append(parent.Children, chapter)
			} else {
				roots = append(roots, chapter)
			}
		}
	}

	return roots
}

// GetChildren retrieves direct children of a chapter
func (r *ChapterRepository) GetChildren(ctx context.Context, parentID int64) ([]*models.Chapter, error) {
	query := `SELECT ` + chapterColumns + `
		FROM chapters
		WHERE parent_id = $1
		ORDER BY order_number
	`

	rows, err := r.pool.Query(ctx, query, parentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get children: %w", err)
	}
	defer rows.Close()

	var chapters []*models.Chapter
	for rows.Next() {
		chapter, err := scanChapter(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan chapter: %w", err)
		}
		chapters = append(chapters, chapter)
	}

	return chapters, nil
}

// Update updates a chapter
func (r *ChapterRepository) Update(ctx context.Context, chapter *models.Chapter) error {
	query := `
		UPDATE chapters
		SET parent_id = $2, title = $3, type = $4, order_number = $5, start_page = $6, end_page = $7, is_apparatus = $8
		WHERE id = $1
		RETURNING updated_at
	`

	err := r.pool.QueryRow(
		ctx, query,
		chapter.ID, chapter.ParentID, chapter.Title, chapter.Type, chapter.OrderNumber, chapter.StartPage, chapter.EndPage, chapter.IsApparatus,
	).Scan(&chapter.UpdatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("chapter not found")
		}
		return fmt.Errorf("failed to update chapter: %w", err)
	}

	return nil
}

// Move updates the parent and order of a chapter (for drag-and-drop reordering)
func (r *ChapterRepository) Move(ctx context.Context, chapterID int64, newParentID *int64, newOrder int) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var workID int64
	var oldParentID *int64
	var oldOrder int
	err = tx.QueryRow(ctx, `SELECT work_id, parent_id, order_number FROM chapters WHERE id = $1`, chapterID).
		Scan(&workID, &oldParentID, &oldOrder)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("chapter not found")
		}
		return fmt.Errorf("failed to get chapter: %w", err)
	}

	if oldOrder == newOrder && ((oldParentID == nil && newParentID == nil) ||
		(oldParentID != nil && newParentID != nil && *oldParentID == *newParentID)) {
		return nil
	}

	sameParent := (oldParentID == nil && newParentID == nil) ||
		(oldParentID != nil && newParentID != nil && *oldParentID == *newParentID)

	_, err = tx.Exec(ctx, `UPDATE chapters SET order_number = -1 WHERE id = $1`, chapterID)
	if err != nil {
		return fmt.Errorf("failed to temporarily update order: %w", err)
	}

	if sameParent {
		if newOrder > oldOrder {
			if newParentID == nil {
				_, err = tx.Exec(ctx, `
					UPDATE chapters 
					SET order_number = order_number - 1 
					WHERE work_id = $1 AND parent_id IS NULL AND order_number > $2 AND order_number <= $3`,
					workID, oldOrder, newOrder)
			} else {
				_, err = tx.Exec(ctx, `
					UPDATE chapters 
					SET order_number = order_number - 1 
					WHERE work_id = $1 AND parent_id = $2 AND order_number > $3 AND order_number <= $4`,
					workID, *newParentID, oldOrder, newOrder)
			}
		} else if newOrder < oldOrder {
			if newParentID == nil {
				_, err = tx.Exec(ctx, `
					UPDATE chapters 
					SET order_number = order_number + 1 
					WHERE work_id = $1 AND parent_id IS NULL AND order_number >= $2 AND order_number < $3`,
					workID, newOrder, oldOrder)
			} else {
				_, err = tx.Exec(ctx, `
					UPDATE chapters 
					SET order_number = order_number + 1 
					WHERE work_id = $1 AND parent_id = $2 AND order_number >= $3 AND order_number < $4`,
					workID, *newParentID, newOrder, oldOrder)
			}
		}
	} else {
		if oldParentID == nil {
			_, err = tx.Exec(ctx, `
				UPDATE chapters 
				SET order_number = order_number - 1 
				WHERE work_id = $1 AND parent_id IS NULL AND order_number > $2`,
				workID, oldOrder)
		} else {
			_, err = tx.Exec(ctx, `
				UPDATE chapters 
				SET order_number = order_number - 1 
				WHERE work_id = $1 AND parent_id = $2 AND order_number > $3`,
				workID, *oldParentID, oldOrder)
		}
		if err != nil {
			return fmt.Errorf("failed to reorder old siblings: %w", err)
		}

		if newParentID == nil {
			_, err = tx.Exec(ctx, `
				UPDATE chapters 
				SET order_number = order_number + 1 
				WHERE work_id = $1 AND parent_id IS NULL AND order_number >= $2`,
				workID, newOrder)
		} else {
			_, err = tx.Exec(ctx, `
				UPDATE chapters 
				SET order_number = order_number + 1 
				WHERE work_id = $1 AND parent_id = $2 AND order_number >= $3`,
				workID, *newParentID, newOrder)
		}
	}
	if err != nil {
		return fmt.Errorf("failed to reorder siblings: %w", err)
	}

	_, err = tx.Exec(ctx, `UPDATE chapters SET parent_id = $2, order_number = $3 WHERE id = $1`,
		chapterID, newParentID, newOrder)
	if err != nil {
		return fmt.Errorf("failed to move chapter: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// ReorderSiblings updates order numbers for all siblings after a move operation
func (r *ChapterRepository) ReorderSiblings(ctx context.Context, workID int64, parentID *int64) error {
	var query string
	var args []interface{}

	if parentID == nil {
		query = `
			WITH ranked AS (
				SELECT id, ROW_NUMBER() OVER (ORDER BY order_number) as new_order
				FROM chapters
				WHERE work_id = $1 AND parent_id IS NULL
			)
			UPDATE chapters c
			SET order_number = r.new_order
			FROM ranked r
			WHERE c.id = r.id
		`
		args = []interface{}{workID}
	} else {
		query = `
			WITH ranked AS (
				SELECT id, ROW_NUMBER() OVER (ORDER BY order_number) as new_order
				FROM chapters
				WHERE work_id = $1 AND parent_id = $2
			)
			UPDATE chapters c
			SET order_number = r.new_order
			FROM ranked r
			WHERE c.id = r.id
		`
		args = []interface{}{workID, *parentID}
	}

	_, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to reorder siblings: %w", err)
	}

	return nil
}

// Delete deletes a chapter
func (r *ChapterRepository) Delete(ctx context.Context, id int64) error {
	query := `DELETE FROM chapters WHERE id = $1`

	result, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete chapter: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("chapter not found")
	}

	return nil
}
