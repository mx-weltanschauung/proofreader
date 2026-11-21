package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

func TestStatsVisiblePagesAdminOnly(t *testing.T) {
	fr := &fakeStatsReader{visibleResult: &repository.VisiblePageStats{Pages: []repository.VisiblePagePoint{{PageNumber: 1, Visitors: 2}, {PageNumber: 2, Visitors: 0}}}}
	router := createTestRouter().WithStats(NewStatsHandler(nil, fr, nil, false)).Setup()
	path := "/api/admin/stats/visible-pages?work_id=49&chapter_id=101&days=30"
	for _, role := range []models.UserRole{"", models.RoleReader, models.RoleEditor, models.RoleAdministrator} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if role != "" {
			req.Header.Set("Authorization", "Bearer "+tokenForRole(t, role))
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if role == models.RoleAdministrator {
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"page_number":2`) {
				t.Fatalf("admin: %d %s", w.Code, w.Body.String())
			}
		} else if w.Code != 401 && w.Code != 403 {
			t.Fatalf("role=%q: %d", role, w.Code)
		}
	}
}

func TestStatsVisiblePagesBadChapter(t *testing.T) {
	fr := &fakeStatsReader{visibleErr: repository.ErrVisibleChapterNotFound}
	h := NewStatsHandler(nil, fr, nil, false)
	w := httptest.NewRecorder()
	h.VisiblePages(w, httptest.NewRequest(http.MethodGet, "/api/admin/stats/visible-pages?work_id=49&chapter_id=100", nil))
	if w.Code != 404 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	for _, query := range []string{"work_id=0&chapter_id=1", "work_id=1&chapter_id=-3", "work_id=1&chapter_id=2&days=366"} {
		w = httptest.NewRecorder()
		h.VisiblePages(w, httptest.NewRequest(http.MethodGet, "/api/admin/stats/visible-pages?"+query, nil))
		if w.Code != 400 {
			t.Fatalf("query=%s status=%d", query, w.Code)
		}
	}
	fr.visibleErr = nil
	fr.visibleResult = &repository.VisiblePageStats{Pages: []repository.VisiblePagePoint{}}
	h.now = func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) }
	w = httptest.NewRecorder()
	h.VisiblePages(w, httptest.NewRequest(http.MethodGet, "/api/admin/stats/visible-pages?work_id=49&chapter_id=100&days=7", nil))
	if fr.from.Format("2006-01-02") != "2026-09-23" || fr.today.Format("2006-01-02") != "2026-09-29" {
		t.Fatalf("period=%v..%v", fr.from, fr.today)
	}
}
