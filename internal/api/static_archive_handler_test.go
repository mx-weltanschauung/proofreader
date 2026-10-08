package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const goodStaticManifest = `{"format":1,"published_at":"2026-10-07T12:00:00Z",
 "current":{"file":"chitalnya-2026-10-07.zip","size":418756548,
  "sha256":"f987dddc719662494949b10a4e8de3809c94e7097c733dc8f7f0772e81469416",
  "md5":"08a1269d82a38dccebcaef8a0269f3f4","date":"2026-10-07","works":203,
  "files":23753,"commit":"abc1234","work_ids":[49,50]},
 "previous":{"file":"chitalnya-2026-09-30.zip","work_ids":[49]}}`

// staticBucket — подставной бакет: отдаёт body с кодом status на
// /manifest.json и считает запросы.
func staticBucket(t *testing.T, status int, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/manifest.json" {
			http.NotFound(w, r)
			return
		}
		hits.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func serveStatic(h http.HandlerFunc, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w
}

func TestStaticArchiveGetShowsCurrentOnly(t *testing.T) {
	srv, _ := staticBucket(t, http.StatusOK, goodStaticManifest)
	h := NewStaticArchiveHandler(srv.URL+"/", srv.Client())
	w := serveStatic(h.Get, "/api/static-archive")
	if w.Code != http.StatusOK {
		t.Fatalf("код %d: %s", w.Code, w.Body)
	}
	var got StaticArchive
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := StaticArchive{File: "chitalnya-2026-10-07.zip", URL: srv.URL + "/chitalnya-2026-10-07.zip",
		Size: 418756548, SHA256: "f987dddc719662494949b10a4e8de3809c94e7097c733dc8f7f0772e81469416",
		Date: "2026-10-07", Works: 203}
	if got != want {
		t.Errorf("архив %+v, хотел %+v", got, want)
	}
	for _, private := range []string{"work_ids", "md5", "commit", "previous", "2026-09-30"} {
		if strings.Contains(w.Body.String(), private) {
			t.Errorf("служебное %q ушло наружу: %s", private, w.Body)
		}
	}
}

func TestStaticArchiveDownloadRedirectsToFile(t *testing.T) {
	srv, _ := staticBucket(t, http.StatusOK, goodStaticManifest)
	h := NewStaticArchiveHandler(srv.URL, srv.Client())
	w := serveStatic(h.Download, "/api/static-archive/download")
	if w.Code != http.StatusFound || w.Header().Get("Location") != srv.URL+"/chitalnya-2026-10-07.zip" {
		t.Errorf("код %d, адрес %q", w.Code, w.Header().Get("Location"))
	}
}

func TestStaticArchiveWithoutURLIsAbsent(t *testing.T) {
	h := NewStaticArchiveHandler("", http.DefaultClient)
	for _, f := range []http.HandlerFunc{h.Get, h.Download} {
		if w := serveStatic(f, "/api/static-archive"); w.Code != http.StatusNotFound {
			t.Errorf("без STATIC_ARCHIVE_URL код %d, хотел 404", w.Code)
		}
	}
}

func TestStaticArchiveAbsentManifestIs404(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{
		{http.StatusNotFound, "NoSuchKey"},
		{http.StatusForbidden, "AccessDenied"},
		{http.StatusOK, `{"format":1,"current":null,"previous":null}`},
	} {
		srv, _ := staticBucket(t, tc.status, tc.body)
		h := NewStaticArchiveHandler(srv.URL, srv.Client())
		for _, f := range []http.HandlerFunc{h.Get, h.Download} {
			if w := serveStatic(f, "/"); w.Code != http.StatusNotFound {
				t.Errorf("бакет %d %q: код %d, хотел 404", tc.status, tc.body, w.Code)
			}
		}
	}
}

func TestStaticArchiveRejectsBadManifest(t *testing.T) {
	good := func(edit func(s string) string) string { return edit(goodStaticManifest) }
	for name, body := range map[string]string{
		"не json": "<html>",
		"формат":  good(func(s string) string { return strings.Replace(s, `"format":1`, `"format":2`, 1) }),
		"адрес вместо имени": good(func(s string) string {
			return strings.Replace(s, `"file":"chitalnya-2026-10-07.zip"`, `"file":"https://evil.example/chitalnya-2026-10-07.zip"`, 1)
		}),
		"путь в имени": good(func(s string) string {
			return strings.Replace(s, `"file":"chitalnya-2026-10-07.zip"`, `"file":"../chitalnya-2026-10-07.zip"`, 1)
		}),
		"дата":   good(func(s string) string { return strings.Replace(s, `"date":"2026-10-07"`, `"date":"2026-10-08"`, 1) }),
		"sha256": good(func(s string) string { return strings.Replace(s, `"sha256":"f987`, `"sha256":"F987`, 1) }),
		"размер": good(func(s string) string { return strings.Replace(s, `"size":418756548`, `"size":0`, 1) }),
		"работ":  good(func(s string) string { return strings.Replace(s, `"works":203`, `"works":0`, 1) }),
	} {
		srv, _ := staticBucket(t, http.StatusOK, body)
		h := NewStaticArchiveHandler(srv.URL, srv.Client())
		if w := serveStatic(h.Download, "/"); w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: код %d (адрес %q), хотел 503", name, w.Code, w.Header().Get("Location"))
		}
	}
	// Годный манифест, добитый пробелами за предел: обрезанный, он остался бы
	// годным JSON, поэтому отказ держится на проверке длины, а не на разборе.
	srv, _ := staticBucket(t, http.StatusOK, goodStaticManifest+strings.Repeat(" ", maxStaticManifest))
	if w := serveStatic(NewStaticArchiveHandler(srv.URL, srv.Client()).Get, "/"); w.Code != http.StatusServiceUnavailable {
		t.Errorf("манифест больше предела: код %d, хотел 503", w.Code)
	}
}

func TestStaticArchiveCachesManifest(t *testing.T) {
	srv, hits := staticBucket(t, http.StatusOK, goodStaticManifest)
	h := NewStaticArchiveHandler(srv.URL, srv.Client())
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	h.now = func() time.Time { return now }

	serveStatic(h.Get, "/")
	serveStatic(h.Download, "/")
	now = now.Add(staticArchiveTTL - time.Second)
	serveStatic(h.Get, "/")
	if n := hits.Load(); n != 1 {
		t.Errorf("за %s в бакет сходили %d раз, хотел 1", staticArchiveTTL, n)
	}
	now = now.Add(2 * time.Second)
	serveStatic(h.Get, "/")
	if n := hits.Load(); n != 2 {
		t.Errorf("после истечения кэша запросов %d, хотел 2", n)
	}
}

func TestStaticArchiveCachesFailureForAMinute(t *testing.T) {
	srv, hits := staticBucket(t, http.StatusInternalServerError, "упал")
	h := NewStaticArchiveHandler(srv.URL, srv.Client())
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	h.now = func() time.Time { return now }

	if w := serveStatic(h.Get, "/"); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("код %d, хотел 503", w.Code)
	}
	now = now.Add(staticArchiveRetryTTL - time.Second)
	serveStatic(h.Get, "/")
	if n := hits.Load(); n != 1 {
		t.Errorf("отказ не закэширован: запросов %d", n)
	}
	now = now.Add(2 * time.Second)
	serveStatic(h.Get, "/")
	if n := hits.Load(); n != 2 {
		t.Errorf("после минуты бакет не спросили снова: запросов %d", n)
	}
}
