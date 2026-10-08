package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

func TestRecordingUploadURLRefusesUnknownType(t *testing.T) {
	h, _, _ := newAudioTestHandler()
	vars := map[string]string{"workId": "47", "id": "5"}
	rec := httptest.NewRecorder()
	h.RecordingUploadURL(rec, audioRequest("POST", "/", map[string]any{"content_type": "video/mp4", "bytes": 10}, vars))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("video/mp4: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.RecordingUploadURL(rec, audioRequest("POST", "/", map[string]any{"content_type": "audio/mpeg", "bytes": 10}, vars))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `works/47/rec/5/`) ||
		!strings.Contains(rec.Body.String(), `.mp3`) {
		t.Errorf("mp3: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	h.RecordingUploadURL(rec, audioRequest("POST", "/", map[string]any{"content_type": "audio/mpeg", "bytes": 10},
		map[string]string{"workId": "47", "id": "6"})) // глава тома 48
	if rec.Code != http.StatusNotFound {
		t.Errorf("чужая глава: %d", rec.Code)
	}
}

func TestRegisterRecordingChecksSizeAndPrefix(t *testing.T) {
	h, f, mem := newAudioTestHandler()
	key := "works/47/rec/5/0123abcd.mp3"
	mem.Put(context.Background(), key, strings.NewReader("mp3data"), -1, "audio/mpeg")
	vars := map[string]string{"workId": "47", "id": "5"}
	// Объекты лежат и под отвергаемыми ключами: отказ обязан дать проверка
	// ключа, а не случайное «файла нет» от Head.
	for _, k := range []string{"works/47/rec/9/0123abcd.mp3", "works/47/rec/5/../../x.mp3", "works/47/rec/5/0123abcd.flac"} {
		mem.Put(context.Background(), k, strings.NewReader("mp3data"), -1, "audio/mpeg")
	}

	for name, body := range map[string]map[string]any{
		"размер":            {"key": key, "content_type": "audio/mpeg", "bytes": 99, "duration_ms": 1000},
		"префикс":           {"key": "works/47/rec/9/0123abcd.mp3", "content_type": "audio/mpeg", "bytes": 7, "duration_ms": 1000},
		"выход из каталога": {"key": "works/47/rec/5/../../x.mp3", "content_type": "audio/mpeg", "bytes": 7, "duration_ms": 1000},
		"расширение":        {"key": "works/47/rec/5/0123abcd.flac", "content_type": "audio/mpeg", "bytes": 7, "duration_ms": 1000},
		"тип":               {"key": key, "content_type": "audio/flac", "bytes": 7, "duration_ms": 1000},
	} {
		rec := httptest.NewRecorder()
		h.RegisterRecording(rec, audioRequest("POST", "/", body, vars))
		if rec.Code != http.StatusConflict && rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	if len(f.recs) != 0 {
		t.Fatalf("записано при отказе: %+v", f.recs)
	}
	rec := httptest.NewRecorder()
	h.RegisterRecording(rec, audioRequest("POST", "/", map[string]any{
		"key": key, "content_type": "audio/mpeg", "bytes": 7, "duration_ms": 1000, "reader": "Иванов"}, vars))
	if rec.Code != http.StatusCreated || len(f.recs) != 1 || f.recs[0].Reader != "Иванов" || f.recs[0].ChapterID != 5 {
		t.Errorf("%d %+v", rec.Code, f.recs)
	}
}

// Тикет 02: повтор ключа (двойной клик) — 409 со своим текстом, а не 500 и
// не общий текст про заявку.
func TestRegisterRecordingTwiceIsConflict(t *testing.T) {
	h, f, mem := newAudioTestHandler()
	key := "works/47/rec/5/0123abcd.mp3"
	if err := mem.Put(context.Background(), key, strings.NewReader("mp3data"), -1, "audio/mpeg"); err != nil {
		t.Fatal(err)
	}
	f.err = repository.ErrAudioDuplicate
	rec := httptest.NewRecorder()
	h.RegisterRecording(rec, audioRequest("POST", "/", map[string]any{
		"key": key, "content_type": "audio/mpeg", "bytes": 7, "duration_ms": 1000},
		map[string]string{"workId": "47", "id": "5"}))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "уже зарегистрирована") {
		t.Errorf("повтор ключа: %d %s", rec.Code, rec.Body)
	}
}

// Тикет 02: размер ограничивался лишь равенством с Head — пустой файл
// регистрировался, а подписанный PUT размера не ограничивает вовсе.
func TestRegisterRecordingRefusesBytesOutOfRange(t *testing.T) {
	h, f, mem := newAudioTestHandler()
	vars := map[string]string{"workId": "47", "id": "5"}
	empty := "works/47/rec/5/00000000.mp3"
	if err := mem.Put(context.Background(), empty, strings.NewReader(""), -1, "audio/mpeg"); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]map[string]any{
		"ноль":         {"key": empty, "content_type": "audio/mpeg", "bytes": 0, "duration_ms": 1000},
		"выше потолка": {"key": "works/47/rec/5/0123abcd.mp3", "content_type": "audio/mpeg", "bytes": maxRecordingBytes + 1, "duration_ms": 1000},
	} {
		rec := httptest.NewRecorder()
		h.RegisterRecording(rec, audioRequest("POST", "/", body, vars))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	if len(f.recs) != 0 {
		t.Errorf("записано при отказе: %+v", f.recs)
	}
}

func TestDeleteRecordingRemovesObject(t *testing.T) {
	h, f, mem := newAudioTestHandler()
	f.recs = []models.AudioRecording{{ID: 7, S3Key: "works/47/rec/5/x.mp3"}}
	mem.Put(context.Background(), "works/47/rec/5/x.mp3", strings.NewReader("x"), -1, "")
	rec := httptest.NewRecorder()
	h.DeleteRecording(rec, audioRequest("DELETE", "/api/audio/rec/7", nil, map[string]string{"id": "7"}))
	if _, _, err := mem.Head(context.Background(), "works/47/rec/5/x.mp3"); rec.Code != http.StatusNoContent || err == nil {
		t.Errorf("%d, объект остался: %v", rec.Code, err)
	}
}
