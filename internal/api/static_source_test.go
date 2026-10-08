package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/internal/seo"
	"proofreader/internal/staticsite"
	"proofreader/pkg/book"
	"proofreader/pkg/markdown"
)

var _ staticsite.Source = (*StaticSource)(nil)

type staticBooksFake struct{ err error }

func (f staticBooksFake) Work(context.Context, int64) (*book.Book, error) { return nil, f.err }

type staticPagesFake struct{ n int }

func (f staticPagesFake) ListPageMap(context.Context, int64) ([]models.PageMapEntry, error) {
	return make([]models.PageMapEntry, f.n), nil
}

type staticIndexFake struct {
	concept *models.IndexConcept
	vols    []models.VolumeLocation
	links   []repository.LinkWithTarget
}

func (f staticIndexFake) GetConceptBySlug(context.Context, string) (*models.IndexConcept, error) {
	return f.concept, nil
}
func (f staticIndexFake) VolumeMap(context.Context, int64) ([]models.VolumeLocation, error) {
	return f.vols, nil
}
func (f staticIndexFake) ArticleLinks(context.Context, int64) ([]repository.LinkWithTarget, error) {
	return f.links, nil
}

type staticCollectionsFake struct {
	list []*models.Collection
	rows []repository.ItemRow
}

func (f staticCollectionsFake) List(context.Context) ([]*models.Collection, error) {
	return f.list, nil
}
func (f staticCollectionsFake) ItemRows(context.Context, int64) ([]repository.ItemRow, error) {
	return f.rows, nil
}

type staticDocumentsFake struct{ total int }

func (f staticDocumentsFake) ListPublished(_ context.Context, limit, offset int) ([]*models.Document, error) {
	var out []*models.Document
	for i := offset; i < f.total && i < offset+limit; i++ {
		out = append(out, &models.Document{ID: int64(i + 1), Slug: fmt.Sprintf("d%d", i+1), Title: "черновое"})
	}
	return out, nil
}
func (f staticDocumentsFake) Page(_ context.Context, nickname, slug string) (*seo.DocumentPage, error) {
	return &seo.DocumentPage{Document: &models.Document{Title: "Одобренное " + slug}, BodyHTML: "<p>" + slug + "</p>"}, nil
}

func newStaticSourceForTest(books StaticBooks, pages StaticPages, index StaticIndex, cols StaticCollections, docs StaticDocuments) *StaticSource {
	return NewStaticSource(nil, nil, books, pages, index, nil, cols, docs, markdown.NewRenderer())
}

func TestStaticSourceVolumeWithoutPagesIsNil(t *testing.T) {
	s := newStaticSourceForTest(staticBooksFake{err: fmt.Errorf("%w: в работе 5 нет страниц", ErrNotFound)}, staticPagesFake{n: 0}, nil, nil, nil)
	b, err := s.Volume(context.Background(), 5)
	if err != nil || b != nil {
		t.Fatalf("Volume = %v, %v; хотел nil, nil", b, err)
	}
}

func TestStaticSourceVolumeNotFoundWithPagesFails(t *testing.T) {
	s := newStaticSourceForTest(staticBooksFake{err: fmt.Errorf("%w: работа 5", ErrNotFound)}, staticPagesFake{n: 3}, nil, nil, nil)
	if _, err := s.Volume(context.Background(), 5); err == nil {
		t.Fatal("работа с полосами, которую не удалось собрать, тихо пропала бы из архива")
	}
}

func TestStaticSourceVolumePassesOtherErrors(t *testing.T) {
	boom := errors.New("база упала")
	s := newStaticSourceForTest(staticBooksFake{err: boom}, staticPagesFake{n: 0}, nil, nil, nil)
	if _, err := s.Volume(context.Background(), 5); !errors.Is(err, boom) {
		t.Fatalf("ошибка = %v, хотел %v", err, boom)
	}
}

func TestStaticSourceConceptResolvesReferences(t *testing.T) {
	indexWork := int64(118)
	index := staticIndexFake{
		concept: &models.IndexConcept{Title: "Диалектика", Articles: []*models.IndexArticle{{
			ID: 1, EditionID: 7, EditionTitle: "Сочинения", WorkID: &indexWork, ArticleMarkdown: "**Диалектика** — метод.",
			References: []*models.IndexReference{
				{VolumeNumber: 1, PageStart: 105, PageEnd: 106, RubricPath: []string{"Метод"}, IsUncertain: true},
				{VolumeNumber: 9, PageStart: 3, PageEnd: 3, Rubric: "Старое", Note: "в черновике"},
			},
		}}},
		vols:  []models.VolumeLocation{{VolumeNumber: 1, WorkID: 100, PageOffset: 100, MaxPage: 50}},
		links: []repository.LinkWithTarget{{IndexConceptLink: models.IndexConceptLink{Kind: "see", TargetTitle: "Анархизм"}, TargetSlug: "anarhizm"}},
	}
	s := newStaticSourceForTest(nil, nil, index, nil, nil)
	c, err := s.Concept(context.Background(), "dialektika")
	if err != nil {
		t.Fatal(err)
	}
	a := c.Articles[0]
	if a.Edition != "Сочинения" || a.WorkID != 118 || !strings.Contains(a.HTML, "<strong>Диалектика</strong>") {
		t.Errorf("статья: %+v", a)
	}
	want0 := staticsite.ConceptRef{Path: []string{"Метод"}, Label: "т. 1, с. 105—106", WorkID: 100, Page: 5, Uncertain: true}
	if fmt.Sprint(a.Refs[0]) != fmt.Sprint(want0) {
		t.Errorf("адрес 1 = %+v, хотел %+v", a.Refs[0], want0)
	}
	want1 := staticsite.ConceptRef{Path: []string{"Старое"}, Label: "т. 9, с. 3", Note: "в черновике"}
	if fmt.Sprint(a.Refs[1]) != fmt.Sprint(want1) {
		t.Errorf("адрес 2 = %+v, хотел %+v", a.Refs[1], want1)
	}
	if len(a.Links) != 1 || a.Links[0] != (staticsite.ConceptLink{Kind: "see", Title: "Анархизм", Slug: "anarhizm"}) {
		t.Errorf("отсылки: %+v", a.Links)
	}
}

func TestStaticSourceCollectionsMapsItems(t *testing.T) {
	published := timePtrForStatic()
	cols := staticCollectionsFake{
		list: []*models.Collection{
			{ID: 5, Slug: "nachala", Title: "Начала", PublishedAt: published},
			{ID: 6, Slug: "moya", Title: "Моя", AuthorNickname: "читатель", PublishedAt: published},
		},
		rows: []repository.ItemRow{
			{Item: models.CollectionItem{Kind: models.CollectionItemKindChapter, ChapterID: int64Ptr(11), WorkID: int64Ptr(100), SnapshotTitle: "Метод"},
				ChapterStartPage: intPtr(3)},
			{Item: models.CollectionItem{Kind: models.CollectionItemKindWork, WorkID: int64Ptr(100)}, WorkTitle: strPtr("Том 1")},
			{Item: models.CollectionItem{Kind: models.CollectionItemKindChapter, WorkID: int64Ptr(100), SnapshotTitle: "Снесённая"}},
		},
	}
	s := newStaticSourceForTest(nil, nil, nil, cols, nil)
	got, err := s.Collections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != 5 {
		t.Fatalf("читательская подборка попала в архив: %+v", got)
	}
	items := got[0].Items
	want := []staticsite.CollectionItem{
		{Title: "Метод", WorkID: 100, ChapterID: 11, StartPage: 3},
		{Title: "Том 1", WorkID: 100},
		{Title: "Снесённая", WorkID: 100, Broken: true},
	}
	if fmt.Sprint(items) != fmt.Sprint(want) {
		t.Errorf("пункты = %+v\nхотел %+v", items, want)
	}
}

func TestStaticSourceDocumentsPagesThroughAndShowsApproved(t *testing.T) {
	s := newStaticSourceForTest(nil, nil, nil, nil, staticDocumentsFake{total: 101})
	got, err := s.Documents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 101 {
		t.Fatalf("разборов %d, хотел 101 — постраничный обход оборвался", len(got))
	}
	if got[0].Title != "Одобренное d1" || got[0].BodyHTML != "<p>d1</p>" {
		t.Errorf("разбор взят не из одобренной редакции: %+v", got[0])
	}
}

func timePtrForStatic() *time.Time { t := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC); return &t }
