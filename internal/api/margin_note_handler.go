package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gorilla/mux"

	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/internal/repository"
)

// marginPageGetter и marginChapterGetter — то, что обработчик заметок берёт у
// полос и глав. В проводку идут ОБЁРТКИ чтения (viewPages, viewChapters):
// закрытая полоса или глава не-сотруднику отвечает «not found», и до Create
// закрытое не доходит — выборка после вставки в репозитории закрытости не
// фильтрует.
type marginPageGetter interface {
	GetByWorkAndPageNumber(ctx context.Context, workID int64, pageNumber int) (*models.Page, error)
}

type marginChapterGetter interface {
	GetByID(ctx context.Context, id int64) (*models.Chapter, error)
}

// MarginNoteHandler — личные заметки читателя на полях: цитата, глава, том.
type MarginNoteHandler struct {
	notes    MarginNoteStore
	works    WorkStore
	pages    marginPageGetter
	chapters marginChapterGetter
}

func NewMarginNoteHandler(notes MarginNoteStore, works WorkStore, pages marginPageGetter, chapters marginChapterGetter) *MarginNoteHandler {
	return &MarginNoteHandler{notes: notes, works: works, pages: pages, chapters: chapters}
}

const (
	marginListDefaultLimit = 50
	marginListMaxLimit     = 200
)

// marginMaxBody — потолок тела POST и PUT. Считан от пределов полей, а не
// взят круглым: самая длинная законная заметка — 10 000 знаков текста и три
// поля цитаты по 4 000, по четыре байта на знак в худшем случае (88 КБ);
// 64 КБ отрезали бы такую заметку раньше проверки длины. Запас — на имена
// полей JSON и экранирование.
const marginMaxBody = (models.MarginNoteBodyMaxRunes+3*models.MarginNoteQuoteMaxRunes)*4 + 16<<10

// marginDecode читает тело запроса под потолком; ok=false — ответ записан.
// Сверх потолка — 413, как у предложений правки и обратной связи.
func marginDecode(w http.ResponseWriter, r *http.Request, req *marginNoteRequest) bool {
	r.Body = http.MaxBytesReader(w, r.Body, marginMaxBody)
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "Заметка слишком велика")
			return false
		}
		writeError(w, http.StatusBadRequest, "Некорректное тело запроса")
		return false
	}
	return true
}

type marginNoteRequest struct {
	Target    string `json:"target"`
	ChapterID int64  `json:"chapter_id"`
	StartPage int    `json:"start_page"`
	EndPage   int    `json:"end_page"`
	QuoteKey  string `json:"quote_key"`
	QuoteTail string `json:"quote_tail"`
	QuoteText string `json:"quote_text"`
	Body      string `json:"body"`
}

// marginBody проверяет текст заметки; ok=false — ответ уже записан.
func marginBody(w http.ResponseWriter, raw string) (string, bool) {
	body := strings.TrimSpace(raw)
	if body == "" {
		writeError(w, http.StatusBadRequest, "Заметка пуста")
		return "", false
	}
	if utf8.RuneCountInString(body) > models.MarginNoteBodyMaxRunes {
		writeError(w, http.StatusBadRequest, "Заметка длиннее 10 000 знаков")
		return "", false
	}
	return body, true
}

func marginClaims(w http.ResponseWriter, r *http.Request) (int64, bool) {
	claims, ok := middleware.GetUserFromContext(r.Context())
	if !ok || claims == nil {
		writeError(w, http.StatusUnauthorized, "Требуется вход")
		return 0, false
	}
	return claims.UserID, true
}

func marginNotFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, "Не найдено")
}

// Create — POST /works/{workId}/margin-notes.
func (h *MarginNoteHandler) Create(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := marginClaims(w, r)
	if !ok {
		return
	}
	workID, err := strconv.ParseInt(mux.Vars(r)["workId"], 10, 64)
	if err != nil {
		marginNotFound(w)
		return
	}
	var req marginNoteRequest
	if !marginDecode(w, r, &req) {
		return
	}

	body, ok := marginBody(w, req.Body)
	if !ok {
		return
	}
	switch req.Target {
	case models.MarginTargetWork, models.MarginTargetChapter, models.MarginTargetQuote:
	default:
		writeError(w, http.StatusBadRequest, "Неизвестный вид заметки")
		return
	}
	if req.Target == models.MarginTargetQuote {
		if req.EndPage < req.StartPage || strings.TrimSpace(req.QuoteKey) == "" {
			writeError(w, http.StatusBadRequest, "Не указано место цитаты")
			return
		}
		for _, f := range []string{req.QuoteKey, req.QuoteTail, req.QuoteText} {
			if utf8.RuneCountInString(f) > models.MarginNoteQuoteMaxRunes {
				writeError(w, http.StatusBadRequest, "Цитата слишком длинная")
				return
			}
		}
	}

	// Контекст без отмены клиента: claims нужны обёрткам чтения, а запись
	// не должна обрываться с уходом читателя.
	ctx := context.WithoutCancel(r.Context())

	// Несуществующий том — 404, а не ошибка внешнего ключа на вставке.
	if _, err := h.works.GetByID(ctx, workID); err != nil {
		if storageNotFound(err) {
			marginNotFound(w)
		} else {
			writeError(w, http.StatusInternalServerError, "Не удалось прочитать том")
		}
		return
	}

	in := models.MarginNoteInput{OwnerID: ownerID, WorkID: workID, Target: req.Target, Body: body}
	switch req.Target {
	case models.MarginTargetChapter:
		chapter, err := h.chapters.GetByID(ctx, req.ChapterID)
		if err != nil {
			if storageNotFound(err) {
				marginNotFound(w)
			} else {
				writeError(w, http.StatusInternalServerError, "Не удалось прочитать главу")
			}
			return
		}
		if chapter.WorkID != workID {
			marginNotFound(w)
			return
		}
		in.ChapterID = &chapter.ID
		in.ChapterTitle = marginChapterTitle(chapter.Title)
	case models.MarginTargetQuote:
		start, err := h.pages.GetByWorkAndPageNumber(ctx, workID, req.StartPage)
		if err != nil {
			h.pageError(w, err)
			return
		}
		end := start
		if req.EndPage != req.StartPage {
			if end, err = h.pages.GetByWorkAndPageNumber(ctx, workID, req.EndPage); err != nil {
				h.pageError(w, err)
				return
			}
		}
		startNum, endNum := start.PageNumber, end.PageNumber
		in.StartPageID, in.EndPageID = &start.ID, &end.ID
		in.StartPageNumber, in.EndPageNumber = &startNum, &endNum
		in.QuoteKey, in.QuoteTail, in.QuoteText = req.QuoteKey, req.QuoteTail, req.QuoteText
	}

	note, err := h.notes.Create(ctx, in)
	if err != nil {
		if errors.Is(err, repository.ErrMarginNoteLimit) {
			writeError(w, http.StatusConflict, "Слишком много заметок: удалите старые")
			return
		}
		writeError(w, http.StatusInternalServerError, "Не удалось сохранить заметку")
		return
	}
	writeJSONStatus(w, http.StatusCreated, note)
}

// marginUntitledChapter — снимок заглавия главы, у которой его нет: таблица
// требует непустой снимок (по нему заметка узнаётся, когда главу снесут), а
// заглавие главы бывает пустым.
const marginUntitledChapter = "Глава без заглавия"

func marginChapterTitle(title string) string {
	if strings.TrimSpace(title) == "" {
		return marginUntitledChapter
	}
	return title
}

func (h *MarginNoteHandler) pageError(w http.ResponseWriter, err error) {
	if storageNotFound(err) {
		marginNotFound(w)
		return
	}
	writeError(w, http.StatusInternalServerError, "Не удалось прочитать полосу")
}

// ListByWork — GET /works/{workId}/margin-notes: свои заметки тома.
func (h *MarginNoteHandler) ListByWork(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := marginClaims(w, r)
	if !ok {
		return
	}
	workID, err := strconv.ParseInt(mux.Vars(r)["workId"], 10, 64)
	if err != nil {
		marginNotFound(w)
		return
	}
	items, err := h.notes.ListByWork(r.Context(), ownerID, workID, staffContext(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать заметки")
		return
	}
	if items == nil {
		items = []*models.MarginNote{}
	}
	writeJSONStatus(w, http.StatusOK, items)
}

// List — GET /margin-notes?work_id=&limit=&offset=: «Мои заметки».
func (h *MarginNoteHandler) List(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := marginClaims(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	var workID *int64
	if s := q.Get("work_id"); s != "" {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Некорректный work_id")
			return
		}
		workID = &id
	}
	limit := marginListDefaultLimit
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 {
		limit = min(n, marginListMaxLimit)
	}
	offset := 0
	if n, err := strconv.Atoi(q.Get("offset")); err == nil && n > 0 {
		offset = n
	}
	items, total, err := h.notes.ListByOwner(r.Context(), ownerID, workID, limit, offset, staffContext(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать заметки")
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

// Update — PUT /margin-notes/{id}: меняется только текст.
func (h *MarginNoteHandler) Update(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := marginClaims(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		marginNotFound(w)
		return
	}
	var req marginNoteRequest
	if !marginDecode(w, r, &req) {
		return
	}
	body, ok := marginBody(w, req.Body)
	if !ok {
		return
	}
	note, err := h.notes.UpdateBody(r.Context(), ownerID, id, body, staffContext(r.Context()))
	if err != nil {
		if storageNotFound(err) {
			marginNotFound(w)
		} else {
			writeError(w, http.StatusInternalServerError, "Не удалось сохранить заметку")
		}
		return
	}
	writeJSONStatus(w, http.StatusOK, note)
}

// Delete — DELETE /margin-notes/{id}.
func (h *MarginNoteHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := marginClaims(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		marginNotFound(w)
		return
	}
	if err := h.notes.Delete(r.Context(), ownerID, id, staffContext(r.Context())); err != nil {
		if storageNotFound(err) {
			marginNotFound(w)
		} else {
			writeError(w, http.StatusInternalServerError, "Не удалось удалить заметку")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
