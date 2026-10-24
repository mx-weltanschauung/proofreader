package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/internal/repository"
)

// Пределы выдачи журнала закрытий: без limit — defaultClosuresLimit, больше
// maxClosuresLimit не отдаётся.
const (
	defaultClosuresLimit = 100
	maxClosuresLimit     = 500
)

// WithChapters подключает главы: проверка «глава этой работы» и план закрытия.
func (h *ClosedHandler) WithChapters(src ClosedChapterSource) *ClosedHandler {
	h.chapters = src
	return h
}

// WithLog подключает чтение журнала закрытий.
func (h *ClosedHandler) WithLog(log ClosureLog) *ClosedHandler {
	h.log = log
	return h
}

type latestClosureResponse struct {
	Closed bool   `json:"closed"`
	At     string `json:"at"`
	// Reason виден только администратору: письмо, от кого, о чём — не для
	// редактора. Редактору — пустая строка.
	Reason string `json:"reason"`
}

// Latest — последняя запись журнала по сущности: кто-нибудь из сотрудников
// спрашивает, когда и закрыто ли. Записей нет — 404.
func (h *ClosedHandler) Latest(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	switch kind {
	case repository.ClosureWork, repository.ClosureEdition, repository.ClosureJournal, repository.ClosureChapter:
	default:
		writeError(w, http.StatusBadRequest, "Неверный вид")
		return
	}
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "Неверный номер")
		return
	}
	if h.log == nil {
		writeError(w, http.StatusInternalServerError, "Журнал не подключён")
		return
	}
	c, err := h.log.LatestClosure(r.Context(), kind, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать журнал")
		return
	}
	if c == nil {
		writeError(w, http.StatusNotFound, "Не найдено")
		return
	}
	resp := latestClosureResponse{Closed: c.Closed, At: c.At.Format(time.RFC3339)}
	if claims, ok := middleware.GetUserFromContext(r.Context()); ok && claims.Role == models.RoleAdministrator {
		resp.Reason = c.Reason
	}
	writeJSONStatus(w, http.StatusOK, resp)
}

// List — журнал закрытий целиком, новые сверху (администратор).
func (h *ClosedHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, offset := defaultClosuresLimit, 0
	q := r.URL.Query()
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "Неверный limit")
			return
		}
		limit = min(n, maxClosuresLimit)
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "Неверный offset")
			return
		}
		offset = n
	}
	if h.log == nil {
		writeError(w, http.StatusInternalServerError, "Журнал не подключён")
		return
	}
	rows, err := h.log.ListClosures(r.Context(), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать журнал")
		return
	}
	if rows == nil {
		rows = []models.Closure{}
	}
	writeJSONStatus(w, http.StatusOK, rows)
}

type sharedPage struct {
	PageNumber int      `json:"page_number"`
	Neighbors  []string `json:"neighbors"`
}

type closePlan struct {
	Chapters int          `json:"chapters"`
	Pages    int          `json:"pages"`
	Shared   []sharedPage `json:"shared"`
}

// Plan — что закроет закрытие главы: глав в поддереве, полос в диапазоне и
// общие полосы — те, что накрывает ещё и открытая глава вне поддерева, — с
// заглавиями этих соседей. Предки закрываемой главы соседями не считаются:
// они по устройству накрывают все её полосы, и перечень стал бы всей главой.
func (h *ClosedHandler) Plan(w http.ResponseWriter, r *http.Request) {
	workID, err1 := strconv.ParseInt(mux.Vars(r)["workId"], 10, 64)
	id, err2 := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err1 != nil || err2 != nil || workID <= 0 || id <= 0 {
		writeError(w, http.StatusBadRequest, "Неверный номер")
		return
	}
	if h.chapters == nil {
		writeError(w, http.StatusInternalServerError, "Главы не подключены")
		return
	}
	ch, err := h.chapters.GetByID(r.Context(), id)
	if err != nil || ch == nil || ch.WorkID != workID {
		writeError(w, http.StatusNotFound, "Не найдено")
		return
	}
	tree, err := h.chapters.ListByWorkHierarchical(r.Context(), workID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать главы")
		return
	}
	writeJSONStatus(w, http.StatusOK, buildClosePlan(tree, id))
}

func buildClosePlan(tree []*models.Chapter, targetID int64) closePlan {
	var target *models.Chapter
	parents := map[int64]*models.Chapter{}
	var all []*models.Chapter
	var walk func(list []*models.Chapter, parent *models.Chapter)
	walk = func(list []*models.Chapter, parent *models.Chapter) {
		for _, c := range list {
			all = append(all, c)
			if parent != nil {
				parents[c.ID] = parent
			}
			if c.ID == targetID {
				target = c
			}
			walk(c.Children, c)
		}
	}
	walk(tree, nil)

	plan := closePlan{Shared: []sharedPage{}}
	if target == nil {
		return plan
	}
	// skip — поддерево закрываемой главы и её предки.
	skip := map[int64]bool{}
	var mark func(c *models.Chapter)
	mark = func(c *models.Chapter) {
		skip[c.ID] = true
		for _, k := range c.Children {
			mark(k)
		}
	}
	mark(target)
	plan.Chapters = len(skip)
	for p := parents[target.ID]; p != nil; p = parents[p.ID] {
		skip[p.ID] = true
	}
	if target.EndPage >= target.StartPage {
		plan.Pages = target.EndPage - target.StartPage + 1
	}
	for page := target.StartPage; page <= target.EndPage; page++ {
		var names []string
		for _, c := range all {
			if !skip[c.ID] && c.StartPage <= page && page <= c.EndPage {
				names = append(names, c.Title)
			}
		}
		if len(names) > 0 {
			plan.Shared = append(plan.Shared, sharedPage{PageNumber: page, Neighbors: names})
		}
	}
	return plan
}
