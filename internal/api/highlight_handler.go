package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

const (
	// maxHighlights — потолок списка: на карточке показывается 3—6 работ,
	// восемь — запас, а не приглашение к витрине из двадцати.
	maxHighlights = 8
	// highlightLabelLimit — в рунах, а не байтах: кириллическая буква занимает
	// два байта, и счёт байтами резал бы подпись вдвое раньше латинской.
	highlightLabelLimit = 80
)

// HighlightHandler — избранные работы собрания: чтение для экрана правки и
// замена списком целиком.
type HighlightHandler struct {
	store HighlightStore
}

// NewHighlightHandler creates a new highlight handler.
func NewHighlightHandler(store HighlightStore) *HighlightHandler {
	return &HighlightHandler{store: store}
}

// editionFromPath отвечает 404 сам и возвращает false, если собрания нет.
// Любая ошибка GetByID — «не найдено», как в EditionHandler.Get.
func (h *HighlightHandler) editionFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Неверный номер собрания")
		return 0, false
	}
	if edition, err := h.store.GetByID(r.Context(), id); err != nil || edition == nil {
		writeError(w, http.StatusNotFound, "Собрание не найдено")
		return 0, false
	}
	return id, true
}

// Get returns the edition's highlights in order. Public, like every read.
func (h *HighlightHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := h.editionFromPath(w, r)
	if !ok {
		return
	}
	list, err := h.store.ListHighlights(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать избранное")
		return
	}
	if list == nil {
		list = []models.EditionHighlight{}
	}
	writeJSONStatus(w, http.StatusOK, list)
}

// Replace заменяет избранное собрания присланным списком и отдаёт новый.
func (h *HighlightHandler) Replace(w http.ResponseWriter, r *http.Request) {
	id, ok := h.editionFromPath(w, r)
	if !ok {
		return
	}
	var items []models.HighlightInput
	// Литерал null декодируется без ошибки в nil-срез и молча снёс бы витрину:
	// снять её можно только явным «[]».
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil || items == nil {
		writeError(w, http.StatusBadRequest, "Тело запроса — список пунктов избранного")
		return
	}
	items, refusal := normalizeHighlights(items)
	if refusal != "" {
		writeError(w, http.StatusBadRequest, refusal)
		return
	}
	if err := h.store.ReplaceHighlights(r.Context(), id, items); err != nil {
		if errors.Is(err, repository.ErrHighlightForeignChapter) {
			writeError(w, http.StatusBadRequest, "В списке есть глава не из этого собрания")
			return
		}
		writeError(w, http.StatusInternalServerError, "Не удалось сохранить избранное")
		return
	}
	h.Get(w, r)
}

// normalizeHighlights срезает пробелы по краям подписи и проверяет список.
// Подпись из одних пробелов — это «подписи нет»: клиент возьмёт заглавие.
// Отказ возвращается текстом для читателя, а не error: фразы начинаются с
// заглавной, и error с такой строкой линтер (ST1005) не пропустил бы.
func normalizeHighlights(items []models.HighlightInput) ([]models.HighlightInput, string) {
	if len(items) > maxHighlights {
		return nil, fmt.Sprintf("В избранном не больше %d работ, прислано %d", maxHighlights, len(items))
	}
	seen := make(map[int64]bool, len(items))
	out := make([]models.HighlightInput, len(items))
	for i, item := range items {
		if item.ChapterID <= 0 {
			return nil, fmt.Sprintf("Пункт %d: не указана глава", i+1)
		}
		if seen[item.ChapterID] {
			return nil, fmt.Sprintf("Пункт %d: эта глава уже есть в списке", i+1)
		}
		seen[item.ChapterID] = true
		label := strings.TrimSpace(item.Label)
		if n := utf8.RuneCountInString(label); n > highlightLabelLimit {
			return nil, fmt.Sprintf("Пункт %d: подпись длиннее %d знаков (%d)", i+1, highlightLabelLimit, n)
		}
		out[i] = models.HighlightInput{ChapterID: item.ChapterID, Label: label}
	}
	return out, ""
}
