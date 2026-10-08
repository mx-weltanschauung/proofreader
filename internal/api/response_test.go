package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"proofreader/internal/models"
	"proofreader/pkg/storage"
)

func TestNewPageResponse_WithPreview(t *testing.T) {
	store := storage.NewMemoryStorage()
	p := &models.Page{ID: 1, WorkID: 5, PageNumber: 2, PreviewPath: "works/5/pages/page_2.png"}
	resp := newPageResponse(context.Background(), p, store, 30*time.Minute)
	if resp.PreviewURL == "" {
		t.Error("expected PreviewURL to be set")
	}
	if !strings.Contains(resp.PreviewURL, "works/5/pages/page_2.png") {
		t.Errorf("PreviewURL = %q", resp.PreviewURL)
	}
}

func TestNewPageResponse_EmptyPath(t *testing.T) {
	store := storage.NewMemoryStorage()
	p := &models.Page{ID: 1, WorkID: 5, PageNumber: 2, PreviewPath: ""}
	resp := newPageResponse(context.Background(), p, store, 30*time.Minute)
	if resp.PreviewURL != "" {
		t.Errorf("expected empty PreviewURL, got %q", resp.PreviewURL)
	}
}

func TestNewPageResponses_Slice(t *testing.T) {
	store := storage.NewMemoryStorage()
	pages := []*models.Page{
		{ID: 1, WorkID: 5, PageNumber: 1, PreviewPath: "works/5/pages/page_1.png"},
		{ID: 2, WorkID: 5, PageNumber: 2, PreviewPath: ""},
	}
	resp := newPageResponses(context.Background(), pages, store, time.Minute)
	if len(resp) != 2 {
		t.Fatalf("len = %d, want 2", len(resp))
	}
	if resp[0].PreviewURL == "" {
		t.Error("resp[0] should have PreviewURL")
	}
	if resp[1].PreviewURL != "" {
		t.Error("resp[1] should have empty PreviewURL")
	}
}

func TestNewWorkResponse_WithFile(t *testing.T) {
	store := storage.NewMemoryStorage()
	wk := &models.Work{ID: 5, Title: "T", FilePath: "works/5/original/1_a.pdf"}
	resp := newWorkResponse(context.Background(), wk, store, time.Minute)
	if resp.FileURL == "" {
		t.Error("expected FileURL to be set")
	}
}
