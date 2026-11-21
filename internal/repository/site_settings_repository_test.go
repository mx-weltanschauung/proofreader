package repository

import (
	"context"
	"testing"
)

func TestSiteSettingsSuggestionLimit(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewSiteSettingsRepository(pool)
	got, err := repo.SuggestionLimit(ctx)
	if err != nil || got != 5 {
		t.Fatalf("initial limit = %d, %v", got, err)
	}
	if err := repo.SetSuggestionLimit(ctx, 7); err != nil {
		t.Fatal(err)
	}
	got, err = repo.SuggestionLimit(ctx)
	if err != nil || got != 7 {
		t.Fatalf("saved limit = %d, %v", got, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE site_settings SET suggestions_per_hour = 0 WHERE id = 1`); err == nil {
		t.Fatal("PostgreSQL accepted zero")
	}
}
