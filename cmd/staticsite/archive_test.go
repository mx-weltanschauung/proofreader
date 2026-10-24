package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"proofreader/internal/api"
	"proofreader/internal/models"
	"proofreader/internal/repository"
)

// Подставные хранилища для сборки источников архива: проверяется проводка
// закрытого в newArchive, а не логика самих источников.
type archDocs struct{ api.DocumentStore }

func (archDocs) GetByAuthorSlug(context.Context, string, string) (*models.Document, error) {
	now := time.Now()
	return &models.Document{ID: 3, Slug: "r", Title: "Разбор", PublishedTitle: "Разбор", PublishedAt: &now,
		MarkdownContent: `<cut id="1">`, PublishedMarkdown: `<cut id="1">`}, nil
}

type archCuts struct {
	api.DocumentCutStore
	work int64
}

func (c archCuts) ByDocument(context.Context, int64) ([]*models.DocumentCut, error) {
	w := c.work
	return []*models.DocumentCut{{ID: 1, WorkID: &w, DocumentID: 3,
		Anchor: models.Anchor{StartPageID: 10, EndPageID: 10, EndOffset: len("Текст источника")},
		Status: models.CutStatusOK, SourceTitle: "Секретный источник"}}, nil
}

func (c archCuts) PagesOfCut(context.Context, *models.DocumentCut) ([]*models.Page, error) {
	return []*models.Page{{ID: 10, WorkID: c.work, PageNumber: 4, ContentMarkdown: "Текст источника"}}, nil
}

type archWorks struct{ workSource }

func (archWorks) GetByID(_ context.Context, id int64) (*models.Work, error) {
	return &models.Work{ID: id}, nil
}

func archiveWith(cutWork int64, spans *prodSpans, whole map[int64]bool, idx indexSource) *archive {
	return newArchive(archiveInputs{
		Works: archWorks{}, Documents: archDocs{}, Cuts: archCuts{work: cutWork},
		Closed: fakeClosedBase{}, Index: idx,
	}, spans, whole, "https://lib.example.org")
}

func cutPage(t *testing.T, a *archive) string {
	t.Helper()
	page, err := a.Documents.Page(context.Background(), "", "r")
	if err != nil {
		t.Fatal(err)
	}
	return page.BodyHTML
}

func assertCutHidden(t *testing.T, body string) {
	t.Helper()
	if strings.Contains(body, "Текст источника") || strings.Contains(body, "Секретный источник") ||
		!strings.Contains(body, "document-cut--unavailable") {
		t.Errorf("вклейка из закрытого попала в архив: %s", body)
	}
}

func TestArchiveDocumentCutFromOpenPageIsShown(t *testing.T) {
	body := cutPage(t, archiveWith(1, &prodSpans{base: fakeSpans{}}, map[int64]bool{}, nil))
	if !strings.Contains(body, "Текст источника") {
		t.Errorf("контроль: открытая вклейка не показана, проверка ниже ничего не доказывает: %s", body)
	}
}

func TestArchiveDocumentCutFromWholeWorkOutIsUnavailable(t *testing.T) {
	spans := &prodSpans{base: fakeSpans{}}
	assertCutHidden(t, cutPage(t, archiveWith(7, spans, map[int64]bool{7: true}, nil)))
}

func TestArchiveDocumentCutFromProdMissingPageIsUnavailable(t *testing.T) {
	spans := &prodSpans{base: fakeSpans{}, extra: map[int64]map[int]bool{1: {4: true}}}
	assertCutHidden(t, cutPage(t, archiveWith(1, spans, map[int64]bool{}, nil)))
}

func TestArchiveIndexHidesArticleWithSourceMissingOnProd(t *testing.T) {
	w10, w11 := int64(10), int64(11)
	art := func(id int64, work *int64) *models.IndexArticle {
		return &models.IndexArticle{ID: id, EditionID: 1, WorkID: work, SourcePageStart: 1, SourcePageEnd: 3}
	}
	idx := fakeIndex{&models.IndexConcept{Title: "К", Articles: []*models.IndexArticle{art(1, &w10), art(2, &w11)}}}
	spans := &prodSpans{base: fakeSpans{}, extra: map[int64]map[int]bool{10: {2: true}}}
	c, err := archiveWith(1, spans, map[int64]bool{}, idx).Index.GetConceptBySlug(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Articles) != 1 || c.Articles[0].ID != 2 {
		t.Errorf("статьи = %v, хотел одну (id 2)", c.Articles)
	}
}

// archCollections — одна опубликованная сотрудническая подборка из пунктов на
// главах работы 1; ChaptersForWorks несёт действующую закрытость глав.
type archCollections struct {
	collectionSource
	rows     []repository.ItemRow
	chapters []*models.Chapter
}

func (c archCollections) List(context.Context) ([]*models.Collection, error) {
	now := time.Now()
	return []*models.Collection{{ID: 1, Slug: "podborka", Title: "Подборка", PublishedAt: &now}}, nil
}

func (c archCollections) ItemRows(context.Context, int64) ([]repository.ItemRow, error) {
	return c.rows, nil
}

func (c archCollections) ChaptersForWorks(context.Context, []int64) ([]*models.Chapter, error) {
	return c.chapters, nil
}

func archChapterItem(itemID, chapterID int64, title string, start, end int) repository.ItemRow {
	work := int64(1)
	return repository.ItemRow{
		Item: models.CollectionItem{ID: itemID, Kind: models.CollectionItemKindChapter,
			ChapterID: &chapterID, WorkID: &work, SnapshotTitle: title, OrderNumber: int(itemID)},
		ChapterTitle: &title, ChapterStartPage: &start, ChapterEndPage: &end,
	}
}

// Пункт подборки на главе, закрытой локально (через предка) или только на
// боевом (её полос нет в архиве), в архив не идёт: ни ссылкой, ни строкой
// «нет в этой копии» с заглавием речи.
func TestArchiveCollectionsDropClosedChapterItems(t *testing.T) {
	cols := archCollections{
		rows: []repository.ItemRow{
			archChapterItem(1, 20, "Открытая статья", 1, 2),
			archChapterItem(2, 21, "Речь, закрытая предком", 3, 4),
			archChapterItem(3, 22, "Речь, закрытая на боевом", 12, 13),
		},
		chapters: []*models.Chapter{
			{ID: 20, WorkID: 1, Title: "Открытая статья", StartPage: 1, EndPage: 2},
			{ID: 21, WorkID: 1, Title: "Речь, закрытая предком", StartPage: 3, EndPage: 4, ClosedBy: models.ChapterClosedByParent},
			{ID: 22, WorkID: 1, Title: "Речь, закрытая на боевом", StartPage: 12, EndPage: 13},
		},
	}
	spans := &prodSpans{base: fakeSpans{}, extra: map[int64]map[int]bool{1: {12: true, 13: true}}}
	a := newArchive(archiveInputs{
		Works: archWorks{}, Documents: archDocs{}, Cuts: archCuts{work: 1},
		Closed: fakeClosedBase{}, Collections: cols,
	}, spans, map[int64]bool{}, "https://lib.example.org")
	got, err := a.Static.Collections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("подборок %d, ждали 1", len(got))
	}
	var titles []string
	for _, it := range got[0].Items {
		titles = append(titles, it.Title)
	}
	if len(titles) != 1 || titles[0] != "Открытая статья" {
		t.Errorf("пункты архива = %q, ждали только открытую статью", titles)
	}
}
