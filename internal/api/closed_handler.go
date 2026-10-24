package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/internal/repository"
)

// ClosedWriter — запись признака «закрыт». Объявлен на стороне потребителя;
// *repository.ClosedRepository удовлетворяет ему как есть. Каждый метод
// возвращает id работ, чья действующая закрытость могла смениться.
type ClosedWriter interface {
	SetWorkClosed(ctx context.Context, id int64, closed bool, actor models.ClosureActor, reason string) ([]int64, error)
	SetEditionClosed(ctx context.Context, id int64, closed bool, actor models.ClosureActor, reason string) ([]int64, error)
	SetJournalClosed(ctx context.Context, id int64, closed bool, actor models.ClosureActor, reason string) ([]int64, error)
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
	// Reason — причина закрытия или открытия; журнал закрытий хранит её
	// обязательной (задача 2 отвечает на пустую 400 до похода в базу).
	Reason string `json:"reason"`
}

// closedAtCreation — причина строки журнала, когда работа, издание или журнал
// заведены сразу закрытыми и своей причины в запросе нет.
const closedAtCreation = "закрыто при создании"

// closureActor — кто пишет в журнал закрытий: учётная запись из JWT. Подпись —
// email сотрудника, у читателя — ник; без claims — «неизвестно», чтобы строка
// журнала не осталась безымянной.
func closureActor(r *http.Request) models.ClosureActor {
	claims, ok := middleware.GetUserFromContext(r.Context())
	if !ok || claims == nil {
		return models.ClosureActor{Name: "неизвестно"}
	}
	name := claims.Email
	if name == "" {
		name = claims.Nickname
	}
	if name == "" {
		name = "неизвестно"
	}
	id := claims.UserID
	return models.ClosureActor{ID: &id, Name: name}
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
	write func(ctx context.Context, id int64, closed bool, actor models.ClosureActor, reason string) ([]int64, error)) {
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
	affected, err := write(r.Context(), id, *body.Closed, closureActor(r), body.Reason)
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
