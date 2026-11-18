package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/pkg/storage"
)

type fakeScanVisibility struct {
	whole     string
	spans     []models.PageSpan
	works     map[int64]bool
	spanCalls int
}

func (f *fakeScanVisibility) ScansHiddenBy(context.Context, int64) (string, error) {
	return f.whole, nil
}
func (f *fakeScanVisibility) ScansHiddenSpans(context.Context, int64) ([]models.PageSpan, error) {
	f.spanCalls++
	return f.spans, nil
}
func (f *fakeScanVisibility) WorksWithHiddenScans(context.Context) (map[int64]bool, error) {
	return f.works, nil
}

func TestScanVisibilityRolesAndSharedPage(t *testing.T) {
	page := &models.Page{ID: 4, WorkID: 1, PageNumber: 5, ContentMarkdown: "открытый текст", PreviewPath: "works/1/pages/5.png"}
	store := &fakePageStore{
		getByIDFn:    func(context.Context, int64) (*models.Page, error) { return page, nil },
		listByWorkFn: func(context.Context, int64) ([]*models.Page, error) { return []*models.Page{page, page}, nil },
	}
	visibility := &fakeScanVisibility{spans: []models.PageSpan{{Start: 5, End: 5}}}
	h := NewPageHandler(store, nil, nil, nil, nil, storage.NewMemoryStorage(), time.Minute).WithScans(visibility)
	for _, role := range []models.UserRole{"", models.RoleReader, models.RoleEditor, models.RoleAdministrator} {
		req := httptest.NewRequest(http.MethodGet, "/api/works/1/pages/4", nil)
		req = mux.SetURLVars(req, map[string]string{"workId": "1", "pageId": "4"})
		if role != "" {
			req = asRole(req, role)
		}
		rec := httptest.NewRecorder()
		h.Get(rec, req)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "открытый текст") {
			t.Fatalf("role=%s response=%d %s", role, rec.Code, rec.Body.String())
		}
		var got struct {
			PreviewPath string `json:"preview_path"`
			PreviewURL  string `json:"preview_url"`
			Restricted  bool   `json:"scan_restricted"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if role == models.RoleAdministrator {
			if got.PreviewPath == "" || got.PreviewURL == "" || got.Restricted {
				t.Errorf("admin: %+v", got)
			}
		} else if got.PreviewPath != "" || got.PreviewURL != "" || !got.Restricted {
			t.Errorf("role=%s: %+v", role, got)
		}
	}
	if page.PreviewPath == "" {
		t.Fatal("redaction mutated stored page")
	}
	visibility.spanCalls = 0
	req := httptest.NewRequest(http.MethodGet, "/api/works/1/pages", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "1"})
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != 200 || visibility.spanCalls != 1 || strings.Contains(rec.Body.String(), "works/1/pages/5.png") {
		t.Fatalf("list: code=%d calls=%d body=%s", rec.Code, visibility.spanCalls, rec.Body.String())
	}
}

func TestScanVisibilityOriginalReopensOnlyWhenSafe(t *testing.T) {
	w := &models.Work{ID: 1, Title: "Том", FilePath: "works/1/original/a.pdf"}
	repo := &filedWorks{fakeWorkRepo: &fakeWorkRepo{}, works: map[int64]*models.Work{1: w}}
	visibility := &fakeScanVisibility{works: map[int64]bool{1: true}}
	h := NewWorkHandler(repo, nil, nil, storage.NewMemoryStorage(), time.Minute, nil, nil).WithScans(visibility)
	get := func(role models.UserRole) string {
		req := httptest.NewRequest(http.MethodGet, "/api/works/1", nil)
		req = mux.SetURLVars(req, map[string]string{"id": "1"})
		if role != "" {
			req = asRole(req, role)
		}
		rec := httptest.NewRecorder()
		h.Get(rec, req)
		if rec.Code != 200 {
			t.Fatalf("code=%d %s", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	if strings.Contains(get(models.RoleEditor), "a.pdf") {
		t.Fatal("editor received original")
	}
	if !strings.Contains(get(models.RoleAdministrator), "a.pdf") {
		t.Fatal("admin lost original")
	}
	visibility.works = map[int64]bool{}
	if !strings.Contains(get(models.RoleEditor), "a.pdf") {
		t.Fatal("original did not reopen")
	}
	h.WithClosedChapterWorks(closedChapterWorks{1: true})
	if strings.Contains(get(""), "a.pdf") {
		t.Fatal("closed chapter still requires original hiding")
	}
	if w.FilePath == "" {
		t.Fatal("redaction mutated stored work")
	}
}

func TestScanVisibilityEditPreservesPreviewPath(t *testing.T) {
	page := &models.Page{ID: 4, WorkID: 1, PageNumber: 5, ContentMarkdown: "было", PreviewPath: "works/1/pages/5.png"}
	var savedPath string
	store := &fakePageStore{
		getByIDFn: func(context.Context, int64) (*models.Page, error) { return page, nil },
		saveEditFn: func(_ context.Context, p *models.Page, _ *models.PageVersion) error {
			savedPath = p.PreviewPath
			page.ContentMarkdown = p.ContentMarkdown
			return nil
		},
	}
	h := NewPageHandler(store, &fakePageVersions{}, newFakeFragmentStore(), &fakeDocumentCutStore{}, nil,
		storage.NewMemoryStorage(), time.Minute).WithScans(&fakeScanVisibility{whole: "work"})
	req := httptest.NewRequest(http.MethodPut, "/api/works/1/pages/4", strings.NewReader(`{"content_markdown":"стало","status":"draft"}`))
	req = mux.SetURLVars(req, map[string]string{"workId": "1", "pageId": "4"})
	req = asRole(req, models.RoleEditor)
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	if rec.Code != 200 || savedPath != "works/1/pages/5.png" || page.PreviewPath != savedPath || strings.Contains(rec.Body.String(), savedPath) {
		t.Fatalf("code=%d saved=%q stored=%q body=%s", rec.Code, savedPath, page.PreviewPath, rec.Body.String())
	}
}
