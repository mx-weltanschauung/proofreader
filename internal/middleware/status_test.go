package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatusWriterRecordsCode(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewStatusWriter(rec)
	w.WriteHeader(http.StatusTeapot)
	if w.Status != http.StatusTeapot {
		t.Fatalf("Status=%d", w.Status)
	}
	w2 := NewStatusWriter(httptest.NewRecorder())
	w2.Write([]byte("x"))
	if w2.Status != http.StatusOK {
		t.Fatalf("неявный код: %d", w2.Status)
	}
	if w2.Unwrap() == nil {
		t.Fatal("Unwrap пуст")
	}
}
