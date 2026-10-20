package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

// EventHandler — чтение организаций, событий и ссылок из глав на события.
type EventHandler struct {
	store  EventStore
	closed ClosedStore
}

func NewEventHandler(store EventStore, closed ClosedStore) *EventHandler {
	return &EventHandler{store: store, closed: closed}
}

func (h *EventHandler) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrEventOrgNotFound),
		errors.Is(err, repository.ErrEventNotFound),
		errors.Is(err, repository.ErrChapterNotInWork):
		writeError(w, http.StatusNotFound, "Не найдено")
	default:
		writeError(w, http.StatusInternalServerError, "Ошибка сервера")
	}
}

// GetOrganization — организация с видами и событиями. Публично.
func (h *EventHandler) GetOrganization(w http.ResponseWriter, r *http.Request) {
	org, err := h.store.OrganizationDetail(r.Context(), mux.Vars(r)["slug"])
	if err != nil {
		h.fail(w, err)
		return
	}
	closed, err := fullClosedSet(r.Context(), h.closed)
	if err != nil {
		h.fail(w, err)
		return
	}
	out, ok := visibleOrganization(org, closed, isStaffRequest(r))
	if !ok {
		writeError(w, http.StatusNotFound, "Не найдено")
		return
	}
	writeJSONStatus(w, http.StatusOK, out)
}

// GetEvent — событие с повесткой. Публично. Слаг организации в адресе должен
// быть слагом организации события.
func (h *EventHandler) GetEvent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	d, err := h.store.EventDetail(r.Context(), vars["eventSlug"])
	if err != nil {
		h.fail(w, err)
		return
	}
	if d.Organization.Slug != vars["slug"] {
		writeError(w, http.StatusNotFound, "Не найдено")
		return
	}
	closed, err := fullClosedSet(r.Context(), h.closed)
	if err != nil {
		h.fail(w, err)
		return
	}
	out, ok := visibleEvent(d, closed, isStaffRequest(r))
	if !ok {
		writeError(w, http.StatusNotFound, "Не найдено")
		return
	}
	writeJSONStatus(w, http.StatusOK, out)
}

// ChapterEvents — «Входит в: …» у главы. Закрытость тома проверил closedGate.
func (h *EventHandler) ChapterEvents(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workID, err1 := strconv.ParseInt(vars["workId"], 10, 64)
	chapterID, err2 := strconv.ParseInt(vars["id"], 10, 64)
	if err1 != nil || err2 != nil {
		writeError(w, http.StatusNotFound, "Не найдено")
		return
	}
	links, err := h.store.ChapterEvents(r.Context(), workID, chapterID)
	if err != nil {
		h.fail(w, err)
		return
	}
	if links == nil {
		links = []models.ChapterEventLink{}
	}
	writeJSONStatus(w, http.StatusOK, links)
}
