package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/internal/repository"
)

var journalSlugRe = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

// maxIssueSpan — сколько номеров разом покрывает одна книжка («10—12»):
// больше — почти наверняка опечатка в теле запроса.
const maxIssueSpan = 3

// JournalHandler — журналы и номера.
type JournalHandler struct {
	store JournalStore
}

func NewJournalHandler(store JournalStore) *JournalHandler {
	return &JournalHandler{store: store}
}

type journalBody struct {
	ID          int64  `json:"id,omitempty"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle"`
	Description string `json:"description"`
}

type issueRequest struct {
	ID         int64  `json:"id,omitempty"`
	WorkID     int64  `json:"work_id,omitempty"`
	Year       int    `json:"year"`
	NumberFrom int    `json:"number_from"`
	NumberTo   int    `json:"number_to"`
	Months     string `json:"months"`
}

func journalPathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	return id, err == nil && id > 0
}

// List — журналы со сводкой. Публично.
func (h *JournalHandler) List(w http.ResponseWriter, r *http.Request) {
	list, err := h.store.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать журналы")
		return
	}
	writeJSONStatus(w, http.StatusOK, list)
}

// Get — журнал и номера по годам. Публично.
func (h *JournalHandler) Get(w http.ResponseWriter, r *http.Request) {
	d, err := h.store.Detail(r.Context(), mux.Vars(r)["slug"])
	if errors.Is(err, repository.ErrJournalNotFound) {
		writeError(w, http.StatusNotFound, "Журнал не найден")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать журнал")
		return
	}
	writeJSONStatus(w, http.StatusOK, d)
}

func (req *journalBody) validate() string {
	req.Title = strings.TrimSpace(req.Title)
	if !journalSlugRe.MatchString(req.Slug) {
		return "Слаг журнала — строчные латинские буквы, цифры и дефис"
	}
	if req.Title == "" {
		return "Не указано заглавие журнала"
	}
	return ""
}

// Create заводит журнал; необязательный явный id — для публикатора.
func (h *JournalHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req journalBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Тело запроса — журнал")
		return
	}
	if req.ID < 0 {
		writeError(w, http.StatusBadRequest, "id должен быть положительным")
		return
	}
	if msg := req.validate(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	j := &models.Journal{ID: req.ID, Slug: req.Slug, Title: req.Title, Subtitle: req.Subtitle, Description: req.Description}
	if err := h.store.Create(r.Context(), j); err != nil {
		h.writeStoreError(w, err, "Не удалось завести журнал")
		return
	}
	writeJSONStatus(w, http.StatusCreated, j)
}

// Update правит журнал.
func (h *JournalHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := journalPathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "Неверный номер журнала")
		return
	}
	var req journalBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Тело запроса — журнал")
		return
	}
	if msg := req.validate(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	j := &models.Journal{ID: id, Slug: req.Slug, Title: req.Title, Subtitle: req.Subtitle, Description: req.Description}
	if err := h.store.Update(r.Context(), j); err != nil {
		h.writeStoreError(w, err, "Не удалось сохранить журнал")
		return
	}
	writeJSONStatus(w, http.StatusOK, j)
}

func (req *issueRequest) normalize() string {
	if req.NumberTo == 0 {
		req.NumberTo = req.NumberFrom
	}
	req.Months = strings.TrimSpace(req.Months)
	switch {
	case req.Year < 1800 || req.Year > 2100:
		return "Год номера вне 1800—2100"
	case req.NumberFrom < 1:
		return "Номер начинается с 1"
	case req.NumberTo < req.NumberFrom:
		return "Последний номер книжки меньше первого"
	case req.NumberTo-req.NumberFrom >= maxIssueSpan:
		return "Книжка покрывает больше трёх номеров — проверьте тело запроса"
	}
	return ""
}

// CreateIssue заводит номер с его работой. Явные id номера и работы —
// для публикатора (id локально = боевой).
func (h *JournalHandler) CreateIssue(w http.ResponseWriter, r *http.Request) {
	journalID, ok := journalPathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "Неверный номер журнала")
		return
	}
	claims, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "Нужен вход")
		return
	}
	var req issueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Тело запроса — номер журнала")
		return
	}
	if req.ID < 0 || req.WorkID < 0 {
		writeError(w, http.StatusBadRequest, "id должен быть положительным")
		return
	}
	if msg := req.normalize(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	j, err := h.store.GetByID(r.Context(), journalID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Журнал не найден")
		return
	}
	label := models.IssueLabel(req.NumberFrom, req.NumberTo)
	issue := &models.JournalIssue{ID: req.ID, JournalID: j.ID, Year: req.Year, NumberFrom: req.NumberFrom,
		NumberTo: req.NumberTo, Label: label, Months: req.Months}
	work := &models.Work{ID: req.WorkID, Title: models.IssueTitle(j.Title, req.Year, label), Language: "ru",
		Status: models.WorkStatusDraft, OwnerID: claims.UserID}
	if err := h.store.CreateIssue(r.Context(), issue, work); err != nil {
		h.writeStoreError(w, err, "Не удалось завести номер")
		return
	}
	writeJSONStatus(w, http.StatusCreated, map[string]any{"issue": issue, "work": work})
}

// UpdateIssue правит координаты номера; заглавие работы пересобирается.
func (h *JournalHandler) UpdateIssue(w http.ResponseWriter, r *http.Request) {
	id, ok := journalPathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "Неверный номер")
		return
	}
	var req issueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Тело запроса — номер журнала")
		return
	}
	if msg := req.normalize(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	issue, err := h.store.GetIssue(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "Номер не найден")
		return
	}
	j, err := h.store.GetByID(r.Context(), issue.JournalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Журнал номера не найден")
		return
	}
	issue.Year, issue.NumberFrom, issue.NumberTo, issue.Months = req.Year, req.NumberFrom, req.NumberTo, req.Months
	issue.Label = models.IssueLabel(req.NumberFrom, req.NumberTo)
	if err := h.store.UpdateIssue(r.Context(), issue, models.IssueTitle(j.Title, issue.Year, issue.Label)); err != nil {
		h.writeStoreError(w, err, "Не удалось сохранить номер")
		return
	}
	writeJSONStatus(w, http.StatusOK, issue)
}

func (h *JournalHandler) writeStoreError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, repository.ErrIDTaken):
		writeError(w, http.StatusConflict, "Этот id уже занят")
	case errors.Is(err, repository.ErrJournalSlugTaken):
		writeError(w, http.StatusConflict, "Журнал с таким слагом уже есть")
	case errors.Is(err, repository.ErrIssueTaken):
		writeError(w, http.StatusConflict, "Такой номер этого года уже есть")
	case errors.Is(err, repository.ErrJournalNotFound), errors.Is(err, repository.ErrIssueNotFound):
		writeError(w, http.StatusNotFound, "Не найдено")
	default:
		writeError(w, http.StatusInternalServerError, fallback)
	}
}
