package api

import (
	"context"

	"proofreader/internal/middleware"
	"proofreader/internal/models"
)

// ScanVisibilityStore reports both inherited work restrictions and chapter spans.
type ScanVisibilityStore interface {
	ScansHiddenBy(context.Context, int64) (string, error)
	ScansHiddenSpans(context.Context, int64) ([]models.PageSpan, error)
	WorksWithHiddenScans(context.Context) (map[int64]bool, error)
}

func scanAdministrator(ctx context.Context) bool {
	claims, ok := middleware.GetUserFromContext(ctx)
	return ok && claims != nil && claims.Role == models.RoleAdministrator
}

// scanAccess checks the restriction once per response, including shared pages.
func scanAccess(ctx context.Context, store ScanVisibilityStore, workID int64) (bool, []models.PageSpan, error) {
	if store == nil || scanAdministrator(ctx) {
		return false, nil, nil
	}
	by, err := store.ScansHiddenBy(ctx, workID)
	if err != nil {
		return false, nil, err
	}
	if by != "" {
		return true, nil, nil
	}
	spans, err := store.ScansHiddenSpans(ctx, workID)
	return false, spans, err
}

func redactPageScan(page *models.Page, restricted bool) *models.Page {
	if !restricted {
		return page
	}
	cp := *page
	cp.PreviewPath = ""
	return &cp
}

func redactWorkOriginal(work *models.Work, restricted bool) *models.Work {
	if !restricted {
		return work
	}
	cp := *work
	cp.FilePath = ""
	return &cp
}
