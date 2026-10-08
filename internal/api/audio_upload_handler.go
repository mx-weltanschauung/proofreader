package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"

	"proofreader/internal/audio"
	"proofreader/internal/models"
	"proofreader/pkg/storage"
)

// trackContentType — контейнер синтеза: Ogg Opus.
const trackContentType = "audio/ogg"

// UploadURL — POST /works/{id}/audio/uploads: ключ и ссылка на заливку
// одной дорожки. Ключ строит сервер: worker не выбирает, куда писать.
func (h *AudioHandler) UploadURL(w http.ResponseWriter, r *http.Request) {
	workID, ok := pathID(w, r, "id", "тома")
	if !ok {
		return
	}
	var body struct {
		StartPage    int    `json:"start_page"`
		EndPage      int    `json:"end_page"`
		RecipeSHA256 string `json:"recipe_sha256"`
		PagesSHA256  string `json:"pages_sha256"`
		Bytes        int64  `json:"bytes"`
		Title        string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil ||
		body.StartPage < 1 || body.EndPage < body.StartPage || body.Bytes <= 0 || body.Title == "" ||
		!audio.ValidSHA256(body.RecipeSHA256) || !audio.ValidSHA256(body.PagesSHA256) {
		writeError(w, http.StatusBadRequest,
			"Нужны start_page ≤ end_page, bytes > 0, непустой title и два sha256 (64 знака 0-9a-f)")
		return
	}
	key := audio.TrackKey(workID, body.RecipeSHA256, body.PagesSHA256, body.StartPage, body.EndPage, body.Title)
	url, err := h.store.PresignPut(r.Context(), key, trackContentType, uploadURLTTL)
	if err != nil {
		log.Printf("ссылка на заливку %s: %v", key, err)
		writeError(w, http.StatusInternalServerError, "Не удалось выдать ссылку на заливку")
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]string{"key": key, "url": url, "content_type": trackContentType})
}

type registerTrack struct {
	Key          string `json:"key"`
	Title        string `json:"title"`
	StartPage    int    `json:"start_page"`
	EndPage      int    `json:"end_page"`
	DurationMS   int64  `json:"duration_ms"`
	Bytes        int64  `json:"bytes"`
	MD5          string `json:"md5"`
	RecipeSHA256 string `json:"recipe_sha256"`
	PagesSHA256  string `json:"pages_sha256"`
}

type objectMismatch struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

// RegisterTracks — POST /works/{id}/audio/tracks: регистрация дорожек одной
// заявки. Каждый объект сверяется Head: размер и ETag (= md5 однократного
// PUT, проверено на SeaweedFS в задаче 1 плана) обязаны совпасть, иначе 409
// с перечнем и ни одной строки — залитое остаётся сиротой до следующего
// прогона, но битый звук не встаёт на место целого.
func (h *AudioHandler) RegisterTracks(w http.ResponseWriter, r *http.Request) {
	workID, ok := pathID(w, r, "id", "тома")
	if !ok {
		return
	}
	var body struct {
		Tracks []registerTrack `json:"tracks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Tracks) == 0 {
		writeError(w, http.StatusBadRequest, "Тело запроса — JSON {tracks: [...]}, хотя бы одна дорожка")
		return
	}
	sorted := append([]registerTrack(nil), body.Tracks...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].StartPage < sorted[j].StartPage })
	for i, t := range sorted {
		if t.StartPage < 1 || t.EndPage < t.StartPage || t.Title == "" || t.Bytes <= 0 || t.DurationMS <= 0 ||
			!audio.ValidSHA256(t.RecipeSHA256) || !audio.ValidSHA256(t.PagesSHA256) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Дорожка %d—%d: неверные поля", t.StartPage, t.EndPage))
			return
		}
		if t.Key != audio.TrackKey(workID, t.RecipeSHA256, t.PagesSHA256, t.StartPage, t.EndPage, t.Title) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Дорожка %d—%d: ключ не тот, что выдан на заливку", t.StartPage, t.EndPage))
			return
		}
		if i > 0 && t.StartPage <= sorted[i-1].EndPage {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Дорожки %d—%d и %d—%d пересекаются",
				sorted[i-1].StartPage, sorted[i-1].EndPage, t.StartPage, t.EndPage))
			return
		}
	}

	var mismatches []objectMismatch
	for _, t := range body.Tracks {
		size, etag, err := h.store.Head(r.Context(), t.Key)
		switch {
		case errors.Is(err, storage.ErrObjectNotFound):
			mismatches = append(mismatches, objectMismatch{t.Key, "объекта нет в хранилище"})
		case err != nil:
			log.Printf("Head %s: %v", t.Key, err)
			writeError(w, http.StatusInternalServerError, "Хранилище не ответило на сверку")
			return
		case size != t.Bytes:
			mismatches = append(mismatches, objectMismatch{t.Key, fmt.Sprintf("размер %d, прислано %d", size, t.Bytes)})
		case etag != strings.ToLower(t.MD5):
			mismatches = append(mismatches, objectMismatch{t.Key, fmt.Sprintf("md5 %s, прислано %s", etag, t.MD5)})
		}
	}
	if len(mismatches) > 0 {
		writeJSONStatus(w, http.StatusConflict, map[string]any{
			"message":    "Залитое не совпало с присланным — дорожки не зарегистрированы",
			"mismatches": mismatches,
		})
		return
	}

	tracks := make([]models.AudioTrack, len(body.Tracks))
	for i, t := range body.Tracks {
		tracks[i] = models.AudioTrack{Title: t.Title, StartPage: t.StartPage, EndPage: t.EndPage, S3Key: t.Key,
			DurationMS: t.DurationMS, Bytes: t.Bytes, MD5: strings.ToLower(t.MD5),
			RecipeSHA256: t.RecipeSHA256, PagesSHA256: t.PagesSHA256}
	}
	inserted, freed, err := h.tracks.RegisterTracks(r.Context(), workID, tracks)
	if err != nil {
		mapAudioErr(w, err, "Том не найден")
		return
	}
	type out struct {
		ID        int64  `json:"id,omitempty"`
		Key       string `json:"key,omitempty"`
		Title     string `json:"title,omitempty"`
		StartPage int    `json:"start_page"`
		EndPage   int    `json:"end_page"`
		Stale     bool   `json:"stale"`
	}
	resp := struct {
		Tracks []out `json:"tracks"`
		Freed  []out `json:"freed"`
	}{Tracks: []out{}, Freed: []out{}}
	for _, t := range inserted {
		resp.Tracks = append(resp.Tracks, out{ID: t.ID, StartPage: t.StartPage, EndPage: t.EndPage, Stale: t.Stale})
	}
	for _, t := range freed {
		resp.Freed = append(resp.Freed, out{Key: t.S3Key, Title: t.Title, StartPage: t.StartPage, EndPage: t.EndPage})
	}
	writeJSONStatus(w, http.StatusOK, resp)
}

// DeleteObjects — DELETE /works/{id}/audio/objects {keys}: удалить объекты
// тома, на которые больше ничто не ссылается. Ключ со ссылкой — отказ всего
// запроса: удалить звук, который слушают, хуже, чем оставить сироту.
func (h *AudioHandler) DeleteObjects(w http.ResponseWriter, r *http.Request) {
	workID, ok := pathID(w, r, "id", "тома")
	if !ok {
		return
	}
	var body struct {
		Keys []string `json:"keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Тело запроса — JSON {keys: [...]}")
		return
	}
	prefix := audio.WorkPrefix(workID)
	var referenced []string
	for _, k := range body.Keys {
		if !strings.HasPrefix(k, prefix) || !audio.IsAudioKey(workID, k) {
			writeError(w, http.StatusBadRequest, "Ключ не из этого тома: "+k)
			return
		}
		ref, err := h.tracks.KeyReferenced(r.Context(), k)
		if err != nil {
			mapAudioErr(w, err, "")
			return
		}
		if ref {
			referenced = append(referenced, k)
		}
	}
	if len(referenced) > 0 {
		writeJSONStatus(w, http.StatusConflict, map[string]any{
			"message": "На эти объекты ссылаются дорожки или записи — ничего не удалено", "referenced": referenced})
		return
	}
	for _, k := range body.Keys {
		if err := h.store.Delete(r.Context(), k); err != nil {
			log.Printf("удаление %s: %v", k, err)
			writeError(w, http.StatusInternalServerError, "Не удалось удалить "+k)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
