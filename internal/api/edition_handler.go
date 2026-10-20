package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

// EditionHandler handles edition endpoints
type EditionHandler struct {
	editionStore EditionStore
}

// NewEditionHandler creates a new edition handler
func NewEditionHandler(editionStore EditionStore) *EditionHandler {
	return &EditionHandler{
		editionStore: editionStore,
	}
}

// EditionRequest is the body of POST/PUT /api/editions.
type EditionRequest struct {
	// ID — необязательный явный id при создании (публикатор, спека
	// 2026-10-03-local-scans-working-set). Update его не читает.
	ID          int64  `json:"id,omitempty"`
	Title       string `json:"title"`
	Slug        string `json:"slug"`
	URLSlug     string `json:"url_slug"`
	Description string `json:"description"`
	// Указатель, а не int: у собрания без известного плана поле пустое, и
	// подпись полки различает «45 из 55» и просто «45 томов». Ноль здесь
	// значил бы «в собрании ноль томов».
	VolumesPlanned *int `json:"volumes_planned"`
	// Closed — завести издание сразу закрытым; Update поле не читает.
	Closed bool `json:"closed,omitempty"`
	// VolumeNumbering — "printed" или "order" (models.VolumeNumbering*).
	// Указатель: поле не пришло — при создании печатная нумерация, при правке
	// прежнее значение. Иначе форма или скрипт, не знающие о поле, молча
	// возвращали бы условной нумерации печатную — и выдуманный номер тома
	// снова уезжал бы в цитату.
	VolumeNumbering *string `json:"volume_numbering"`
}

// volumeNumberingOf — значение поля из тела запроса; пусто — не пришло.
// Ошибка — значение вне перечня.
func volumeNumberingOf(req EditionRequest) (string, error) {
	if req.VolumeNumbering == nil {
		return "", nil
	}
	v := *req.VolumeNumbering
	if !models.ValidVolumeNumbering(v) {
		return "", fmt.Errorf("volume_numbering must be %q or %q, got %q",
			models.VolumeNumberingPrinted, models.VolumeNumberingOrder, v)
	}
	return v, nil
}

// List retrieves all editions
func (h *EditionHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	editions, err := h.editionStore.List(ctx)
	if err != nil {
		http.Error(w, "Failed to retrieve editions", http.StatusInternalServerError)
		return
	}

	// An empty result set comes back from the repository as a nil slice, and
	// json encodes that as null. The client expects a list — return [].
	if editions == nil {
		editions = []*models.Edition{}
	}
	if !isStaffRequest(r) {
		open := make([]*models.Edition, 0, len(editions))
		for _, e := range editions {
			if !e.Closed {
				open = append(open, e)
			}
		}
		editions = open
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(editions)
}

// Get retrieves one edition by id. Reading is public, like every GET in this
// project.
func (h *EditionHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid edition ID", http.StatusBadRequest)
		return
	}

	// A missing row surfaces as an error from the repository (pgx.ErrNoRows),
	// the same way it does in ChapterHandler.Get — any error here means "not
	// found", not a distinguishable server fault.
	edition, err := h.editionStore.GetByID(context.Background(), id)
	if err != nil || edition == nil || (edition.Closed && !isStaffRequest(r)) {
		http.Error(w, "Edition not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(edition)
}

// Create creates a new edition
func (h *EditionHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req EditionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Title == "" || req.Slug == "" {
		http.Error(w, "title and slug are required", http.StatusBadRequest)
		return
	}
	if req.ID < 0 {
		http.Error(w, "id must be positive", http.StatusBadRequest)
		return
	}
	numbering, err := volumeNumberingOf(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if numbering == "" {
		numbering = models.VolumeNumberingPrinted
	}

	edition := &models.Edition{
		ID:              req.ID,
		Title:           req.Title,
		Slug:            req.Slug,
		URLSlug:         req.URLSlug,
		Description:     req.Description,
		VolumesPlanned:  req.VolumesPlanned,
		VolumeNumbering: numbering,
		Closed:          req.Closed,
	}

	ctx := context.Background()
	if err := h.editionStore.Create(ctx, edition); err != nil {
		if errors.Is(err, repository.ErrIDTaken) {
			http.Error(w, fmt.Sprintf("edition id %d is already taken", req.ID), http.StatusConflict)
			return
		}
		http.Error(w, "Failed to create edition", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(edition)
}

// Update updates an edition
func (h *EditionHandler) Update(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid edition ID", http.StatusBadRequest)
		return
	}

	var req EditionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Title == "" || req.Slug == "" {
		http.Error(w, "title and slug are required", http.StatusBadRequest)
		return
	}
	numbering, err := volumeNumberingOf(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	// Same "any error or nil means not found" rule as Get — kept in one place
	// there, but Update dereferences edition right below, so the nil check
	// belongs here too.
	edition, err := h.editionStore.GetByID(ctx, id)
	if err != nil || edition == nil {
		http.Error(w, "Edition not found", http.StatusNotFound)
		return
	}

	edition.Title = req.Title
	edition.Slug = req.Slug
	edition.URLSlug = req.URLSlug
	edition.Description = req.Description
	edition.VolumesPlanned = req.VolumesPlanned
	if numbering != "" {
		edition.VolumeNumbering = numbering
	}

	if err := h.editionStore.Update(ctx, edition); err != nil {
		http.Error(w, "Failed to update edition", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(edition)
}

// Delete deletes an edition
func (h *EditionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid edition ID", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	if err := h.editionStore.Delete(ctx, id); err != nil {
		// Тома закрытого издания открылись бы сносом молча (edition_id —
		// ON DELETE SET NULL): отказ тем же голосом, что у шага вниз 000040.
		if errors.Is(err, repository.ErrEditionClosed) {
			writeError(w, http.StatusConflict, "Собрание закрыто: удаление открыло бы все его тома — сначала откройте его, потом удаляйте")
			return
		}
		http.Error(w, "Failed to delete edition", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListWorks retrieves the works (volumes) belonging to an edition
func (h *EditionHandler) ListWorks(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid edition ID", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	staff := isStaffRequest(r)
	if !staff {
		// Закрытое издание закрывает всё внутри: его тома не-сотруднику
		// не отдаются, а само издание «не найдено».
		if edition, err := h.editionStore.GetByID(ctx, id); err == nil && edition != nil && edition.Closed {
			http.Error(w, "Edition not found", http.StatusNotFound)
			return
		}
	}
	works, err := h.editionStore.ListWorkSummaries(ctx, id)
	if err != nil {
		http.Error(w, "Failed to retrieve works", http.StatusInternalServerError)
		return
	}
	if !staff {
		open := make([]*models.VolumeSummary, 0, len(works))
		for _, wk := range works {
			if wk.ClosedBy == "" {
				open = append(open, wk)
			}
		}
		works = open
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(works)
}
