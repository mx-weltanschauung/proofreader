package seo

import (
	"context"
	"errors"
	"strings"

	"proofreader/internal/repository"
	"proofreader/internal/site"
)

func journalPath(slug string) string {
	return "/journals/" + slug
}

// Journal печатает карточку журнала. Номера по-прежнему открываются из
// читательского каталога и по прямым адресам.
func (s *Source) Journal(ctx context.Context, slug string) (*Doc, error) {
	journal, err := s.Journals.GetBySlug(ctx, slug, false)
	if errors.Is(err, repository.ErrJournalNotFound) {
		return nil, notFound("journal %q", slug)
	}
	if err != nil {
		return nil, err
	}

	var body strings.Builder
	body.WriteString("<h1>" + Esc(journal.Title) + "</h1>\n")
	if journal.Subtitle != "" {
		body.WriteString("<p>" + Esc(journal.Subtitle) + "</p>\n")
	}
	if journal.Description != "" {
		body.WriteString("<p>" + Esc(journal.Description) + "</p>\n")
	}

	return &Doc{
		Title:       journal.Title + " — " + site.Name(),
		Description: sentences(journal.Subtitle, Excerpt(Esc(journal.Description), 160)),
		Canonical:   s.abs(journalPath(journal.Slug)),
		OGType:      "website",
		Robots:      RobotsIndex,
		Body:        body.String(),
		CacheKey:    journal.UpdatedAt,
	}, nil
}
