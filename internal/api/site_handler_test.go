package api

import (
	"encoding/json"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"proofreader/internal/config"
)

func TestSiteHandlerGet(t *testing.T) {
	h := NewSiteHandler(config.SiteConfig{Name: "Тест", Description: "о собрании",
		SupportURL: "https://example.org/give", ChannelURL: "https://example.org/news", AgeRating: "18+", Tagline: "собрания"})
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest("GET", "/api/site", nil))
	if rec.Code != 200 {
		t.Fatalf("код %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=300" {
		t.Fatalf("Cache-Control %q", cc)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"site_name": "Тест", "site_description": "о собрании",
		"support_url": "https://example.org/give", "channel_url": "https://example.org/news", "age_rating": "18+", "site_tagline": "собрания"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}

// Ответ не обещает полей, которыми фронт ничего не делает: контакт для
// правообладателей живёт в instance/legal.html, а не в окружении.
func TestSiteHandlerFieldsAreExactlyUsed(t *testing.T) {
	rec := httptest.NewRecorder()
	NewSiteHandler(config.SiteConfig{Name: "Тест"}).Get(rec, httptest.NewRequest("GET", "/api/site", nil))
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := "age_rating channel_url site_description site_name site_tagline support_url"
	if strings.Join(keys, " ") != want {
		t.Errorf("поля ответа: %v, ждали %s", keys, want)
	}
}

// Пустое имя и обработчик, не подключённый к роутеру (nil), отдают умолчание,
// а не пустую строку и не панику: фронт печатает имя как есть.
func TestSiteHandlerDefaults(t *testing.T) {
	for name, h := range map[string]*SiteHandler{"пустое имя": NewSiteHandler(config.SiteConfig{}), "nil": nil} {
		rec := httptest.NewRecorder()
		h.Get(rec, httptest.NewRequest("GET", "/api/site", nil))
		var got map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if got["site_name"] != config.DefaultSiteName {
			t.Errorf("%s: site_name = %v", name, got["site_name"])
		}
	}
}
