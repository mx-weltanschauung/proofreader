package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/internal/seo"
	"proofreader/internal/staticsite"
	"proofreader/pkg/book"
	"proofreader/pkg/markdown"
)

// StaticSource — источник статической читальни (internal/staticsite): те же
// репозитории и сборщики, что у выгрузок и краулера, сведённые к типам
// генератора. Живёт здесь, а не в staticsite: резолв адресов указателя,
// подписи пунктов подборки и одобренная редакция разбора — внутренности
// этого пакета, и вторая их копия разошлась бы с первой.

type StaticEditions interface {
	ShelfEditions
	ListAllHighlights(ctx context.Context) ([]models.EditionHighlight, error)
}

type StaticWorks interface {
	ShelfWorks
	GetByID(ctx context.Context, id int64) (*models.Work, error)
	ListChildren(ctx context.Context, parentID int64) ([]*models.Work, error)
}

// StaticBooks — DownloadSource.WithChapterAnchors().
type StaticBooks interface {
	Work(ctx context.Context, workID int64) (*book.Book, error)
}

type StaticPages interface {
	ListPageMap(ctx context.Context, workID int64) ([]models.PageMapEntry, error)
}

type StaticIndex interface {
	ConceptIndex
	ArticleLinks(ctx context.Context, articleID int64) ([]repository.LinkWithTarget, error)
}

type StaticConceptShelf interface {
	ConceptShelf(ctx context.Context) ([]repository.ConceptShelfRow, error)
}

type StaticCollections interface {
	List(ctx context.Context) ([]*models.Collection, error)
	ItemRows(ctx context.Context, collectionID int64) ([]repository.ItemRow, error)
}

// StaticDocuments — *SEODocumentSource: он уже сводит разбор к одобренной
// редакции (documentForViewer) и собирает тело через ForUntrustedAuthor.
type StaticDocuments interface {
	ListPublished(ctx context.Context, limit, offset int) ([]*models.Document, error)
	Page(ctx context.Context, nickname, slug string) (*seo.DocumentPage, error)
}

type StaticSource struct {
	editions    StaticEditions
	works       StaticWorks
	books       StaticBooks
	pages       StaticPages
	index       StaticIndex
	concepts    StaticConceptShelf
	collections StaticCollections
	documents   StaticDocuments
	renderer    *markdown.Renderer
	volumeMaps  map[int64]map[volumeKey]models.VolumeLocation
}

func NewStaticSource(
	editions StaticEditions, works StaticWorks, books StaticBooks, pages StaticPages,
	index StaticIndex, concepts StaticConceptShelf, collections StaticCollections,
	documents StaticDocuments, renderer *markdown.Renderer,
) *StaticSource {
	return &StaticSource{
		editions: editions, works: works, books: books, pages: pages, index: index,
		concepts: concepts, collections: collections, documents: documents, renderer: renderer,
		volumeMaps: map[int64]map[volumeKey]models.VolumeLocation{},
	}
}

func (s *StaticSource) Shelf(ctx context.Context) (*models.Shelf, error) {
	editions, err := s.editions.List(ctx)
	if err != nil {
		return nil, err
	}
	summaries, err := s.editions.ListAllWorkSummaries(ctx)
	if err != nil {
		return nil, err
	}
	loose, err := s.works.ListWithoutEdition(ctx)
	if err != nil {
		return nil, err
	}
	return &models.Shelf{Editions: groupByEdition(editions, summaries), LooseWorks: loose}, nil
}

func (s *StaticSource) Highlights(ctx context.Context) ([]models.EditionHighlight, error) {
	return s.editions.ListAllHighlights(ctx)
}

func (s *StaticSource) Work(ctx context.Context, id int64) (*models.Work, error) {
	return s.works.GetByID(ctx, id)
}

func (s *StaticSource) FrontMatter(ctx context.Context, volumeID int64) ([]*models.Work, error) {
	return s.works.ListChildren(ctx, volumeID)
}

// Volume — работа книгой. ErrNotFound у DownloadSource.Work значит и «нет
// полос», и «работу не прочли» (любая ошибка GetByID сводится там к
// ErrNotFound), а сборка обязана падать громко — поэтому пустоту
// подтверждаем отдельным запросом карты полос.
func (s *StaticSource) Volume(ctx context.Context, workID int64) (*book.Book, error) {
	b, err := s.books.Work(ctx, workID)
	if err == nil {
		return b, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	pageMap, perr := s.pages.ListPageMap(ctx, workID)
	if perr != nil {
		return nil, fmt.Errorf("%w (карта полос: %v)", err, perr)
	}
	if len(pageMap) == 0 {
		return nil, nil
	}
	return nil, err
}

func (s *StaticSource) ConceptList(ctx context.Context) ([]staticsite.ConceptEntry, error) {
	rows, err := s.concepts.ConceptShelf(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]staticsite.ConceptEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, staticsite.ConceptEntry{Slug: r.Slug, Title: r.Title, Places: r.Places})
	}
	return out, nil
}

func (s *StaticSource) volumeMap(ctx context.Context, editionID int64) (map[volumeKey]models.VolumeLocation, error) {
	if m, ok := s.volumeMaps[editionID]; ok {
		return m, nil
	}
	locs, err := s.index.VolumeMap(ctx, editionID)
	if err != nil {
		return nil, err
	}
	m := buildVolumeMap(locs)
	s.volumeMaps[editionID] = m
	return m, nil
}

func (s *StaticSource) Concept(ctx context.Context, slug string) (*staticsite.Concept, error) {
	c, err := s.index.GetConceptBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	out := &staticsite.Concept{Title: c.Title}
	for _, a := range c.Articles {
		vols, err := s.volumeMap(ctx, a.EditionID)
		if err != nil {
			return nil, fmt.Errorf("карта томов издания %d: %w", a.EditionID, err)
		}
		art := staticsite.ConceptArticle{Edition: a.EditionTitle}
		if a.WorkID != nil {
			art.WorkID = *a.WorkID
		}
		if strings.TrimSpace(a.ArticleMarkdown) != "" {
			art.HTML = s.renderer.Render(a.ArticleMarkdown)
		}
		for _, ref := range a.References {
			path := ref.RubricPath
			if len(path) == 0 && ref.Rubric != "" {
				// Марксов разборщик вложенности не знает и шлёт только Rubric
				// (см. комментарий у models.IndexReference.RubricPath).
				path = []string{ref.Rubric}
			}
			r := staticsite.ConceptRef{
				Path:      path,
				Label:     referenceVolume(ref) + ", с. " + printedSpan(ref.PageStart, ref.PageEnd),
				Uncertain: ref.IsUncertain,
				Note:      ref.Note,
			}
			if workID, page, ok := resolveReference(ref, vols); ok {
				r.WorkID, r.Page = workID, page
			}
			art.Refs = append(art.Refs, r)
		}
		links, err := s.index.ArticleLinks(ctx, a.ID)
		if err != nil {
			return nil, fmt.Errorf("отсылки статьи %d: %w", a.ID, err)
		}
		for _, l := range links {
			art.Links = append(art.Links, staticsite.ConceptLink{Kind: l.Kind, Title: l.TargetTitle, Slug: l.TargetSlug})
		}
		out.Articles = append(out.Articles, art)
	}
	return out, nil
}

// Collections — витрина. CollectionRepository.List уже отбирает только
// сотруднические опубликованные; проверка здесь — вторая страховка: читательская
// подборка живёт по прямой ссылке и в архив, который раздают всем, не идёт.
func (s *StaticSource) Collections(ctx context.Context) ([]staticsite.Collection, error) {
	list, err := s.collections.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []staticsite.Collection
	for _, c := range list {
		if c.AuthorNickname != "" || c.PublishedAt == nil {
			continue
		}
		rows, err := s.collections.ItemRows(ctx, c.ID)
		if err != nil {
			return nil, fmt.Errorf("состав подборки %q: %w", c.Slug, err)
		}
		col := staticsite.Collection{ID: c.ID, Slug: c.Slug, Title: c.Title, Description: c.Description}
		for _, row := range rows {
			it := staticsite.CollectionItem{Title: itemLabel(row), Author: itemAuthor(row)}
			if row.Item.WorkID != nil {
				it.WorkID = *row.Item.WorkID
			}
			if row.Item.Kind == models.CollectionItemKindChapter {
				if row.Item.ChapterID != nil {
					it.ChapterID = *row.Item.ChapterID
				}
				if row.ChapterStartPage != nil {
					it.StartPage = *row.ChapterStartPage
				}
				it.Broken = it.ChapterID == 0 || it.WorkID == 0
			} else {
				it.Broken = it.WorkID == 0
			}
			col.Items = append(col.Items, it)
		}
		out = append(out, col)
	}
	return out, nil
}

const staticDocumentBatch = 100

func (s *StaticSource) Documents(ctx context.Context) ([]staticsite.Document, error) {
	var out []staticsite.Document
	for offset := 0; ; offset += staticDocumentBatch {
		docs, err := s.documents.ListPublished(ctx, staticDocumentBatch, offset)
		if err != nil {
			return nil, err
		}
		for _, d := range docs {
			page, err := s.documents.Page(ctx, d.AuthorNickname, d.Slug)
			if err != nil {
				return nil, fmt.Errorf("разбор %q: %w", d.Slug, err)
			}
			out = append(out, staticsite.Document{
				ID: d.ID, Slug: d.Slug, Title: page.Document.Title,
				Author: d.AuthorNickname, BodyHTML: page.BodyHTML,
			})
		}
		if len(docs) < staticDocumentBatch {
			return out, nil
		}
	}
}
