package api

import (
	"context"
	"encoding/json"
	"net/http"
)

func (h *ReaderCommentHandler) Reply(w http.ResponseWriter, r *http.Request) {
	viewer := commentViewer(r)
	if viewer.ID == 0 {
		writeError(w, 401, "Требуется вход")
		return
	}
	var req commentRequest
	if !decodeComment(w, r, &req) {
		return
	}
	body, ok := commentBody(w, req.Body)
	if !ok {
		return
	}
	reply, err := h.comments.Reply(context.WithoutCancel(r.Context()), viewer, commentID(r, "id"),
		body, hashIP(clientIP(r, h.trustProxy), h.secret))
	if err != nil {
		commentError(w, err)
		return
	}
	writeJSONStatus(w, http.StatusCreated, reply)
}

func (h *ReaderCommentHandler) Thread(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	thread, err := h.comments.Thread(r.Context(), commentViewer(r), commentID(r, "id"))
	if err != nil {
		commentError(w, err)
		return
	}
	writeJSON(w, thread)
}

func (h *ReaderCommentHandler) IncomingReplies(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	limit, offset := commentPagination(r)
	replies, err := h.comments.IncomingReplies(r.Context(), commentViewer(r), limit, offset)
	if err != nil {
		commentError(w, err)
		return
	}
	writeJSON(w, replies)
}

func (h *ReaderCommentHandler) UnreadReplies(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	count, err := h.comments.UnreadReplies(r.Context(), commentViewer(r))
	if err != nil {
		commentError(w, err)
		return
	}
	writeJSON(w, map[string]int{"count": count})
}

func (h *ReaderCommentHandler) MarkRepliesRead(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) == 0 || len(req.IDs) > 200 {
		writeError(w, 400, "Укажите от 1 до 200 ответов в ветке")
		return
	}
	if err := h.comments.MarkRepliesRead(r.Context(), commentViewer(r), commentID(r, "id"), req.IDs); err != nil {
		commentError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
