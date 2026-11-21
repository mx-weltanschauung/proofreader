package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"proofreader/internal/models"
)

func (h *ProofreadHandler) submit(w http.ResponseWriter, r *http.Request, run models.ProofreadRun) {
	ctx := r.Context()
	pages, err := h.runs.ListSubmissionPages(ctx, run.ID)
	if err != nil {
		proofreadFailure(w, err)
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
		result, err := h.submitPage(ctx, run, snapshot, ipHash, limit)
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

func (h *ProofreadHandler) submitPage(ctx context.Context, run models.ProofreadRun, snapshot models.ProofreadRunPage, ipHash string, limit int) (models.PageSubmitResult, error) {
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
	return h.runs.SubmitReadyPage(ctx, run.ID, run.UserID, snapshot.PageID, ipHash, limit, time.Now().Add(-time.Hour))
}
