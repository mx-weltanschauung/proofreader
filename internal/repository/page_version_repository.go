package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

// PageVersionRepository handles page version data access
type PageVersionRepository struct {
	pool *pgxpool.Pool
}

// NewPageVersionRepository creates a new page version repository
func NewPageVersionRepository(pool *pgxpool.Pool) *PageVersionRepository {
	return &PageVersionRepository{pool: pool}
}

// Create creates a new page version
func (r *PageVersionRepository) Create(ctx context.Context, version *models.PageVersion) error {
	query := `
		INSERT INTO page_versions (page_id, content_markdown, version_number, user_id, comment)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`

	err := r.pool.QueryRow(
		ctx, query,
		version.PageID, version.ContentMarkdown, version.VersionNumber, version.UserID, version.Comment,
	).Scan(&version.ID, &version.CreatedAt)

	if err != nil {
		return fmt.Errorf("failed to create page version: %w", err)
	}

	return nil
}

// GetByID retrieves a page version by ID
func (r *PageVersionRepository) GetByID(ctx context.Context, id int64) (*models.PageVersion, error) {
	query := `
		SELECT id, page_id, content_markdown, version_number, user_id, comment, created_at
		FROM page_versions
		WHERE id = $1
	`

	var version models.PageVersion
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&version.ID, &version.PageID, &version.ContentMarkdown, &version.VersionNumber,
		&version.UserID, &version.Comment, &version.CreatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get page version: %w", err)
	}

	return &version, nil
}

// ListByPage retrieves all versions for a page
func (r *PageVersionRepository) ListByPage(ctx context.Context, pageID int64) ([]*models.PageVersion, error) {
	query := `
		SELECT id, page_id, content_markdown, version_number, user_id, comment, created_at
		FROM page_versions
		WHERE page_id = $1
		ORDER BY version_number DESC
	`

	rows, err := r.pool.Query(ctx, query, pageID)
	if err != nil {
		return nil, fmt.Errorf("failed to list page versions: %w", err)
	}
	defer rows.Close()

	var versions []*models.PageVersion
	for rows.Next() {
		var version models.PageVersion
		err := rows.Scan(
			&version.ID, &version.PageID, &version.ContentMarkdown, &version.VersionNumber,
			&version.UserID, &version.Comment, &version.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan page version: %w", err)
		}
		versions = append(versions, &version)
	}

	return versions, nil
}

// GetLatestVersionNumber retrieves the latest version number for a page
func (r *PageVersionRepository) GetLatestVersionNumber(ctx context.Context, pageID int64) (int, error) {
	query := `
		SELECT COALESCE(MAX(version_number), 0)
		FROM page_versions
		WHERE page_id = $1
	`

	var versionNumber int
	err := r.pool.QueryRow(ctx, query, pageID).Scan(&versionNumber)
	if err != nil {
		return 0, fmt.Errorf("failed to get latest version number: %w", err)
	}

	return versionNumber, nil
}
