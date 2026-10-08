package staticsite

import (
	"strings"
	"testing"
)

func TestRel(t *testing.T) {
	cases := []struct{ from, to, want string }{
		{"index.html", "works/1/index.html", "works/1/index.html"},
		{"works/1-a/2-b.html", "concepts/x.html#p3", "../../concepts/x.html#p3"},
		{"concepts/x.html", "index.html", "../index.html"},
		{"works/1/2.html", "works/1/2.html#ch-5", "#ch-5"},
		{"works/1/2.html", "works/1/3.html", "../../works/1/3.html"},
	}
	for _, c := range cases {
		if got := rel(c.from, c.to); got != c.want {
			t.Errorf("rel(%q, %q) = %q, хотел %q", c.from, c.to, got, c.want)
		}
	}
}

func TestPaths(t *testing.T) {
	cases := map[string]string{
		WorkIndex(49, "lenin-t06"):                        "works/49-lenin-t06/index.html",
		WorkIndex(7, ""):                                  "works/7/index.html",
		ChapterFile(49, "lenin-t06", 10125, "chto-delat"): "works/49-lenin-t06/10125-chto-delat.html",
		ChapterFile(49, "lenin-t06", 3, ""):               "works/49-lenin-t06/3.html",
		GapFile(49, "lenin-t06", 1):                       "works/49-lenin-t06/vne-glav-1.html",
		EditionIndex("stalin"):                            "editions/stalin/index.html",
		CollectionFile(5, "manifest"):                     "collections/5-manifest.html",
		DocumentFile(3, "chto-delat"):                     "documents/3-chto-delat.html",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("путь %q, хотел %q", got, want)
		}
	}
}

func TestConceptFileIsShortAndDistinct(t *testing.T) {
	long := strings.Repeat("dva-puti-razvitiya-kapitalizma-", 8) + "a"
	a, b := ConceptFile(long), ConceptFile(long+"b")
	if a == b {
		t.Fatalf("разные слаги дали один файл %q", a)
	}
	name := strings.TrimSuffix(strings.TrimPrefix(a, "concepts/"), ".html")
	if len(name) > 60+1+8 {
		t.Errorf("имя файла понятия %d знаков (%q) — Windows не откроет длинный путь", len(name), name)
	}
	if got := ConceptFile("dialektika"); !strings.HasPrefix(got, "concepts/dialektika-") {
		t.Errorf("короткий слаг потерял читаемую часть: %q", got)
	}
}
