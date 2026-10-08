package opds

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var (
	t0 = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	t1 = time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	t2 = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
)

// Том Сталина: верхний уровень — не работы, а годы («1901–1907»); внутри
// годов — работы-листья. Рядом работа с разделами (как «Манифест»), узел,
// все дети которого — аппарат, глава аппарата с поддеревом и глава без
// полос.
func stalinTree() *Tree {
	v := Volume{
		ID: 252, Title: "И. В. Сталин. Сочинения. Том 1", Author: "И. В. Сталин",
		Year: 1946, EditionID: 7, EditionTitle: "Сочинения в 13 томах", VolumeLabel: "Том 1",
		PageFrom: 1, PageTo: 420, HTMLPath: "/works/252-stalin-t01", Updated: t2,
	}
	leaf := func(id int64, title string, from, to int) *Node {
		return &Node{ID: id, Title: title, PageFrom: from, PageTo: to,
			HTMLPath: fmt.Sprintf("/works/252-stalin-t01/chapters/%d", id), Updated: t1, HasPages: true}
	}
	years := leaf(10, "1901–1907", 3, 372)
	years.Children = []*Node{
		leaf(11, "Как понимает социал-демократия национальный вопрос?", 32, 55),
		leaf(12, "Письмо из Кутаиса", 56, 58),
	}
	manifest := leaf(20, "Манифест", 373, 380)
	manifest.Children = []*Node{leaf(21, "I. Буржуа и пролетарии", 373, 376), leaf(22, "II. Пролетарии и коммунисты", 377, 380)}
	// Все дети — аппарат: узел для каталога — лист.
	withNotes := leaf(30, "Речь с комментарием", 381, 385)
	withNotes.Children = []*Node{{ID: 31, Title: "Примечания", IsApparatus: true, HasPages: true, PageFrom: 384, PageTo: 385}}
	apparatus := &Node{ID: 40, Title: "Примечания", IsApparatus: true, HasPages: true, PageFrom: 386, PageTo: 410,
		Children: []*Node{leaf(41, "К статье «Письмо из Кутаиса»", 386, 390)}}
	empty := &Node{ID: 50, Title: "Без полос", HasPages: false, PageFrom: 500, PageTo: 501}
	return &Tree{Volume: v, Nodes: []*Node{years, manifest, withNotes, apparatus, empty}}
}

// wideTree — том id с узлом «Письма» (id 60) из n детей.
func wideTree(id int64, n int) *Tree {
	t := stalinTree()
	t.Volume.ID = id
	wide := &Node{ID: 60, Title: "Письма", HasPages: true, PageFrom: 1, PageTo: 999, Updated: t1}
	for i := 0; i < n; i++ {
		wide.Children = append(wide.Children, &Node{ID: int64(1000 + i), Title: fmt.Sprintf("Письмо %d", i+1),
			HasPages: true, PageFrom: i + 1, PageTo: i + 1, Updated: t0})
	}
	t.Nodes = append(t.Nodes, wide)
	return t
}

type fakeSource struct {
	shelf     *Shelf
	trees     map[int64]*Tree
	recent    []Volume
	search    *SearchResult
	searchErr error
	err       error // общий сбой: отвечают им все методы
	gotQuery  string
	gotChaps  []bool // запрошены ли главы — по вызову поиска
}

func (f *fakeSource) Shelf(context.Context) (*Shelf, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.shelf, nil
}

func (f *fakeSource) Recent(_ context.Context, limit, offset int) ([]Volume, error) {
	if f.err != nil {
		return nil, f.err
	}
	if offset >= len(f.recent) {
		return nil, nil
	}
	return f.recent[offset:min(offset+limit, len(f.recent))], nil
}

func (f *fakeSource) Tree(_ context.Context, id int64) (*Tree, error) {
	if f.err != nil {
		return nil, f.err
	}
	t, ok := f.trees[id]
	if !ok {
		return nil, ErrNotFound
	}
	return t, nil
}

func (f *fakeSource) Search(_ *http.Request, q string, chapters bool) (*SearchResult, error) {
	f.gotQuery = q
	f.gotChaps = append(f.gotChaps, chapters)
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.search, nil
}

func fixtureSource() *fakeSource {
	tree := stalinTree()
	loose := Volume{ID: 300, Title: "Отдельная книга", Updated: t0, HTMLPath: "/works/300", PageFrom: 1, PageTo: 10}
	return &fakeSource{
		shelf: &Shelf{
			Editions: []Edition{
				{ID: 7, Title: "И. В. Сталин. Сочинения в 13 томах", Volumes: []Volume{tree.Volume}},
				{ID: 8, Title: "Пустое собрание"},
			},
			Loose: []Volume{loose},
		},
		trees:  map[int64]*Tree{252: tree, 253: wideTree(253, 250)},
		recent: []Volume{tree.Volume, loose},
		search: &SearchResult{
			Chapters: []ChapterHit{
				{Tree: tree, ChapterID: 12}, // лист — книга
				{Tree: tree, ChapterID: 20}, // узел — навигация
				{Tree: tree, ChapterID: 41}, // внутри аппарата — выпадает
			},
			Volumes: []VolumeHit{{Volume: tree.Volume, TextHits: 21}},
		},
	}
}

const testBase = "https://lib.example.org"

func serve(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}
