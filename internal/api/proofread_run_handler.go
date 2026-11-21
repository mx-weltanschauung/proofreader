package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/pkg/storage"
)

type ProofreadHandler struct {
	runs     ProofreadRunStore
	chapters ChapterGetter
	pages    PageStore
	closed   ClosedStore
	scans    ScanVisibilityStore
	storage  storage.Storage
	ttl      time.Duration
}

func (rt *Router) WithProofread(h *ProofreadHandler) *Router { rt.proofread = h; return rt }

func proofreadBody(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil || d.Decode(new(any)) != io.EOF {
		writeError(w, 400, "Некорректный JSON")
		return false
	}
	return true
}

func proofreadFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrProofreadRunNotFound), errors.Is(err, errHiddenChapter):
		writeError(w, 404, "Работа недоступна")
	case errors.Is(err, repository.ErrProofreadCredential):
		writeError(w, 403, "Недействительные данные доступа")
	case errors.Is(err, repository.ErrProofreadBaseMismatch), errors.Is(err, repository.ErrProofreadPageSubmitted):
		writeError(w, 409, "Состояние полосы изменилось")
	case errors.Is(err, repository.ErrProofreadInvalidFinding), errors.Is(err, repository.ErrProofreadInvalidDecision):
		writeError(w, 422, "Некорректная правка")
	default:
		writeError(w, 500, "Ошибка чтения или сохранения работы")
	}
}

func proofreadID(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(mux.Vars(r)[name], 10, 64)
	return n
}

func (h *ProofreadHandler) chapter(ctx context.Context, run models.ProofreadRun) (*models.Chapter, error) {
	by, err := h.closed.ClosedBy(ctx, run.WorkID)
	if err != nil {
		return nil, err
	}
	if by != "" {
		return nil, errHiddenChapter
	}
	c, err := h.chapters.GetByID(ctx, run.ChapterID)
	if err != nil {
		return nil, err
	}
	if c == nil || c.WorkID != run.WorkID || chapterIsClosed(c) {
		return nil, errHiddenChapter
	}
	return c, nil
}

type proofreadPage struct {
	models.ProofreadRunPage
	SkipReason string `json:"skip_reason,omitempty"`
	ScanURL    string `json:"scan_url,omitempty"`
}

func (h *ProofreadHandler) page(ctx context.Context, c *models.Chapter, s models.ProofreadRunPage) (proofreadPage, error) {
	skipped := proofreadPage{ProofreadRunPage: models.ProofreadRunPage{PageID: s.PageID, PageNumber: s.PageNumber}, SkipReason: "Полоса недоступна"}
	p, err := h.pages.GetByID(ctx, s.PageID)
	if errors.Is(err, errHiddenPage) {
		return skipped, nil
	}
	if err != nil {
		return skipped, err
	}
	if p == nil || p.WorkID != c.WorkID || p.PageNumber != s.PageNumber || p.PageNumber < c.StartPage || p.PageNumber > c.EndPage {
		return skipped, nil
	}
	if p.Status == models.PageStatusEmpty || models.IsBlankArticleText(p.ContentMarkdown) || models.IsBlankArticleText(s.BaseMarkdown) {
		skipped.SkipReason = "Пустая полоса"
		return skipped, nil
	}
	whole, spans, err := scanAccess(ctx, h.scans, c.WorkID)
	if err != nil {
		return skipped, err
	}
	skipped.SkipReason = "Скан недоступен"
	if whole || inSpans(spans, p.PageNumber) || p.PreviewPath == "" {
		return skipped, nil
	}
	if _, _, err := h.storage.Head(ctx, p.PreviewPath); err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) {
			return skipped, nil
		}
		return skipped, err
	}
	url := presign(ctx, h.storage, p.PreviewPath, h.ttl)
	if url == "" {
		return skipped, nil
	}
	return proofreadPage{ProofreadRunPage: s, ScanURL: url}, nil
}

func (h *ProofreadHandler) reader(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx := r.Context()
	claims, ok := middleware.GetUserFromContext(ctx)
	if !ok {
		writeError(w, 401, "Требуется вход")
		return
	}
	if claims.Role != models.RoleReader {
		writeError(w, 403, "Требуется аккаунт читателя")
		return
	}
	id := proofreadID(r, "id")
	if mux.Vars(r)["id"] == "" {
		h.create(w, r, claims.UserID)
		return
	}
	run, err := h.runs.GetForUser(ctx, id, claims.UserID)
	if err != nil {
		proofreadFailure(w, err)
		return
	}
	c, err := h.chapter(ctx, run)
	if err != nil {
		proofreadFailure(w, err)
		return
	}
	if r.Method == "POST" {
		code, err := h.runs.IssuePairCode(ctx, id, claims.UserID)
		if err != nil {
			proofreadFailure(w, err)
			return
		}
		writeJSON(w, map[string]string{"code": code})
		return
	}
	pages, err := h.runs.ListPages(ctx, id)
	if err != nil {
		proofreadFailure(w, err)
		return
	}
	out := make([]proofreadPage, 0, len(pages))
	for _, s := range pages {
		p, err := h.page(ctx, c, s)
		if err != nil {
			proofreadFailure(w, err)
			return
		}
		if r.Method == http.MethodPatch {
			for _, f := range p.Findings {
				if f.ID == proofreadID(r, "findingId") {
					h.decide(w, r, run, f)
					return
				}
			}
			continue
		}
		out = append(out, p)
	}
	if r.Method == "PATCH" {
		proofreadFailure(w, repository.ErrProofreadRunNotFound)
		return
	}
	writeJSON(w, struct {
		models.ProofreadRun
		Pages []proofreadPage `json:"pages"`
	}{run, out})
}

func (h *ProofreadHandler) create(w http.ResponseWriter, r *http.Request, userID int64) {
	ctx := r.Context()
	var b struct {
		WorkID    int64 `json:"work_id"`
		ChapterID int64 `json:"chapter_id"`
	}
	if !proofreadBody(w, r, &b) {
		return
	}
	if b.WorkID <= 0 || b.ChapterID <= 0 {
		writeError(w, 400, "Нужны том и глава")
		return
	}
	_, err := h.chapter(ctx, models.ProofreadRun{WorkID: b.WorkID, ChapterID: b.ChapterID})
	if err != nil {
		proofreadFailure(w, err)
		return
	}
	run, err := h.runs.Create(ctx, userID, b.WorkID, b.ChapterID)
	if err != nil {
		proofreadFailure(w, err)
		return
	}
	writeJSONStatus(w, 201, run)
}

func (h *ProofreadHandler) decide(w http.ResponseWriter, r *http.Request, run models.ProofreadRun, f models.ProofreadFinding) {
	var b struct {
		Decision    models.FindingDecision `json:"decision"`
		Replacement *string                `json:"replacement"`
	}
	if !proofreadBody(w, r, &b) {
		return
	}
	replacement := f.New
	if b.Replacement != nil {
		replacement = *b.Replacement
	}
	err := h.runs.SetDecision(r.Context(), run.ID, f.ID, run.UserID, b.Decision, replacement)
	if err != nil {
		proofreadFailure(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
