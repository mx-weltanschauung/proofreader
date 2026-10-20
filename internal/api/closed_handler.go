package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"proofreader/internal/repository"
)

// ClosedWriter — запись признака «закрыт». Объявлен на стороне потребителя;
// *repository.ClosedRepository удовлетворяет ему как есть. Каждый метод
// возвращает id работ, чья действующая закрытость могла смениться.
type ClosedWriter interface {
	SetWorkClosed(ctx context.Context, id int64, closed bool) ([]int64, error)
	SetEditionClosed(ctx context.Context, id int64, closed bool) ([]int64, error)
	SetJournalClosed(ctx context.Context, id int64, closed bool) ([]int64, error)
}

// ClosedHandler — маршруты «закрыть/открыть» работу, издание и журнал.
// Общие Update признак не пишут намеренно: у него один путь записи, и он
// же сбрасывает кэши.
type ClosedHandler struct {
	store ClosedWriter
	cache *ServingCache
}

func NewClosedHandler(store ClosedWriter, cache *ServingCache) *ClosedHandler {
	return &ClosedHandler{store: store, cache: cache}
}

type closedBody struct {
	Closed *bool `json:"closed"`
}

type closedResponse struct {
	ID       int64   `json:"id"`
	Closed   bool    `json:"closed"`
	Affected []int64 `json:"affected"`
}

func (h *ClosedHandler) Work(w http.ResponseWriter, r *http.Request) {
	h.set(w, r, h.store.SetWorkClosed)
}

func (h *ClosedHandler) Edition(w http.ResponseWriter, r *http.Request) {
	h.set(w, r, h.store.SetEditionClosed)
}

func (h *ClosedHandler) Journal(w http.ResponseWriter, r *http.Request) {
	h.set(w, r, h.store.SetJournalClosed)
}

func (h *ClosedHandler) set(w http.ResponseWriter, r *http.Request,
	write func(ctx context.Context, id int64, closed bool) ([]int64, error)) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "Неверный номер")
		return
	}
	var body closedBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Closed == nil {
		writeError(w, http.StatusBadRequest, "Тело запроса — {\"closed\": true|false}")
		return
	}
	affected, err := write(r.Context(), id, *body.Closed)
	if errors.Is(err, repository.ErrClosedTargetNotFound) {
		writeError(w, http.StatusNotFound, "Не найдено")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось сохранить признак")
		return
	}
	// Кэши бьются и при закрытии, и при открытии: открытое тоже могло лежать
	// в кэше отказом. Краулерская половина сбрасывается всегда, даже без
	// затронутых работ: у издания и журнала своя страница и карточка.
	if len(affected) == 0 {
		h.cache.DropCrawler()
	}
	for _, wid := range affected {
		h.cache.DropWork(wid)
	}
	if affected == nil {
		affected = []int64{}
	}
	writeJSONStatus(w, http.StatusOK, closedResponse{ID: id, Closed: *body.Closed, Affected: affected})
}
