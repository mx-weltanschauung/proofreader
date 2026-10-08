package staticsite

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTitlesCoverEverySurface(t *testing.T) {
	out := build(t, func() *fakeSource {
		s := extrasFixture()
		c := conceptFixture()
		s.conceptList, s.concepts = c.conceptList, c.concepts
		return s
	}())
	raw := read(t, out, assetTitles)
	if !strings.HasPrefix(raw, "window.TITLES=") || !strings.HasSuffix(raw, ";\n") {
		t.Fatalf("titles.js не присваивает window.TITLES: %.80s", raw)
	}
	var entries [][4]string
	if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(raw, "window.TITLES="), ";\n")), &entries); err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, e := range entries {
		kinds[e[0]] = true
		if strings.HasPrefix(e[3], "/") || !strings.HasSuffix(strings.SplitN(e[3], "#", 2)[0], ".html") {
			t.Errorf("путь записи %q не относительный файл", e[3])
		}
	}
	for _, k := range []string{"work", "chapter", "concept", "collection", "document"} {
		if !kinds[k] {
			t.Errorf("в titles.js нет записей вида %q", k)
		}
	}
	found := false
	for _, e := range entries {
		if e[0] == "chapter" && e[1] == "Диалектический метод" {
			found = true
			if e[3] != ch10+"#ch-11" {
				t.Errorf("подглава ведёт на %q, хотел %q", e[3], ch10+"#ch-11")
			}
		}
	}
	if !found {
		t.Error("подглавы нет в поиске по заглавиям")
	}
}

func TestChapterFileFeedsPagefindVolumeAndSkipsFolios(t *testing.T) {
	out := build(t, fixture())
	body := read(t, out, ch10)
	mustContain(t, "глава для pagefind", body,
		`data-pagefind-meta="volume[data-value]" data-value="И. В. Сталин. Сочинения. Том 1"`,
		`<span class="page-marker" data-pagefind-ignore id="p2">`)
}

func TestSearchPageLoadsScriptsLocally(t *testing.T) {
	out := build(t, fixture())
	mustContain(t, "страница поиска", read(t, out, SearchFile),
		`<input id="q" type="search" name="q"`,
		`<script src="../assets/titles.js"></script>`,
		`<script src="../assets/search.js"></script>`,
		`<script src="../assets/search-page.js"></script>`,
		`<select id="edition" hidden>`)
}
