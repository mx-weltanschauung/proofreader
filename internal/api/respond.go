package api

import (
	"encoding/json"
	"net/http"
)

// writeJSONStatus writes v as JSON with an explicit status code.
func writeJSONStatus(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError answers with {"message": "…"} instead of plain text.
//
// http.Error пишет text/plain, а фронт (frontend/src/utils/apiError.ts) читает
// текст ошибки как err.response.data.message объекта — строку он отбрасывает и
// показывает запасную фразу. То есть все объяснения обработчиков сейчас теряются
// по дороге. В этой форме отвечают обработчики обращений и читательских
// предложений и PUT /pages/{id}; перевод остальных ~270 вызовов http.Error —
// отдельная задача.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSONStatus(w, status, map[string]string{"message": message})
}
