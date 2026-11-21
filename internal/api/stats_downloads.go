package api

import "net/http"

func (h *StatsHandler) Downloads(w http.ResponseWriter, r *http.Request) {
	from, today, ok := h.period(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "days — число от 1 до 365")
		return
	}
	data, err := h.reader.Downloads(r.Context(), from, today, defaultTopLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать выдачи файлов")
		return
	}
	data.Summary = nonNil(data.Summary)
	data.Top = nonNil(data.Top)
	writeJSONStatus(w, http.StatusOK, data)
}
