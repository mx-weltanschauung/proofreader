package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

// PageRepository handles page data access
type PageRepository struct {
	pool *pgxpool.Pool
}

// NewPageRepository creates a new page repository
func NewPageRepository(pool *pgxpool.Pool) *PageRepository {
	return &PageRepository{pool: pool}
}

// Create creates a new page
func (r *PageRepository) Create(ctx context.Context, page *models.Page) error {
	query := `
		INSERT INTO pages (work_id, page_number, preview_path, content_markdown, status, chapter_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at
	`

	err := r.pool.QueryRow(
		ctx, query,
		page.WorkID, page.PageNumber, page.PreviewPath, page.ContentMarkdown, page.Status, page.ChapterID,
	).Scan(&page.ID, &page.CreatedAt, &page.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create page: %w", err)
	}

	return nil
}

// textEditedAtSubquery — дата последней НАСТОЯЩЕЙ правки текста.
//
// page_versions хранит ПРЕЖНИЙ текст на момент правки (applyPageEdit кладёт
// снимок до записи нового), поэтому «правка изменила текст» проверяется
// сравнением версии с ТЕКУЩИМ content_markdown полосы: если они совпадают,
// версия холостая — правка не тронула текст (машинная вычитка шлёт update_page
// с тем же текстом на смену одного статуса, и applyPageEdit пишет версию
// безусловно, даже когда новый текст равен прежнему). Такой хвост
// отсеивается целиком, а MAX берёт дату той версии, что произвела текущий
// текст. Полоса, которую правили только статусом, честно отдаёт NULL —
// «правок не было», а не дату холостой записи.
const textEditedAtSubquery = `(SELECT max(v.created_at) FROM page_versions v
	WHERE v.page_id = pages.id
	  AND v.content_markdown IS DISTINCT FROM pages.content_markdown)`

// GetByID retrieves a page by ID
func (r *PageRepository) GetByID(ctx context.Context, id int64) (*models.Page, error) {
	query := `
		SELECT id, work_id, page_number, preview_path, content_markdown, status, chapter_id,
		       created_at, updated_at,
		       ` + textEditedAtSubquery + `
		FROM pages
		WHERE id = $1
	`

	var page models.Page
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&page.ID, &page.WorkID, &page.PageNumber, &page.PreviewPath, &page.ContentMarkdown,
		&page.Status, &page.ChapterID, &page.CreatedAt, &page.UpdatedAt, &page.TextEditedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("page not found")
		}
		return nil, fmt.Errorf("failed to get page: %w", err)
	}

	return &page, nil
}

// GetByWorkAndPageNumber retrieves a page by work ID and page number
func (r *PageRepository) GetByWorkAndPageNumber(ctx context.Context, workID int64, pageNumber int) (*models.Page, error) {
	query := `
		SELECT id, work_id, page_number, preview_path, content_markdown, status, chapter_id,
		       created_at, updated_at,
		       ` + textEditedAtSubquery + `
		FROM pages
		WHERE work_id = $1 AND page_number = $2
	`

	var page models.Page
	err := r.pool.QueryRow(ctx, query, workID, pageNumber).Scan(
		&page.ID, &page.WorkID, &page.PageNumber, &page.PreviewPath, &page.ContentMarkdown,
		&page.Status, &page.ChapterID, &page.CreatedAt, &page.UpdatedAt, &page.TextEditedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("page not found")
		}
		return nil, fmt.Errorf("failed to get page: %w", err)
	}

	return &page, nil
}

// ListByWork retrieves all pages for a work
func (r *PageRepository) ListByWork(ctx context.Context, workID int64) ([]*models.Page, error) {
	query := `
		SELECT id, work_id, page_number, preview_path, content_markdown, status, chapter_id, created_at, updated_at
		FROM pages
		WHERE work_id = $1
		ORDER BY page_number
	`

	rows, err := r.pool.Query(ctx, query, workID)
	if err != nil {
		return nil, fmt.Errorf("failed to list pages: %w", err)
	}
	defer rows.Close()

	var pages []*models.Page
	for rows.Next() {
		var page models.Page
		err := rows.Scan(
			&page.ID, &page.WorkID, &page.PageNumber, &page.PreviewPath, &page.ContentMarkdown,
			&page.Status, &page.ChapterID, &page.CreatedAt, &page.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan page: %w", err)
		}
		pages = append(pages, &page)
	}

	return pages, nil
}

// ListPageMap возвращает номера и статусы всех страниц работы по возрастанию
// номера. Пустой срез, а не nil: ручка отдаёт его в JSON как есть.
func (r *PageRepository) ListPageMap(ctx context.Context, workID int64) ([]models.PageMapEntry, error) {
	query := `
		SELECT page_number, status
		FROM pages
		WHERE work_id = $1
		ORDER BY page_number
	`

	rows, err := r.pool.Query(ctx, query, workID)
	if err != nil {
		return nil, fmt.Errorf("failed to list page map: %w", err)
	}
	defer rows.Close()

	entries := make([]models.PageMapEntry, 0)
	for rows.Next() {
		var entry models.PageMapEntry
		if err := rows.Scan(&entry.PageNumber, &entry.Status); err != nil {
			return nil, fmt.Errorf("failed to scan page map entry: %w", err)
		}
		entries = append(entries, entry)
	}

	return entries, rows.Err()
}

// ListByChapter retrieves all pages for a chapter
func (r *PageRepository) ListByChapter(ctx context.Context, chapterID int64) ([]*models.Page, error) {
	query := `
		SELECT id, work_id, page_number, preview_path, content_markdown, status, chapter_id, created_at, updated_at
		FROM pages
		WHERE chapter_id = $1
		ORDER BY page_number
	`

	rows, err := r.pool.Query(ctx, query, chapterID)
	if err != nil {
		return nil, fmt.Errorf("failed to list pages: %w", err)
	}
	defer rows.Close()

	var pages []*models.Page
	for rows.Next() {
		var page models.Page
		err := rows.Scan(
			&page.ID, &page.WorkID, &page.PageNumber, &page.PreviewPath, &page.ContentMarkdown,
			&page.Status, &page.ChapterID, &page.CreatedAt, &page.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan page: %w", err)
		}
		pages = append(pages, &page)
	}

	return pages, nil
}

// Update updates a page
func (r *PageRepository) Update(ctx context.Context, page *models.Page) error {
	query := `
		UPDATE pages
		SET content_markdown = $2, status = $3, chapter_id = $4, preview_path = $5
		WHERE id = $1
		RETURNING updated_at
	`

	err := r.pool.QueryRow(
		ctx, query,
		page.ID, page.ContentMarkdown, page.Status, page.ChapterID, page.PreviewPath,
	).Scan(&page.UpdatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("page not found")
		}
		return fmt.Errorf("failed to update page: %w", err)
	}

	return nil
}

// SaveEdit пишет снимок прежнего текста в page_versions и новый текст в pages
// ОДНОЙ транзакцией.
//
// Две записи живут в одном методе потому, что порознь они расходились: снимок
// шёл первым и уже был закоммичен, когда запись полосы падала (на боевом —
// PUT без поля status, пустая строка не проходит enum page_status), и
// откатывать его было нечем. В истории оставалась версия правки, которой не
// было: по содержимому — точная копия текущего текста, а список версий объёма
// диффа не показывает, так что снаружи такая строка неотличима от настоящей.
//
// Номер версии считает вызывающий: уникальность (page_id, version_number)
// сторожит база, и две одновременные правки не разъедутся молча — проигравшая
// откатится целиком вместе со своим снимком.
func (r *PageRepository) SaveEdit(ctx context.Context, page *models.Page, version *models.PageVersion) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin page edit: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit это no-op

	err = tx.QueryRow(ctx, `
		INSERT INTO page_versions (page_id, content_markdown, version_number, user_id, comment)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`, version.PageID, version.ContentMarkdown, version.VersionNumber,
		version.UserID, version.Comment,
	).Scan(&version.ID, &version.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create page version: %w", err)
	}

	err = tx.QueryRow(ctx, `
		UPDATE pages
		SET content_markdown = $2, status = $3, chapter_id = $4, preview_path = $5
		WHERE id = $1
		RETURNING updated_at
	`, page.ID, page.ContentMarkdown, page.Status, page.ChapterID, page.PreviewPath,
	).Scan(&page.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("page not found")
		}
		return fmt.Errorf("failed to update page: %w", err)
	}

	// Звук, синтезированный по прежнему тексту, устарел. Здесь, а не шагом
	// applyPageEdit после транзакции: метка обязана уехать вместе с правкой,
	// и путь записи текста полосы по-прежнему один (SaveEdit зовёт только
	// applyPageEdit). Правка одного статуса звук не старит — вычитчик,
	// ставящий «вычитана» на сотни полос, иначе переозвучивал бы корпус.
	if version.ContentMarkdown != page.ContentMarkdown {
		if _, err := tx.Exec(ctx, `
			UPDATE audio_tracks SET stale = true
			WHERE work_id = $1 AND $2 BETWEEN start_page AND end_page`,
			page.WorkID, page.PageNumber); err != nil {
			return fmt.Errorf("пометка звука полосы %d: %w", page.ID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit page edit: %w", err)
	}
	return nil
}

// Delete deletes a page
func (r *PageRepository) Delete(ctx context.Context, id int64) error {
	query := `DELETE FROM pages WHERE id = $1`

	result, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete page: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("page not found")
	}

	return nil
}

// GetPageRange retrieves pages in a range for a work
func (r *PageRepository) GetPageRange(ctx context.Context, workID int64, startPage, endPage int) ([]*models.Page, error) {
	query := `
		SELECT id, work_id, page_number, preview_path, content_markdown, status, chapter_id, created_at, updated_at
		FROM pages
		WHERE work_id = $1 AND page_number >= $2 AND page_number <= $3
		ORDER BY page_number
	`

	rows, err := r.pool.Query(ctx, query, workID, startPage, endPage)
	if err != nil {
		return nil, fmt.Errorf("failed to get page range: %w", err)
	}
	defer rows.Close()

	var pages []*models.Page
	for rows.Next() {
		var page models.Page
		err := rows.Scan(
			&page.ID, &page.WorkID, &page.PageNumber, &page.PreviewPath, &page.ContentMarkdown,
			&page.Status, &page.ChapterID, &page.CreatedAt, &page.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan page: %w", err)
		}
		pages = append(pages, &page)
	}

	return pages, nil
}

// GetPagesByNumbers retrieves the pages of a work whose page_number is in the
// given list, ordered by page_number.
//
// Deliberately not GetPageRange(min, max): the addresses of one concept are
// scattered across a volume (т. 23, с. 46…551), so a range would pull hundreds
// of markdown bodies to serve twenty pages.
func (r *PageRepository) GetPagesByNumbers(ctx context.Context, workID int64, numbers []int) ([]*models.Page, error) {
	if len(numbers) == 0 {
		return nil, nil
	}

	query := `
		SELECT id, work_id, page_number, preview_path, content_markdown, status, chapter_id, created_at, updated_at
		FROM pages
		WHERE work_id = $1 AND page_number = ANY($2)
		ORDER BY page_number
	`

	rows, err := r.pool.Query(ctx, query, workID, numbers)
	if err != nil {
		return nil, fmt.Errorf("failed to get pages by numbers: %w", err)
	}
	defer rows.Close()

	var pages []*models.Page
	for rows.Next() {
		var page models.Page
		err := rows.Scan(
			&page.ID, &page.WorkID, &page.PageNumber, &page.PreviewPath, &page.ContentMarkdown,
			&page.Status, &page.ChapterID, &page.CreatedAt, &page.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan page: %w", err)
		}
		pages = append(pages, &page)
	}

	return pages, nil
}

// MaxPageNumber возвращает наибольший номер страницы работы, 0 — если страниц
// нет.
//
// Это же число — длина работы: нумерация в базе сплошная и начинается с
// единицы (проверено по всем работам). COUNT(*) дал бы то же, но прошёл бы по
// строкам вместо чтения края индекса.
func (r *PageRepository) MaxPageNumber(ctx context.Context, workID int64) (int, error) {
	query := `SELECT COALESCE(MAX(page_number), 0) FROM pages WHERE work_id = $1`

	var max int
	if err := r.pool.QueryRow(ctx, query, workID).Scan(&max); err != nil {
		return 0, fmt.Errorf("failed to get max page number: %w", err)
	}
	return max, nil
}
