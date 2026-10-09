package api

import (
	"context"
	"time"

	"proofreader/internal/models"
	"proofreader/pkg/storage"
)

// pageResponse wraps a Page with a presigned preview URL for the frontend.
type pageResponse struct {
	*models.Page
	PreviewURL string `json:"preview_url,omitempty"`
}

// workResponse wraps a Work with a presigned original-file URL. children
// заполняется только на карточке работы: в списке он не нужен и стоил бы
// запроса на каждую строку.
type workResponse struct {
	*models.Work
	FileURL  string         `json:"file_url,omitempty"`
	Children []workResponse `json:"children,omitempty"`
	// JournalIssue — только у работы номера журнала.
	JournalIssue *models.WorkJournalIssue `json:"journal_issue,omitempty"`
}

// presign returns a presigned GET URL for key, or "" if key is empty or
// presigning fails (the URL is best-effort; a failure must not break the API).
func presign(ctx context.Context, store storage.Storage, key string, ttl time.Duration) string {
	if key == "" {
		return ""
	}
	url, err := store.PresignGet(ctx, key, ttl)
	if err != nil {
		return ""
	}
	return url
}

func newPageResponse(ctx context.Context, p *models.Page, store storage.Storage, ttl time.Duration) pageResponse {
	return pageResponse{Page: p, PreviewURL: presign(ctx, store, p.PreviewPath, ttl)}
}

func newPageResponses(ctx context.Context, pages []*models.Page, store storage.Storage, ttl time.Duration) []pageResponse {
	out := make([]pageResponse, len(pages))
	for i, p := range pages {
		out[i] = newPageResponse(ctx, p, store, ttl)
	}
	return out
}

func newWorkResponse(ctx context.Context, wk *models.Work, store storage.Storage, ttl time.Duration) workResponse {
	return workResponse{Work: wk, FileURL: presign(ctx, store, wk.FilePath, ttl)}
}
