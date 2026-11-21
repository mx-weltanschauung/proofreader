package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"proofreader/internal/settings"
)

// SettingsHandler отдаёт и меняет настройки администратора.
type SettingsHandler struct{ limit *settings.SuggestionLimit }

func NewSettingsHandler(limit *settings.SuggestionLimit) *SettingsHandler {
	return &SettingsHandler{limit: limit}
}

func (h *SettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	value, err := h.limit.Fresh(r.Context())
	if err != nil {
		log.Printf("read site settings: %v", err)
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать настройки")
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]int{"suggestions_per_hour": value})
}

func (h *SettingsHandler) Patch(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		SuggestionsPerHour *int64 `json:"suggestions_per_hour"`
	}
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Укажите положительное целое число предложений за час")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF || input.SuggestionsPerHour == nil || *input.SuggestionsPerHour < 1 || *input.SuggestionsPerHour > 2147483647 {
		writeError(w, http.StatusBadRequest, "Укажите положительное целое число предложений за час")
		return
	}
	value := int(*input.SuggestionsPerHour)
	if err := h.limit.Set(r.Context(), value); err != nil {
		log.Printf("update site settings: %v", err)
		writeError(w, http.StatusInternalServerError, "Не удалось сохранить настройки")
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]int{"suggestions_per_hour": value})
}
