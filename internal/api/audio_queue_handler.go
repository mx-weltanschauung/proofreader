package api

import (
	"encoding/json"
	"net/http"
	"time"

	"proofreader/internal/middleware"
	"proofreader/internal/models"
)

// chapterOfWork — глава, принадлежащая тому; иначе ответ 404 и false.
func (h *AudioHandler) chapterOfWork(w http.ResponseWriter, r *http.Request, workID, chapterID int64) (*models.Chapter, bool) {
	c, err := h.chapters.GetByID(r.Context(), chapterID)
	if err != nil || c == nil || c.WorkID != workID {
		writeError(w, http.StatusNotFound, "Глава не найдена в этом томе")
		return nil, false
	}
	return c, true
}

// Enqueue — POST /works/{id}/audio/queue {chapter_id?}.
func (h *AudioHandler) Enqueue(w http.ResponseWriter, r *http.Request) {
	workID, ok := pathID(w, r, "id", "тома")
	if !ok {
		return
	}
	var body struct {
		ChapterID *int64 `json:"chapter_id"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "Тело запроса — JSON {chapter_id?}")
			return
		}
	}
	if body.ChapterID != nil {
		if _, ok := h.chapterOfWork(w, r, workID, *body.ChapterID); !ok {
			return
		}
	}
	var by *int64
	if claims, ok := middleware.GetUserFromContext(r.Context()); ok {
		by = &claims.UserID
	}
	item, created, err := h.queue.Enqueue(r.Context(), workID, body.ChapterID, by)
	if err != nil {
		mapAudioErr(w, err, "Том не найден")
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSONStatus(w, status, item)
}

// Queue — GET /audio/queue: незавершённые и упавшие заявки плюс сводка
// устаревших дорожек по томам.
func (h *AudioHandler) Queue(w http.ResponseWriter, r *http.Request) {
	items, err := h.queue.ListOpenQueue(r.Context())
	if err != nil {
		mapAudioErr(w, err, "")
		return
	}
	stale, err := h.queue.StaleSummary(r.Context())
	if err != nil {
		mapAudioErr(w, err, "")
		return
	}
	if items == nil {
		items = []models.AudioQueueItem{}
	}
	if stale == nil {
		stale = []models.AudioStaleWork{}
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{"items": items, "stale": stale})
}

func (h *AudioHandler) CancelQueue(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id", "заявки")
	if !ok {
		return
	}
	if err := h.queue.CancelQueueItem(r.Context(), id); err != nil {
		mapAudioErr(w, err, "Заявки нет")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AudioHandler) RetryQueue(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id", "заявки")
	if !ok {
		return
	}
	item, err := h.queue.RetryQueueItem(r.Context(), id)
	if err != nil {
		mapAudioErr(w, err, "Заявки нет")
		return
	}
	writeJSONStatus(w, http.StatusOK, item)
}

// RequeueStale — POST /works/{id}/audio/requeue-stale: на каждую устаревшую
// дорожку — заявка на самую узкую главу, накрывающую её первую полосу (то же
// правило, что у /seo), а если первая полоса вне глав — её последнюю;
// не накрытая ничем с обоих концов — заявка на весь том. Свежие
// дорожки тех же глав worker пропустит сам.
func (h *AudioHandler) RequeueStale(w http.ResponseWriter, r *http.Request) {
	workID, ok := pathID(w, r, "id", "тома")
	if !ok {
		return
	}
	stale, err := h.tracks.StaleTracks(r.Context(), workID)
	if err != nil {
		mapAudioErr(w, err, "")
		return
	}
	var by *int64
	if claims, ok := middleware.GetUserFromContext(r.Context()); ok {
		by = &claims.UserID
	}
	seen := map[int64]bool{} // 0 — весь том
	items := []models.AudioQueueItem{}
	for _, t := range stale {
		c, err := h.chapters.FindByPage(r.Context(), workID, t.StartPage)
		if err == nil && c == nil {
			// Слипание уносит титул перед первой главой в её дорожку: начало
			// вне глав, конец в главе — заявка на главу конца, не на том.
			c, err = h.chapters.FindByPage(r.Context(), workID, t.EndPage)
		}
		if err != nil {
			mapAudioErr(w, err, "")
			return
		}
		var chapterID *int64
		key := int64(0)
		if c != nil {
			chapterID, key = &c.ID, c.ID
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		item, _, err := h.queue.Enqueue(r.Context(), workID, chapterID, by)
		if err != nil {
			mapAudioErr(w, err, "Том не найден")
			return
		}
		items = append(items, item)
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{"queued": len(items), "items": items})
}

// Claim — POST /audio/queue/claim {ids?, reclaim?}.
func (h *AudioHandler) Claim(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs     []int64 `json:"ids"`
		Reclaim bool    `json:"reclaim"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "Тело запроса — JSON {ids?, reclaim?}")
			return
		}
	}
	items, err := h.queue.Claim(r.Context(), body.IDs, body.Reclaim)
	if err != nil {
		mapAudioErr(w, err, "")
		return
	}
	if items == nil {
		items = []models.AudioQueueItem{}
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{"items": items})
}

// Result — POST /audio/queue/{id}/result {status, claimed_at, error?,
// status_counts?}. claimed_at — из ответа claim: итог прежнего забора после
// --reclaim получает 409 (FinishQueueItem сверяет отметку).
func (h *AudioHandler) Result(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id", "заявки")
	if !ok {
		return
	}
	var body struct {
		Status       string         `json:"status"`
		Error        string         `json:"error"`
		StatusCounts map[string]int `json:"status_counts"`
		// Отметка забора из ответа claim — токен: итог прежнего забора после
		// --reclaim получает 409, а не закрывает чужую заявку.
		ClaimedAt *time.Time `json:"claimed_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ClaimedAt == nil {
		writeError(w, http.StatusBadRequest, "Тело запроса — JSON {status, claimed_at, error?, status_counts?}; "+
			"claimed_at — из ответа claim")
		return
	}
	switch body.Status {
	case models.AudioDone, models.AudioQueued:
	case models.AudioFailed:
		if body.Error == "" {
			writeError(w, http.StatusBadRequest, "У ошибки обязателен текст — иначе на экране очереди нечего показать")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "status — готово, ошибка или в_очереди")
		return
	}
	if err := h.queue.FinishQueueItem(r.Context(), id, *body.ClaimedAt, body.Status, body.Error, body.StatusCounts); err != nil {
		mapAudioErr(w, err, "Заявки нет")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
