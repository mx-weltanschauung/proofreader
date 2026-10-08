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

// WorkRepository handles work data access
type WorkRepository struct {
	pool *pgxpool.Pool
}

// NewWorkRepository creates a new work repository
func NewWorkRepository(pool *pgxpool.Pool) *WorkRepository {
	return &WorkRepository{pool: pool}
}

// workColumns — единственное место, где перечислены колонки работы. Три
// читающих запроса сканируют их в одном порядке через scanWork.
const workColumns = `id, title, author, publication_date, language, country,
	file_path, status, edition_id, volume_number, volume_part, page_offset,
	parent_work_id, role, numbering_style, precedes_volume, shelf_label, description,
	owner_id, created_at, updated_at,
	slug_edition, slug_volume, slug_part, edition_title`

// workSlugColumns — три ингредиента слага, добираемые к строке работы:
// адресный слаг собрания (с откатом на editions.slug), номер и часть тома.
// У служебных передних листов берутся координаты родителя — своих у них нет.
const workSlugColumns = `
	COALESCE((SELECT COALESCE(NULLIF(e.url_slug, ''), e.slug)
	          FROM editions e
	          WHERE e.id = COALESCE(works.edition_id,
	                (SELECT p.edition_id FROM works p WHERE p.id = works.parent_work_id))), '') AS slug_edition,
	COALESCE(works.volume_number,
	         (SELECT p.volume_number FROM works p WHERE p.id = works.parent_work_id)) AS slug_volume,
	COALESCE(works.volume_part,
	         (SELECT p.volume_part FROM works p WHERE p.id = works.parent_work_id)) AS slug_part`

// workEditionTitleColumn — название собрания работы, добираемое тем же
// подзапросом, что и адресный слаг: у передних листов берётся издание
// родителя, у работы вне собрания выходит пустая строка.
//
// Отдельной константой, а НЕ внутри workSlugColumns, и это не вкусовщина.
// workSlugColumns подставляется ещё в пять запросов, которые разбирают
// результат позиционно (seo_repository.Works, workSummariesSlugColumns,
// searchWorkSlugColumns, volumeMapSlugColumns), а два из них вдобавок
// группируются — лишнее скалярное подвыражение сломало бы их все разом.
// Здесь оно нужно ровно одному потребителю: строке работы.
const workEditionTitleColumn = `
	COALESCE((SELECT e.title
	          FROM editions e
	          WHERE e.id = COALESCE(works.edition_id,
	                (SELECT p.edition_id FROM works p WHERE p.id = works.parent_work_id))), '') AS edition_title`

// worksWithSlug подставляется вместо `FROM works` во всех запросах, читающих
// workColumns. Подзапрос назван works, поэтому условия и порядок остальных
// запросов («WHERE parent_work_id = $1», «ORDER BY created_at DESC») работают
// без правок; простой подзапрос Postgres разворачивает, так что фильтр по id
// по-прежнему уходит внутрь.
const worksWithSlug = `(SELECT works.*, ` + workSlugColumns + `,` + workEditionTitleColumn + ` FROM works) works`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanWork(row rowScanner) (*models.Work, error) {
	var w models.Work
	var slugEdition string
	var slugVolume *int
	var slugPart *string
	err := row.Scan(
		&w.ID, &w.Title, &w.Author, &w.PublicationDate, &w.Language,
		&w.Country, &w.FilePath, &w.Status,
		&w.EditionID, &w.VolumeNumber, &w.VolumePart, &w.PageOffset,
		&w.ParentWorkID, &w.Role, &w.NumberingStyle, &w.PrecedesVolume,
		&w.ShelfLabel, &w.Description,
		&w.OwnerID, &w.CreatedAt, &w.UpdatedAt,
		&slugEdition, &slugVolume, &slugPart, &w.EditionTitle,
	)
	if err != nil {
		return nil, err
	}
	w.Slug = workSlugFrom(w.Role, slugEdition, slugVolume, slugPart, w.PrecedesVolume, w.Title)
	return &w, nil
}

// Create creates a new work
func (r *WorkRepository) Create(ctx context.Context, work *models.Work) error {
	// role/numbering_style — NOT NULL с CHECK, а не полагаемся на DEFAULT в
	// колонке: Create перечисляет обе колонки явно (нужно для служебных
	// работ), поэтому DEFAULT 'volume'/'arabic' в схеме не подхватывается —
	// незаполненное поле структуры ушло бы в базу пустой строкой и упало бы
	// на works_role_check/works_numbering_style_check. Вызывающие, ещё не
	// знающие о ролях (текущий POST /api/works), оставляют оба поля пустыми
	// и должны получать том, как до этой миграции.
	if work.Role == "" {
		work.Role = models.WorkRoleVolume
	}
	if work.NumberingStyle == "" {
		work.NumberingStyle = models.NumberingArabic
	}

	cols := []string{"title", "author", "publication_date", "language", "country", "file_path",
		"status", "edition_id", "volume_number", "volume_part", "page_offset", "parent_work_id",
		"role", "numbering_style", "precedes_volume", "shelf_label", "description", "owner_id"}
	args := []any{
		work.Title, work.Author, work.PublicationDate, work.Language,
		work.Country, work.FilePath, work.Status,
		work.EditionID, work.VolumeNumber, work.VolumePart, work.PageOffset,
		work.ParentWorkID, work.Role, work.NumberingStyle, work.PrecedesVolume,
		work.ShelfLabel, work.Description,
		work.OwnerID,
	}
	if err := insertRow(ctx, r.pool, "works", work.ID, cols, args,
		&work.ID, &work.CreatedAt, &work.UpdatedAt); err != nil {
		return fmt.Errorf("failed to create work: %w", err)
	}
	return nil
}

// GetByID retrieves a work by ID
func (r *WorkRepository) GetByID(ctx context.Context, id int64) (*models.Work, error) {
	query := `SELECT ` + workColumns + ` FROM ` + worksWithSlug + ` WHERE id = $1`

	work, err := scanWork(r.pool.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("work not found")
		}
		return nil, fmt.Errorf("failed to get work: %w", err)
	}
	return work, nil
}

// List retrieves works with optional filters
func (r *WorkRepository) List(ctx context.Context, limit, offset int, status *models.WorkStatus, ownerID *int64) ([]*models.Work, error) {
	query := `SELECT ` + workColumns + `
		FROM ` + worksWithSlug + `
		WHERE parent_work_id IS NULL AND role = 'volume'
	`
	args := []interface{}{}
	argIdx := 1

	if status != nil {
		query += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, *status)
		argIdx++
	}

	if ownerID != nil {
		query += fmt.Sprintf(" AND owner_id = $%d", argIdx)
		args = append(args, *ownerID)
		argIdx++
	}

	// id DESC breaks ties on created_at: without a full order, paging through
	// works with equal timestamps can skip or repeat a row across pages — and
	// a skipped work vanishes from the export's works.md with no page-diff to
	// explain it, git just shows a deleted work.
	query += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list works: %w", err)
	}
	defer rows.Close()

	var works []*models.Work
	for rows.Next() {
		work, err := scanWork(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan work: %w", err)
		}
		works = append(works, work)
	}

	return works, nil
}

// ListWithoutEdition возвращает работы, не приписанные ни к одному собранию —
// отдельный список на главной. Отбор повторяет List (только тома верхнего
// уровня, тот же порядок), но берёт лишь id и заголовок: список показывает
// имя со ссылкой, и полная строка works в нём была бы возимым впустую весом.
func (r *WorkRepository) ListWithoutEdition(ctx context.Context) ([]models.ShelfWork, error) {
	query := `
		SELECT id, title
		FROM works
		WHERE parent_work_id IS NULL AND role = 'volume' AND edition_id IS NULL
		ORDER BY created_at DESC, id DESC
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list works without edition: %w", err)
	}
	defer rows.Close()

	works := make([]models.ShelfWork, 0)
	for rows.Next() {
		var w models.ShelfWork
		if err := rows.Scan(&w.ID, &w.Title); err != nil {
			return nil, fmt.Errorf("failed to scan work without edition: %w", err)
		}
		// Работа вне собрания слага собрания не имеет — только транслитерация
		// заголовка.
		w.Slug = slug.Text(w.Title)
		works = append(works, w)
	}

	return works, rows.Err()
}

// ListChildren возвращает служебные работы тома (передние листы) в
// устойчивом порядке. Каскад по parent_work_id снимает их вместе с томом,
// поэтому осиротевших строк тут не бывает.
func (r *WorkRepository) ListChildren(ctx context.Context, parentID int64) ([]*models.Work, error) {
	query := `SELECT ` + workColumns + `
		FROM ` + worksWithSlug + ` WHERE parent_work_id = $1 ORDER BY role, id`

	rows, err := r.pool.Query(ctx, query, parentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list child works: %w", err)
	}
	defer rows.Close()

	works := []*models.Work{}
	for rows.Next() {
		work, err := scanWork(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan child work: %w", err)
		}
		works = append(works, work)
	}
	return works, nil
}

// Update updates a work
func (r *WorkRepository) Update(ctx context.Context, work *models.Work) error {
	query := `
		UPDATE works
		SET title = $2, author = $3, publication_date = $4, language = $5,
		    country = $6, file_path = $7, status = $8,
		    edition_id = $9, volume_number = $10, volume_part = $11, page_offset = $12,
		    parent_work_id = $13, role = $14, numbering_style = $15, precedes_volume = $16,
		    shelf_label = $17, description = $18
		WHERE id = $1
		RETURNING updated_at
	`

	err := r.pool.QueryRow(
		ctx, query,
		work.ID, work.Title, work.Author, work.PublicationDate,
		work.Language, work.Country, work.FilePath, work.Status,
		work.EditionID, work.VolumeNumber, work.VolumePart, work.PageOffset,
		work.ParentWorkID, work.Role, work.NumberingStyle, work.PrecedesVolume,
		work.ShelfLabel, work.Description,
	).Scan(&work.UpdatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("work not found")
		}
		return fmt.Errorf("failed to update work: %w", err)
	}

	return nil
}

// Delete deletes a work
func (r *WorkRepository) Delete(ctx context.Context, id int64) error {
	query := `DELETE FROM works WHERE id = $1`

	result, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete work: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("work not found")
	}

	return nil
}

// AddCategory adds a category to a work
func (r *WorkRepository) AddCategory(ctx context.Context, workID, categoryID int64) error {
	query := `
		INSERT INTO work_categories (work_id, category_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`

	_, err := r.pool.Exec(ctx, query, workID, categoryID)
	if err != nil {
		return fmt.Errorf("failed to add category to work: %w", err)
	}

	return nil
}

// RemoveCategory removes a category from a work
func (r *WorkRepository) RemoveCategory(ctx context.Context, workID, categoryID int64) error {
	query := `DELETE FROM work_categories WHERE work_id = $1 AND category_id = $2`

	_, err := r.pool.Exec(ctx, query, workID, categoryID)
	if err != nil {
		return fmt.Errorf("failed to remove category from work: %w", err)
	}

	return nil
}

// GetCategories retrieves all categories for a work
func (r *WorkRepository) GetCategories(ctx context.Context, workID int64) ([]*models.Category, error) {
	query := `
		SELECT c.id, c.name, c.slug, c.description, c.created_at, c.updated_at
		FROM categories c
		INNER JOIN work_categories wc ON c.id = wc.category_id
		WHERE wc.work_id = $1
		ORDER BY c.name
	`

	rows, err := r.pool.Query(ctx, query, workID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work categories: %w", err)
	}
	defer rows.Close()

	var categories []*models.Category
	for rows.Next() {
		var category models.Category
		err := rows.Scan(
			&category.ID, &category.Name, &category.Slug, &category.Description,
			&category.CreatedAt, &category.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan category: %w", err)
		}
		categories = append(categories, &category)
	}

	return categories, nil
}

// Count returns the total number of works
func (r *WorkRepository) Count(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM works`

	var count int
	err := r.pool.QueryRow(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count works: %w", err)
	}

	return count, nil
}
