package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

type workerCandidate struct {
	CandidateKey string          `json:"candidate_key"`
	StartRune    *int            `json:"start_rune"`
	EndRune      *int            `json:"end_rune"`
	Old          *string         `json:"old"`
	New          *string         `json:"new"`
	Source       string          `json:"source"`
	Reason       string          `json:"reason"`
	ScanBBox     json.RawMessage `json:"scan_bbox,omitempty"`
}

func validWorkerFinding(base []rune, f models.ProofreadFinding) bool {
	if strings.TrimSpace(f.CandidateKey) == "" || utf8.RuneCountInString(f.CandidateKey) > 256 || strings.TrimSpace(f.Reason) == "" {
		return false
	}
	if f.Source != "model" && f.Source != "ocr" {
		return false
	}
	if f.ID != 0 || f.Decision != "" || f.Replacement != "" || f.Old == f.New {
		return false
	}
	if f.StartRune < 0 || f.EndRune < f.StartRune || f.EndRune > len(base) || string(base[f.StartRune:f.EndRune]) != f.Old {
		return false
	}
	if len(f.ScanBBox) == 0 || string(f.ScanBBox) == "null" {
		return true
	}
	// Normalized [x, y, width, height] in the original scan.
	var box []*float64
	if json.Unmarshal(f.ScanBBox, &box) != nil || len(box) != 4 {
		return false
	}
	for _, v := range box {
		if v == nil {
			return false
		}
	}
	x, y, width, height := *box[0], *box[1], *box[2], *box[3]
	return x >= 0 && y >= 0 && width > 0 && height > 0 && x+width <= 1 && y+height <= 1
}

func (h *ProofreadHandler) pair(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Code string `json:"code"`
	}
	if !proofreadBody(w, r, &b) {
		return
	}
	if b.Code == "" {
		writeError(w, 400, "Введите код")
		return
	}
	run, token, err := h.runs.ConsumePairCode(r.Context(), b.Code)
	if err != nil {
		proofreadFailure(w, err)
		return
	}
	if _, err = h.chapter(r.Context(), run); err != nil {
		proofreadFailure(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"run_id": run.ID, "token": token})
}

func (h *ProofreadHandler) worker(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		writeError(w, 401, "Требуется токен работы")
		return
	}
	ctx := r.Context()
	id := proofreadID(r, "id")
	run, err := h.runs.GetForWorker(ctx, id, token)
	if err != nil {
		proofreadFailure(w, err)
		return
	}
	var upload workerUpload
	if r.Method == http.MethodPut && !proofreadBody(w, r, &upload) {
		return
	}
	c, err := h.chapter(ctx, run)
	if err != nil {
		proofreadFailure(w, err)
		return
	}
	after := 0
	if raw := r.URL.Query().Get("after"); raw != "" {
		after, err = strconv.Atoi(raw)
		if err != nil || after < 0 {
			writeError(w, 400, "Неверный курсор")
			return
		}
	}
	var s models.ProofreadRunPage
	if r.Method == http.MethodGet {
		s, err = h.runs.NextPage(ctx, id, after)
		if errors.Is(err, repository.ErrProofreadRunNotFound) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	} else {
		s, err = h.runs.RunPage(ctx, id, proofreadID(r, "pageId"))
	}
	if err != nil {
		proofreadFailure(w, err)
		return
	}
	p, err := h.page(ctx, c, s)
	if err != nil {
		proofreadFailure(w, err)
		return
	}
	if r.Method == http.MethodGet {
		p.Findings = nil
		writeJSON(w, p)
		return
	}
	if p.SkipReason != "" {
		writeError(w, 422, p.SkipReason)
		return
	}
	h.upload(w, r, id, s, upload)
}

type workerUpload struct {
	Base          string            `json:"base_sha256"`
	Findings      []workerCandidate `json:"findings"`
	AnalysisError string            `json:"analysis_error"`
}

func (h *ProofreadHandler) upload(w http.ResponseWriter, r *http.Request, id int64, p models.ProofreadRunPage, b workerUpload) {
	if b.Base != p.BaseSHA256 {
		proofreadFailure(w, repository.ErrProofreadBaseMismatch)
		return
	}
	if (b.Findings == nil) == (strings.TrimSpace(b.AnalysisError) == "") {
		writeError(w, 422, "Передайте находки или ошибку анализа")
		return
	}
	var err error
	if b.Findings != nil {
		base := []rune(p.BaseMarkdown)
		keys := make(map[string]bool, len(b.Findings))
		findings := make([]models.ProofreadFinding, 0, len(b.Findings))
		for _, candidate := range b.Findings {
			if candidate.EndRune == nil || candidate.Old == nil || candidate.StartRune == nil || candidate.New == nil {
				proofreadFailure(w, repository.ErrProofreadInvalidFinding)
				return
			}
			f := models.ProofreadFinding{CandidateKey: candidate.CandidateKey, StartRune: *candidate.StartRune, EndRune: *candidate.EndRune, Old: *candidate.Old, New: *candidate.New, Source: candidate.Source, Reason: candidate.Reason, ScanBBox: candidate.ScanBBox}

			if keys[f.CandidateKey] || !validWorkerFinding(base, f) {
				proofreadFailure(w, repository.ErrProofreadInvalidFinding)
				return
			}
			keys[f.CandidateKey] = true
			findings = append(findings, f)
		}
		err = h.runs.PutFindings(r.Context(), id, p.PageID, b.Base, findings)
	} else {
		err = h.runs.SetPageAnalysisError(r.Context(), id, p.PageID, b.Base, b.AnalysisError)
	}
	if err != nil {
		proofreadFailure(w, err)
		return
	}
	w.WriteHeader(204)
}
