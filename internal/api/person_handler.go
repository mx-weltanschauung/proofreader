package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

const (
	maxCredits         = 10
	creditPrintedLimit = 200
	personNameLimit    = 200
	personSearchLimit  = 20
)

// PersonHandler — люди и подписи статей.
type PersonHandler struct {
	persons  PersonStore
	credits  CreditStore
	chapters ChapterStore
}

func NewPersonHandler(persons PersonStore, credits CreditStore, chapters ChapterStore) *PersonHandler {
	return &PersonHandler{persons: persons, credits: credits, chapters: chapters}
}

// Search — подсказка формы подписи: люди по началу ключа. Публично.
func (h *PersonHandler) Search(w http.ResponseWriter, r *http.Request) {
	list, err := h.persons.Search(r.Context(), r.URL.Query().Get("q"), personSearchLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось найти авторов")
		return
	}
	writeJSONStatus(w, http.StatusOK, list)
}

// Get — страница автора. Публично.
func (h *PersonHandler) Get(w http.ResponseWriter, r *http.Request) {
	d, err := h.persons.Detail(r.Context(), mux.Vars(r)["slug"])
	if errors.Is(err, repository.ErrPersonNotFound) {
		writeError(w, http.StatusNotFound, "Автор не найден")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать автора")
		return
	}
	writeJSONStatus(w, http.StatusOK, d)
}

type personRequest struct {
	ID      int64  `json:"id,omitempty"`
	Name    string `json:"name"`
	SortKey string `json:"sort_key"`
}

func (req *personRequest) validate() string {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return "Не указано имя"
	}
	if utf8.RuneCountInString(req.Name) > personNameLimit {
		return "Имя длиннее 200 знаков"
	}
	return ""
}

func (h *PersonHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req personRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID < 0 {
		writeError(w, http.StatusBadRequest, "Тело запроса — человек")
		return
	}
	if msg := req.validate(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	p := &models.Person{ID: req.ID, Name: req.Name, SortKey: strings.TrimSpace(req.SortKey)}
	if err := h.persons.Create(r.Context(), p); err != nil {
		if errors.Is(err, repository.ErrIDTaken) {
			writeError(w, http.StatusConflict, "Этот id уже занят")
			return
		}
		writeError(w, http.StatusInternalServerError, "Не удалось завести автора")
		return
	}
	writeJSONStatus(w, http.StatusCreated, p)
}

func (h *PersonHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := journalPathID(r)
	var req personRequest
	if !ok || json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, "Тело запроса — человек")
		return
	}
	if msg := req.validate(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	p := &models.Person{ID: id, Name: req.Name, SortKey: strings.TrimSpace(req.SortKey)}
	if err := h.persons.Update(r.Context(), p); err != nil {
		if errors.Is(err, repository.ErrPersonNotFound) {
			writeError(w, http.StatusNotFound, "Автор не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "Не удалось сохранить автора")
		return
	}
	writeJSONStatus(w, http.StatusOK, p)
}

// Merge сливает человека {from} в человека из пути.
func (h *PersonHandler) Merge(w http.ResponseWriter, r *http.Request) {
	id, ok := journalPathID(r)
	var req struct {
		From int64 `json:"from"`
	}
	if !ok || json.NewDecoder(r.Body).Decode(&req) != nil || req.From <= 0 {
		writeError(w, http.StatusBadRequest, "Тело запроса — {\"from\": id}")
		return
	}
	switch err := h.persons.Merge(r.Context(), id, req.From); {
	case errors.Is(err, repository.ErrMergeSelf):
		writeError(w, http.StatusBadRequest, "Человека нельзя слить с самим собой")
	case errors.Is(err, repository.ErrPersonNotFound):
		writeError(w, http.StatusNotFound, "Автор не найден")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "Не удалось слить авторов")
	default:
		p, err := h.persons.GetByID(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Не удалось прочитать автора")
			return
		}
		writeJSONStatus(w, http.StatusOK, p)
	}
}

// ReplaceCredits заменяет подпись статьи списком целиком.
func (h *PersonHandler) ReplaceCredits(w http.ResponseWriter, r *http.Request) {
	workID, err1 := strconv.ParseInt(mux.Vars(r)["workId"], 10, 64)
	chapterID, err2 := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err1 != nil || err2 != nil {
		writeError(w, http.StatusBadRequest, "Неверный адрес главы")
		return
	}
	var in []models.CreditInput
	// null декодируется в nil-срез без ошибки и молча снял бы подпись:
	// снять её можно только явным «[]».
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in == nil {
		writeError(w, http.StatusBadRequest, "Тело запроса — список строк подписи")
		return
	}
	if len(in) > maxCredits {
		writeError(w, http.StatusBadRequest, "В подписи не больше 10 строк")
		return
	}
	for i := range in {
		in[i].Printed = strings.TrimSpace(in[i].Printed)
		if in[i].Role != models.CreditRoleAuthor && in[i].Role != models.CreditRoleTranslator {
			writeError(w, http.StatusBadRequest, "Роль подписи — author или translator")
			return
		}
		if in[i].Printed == "" || utf8.RuneCountInString(in[i].Printed) > creditPrintedLimit {
			writeError(w, http.StatusBadRequest, "Печатная подпись пуста или длиннее 200 знаков")
			return
		}
	}
	ch, err := h.chapters.GetByID(r.Context(), chapterID)
	if err != nil || ch == nil || ch.WorkID != workID {
		writeError(w, http.StatusNotFound, "Глава не найдена")
		return
	}
	if err := h.credits.ReplaceCredits(r.Context(), chapterID, in); err != nil {
		if errors.Is(err, repository.ErrPersonNotFound) {
			writeError(w, http.StatusBadRequest, "В подписи указан несуществующий автор")
			return
		}
		writeError(w, http.StatusInternalServerError, "Не удалось сохранить подпись")
		return
	}
	all, err := h.credits.ListCreditsByWork(r.Context(), workID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать подпись")
		return
	}
	out := all[chapterID]
	if out == nil {
		out = []models.ArticleCredit{}
	}
	writeJSONStatus(w, http.StatusOK, out)
}
