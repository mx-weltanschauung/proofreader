package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"proofreader/internal/audio"
	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/pkg/storage"
)

// maxRecordingBytes — потолок одного файла записи человека (час FLAC —
// 300—600 МБ; диск боевого общий со сканами).
const maxRecordingBytes = 2 << 30

func (h *AudioHandler) recordingChapter(w http.ResponseWriter, r *http.Request) (workID, chapterID int64, ok bool) {
	if workID, ok = pathID(w, r, "workId", "тома"); !ok {
		return
	}
	if chapterID, ok = pathID(w, r, "id", "главы"); !ok {
		return
	}
	_, ok = h.chapterOfWork(w, r, workID, chapterID)
	return
}

// RecordingUploadURL — ключ и ссылка на заливку одного файла записи.
func (h *AudioHandler) RecordingUploadURL(w http.ResponseWriter, r *http.Request) {
	workID, chapterID, ok := h.recordingChapter(w, r)
	if !ok {
		return
	}
	var body struct {
		ContentType string `json:"content_type"`
		Bytes       int64  `json:"bytes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Тело запроса — JSON {content_type, bytes}")
		return
	}
	ext, known := models.RecordingContentTypes[body.ContentType]
	if !known {
		writeError(w, http.StatusBadRequest, "Формат не поддерживается: "+body.ContentType+
			". Годятся mp3, m4a, ogg, opus, flac")
		return
	}
	if body.Bytes <= 0 || body.Bytes > maxRecordingBytes {
		writeError(w, http.StatusBadRequest, "Размер файла — от 1 байта до 2 ГБ")
		return
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось выдать ключ")
		return
	}
	key := audio.RecordingKey(workID, chapterID, hex.EncodeToString(token), ext)
	url, err := h.store.PresignPut(r.Context(), key, body.ContentType, uploadURLTTL)
	if err != nil {
		log.Printf("ссылка на заливку записи %s: %v", key, err)
		writeError(w, http.StatusInternalServerError, "Не удалось выдать ссылку на заливку")
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]string{"key": key, "url": url, "content_type": body.ContentType})
}

// RegisterRecording — строка записи после заливки: Head обязан совпасть по
// размеру, позиция — в конец главы.
func (h *AudioHandler) RegisterRecording(w http.ResponseWriter, r *http.Request) {
	workID, chapterID, ok := h.recordingChapter(w, r)
	if !ok {
		return
	}
	var body struct {
		Key         string `json:"key"`
		ContentType string `json:"content_type"`
		Bytes       int64  `json:"bytes"`
		DurationMS  int64  `json:"duration_ms"`
		Reader      string `json:"reader"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Тело запроса — JSON {key, content_type, bytes, duration_ms, reader?}")
		return
	}
	ext, known := models.RecordingContentTypes[body.ContentType]
	prefix := fmt.Sprintf("works/%d/rec/%d/", workID, chapterID)
	// IsAudioKey не знает главы — префикс с номером главы проверяется отдельно.
	if !known || !strings.HasPrefix(body.Key, prefix) || !strings.HasSuffix(body.Key, "."+ext) ||
		!audio.IsAudioKey(workID, body.Key) || body.DurationMS <= 0 {
		writeError(w, http.StatusBadRequest, "Ключ, формат или длительность не те, что выданы на заливку")
		return
	}
	// Та же граница, что при выдаче ссылки: подписанный PUT размера не
	// ограничивает, а равенство с Head пропустило бы и пустой файл.
	if body.Bytes <= 0 || body.Bytes > maxRecordingBytes {
		writeError(w, http.StatusBadRequest, "Размер файла — от 1 байта до 2 ГБ")
		return
	}
	size, _, err := h.store.Head(r.Context(), body.Key)
	if errors.Is(err, storage.ErrObjectNotFound) {
		writeError(w, http.StatusConflict, "Файла нет в хранилище — заливка не дошла")
		return
	}
	if err != nil {
		log.Printf("Head %s: %v", body.Key, err)
		writeError(w, http.StatusInternalServerError, "Хранилище не ответило на сверку")
		return
	}
	if size != body.Bytes {
		writeError(w, http.StatusConflict, fmt.Sprintf("В хранилище %d байт, прислано %d — заливка оборвалась", size, body.Bytes))
		return
	}
	rec := &models.AudioRecording{WorkID: workID, ChapterID: chapterID, Reader: strings.TrimSpace(body.Reader),
		S3Key: body.Key, ContentType: body.ContentType, Bytes: body.Bytes, DurationMS: body.DurationMS}
	if claims, ok := middleware.GetUserFromContext(r.Context()); ok {
		rec.UploadedBy = &claims.UserID
	}
	if err := h.recs.CreateRecording(r.Context(), rec); err != nil {
		if errors.Is(err, repository.ErrAudioDuplicate) {
			writeError(w, http.StatusConflict, "Эта запись уже зарегистрирована — повторная отправка")
			return
		}
		mapAudioErr(w, err, "Глава не найдена в этом томе")
		return
	}
	writeJSONStatus(w, http.StatusCreated, recordingView{*rec, audio.RecordingURL("", rec.ID)})
}

// UpdateRecording — PATCH /audio/rec/{id} {reader?, position?}.
func (h *AudioHandler) UpdateRecording(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id", "записи")
	if !ok {
		return
	}
	var body struct {
		Reader   *string `json:"reader"`
		Position *int    `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || (body.Reader == nil && body.Position == nil) {
		writeError(w, http.StatusBadRequest, "Тело запроса — JSON {reader?, position?}, хотя бы одно поле")
		return
	}
	if body.Reader != nil {
		s := strings.TrimSpace(*body.Reader)
		body.Reader = &s
	}
	rec, err := h.recs.UpdateRecording(r.Context(), id, body.Reader, body.Position)
	if err != nil {
		mapAudioErr(w, err, "Записи нет")
		return
	}
	writeJSONStatus(w, http.StatusOK, recordingView{rec, audio.RecordingURL("", rec.ID)})
}

// DeleteRecording — строка, затем объект. Строка уходит первой: запись без
// файла отвечала бы слушателю ошибкой, а файл без строки — просто сирота.
func (h *AudioHandler) DeleteRecording(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id", "записи")
	if !ok {
		return
	}
	key, err := h.recs.DeleteRecording(r.Context(), id)
	if err != nil {
		mapAudioErr(w, err, "Записи нет")
		return
	}
	if err := h.store.Delete(r.Context(), key); err != nil {
		log.Printf("удаление записи %s: %v", key, err)
		writeError(w, http.StatusInternalServerError, "Запись снята, но файл остался в хранилище: "+key)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
