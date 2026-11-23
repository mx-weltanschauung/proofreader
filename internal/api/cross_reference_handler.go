package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/pkg/markdown"
)

type CrossReferenceHandler struct {
	editionID int64
	repo      *repository.CrossReferenceRepository
	pages     PageStore
	renderer  *markdown.Renderer
}

func crossReferenceID(r *http.Request, key string) int64 {
	id, err := strconv.ParseInt(mux.Vars(r)[key], 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func (h *CrossReferenceHandler) scope(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	w.Header().Set("Cache-Control", "no-store")
	editionID, workID := crossReferenceID(r, "id"), crossReferenceID(r, "workId")
	if editionID < 1 || workID < 1 {
		writeError(w, http.StatusBadRequest, "Неверный адрес тома")
		return 0, 0, false
	}
	if editionID != h.editionID {
		writeError(w, http.StatusNotFound, "Сверка отсылок для этого собрания недоступна")
		return 0, 0, false
	}
	exists, err := h.repo.InScope(r.Context(), editionID, workID)
	if err != nil {
		crossReferenceError(w, err)
		return 0, 0, false
	}
	if !exists {
		writeError(w, http.StatusNotFound, "Том с печатной нумерацией в этом собрании не найден")
		return 0, 0, false
	}
	return editionID, workID, true
}

func crossReferenceError(w http.ResponseWriter, err error) {
	log.Printf("Библиографические ссылки: %v", err)
	writeError(w, http.StatusInternalServerError, "Не удалось обработать ссылки")
}

func (h *CrossReferenceHandler) Scan(w http.ResponseWriter, r *http.Request) {
	editionID, workID, ok := h.scope(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	pages, err := h.pages.ListByWork(ctx, workID)
	if err != nil {
		crossReferenceError(w, err)
		return
	}
	var found []models.CrossReferenceOccurrence
	for _, p := range pages {
		if ctx.Err() != nil {
			return
		}
		digest := sha256.Sum256([]byte(p.ContentMarkdown))
		content := markdown.PageContent{PageNumber: p.PageNumber, Content: p.ContentMarkdown}
		content.Decorate = func(note, html string) string {
			return markdown.LinkBibliographicReferences(html, func(ref markdown.BibliographicReference) int64 {
				found = append(found, models.CrossReferenceOccurrence{
					SourcePageID: p.ID, SourceDigest: digest[:], Note: note, Ordinal: ref.Ordinal,
					Quote: ref.Quote, Context: ref.Context, Volume: ref.Volume, PrintedPage: ref.PrintedPage,
					EditionNumber: ref.EditionNumber,
				})
				return 0
			})
		}
		h.renderer.CollectPages([]markdown.PageContent{content})
	}
	added, err := h.repo.Add(ctx, editionID, workID, found)
	if err != nil {
		crossReferenceError(w, err)
		return
	}
	writeJSON(w, map[string]int{"detected": len(found), "added": added})
}

func (h *CrossReferenceHandler) List(w http.ResponseWriter, r *http.Request) {
	editionID, workID, ok := h.scope(w, r)
	if !ok {
		return
	}
	status := r.URL.Query().Get("status")
	switch status {
	case "":
		status = models.CrossReferencePending
	case models.CrossReferencePending, models.CrossReferenceConfirmed, models.CrossReferenceRejected:
	default:
		writeError(w, http.StatusBadRequest, "Неизвестное решение")
		return
	}
	var after int64
	if raw := r.URL.Query().Get("after"); raw != "" {
		var err error
		after, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || after < 0 {
			writeError(w, http.StatusBadRequest, "Неверный адрес очереди")
			return
		}
	}
	const size = 30
	items, err := h.repo.List(r.Context(), editionID, workID, after, status, size+1)
	if err != nil {
		crossReferenceError(w, err)
		return
	}
	response := struct {
		Items     []models.CrossReferenceCandidate `json:"items"`
		NextAfter int64                            `json:"next_after,omitempty"`
	}{Items: items}
	if len(items) > size {
		response.Items = items[:size]
		response.NextAfter = items[size-1].ID
	}
	writeJSON(w, response)
}

func (h *CrossReferenceHandler) Decide(w http.ResponseWriter, r *http.Request) {
	editionID, workID, ok := h.scope(w, r)
	if !ok {
		return
	}
	var in struct {
		Decision     string `json:"decision"`
		TargetPageID *int64 `json:"target_page_id"`
	}
	id := crossReferenceID(r, "referenceId")
	if id < 1 || json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil ||
		!validCrossReferenceDecision(in.Decision, in.TargetPageID) {
		writeError(w, http.StatusBadRequest, "Укажите решение и полосу для подтверждения")
		return
	}
	changed, err := h.repo.Decide(r.Context(), editionID, workID, id, in.Decision, in.TargetPageID)
	if err != nil {
		crossReferenceError(w, err)
		return
	}
	if !changed {
		writeError(w, http.StatusConflict, "Ссылка уже рассмотрена, исходный текст или адрес цели изменились")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validCrossReferenceDecision(decision string, target *int64) bool {
	return (decision == models.CrossReferenceRejected && target == nil) ||
		(decision == models.CrossReferenceConfirmed && target != nil && *target > 0)
}

func (h *CrossReferenceHandler) Open(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := crossReferenceID(r, "referenceId")
	if id < 1 || h.editionID < 1 {
		http.NotFound(w, r)
		return
	}
	ref, ok, err := h.repo.Destination(r.Context(), h.editionID, id)
	if err != nil {
		crossReferenceError(w, err)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/works/%d/pages/%d?verified_page_id=%d", ref.TargetWorkID, ref.TargetPageNumber, ref.TargetPageID), http.StatusFound)
}

// Published links are never written to the shared range cache. The redirect
// also checks the live address and visibility for already opened tabs.
func (h *CrossReferenceHandler) published(w http.ResponseWriter, r *http.Request, workID int64, start, end int) publishedByPage {
	if h == nil || h.editionID < 1 {
		return nil
	}
	refs, err := h.repo.Published(r.Context(), h.editionID, workID, start, end)
	if err != nil {
		log.Printf("Проверка библиографических ссылок тома %d: %v", workID, err)
		return nil
	}
	if len(refs) == 0 {
		return nil
	}
	w.Header().Set("Cache-Control", "no-store")
	byPage := make(publishedByPage)
	for _, ref := range refs {
		byPage[ref.SourcePageID] = append(byPage[ref.SourcePageID], ref)
	}
	return byPage
}

type publishedByPage map[int64][]models.PublishedCrossReference

func (refs publishedByPage) content(page *models.Page) markdown.PageContent {
	content := markdown.PageContent{PageNumber: page.PageNumber, Content: page.ContentMarkdown}
	published := refs[page.ID]
	if len(published) == 0 {
		return content
	}
	digest := sha256.Sum256([]byte(page.ContentMarkdown))
	if !bytes.Equal(digest[:], published[0].SourceDigest) {
		// A page edit may have landed between the visibility query and the
		// range read. An identical quote on the new page proves nothing.
		return content
	}
	content.Decorate = func(note, html string) string {
		return markdown.LinkBibliographicReferences(html, func(ref markdown.BibliographicReference) int64 {
			for _, link := range published {
				if note == link.Note && ref.Ordinal == link.Ordinal && ref.Quote == link.Quote {
					return link.ID
				}
			}
			return 0
		})
	}
	return content
}
