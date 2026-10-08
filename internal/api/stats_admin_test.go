package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"proofreader/internal/repository"
)

type fakeStatsReader struct {
	from, today time.Time
	kind        string
}

func (f *fakeStatsReader) Traffic(_ context.Context, from, today time.Time) ([]repository.TrafficRow, error) {
	f.from, f.today = from, today
	return nil, nil
}
func (f *fakeStatsReader) Top(_ context.Context, _, kind string, from, today time.Time, _ int) ([]repository.TopRow, error) {
	f.kind = kind
	return nil, nil
}
func (f *fakeStatsReader) Referrers(context.Context, time.Time, time.Time, int) ([]repository.CountRow, error) {
	return nil, nil
}
func (f *fakeStatsReader) Searches(context.Context, time.Time, time.Time, int) (*repository.SearchStats, error) {
	return &repository.SearchStats{}, nil
}
func (f *fakeStatsReader) Crawlers(context.Context, time.Time, time.Time) ([]repository.CrawlerRow, error) {
	return nil, nil
}

func (f *fakeStatsReader) Devices(_ context.Context, from, today time.Time) ([]repository.DeviceRow, error) {
	f.from, f.today = from, today
	return nil, nil
}

func TestStatsDevicesPeriodAndEmptyArray(t *testing.T) {
	fr := &fakeStatsReader{}
	h := NewStatsHandler(nil, fr, nil, false)
	h.now = func() time.Time { return time.Date(2026, 9, 28, 22, 0, 0, 0, time.UTC) } // 01:00 МСК 29-го
	w := httptest.NewRecorder()
	h.Devices(w, httptest.NewRequest("GET", "/api/admin/stats/devices?days=7", nil))
	if w.Code != 200 || w.Body.String() != "[]\n" {
		t.Fatalf("код %d, тело %q", w.Code, w.Body.String())
	}
	if fr.today.Format("2006-01-02") != "2026-09-29" || fr.from.Format("2006-01-02") != "2026-09-23" {
		t.Fatalf("период %v — %v", fr.from, fr.today)
	}
	w = httptest.NewRecorder()
	h.Devices(w, httptest.NewRequest("GET", "/api/admin/stats/devices?days=0", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("days=0: код %d", w.Code)
	}
}

func TestStatsTrafficPeriodAndEmptyArray(t *testing.T) {
	fr := &fakeStatsReader{}
	h := NewStatsHandler(nil, fr, nil, false)
	h.now = func() time.Time { return time.Date(2026, 9, 28, 22, 0, 0, 0, time.UTC) } // 01:00 МСК 29-го
	w := httptest.NewRecorder()
	h.Traffic(w, httptest.NewRequest("GET", "/api/admin/stats/traffic?days=7", nil))
	if w.Code != 200 || w.Body.String() != "[]\n" {
		t.Fatalf("код %d, тело %q", w.Code, w.Body.String())
	}
	if fr.today.Format("2006-01-02") != "2026-09-29" || fr.from.Format("2006-01-02") != "2026-09-23" {
		t.Fatalf("период %v — %v", fr.from, fr.today)
	}
}

func TestStatsRejectsBadParams(t *testing.T) {
	h := NewStatsHandler(nil, &fakeStatsReader{}, nil, false)
	for _, target := range []string{
		"/api/admin/stats/traffic?days=0",
		"/api/admin/stats/traffic?days=366",
		"/api/admin/stats/traffic?days=x",
		"/api/admin/stats/top?kind=users",
		"/api/admin/stats/top?kind=work&channel=evil",
		"/api/admin/stats/top?kind=work&limit=100000",
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", target, nil)
		if req.URL.Path == "/api/admin/stats/top" {
			h.Top(w, req)
		} else {
			h.Traffic(w, req)
		}
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: код %d", target, w.Code)
		}
	}
}

func TestStatsHealthWithoutMetrics(t *testing.T) {
	w := httptest.NewRecorder()
	NewStatsHandler(nil, nil, nil, false).Health(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("код %d", w.Code)
	}
}
