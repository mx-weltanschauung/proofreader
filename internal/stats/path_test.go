package stats

import (
	"strings"
	"testing"
)

func TestParsePath(t *testing.T) {
	cases := []struct {
		path string
		want Target
		ok   bool
	}{
		{"/", Target{Kind: "home"}, true},
		{"/search", Target{Kind: "search"}, true},
		{"/help", Target{Kind: "help"}, true},
		{"/legal", Target{Kind: "other", SlugKey: "legal"}, true},
		{"/documents", Target{Kind: "list", SlugKey: "documents"}, true},
		{"/concepts", Target{Kind: "list", SlugKey: "concepts"}, true},
		{"/collections", Target{Kind: "list", SlugKey: "collections"}, true},
		{"/editions/3-lenin", Target{Kind: "edition", EntityID: 3}, true},
		{"/works/49-lenin-t06", Target{Kind: "work", WorkID: 49}, true},
		{"/works/49/", Target{Kind: "work", WorkID: 49}, true},
		{"/works/49-lenin-t06/chapters/10125-chto-delat", Target{Kind: "chapter", WorkID: 49, EntityID: 10125}, true},
		{"/works/49/pages/233", Target{Kind: "page", WorkID: 49, EntityID: 233}, true},
		{"/works/49/read/12", Target{Kind: "read", WorkID: 49, EntityID: 12}, true},
		{"/documents/chto-delat", Target{Kind: "document", SlugKey: "chto-delat"}, true},
		{"/documents/chitatel/chto-delat", Target{Kind: "document", SlugKey: "chitatel/chto-delat"}, true},
		{"/collections/marx", Target{Kind: "collection", SlugKey: "marx"}, true},
		{"/collections/chitatel/marx", Target{Kind: "collection", SlugKey: "chitatel/marx"}, true},
		{"/collections/marx/read/7", Target{Kind: "collection", SlugKey: "marx"}, true},
		{"/concepts/stoimost", Target{Kind: "concept", SlugKey: "stoimost"}, true},
		{"/collections/chitatel/marx/read/7", Target{Kind: "collection", SlugKey: "chitatel/marx"}, true},
		{"/documents/%D0%B2%D0%B0%D1%81%D1%8F/chto-delat", Target{Kind: "document", SlugKey: "вася/chto-delat"}, true},
		{"/collections/%D0%B2%D0%B0%D1%81%D1%8F/marx", Target{Kind: "collection", SlugKey: "вася/marx"}, true},
		// не считаются
		{"/works/49/%ZZ", Target{}, false},
		{"/documents/a%2525b", Target{}, false},
		{"/documents/<script>", Target{}, false},
		{"/documents/" + strings.Repeat("я", 150), Target{}, false},
		{"/concepts/a b", Target{}, false},
		{"/documents/%D0%B2/%65dit", Target{}, false},
		{"/admin/users", Target{}, false},
		{"/login", Target{}, false},
		{"/join", Target{}, false},
		{"/mine", Target{}, false},
		{"/documents/new", Target{}, false},
		{"/documents/chto-delat/edit", Target{}, false},
		{"/documents/chitatel/chto-delat/edit", Target{}, false},
		{"/collections/new", Target{}, false},
		{"/collections/marx/edit", Target{}, false},
		{"/works/49/pages/233/suggest", Target{}, false},
		{"/works/abc", Target{}, false},
		{"/works/49/chapters/", Target{}, false},
		{"/works/49/chapters/x", Target{}, false},
		{"/works/49/chapters/1/x", Target{}, false},
		{"/nonsense/1", Target{}, false},
		{"works/49", Target{}, false},
		{"/" + strings.Repeat("a", 600), Target{}, false},
	}
	for _, c := range cases {
		got, ok := ParsePath(c.path)
		if ok != c.ok || got != c.want {
			t.Errorf("ParsePath(%q) = %+v, %v; ожидалось %+v, %v", c.path, got, ok, c.want, c.ok)
		}
	}
}

func TestCleanQuery(t *testing.T) {
	if got := CleanQuery("  Прибавочная СТОИМОСТЬ "); got != "прибавочная стоимость" {
		t.Fatalf("got %q", got)
	}
	if got := CleanQuery(strings.Repeat("я", 300)); len([]rune(got)) != MaxQueryRunes {
		t.Fatalf("длина %d", len([]rune(got)))
	}
}
