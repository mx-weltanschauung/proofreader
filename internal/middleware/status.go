package middleware

import "net/http"

// StatusWriter запоминает код ответа для тех, кто смотрит на него после
// обработчика (метрики, статистика). Unwrap обязателен: без него
// http.ResponseController не доходит до соединения (см. responseWriter в
// logging.go).
type StatusWriter struct {
	http.ResponseWriter
	Status int
}

func NewStatusWriter(w http.ResponseWriter) *StatusWriter {
	return &StatusWriter{ResponseWriter: w}
}

func (w *StatusWriter) WriteHeader(code int) {
	if w.Status == 0 {
		w.Status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *StatusWriter) Write(b []byte) (int, error) {
	if w.Status == 0 {
		w.Status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *StatusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
