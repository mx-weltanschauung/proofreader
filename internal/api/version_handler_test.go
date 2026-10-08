package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVersionReportsSchemaAndCommit(t *testing.T) {
	h := NewVersionHandler(func(context.Context) (int, bool, error) {
		return 10, false, nil
	}, "abc1234")

	rr := httptest.NewRecorder()
	h.Version(rr, httptest.NewRequest(http.MethodGet, "/api/version", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("код ответа = %d, ожидался 200", rr.Code)
	}
	var got versionResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("тело не разобралось: %v", err)
	}
	if got.SchemaVersion != 10 || got.SchemaDirty || got.Commit != "abc1234" {
		t.Fatalf("ответ = %+v", got)
	}
}

// Недоступная база — это отказ сервера, а не «версия 0»: выкатка не должна
// принять пустой ответ за успешную проверку.
func TestVersionFailsWhenSchemaIsUnreadable(t *testing.T) {
	h := NewVersionHandler(func(context.Context) (int, bool, error) {
		return 0, false, errors.New("нет связи с базой")
	}, "abc1234")

	rr := httptest.NewRecorder()
	h.Version(rr, httptest.NewRequest(http.MethodGet, "/api/version", nil))

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("код ответа = %d, ожидался 500", rr.Code)
	}
}

// Грязная миграция обязана быть видна снаружи: выкатывать поверх неё нельзя.
func TestVersionReportsDirtySchema(t *testing.T) {
	h := NewVersionHandler(func(context.Context) (int, bool, error) {
		return 9, true, nil
	}, "deadbee")

	rr := httptest.NewRecorder()
	h.Version(rr, httptest.NewRequest(http.MethodGet, "/api/version", nil))

	var got versionResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("тело не разобралось: %v", err)
	}
	if !got.SchemaDirty || got.SchemaVersion != 9 {
		t.Fatalf("ответ = %+v", got)
	}
}
