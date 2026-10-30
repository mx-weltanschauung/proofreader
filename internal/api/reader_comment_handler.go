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

type CommentStore interface {
	Save(context.Context, models.CommentViewer, int64, models.MarginNoteInput, string, int64, int) (*models.ReaderComment, error)
	Get(context.Context, models.CommentViewer, int64) (*models.ReaderComment, error)
	List(context.Context, models.CommentViewer, int64, bool, bool, bool, int, int) ([]*models.ReaderComment, error)
	History(context.Context, models.CommentViewer, int64) ([]models.CommentRevision, error)
	Review(context.Context, models.CommentViewer, int64, int, bool, string) error
	Remove(context.Context, models.CommentViewer, int64) error
	ToNote(context.Context, models.CommentViewer, int64) (*models.MarginNote, error)
	Ban(context.Context, models.CommentViewer, int64) error
	Unban(context.Context, models.CommentViewer, int64) error
	PendingCount(context.Context, models.CommentViewer) (int, error)
	Banned(context.Context, models.CommentViewer) ([]models.BannedCommenter, error)
}

type ReaderCommentHandler struct {
	comments   CommentStore
	works      WorkStore
	pages      marginPageGetter
	chapters   marginChapterGetter
	secret     string
	trustProxy bool
}

func NewReaderCommentHandler(comments CommentStore, works WorkStore, pages marginPageGetter, chapters marginChapterGetter, secret string, trust bool) *ReaderCommentHandler {
	return &ReaderCommentHandler{comments, works, pages, chapters, secret, trust}
}
func commentViewer(r *http.Request) models.CommentViewer {
	c, ok := middleware.GetUserFromContext(r.Context())
	if !ok || c == nil {
		return models.CommentViewer{}
	}
	return models.CommentViewer{ID: c.UserID, Staff: isStaffClaims(c), Admin: c.Role == models.RoleAdministrator}
}
func commentID(r *http.Request, key string) int64 {
	id, _ := strconv.ParseInt(mux.Vars(r)[key], 10, 64)
	return id
}
func commentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrCommentNotFound), storageNotFound(err):
		marginNotFound(w)
	case errors.Is(err, repository.ErrCommentConflict):
		writeError(w, 409, "Комментарий изменился или уже рассмотрен. Обновите список")
	case errors.Is(err, repository.ErrCommentRate):
		writeError(w, 429, "Слишком много комментариев. Попробуйте позже")
	case errors.Is(err, repository.ErrCommentBanned):
		writeError(w, 403, "Редакция запретила вам комментировать")
	case errors.Is(err, repository.ErrMarginNoteLimit):
		writeError(w, 409, "Слишком много заметок: удалите старые")
	default:
		writeError(w, 500, "Не удалось выполнить действие с комментарием")
	}
}

type commentRequest struct {
	marginNoteRequest
	Revision int    `json:"revision"`
	NoteID   int64  `json:"note_id"`
	Reason   string `json:"reason"`
}

func decodeComment(w http.ResponseWriter, r *http.Request, req *commentRequest) bool {
	r.Body = http.MaxBytesReader(w, r.Body, marginMaxBody)
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			writeError(w, 413, "Комментарий слишком велик")
		} else {
			writeError(w, 400, "Некорректное тело запроса")
		}
		return false
	}
	return true
}
func commentBody(w http.ResponseWriter, raw string) (string, bool) {
	body := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(body); n < 1 || n > 2000 {
		writeError(w, 400, "Комментарий должен содержать от 1 до 2 000 знаков")
		return "", false
	}
	return body, true
}

// resolveTarget проверяет цель через те же обёртки чтения, что у заметок.
func (h *ReaderCommentHandler) resolveTarget(w http.ResponseWriter, r *http.Request, req commentRequest) (models.MarginNoteInput, bool) {
	in := models.MarginNoteInput{WorkID: commentID(r, "workId"), Target: req.Target}
	ctx := r.Context()
	if in.WorkID <= 0 {
		marginNotFound(w)
		return in, false
	}
	if _, err := h.works.GetByID(ctx, in.WorkID); err != nil {
		if storageNotFound(err) {
			marginNotFound(w)
		} else {
			writeError(w, 500, "Не удалось прочитать том")
		}
		return in, false
	}
	switch req.Target {
	case models.MarginTargetWork:
	case models.MarginTargetChapter:
		c, err := h.chapters.GetByID(ctx, req.ChapterID)
		if err != nil {
			commentError(w, err)
			return in, false
		}
		if c.WorkID != in.WorkID {
			marginNotFound(w)
			return in, false
		}
		in.ChapterID = &c.ID
		in.ChapterTitle = marginChapterTitle(c.Title)
	case models.MarginTargetQuote:
		if req.StartPage < 1 || req.EndPage < req.StartPage || strings.TrimSpace(req.QuoteKey) == "" {
			writeError(w, 400, "Не указано место цитаты")
			return in, false
		}
		for _, s := range []string{req.QuoteKey, req.QuoteTail, req.QuoteText} {
			if utf8.RuneCountInString(s) > 4000 {
				writeError(w, 400, "Цитата слишком длинная")
				return in, false
			}
		}
		first, err := h.pages.GetByWorkAndPageNumber(ctx, in.WorkID, req.StartPage)
		if err != nil {
			commentError(w, err)
			return in, false
		}
		last := first
		if req.EndPage != req.StartPage {
			last, err = h.pages.GetByWorkAndPageNumber(ctx, in.WorkID, req.EndPage)
			if err != nil {
				commentError(w, err)
				return in, false
			}
		}
		in.StartPageID = &first.ID
		in.EndPageID = &last.ID
		in.StartPageNumber = &first.PageNumber
		in.EndPageNumber = &last.PageNumber
		in.QuoteKey = req.QuoteKey
		in.QuoteTail = req.QuoteTail
		in.QuoteText = req.QuoteText
	default:
		writeError(w, 400, "Неизвестный вид комментария")
		return in, false
	}
	return in, true
}
func (h *ReaderCommentHandler) Create(w http.ResponseWriter, r *http.Request) { h.save(w, r, false) }
func (h *ReaderCommentHandler) Update(w http.ResponseWriter, r *http.Request) { h.save(w, r, true) }
func (h *ReaderCommentHandler) save(w http.ResponseWriter, r *http.Request, update bool) {
	v := commentViewer(r)
	if v.ID == 0 {
		writeError(w, 401, "Требуется вход")
		return
	}
	var req commentRequest
	if !decodeComment(w, r, &req) {
		return
	}
	body, ok := commentBody(w, req.Body)
	if !ok {
		return
	}
	var in models.MarginNoteInput
	var id int64
	if update {
		id = commentID(r, "id")
		if id <= 0 || req.Revision < 1 {
			writeError(w, 400, "Не указана редакция комментария")
			return
		}
		req.NoteID = 0
	} else if req.NoteID == 0 {
		in, ok = h.resolveTarget(w, r, req)
		if !ok {
			return
		}
	}
	in.OwnerID = v.ID
	in.Body = body
	c, err := h.comments.Save(context.WithoutCancel(r.Context()), v, id, in, hashIP(clientIP(r, h.trustProxy), h.secret), req.NoteID, req.Revision)
	if err != nil {
		commentError(w, err)
		return
	}
	if update {
		writeJSON(w, c)
	} else {
		writeJSONStatus(w, 201, c)
	}
}
func commentPagination(r *http.Request) (int, int) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit < 1 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
func (h *ReaderCommentHandler) ListByWork(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, false, false)
}
func (h *ReaderCommentHandler) Mine(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, true, false)
}
func (h *ReaderCommentHandler) Queue(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, false, true)
}
func (h *ReaderCommentHandler) list(w http.ResponseWriter, r *http.Request, mine, queue bool) {
	v := commentViewer(r)
	if mine && v.ID == 0 {
		writeError(w, 401, "Требуется вход")
		return
	}
	if queue && !v.Staff {
		writeError(w, 403, "Требуется сотрудник")
		return
	}
	limit, offset := commentPagination(r)
	wid := commentID(r, "workId")
	if !mine && !queue && wid <= 0 {
		marginNotFound(w)
		return
	}
	list, err := h.comments.List(r.Context(), v, wid, r.URL.Query().Get("public") == "true", mine, queue, limit, offset)
	if err != nil {
		commentError(w, err)
		return
	}
	writeJSON(w, list)
}
func (h *ReaderCommentHandler) History(w http.ResponseWriter, r *http.Request) {
	out, err := h.comments.History(r.Context(), commentViewer(r), commentID(r, "id"))
	if err != nil {
		commentError(w, err)
		return
	}
	writeJSON(w, out)
}
func (h *ReaderCommentHandler) Approve(w http.ResponseWriter, r *http.Request) { h.review(w, r, true) }
func (h *ReaderCommentHandler) Reject(w http.ResponseWriter, r *http.Request)  { h.review(w, r, false) }
func (h *ReaderCommentHandler) review(w http.ResponseWriter, r *http.Request, approve bool) {
	var req commentRequest
	if !decodeComment(w, r, &req) {
		return
	}
	if req.Revision < 1 || utf8.RuneCountInString(req.Reason) > 1000 {
		writeError(w, 400, "Некорректная редакция или причина отказа")
		return
	}
	if err := h.comments.Review(r.Context(), commentViewer(r), commentID(r, "id"), req.Revision, approve, strings.TrimSpace(req.Reason)); err != nil {
		commentError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *ReaderCommentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.comments.Remove(r.Context(), commentViewer(r), commentID(r, "id")); err != nil {
		commentError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *ReaderCommentHandler) ToNote(w http.ResponseWriter, r *http.Request) {
	note, err := h.comments.ToNote(r.Context(), commentViewer(r), commentID(r, "id"))
	if err != nil {
		commentError(w, err)
		return
	}
	writeJSON(w, note)
}

func (h *ReaderCommentHandler) Ban(w http.ResponseWriter, r *http.Request) {
	if err := h.comments.Ban(r.Context(), commentViewer(r), commentID(r, "id")); err != nil {
		commentError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *ReaderCommentHandler) Unban(w http.ResponseWriter, r *http.Request) {
	if err := h.comments.Unban(r.Context(), commentViewer(r), commentID(r, "id")); err != nil {
		commentError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *ReaderCommentHandler) PendingCount(w http.ResponseWriter, r *http.Request) {
	n, err := h.comments.PendingCount(r.Context(), commentViewer(r))
	if err != nil {
		commentError(w, err)
		return
	}
	writeJSON(w, map[string]int{"count": n})
}
func (h *ReaderCommentHandler) Banned(w http.ResponseWriter, r *http.Request) {
	list, err := h.comments.Banned(r.Context(), commentViewer(r))
	if err != nil {
		commentError(w, err)
		return
	}
	writeJSON(w, list)
}
