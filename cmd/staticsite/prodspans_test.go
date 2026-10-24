package main

import (
	"context"
	"reflect"
	"testing"

	"proofreader/internal/models"
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
