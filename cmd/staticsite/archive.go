package main

import (
	"context"

	"proofreader/internal/api"
	"proofreader/internal/models"
	"proofreader/pkg/markdown"
)

// workSource, editionSource и indexSource — срезы репозиториев, которые
// api принимает неэкспортированными интерфейсами; здесь они повторены, чтобы
// сборку источников можно было проверить на подставных хранилищах.
type workSource interface {
	GetByID(ctx context.Context, id int64) (*models.Work, error)
	List(ctx context.Context, limit, offset int, status *models.WorkStatus, ownerID *int64) ([]*models.Work, error)
	ListChildren(ctx context.Context, parentID int64) ([]*models.Work, error)
	ListWithoutEdition(ctx context.Context) ([]models.ShelfWork, error)
}

type editionSource interface {
	GetByID(ctx context.Context, id int64) (*models.Edition, error)
	List(ctx context.Context) ([]*models.Edition, error)
	ListWorks(ctx context.Context, editionID int64) ([]*models.Work, error)
	ListWorkSummaries(ctx context.Context, editionID int64) ([]*models.VolumeSummary, error)
	ListAllWorkSummaries(ctx context.Context) ([]*models.VolumeSummary, error)
	ListAllHighlights(ctx context.Context) ([]models.EditionHighlight, error)
}

type indexSource interface {
	api.StaticIndex
	HiddenConceptSlugs(ctx context.Context, slugs []string) (map[string]bool, error)
	GetConceptBySlug(ctx context.Context, slug string) (*models.IndexConcept, error)
	VolumeMap(ctx context.Context, editionID int64) ([]models.VolumeLocation, error)
	ListConcepts(ctx context.Context, query, letter string, limit, offset int, includeClosed bool) ([]*models.IndexConcept, error)
}

type collectionSource interface {
	api.CollectionStore
	api.StaticCollections
}

// archiveInputs — хранилища, из которых строятся источники архива.
type archiveInputs struct {
	Works       workSource
	Editions    editionSource
	Chapters    api.ChapterReadStore
	Pages       api.PageStore
	Collections collectionSource
	Index       indexSource
	Concepts    api.StaticConceptShelf
	Documents   api.DocumentStore
	Cuts        api.DocumentCutStore
	Closed      closedBase
}

// archive — источники чтения статической читальни. Все они строятся здесь,
// одним местом: ошибка проводки закрытого (разбор или указатель, которым
// достался только локально закрытый набор) открывает утечку, которую
// самопроверка выпуска не видит, поэтому проводку проверяет тест.
type archive struct {
	Static    *api.StaticSource
	Documents *api.SEODocumentSource
	Index     *api.TokenlessIndex
	Closed    *prodClosed
}

func newArchive(in archiveInputs, spans *prodSpans, whole map[int64]bool, base string) *archive {
	renderer := markdown.NewRenderer()
	// Архив раздают всем, токена у него нет: работы и издания — открытым
	// видом, закрытое для сборки не существует.
	openWorks := api.NewOpenWorks(in.Works, in.Closed)
	openEditions := api.NewOpenEditions(in.Editions, in.Closed)
	// Полосы и главы — той же обёрткой зрителя, что у сайта; закрытые
	// диапазоны — локальные плюс полосы, которых нет на боевом.
	viewPages := api.NewViewPages(in.Pages, spans)
	viewChapters := api.NewViewChapters(in.Chapters, spans)
	// Один набор закрытого — указателю, разборам и подборкам: локально
	// закрытые работы плюс те, которых нет в архиве целиком.
	closed := &prodClosed{closedBase: in.Closed, spans: spans, whole: whole}
	index := api.NewTokenlessIndex(in.Index, closed)
	books := api.NewDownloadSource(openWorks, viewChapters, viewPages, openEditions, in.Collections, renderer, base).WithChapterAnchors()
	documents := api.NewSEODocumentSource(in.Documents, in.Cuts, openWorks, renderer).
		WithClosed(closed).WithClosedSpans(spans)
	static := api.NewStaticSource(openEditions, openWorks, books, viewPages, index, in.Concepts,
		in.Collections, documents, renderer).WithClosed(closed)
	return &archive{Static: static, Documents: documents, Index: index, Closed: closed}
}
