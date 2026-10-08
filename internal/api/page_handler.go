package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/pkg/markdown"
	"proofreader/pkg/storage"
)

// PageHandler handles page endpoints
type PageHandler struct {
	pageRepo        PageStore
	pageVersionRepo PageVersionStore
	fragments       FragmentStore
	cuts            DocumentCutStore
	renderer        *markdown.Renderer
	store           storage.Storage
	presignTTL      time.Duration
}

// NewPageHandler creates a new page handler
func NewPageHandler(
	pageRepo PageStore,
	pageVersionRepo PageVersionStore,
	fragments FragmentStore,
	cuts DocumentCutStore,
	renderer *markdown.Renderer,
	store storage.Storage,
	presignTTL time.Duration,
) *PageHandler {
	return &PageHandler{
		pageRepo:        pageRepo,
		pageVersionRepo: pageVersionRepo,
		fragments:       fragments,
		cuts:            cuts,
		renderer:        renderer,
		store:           store,
		presignTTL:      presignTTL,
	}
}

// CreatePageRequest represents a request to create a page
type CreatePageRequest struct {
	PageNumber      int               `json:"page_number"`
	ContentMarkdown string            `json:"content_markdown"`
	Status          models.PageStatus `json:"status"`
}

// UpdatePageRequest represents a request to update a page
type UpdatePageRequest struct {
	ContentMarkdown string            `json:"content_markdown"`
	Status          models.PageStatus `json:"status"`
	Comment         string            `json:"comment,omitempty"`
}

// List retrieves all pages for a work
func (h *PageHandler) List(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workID, err := strconv.ParseInt(vars["workId"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid work ID", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	pages, err := h.pageRepo.ListByWork(ctx, workID)
	if err != nil {
		http.Error(w, "Failed to retrieve pages", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(newPageResponses(r.Context(), pages, h.store, h.presignTTL))
}

// PageMap returns the number and status of every page in the work.
//
// Отдельная ручка, а не поле в List: карточке тома нужны 840 пар
// «номер — статус», а List отдаёт полный markdown каждой страницы и
// подписанный URL превью на каждую.
func (h *PageHandler) PageMap(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workID, err := strconv.ParseInt(vars["workId"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid work ID", http.StatusBadRequest)
		return
	}

	entries, err := h.pageRepo.ListPageMap(r.Context(), workID)
	if err != nil {
		http.Error(w, "Failed to retrieve page map", http.StatusInternalServerError)
		return
	}
	if entries == nil {
		entries = []models.PageMapEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(entries); err != nil {
		log.Printf("page map: encode: %v", err)
	}
}

// pageOfWork разбирает пару координат пути и отдаёт полосу, ПРИНАДЛЕЖАЩУЮ
// названной работе. Ответ уже написан, когда второе значение false.
//
// Сверка обязательна: без неё /works/142/pages/233 отдавал полосу работы 1 —
// номер работы в пути не читался вовсе. Тот же прецедент уже стоит в
// seo/canonical.go:69 и заведён там именно потому, что здесь его не было.
func pageOfWork(w http.ResponseWriter, r *http.Request, pages PageStore) (*models.Page, bool) {
	vars := mux.Vars(r)
	workID, err := strconv.ParseInt(vars["workId"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid work ID", http.StatusBadRequest)
		return nil, false
	}
	pageID, err := strconv.ParseInt(vars["pageId"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid page ID", http.StatusBadRequest)
		return nil, false
	}
	page, err := pages.GetByID(r.Context(), pageID)
	if err != nil || page == nil || page.WorkID != workID {
		http.Error(w, "Page not found", http.StatusNotFound)
		return nil, false
	}
	return page, true
}

// Get retrieves a page by ID
func (h *PageHandler) Get(w http.ResponseWriter, r *http.Request) {
	page, ok := pageOfWork(w, r, h.pageRepo)
	if !ok {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(newPageResponse(r.Context(), page, h.store, h.presignTTL))
}

// GetByNumber retrieves a page by its number within a work. Page numbers are
// unique per work (UNIQUE (work_id, page_number)), which is what lets the
// frontend address pages by the number a reader actually sees instead of by
// database ID. The response is identical to Get's, so the caller gets the ID
// it needs for render/update/versions in the same round trip.
func (h *PageHandler) GetByNumber(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workID, err := strconv.ParseInt(vars["workId"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid work ID", http.StatusBadRequest)
		return
	}

	pageNumber, err := strconv.Atoi(vars["pageNumber"])
	if err != nil {
		http.Error(w, "Invalid page number", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	page, err := h.pageRepo.GetByWorkAndPageNumber(ctx, workID, pageNumber)
	if err != nil {
		http.Error(w, "Page not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(newPageResponse(r.Context(), page, h.store, h.presignTTL))
}

// Update updates a page and creates a version
func (h *PageHandler) Update(w http.ResponseWriter, r *http.Request) {
	// Формат pageId проверяем до чтения тела: это дешёвая проверка, а
	// декодирование тела ниже уже само по себе отвечает 400 на пустое/
	// нечитаемое тело — оставлять его без этой ранней проверки незачем.
	if _, err := strconv.ParseInt(mux.Vars(r)["pageId"], 10, 64); err != nil {
		http.Error(w, "Invalid page ID", http.StatusBadRequest)
		return
	}

	var req UpdatePageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get user from context
	claims, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Сверка с работой из пути — тем же довесом, что и в Get: без неё PUT
	// /works/142/pages/7 правил бы полосу работы 1.
	page, ok := pageOfWork(w, r, h.pageRepo)
	if !ok {
		return
	}

	ctx := context.Background()
	if err := applyPageEdit(ctx, h.pageRepo, h.pageVersionRepo, h.fragments, h.cuts,
		page, req.ContentMarkdown, req.Status, claims.UserID, req.Comment); err != nil {
		log.Printf("apply page edit %d: %v", page.ID, err)
		writeError(w, http.StatusInternalServerError, "Не удалось сохранить страницу")
		return
	}

	// page.TextEditedAt был прочитан ДО правки (pageOfWork выше) и applyPageEdit
	// его не трогает — отдать его как есть значило бы соврать датой прямо в
	// ответе на запрос, который эту дату только что изменил. Перечитываем
	// полосу: она уже сохранена, второй запрос ей не в тягость (тот же приём,
	// что и у остальных одиночных чтений полосы — GetByID, а не список).
	fresh, err := h.pageRepo.GetByID(ctx, page.ID)
	if err != nil {
		log.Printf("reload page %d after edit: %v", page.ID, err)
		writeError(w, http.StatusInternalServerError, "Не удалось сохранить страницу")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(fresh)
}

// Render renders a page as HTML
func (h *PageHandler) Render(w http.ResponseWriter, r *http.Request) {
	page, ok := pageOfWork(w, r, h.pageRepo)
	if !ok {
		return
	}

	// Со сносками в разметке главы, а не <ol> gomarkdown, — см.
	// RenderWithNotes. Полоса одна, область имён не нужна.
	html := h.renderer.RenderWithNotes("", page.PageNumber, page.ContentMarkdown)

	response := map[string]interface{}{
		"html": html,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ListVersions lists all versions of a page
func (h *PageHandler) ListVersions(w http.ResponseWriter, r *http.Request) {
	// Списку версий сама полоса не нужна — но без сверки с работой из пути он
	// отдавал бы историю правок чужой полосы по её голому ID.
	page, ok := pageOfWork(w, r, h.pageRepo)
	if !ok {
		return
	}

	ctx := context.Background()
	versions, err := h.pageVersionRepo.ListByPage(ctx, page.ID)
	if err != nil {
		http.Error(w, "Failed to retrieve versions", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(versions)
}

// GetVersion retrieves a specific version of a page
func (h *PageHandler) GetVersion(w http.ResponseWriter, r *http.Request) {
	versionID, err := strconv.ParseInt(mux.Vars(r)["versionId"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid version ID", http.StatusBadRequest)
		return
	}

	// Сама версия дальше адресуется versionID, но без этой сверки конкретная
	// версия отдавалась бы независимо от того, чья это полоса и чья работа.
	page, ok := pageOfWork(w, r, h.pageRepo)
	if !ok {
		return
	}

	ctx := context.Background()
	version, err := h.pageVersionRepo.GetByID(ctx, versionID)
	if err != nil {
		http.Error(w, "Version not found", http.StatusNotFound)
		return
	}

	// versionID сам по себе — самостоятельная координата пути, не связанная
	// с pageId: GetByID ищет по id версии и найдёт её независимо от того,
	// какой полосе она принадлежит. Без этой сверки /works/1/pages/7/versions/55
	// отдал бы версию полосы 999, если такая версия вообще существует.
	if version.PageID != page.ID {
		http.Error(w, "Version not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(version)
}

// RestoreVersion restores a page to a specific version
func (h *PageHandler) RestoreVersion(w http.ResponseWriter, r *http.Request) {
	// Формат pageId — раньше decode тела нет, но проверить его дёшево и
	// незачем откладывать до pageOfWork ниже.
	if _, err := strconv.ParseInt(mux.Vars(r)["pageId"], 10, 64); err != nil {
		http.Error(w, "Invalid page ID", http.StatusBadRequest)
		return
	}

	versionID, err := strconv.ParseInt(mux.Vars(r)["versionId"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid version ID", http.StatusBadRequest)
		return
	}

	// Get user from context
	claims, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	ctx := context.Background()

	// Get the version to restore
	version, err := h.pageVersionRepo.GetByID(ctx, versionID)
	if err != nil {
		http.Error(w, "Version not found", http.StatusNotFound)
		return
	}

	// Get the current page — сверенную с работой из пути: без этого возврат
	// версии, как и PUT, писал бы поверх полосы работы 1 по адресу работы 142.
	page, ok := pageOfWork(w, r, h.pageRepo)
	if !ok {
		return
	}

	// versionID — отдельная координата пути, не связанная с pageId: без этой
	// сверки восстанавливалась бы ЛЮБАЯ существующая версия, включая ту, что
	// принадлежит чужой полосе. Это путь на запись — applyPageEdit ниже
	// возьмёт version.ContentMarkdown и запишет его поверх текста ЭТОЙ
	// полосы, так что подмена версии здесь не чтение не туда, а порча
	// текста полосы 7 текстом полосы 999.
	if version.PageID != page.ID {
		http.Error(w, "Version not found", http.StatusNotFound)
		return
	}

	// Возврат к прежней версии — такая же правка текста полосы, как и PUT:
	// снимок текущего текста в page_versions, запись выбранного текста,
	// переякоривание вырезок. Своя копия этих трёх шагов здесь и стояла — и
	// была вторым местом, где живёт переякоривание, то есть ровно тем
	// дублированием, ради снятия которого applyPageEdit и вынесена.
	//
	// Статус полосы возврат не трогает: page.Status передаётся как есть.
	if err := applyPageEdit(ctx, h.pageRepo, h.pageVersionRepo, h.fragments, h.cuts,
		page, version.ContentMarkdown, page.Status, claims.UserID, "Before restore"); err != nil {
		log.Printf("restore page %d to version %d: %v", page.ID, versionID, err)
		http.Error(w, "Failed to restore page", http.StatusInternalServerError)
		return
	}

	// Тот же довесок, что и у Update: page прочитан ДО правки (pageOfWork
	// выше), а text_edited_at считается подзапросом в момент чтения из базы —
	// у структуры, пролежавшей в памяти через правку, оно остаётся прежним.
	// Отдать её значило бы соврать датой в ответе на запрос, который эту дату
	// только что изменил. При отказе перечитывания — 500, а не молча
	// устаревшее тело: клиент принял бы его за состояние после возврата.
	fresh, err := h.pageRepo.GetByID(ctx, page.ID)
	if err != nil {
		log.Printf("reload page %d after restore: %v", page.ID, err)
		http.Error(w, "Failed to restore page", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(fresh)
}
