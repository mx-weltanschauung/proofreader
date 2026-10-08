package staticsite

import (
	"fmt"
	"proofreader/internal/site"
	"strings"

	"proofreader/internal/seo"
)

const (
	assetCSS        = "assets/site.css"
	assetSearch     = "assets/search.js"
	assetSearchPage = "assets/search-page.js"
	assetTitles     = "assets/titles.js"
)

// Esc и OneLine — те же, что у страниц краулера: одна функция экранирования
// и текста, и значения атрибута в кавычках.
func Esc(s string) string     { return seo.Esc(s) }
func OneLine(s string) string { return seo.OneLine(s) }

type crumb struct{ title, path string }

// shell — страница сборки до обёртки.
type shell struct {
	path    string // путь этого файла от корня
	title   string // пусто — только «Читальня»
	crumbs  []crumb
	body    string // готовый HTML, ссылки уже относительные
	prod    string // путь на боевом («/works/49-…»); пусто — без ссылки
	scripts []string
}

func (g *Generator) page(s shell) string {
	href := func(to string) string { return Esc(rel(s.path, to)) }
	title := site.Name()
	if t := OneLine(s.title); t != "" {
		title = t + " — " + site.Name()
	}

	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"ru\">\n<head>\n<meta charset=\"utf-8\">\n")
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">` + "\n")
	b.WriteString("<title>" + Esc(title) + "</title>\n")
	b.WriteString(`<link rel="stylesheet" href="` + href(assetCSS) + `">` + "\n</head>\n<body>\n")

	b.WriteString(`<header class="site"><a class="brand" href="` + href(HomeFile) + `">` + Esc(site.Name()) + `</a>`)
	b.WriteString(`<nav><a href="` + href(ConceptsIndex) + `">Указатель</a><a href="` + href(SearchFile) + `">Поиск</a></nav>`)
	b.WriteString(`<form class="site-search" action="` + href(SearchFile) + `" method="get">`)
	b.WriteString(`<input type="search" name="q" placeholder="Поиск по заглавиям" aria-label="Поиск по заглавиям"></form>`)
	b.WriteString("</header>\n")

	if len(s.crumbs) > 0 {
		b.WriteString(`<nav class="crumbs">`)
		for i, c := range s.crumbs {
			if i > 0 {
				b.WriteString(" › ")
			}
			fmt.Fprintf(&b, `<a href="%s">%s</a>`, href(c.path), Esc(OneLine(c.title)))
		}
		b.WriteString("</nav>\n")
	}

	b.WriteString("<main>\n" + s.body + "</main>\n")

	b.WriteString(`<footer class="site"><p>Статическая копия читальни, собрана ` + Esc(g.BuildDate) + ".")
	if g.BaseURL != "" && s.prod != "" {
		u := strings.TrimSuffix(g.BaseURL, "/") + s.prod
		b.WriteString(` <a href="` + Esc(u) + `">Эта страница в читальне онлайн</a>.`)
	}
	if g.BaseURL != "" {
		// Откуда текст и на каких условиях — то, что копия, разойдясь по
		// рукам, обязана нести с собой.
		b.WriteString(` <a href="` + Esc(strings.TrimSuffix(g.BaseURL, "/")+"/legal#copyright") + `">Права на тексты и снятие по жалобе</a>.`)
	}
	b.WriteString("</p></footer>\n")

	for _, src := range s.scripts {
		b.WriteString(`<script src="` + href(src) + `"></script>` + "\n")
	}
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

// printedSpan — «46» или «46—47», как в указателе.
func printedSpan(from, to int) string {
	if to <= from {
		return fmt.Sprint(from)
	}
	return fmt.Sprintf("%d—%d", from, to)
}
