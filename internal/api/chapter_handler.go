package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/pagecache"
	"proofreader/pkg/markdown"
)

// ChapterHandler handles chapter endpoints
type ChapterHandler struct {
	chapterRepo ChapterStore
	pageRepo    PageStore
	renderer    *markdown.Renderer
	cache       *RangeCache
	recordings  RecordingCounter
	credits     CreditLister
}

// RecordingCounter — сколько записей человека у главы и её подглав.
type RecordingCounter interface {
	CountInChapterSubtree(ctx context.Context, chapterID int64) (int, error)
}

// WithRecordings включает отказ удалять главу с прикреплённой записью.
func (h *ChapterHandler) WithRecordings(c RecordingCounter) *ChapterHandler {
	h.recordings = c
	return h
}

// WithCredits подключает подписи статей журнала к ответам глав.
func (h *ChapterHandler) WithCredits(c CreditLister) *ChapterHandler {
	h.credits = c
	return h
}

// attachCredits раскладывает подписи по статьям дерева. Запрос в базу —
// только если в дереве есть статья: у томов собраний его нет вовсе.
func (h *ChapterHandler) attachCredits(ctx context.Context, workID int64, tree []*models.Chapter) {
	if h.credits == nil || !hasArticle(tree) {
		return
	}
	m, err := h.credits.ListCreditsByWork(ctx, workID)
	if err != nil {
		log.Printf("credits for work %d: %v", workID, err)
		return
	}
	var walk func([]*models.Chapter)
	walk = func(cs []*models.Chapter) {
		for _, c := range cs {
			c.Credits = m[c.ID]
			walk(c.Children)
		}
	}
	walk(tree)
}

func hasArticle(cs []*models.Chapter) bool {
	for _, c := range cs {
		if c.ArticleKind != nil || hasArticle(c.Children) {
			return true
		}
	}
	return false
}

// NewChapterHandler creates a new chapter handler
func NewChapterHandler(
	chapterRepo ChapterStore,
	pageRepo PageStore,
	renderer *markdown.Renderer,
	cache *RangeCache,
) *ChapterHandler {
	return &ChapterHandler{
		chapterRepo: chapterRepo,
		pageRepo:    pageRepo,
		renderer:    renderer,
		cache:       cache,
	}
}

// CreateChapterRequest represents a request to create a chapter
type CreateChapterRequest struct {
	ParentID    *int64 `json:"parent_id,omitempty"`
	Title       string `json:"title"`
	Type        string `json:"type"`
	OrderNumber int    `json:"order_number"`
	StartPage   int    `json:"start_page"`
	EndPage     int    `json:"end_page"`
	// IsApparatus — ручная поправка классификатора: nil означает «решай сам»
	// (по заголовку и родителю), непустое значение побеждает. Через него и
	// правится спорный случай вроде «Указаний читателю».
	IsApparatus *bool `json:"is_apparatus,omitempty"`
	// ArticleKind — вид статьи журнала: nil — не трогать, "" — снять.
	ArticleKind *string `json:"article_kind,omitempty"`
}

// MoveChapterRequest represents a request to move/reorder a chapter
type MoveChapterRequest struct {
	ParentID    *int64 `json:"parent_id"`
	OrderNumber int    `json:"order_number"`
}

// List retrieves all chapters for a work as a hierarchical tree
func (h *ChapterHandler) List(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workID, err := strconv.ParseInt(vars["workId"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid work ID", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	chapters, err := h.chapterRepo.ListByWorkHierarchical(ctx, workID)
	if err != nil {
		http.Error(w, "Failed to retrieve chapters", http.StatusInternalServerError)
		return
	}

	h.attachCredits(ctx, workID, chapters)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chapters)
}

// chapterTitleMaxRunes — сколько знаков вмещает `chapters.title`
// (VARCHAR(500), 000001_initial_schema.up.sql). Счёт РУНАМИ, а не байтами:
// Postgres считает символы, а кириллическая буква в UTF-8 занимает два байта,
// и проверка по len() отвергала бы вдвое более короткий заголовок.
//
// Без этой проверки слишком длинный заголовок доезжал до Postgres, тот отвечал
// «value too long for type character varying(500)», а обработчик переводил
// любую ошибку репозитория в 500 «Failed to create chapter» простым текстом:
// клиент видел «ошибка сервера» и не узнавал ни причины, ни того, что чинить
// её ему. Так это и вскрылось на томе 3 ПСС Ленина, где разбор содержания
// приклеивал к записям аналитические сводки и четыре заголовка из 123
// вырастали за предел (самый длинный — 839 знаков).
const chapterTitleMaxRunes = 500

// chapterTitleProblem — объяснение, если заголовок не лезет в колонку.
func chapterTitleProblem(title string) string {
	if n := utf8.RuneCountInString(title); n > chapterTitleMaxRunes {
		return fmt.Sprintf("заголовок главы длиннее %d знаков (%d): "+
			"столько вмещает колонка chapters.title", chapterTitleMaxRunes, n)
	}
	return ""
}

// Create creates a new chapter
func (h *ChapterHandler) Create(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workID, err := strconv.ParseInt(vars["workId"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid work ID", http.StatusBadRequest)
		return
	}

	var req CreateChapterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if problem := chapterTitleProblem(req.Title); problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}

	chapter := &models.Chapter{
		WorkID:      workID,
		ParentID:    req.ParentID,
		Title:       req.Title,
		Type:        req.Type,
		OrderNumber: req.OrderNumber,
		StartPage:   req.StartPage,
		EndPage:     req.EndPage,
	}
	if req.IsApparatus != nil {
		chapter.IsApparatus = *req.IsApparatus
	}
	if req.ArticleKind != nil {
		if *req.ArticleKind == "" {
			chapter.ArticleKind = nil
		} else if !models.ValidArticleKind(*req.ArticleKind) {
			writeError(w, http.StatusBadRequest, "Неизвестный вид статьи")
			return
		} else {
			k := *req.ArticleKind
			chapter.ArticleKind = &k
		}
	}

	ctx := context.Background()
	if err := h.chapterRepo.Create(ctx, chapter); err != nil {
		http.Error(w, "Failed to create chapter", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(chapter)
}

// Get retrieves a chapter by ID
func (h *ChapterHandler) Get(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	chapterID, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid chapter ID", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	chapter, err := h.chapterRepo.GetByID(ctx, chapterID)
	if err != nil {
		http.Error(w, "Chapter not found", http.StatusNotFound)
		return
	}
	h.attachCredits(ctx, chapter.WorkID, []*models.Chapter{chapter})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chapter)
}

// Update updates a chapter
func (h *ChapterHandler) Update(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	chapterID, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid chapter ID", http.StatusBadRequest)
		return
	}

	var req CreateChapterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if problem := chapterTitleProblem(req.Title); problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}

	ctx := context.Background()
	chapter, err := h.chapterRepo.GetByID(ctx, chapterID)
	if err != nil {
		http.Error(w, "Chapter not found", http.StatusNotFound)
		return
	}

	chapter.ParentID = req.ParentID
	chapter.Title = req.Title
	chapter.Type = req.Type
	chapter.OrderNumber = req.OrderNumber
	chapter.StartPage = req.StartPage
	chapter.EndPage = req.EndPage
	// Без явного значения признак аппарата остаётся тем, что лежит в базе:
	// правка заголовка не переклассифицирует главу и не затирает поправку,
	// сделанную человеком.
	if req.IsApparatus != nil {
		chapter.IsApparatus = *req.IsApparatus
	}
	if req.ArticleKind != nil {
		if *req.ArticleKind == "" {
			chapter.ArticleKind = nil
		} else if !models.ValidArticleKind(*req.ArticleKind) {
			writeError(w, http.StatusBadRequest, "Неизвестный вид статьи")
			return
		} else {
			k := *req.ArticleKind
			chapter.ArticleKind = &k
		}
	}

	if err := h.chapterRepo.Update(ctx, chapter); err != nil {
		http.Error(w, "Failed to update chapter", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chapter)
}

// Move moves a chapter to a new position (changes parent and/or order)
func (h *ChapterHandler) Move(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workID, err := strconv.ParseInt(vars["workId"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid work ID", http.StatusBadRequest)
		return
	}

	chapterID, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid chapter ID", http.StatusBadRequest)
		return
	}

	var req MoveChapterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx := context.Background()

	chapter, err := h.chapterRepo.GetByID(ctx, chapterID)
	if err != nil {
		http.Error(w, "Chapter not found", http.StatusNotFound)
		return
	}

	if chapter.WorkID != workID {
		http.Error(w, "Chapter does not belong to this work", http.StatusBadRequest)
		return
	}

	if err := h.chapterRepo.Move(ctx, chapterID, req.ParentID, req.OrderNumber); err != nil {
		http.Error(w, "Failed to move chapter", http.StatusInternalServerError)
		return
	}

	updatedChapter, err := h.chapterRepo.GetByID(ctx, chapterID)
	if err != nil {
		http.Error(w, "Failed to get updated chapter", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updatedChapter)
}

// Delete deletes a chapter
func (h *ChapterHandler) Delete(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	chapterID, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid chapter ID", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	// Каскад унёс бы строки записей человека, а файлы в аудиобакете остались
	// бы сиротами молча. Публикатор тома главы на боевом не удаляет, так что
	// отказ задевает только ручное удаление.
	if h.recordings != nil {
		n, err := h.recordings.CountInChapterSubtree(ctx, chapterID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Не удалось проверить записи главы")
			return
		}
		if n > 0 {
			writeError(w, http.StatusConflict, fmt.Sprintf(
				"У главы или её подглав %d запис(ей) человека — сначала сними их, потом удаляй главу", n))
			return
		}
	}
	if err := h.chapterRepo.Delete(ctx, chapterID); err != nil {
		http.Error(w, "Failed to delete chapter", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ChapterPage — одна полоса главы: вёрстка и один бит «рисовать ли маркер
// номера». Ни markdown-источника, ни id, ни статуса, ни пути превью: полная
// модель страницы везла бы рядом с вёрсткой тот же текст ещё раз и удваивала
// сырой ответ (на главе в 222 полосы — 1.9 МБ вместо 0.97).
type ChapterPage struct {
	PageNumber int    `json:"page_number"`
	HTML       string `json:"html"`
	Blank      bool   `json:"blank"`
}

// ChapterPagesResponse represents the response for chapter pages with aggregated footnotes
type ChapterPagesResponse struct {
	Pages         []ChapterPage `json:"pages"`
	FootnotesHTML string        `json:"footnotes_html"`
}

// chapterPagesFromRange собирает слим-полосы из отрендеренного диапазона.
// Общая для глав и элементов подборок: их ответы читает один и тот же
// ReadingSurface, и форма провода обязана оставаться одной.
func chapterPagesFromRange(pages []*models.Page, pageHTML []string) []ChapterPage {
	result := make([]ChapterPage, len(pages))
	for i, page := range pages {
		result[i] = ChapterPage{
			PageNumber: page.PageNumber,
			HTML:       pageHTML[i],
			// Бит пустой полосы считается здесь: markdown фронту больше не
			// возится, а маркер номера пустой полосе рисовать нельзя — её
			// секция схлопывается, и маркер сел бы на номер соседа.
			Blank: strings.TrimSpace(page.ContentMarkdown) == "",
		}
	}
	return result
}

// ListPages returns all pages in a chapter's page range with rendered HTML
func (h *ChapterHandler) ListPages(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	chapterID, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid chapter ID", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	chapter, err := h.chapterRepo.GetByID(ctx, chapterID)
	if err != nil {
		http.Error(w, "Chapter not found", http.StatusNotFound)
		return
	}

	key := pagecache.Key{WorkID: chapter.WorkID, Start: chapter.StartPage, End: chapter.EndPage}
	h.cache.Serve(w, r, key, func() (any, error) {
		pages, err := h.pageRepo.GetPageRange(ctx, chapter.WorkID, chapter.StartPage, chapter.EndPage)
		if err != nil {
			return nil, err
		}
		pageHTML, notes := renderPageRange(h.renderer, pages)
		return ChapterPagesResponse{
			Pages:         chapterPagesFromRange(pages, pageHTML),
			FootnotesHTML: markdown.RenderNotes(notes),
		}, nil
	})
}
