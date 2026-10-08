package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
)

const (
	// Предел частоты: сколько писем принимается с одной отметки адреса за окно.
	feedbackRateLimit  = 5
	feedbackRateWindow = time.Hour
	// Потолок тела запроса. Проверка «не длиннее 4000 символов» наступает
	// после чтения тела целиком — без потолка она слишком поздна.
	feedbackMaxBody = 64 << 10
	// Список в админке отдаётся без разбиения на страницы: писем ожидаются
	// единицы в месяц. Потолок всё равно нужен — иначе однажды страница
	// станет неподъёмной, и никто не узнает почему.
	feedbackListLimit = 500
	// feedbackNoAddressBucket — общее ведро для писем без адреса.
	//
	// Пустая отметка не должна отключать предел: раньше ветка
	// `if ipHash != ""` пропускала такое письмо вообще без проверки.
	// Безадресные запросы идут в одно общее ведро с тем же пределом —
	// строже, чем «мимо кассы».
	feedbackNoAddressBucket = "no-address"
)

// feedbackStore is the slice of repository.FeedbackRepository the handler uses.
// *repository.FeedbackRepository satisfies it as written; тесты подставляют
// склад в памяти и обходятся без базы.
type feedbackStore interface {
	CreateWithinLimit(ctx context.Context, f *models.Feedback, limit int, since time.Time) (bool, error)
	List(ctx context.Context, handled *bool, limit int) ([]*models.Feedback, error)
	CountUnread(ctx context.Context) (int, error)
	SetHandled(ctx context.Context, id int64, handled bool) error
	Delete(ctx context.Context, id int64) error
}

// FeedbackHandler serves the reader feedback form and its admin side.
type FeedbackHandler struct {
	repo       feedbackStore
	salt       string
	trustProxy bool
	// now подменяется в тесте, чтобы проверить сдвиг окна частоты, не ожидая час.
	now func() time.Time
}

// NewFeedbackHandler creates a new feedback handler.
func NewFeedbackHandler(repo feedbackStore, salt string, trustProxy bool) *FeedbackHandler {
	return &FeedbackHandler{repo: repo, salt: salt, trustProxy: trustProxy, now: time.Now}
}

// createFeedbackRequest is the public request body.
type createFeedbackRequest struct {
	Message string `json:"message"`
	// Contact здесь нет намеренно. Читальня не собирает персональных данных:
	// поля «как ответить» в форме больше нет, а присланный старым клиентом или
	// ботом `contact` json.Decoder молча отбросит — незнакомые ключи он
	// игнорирует. Письмо примут, контакт не сохранится.
	SourcePath string `json:"source_path"`
	// BindingRef — ловушка. Поле уведено с экрана стилями, человек его не
	// видит и не заполняет; бот, разбирающий форму, заполняет всё подряд.
	//
	// Имя нарочно вне словарей автозаполнения. Названное `website` поле
	// заполняют менеджеры паролей сами — и ловушка съедает письмо живого
	// человека, ответив ему «принято». Про переплёт в этом API ничего нет,
	// так что за настоящее поле его не примут и при чтении кода.
	BindingRef string `json:"binding_ref"`
}

// Create accepts a letter from a reader. Public: no auth.
func (h *FeedbackHandler) Create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, feedbackMaxBody)

	var req createFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "письмо слишком длинное")
			return
		}
		writeError(w, http.StatusBadRequest, "не удалось разобрать запрос")
		return
	}

	// Ловушка сработала: отвечаем ровно как при успехе и ничего не пишем.
	if strings.TrimSpace(req.BindingRef) != "" {
		writeJSONStatus(w, http.StatusCreated, map[string]bool{"ok": true})
		return
	}

	message, err := validateMessage(req.Message)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ipHash := hashIP(clientIP(r, h.trustProxy), h.salt)
	// Пустая отметка не должна отключать предел: раньше ветка `if ipHash != ""`
	// пропускала такое письмо вообще без проверки. Безадресные запросы идут в
	// одно общее ведро с тем же пределом — строже, чем «мимо кассы».
	if ipHash == "" {
		ipHash = feedbackNoAddressBucket
	}

	letter := &models.Feedback{
		Message:    message,
		SourcePath: normalizeSourcePath(req.SourcePath),
		IPHash:     ipHash,
	}

	accepted, err := h.repo.CreateWithinLimit(r.Context(), letter, feedbackRateLimit, h.now().Add(-feedbackRateWindow))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось сохранить письмо")
		return
	}
	if !accepted {
		writeError(w, http.StatusTooManyRequests,
			"вы уже отправили несколько писем за последний час, попробуйте позже")
		return
	}

	writeJSONStatus(w, http.StatusCreated, map[string]bool{"ok": true})
}

// List returns letters, newest first. Administrator only.
func (h *FeedbackHandler) List(w http.ResponseWriter, r *http.Request) {
	var handled *bool
	switch r.URL.Query().Get("handled") {
	case "true":
		yes := true
		handled = &yes
	case "false":
		no := false
		handled = &no
	}

	letters, err := h.repo.List(r.Context(), handled, feedbackListLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось загрузить обращения")
		return
	}

	// make, а не var: пустой срез обязан стать [], иначе фронт получит null.
	out := make([]*models.Feedback, 0, len(letters))
	out = append(out, letters...)
	writeJSON(w, out)
}

// UnreadCount returns how many letters are still unhandled. Administrator only.
//
// Отдельный маршрут, а не поле в списке: счётчик висит в шапке на каждой
// странице у администратора, и тянуть ради него все письма — то же
// расточительство, из-за которого рядом с /pages появился page-map.
func (h *FeedbackHandler) UnreadCount(w http.ResponseWriter, r *http.Request) {
	count, err := h.repo.CountUnread(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось сосчитать обращения")
		return
	}

	writeJSON(w, map[string]int{"count": count})
}

// SetHandled marks a letter handled or returns it among the new ones.
func (h *FeedbackHandler) SetHandled(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "неверный идентификатор обращения")
		return
	}

	var req struct {
		Handled bool `json:"handled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "не удалось разобрать запрос")
		return
	}

	if err := h.repo.SetHandled(r.Context(), id, req.Handled); err != nil {
		writeError(w, http.StatusNotFound, "обращение не найдено")
		return
	}

	writeJSON(w, map[string]bool{"ok": true})
}

// Delete removes a letter for good.
func (h *FeedbackHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "неверный идентификатор обращения")
		return
	}

	if err := h.repo.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "обращение не найдено")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
