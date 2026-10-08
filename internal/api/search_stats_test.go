package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"proofreader/internal/models"
	"proofreader/internal/stats"
)

func TestSearchRecordsQueryAndHits(t *testing.T) {
	rec := &captureRecorder{}
	store := &fakeSearchStore{
		terms:  []string{"прибавочн"},
		result: &models.SearchResult{Terms: []string{"прибавочн"}, TotalHits: 0},
	}
	h := NewSearchHandler(store).WithRecorder(rec, true)

	req := httptest.NewRequest(http.MethodGet, "/api/search?q=Прибавочная", nil)
	req.Header.Set("X-Real-IP", "203.0.113.7")
	h.Search(httptest.NewRecorder(), req)

	req = httptest.NewRequest(http.MethodGet, "/api/search?q=Прибавочная&terms_only=1", nil)
	h.Search(httptest.NewRecorder(), req)

	ev := rec.all()
	if len(ev) != 1 {
		t.Fatalf("событий %d (terms_only не должен писаться)", len(ev))
	}
	e := ev[0]
	if e.Channel != stats.ChannelSearch || e.Query != "прибавочная" || e.Hits == nil || *e.Hits != 0 || e.IP == "" {
		t.Fatalf("событие: %+v", e)
	}
}
