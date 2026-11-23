package api

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"proofreader/internal/middleware"
	"proofreader/internal/repository"
)

type ReadLaterHandler struct {
	shelf *repository.ReadLaterRepository
}

func (h *ReadLaterHandler) List(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.GetUserFromContext(r.Context())
	w.Header().Set("Cache-Control", "private, no-store")
	entries, err := h.shelf.List(r.Context(), claims.UserID, staffContext(r.Context()))
	if err != nil {
		log.Printf("read later list: %v", err)
		writeError(w, http.StatusInternalServerError, "Не удалось загрузить полку")
		return
	}
	writeJSON(w, entries)
}

func (h *ReadLaterHandler) Change(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.GetUserFromContext(r.Context())
	vars := mux.Vars(r)
	kind := vars["kind"]
	id, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil || id <= 0 || (kind != "work" && kind != "chapter") {
		writeError(w, http.StatusBadRequest, "Некорректный адрес публикации")
		return
	}

	if r.Method == http.MethodDelete {
		err = h.shelf.Remove(r.Context(), claims.UserID, kind == "chapter", id)
	} else {
		var available bool
		available, err = h.shelf.Save(r.Context(), claims.UserID, staffContext(r.Context()), kind == "chapter", id)
		if err == nil && !available {
			writeError(w, http.StatusNotFound, "Публикация не найдена")
			return
		}
	}

	if err != nil {
		log.Printf("read later change: %v", err)
		writeError(w, http.StatusInternalServerError, "Не удалось обновить полку")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
