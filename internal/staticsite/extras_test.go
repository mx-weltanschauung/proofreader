package staticsite

import (
	"strings"
	"testing"

	"proofreader/internal/staticsite/sitecheck"
)

func TestDocumentAuthorLinksStayReadableAndPassSiteCheck(t *testing.T) {
	src := fixture()
	src.documents = []Document{{ID: 4, Slug: "ssylki", Title: "Со ссылками", BodyHTML: `<p>` +
		`<a href="https://ru.wikipedia.org/wiki/X" target="_blank">вики</a> · ` +
		`<a href="/works/49-lenin-t06/chapters/10-x">глава</a> · ` +
		`<a href="https://lib.example.org/concepts/y">понятие</a> · ` +
		`<a href="../soseden.html">сосед</a></p>` +
		`<div class="document-cut"><div class="document-cut-page">` +
		`<a class="document-cut-folio" href="/works/100/pages/2">2</a>` +
		`<p>Вклейка <a href="в скобках" target="_blank">слова</a>.</p></div>` +
		`<li id="fn:c1-2-1"><a class="fn-back" href="#fnref:c1-2-1">1</a> сирота</li></div>`}}
	out := build(t, src)
	body := read(t, out, "documents/4-ssylki.html")
	mustContain(t, "разбор со ссылками", body,
		`вики <span class="link-url">(https://ru.wikipedia.org/wiki/X)</span>`,
		`<a href="https://lib.example.org/works/49-lenin-t06/chapters/10-x">глава</a>`,
		`<a href="https://lib.example.org/concepts/y">понятие</a>`,
		`[сосед](../soseden.html)`,
		`<a class="document-cut-folio" href="../works/100-stalin-t01/10-anarhizm-ili-socializm.html#p2">2</a>`,
		`Вклейка [слова](в скобках).`,
		`<span class="fn-back">1</span> сирота`)
	problems, err := sitecheck.Run(sitecheck.Options{Dir: out, AllowedExternal: "https://lib.example.org/"})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		if strings.HasPrefix(p.File, "documents/") {
			t.Error(p)
		}
	}
}

func extrasFixture() *fakeSource {
	src := fixture()
	src.collections = []Collection{{ID: 5, Slug: "manifest", Title: "Начала", Description: "Для первого чтения.", Items: []CollectionItem{
		{Title: "Диалектический метод", Author: "И. В. Сталин", WorkID: 100, ChapterID: 11},
		// Глава 99 в сборке своего якоря не получила (съедена соседом) —
		// пункт ведёт на её первую полосу.
		{Title: "Съеденная", WorkID: 100, ChapterID: 99, StartPage: 4},
		{Title: "Том 1 целиком", WorkID: 100},
		{Title: "Удалённая глава", Broken: true},
		{Title: "Глава вне сборки", WorkID: 500, ChapterID: 501, StartPage: 1},
	}}}
	src.documents = []Document{{ID: 3, Slug: "chto-delat", Title: "Что делать с методом", Author: "читатель",
		BodyHTML: `<p>Текст.</p><a class="document-cut-folio" href="/works/100/pages/2">2</a>` +
			`<a href="/works/100/pages/77">Читать в томе</a>`}}
	return src
}

func TestCollectionLinksItems(t *testing.T) {
	out := build(t, extrasFixture())
	body := read(t, out, "collections/5-manifest.html")
	mustContain(t, "подборка", body,
		"<h1>Начала</h1>", "Для первого чтения.",
		`<a href="../works/100-stalin-t01/10-anarhizm-ili-socializm.html#ch-11">Диалектический метод</a> <span class="author">— И. В. Сталин</span>`,
		`<a href="../works/100-stalin-t01/index.html">Том 1 целиком</a>`,
		`<li class="broken">Удалённая глава <span class="note">(источник недоступен)</span></li>`,
		`<li class="absent">Глава вне сборки <span class="note">(нет в этой копии)</span></li>`)
	mustContain(t, "главная", read(t, out, HomeFile), "<h2>Подборки</h2>", `<a href="collections/5-manifest.html">Начала</a>`)
}

func TestCollectionItemFallsBackToStartPage(t *testing.T) {
	out := build(t, extrasFixture())
	body := read(t, out, "collections/5-manifest.html")
	mustContain(t, "запасной путь", body, `<a href="../works/100-stalin-t01/`+strings.TrimPrefix(ch12, "works/100-stalin-t01/")+`#p4">Съеденная</a>`)
}

func TestDocumentRelinksCutPages(t *testing.T) {
	out := build(t, extrasFixture())
	body := read(t, out, "documents/3-chto-delat.html")
	mustContain(t, "разбор", body,
		"<h1>Что делать с методом</h1>", `<p class="author">читатель</p>`,
		`href="../works/100-stalin-t01/10-anarhizm-ili-socializm.html#p2"`,
		`data-missing-page="77"`,
		`href="https://lib.example.org/documents/читатель/chto-delat"`)
	if strings.Contains(body, `href="/works/`) {
		t.Error("в разборе остался абсолютный адрес /works/ — с диска он не откроется")
	}
}
