package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"proofreader/internal/api"
	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/internal/seo"
)

type fakeSpans struct{}

func (fakeSpans) ClosedSpans(context.Context, int64) ([]models.PageSpan, error) {
	return []models.PageSpan{{Start: 8, End: 9}}, nil
}
func (fakeSpans) ClosedChapterIDs(context.Context, int64) (map[int64]bool, error) {
	return map[int64]bool{5: true}, nil
}

// Диапазоны для обёрток зрителя — настоящие закрытые плюс полосы, которых нет
// на боевом, по порядку начала; у работы без добавки — как у основания.
func TestProdSpansUnionsLocalAndProd(t *testing.T) {
	s := &prodSpans{base: fakeSpans{}, extra: map[int64]map[int]bool{1: {2: true, 3: true, 12: true}}}
	got, err := s.ClosedSpans(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []models.PageSpan{{Start: 2, End: 3}, {Start: 8, End: 9}, {Start: 12, End: 12}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ClosedSpans = %v, хотел %v", got, want)
	}
	got, _ = s.ClosedSpans(context.Background(), 2)
	if !reflect.DeepEqual(got, []models.PageSpan{{Start: 8, End: 9}}) {
		t.Errorf("работа без добавки: %v", got)
	}
	if ids, _ := s.ClosedChapterIDs(context.Background(), 1); !ids[5] {
		t.Error("ClosedChapterIDs потерял основание")
	}
}

type fakeClosedBase struct{ fakeSpans }

func (fakeClosedBase) ClosedBy(context.Context, int64) (string, error) { return "", nil }
func (fakeClosedBase) ClosedIDs(context.Context) (map[int64]bool, error) {
	return map[int64]bool{}, nil
}
func (fakeClosedBase) ClosedEditionIDs(context.Context) (map[int64]bool, error) {
	return map[int64]bool{}, nil
}
func (fakeClosedBase) WorksWithClosedChapters(context.Context) (map[int64]bool, error) {
	return map[int64]bool{}, nil
}

type fakeIndex struct{ concept *models.IndexConcept }

func (f fakeIndex) GetConceptBySlug(context.Context, string) (*models.IndexConcept, error) {
	return f.concept, nil
}
func (fakeIndex) VolumeMap(context.Context, int64) ([]models.VolumeLocation, error) {
	return nil, nil
}
func (fakeIndex) HiddenConceptSlugs(context.Context, []string) (map[string]bool, error) {
	return map[string]bool{}, nil
}
func (fakeIndex) ArticleLinks(context.Context, int64) ([]repository.LinkWithTarget, error) {
	return nil, nil
}
func (fakeIndex) ListConcepts(context.Context, string, string, int, int, bool) ([]*models.IndexConcept, error) {
	return nil, nil
}

// Статья указателя, чей источник накрыт полосой, закрытой на боевом (у нас
// она ещё открыта), в архив не идёт; понятие без других статей скрыто.
func TestIndexHidesArticleWithSourceClosedOnProd(t *testing.T) {
	w10, w11 := int64(10), int64(11)
	art := func(id int64, work *int64, from, to int) *models.IndexArticle {
		return &models.IndexArticle{ID: id, EditionID: 1, WorkID: work, SourcePageStart: from, SourcePageEnd: to}
	}
	spans := &prodSpans{base: fakeSpans{}, extra: map[int64]map[int]bool{10: {2: true}}}
	idx := api.NewTokenlessIndex(fakeIndex{&models.IndexConcept{Title: "К",
		Articles: []*models.IndexArticle{art(1, &w10, 1, 3), art(2, &w11, 1, 3)}}}, &prodClosed{closedBase: fakeClosedBase{}, spans: spans})
	c, err := idx.GetConceptBySlug(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Articles) != 1 || c.Articles[0].ID != 2 {
		t.Errorf("статьи = %v, хотел одну (id 2)", c.Articles)
	}

	idx = api.NewTokenlessIndex(fakeIndex{&models.IndexConcept{Title: "К",
		Articles: []*models.IndexArticle{art(1, &w10, 1, 3)}}}, &prodClosed{closedBase: fakeClosedBase{}, spans: spans})
	if _, err := idx.GetConceptBySlug(context.Background(), "k"); !errors.Is(err, seo.ErrNotFound) {
		t.Errorf("понятие с единственной закрытой статьёй отвечает %v, хотел скрытое", err)
	}
}
