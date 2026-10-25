package api

import (
	"context"
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
	// chapters — закрытые главы; nil — закрытых глав нет.
	chapters closedChapterLister
}

// closedChapterLister — все главы, закрытые действующе
// (repository.ClosedRepository).
type closedChapterLister interface {
	AllClosedChapterIDs(ctx context.Context) (map[int64]bool, error)
}

// WithClosedChapters подключает закрытые главы: цель-глава в них для
// не-сотрудника не существует, как цель в закрытой работе.
func (h *EventHandler) WithClosedChapters(c closedChapterLister) *EventHandler {
	h.chapters = c
	return h
}

// allClosedChapters — множество закрытых глав; nil-хранилище — пустое.
func allClosedChapters(ctx context.Context, l closedChapterLister) (ClosedSet, error) {
	if l == nil {
		return nil, nil
	}
	ids, err := l.AllClosedChapterIDs(ctx)
	if err != nil {
		return nil, err
	}
	return ClosedSet(ids), nil
}

func NewEventHandler(store EventStore, closed ClosedStore) *EventHandler {
	return &EventHandler{store: store, closed: closed}
}

func (h *EventHandler) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrEventOrgNotFound),
		errors.Is(err, repository.ErrEventKindNotFound),
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
	closedChapters, err := allClosedChapters(r.Context(), h.chapters)
	if err != nil {
		h.fail(w, err)
		return
	}
	out, ok := visibleOrganization(org, closed, closedChapters, isStaffRequest(r))
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
	closedChapters, err := allClosedChapters(r.Context(), h.chapters)
	if err != nil {
		h.fail(w, err)
		return
	}
	out, ok := visibleEvent(d, closed, closedChapters, isStaffRequest(r))
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
	// Закрытая глава не-сотруднику не существует: ссылки «Входит в» её не
	// выдают, 404 как у несуществующей.
	if !isStaffRequest(r) {
		closedChapters, err := allClosedChapters(r.Context(), h.chapters)
		if err != nil {
			h.fail(w, err)
			return
		}
		if closedChapters.Has(chapterID) {
			writeError(w, http.StatusNotFound, "Не найдено")
			return
		}
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
