package mcp

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseIDShapes(t *testing.T) {
	cases := []struct {
		id   string
		want Address
	}{
		{"/", Address{Kind: KindCatalog}},
		{"", Address{Kind: KindCatalog}},
		{"/llms.txt", Address{Kind: KindCatalog}},
		{"https://lib.example.org/", Address{Kind: KindCatalog}},
		{"/works/114-lenin-t43", Address{Kind: KindWork, WorkID: 114}},
		{"/works/114.md", Address{Kind: KindWork, WorkID: 114}},
		{"/works/114-lenin-t43/chapters/10807-rech", Address{Kind: KindChapter, WorkID: 114, ChapterID: 10807, Part: 1}},
		{"/works/47/chapters/2066.md", Address{Kind: KindChapter, WorkID: 47, ChapterID: 2066, Part: 1}},
		{"/works/47-mae-t23/chapters/2066-kniga/part-3.md", Address{Kind: KindChapter, WorkID: 47, ChapterID: 2066, Part: 3}},
		{"/works/47/chapters/2066/part-3", Address{Kind: KindChapter, WorkID: 47, ChapterID: 2066, Part: 3}},
		{"/works/114-lenin-t43/pages/4", Address{Kind: KindPages, WorkID: 114, From: 4, To: 4}},
		{"/works/114/pages/4-13", Address{Kind: KindPages, WorkID: 114, From: 4, To: 13}},
		{"/works/114/read/7", Address{Kind: KindPages, WorkID: 114, From: 7, To: 7}},
		{"/concepts/kooperaciya", Address{Kind: KindConcept, Slug: "kooperaciya", Part: 1}},
		{"https://lib.example.org/concepts/kooperaciya/part-3.md", Address{Kind: KindConcept, Slug: "kooperaciya", Part: 3}},
		{"/concepts", Address{Kind: KindConceptShelf, Part: 1}},
		{"/concepts.md", Address{Kind: KindConceptShelf, Part: 1}},
		{"/concepts/part-2", Address{Kind: KindConceptShelf, Part: 2}},
		{"/concepts/", Address{Kind: KindConceptShelf, Part: 1}},
		// Адрес, который кладёт кнопка «Спросить нейросеть» при выбранной
		// подрубрике, — и id, которым fetch его же возвращает.
		{"https://lib.example.org/concepts/abstraktnyj-trud.md?rubric_path=%25D0%25BE%25D0%25BF%25D1%2580%25D0%25B5%25D0%25B4%25D0%25B5%25D0%25BB%25D0%25B5%25D0%25BD%25D0%25B8%25D0%25B5",
			Address{Kind: KindConcept, Slug: "abstraktnyj-trud", Part: 1, Rubric: []string{"определение"}}},
		{"/concepts/kpss/part-2?rubric_path=%25D0%259A%3AII#x",
			Address{Kind: KindConcept, Slug: "kpss", Part: 2, Rubric: []string{"К", "II"}}},
		{"/concepts/kooperaciya?q=x", Address{Kind: KindConcept, Slug: "kooperaciya", Part: 1}},
		{"https://lib.example.org/works/114-lenin-t43/chapters/10807-rech", Address{Kind: KindChapter, WorkID: 114, ChapterID: 10807, Part: 1}},
		// Review Focus 1: хвосты адреса не мешают.
		{"https://lib.example.org/works/114-lenin-t43/pages/4?q=кооперация#p4", Address{Kind: KindPages, WorkID: 114, From: 4, To: 4}},
		{"/works/114/", Address{Kind: KindWork, WorkID: 114}},
		{"  /works/114  ", Address{Kind: KindWork, WorkID: 114}},
	}
	for _, c := range cases {
		got, err := ParseID(c.id)
		if err != nil {
			t.Errorf("%q: %v", c.id, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: %+v, ожидалось %+v", c.id, got, c.want)
		}
	}
}

func TestParseIDRejects(t *testing.T) {
	for _, id := range []string{
		"/works/abc", "/works/114/chapters/x", "/works/114/chapters/1/part-0",
		"/works/114/chapters/1/part-x", "/works/114/pages/0", "/works/114/pages/7-4",
		"/works/114/pages/4-", "/editions/4", "/works/114/chapters/1/extra/more",
		"/concepts/kooperaciya/part-0", "/concepts/a/b/c", "/concepts//part-2",
		// Подрубрика бывает только у понятия и обязана разбираться.
		"/concepts?rubric_path=a", "/works/114/chapters/1?rubric_path=a",
		"/concepts/kooperaciya?rubric_path=%25ZZ",
	} {
		if _, err := ParseID(id); !errors.Is(err, ErrBadAddress) {
			t.Errorf("%q: %v, ожидался ErrBadAddress", id, err)
		}
	}
}

func TestParseIDTooManyPagesSuggestsFirstTen(t *testing.T) {
	// 4-13 — ровно десять, принят (TestParseIDShapes); 4-14 — одиннадцать.
	for _, id := range []string{"/works/114-lenin-t43/pages/4-14", "/works/114/pages/4-20"} {
		_, err := ParseID(id)
		if err == nil {
			t.Fatalf("%s принят", id)
		}
		var u errUser
		if !errors.As(err, &u) || !strings.Contains(err.Error(), "Не больше 10 полос") ||
			!strings.Contains(err.Error(), "/works/114/pages/4-13") {
			t.Errorf("%s: отказ не подсказывает первые десять: %v", id, err)
		}
	}
}
