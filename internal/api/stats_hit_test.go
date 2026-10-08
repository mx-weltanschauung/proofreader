package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"proofreader/internal/models"
	"proofreader/internal/stats"
)

type captureRecorder struct {
	mu     sync.Mutex
	events []stats.Event
}

func (c *captureRecorder) Record(e stats.Event) {
	c.mu.Lock()
	c.events = append(c.events, e)
	c.mu.Unlock()
}

func (c *captureRecorder) all() []stats.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]stats.Event(nil), c.events...)
}

const humanUA = "Mozilla/5.0 (X11; Linux x86_64) Chrome/129"

// hitRouter — полный роутер с WithStats: фильтр сотрудников держится на
// OptionalAuth подроутера public, и мимо роутера его не проверить.
func hitRouter(rec *captureRecorder) http.Handler {
	return createTestRouter().WithStats(NewStatsHandler(rec, nil, nil, true)).Setup()
}

func postHit(t *testing.T, h http.Handler, body, ua, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/hit", strings.NewReader(body))
	req.Header.Set("User-Agent", ua)
	req.Header.Set("X-Real-IP", "203.0.113.7")
	req.Host = "lib.example.org"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestHitRecordsHumanView(t *testing.T) {
	rec := &captureRecorder{}
	w := postHit(t, hitRouter(rec),
		`{"path":"/works/49-lenin-t06/chapters/10125-x","ref":"https://yandex.ru/search?text=1"}`, humanUA, "")
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("код %d, тело %q", w.Code, w.Body.String())
	}
	ev := rec.all()
	if len(ev) != 1 {
		t.Fatalf("событий %d", len(ev))
	}
	e := ev[0]
	if e.Channel != stats.ChannelSPA || e.Kind != "chapter" || e.WorkID != 49 || e.EntityID != 10125 ||
		e.RefHost != "yandex.ru" || e.IP != "203.0.113.7" || e.UA != humanUA || e.TS.IsZero() {
		t.Fatalf("событие: %+v", e)
	}
}

func TestHitRecordsDeviceClass(t *testing.T) {
	cases := []struct {
		name, body string
		events     int
		device     string
	}{
		{"narrow", `{"path":"/works/1","ref":"","device":"narrow"}`, 1, stats.DeviceNarrow},
		{"wide", `{"path":"/works/1","ref":"","device":"wide"}`, 1, stats.DeviceWide},
		{"мусор не пишется", `{"path":"/works/1","ref":"","device":"phone"}`, 1, ""},
		{"старый клиент без поля", `{"path":"/works/1","ref":""}`, 1, ""},
		{"число вместо строки — тело отвергнуто", `{"path":"/works/1","ref":"","device":42}`, 0, ""},
	}
	for _, c := range cases {
		rec := &captureRecorder{}
		postHit(t, hitRouter(rec), c.body, humanUA, "")
		ev := rec.all()
		if len(ev) != c.events {
			t.Errorf("%s: событий %d, ждали %d", c.name, len(ev), c.events)
			continue
		}
		if c.events == 1 && ev[0].Device != c.device {
			t.Errorf("%s: device=%q, ждали %q", c.name, ev[0].Device, c.device)
		}
	}
}

func TestHitIgnoresBotsStaffAndJunk(t *testing.T) {
	cases := []struct {
		name, body, ua string
		role           models.UserRole
	}{
		{"googlebot", `{"path":"/works/1"}`, "Mozilla/5.0 (compatible; Googlebot/2.1)", ""},
		{"editor", `{"path":"/works/1"}`, humanUA, models.RoleEditor},
		{"admin", `{"path":"/works/1"}`, humanUA, models.RoleAdministrator},
		{"admin path", `{"path":"/admin/users"}`, humanUA, ""},
		{"garbage path", `{"path":"/works/abc"}`, humanUA, ""},
		{"not json", `path=/works/1`, humanUA, ""},
		{"huge", `{"path":"/` + strings.Repeat("a", 20000) + `"}`, humanUA, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &captureRecorder{}
			token := ""
			if c.role != "" {
				token = tokenForRole(t, c.role)
			}
			w := postHit(t, hitRouter(rec), c.body, c.ua, token)
			if w.Code != http.StatusNoContent {
				t.Fatalf("код %d", w.Code)
			}
			if n := len(rec.all()); n != 0 {
				t.Fatalf("записано %d событий", n)
			}
		})
	}
}

func TestHitCountsReader(t *testing.T) {
	rec := &captureRecorder{}
	postHit(t, hitRouter(rec), `{"path":"/works/1"}`, humanUA, tokenForRole(t, models.RoleReader))
	if len(rec.all()) != 1 {
		t.Fatal("читатель с токеном не посчитан")
	}
}

func TestHitRateLimited(t *testing.T) {
	rec := &captureRecorder{}
	h := hitRouter(rec)
	for i := 0; i < hitsPerMinute+5; i++ {
		postHit(t, h, `{"path":"/works/1"}`, humanUA, "")
	}
	if n := len(rec.all()); n != hitsPerMinute {
		t.Fatalf("записано %d, ожидалось %d", n, hitsPerMinute)
	}
}

func TestChannelsRecordSeoMdAndDownload(t *testing.T) {
	rec := &captureRecorder{}
	sh := NewStatsHandler(rec, nil, nil, true)
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	notFound := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })

	serve := func(h http.Handler, target, ua string) {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("User-Agent", ua)
		req.Header.Set("X-Real-IP", "203.0.113.7")
		sh.Channels(h).ServeHTTP(httptest.NewRecorder(), req)
	}
	serve(ok, "/seo/works/49/chapters/10125", "Mozilla/5.0 (compatible; YandexBot/3.0)")
	serve(ok, "/seo/works/49/chapters/10125/part-2.md", humanUA)
	serve(ok, "/seo/llms.txt", "Claude-User")
	serve(ok, "/api/works/49/download?format=epub", humanUA)
	serve(notFound, "/seo/works/999", "Googlebot") // 404 не пишется
	serve(ok, "/api/works/49", humanUA)            // обычный API не пишется

	ev := rec.all()
	if len(ev) != 4 {
		t.Fatalf("событий %d: %+v", len(ev), ev)
	}
	if e := ev[0]; e.Channel != stats.ChannelSEO || e.Kind != "chapter" || e.Agent != "YandexBot" {
		t.Errorf("seo: %+v", e)
	}
	if e := ev[1]; e.Channel != stats.ChannelMD || e.Kind != "chapter" || e.EntityID != 10125 || e.Agent != "" {
		t.Errorf("md: %+v", e)
	}
	if e := ev[2]; e.Channel != stats.ChannelMD || e.Kind != "llms" || e.Agent != "Claude-User" {
		t.Errorf("llms: %+v", e)
	}
	if e := ev[3]; e.Channel != stats.ChannelDownload || e.Kind != "work" || e.WorkID != 49 || e.SlugKey != "epub" {
		t.Errorf("download: %+v", e)
	}
}

func TestChannelsCountArchiveDownloadByRedirect(t *testing.T) {
	rec := &captureRecorder{}
	sh := NewStatsHandler(rec, nil, nil, true)
	found := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusFound) })
	notFound := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })
	serve := func(h http.Handler, target string) {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("User-Agent", humanUA)
		req.Header.Set("X-Real-IP", "203.0.113.7")
		sh.Channels(h).ServeHTTP(httptest.NewRecorder(), req)
	}
	serve(found, "/api/static-archive/download")
	serve(notFound, "/api/static-archive/download") // архива нет — не скачивание
	serve(found, "/api/works/49/download")          // 302 чужого маршрута не считается
	serve(found, "/api/static-archive")

	ev := rec.all()
	if len(ev) != 1 {
		t.Fatalf("событий %d, хотел 1: %+v", len(ev), ev)
	}
	if e := ev[0]; e.Channel != stats.ChannelDownload || e.Kind != "archive" || e.Agent != "" {
		t.Errorf("скачивание архива: %+v", e)
	}
}
