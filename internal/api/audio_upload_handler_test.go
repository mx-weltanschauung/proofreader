package api

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"proofreader/internal/audio"
	"proofreader/internal/models"
)

const (
	recipeT = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	pagesT  = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func TestUploadURLValidatesAndBuildsKey(t *testing.T) {
	h, _, _ := newAudioTestHandler()
	ok := map[string]any{"start_page": 43, "end_page": 93, "recipe_sha256": recipeT, "pages_sha256": pagesT, "bytes": 1000,
		"title": "Глава первая"}
	rec := httptest.NewRecorder()
	h.UploadURL(rec, audioRequest("POST", "/api/works/47/audio/uploads", ok, map[string]string{"id": "47"}))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), audio.TrackKey(47, recipeT, pagesT, 43, 93, "Глава первая")) ||
		!strings.Contains(rec.Body.String(), "audio/ogg") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	for _, bad := range []map[string]any{
		{"start_page": 5, "end_page": 4, "recipe_sha256": recipeT, "pages_sha256": pagesT, "bytes": 1, "title": "Глава"},
		{"start_page": 1, "end_page": 2, "recipe_sha256": "../x", "pages_sha256": pagesT, "bytes": 1, "title": "Глава"},
		{"start_page": 1, "end_page": 2, "recipe_sha256": recipeT, "pages_sha256": pagesT, "bytes": 0, "title": "Глава"},
		// Тикет 05: ключ строится по заголовку, а пустой регистрация не примет.
		{"start_page": 1, "end_page": 2, "recipe_sha256": recipeT, "pages_sha256": pagesT, "bytes": 1},
	} {
		rec := httptest.NewRecorder()
		h.UploadURL(rec, audioRequest("POST", "/api/works/47/audio/uploads", bad, map[string]string{"id": "47"}))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%v: %d", bad, rec.Code)
		}
	}
}

func regBody(key string, bytes int, md5hex string) map[string]any {
	return map[string]any{"tracks": []map[string]any{{
		"key": key, "title": "Глава", "start_page": 43, "end_page": 93, "duration_ms": 5000,
		"bytes": bytes, "md5": md5hex, "recipe_sha256": recipeT, "pages_sha256": pagesT,
	}}}
}

func TestRegisterTracksChecksHeadAndWritesNothingOnMismatch(t *testing.T) {
	h, f, mem := newAudioTestHandler()
	key := audio.TrackKey(47, recipeT, pagesT, 43, 93, "Глава")
	data := []byte("звук дорожки")
	mem.Put(context.Background(), key, strings.NewReader(string(data)), -1, "audio/ogg")
	sum := md5.Sum(data)
	good := hex.EncodeToString(sum[:])

	for name, body := range map[string]map[string]any{
		"размер":     regBody(key, len(data)+1, good),
		"md5":        regBody(key, len(data), strings.Repeat("0", 32)),
		"нет файла":  regBody(audio.TrackKey(47, recipeT, pagesT, 1, 2, "Глава"), len(data), good),
		"чужой ключ": regBody("works/47/"+recipeT[:8]+"/43-93-deadbeef.opus", len(data), good),
	} {
		f.registered = nil
		rec := httptest.NewRecorder()
		h.RegisterTracks(rec, audioRequest("POST", "/api/works/47/audio/tracks", body, map[string]string{"id": "47"}))
		if rec.Code != http.StatusConflict && rec.Code != http.StatusBadRequest {
			t.Errorf("%s: код %d", name, rec.Code)
		}
		if f.registered != nil {
			t.Errorf("%s: строки записаны при отказе", name)
		}
	}

	f.freed = []models.AudioTrack{{S3Key: "works/47/old.opus", StartPage: 1, EndPage: 100, Title: "Том"}}
	rec := httptest.NewRecorder()
	h.RegisterTracks(rec, audioRequest("POST", "/api/works/47/audio/tracks", regBody(key, len(data), good),
		map[string]string{"id": "47"}))
	if rec.Code != http.StatusOK || len(f.registered) != 1 || f.registered[0].MD5 != good {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	// md5 прописными в запросе — тоже успех
	f.registered = nil
	rec = httptest.NewRecorder()
	h.RegisterTracks(rec, audioRequest("POST", "/api/works/47/audio/tracks", regBody(key, len(data), strings.ToUpper(good)),
		map[string]string{"id": "47"}))
	if rec.Code != http.StatusOK || len(f.registered) != 1 || f.registered[0].MD5 != good {
		t.Fatalf("md5 прописными: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"key":"works/47/old.opus"`) || !strings.Contains(rec.Body.String(), `"start_page":1`) {
		t.Errorf("освободившиеся не отданы: %s", rec.Body)
	}
}

// Ключ не по схеме, но объект лежит, размер и md5 верны: только сверка
// ключа с TrackKey не даёт зарегистрировать чужое имя.
func TestRegisterTracksRefusesKeyOffScheme(t *testing.T) {
	h, f, mem := newAudioTestHandler()
	data := []byte("звук")
	sum := md5.Sum(data)
	bad := "works/47/" + recipeT[:8] + "/43-93-deadbeef.opus"
	mem.Put(context.Background(), bad, strings.NewReader(string(data)), -1, "audio/ogg")
	rec := httptest.NewRecorder()
	h.RegisterTracks(rec, audioRequest("POST", "/api/works/47/audio/tracks", regBody(bad, len(data), hex.EncodeToString(sum[:])),
		map[string]string{"id": "47"}))
	if rec.Code != http.StatusBadRequest || f.registered != nil {
		t.Errorf("ключ не по схеме: %d, записано %v", rec.Code, f.registered)
	}
}

// Тикет 05: ключ, выданный под одно название, под другим не регистрируется —
// иначе переименованная глава легла бы поверх объекта прежнего названия.
func TestRegisterTracksRefusesKeyOfAnotherTitle(t *testing.T) {
	h, f, mem := newAudioTestHandler()
	data := []byte("звук")
	sum := md5.Sum(data)
	key := audio.TrackKey(47, recipeT, pagesT, 43, 93, "Прежнее название")
	if err := mem.Put(context.Background(), key, strings.NewReader(string(data)), -1, "audio/ogg"); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.RegisterTracks(rec, audioRequest("POST", "/api/works/47/audio/tracks", regBody(key, len(data), hex.EncodeToString(sum[:])),
		map[string]string{"id": "47"}))
	if rec.Code != http.StatusBadRequest || f.registered != nil {
		t.Errorf("ключ чужого названия: %d, записано %v", rec.Code, f.registered)
	}
}

func TestRegisterTracksRefusesOverlapWithinRequest(t *testing.T) {
	h, _, _ := newAudioTestHandler()
	a := audio.TrackKey(47, recipeT, pagesT, 1, 10, "a")
	b := audio.TrackKey(47, recipeT, pagesT, 10, 20, "b")
	body := map[string]any{"tracks": []map[string]any{
		{"key": a, "title": "a", "start_page": 1, "end_page": 10, "duration_ms": 1, "bytes": 1, "md5": "x", "recipe_sha256": recipeT, "pages_sha256": pagesT},
		{"key": b, "title": "b", "start_page": 10, "end_page": 20, "duration_ms": 1, "bytes": 1, "md5": "x", "recipe_sha256": recipeT, "pages_sha256": pagesT},
	}}
	rec := httptest.NewRecorder()
	h.RegisterTracks(rec, audioRequest("POST", "/api/works/47/audio/tracks", body, map[string]string{"id": "47"}))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("пересечение внутри запроса: %d", rec.Code)
	}
}

func TestDeleteObjectsRefusesReferencedAndForeign(t *testing.T) {
	h, f, mem := newAudioTestHandler()
	ctx := context.Background()
	mem.Put(ctx, "works/47/rec/9/aaa.mp3", strings.NewReader("a"), -1, "")
	mem.Put(ctx, "works/47/rec/9/bbb.mp3", strings.NewReader("b"), -1, "")
	f.referenced = map[string]bool{"works/47/rec/9/bbb.mp3": true}

	rec := httptest.NewRecorder()
	h.DeleteObjects(rec, audioRequest("DELETE", "/api/works/47/audio/objects",
		map[string]any{"keys": []string{"works/47/rec/9/aaa.mp3", "works/47/rec/9/bbb.mp3"}}, map[string]string{"id": "47"}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("ключ со ссылкой: %d", rec.Code)
	}
	if _, _, err := mem.Head(ctx, "works/47/rec/9/aaa.mp3"); err != nil {
		t.Errorf("при отказе удалён соседний ключ: %v", err)
	}
	rec = httptest.NewRecorder()
	h.DeleteObjects(rec, audioRequest("DELETE", "/api/works/47/audio/objects",
		map[string]any{"keys": []string{"works/48/rec/9/aaa.mp3"}}, map[string]string{"id": "47"}))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("чужой том: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.DeleteObjects(rec, audioRequest("DELETE", "/api/works/47/audio/objects",
		map[string]any{"keys": []string{"works/47/rec/9/aaa.mp3"}}, map[string]string{"id": "47"}))
	if _, _, err := mem.Head(ctx, "works/47/rec/9/aaa.mp3"); rec.Code != http.StatusNoContent || err == nil {
		t.Errorf("свободный ключ не удалён: %d %v", rec.Code, err)
	}
}

func TestDeleteObjectsRefusesNonAudioShapes(t *testing.T) {
	h, _, mem := newAudioTestHandler()
	ctx := context.Background()
	mem.Put(ctx, "works/47/pages/page_1.png", strings.NewReader("p"), -1, "")
	mem.Put(ctx, "works/47/rec/9/abc123.mp3", strings.NewReader("r"), -1, "")
	rec := httptest.NewRecorder()
	h.DeleteObjects(rec, audioRequest("DELETE", "/api/works/47/audio/objects",
		map[string]any{"keys": []string{"works/47/rec/9/abc123.mp3", "works/47/pages/page_1.png"}}, map[string]string{"id": "47"}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("превью: %d", rec.Code)
	}
	if _, _, err := mem.Head(ctx, "works/47/pages/page_1.png"); err != nil {
		t.Errorf("превью удалено: %v", err)
	}
	if _, _, err := mem.Head(ctx, "works/47/rec/9/abc123.mp3"); err != nil {
		t.Errorf("соседний ключ удалён при отказе: %v", err)
	}
	rec = httptest.NewRecorder()
	h.DeleteObjects(rec, audioRequest("DELETE", "/api/works/47/audio/objects",
		map[string]any{"keys": []string{"works/47/rec/9/abc123.mp3"}}, map[string]string{"id": "47"}))
	if _, _, err := mem.Head(ctx, "works/47/rec/9/abc123.mp3"); rec.Code != http.StatusNoContent || err == nil {
		t.Errorf("запись не удалена: %d %v", rec.Code, err)
	}
}

func TestRegisterTracksRefusesZeroBytesAndDuration(t *testing.T) {
	h, f, _ := newAudioTestHandler()
	key := audio.TrackKey(47, recipeT, pagesT, 43, 93, "Глава")
	for name, mut := range map[string]func(map[string]any){
		"bytes":    func(m map[string]any) { m["bytes"] = 0 },
		"duration": func(m map[string]any) { m["duration_ms"] = 0 },
	} {
		body := regBody(key, 5, "x")
		mut(body["tracks"].([]map[string]any)[0])
		rec := httptest.NewRecorder()
		h.RegisterTracks(rec, audioRequest("POST", "/api/works/47/audio/tracks", body, map[string]string{"id": "47"}))
		if rec.Code != http.StatusBadRequest || f.registered != nil {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
}

// Провод для worker'а: массивы, не null; mismatches несёт key и reason.
func TestRegisterTracksWireShapes(t *testing.T) {
	h, f, mem := newAudioTestHandler()
	key := audio.TrackKey(47, recipeT, pagesT, 43, 93, "Глава")
	data := []byte("звук")
	mem.Put(context.Background(), key, strings.NewReader(string(data)), -1, "audio/ogg")
	sum := md5.Sum(data)
	f.freed = nil
	rec := httptest.NewRecorder()
	h.RegisterTracks(rec, audioRequest("POST", "/api/works/47/audio/tracks", regBody(key, len(data), hex.EncodeToString(sum[:])),
		map[string]string{"id": "47"}))
	var ok struct {
		Tracks []map[string]any `json:"tracks"`
		Freed  []any            `json:"freed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ok); err != nil || len(ok.Tracks) != 1 || ok.Freed == nil || len(ok.Freed) != 0 ||
		!strings.Contains(rec.Body.String(), `"freed":[]`) {
		t.Fatalf("успех: %v %s", err, rec.Body)
	}
	for _, k := range []string{"id", "start_page", "end_page", "stale"} {
		if _, has := ok.Tracks[0][k]; !has {
			t.Errorf("в tracks нет %s: %s", k, rec.Body)
		}
	}
	rec = httptest.NewRecorder()
	h.RegisterTracks(rec, audioRequest("POST", "/api/works/47/audio/tracks", regBody(key, len(data)+1, hex.EncodeToString(sum[:])),
		map[string]string{"id": "47"}))
	var bad struct {
		Message    string              `json:"message"`
		Mismatches []map[string]string `json:"mismatches"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &bad); err != nil || rec.Code != http.StatusConflict ||
		bad.Message == "" || len(bad.Mismatches) != 1 || bad.Mismatches[0]["key"] != key || bad.Mismatches[0]["reason"] == "" {
		t.Errorf("409: %d %s", rec.Code, rec.Body)
	}
}
