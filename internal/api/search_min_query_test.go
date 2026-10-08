package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"proofreader/internal/models"
)

// Порог в два символа. Одна кириллическая буква — это ДВА байта и ОДИН
// символ; проверка по len(string) пропустила бы её, и «и» ушло бы в скан
// почти всего корпуса (49 тыс. полос из 50 810). Поэтому тест берёт именно
// кириллицу: на латинице разница между байтами и рунами не видна.
func TestSearchHandler_TooShortQueryIs400(t *testing.T) {
	cases := []struct {
		url string
		fn  func(*SearchHandler) http.HandlerFunc
	}{
		{"/api/search?q=и", func(h *SearchHandler) http.HandlerFunc { return h.Search }},
		{"/api/search?q=и&terms_only=1", func(h *SearchHandler) http.HandlerFunc { return h.Search }},
		{"/api/search/pages?q=я&work_id=1", func(h *SearchHandler) http.HandlerFunc { return h.Pages }},
	}
	for _, c := range cases {
		store := &fakeSearchStore{
			terms:  []string{"и"},
			result: &models.SearchResult{Query: "и", Terms: []string{"и"}},
			pages:  &models.SearchPagesResult{Query: "я", Terms: []string{"я"}},
		}
		h := NewSearchHandler(store)
		rec := doSearch(t, h, c.url, c.fn(h))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: code %d, body %s", c.url, rec.Code, rec.Body.String())
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["message"] == "" {
			t.Errorf("%s: ошибка не в форме {message}: %s", c.url, rec.Body.String())
		}
		// Короткий запрос не должен доходить до хранилища: весь смысл порога
		// в том, чтобы скана не было.
		if store.lastQuery.Text != "" || store.searchCalls != 0 {
			t.Errorf("%s: запрос дошёл до хранилища (%q, вызовов Search %d)",
				c.url, store.lastQuery.Text, store.searchCalls)
		}
	}
}

// Ровно два символа — уже запрос, а не отказ: порог «не меньше двух».
func TestSearchHandler_TwoRuneQueryPasses(t *testing.T) {
	store := &fakeSearchStore{
		terms:  []string{"на"},
		result: &models.SearchResult{Query: "на", Terms: []string{"на"}},
	}
	h := NewSearchHandler(store)
	rec := doSearch(t, h, "/api/search?q=на", h.Search)
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d, body %s", rec.Code, rec.Body.String())
	}
	if store.searchCalls != 1 {
		t.Errorf("Search вызван %d раз", store.searchCalls)
	}
}
