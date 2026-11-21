package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

func TestStatsDownloadsAdminOnly(t *testing.T) {
	fr := &fakeStatsReader{downloadsResult: &repository.DownloadStats{
		Summary: []repository.DownloadSummaryRow{{Kind: "collection", Format: "epub", Views: 2}},
		Top:     []repository.DownloadTopRow{{Kind: "work", WorkID: 49, Format: "pdf", Title: "Том", Views: 1}},
	}}
	router := createTestRouter().WithStats(NewStatsHandler(nil, fr, nil, false)).Setup()
	for _, role := range []models.UserRole{models.RoleReader, models.RoleAdministrator} {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/stats/downloads?days=7", nil)
		req.Header.Set("Authorization", "Bearer "+tokenForRole(t, role))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if role == models.RoleAdministrator {
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"summary"`) || !strings.Contains(w.Body.String(), `"top"`) {
				t.Fatalf("admin: %d %s", w.Code, w.Body.String())
			}
		} else if w.Code != 403 {
			t.Fatalf("reader: %d", w.Code)
		}
	}
	h := NewStatsHandler(nil, fr, nil, false)
	w := httptest.NewRecorder()
	h.Downloads(w, httptest.NewRequest(http.MethodGet, "/api/admin/stats/downloads?days=366", nil))
	if w.Code != 400 {
		t.Fatalf("days=366 status=%d", w.Code)
	}
}
