package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

// EditionRepository handles edition data access
type EditionRepository struct {
	pool *pgxpool.Pool
}

// NewEditionRepository creates a new edition repository
func NewEditionRepository(pool *pgxpool.Pool) *EditionRepository {
	return &EditionRepository{pool: pool}
}

// Create creates a new edition
func (r *EditionRepository) Create(ctx context.Context, edition *models.Edition) error {
	cols := []string{"title", "slug", "url_slug", "description", "volumes_planned"}
	args := []any{edition.Title, edition.Slug, edition.URLSlug, edition.Description, edition.VolumesPlanned}
	if err := insertRow(ctx, r.pool, "editions", edition.ID, cols, args,
		&edition.ID, &edition.CreatedAt, &edition.UpdatedAt); err != nil {
		return fmt.Errorf("failed to create edition: %w", err)
	}
	return nil
}

// GetByID retrieves an edition by ID
func (r *EditionRepository) GetByID(ctx context.Context, id int64) (*models.Edition, error) {
	query := `
		SELECT id, title, slug, url_slug, description, volumes_planned, created_at, updated_at
		FROM editions
		WHERE id = $1
	`

	var edition models.Edition
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&edition.ID, &edition.Title, &edition.Slug, &edition.URLSlug, &edition.Description,
		&edition.VolumesPlanned, &edition.CreatedAt, &edition.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("edition not found")
		}
		return nil, fmt.Errorf("failed to get edition: %w", err)
	}

	return &edition, nil
}

// List retrieves all editions
func (r *EditionRepository) List(ctx context.Context) ([]*models.Edition, error) {
	query := `
		SELECT id, title, slug, url_slug, description, volumes_planned, created_at, updated_at
		FROM editions
		ORDER BY title
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list editions: %w", err)
	}
	defer rows.Close()

	var editions []*models.Edition
	for rows.Next() {
		var edition models.Edition
		err := rows.Scan(
			&edition.ID, &edition.Title, &edition.Slug, &edition.URLSlug, &edition.Description,
			&edition.VolumesPlanned, &edition.CreatedAt, &edition.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan edition: %w", err)
		}
		editions = append(editions, &edition)
	}

	return editions, nil
}

// Update updates an edition
func (r *EditionRepository) Update(ctx context.Context, edition *models.Edition) error {
	query := `
		UPDATE editions
		SET title = $2, slug = $3, url_slug = $4, description = $5, volumes_planned = $6
		WHERE id = $1
		RETURNING updated_at
	`

	err := r.pool.QueryRow(
		ctx, query, edition.ID, edition.Title, edition.Slug, edition.URLSlug, edition.Description,
		edition.VolumesPlanned,
	).Scan(&edition.UpdatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("edition not found")
		}
		return fmt.Errorf("failed to update edition: %w", err)
	}

	return nil
}

// Delete deletes an edition
func (r *EditionRepository) Delete(ctx context.Context, id int64) error {
	query := `DELETE FROM editions WHERE id = $1`

	result, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete edition: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("edition not found")
	}

	return nil
}

// listWorksQuery переведён на workColumns/worksWithSlug/scanWork (добавление
// контроллера к задаче 6): без слага страница издания для краулера
// (internal/seo/render_volume.go) печатала бы ссылки на тома без слага — а это
// лишний 301 на каждой из них. Колонки, которых раньше не было в узком наборе
// (parent_work_id, numbering_style, shelf_label), теперь заполняются по-настоящему,
// а не остаются нулевыми: форма ответа (models.Work) не менялась, JSON-тег
// каждого поля был и остаётся, просто значения были всегда пустыми.
const listWorksQuery = `
	SELECT ` + workColumns + `
	FROM ` + worksWithSlug + `
	WHERE edition_id = $1
	-- (role <> 'edition_front_matter') — false у предваряющих работ, true у
	-- томов; false < true, поэтому предисловия сортируются первыми без CASE.
	-- COALESCE(precedes_volume, volume_number) сводит два разных столбца-номера
	-- в один ключ: у предисловия заполнен только precedes_volume («перед каким
	-- томом стоит»), у тома — только volume_number, и они не пересекаются.
	ORDER BY (role <> 'edition_front_matter'),
	         COALESCE(precedes_volume, volume_number) NULLS LAST,
	         volume_part NULLS FIRST, title
`

// ListWorks retrieves the works (volumes) belonging to an edition, in
// edition order.
func (r *EditionRepository) ListWorks(ctx context.Context, editionID int64) ([]*models.Work, error) {
	rows, err := r.pool.Query(ctx, listWorksQuery, editionID)
	if err != nil {
		return nil, fmt.Errorf("failed to list edition works: %w", err)
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

// workSummariesQueryTemplate — тело запроса сводок с тремя дырами: %[1]s —
// условие отбора томов, %[2]s — приставка к ORDER BY, %[3]s — ингредиенты
// слага (см. workSummariesSlugColumns ниже). Тяжёлые CTE (page_stats
// по всем страницам, works_only по всем главам) не зависят от собрания и
// считаются по всему корпусу в обоих вариантах; фильтр стоит только в финальном
// SELECT. Отсюда и смысл общего варианта: запрос по всем собраниям обходится
// ровно во столько же, во сколько по одному (замер на живом корпусе — 35 мс
// против 38 мс), а вызовов вместо четырёх один. Держать это одним шаблоном, а
// не двумя копиями, обязательно: шестьдесят строк CTE, разъехавшиеся между
// главной и страницей собрания, дали бы разные цифры в двух местах читальни.
const workSummariesQueryTemplate = `
	WITH page_stats AS (
	    SELECT work_id,
	           SUM(cnt)::int AS pages_total,
	           jsonb_object_agg(status, cnt) AS by_status
	    FROM (SELECT work_id, status, COUNT(*) AS cnt FROM pages GROUP BY work_id, status) s
	    GROUP BY work_id
	),
	top_level AS (
	    SELECT work_id, title, (end_page - start_page + 1) AS pages, is_apparatus
	    FROM chapters
	    WHERE parent_id IS NULL
	),
	chapter_stats AS (
	    SELECT work_id, COUNT(*)::int AS chapters_total
	    FROM top_level GROUP BY work_id
	),
	-- Аппарат тома: примечания, указатели, списки, даты жизни, приложения.
	-- Он крупнее любой настоящей работы — в 45 томах Ленина «Примечания» и два
	-- указателя стоят главами верхнего уровня, — и работой не является. Ни в
	-- подпись корешка, ни в знаменатель доли он входить не должен: пока он был
	-- в знаменателе, доля всякой настоящей работы оказывалась занижена.
	--
	-- Здесь стоял предикат по заголовку — сперва регексп, потом LIKE по
	-- префиксам (87.6 мс против 8.7 мс: это было главным слагаемым запроса).
	-- Теперь ответ хранится в chapters.is_apparatus: заголовок классифицируется
	-- один раз, при создании главы (models.IsApparatusTitle), а спорный случай
	-- человек правит руками — границы тут тонкие, «Указатель имен» аппарат, а
	-- «Указания читателю» нет.
	works_only AS (
	    SELECT work_id, title, pages FROM top_level WHERE NOT is_apparatus
	),
	works_volume AS (
	    SELECT work_id, SUM(pages)::int AS total FROM works_only GROUP BY work_id
	),
	ranked AS (
	    SELECT work_id, title, pages,
	           row_number() OVER (PARTITION BY work_id ORDER BY pages DESC, title) AS rn
	    FROM works_only
	),
	top_works AS (
	    SELECT r.work_id,
	           jsonb_agg(jsonb_build_object(
	               'title', r.title,
	               'pages', r.pages,
	               'share', r.pages::float8 / wv.total
	           ) ORDER BY r.rn) AS items
	    FROM ranked r
	    JOIN works_volume wv ON wv.work_id = r.work_id AND wv.total > 0
	    WHERE r.rn <= 4
	    GROUP BY r.work_id
	)
	SELECT w.id, w.title, w.author, w.publication_date, w.language, w.country,
	       w.file_path, w.status, w.edition_id, w.volume_number, w.volume_part,
	       w.page_offset, w.role, w.precedes_volume, w.shelf_label,
	       w.owner_id, w.created_at, w.updated_at,
	       %[3]s,
	       COALESCE(ps.pages_total, 0),
	       COALESCE(ps.by_status, '{}'::jsonb),
	       COALESCE(cs.chapters_total, 0),
	       COALESCE(tw.items, '[]'::jsonb)
	FROM works w
	LEFT JOIN page_stats ps ON ps.work_id = w.id
	LEFT JOIN chapter_stats cs ON cs.work_id = w.id
	LEFT JOIN top_works tw ON tw.work_id = w.id
	WHERE %[1]s
	-- (w.role <> 'edition_front_matter') — false у предваряющих работ, true у
	-- томов; false < true, поэтому предисловия сортируются первыми без CASE.
	-- COALESCE(w.precedes_volume, w.volume_number) сводит два разных
	-- столбца-номера в один ключ: у предисловия заполнен только
	-- precedes_volume («перед каким томом стоит»), у тома — только
	-- volume_number, и они не пересекаются.
	ORDER BY %[2]s (w.role <> 'edition_front_matter'),
	         COALESCE(w.precedes_volume, w.volume_number) NULLS LAST,
	         w.volume_part NULLS FIRST, w.title
`

// workSummariesSlugColumns — те же три ингредиента слага, что и в
// workSlugColumns (work_repository.go), только с переписанным префиксом: в
// шаблоне сводок работа зовётся w, а в workSlugColumns — works. Подставляем
// одну и ту же константу, а не держим второй текст, — второй копии выражения
// не существует, поэтому разойтись с полкой карточка тома не может.
// ReplaceAll трогает только префикс works.<колонка> (шесть вхождений во всей
// константе); внутренние подзапросы обращаются к таблице по алиасу `FROM
// works p` — без точки после works, так что замена их не задевает.
var workSummariesSlugColumns = strings.ReplaceAll(workSlugColumns, "works.", "w.")

var (
	listWorkSummariesQuery = fmt.Sprintf(
		workSummariesQueryTemplate, "w.edition_id = $1", "", workSummariesSlugColumns,
	)
	// Общий вариант группируется по собранию первым ключом, чтобы читающий
	// ответ мог резать его подряд, не собирая карту.
	listAllWorkSummariesQuery = fmt.Sprintf(
		workSummariesQueryTemplate, "w.edition_id IS NOT NULL", "w.edition_id,", workSummariesSlugColumns,
	)
)

// ListWorkSummaries retrieves the edition's volumes with the aggregates the
// shelf needs: page counts by status, top-level chapter count, and the
// largest top-level chapter.
func (r *EditionRepository) ListWorkSummaries(
	ctx context.Context, editionID int64,
) ([]*models.VolumeSummary, error) {
	return r.scanSummaries(ctx, listWorkSummariesQuery, editionID)
}

// ListAllWorkSummaries returns the same summaries for every edition at once,
// ordered by edition and then in edition order. The home page needs the whole
// set and would otherwise ask per edition — four round trips for a query whose
// cost does not depend on the filter.
func (r *EditionRepository) ListAllWorkSummaries(
	ctx context.Context,
) ([]*models.VolumeSummary, error) {
	return r.scanSummaries(ctx, listAllWorkSummariesQuery)
}

func (r *EditionRepository) scanSummaries(
	ctx context.Context, query string, args ...any,
) ([]*models.VolumeSummary, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list edition volume summaries: %w", err)
	}
	defer rows.Close()

	summaries := make([]*models.VolumeSummary, 0)
	for rows.Next() {
		var s models.VolumeSummary
		var slugEdition string
		var slugVolume *int
		var slugPart *string
		err := rows.Scan(
			&s.ID, &s.Title, &s.Author, &s.PublicationDate, &s.Language, &s.Country,
			&s.FilePath, &s.Status, &s.EditionID, &s.VolumeNumber, &s.VolumePart,
			&s.PageOffset, &s.Role, &s.PrecedesVolume, &s.ShelfLabel,
			&s.OwnerID, &s.CreatedAt, &s.UpdatedAt,
			&slugEdition, &slugVolume, &slugPart,
			&s.PagesTotal, &s.PagesByStatus, &s.ChaptersTotal, &s.TopChapters,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan volume summary: %w", err)
		}
		s.Slug = workSlugFrom(s.Role, slugEdition, slugVolume, slugPart, s.PrecedesVolume, s.Title)
		summaries = append(summaries, &s)
	}

	return summaries, rows.Err()
}
