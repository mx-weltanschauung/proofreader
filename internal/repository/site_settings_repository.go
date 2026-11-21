package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SiteSettingsRepository хранит настройки, общие для всех процессов сервера.
type SiteSettingsRepository struct{ pool *pgxpool.Pool }

func NewSiteSettingsRepository(pool *pgxpool.Pool) *SiteSettingsRepository {
	return &SiteSettingsRepository{pool: pool}
}

func (r *SiteSettingsRepository) SuggestionLimit(ctx context.Context) (int, error) {
	var value int
	if err := r.pool.QueryRow(ctx, `SELECT suggestions_per_hour FROM site_settings WHERE id = 1`).Scan(&value); err != nil {
		return 0, fmt.Errorf("read suggestion limit: %w", err)
	}
	return value, nil
}

func (r *SiteSettingsRepository) SetSuggestionLimit(ctx context.Context, value int) error {
	tag, err := r.pool.Exec(ctx, `UPDATE site_settings SET suggestions_per_hour = $1 WHERE id = 1`, value)
	if err != nil {
		return fmt.Errorf("update suggestion limit: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("update suggestion limit: settings row missing")
	}
	return nil
}
