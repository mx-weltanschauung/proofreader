package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// bigJSON — ответ заведомо крупнее порога сжатия, из повторяющегося текста:
// такой жмётся в разы, поэтому «сжато или нет» видно по размеру.
func bigJSON() string {
	return `{"pages":["` + strings.Repeat("текст страницы тома, повторяющийся раз за разом. ", 200) + `"]}`
}

func jsonHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	})
}

func TestGzipCompressesLargeJSON(t *testing.T) {
	body := bigJSON()
	req := httptest.NewRequest(http.MethodGet, "/api/works/47/chapters/2066/pages", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	Gzip(jsonHandler(body)).ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, ожидался gzip", got)
	}
	if rec.Body.Len() >= len(body) {
		t.Fatalf("сжатое тело %d байт не меньше исходных %d", rec.Body.Len(), len(body))
	}

	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("тело не читается как gzip: %v", err)
	}
	got, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("распаковка не удалась: %v", err)
	}
	if string(got) != body {
		t.Fatalf("после распаковки тело не совпало с исходным")
	}
}

func TestGzipSkippedWithoutAcceptEncoding(t *testing.T) {
	body := bigJSON()
	req := httptest.NewRequest(http.MethodGet, "/api/works/47", nil)
	rec := httptest.NewRecorder()

	Gzip(jsonHandler(body)).ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, клиент gzip не просил", got)
	}
	if rec.Body.String() != body {
		t.Fatalf("тело изменилось, хотя сжатия быть не должно")
	}
}

// Короткий ответ жать незачем: заголовок gzip сам занимает под два десятка
// байт, и ответ вроде {"ok":true} после сжатия становится длиннее.
func TestGzipSkipsShortResponses(t *testing.T) {
	body := `{"ok":true}`
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	Gzip(jsonHandler(body)).ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, короткий ответ жать не нужно", got)
	}
	if rec.Body.String() != body {
		t.Fatalf("тело = %q, ожидалось %q", rec.Body.String(), body)
	}
}

// Скан страницы — уже сжатый PNG: второй проход тратит процессор и не даёт
// ничего.
func TestGzipSkipsAlreadyCompressedTypes(t *testing.T) {
	body := strings.Repeat("x", 40000)
	req := httptest.NewRequest(http.MethodGet, "/api/works/47/pages/1/preview", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.WriteString(w, body)
	})).ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, PNG жать не нужно", got)
	}
	if rec.Body.String() != body {
		t.Fatalf("тело картинки изменилось")
	}
}

// Без Vary общий кэш отдал бы сжатый ответ клиенту, который gzip не просил.
func TestGzipAlwaysSetsVary(t *testing.T) {
	for _, accept := range []string{"gzip", ""} {
		req := httptest.NewRequest(http.MethodGet, "/api/works/47", nil)
		if accept != "" {
			req.Header.Set("Accept-Encoding", accept)
		}
		rec := httptest.NewRecorder()

		Gzip(jsonHandler(bigJSON())).ServeHTTP(rec, req)

		if got := rec.Header().Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
			t.Fatalf("Accept-Encoding=%q: Vary = %q, ожидался Accept-Encoding", accept, got)
		}
	}
}

// Content-Length от обработчика считает несжатое тело; оставить его — значит
// заставить клиента ждать байты, которых не будет.
func TestGzipDropsStaleContentLength(t *testing.T) {
	body := bigJSON()
	req := httptest.NewRequest(http.MethodGet, "/api/works/47", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = io.WriteString(w, body)
	})).ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Length"); got != "" {
		t.Fatalf("Content-Length = %q, ожидался пустой", got)
	}
}

func TestGzipKeepsStatusCode(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/works/999", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, bigJSON())
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("код ответа = %d, ожидался 404", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, ожидался gzip и на ошибке", got)
	}
}

// Обработчик, отдающий уже сжатые байты (файловый кэш глав), выставляет
// Content-Encoding сам. Второй проход сжатия превратил бы ответ в мусор:
// клиент разожмёт один слой и получит gzip вместо JSON.
func TestGzipLeavesPreCompressedResponseAlone(t *testing.T) {
	payload := bytes.Repeat([]byte("сжатое тело"), 500)

	handler := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/works/47/chapters/2066/pages", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, ожидался gzip", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), payload) {
		t.Errorf("тело изменено: длина %d, ожидалась %d — middleware сжал уже сжатое",
			rec.Body.Len(), len(payload))
	}
}
