package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"proofreader/internal/models"
)

func (h *ProofreadHandler) submit(w http.ResponseWriter, r *http.Request, run models.ProofreadRun) {
	var body struct {
		Pages []struct {
			PageID   int64            `json:"page_id"`
			Accepted map[int64]string `json:"accepted"`
		} `json:"pages"`
	}
	if !proofreadBody(w, r, &body) {
		return
	}
	previews := make(map[int64]map[int64]string, len(body.Pages))
	pageIDs := make([]int64, 0, len(body.Pages))
	for _, page := range body.Pages {
		if page.PageID <= 0 || len(page.Accepted) == 0 || previews[page.PageID] != nil {
			writeError(w, 400, "Неверный список проверенных полос")
			return
		}
		for findingID := range page.Accepted {
			if findingID <= 0 {
				writeError(w, 400, "Неверный номер находки")
				return
			}
		}
		previews[page.PageID] = page.Accepted
		pageIDs = append(pageIDs, page.PageID)
	}
	if len(pageIDs) == 0 {
		writeError(w, 400, "Нужны проверенные полосы")
		return
	}
	ctx := r.Context()
	if _, err := h.chapter(ctx, run); err != nil {
		proofreadFailure(w, err)
		return
	}
	pages, err := h.runs.ListSubmissionPages(ctx, run.ID, pageIDs)
	if err != nil {
		proofreadFailure(w, err)
		return
	}
	if len(pages) != len(pageIDs) {
		writeError(w, 404, "Полоса недоступна")
		return
	}
	limit, err := h.limit.Get(ctx)
	if err != nil {
		proofreadFailure(w, err)
		return
	}
	ipHash := hashIP(clientIP(r, h.trustProxy), h.jwtSecret)
	results := make([]models.PageSubmitResult, 0, len(pages))
	for _, snapshot := range pages {
		result, err := h.submitPage(ctx, run, snapshot, previews[snapshot.PageID], ipHash, limit)
		if err != nil {
			log.Printf("proofread submit run %d page %d: %v", run.ID, snapshot.PageID, err)
			result = models.PageSubmitResult{PageID: snapshot.PageID, Status: "failed"}
		}
		results = append(results, result)
	}
	writeJSON(w, struct {
		Pages []models.PageSubmitResult `json:"pages"`
	}{results})
}

func (h *ProofreadHandler) submitPage(ctx context.Context, run models.ProofreadRun, snapshot models.ProofreadRunPage, accepted map[int64]string, ipHash string, limit int) (models.PageSubmitResult, error) {
	result := models.PageSubmitResult{PageID: snapshot.PageID, Status: "failed"}
	if snapshot.SubmittedAt != nil {
		result.Status = "already_submitted"
		result.SuggestionID = snapshot.SuggestionID
		return result, nil
	}
	chapter, err := h.chapter(ctx, run)
	if err != nil {
		return result, err
	}
	page, err := h.page(ctx, chapter, snapshot)
	if err != nil || page.SkipReason != "" {
		return result, err
	}
	return h.runs.SubmitReadyPage(ctx, run.ID, run.UserID, snapshot.PageID, accepted, ipHash, limit, time.Now().Add(-time.Hour))
}
