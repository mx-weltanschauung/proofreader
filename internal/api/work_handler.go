package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/pkg/fileprocessor"
	"proofreader/pkg/markdown"
	"proofreader/pkg/storage"
)

// WorkHandler handles work endpoints
type WorkHandler struct {
	workRepo      WorkRepo
	pageRepo      PageStore
	renderer      *markdown.Renderer
	store         storage.Storage
	presignTTL    time.Duration
	fileProcessor *fileprocessor.Processor
	// cache — кэши отдачи, которые обязан унести снос тома. nil означает
	// «кэшей нет» и допустим: так собран сервер с пустым PAGE_CACHE_DIR.
	cache *ServingCache
	// apparatus — снятие аппарата тома (apparatus_handler.go).
	apparatus ApparatusStore
	// audioStore — аудиобакет; nil — звука нет, снос его не касается.
	audioStore storage.Storage
}

// WithAudioStore подключает аудиобакет: снос тома и снятие аппарата обязаны
// чистить и его — каскад Postgres про объекты не знает.
func (h *WorkHandler) WithAudioStore(s storage.Storage) *WorkHandler {
	h.audioStore = s
	return h
}

// NewWorkHandler creates a new work handler
func NewWorkHandler(
	workRepo WorkRepo,
	pageRepo PageStore,
	renderer *markdown.Renderer,
	store storage.Storage,
	presignTTL time.Duration,
	cache *ServingCache,
	apparatus ApparatusStore,
) *WorkHandler {
	return &WorkHandler{
		workRepo:      workRepo,
		pageRepo:      pageRepo,
		renderer:      renderer,
		store:         store,
		presignTTL:    presignTTL,
		fileProcessor: fileprocessor.NewProcessor(),
		cache:         cache,
		apparatus:     apparatus,
	}
}

// CreateWorkRequest represents a request to create a work
type CreateWorkRequest struct {
	// ID — необязательный явный id: публикатор заводит том на боевом под
	// локальным id, чтобы ключ скана works/N/ был одним и тем же (спека
	// 2026-10-03-local-scans-working-set). Ноль — id выдаст последовательность.
	ID              int64             `json:"id,omitempty"`
	Title           string            `json:"title"`
	Author          string            `json:"author"`
	PublicationDate string            `json:"publication_date,omitempty"`
	Language        string            `json:"language"`
	Country         string            `json:"country"`
	Status          models.WorkStatus `json:"status"`
}

// UpdateWorkRequest represents a request to update a work.
//
// The volume coordinates (edition_id, volume_number, volume_part,
// page_offset) and the role fields (role, parent_work_id, numbering_style)
// are deliberately absent here: they need to tell "key omitted" from "key
// sent as null", which a plain struct cannot. They are read from the same
// body by parseVolumeUpdate (work_volume.go) and parseWorkMeta (work_meta.go).
type UpdateWorkRequest struct {
	Title           string            `json:"title"`
	Author          string            `json:"author"`
	PublicationDate string            `json:"publication_date,omitempty"`
	Language        string            `json:"language"`
	Country         string            `json:"country"`
	Status          models.WorkStatus `json:"status"`
}

// List retrieves a list of works
func (h *WorkHandler) List(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 50
	offset := 0

	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil {
			offset = o
		}
	}

	ctx := context.Background()
	works, err := h.workRepo.List(ctx, limit, offset, nil, nil)
	if err != nil {
		http.Error(w, "Failed to retrieve works", http.StatusInternalServerError)
		return
	}

	// Ensure we return empty array instead of null
	if works == nil {
		works = []*models.Work{}
	}

	resp := make([]workResponse, len(works))
	for i, wk := range works {
		resp[i] = newWorkResponse(r.Context(), wk, h.store, h.presignTTL)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Create creates a new work
func (h *WorkHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateWorkRequest
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

	if req.ID < 0 {
		http.Error(w, "id must be positive", http.StatusBadRequest)
		return
	}

	// Create work
	work := &models.Work{
		ID:       req.ID,
		Title:    req.Title,
		Author:   req.Author,
		Language: req.Language,
		Country:  req.Country,
		FilePath: "", // Will be set during file upload
		Status:   req.Status,
		OwnerID:  claims.UserID,
	}

	ctx := context.Background()
	if err := h.workRepo.Create(ctx, work); err != nil {
		if errors.Is(err, repository.ErrIDTaken) {
			http.Error(w, fmt.Sprintf("work id %d is already taken", req.ID), http.StatusConflict)
			return
		}
		http.Error(w, "Failed to create work", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(work)
}

// Get retrieves a work by ID
func (h *WorkHandler) Get(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid work ID", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	work, err := h.workRepo.GetByID(ctx, id)
	if err != nil {
		http.Error(w, "Work not found", http.StatusNotFound)
		return
	}

	resp := newWorkResponse(r.Context(), work, h.store, h.presignTTL)
	// Служебные работы (передние листы) не показываются в каталоге, поэтому
	// единственный путь к ним для человека — карточка их тома. Ошибка выборки
	// не роняет карточку тома, но и молчать о ней нельзя: без записи в лог
	// дети просто исчезли бы, и отличить «их нет» от «репозиторий отказал»
	// стало бы нечем.
	kids, err := h.workRepo.ListChildren(ctx, id)
	if err != nil {
		log.Printf("work %d: failed to list child works: %v", id, err)
	}
	for _, kid := range kids {
		resp.Children = append(resp.Children, newWorkResponse(r.Context(), kid, h.store, h.presignTTL))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Update updates a work
func (h *WorkHandler) Update(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid work ID", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var req UpdateWorkRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	volume, err := parseVolumeUpdate(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	meta, err := parseWorkMeta(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	work, err := h.workRepo.GetByID(ctx, id)
	if err != nil {
		http.Error(w, "Work not found", http.StatusNotFound)
		return
	}

	// Update fields
	work.Title = req.Title
	work.Author = req.Author
	work.Language = req.Language
	work.Country = req.Country
	work.Status = req.Status

	if err := applyVolumeFields(work, volume); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := applyWorkMeta(work, meta); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.workRepo.Update(ctx, work); err != nil {
		if isVolumeTakenError(err) {
			http.Error(w, "volume already taken by another work in this edition", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to update work", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(work)
}

// Delete deletes a work
func (h *WorkHandler) Delete(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid work ID", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	// Служебные работы (передние листы) снимает каскад по parent_work_id, но
	// каскад в Postgres ничего не знает про S3: без этого обхода превью и
	// производный PDF ребёнка остались бы в бакете сиротами, на которые уже
	// ничто не ссылается. Дети сносятся ДО тома — иначе список после удаления
	// строки взять уже неоткуда.
	kids, err := h.workRepo.ListChildren(ctx, id)
	if err != nil {
		http.Error(w, "Failed to list child works", http.StatusInternalServerError)
		return
	}
	for _, kid := range kids {
		if err := h.store.DeletePrefix(ctx, fmt.Sprintf("works/%d/", kid.ID)); err != nil {
			http.Error(w, "Failed to delete work files", http.StatusInternalServerError)
			return
		}
	}
	if err := h.store.DeletePrefix(ctx, fmt.Sprintf("works/%d/", id)); err != nil {
		http.Error(w, "Failed to delete work files", http.StatusInternalServerError)
		return
	}
	// Звук тома и его детей — тот же префикс в аудиобакете. До строки тома:
	// после неё не узнать, что сносить, а адреса дорожек и так ответят 410.
	if h.audioStore != nil {
		for _, kid := range kids {
			if err := h.audioStore.DeletePrefix(ctx, fmt.Sprintf("works/%d/", kid.ID)); err != nil {
				http.Error(w, "Failed to delete work audio", http.StatusInternalServerError)
				return
			}
		}
		if err := h.audioStore.DeletePrefix(ctx, fmt.Sprintf("works/%d/", id)); err != nil {
			http.Error(w, "Failed to delete work audio", http.StatusInternalServerError)
			return
		}
	}
	if err := h.workRepo.Delete(ctx, id); err != nil {
		http.Error(w, "Failed to delete work", http.StatusInternalServerError)
		return
	}

	// Кэши отдачи — последним шагом, уже после базы и S3. Про них каскад в
	// Postgres не знает ровно так же, как про хранилище: без этого снятый том
	// продолжал бы отдаваться готовыми главами до истечения часа, а карточка
	// его ссылки в мессенджерах — до суток. Догнать их потом адресно нечем:
	// PurgeChapter резолвит границы главы из базы, а строки уже нет.
	//
	// Ребёнок идёт своим вызовом: в базе он уезжает каскадом, но его кэш —
	// отдельный каталог, и каскад про него не знает.
	for _, kid := range kids {
		h.cache.DropWork(kid.ID)
	}
	h.cache.DropWork(id)

	w.WriteHeader(http.StatusNoContent)
}

// ListNotes returns the per-work editorial note index (number -> body + target page).
func (h *WorkHandler) ListNotes(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid work ID", http.StatusBadRequest)
		return
	}

	pages, err := h.pageRepo.ListByWork(context.Background(), id)
	if err != nil {
		http.Error(w, "Failed to retrieve pages", http.StatusInternalServerError)
		return
	}

	index := buildNoteIndex(pages, h.renderer)
	if index == nil {
		index = []NoteIndexEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(index)
}

// AddCategory adds a category to a work
func (h *WorkHandler) AddCategory(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workID, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid work ID", http.StatusBadRequest)
		return
	}

	var req struct {
		CategoryID int64 `json:"category_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	if err := h.workRepo.AddCategory(ctx, workID, req.CategoryID); err != nil {
		http.Error(w, "Failed to add category", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RemoveCategory removes a category from a work
func (h *WorkHandler) RemoveCategory(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workID, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid work ID", http.StatusBadRequest)
		return
	}

	categoryID, err := strconv.ParseInt(vars["categoryId"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid category ID", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	if err := h.workRepo.RemoveCategory(ctx, workID, categoryID); err != nil {
		http.Error(w, "Failed to remove category", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// UploadFile handles file upload for a work
func (h *WorkHandler) UploadFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workID, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid work ID", http.StatusBadRequest)
		return
	}

	// Parse multipart form (32MB max)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "Failed to parse form data", http.StatusBadRequest)
		return
	}
	// net/http чистит временные файлы формы только у исходного запроса, а сюда
	// приходит копия (mux.Vars и middleware делают r.WithContext) — без этой
	// строки каждый оригинал сверх 32 МБ оставался в /tmp до пересоздания
	// контейнера.
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Failed to get file from form", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate file extension
	ext := filepath.Ext(header.Filename)
	if ext != ".pdf" && ext != ".djvu" {
		http.Error(w, "Invalid file type. Only PDF and DJVU files are allowed", http.StatusBadRequest)
		return
	}

	// Write the upload to a temp file so we can both store it and inspect it.
	tmp, err := os.CreateTemp("", "upload-*"+ext)
	if err != nil {
		http.Error(w, "Failed to create temp file", http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	size, err := io.Copy(tmp, file)
	if err != nil {
		http.Error(w, "Failed to buffer file", http.StatusInternalServerError)
		return
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "Failed to rewind file", http.StatusInternalServerError)
		return
	}

	key := storage.OriginalKey(workID, header.Filename, time.Now().Unix())
	contentType := "application/octet-stream"
	if ext == ".pdf" {
		contentType = "application/pdf"
	}
	if err := h.store.Put(r.Context(), key, tmp, size, contentType); err != nil {
		http.Error(w, "Failed to store file", http.StatusInternalServerError)
		return
	}

	ctx := context.Background()
	work, err := h.workRepo.GetByID(ctx, workID)
	if err != nil {
		http.Error(w, "Failed to get work", http.StatusInternalServerError)
		return
	}
	work.FilePath = key
	work.UpdatedAt = time.Now()
	if err := h.workRepo.Update(ctx, work); err != nil {
		http.Error(w, "Failed to update work", http.StatusInternalServerError)
		return
	}

	// Page count needs a local file; the temp file is still on disk.
	pageCount, err := h.fileProcessor.GetPageCount(tmp.Name())
	if err != nil {
		pageCount = 0
	}

	response := map[string]interface{}{
		"message":    "File uploaded successfully",
		"file_key":   key,
		"page_count": pageCount,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// CreatePages creates pages from the uploaded file
func (h *WorkHandler) CreatePages(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workID, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid work ID", http.StatusBadRequest)
		return
	}

	// Parse request body for page range
	var req struct {
		PageRange string `json:"page_range"` // e.g. "1-5,7,9-11"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// If no body, default to all pages
		req.PageRange = ""
	}

	ctx := context.Background()

	// Get work
	work, err := h.workRepo.GetByID(ctx, workID)
	if err != nil {
		http.Error(w, "Failed to get work", http.StatusInternalServerError)
		return
	}

	if work.FilePath == "" {
		http.Error(w, "No file uploaded for this work", http.StatusBadRequest)
		return
	}

	// Download the original from storage to a temp dir for CLI processing.
	tmpDir, err := os.MkdirTemp("", fmt.Sprintf("work_%d_*", workID))
	if err != nil {
		http.Error(w, "Failed to create temp dir", http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tmpDir)

	localOriginal := filepath.Join(tmpDir, "original"+filepath.Ext(work.FilePath))
	if err := h.downloadToFile(ctx, work.FilePath, localOriginal); err != nil {
		http.Error(w, "Failed to fetch source file", http.StatusInternalServerError)
		return
	}

	// Get page count
	pageCount, err := h.fileProcessor.GetPageCount(localOriginal)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get page count: %v", err), http.StatusInternalServerError)
		return
	}

	if pageCount == 0 {
		http.Error(w, "File has no pages", http.StatusBadRequest)
		return
	}

	// Parse page range
	var pagesToCreate []int
	if req.PageRange == "" {
		// Create all pages
		for i := 1; i <= pageCount; i++ {
			pagesToCreate = append(pagesToCreate, i)
		}
	} else {
		pagesToCreate, err = parsePageRange(req.PageRange, pageCount)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid page range: %v", err), http.StatusBadRequest)
			return
		}
	}

	createdCount := 0
	for _, i := range pagesToCreate {
		previewPath, err := h.fileProcessor.ExtractPageImage(localOriginal, i, tmpDir)
		previewKey := ""
		if err != nil {
			fmt.Printf("Failed to extract preview for page %d: %v\n", i, err)
		} else {
			previewKey = storage.PagePreviewKey(workID, i)
			if perr := h.uploadFile(ctx, previewPath, previewKey, "image/png"); perr != nil {
				fmt.Printf("Failed to upload preview for page %d: %v\n", i, perr)
				previewKey = ""
			}
		}

		textContent, err := h.fileProcessor.ExtractPageText(localOriginal, i)
		if err != nil {
			fmt.Printf("Failed to extract text for page %d: %v\n", i, err)
			textContent = ""
		}

		page := &models.Page{
			WorkID:          workID,
			PageNumber:      i,
			ContentMarkdown: textContent,
			PreviewPath:     previewKey,
			Status:          models.PageStatusNotProofread,
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}
		if err := h.pageRepo.Create(ctx, page); err != nil {
			fmt.Printf("Failed to create page %d: %v\n", i, err)
			continue
		}
		createdCount++
	}

	response := map[string]interface{}{
		"message":         "Pages created successfully",
		"created_pages":   createdCount,
		"total_pages":     len(pagesToCreate),
		"requested_pages": len(pagesToCreate),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// downloadToFile streams a stored object to a local path.
func (h *WorkHandler) downloadToFile(ctx context.Context, key, dest string) error {
	rc, err := h.store.Get(ctx, key)
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

// uploadFile streams a local file into storage under key.
func (h *WorkHandler) uploadFile(ctx context.Context, src, key, contentType string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	return h.store.Put(ctx, key, f, info.Size(), contentType)
}

// parsePageRange parses a page range string like "1-5,7,9-11" into a slice of page numbers
func parsePageRange(rangeStr string, maxPage int) ([]int, error) {
	pages := make(map[int]bool) // Use map to avoid duplicates
	parts := strings.Split(rangeStr, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Check if it's a range (e.g., "1-5")
		if strings.Contains(part, "-") {
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) != 2 {
				return nil, fmt.Errorf("invalid range format: %s", part)
			}

			start, err := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid start page: %s", rangeParts[0])
			}

			end, err := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid end page: %s", rangeParts[1])
			}

			if start < 1 || end > maxPage || start > end {
				return nil, fmt.Errorf("invalid range %d-%d (file has %d pages)", start, end, maxPage)
			}

			for i := start; i <= end; i++ {
				pages[i] = true
			}
		} else {
			// Single page number
			pageNum, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid page number: %s", part)
			}

			if pageNum < 1 || pageNum > maxPage {
				return nil, fmt.Errorf("page %d out of range (file has %d pages)", pageNum, maxPage)
			}

			pages[pageNum] = true
		}
	}

	// Convert map to sorted slice
	result := make([]int, 0, len(pages))
	for pageNum := range pages {
		result = append(result, pageNum)
	}

	// Sort pages
	sort.Ints(result)

	return result, nil
}
