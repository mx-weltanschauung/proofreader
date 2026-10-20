package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

// Пределы повестки и подписей (решение плана событий).
const (
	agendaMaxSections = 100
	agendaMaxEntries  = 200
	agendaMaxTargets  = 50
	agendaMaxRunes    = 200
)

func eventPathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	return id, err == nil && id > 0
}

// writeFail — ошибки записи: занятое — 409, нет — 404, плохая цель — 400.
func (h *EventHandler) writeFail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrEventIDTaken), errors.Is(err, repository.ErrEventSlugTaken):
		writeError(w, http.StatusConflict, "Такой id или слаг уже занят")
	case errors.Is(err, repository.ErrEventKindInUse):
		writeError(w, http.StatusConflict, "В виде есть события — сначала перенесите или удалите их")
	case errors.Is(err, repository.ErrAgendaTarget):
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), repository.ErrAgendaTarget.Error()+": "))
	default:
		h.fail(w, err)
	}
}

func decodeEventBody(w http.ResponseWriter, r *http.Request, v any, what string) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "Тело запроса — "+what)
		return false
	}
	return true
}

// checkSlug — слаг при создании: журнальная форма, не слова экрана редактора.
func checkSlug(slug string, reserved bool) string {
	if !journalSlugRe.MatchString(slug) {
		return "Слаг — строчные латинские буквы, цифры и дефис"
	}
	if reserved && (slug == "edit" || slug == "new") {
		return "Слова edit и new заняты адресами экрана редактора"
	}
	return ""
}

func checkTitle(title, what string) string {
	if strings.TrimSpace(title) == "" {
		return "Не указано " + what
	}
	if utf8.RuneCountInString(title) > agendaMaxRunes {
		return what + " длиннее 200 знаков"
	}
	return ""
}

// CreateOrganization заводит организацию; необязательный явный id.
func (h *EventHandler) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	var o models.EventOrganization
	if !decodeEventBody(w, r, &o, "организация") {
		return
	}
	o.Title = strings.TrimSpace(o.Title)
	if o.ID < 0 {
		writeError(w, http.StatusBadRequest, "id должен быть положительным")
		return
	}
	if msg := checkSlug(o.Slug, false); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if msg := checkTitle(o.Title, "заглавие организации"); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if err := h.store.CreateOrganization(r.Context(), &o); err != nil {
		h.writeFail(w, err)
		return
	}
	writeJSONStatus(w, http.StatusCreated, o)
}

// UpdateOrganization правит организацию; слаг тела не читается.
func (h *EventHandler) UpdateOrganization(w http.ResponseWriter, r *http.Request) {
	id, ok := eventPathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "Неверный номер организации")
		return
	}
	var o models.EventOrganization
	if !decodeEventBody(w, r, &o, "организация") {
		return
	}
	o.ID, o.Slug, o.Title = id, "", strings.TrimSpace(o.Title)
	if msg := checkTitle(o.Title, "заглавие организации"); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if err := h.store.UpdateOrganization(r.Context(), &o); err != nil {
		h.writeFail(w, err)
		return
	}
	// Слаг не меняется и в ответ не входит: экран перечитывает организацию.
	writeJSONStatus(w, http.StatusOK, o)
}

// CreateKind заводит вид в организации из пути.
func (h *EventHandler) CreateKind(w http.ResponseWriter, r *http.Request) {
	orgID, ok := eventPathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "Неверный номер организации")
		return
	}
	var k models.EventKind
	if !decodeEventBody(w, r, &k, "вид событий") {
		return
	}
	k.OrganizationID, k.Title = orgID, strings.TrimSpace(k.Title)
	if k.ID < 0 {
		writeError(w, http.StatusBadRequest, "id должен быть положительным")
		return
	}
	if msg := checkTitle(k.Title, "заглавие вида"); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if err := h.store.CreateKind(r.Context(), &k); err != nil {
		h.writeFail(w, err)
		return
	}
	writeJSONStatus(w, http.StatusCreated, k)
}

// UpdateKind правит заглавие и позицию вида; организация не меняется.
func (h *EventHandler) UpdateKind(w http.ResponseWriter, r *http.Request) {
	id, ok := eventPathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "Неверный номер вида")
		return
	}
	var k models.EventKind
	if !decodeEventBody(w, r, &k, "вид событий") {
		return
	}
	k.Title = strings.TrimSpace(k.Title)
	if msg := checkTitle(k.Title, "заглавие вида"); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	stored, err := h.store.KindByID(r.Context(), id)
	if err != nil {
		h.writeFail(w, err)
		return
	}
	k.ID, k.OrganizationID = id, stored.OrganizationID
	if err := h.store.UpdateKind(r.Context(), &k); err != nil {
		h.writeFail(w, err)
		return
	}
	writeJSONStatus(w, http.StatusOK, k)
}

// DeleteKind удаляет пустой вид; с событиями — 409.
func (h *EventHandler) DeleteKind(w http.ResponseWriter, r *http.Request) {
	id, ok := eventPathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "Неверный номер вида")
		return
	}
	if err := h.store.DeleteKind(r.Context(), id); err != nil {
		h.writeFail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CreateEvent заводит событие в виде из пути.
func (h *EventHandler) CreateEvent(w http.ResponseWriter, r *http.Request) {
	kindID, ok := eventPathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "Неверный номер вида")
		return
	}
	var e models.Event
	if !decodeEventBody(w, r, &e, "событие") {
		return
	}
	e.KindID, e.Title = kindID, strings.TrimSpace(e.Title)
	if e.ID < 0 {
		writeError(w, http.StatusBadRequest, "id должен быть положительным")
		return
	}
	if msg := checkSlug(e.Slug, true); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if msg := checkTitle(e.Title, "заглавие события"); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if err := h.store.CreateEvent(r.Context(), &e); err != nil {
		h.writeFail(w, err)
		return
	}
	writeJSONStatus(w, http.StatusCreated, e)
}

// UpdateEvent правит событие; слаг тела не читается. kind_id в теле переносит
// событие в другой вид — только той же организации.
func (h *EventHandler) UpdateEvent(w http.ResponseWriter, r *http.Request) {
	id, ok := eventPathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "Неверный номер события")
		return
	}
	var body models.Event
	if !decodeEventBody(w, r, &body, "событие") {
		return
	}
	body.Title = strings.TrimSpace(body.Title)
	if msg := checkTitle(body.Title, "заглавие события"); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	stored, err := h.store.EventByID(r.Context(), id)
	if err != nil {
		h.writeFail(w, err)
		return
	}
	if body.KindID != 0 && body.KindID != stored.KindID {
		cur, err := h.store.KindByID(r.Context(), stored.KindID)
		if err != nil {
			h.writeFail(w, err)
			return
		}
		target, err := h.store.KindByID(r.Context(), body.KindID)
		if errors.Is(err, repository.ErrEventKindNotFound) {
			writeError(w, http.StatusBadRequest, "Нет такого вида событий")
			return
		}
		if err != nil {
			h.writeFail(w, err)
			return
		}
		if target.OrganizationID != cur.OrganizationID {
			writeError(w, http.StatusBadRequest, "Событие можно перенести только в вид той же организации")
			return
		}
		stored.KindID = target.ID
	}
	stored.Title, stored.Dates, stored.Place = body.Title, body.Dates, body.Place
	stored.Description, stored.Position = body.Description, body.Position
	if err := h.store.UpdateEvent(r.Context(), stored); err != nil {
		h.writeFail(w, err)
		return
	}
	writeJSONStatus(w, http.StatusOK, stored)
}

// DeleteEvent удаляет событие вместе с повесткой.
func (h *EventHandler) DeleteEvent(w http.ResponseWriter, r *http.Request) {
	id, ok := eventPathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "Неверный номер события")
		return
	}
	if err := h.store.DeleteEvent(r.Context(), id); err != nil {
		h.writeFail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validateAgenda возвращает текст первой ошибки с номерами пункта и строки
// (с единицы) или пустую строку.
func validateAgenda(in *models.AgendaInput) string {
	if len(in.Sections) > agendaMaxSections {
		return fmt.Sprintf("пунктов не больше %d", agendaMaxSections)
	}
	long := func(s string) bool { return utf8.RuneCountInString(s) > agendaMaxRunes }
	for si, sec := range in.Sections {
		at := fmt.Sprintf("пункт %d", si+1)
		if strings.TrimSpace(sec.Title) == "" {
			return at + ": не указано заглавие пункта"
		}
		if long(sec.Title) {
			return at + ": заглавие длиннее 200 знаков"
		}
		if len(sec.Entries) > agendaMaxEntries {
			return fmt.Sprintf("%s: строк не больше %d", at, agendaMaxEntries)
		}
		for ei, ent := range sec.Entries {
			at := fmt.Sprintf("пункт %d, строка %d", si+1, ei+1)
			if strings.TrimSpace(ent.Label) == "" {
				return at + ": не указана подпись строки"
			}
			if long(ent.Label) {
				return at + ": подпись длиннее 200 знаков"
			}
			if len(ent.Targets) > agendaMaxTargets {
				return fmt.Sprintf("%s: целей не больше %d", at, agendaMaxTargets)
			}
			for ti, tg := range ent.Targets {
				at := fmt.Sprintf("%s, цель %d", at, ti+1)
				switch tg.Kind {
				case models.EventTargetChapter:
					if tg.ChapterID == nil {
						return at + ": у цели-главы нет chapter_id"
					}
				case models.EventTargetWork:
					if tg.ChapterID != nil {
						return at + ": у цели-тома не бывает chapter_id"
					}
				default:
					return at + ": вид цели — chapter или work"
				}
				if long(tg.Label) {
					return at + ": подпись длиннее 200 знаков"
				}
			}
		}
	}
	return ""
}

// ReplaceAgenda заменяет повестку события и отдаёт его в сотрудническом виде.
func (h *EventHandler) ReplaceAgenda(w http.ResponseWriter, r *http.Request) {
	id, ok := eventPathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "Неверный номер события")
		return
	}
	var in models.AgendaInput
	if !decodeEventBody(w, r, &in, "повестка") {
		return
	}
	if msg := validateAgenda(&in); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	ev, err := h.store.EventByID(r.Context(), id)
	if err != nil {
		h.writeFail(w, err)
		return
	}
	if err := h.store.ReplaceAgenda(r.Context(), id, in); err != nil {
		h.writeFail(w, err)
		return
	}
	d, err := h.store.EventDetail(r.Context(), ev.Slug)
	if err != nil {
		h.writeFail(w, err)
		return
	}
	closed, err := fullClosedSet(r.Context(), h.closed)
	if err != nil {
		h.fail(w, err)
		return
	}
	out, ok := visibleEvent(d, closed, true)
	if !ok {
		writeError(w, http.StatusNotFound, "Не найдено")
		return
	}
	writeJSONStatus(w, http.StatusOK, out)
}
