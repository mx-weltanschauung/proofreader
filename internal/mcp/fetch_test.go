package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"proofreader/internal/limit"
	"proofreader/internal/seo"
	"proofreader/pkg/book"
)

func textDeps(ft *fakeText) Deps {
	return Deps{BaseURL: base, Text: ft}
}

func TestFetchChapterPartGoesThroughSEOText(t *testing.T) {
	ft := &fakeText{byPath: map[string]*seo.TextResult{
		"/works/47/chapters/2066/part-3.md": {
			Path:      "/works/47-mae-t23/chapters/2066-kniga/part-3.md",
			Body:      "Глава «Книга первая» — К. Маркс\nИсточник: …\n\n---\n\n[100]\n\nТекст",
			Canonical: base + "/works/47-mae-t23/chapters/2066-kniga",
		},
	}}
	s := newService(textDeps(ft), testGates())
	out, err := s.fetch(context.Background(), FetchInput{ID: "https://lib.example.org/works/47-mae-t23/chapters/2066-kniga/part-3.md"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != "/works/47-mae-t23/chapters/2066-kniga/part-3" || out.URL != base+"/works/47-mae-t23/chapters/2066-kniga" {
		t.Errorf("id %q url %q", out.ID, out.URL)
	}
	if out.Title != "Глава «Книга первая» — К. Маркс" || !strings.Contains(out.Text, "[100]") {
		t.Errorf("title %q, text %q", out.Title, out.Text)
	}
}

func TestFetchCatalogAndWork(t *testing.T) {
	ft := &fakeText{byPath: map[string]*seo.TextResult{
		"/llms.txt":     {Path: "/llms.txt", Body: "# Читальня\n\n> Библиотека"},
		"/works/114.md": {Path: "/works/114-lenin-t43.md", Body: "# Том 43\n", Canonical: base + "/works/114-lenin-t43"},
	}}
	s := newService(textDeps(ft), testGates())
	out, err := s.fetch(context.Background(), FetchInput{ID: "/"})
	if err != nil || out.ID != "/" || out.URL != base+"/" || out.Title != "Читальня" {
		t.Fatalf("каталог: %+v, %v", out, err)
	}
	out, err = s.fetch(context.Background(), FetchInput{ID: "/works/114"})
	if err != nil || out.ID != "/works/114-lenin-t43" || out.URL != base+"/works/114-lenin-t43" {
		t.Fatalf("том: %+v, %v", out, err)
	}
}

func TestFetchMissingTextIsNotFound(t *testing.T) {
	s := newService(textDeps(&fakeText{}), testGates())
	_, err := s.fetch(context.Background(), FetchInput{ID: "/works/45/chapters/1889"})
	if userError(err).Error() != msgNotFound {
		t.Fatalf("%v", err)
	}
}

// Рендер главы MCP — не больше одного общего тяжёлого слота.
func TestFetchChapterHoldsAtMostOneHeavySlot(t *testing.T) {
	heavy := limit.New(2)
	ft := &fakeText{heavy: heavy, block: make(chan struct{}), entered: make(chan struct{}, 4),
		byPath: map[string]*seo.TextResult{"/works/1/chapters/1.md": {Path: "/works/1/chapters/1.md", Body: "x"}}}
	s := newService(textDeps(ft), testGates())
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := s.fetch(context.Background(), FetchInput{ID: "/works/1/chapters/1"})
			errs <- err
		}()
	}
	<-ft.entered
	time.Sleep(30 * time.Millisecond)
	if n := len(heavy); n != 1 {
		t.Fatalf("MCP держит %d тяжёлых слотов, ожидался 1", n)
	}
	close(ft.block)
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}

func rangeBook(from, to int) *book.Book {
	var pages book.Pages
	for n := from; n <= to; n++ {
		pages = append(pages, book.Page{Internal: n, Printed: n, Markdown: fmt.Sprintf("Текст полосы %d.", n)})
	}
	return &book.Book{
		Meta: book.Meta{Title: "Том 43", Authors: []string{"В. И. Ленин"}, Edition: "ПСС", Volume: "Том 43",
			PageFrom: from, PageTo: to},
		Sections: []book.Section{{Title: "1. Речь", Blocks: []book.Block{{Pages: pages}}}},
	}
}

func pagesDeps(lib *fakeLibrary) Deps {
	return Deps{
		BaseURL: base,
		Works:   fakeWorks{114: {ID: 114, Slug: "lenin-t43", Title: "Том 43", Author: "В. И. Ленин"}},
		Chapters: fakeChapters{
			4: {ID: 10807, WorkID: 114, Title: "1. Речь", Slug: "1-rech", StartPage: 4, EndPage: 6},
			6: {ID: 10807, WorkID: 114, Title: "1. Речь", Slug: "1-rech", StartPage: 4, EndPage: 6},
		},
		Library: lib,
	}
}

func TestFetchPagesIsWriterOutputWithHeader(t *testing.T) {
	b := rangeBook(4, 6)
	s := newService(pagesDeps(&fakeLibrary{books: map[[3]int]*book.Book{{114, 4, 6}: b}}), testGates())
	out, err := s.fetch(context.Background(), FetchInput{ID: "/works/114-lenin-t43/pages/4-6"})
	if err != nil {
		t.Fatal(err)
	}
	text, starts := book.MarkdownWithPageStarts(b)
	if !strings.HasSuffix(out.Text, "---\n\n"+text[starts[0].Offset:]) {
		t.Errorf("тело — не вывод MarkdownWriter с первой полосы:\n%s", out.Text)
	}
	if strings.Count(out.Text, "Глава «1. Речь»") != 1 || !strings.Contains(out.Text, base+"/works/114-lenin-t43/chapters/10807-1-rech") {
		t.Errorf("шапка не называет главу один раз с адресом:\n%s", out.Text)
	}
	if !strings.Contains(out.Text, seo.LLMReadingNote) || !strings.Contains(out.Text, "с. 4—6") {
		t.Errorf("шапка без печатных страниц или правила чтения:\n%s", out.Text)
	}
	if out.ID != "/works/114-lenin-t43/pages/4-6" || out.URL != base+"/works/114-lenin-t43/pages/4" {
		t.Errorf("id %q url %q", out.ID, out.URL)
	}
	if !strings.Contains(out.Text, "Предыдущие полосы: /works/114-lenin-t43/pages/1-3") ||
		!strings.Contains(out.Text, "Следующие полосы: /works/114-lenin-t43/pages/7-16") {
		t.Errorf("нет навигации:\n%s", out.Text)
	}
}

// Review Focus 2: за концом тома — фактический диапазон, «следующих» нет.
func TestFetchPagesPastTheEnd(t *testing.T) {
	b := rangeBook(838, 840)
	s := newService(pagesDeps(&fakeLibrary{books: map[[3]int]*book.Book{{114, 838, 847}: b}}), testGates())
	out, err := s.fetch(context.Background(), FetchInput{ID: "/works/114/pages/838-847"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != "/works/114-lenin-t43/pages/838-840" {
		t.Errorf("id %q, ожидался фактический диапазон", out.ID)
	}
	if strings.Contains(out.Text, "Следующие полосы") {
		t.Error("за концом тома обещаны следующие полосы")
	}
}

func TestFetchPagesUnknownWorkIsNotFound(t *testing.T) {
	s := newService(pagesDeps(&fakeLibrary{}), testGates())
	_, err := s.fetch(context.Background(), FetchInput{ID: "/works/999/pages/1"})
	if userError(err).Error() != msgNotFound {
		t.Fatalf("%v", err)
	}
}

func TestFetchConceptGoesThroughSEOText(t *testing.T) {
	ft := &fakeText{byPath: map[string]*seo.TextResult{
		"/concepts/kooperaciya/part-2.md": {
			Path:      "/concepts/kooperaciya/part-2.md",
			Body:      "Понятие «Кооперация» — предметный указатель\nАдрес понятия: …\n\n[4]\n\nТекст",
			Canonical: base + "/concepts/kooperaciya",
		},
		"/concepts.md":        {Path: "/concepts.md", Body: "# Предметный указатель — Читальня\n", Canonical: base + "/concepts"},
		"/concepts/part-2.md": {Path: "/concepts/part-2.md", Body: "# Предметный указатель — Читальня\nЧасть 2 из 4.", Canonical: base + "/concepts"},
	}}
	s := newService(textDeps(ft), testGates())
	out, err := s.fetch(context.Background(), FetchInput{ID: "/concepts/kooperaciya/part-2"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != "/concepts/kooperaciya/part-2" || out.URL != base+"/concepts/kooperaciya" ||
		out.Title != "Понятие «Кооперация» — предметный указатель" || !strings.Contains(out.Text, "[4]") {
		t.Errorf("%+v", out)
	}
	shelf, err := s.fetch(context.Background(), FetchInput{ID: "/concepts"})
	if err != nil {
		t.Fatal(err)
	}
	if shelf.ID != "/concepts" || shelf.Title != "Предметный указатель — Читальня" {
		t.Errorf("%+v", shelf)
	}
	shelf2, err := s.fetch(context.Background(), FetchInput{ID: "https://lib.example.org/concepts/part-2.md"})
	if err != nil {
		t.Fatal(err)
	}
	if shelf2.ID != "/concepts/part-2" || !strings.Contains(shelf2.Text, "Часть 2 из 4.") {
		t.Errorf("%+v", shelf2)
	}
}

// Адрес подрубрики (его кладёт кнопка «Спросить нейросеть») идёт в текст
// /seo с той же подрубрикой в канонической форме, а не в понятие целиком; id
// результата её сохраняет.
func TestFetchConceptRubricKeepsRubric(t *testing.T) {
	q := seo.RubricQuery([]string{"определение"})
	ft := &fakeText{byPath: map[string]*seo.TextResult{
		"/concepts/abstraktnyj-trud/part-2.md" + q: {
			Path:      "/concepts/abstraktnyj-trud/part-2.md" + q,
			Body:      "Понятие «Абстрактный труд» — предметный указатель\nТолько подрубрика «определение».",
			Canonical: base + "/concepts/abstraktnyj-trud" + q,
		},
	}}
	s := newService(textDeps(ft), testGates())
	out, err := s.fetch(context.Background(), FetchInput{
		ID: base + "/concepts/abstraktnyj-trud/part-2.md?rubric_path=%25D0%25BE%25D0%25BF%25D1%2580%25D0%25B5%25D0%25B4%25D0%25B5%25D0%25BB%25D0%25B5%25D0%25BD%25D0%25B8%25D0%25B5",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != "/concepts/abstraktnyj-trud/part-2"+q || !strings.Contains(out.Text, "Только подрубрика") {
		t.Errorf("%+v", out)
	}
	// id обратно в fetch — тот же текст.
	again, err := s.fetch(context.Background(), FetchInput{ID: out.ID})
	if err != nil || again.Text != out.Text {
		t.Errorf("повторный fetch по id: %v %+v", err, again)
	}
}

func TestFetchBadAddress(t *testing.T) {
	s := newService(Deps{BaseURL: base}, testGates())
	_, err := s.fetch(context.Background(), FetchInput{ID: "/editions/4"})
	if !errors.Is(userError(err), ErrBadAddress) {
		t.Fatalf("%v", err)
	}
}
