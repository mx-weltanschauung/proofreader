package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"proofreader/internal/models"
)

// Буквальные сегменты очереди (claim, queue, rec, requeue-stale) соседствуют
// с переменными того же уровня; порядок регистрации в router.go решает, кто
// ответит (ср. /documents/review). Тест идёт через настоящий роутер.
func TestAudioLiteralSegmentsReachTheirHandlers(t *testing.T) {
	h, f, _ := newAudioTestHandler()
	f.tracks = []models.AudioTrack{{ID: 1, S3Key: "works/47/k.opus"}}
	r := newTestRouter(t).WithAudio(h).Setup()
	staff := tokenForRole(t, models.RoleEditor)

	do := func(method, url, body string, token string) int {
		req := httptest.NewRequest(method, url, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := do("POST", "/api/audio/queue/claim", `{"ids":[7]}`, staff); code != http.StatusOK || len(f.claimArgs.ids) != 1 {
		t.Errorf("claim: %d %v", code, f.claimArgs.ids)
	}
	if code := do("POST", "/api/audio/queue/7/result", `{"status":"готово","claimed_at":"`+claimedT+`"}`, staff); code != http.StatusNoContent || f.finished[7] != "готово" {
		t.Errorf("result: %d %v", code, f.finished)
	}
	if code := do("GET", "/api/audio/1.opus", "", ""); code != http.StatusFound {
		t.Errorf("opus: %d", code)
	}
	if code := do("GET", "/api/audio/queue", "", staff); code != http.StatusOK {
		t.Errorf("queue: %d", code)
	}
	// DELETE /audio/queue/{id:[0-9]+} не должен принимать буквальный claim.
	if code := do("DELETE", "/api/audio/queue/claim", "", staff); code != http.StatusMethodNotAllowed && code != http.StatusNotFound {
		t.Errorf("DELETE claim: %d, ждали 404/405 (400 значит, что claim принят за {id})", code)
	}
	if code := do("DELETE", "/api/audio/queue/3", "", staff); code != http.StatusNoContent {
		t.Errorf("DELETE queue/3: %d", code)
	}
	if code := do("POST", "/api/audio/queue/3/retry", "", staff); code != http.StatusOK {
		t.Errorf("retry: %d", code)
	}
	if code := do("GET", "/api/works/47/audio", "", ""); code != http.StatusOK {
		t.Errorf("works audio: %d", code)
	}
	if code := do("GET", "/api/audio/queue", "", ""); code != http.StatusUnauthorized {
		t.Errorf("queue анониму: %d", code)
	}
	// Публичный GET того же пути не должен пускать правку анониму.
	if code := do("PATCH", "/api/audio/rec/1", `{"reader":"x"}`, ""); code != http.StatusUnauthorized {
		t.Errorf("PATCH rec анониму: %d", code)
	}
}
