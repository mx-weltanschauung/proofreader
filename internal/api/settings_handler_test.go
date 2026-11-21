package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"proofreader/internal/settings"
)

type fakeSiteSettingsStore struct {
	value int
	err   error
}

func (s *fakeSiteSettingsStore) SuggestionLimit(context.Context) (int, error) { return s.value, s.err }
func (s *fakeSiteSettingsStore) SetSuggestionLimit(_ context.Context, value int) error {
	if s.err != nil {
		return s.err
	}
	s.value = value
	return nil
}

func TestSettingsGetAndPatch(t *testing.T) {
	store := &fakeSiteSettingsStore{value: 5}
	h := NewSettingsHandler(settings.NewSuggestionLimit(store, time.Minute))
	get := httptest.NewRecorder()
	h.Get(get, httptest.NewRequest(http.MethodGet, "/api/admin/settings", nil))
	var body struct {
		SuggestionsPerHour int `json:"suggestions_per_hour"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if get.Code != http.StatusOK || body.SuggestionsPerHour != 5 {
		t.Fatalf("GET = %d, %+v", get.Code, body)
	}

	patch := httptest.NewRecorder()
	h.Patch(patch, httptest.NewRequest(http.MethodPatch, "/api/admin/settings", strings.NewReader(`{"suggestions_per_hour":7}`)))
	if err := json.Unmarshal(patch.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if patch.Code != http.StatusOK || body.SuggestionsPerHour != 7 || store.value != 7 {
		t.Fatalf("PATCH = %d, %+v, stored %d", patch.Code, body, store.value)
	}
}

func TestSettingsPatchRejectsInvalidValues(t *testing.T) {
	cases := []string{
		`{"suggestions_per_hour":0}`,
		`{"suggestions_per_hour":-1}`,
		`{"suggestions_per_hour":1.5}`,
		`{}`,
		`{"suggestions_per_hour":2147483648}`,
		`{"suggestions_per_hour":5,"other":1}`,
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			store := &fakeSiteSettingsStore{value: 5}
			h := NewSettingsHandler(settings.NewSuggestionLimit(store, time.Minute))
			rec := httptest.NewRecorder()
			h.Patch(rec, httptest.NewRequest(http.MethodPatch, "/api/admin/settings", strings.NewReader(raw)))
			if rec.Code != http.StatusBadRequest || store.value != 5 {
				t.Fatalf("PATCH %s = %d, stored %d", raw, rec.Code, store.value)
			}
		})
	}
}

func TestSettingsDatabaseError(t *testing.T) {
	h := NewSettingsHandler(settings.NewSuggestionLimit(&fakeSiteSettingsStore{value: 5, err: errors.New("db down")}, time.Minute))
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/admin/settings", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("GET = %d", rec.Code)
	}
}
