package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"proofreader/internal/models"
	"proofreader/internal/stats"
)

func postVisiblePage(t *testing.T, h http.Handler, body, ua, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/visible-page", strings.NewReader(body))
	req.Header.Set("User-Agent", ua)
	req.Header.Set("X-Real-IP", "203.0.113.7")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestVisiblePageAcceptsHuman(t *testing.T) {
	rec := &captureRecorder{}
	w := postVisiblePage(t, hitRouter(rec), `{"work_id":49,"page_number":12}`, humanUA, "")
	if w.Code != 204 || w.Body.Len() != 0 {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	ev := rec.all()
	if len(ev) != 1 || ev[0].Channel != stats.ChannelReading || ev[0].WorkID != 49 || ev[0].EntityID != 12 {
		t.Fatalf("events=%+v", ev)
	}
}

func TestVisiblePageIgnoresBotStaffAndInvalid(t *testing.T) {
	cases := []struct{ name, body, ua, token string }{
		{"bot", `{"work_id":49,"page_number":12}`, "Googlebot", ""},
		{"staff", `{"work_id":49,"page_number":12}`, humanUA, tokenForRole(t, models.RoleEditor)},
		{"negative", `{"work_id":-1,"page_number":12}`, humanUA, ""},
		{"zero", `{"work_id":49,"page_number":0}`, humanUA, ""},
		{"huge", `{"work_id":49,"page_number":999999999}`, humanUA, ""},
		{"float", `{"work_id":49,"page_number":1.5}`, humanUA, ""},
		{"body too large", strings.Repeat("x", 5000), humanUA, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &captureRecorder{}
			w := postVisiblePage(t, hitRouter(rec), c.body, c.ua, c.token)
			if w.Code != 204 || w.Body.Len() != 0 || len(rec.all()) != 0 {
				t.Fatalf("status=%d body=%q events=%+v", w.Code, w.Body.String(), rec.all())
			}
		})
	}
}

func TestVisiblePageSharesRateLimit(t *testing.T) {
	rec := &captureRecorder{}
	h := hitRouter(rec)
	for i := 0; i < hitsPerMinute; i++ {
		postHit(t, h, `{"path":"/works/49"}`, humanUA, "")
	}
	postVisiblePage(t, h, `{"work_id":49,"page_number":12}`, humanUA, "")
	if len(rec.all()) != hitsPerMinute {
		t.Fatalf("events=%d", len(rec.all()))
	}
}
