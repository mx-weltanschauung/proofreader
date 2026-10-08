package book

import (
	"fmt"
	"io"
	"strings"

	"proofreader/pkg/xhtml"
)

// HTMLWriter отдаёт книгу одним самодостаточным файлом: стили внутри, внешних
// ресурсов нет, открывается офлайн и печатается штатной печатью браузера.
type HTMLWriter struct{}

func (HTMLWriter) ContentType() string { return "text/html; charset=utf-8" }
func (HTMLWriter) Ext() string         { return "html" }

func (HTMLWriter) Write(w io.Writer, b *Book) error {
	var out strings.Builder

	out.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	out.WriteString(`<html xmlns="http://www.w3.org/1999/xhtml" xml:lang="` + xhtml.EscapeAttr(langOf(b)) + `">` + "\n")
	out.WriteString("<head>\n")
	out.WriteString("<meta charset=\"utf-8\"/>\n")
	out.WriteString("<title>" + xhtml.EscapeText(oneLine(b.Meta.Title)) + "</title>\n")
	out.WriteString("<style>\n" + readingCSS + "</style>\n")
	out.WriteString("</head>\n<body>\n")

	writeBody(&out, b)

	out.WriteString("</body>\n</html>\n")

	_, err := io.WriteString(w, out.String())
	return err
}

// BodyHTML — содержимое <body> книги без обёртки документа. Двух
// потребителей у него ровно два: файл выгрузки (HTMLWriter выше) и страница
// для краулера (internal/seo), и оба обязаны печатать один и тот же текст —
// иначе поисковик индексирует одно, а читатель скачивает другое.
// Проверяется TestBodyHTMLMatchesWriterBody.
func BodyHTML(b *Book) string {
	var out strings.Builder
	writeBody(&out, b)
	return out.String()
}

func writeBody(out *strings.Builder, b *Book) {
	writeTitlePage(out, b)
	writeTableOfContents(out, b)
	for i, s := range b.Sections {
		writeHTMLSection(out, s, 1, sectionAnchor(i))
	}
}

func langOf(b *Book) string {
	if b.Meta.Lang == "" {
		return "ru"
	}
	return b.Meta.Lang
}

// sectionAnchor — якорь секции верхнего уровня. Общий для HTML и EPUB: в EPUB
// он же становится именем файла sec-1.xhtml.
func sectionAnchor(index int) string { return fmt.Sprintf("sec-%d", index+1) }

// childAnchor — детерминированный якорь вложенной секции: якорь родителя плюс
// позиция среди её дочерних секций (1-based, считая только дочерние секции,
// а не прогоны страниц). Общий для HTML- и EPUB-писателей и для их оглавлений
// — id, который печатает writeHTMLSection, и href, который печатают
// writeTocChildren/writeNavChildren, обязаны совпадать по одной и той же
// схеме, иначе ссылка ведёт в никуда.
func childAnchor(parentAnchor string, index int) string {
	return fmt.Sprintf("%s-%d", parentAnchor, index)
}

// pageAnchor — якорь печатной страницы, чтобы на неё можно было сослаться.
func pageAnchor(printed int) string { return fmt.Sprintf("p%d", printed) }

// PageAnchor — якорь печатной страницы для соседних пакетов: статическая
// читальня ведёт на полосу ссылками из указателя и подборок, и якорь
// обязан совпадать с тем, что печатает writeHTMLSection.
func PageAnchor(printed int) string { return pageAnchor(printed) }

// ChapterAnchor — якорь главы по её id. Ставится вместо позиционного, когда
// секция несёт ChapterID: на подглаву статической читальни ссылаются
// оглавление тома, указатель и подборки, и такой якорь не сдвигается от
// вставки соседней главы, в отличие от «sec-1-3».
func ChapterAnchor(id int64) string { return fmt.Sprintf("ch-%d", id) }

// shownAnchor — якорь, который печатается у секции и в ссылках оглавления на
// неё. positional — её позиционный якорь; он же продолжает цепочку детей
// (childAnchor), чтобы их позиции не зависели от ChapterID.
func shownAnchor(s Section, positional string) string {
	if s.ChapterID != 0 {
		return ChapterAnchor(s.ChapterID)
	}
	return positional
}

// SectionHTML — одна секция верхнего уровня без титула и оглавления книги:
// статическая читальня кладёт каждую главу верхнего уровня в свой файл и
// шапку печатает сама.
func SectionHTML(s Section) string {
	var out strings.Builder
	writeHTMLSection(&out, s, 1, sectionAnchor(0))
	return out.String()
}

func writeTitlePage(out *strings.Builder, b *Book) {
	out.WriteString(`<section class="titlepage">` + "\n")
	for _, line := range TitleLines(b.Meta) {
		if line == "" {
			continue
		}
		out.WriteString("<p>" + xhtml.EscapeText(line) + "</p>\n")
	}
	out.WriteString("</section>\n")
}

func writeTableOfContents(out *strings.Builder, b *Book) {
	if len(b.Sections) == 0 {
		return
	}
	out.WriteString(`<nav class="toc"><h2>Содержание</h2>` + "\n<ul>\n")
	for i, s := range b.Sections {
		anchor := sectionAnchor(i)
		out.WriteString(`<li><a href="#` + shownAnchor(s, anchor) + `">` +
			xhtml.EscapeText(oneLine(s.Title)) + "</a>")
		writeTocChildren(out, s, anchor)
		out.WriteString("</li>\n")
	}
	out.WriteString("</ul>\n</nav>\n")
}

// writeTocChildren печатает вложенные пункты оглавления как ссылки — не
// голым текстом, иначе в 1260 вложенных главах корпуса некуда ткнуть. anchor
// — якорь секции s, которой принадлежат дочерние; из него по childAnchor
// строится якорь каждого ребёнка, той же схемой, что и id в writeHTMLSection.
func writeTocChildren(out *strings.Builder, s Section, anchor string) {
	var children []Section
	for _, b := range s.Blocks {
		if b.Child != nil {
			children = append(children, *b.Child)
		}
	}
	if len(children) == 0 {
		return
	}
	out.WriteString("\n<ul>\n")
	for i, c := range children {
		childA := childAnchor(anchor, i+1)
		out.WriteString(`<li><a href="#` + shownAnchor(c, childA) + `">` + xhtml.EscapeText(oneLine(c.Title)) + "</a>")
		writeTocChildren(out, c, childA)
		out.WriteString("</li>\n")
	}
	out.WriteString("</ul>\n")
}

// writeHTMLSection печатает секцию. anchor — её якорь: у секций верхнего
// уровня это sectionAnchor(i), у вложенных — childAnchor от якоря родителя.
// На него ссылается оглавление (writeTocChildren/writeNavChildren), и по
// якорям верхнего уровня режется EPUB.
func writeHTMLSection(out *strings.Builder, s Section, level int, anchor string) {
	if level > 6 {
		level = 6
	}
	tag := fmt.Sprintf("h%d", level)

	// С ChapterID якорь стоит на заголовке, а не на <section>: pagefind
	// (полнотекстовый поиск статической читальни) ведёт подрезультат только
	// на заголовок с id. Без ChapterID — прежняя вёрстка байт в байт.
	headingID := ""
	out.WriteString(`<section`)
	if s.ChapterID != 0 {
		headingID = ` id="` + xhtml.EscapeAttr(ChapterAnchor(s.ChapterID)) + `"`
	} else if anchor != "" {
		out.WriteString(` id="` + xhtml.EscapeAttr(anchor) + `"`)
	}
	out.WriteString(">\n")

	out.WriteString("<" + tag + headingID + ">" + xhtml.EscapeText(oneLine(s.Title)) + "</" + tag + ">\n")
	if s.Author != "" {
		out.WriteString(`<p class="section-author">` + xhtml.EscapeText(oneLine(s.Author)) + "</p>\n")
	}

	childIdx := 0
	for _, block := range s.Blocks {
		if block.Child != nil {
			childIdx++
			writeHTMLSection(out, *block.Child, level+1, childAnchor(anchor, childIdx))
			continue
		}
		for _, p := range block.Pages {
			fmt.Fprintf(out, `<span class="page-marker" id="%s">%d</span>`+"\n",
				xhtml.EscapeAttr(pageAnchor(p.Printed)), p.Printed)
			if strings.TrimSpace(p.HTML) != "" {
				out.WriteString(withMathML(p.HTML) + "\n")
			}
		}
	}

	// Примечания ложатся в конец своей секции верхнего уровня: CollectPages
	// перенумеровывает подстрочные сквозь весь переданный кусок, и собрать их
	// на всю книгу значило бы получить (1)…(1500) одной простынёй.
	if strings.TrimSpace(s.NotesHTML) != "" {
		out.WriteString(withMathML(s.NotesHTML) + "\n")
	}

	out.WriteString("</section>\n")
}

// readingCSS — типографика читальни: засечный шрифт, мера строки около 34em,
// тёмная тема по системной настройке. Общий с EPUB, там уезжает в style.css.
const readingCSS = `
/* Осторожно: угловые скобки и амперсанд в этом CSS ломают выход HTML-писателя
   и EPUB. Содержимое тега style в XML не является CDATA, поэтому селектор
   дочернего элемента через знак «больше» (например, a, за которым следует
   такой знак и b), добавленный позже, молча сделает файл неразбираемым.
   Тест TestHTMLIsWellFormedXML это поймает. */
:root { --ink: #1a1a1a; --paper: #fdfdfb; --muted: #8a8578; }
@media (prefers-color-scheme: dark) {
  :root { --ink: #ded9d0; --paper: #17181a; --muted: #6f6a60; }
}
body {
  margin: 0 auto; padding: 2rem 1.25rem; max-width: 34em;
  font-family: "PT Serif", Georgia, "Times New Roman", serif;
  font-size: 1.05rem; line-height: 1.6;
  color: var(--ink); background: var(--paper);
  hyphens: auto;
}
h1, h2, h3, h4, h5, h6 { line-height: 1.25; font-weight: 600; }
.titlepage { margin-bottom: 3rem; }
.titlepage p { margin: 0.35rem 0; color: var(--muted); }
.titlepage p:first-child, .titlepage p:nth-child(2) { color: var(--ink); font-size: 1.15rem; }
.toc { margin-bottom: 3rem; }
.toc ul { list-style: none; padding-left: 1rem; }
.toc a { color: inherit; }
.section-author { color: var(--muted); font-style: italic; }
.page-marker {
  float: left; margin-left: -3.5rem; width: 3rem; text-align: right;
  color: var(--muted); font-size: 0.75rem; user-select: none;
}
@media (max-width: 48em) {
  .page-marker { float: none; margin: 0 0 0 0.5rem; display: inline-block; width: auto; }
}
blockquote { margin: 1rem 0 1rem 1.5rem; color: var(--muted); font-style: italic; }
table { border-collapse: collapse; margin: 1rem 0; }
td, th { border: 1px solid var(--muted); padding: 0.3rem 0.5rem; }
.footnotes { margin-top: 3rem; padding-top: 1rem; border-top: 1px solid var(--muted); font-size: 0.9rem; }
.footnotes a { color: inherit; }
@media print {
  body { max-width: none; color: #000; background: #fff; }
  .page-marker { color: #888; }
}
`
