package models

import "testing"

func TestSearchResultFoundCount(t *testing.T) {
	cases := []struct {
		name string
		res  SearchResult
		want int
	}{
		{"пусто", SearchResult{}, 0},
		{"только понятие", SearchResult{Concepts: []SearchConcept{{Slug: "a"}}}, 1},
		{"только глава", SearchResult{Chapters: []SearchChapter{{ID: 1}}}, 1},
		{"только аппарат", SearchResult{Volumes: []SearchVolume{{ApparatusHits: 3}, {ApparatusHits: 2}}}, 5},
		{"текст и понятие", SearchResult{TotalHits: 4, Concepts: []SearchConcept{{Slug: "a"}}}, 5},
	}
	for _, c := range cases {
		if got := c.res.FoundCount(); got != c.want {
			t.Errorf("%s: %d, ожидалось %d", c.name, got, c.want)
		}
	}
}
