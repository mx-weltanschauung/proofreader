package seo

import (
	"context"
	"encoding/xml"
	"net/http"
	"strings"
	"testing"
	"time"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

type fakeJournals []models.JournalSummary

func (f fakeJournals) List(_ context.Context, includeClosed bool) ([]models.JournalSummary, error) {
	var out []models.JournalSummary
	for _, journal := range f {
		if includeClosed || !journal.Closed {
			out = append(out, journal)
		}
	}
	return out, nil
}

func (f fakeJournals) GetBySlug(_ context.Context, slug string, includeClosed bool) (*models.Journal, error) {
	for _, journal := range f {
		if journal.Slug == slug && (includeClosed || !journal.Closed) {
			return &journal.Journal, nil
		}
	}
	return nil, repository.ErrJournalNotFound
}

func TestJournalSEO(t *testing.T) {
	src := indexSource()
	src.Journals = fakeJournals{
		{Journal: models.Journal{
			Slug: "open-journal", Title: "Открытый журнал",
			Subtitle: "Подзаголовок", Description: "Описание <журнала>",
			UpdatedAt: time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC),
		}, IssuesTotal: 1},
		{Journal: models.Journal{Slug: "hidden-journal", Title: "Закрытый журнал", Closed: true}},
	}
	h := handlerFor(src)
	home := get(t, h, "/", nil)
	if home.Code != http.StatusOK || !strings.Contains(home.Body.String(), `href="/journals/open-journal">Открытый журнал</a>`) {
		t.Fatalf("главная не ссылается на открытый журнал: %d, %s", home.Code, home.Body)
	}
	if strings.Contains(home.Body.String(), "hidden-journal") {
		t.Fatal("главная ссылается на закрытый журнал")
	}

	sitemap := fetch(t, h, "/sitemap-journals.xml", h.Sitemap)
	var urls urlSet
	if err := xml.Unmarshal(sitemap.Body.Bytes(), &urls); err != nil {
		t.Fatalf("карта журналов: %d, %v", sitemap.Code, err)
	}
	const canonical = "https://lib.example.org/journals/open-journal"
	if len(urls.URLs) != 1 || urls.URLs[0].Loc != canonical || urls.URLs[0].LastMod != "2026-11-25" {
		t.Fatalf("карта журналов: %+v", urls.URLs)
	}

	page := get(t, h, strings.TrimPrefix(urls.URLs[0].Loc, src.BaseURL), nil)
	if page.Code != http.StatusOK {
		t.Fatalf("адрес из карты отвечает %d: %s", page.Code, page.Body)
	}
	for _, want := range []string{
		"<title>Открытый журнал",
		"<h1>Открытый журнал</h1>",
		"<p>Подзаголовок</p>",
		"<p>Описание &lt;журнала&gt;</p>",
		`<meta name="robots" content="index,follow">`,
		`<link rel="canonical" href="` + canonical + `">`,
	} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("в карточке нет %q", want)
		}
	}

	for _, path := range []string{"/journals/hidden-journal", "/journals/unknown"} {
		if rec := get(t, h, path, nil); rec.Code != http.StatusGone {
			t.Errorf("%s: %d, ожидался 410", path, rec.Code)
		}
	}
}
