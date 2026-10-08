package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"proofreader/internal/auth"
	"proofreader/internal/middleware"
	"proofreader/internal/models"
)

func searchEvents(t *testing.T, ua string, role models.UserRole, result *models.SearchResult) []int {
	t.Helper()
	rec := &captureRecorder{}
	store := &fakeSearchStore{terms: []string{"прибавочн"}, result: result}
	h := NewSearchHandler(store).WithRecorder(rec, true)
	req := httptest.NewRequest(http.MethodGet, "/api/search?q=Прибавочная", nil)
	req.Header.Set("X-Real-IP", "203.0.113.7")
	req.Header.Set("User-Agent", ua)
	if role != "" {
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey,
			&auth.Claims{UserID: 1, Role: role}))
	}
	h.Search(httptest.NewRecorder(), req)
	var out []int
	for _, e := range rec.all() {
		out = append(out, *e.Hits)
	}
	return out
}

func TestSearchStatsSkipsStaffAndBots(t *testing.T) {
	res := &models.SearchResult{Terms: []string{"прибавочн"}}
	if got := searchEvents(t, humanUA, models.RoleEditor, res); len(got) != 0 {
		t.Fatalf("поиск редактора записан: %v", got)
	}
	if got := searchEvents(t, humanUA, models.RoleAdministrator, res); len(got) != 0 {
		t.Fatalf("поиск администратора записан: %v", got)
	}
	if got := searchEvents(t, "Mozilla/5.0 (compatible; Googlebot/2.1)", "", res); len(got) != 0 {
		t.Fatalf("поиск бота записан: %v", got)
	}
	if got := searchEvents(t, humanUA, models.RoleReader, res); len(got) != 1 {
		t.Fatalf("поиск читателя не записан: %v", got)
	}
}

// Запрос, нашедший только понятие или аппарат, не «нулевой».
func TestSearchStatsHitsCountAnythingFound(t *testing.T) {
	onlyConcept := &models.SearchResult{Terms: []string{"прибавочн"}, Concepts: []models.SearchConcept{{Slug: "a", Title: "А"}}}
	if got := searchEvents(t, humanUA, "", onlyConcept); len(got) != 1 || got[0] == 0 {
		t.Fatalf("понятие не засчитано: %v", got)
	}
	onlyApparatus := &models.SearchResult{Terms: []string{"прибавочн"}, Volumes: []models.SearchVolume{{ApparatusHits: 2}}}
	if got := searchEvents(t, humanUA, "", onlyApparatus); len(got) != 1 || got[0] == 0 {
		t.Fatalf("аппарат не засчитан: %v", got)
	}
}

func TestHitIPv6LimitedByPrefix(t *testing.T) {
	rec := &captureRecorder{}
	sh := NewStatsHandler(rec, nil, nil, true)
	for i := 0; i < hitsPerMinute+20; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/hit", strings.NewReader(`{"path":"/works/1"}`))
		req.Header.Set("User-Agent", humanUA)
		req.Header.Set("X-Real-IP", fmt.Sprintf("2001:db8:1:2::%x", i+1))
		sh.Hit(httptest.NewRecorder(), req)
	}
	if n := len(rec.all()); n != hitsPerMinute {
		t.Fatalf("записано %d, ожидалось %d: адреса одной /64 обязаны делить предел", n, hitsPerMinute)
	}
}

func TestHitGlobalCap(t *testing.T) {
	rec := &captureRecorder{}
	sh := NewStatsHandler(rec, nil, nil, true)
	for i := 0; i < hitsGlobalPerMinute+50; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/hit", strings.NewReader(`{"path":"/works/1"}`))
		req.Header.Set("User-Agent", humanUA)
		req.Header.Set("X-Real-IP", fmt.Sprintf("10.%d.%d.%d", i/65536, (i/256)%256, i%256))
		sh.Hit(httptest.NewRecorder(), req)
	}
	if n := len(rec.all()); n != hitsGlobalPerMinute {
		t.Fatalf("записано %d, ожидалось потолок %d", n, hitsGlobalPerMinute)
	}
}
