package book

import (
	"fmt"
	"io"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"proofreader/pkg/xhtml"
)

// FB2Writer собирает книгу в FB2 — формат, привычный читателям русских
// электронных библиотек.
//
// Единственный писатель со своим обходом дерева: FB2 — не HTML, у него свой
// набор тегов, и обернуть XHTML не выйдет.
type FB2Writer struct{}

func (FB2Writer) ContentType() string { return "application/x-fictionbook+xml" }
func (FB2Writer) Ext() string         { return "fb2" }

// lineBreak — служебный разделитель, которым обход помечает <br/>. Абзац потом
// режется по нему на несколько <p>: в корпусе так размечены стихи, и склейка
// строк пробелом превратила бы их в прозу.
//
// \x00 безопасен как маркер, а не совпадёт с текстом страницы: EscapeText
// вырезает управляющие байты (isDisallowedXMLControl в pkg/xhtml) дважды на
// пути сюда — один раз в xhtml.Body при снятии обёртки (fragment() в
// pkg/markdown, при разборе страницы), второй раз здесь же, в
// writeFB2Inline, — так что настоящий \x00 из текста до этой точки не
// доходит ни при каком входе.
const lineBreak = "\x00"

func (FB2Writer) Write(w io.Writer, b *Book) error {
	var out strings.Builder

	out.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	out.WriteString(`<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0" ` +
		`xmlns:l="http://www.w3.org/1999/xlink">` + "\n")

	writeFB2Description(&out, b)

	out.WriteString("<body>\n")
	for _, s := range b.Sections {
		writeFB2Section(&out, s)
	}
	out.WriteString("</body>\n")

	writeFB2Notes(&out, b)

	out.WriteString("</FictionBook>\n")

	_, err := io.WriteString(w, out.String())
	return err
}

func writeFB2Description(out *strings.Builder, b *Book) {
	out.WriteString("<description>\n<title-info>\n")
	out.WriteString("<genre>nonfiction</genre>\n")
	authors := b.Meta.Authors
	if len(authors) == 0 {
		// Схема FB2 требует title-info/author+ (хотя бы один). Подборки
		// (Task 10) собирают авторов из элементов оглавления и могут отдать
		// nil, если у всех элементов автор пуст, — печатаем пустой, но
		// присутствующий элемент, а не пропускаем обязательное поле.
		authors = []string{""}
	}
	for _, a := range authors {
		first, last := splitName(oneLine(a))
		out.WriteString("<author><first-name>" + xhtml.EscapeText(first) +
			"</first-name><last-name>" + xhtml.EscapeText(last) + "</last-name></author>\n")
	}
	out.WriteString("<book-title>" + xhtml.EscapeText(oneLine(b.Meta.Title)) + "</book-title>\n")

	out.WriteString("<annotation>\n")
	for _, line := range TitleLines(b.Meta) {
		if line == "" {
			continue
		}
		out.WriteString("<p>" + xhtml.EscapeText(line) + "</p>\n")
	}
	out.WriteString("</annotation>\n")
	out.WriteString("<lang>" + xhtml.EscapeText(langOf(b)) + "</lang>\n")
	out.WriteString("</title-info>\n<document-info>\n")
	out.WriteString("<author><nickname>proofreader</nickname></author>\n")

	// Схема FB2 задаёт порядок: author, program-used?, date, src-url*,
	// src-ocr?, id, version — date обязателен, src-url перед id и version, а
	// не после. date в title-info — другое дело, там он не обязателен и его
	// нет вовсе; не путать одно с другим.
	modified := b.Meta.Modified
	if modified.IsZero() {
		modified = time.Unix(0, 0).UTC()
	}
	out.WriteString(`<date value="` + modified.UTC().Format("2006-01-02") + `">` +
		xhtml.EscapeText(RussianDate(modified)) + "</date>\n")
	if b.Meta.URL != "" {
		out.WriteString("<src-url>" + xhtml.EscapeText(b.Meta.URL) + "</src-url>\n")
	}
	out.WriteString("<id>" + xhtml.EscapeText(bookUID(b)) + "</id>\n")
	out.WriteString("<version>1.0</version>\n")
	out.WriteString("</document-info>\n</description>\n")
}

// splitName делит «К. Маркс» на имя и фамилию. Последнее слово — фамилия,
// всё перед ним — имя; одно слово — только фамилия.
func splitName(s string) (first, last string) {
	fields := strings.Fields(s)
	switch len(fields) {
	case 0:
		return "", ""
	case 1:
		return "", fields[0]
	default:
		return strings.Join(fields[:len(fields)-1], " "), fields[len(fields)-1]
	}
}

func writeFB2Section(out *strings.Builder, s Section) {
	out.WriteString("<section>\n")
	out.WriteString("<title><p>" + xhtml.EscapeText(oneLine(s.Title)) + "</p></title>\n")
	if s.Author != "" {
		out.WriteString("<subtitle>" + xhtml.EscapeText(oneLine(s.Author)) + "</subtitle>\n")
	}

	// sectionType в схеме FB2 — xs:choice: <section> содержит либо только
	// вложенные <section>, либо только листовое содержимое (p/subtitle/cite/…
	// ), никогда оба сразу. У секции с преамбулой перед первым ребёнком,
	// дырой между соседями или хвостом после последнего (BuildSection,
	// pkg/book/tree.go) блоки перемежаются — 89 глав живого корпуса устроены
	// именно так, не считая того же на верхнем уровне тома. Прогон
	// собственных страниц в этом случае заворачивается в анонимную вложенную
	// <section> без заголовка, чтобы внешняя секция осталась только со
	// вложенными секциями; секция без перемежающихся блоков обёртки не
	// получает вовсе.
	mixed := hasChildBlock(s.Blocks) && hasOwnPagesBlock(s.Blocks)
	for _, block := range s.Blocks {
		if block.Child != nil {
			writeFB2Section(out, *block.Child)
			continue
		}
		if len(block.Pages) == 0 {
			continue
		}
		if mixed {
			out.WriteString("<section>\n")
		}
		for _, p := range block.Pages {
			fmt.Fprintf(out, "<subtitle>[%d]</subtitle>\n", p.Printed)
			out.WriteString(fb2Blocks(p.HTML))
		}
		if mixed {
			out.WriteString("</section>\n")
		}
	}

	out.WriteString("</section>\n")
}

func hasChildBlock(blocks []Block) bool {
	for _, b := range blocks {
		if b.Child != nil {
			return true
		}
	}
	return false
}

func hasOwnPagesBlock(blocks []Block) bool {
	for _, b := range blocks {
		if b.Child == nil && len(b.Pages) > 0 {
			return true
		}
	}
	return false
}

// writeFB2Notes собирает body name="notes" из блоков примечаний всех секций.
// Ссылка type="note", не находящая своей секции, роняет читалки молча, поэтому
// сюда попадает каждый li с якорем.
func writeFB2Notes(out *strings.Builder, b *Book) {
	notes := map[string]noteEntry{}
	var order []string
	for _, s := range b.Sections {
		collectNotes(s, notes, &order)
	}
	if len(order) == 0 {
		return
	}

	out.WriteString(`<body name="notes">` + "\n")
	for _, id := range order {
		e := notes[id]
		out.WriteString(`<section id="` + xhtml.EscapeAttr(fb2ID(id)) + `">` + "\n")
		// Заголовок — маркер, который читатель увидел и нажал в тексте
		// (RenderNotes кладёт его в текст a.fn-back, pkg/markdown/notes.go):
		// книжный номер для сноски, «бегущий» (1)/(2) для подстрочного
		// примечания. Не якорь id: тот — внутренний идентификатор, а с тех
		// пор, как одинаковые номера в разных секциях различают префиксом
		// (disambiguateFootnotes, internal/api/download_source.go), ещё и
		// нечитаемый ("s0-5-1"). Маркер не нашёлся — старая или упрощённая
		// разметка без a.fn-back, а не признак битого примечания — title у
		// section в схеме FB2 необязателен (minOccurs="0"), поэтому в этом
		// случае секция остаётся без заголовка, а не печатает якорь вместо
		// него.
		if e.marker != "" {
			out.WriteString("<title><p>" + xhtml.EscapeText(e.marker) + "</p></title>\n")
		}
		out.WriteString(e.body)
		out.WriteString("</section>\n")
	}
	out.WriteString("</body>\n")
}

func collectNotes(s Section, notes map[string]noteEntry, order *[]string) {
	// Один обход разметки на секцию: маркер, тело и порядок появления вместе,
	// а не тремя отдельными проходами по одному и тому же DOM. Порядок важен
	// сам по себе — перебор map недетерминирован, а от порядка зависит
	// побайтовая воспроизводимость файла.
	for _, e := range parseNoteEntries(s.NotesHTML) {
		if _, seen := notes[e.id]; seen {
			continue
		}
		notes[e.id] = e
		*order = append(*order, e.id)
	}
	for _, b := range s.Blocks {
		if b.Child != nil {
			collectNotes(*b.Child, notes, order)
		}
	}
}

// noteEntry — одно примечание, извлечённое из блока примечаний секции.
type noteEntry struct {
	id     string
	marker string // текст a.fn-back — то, что читатель нажал в тексте; может быть пустым
	body   string
}

// parseNoteEntries разбирает блок примечаний секции по якорям fn:.
func parseNoteEntries(notesHTML string) []noteEntry {
	var out []noteEntry
	if strings.TrimSpace(notesHTML) == "" {
		return out
	}
	doc, err := html.Parse(strings.NewReader(notesHTML))
	if err != nil {
		return out
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if id := attrValue(n, "id"); n.Type == html.ElementNode && strings.HasPrefix(id, "fn:") {
			out = append(out, noteEntry{id: id, marker: noteBackLinkMarker(n), body: noteBodyOf(n)})
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}

// noteBackLinkMarker достаёт из <li> примечания текст обратной ссылки на
// вызов в тексте (a.fn-back) — печатный маркер примечания. Пустой результат
// не ошибка: разметка старше a.fn-back или собрана вручную в тесте.
func noteBackLinkMarker(li *html.Node) string {
	for c := li.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.DataAtom == atom.A && strings.Contains(attrValue(c, "class"), "fn-back") {
			return oneLine(textOf(c))
		}
	}
	return ""
}

// noteBodyOf печатает тело примечания, пропуская саму обратную ссылку —
// её текст уже стал заголовком секции (writeFB2Notes), и второй раз в теле
// как самостоятельный «висячий» абзац ему делать нечего.
//
// Тело — плоский инлайн (текст, <sup> со ссылкой на другое примечание,
// <em>/<strong>), изредка с настоящим блоком внутри (вложенный список,
// таблица), а не последовательность готовых блочных узлов — печатается через
// writeFB2Flow, а не поштучным writeFB2Node, иначе текст с <sup> посередине
// рвётся на несколько <p> (см. writeFB2Flow).
func noteBodyOf(li *html.Node) string {
	var body strings.Builder
	writeFB2Flow(&body, li.FirstChild, "", isFB2NoteBackLink)
	return wrapLooseText(body.String())
}

func isFB2NoteBackLink(n *html.Node) bool {
	return n.Type == html.ElementNode && n.DataAtom == atom.A && strings.Contains(attrValue(n, "class"), "fn-back")
}

// fb2ID чистит якорь: двоеточие в XML-имени зарезервировано под пространства
// имён, и часть читалок на нём спотыкается.
func fb2ID(id string) string { return strings.ReplaceAll(id, ":", "_") }

func attrValue(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// fb2Blocks переводит фрагмент XHTML в блоки FB2.
func fb2Blocks(fragment string) string {
	if strings.TrimSpace(fragment) == "" {
		return ""
	}
	doc, err := html.Parse(strings.NewReader(fragment))
	if err != nil {
		return ""
	}
	body := xhtml.FindBody(doc)
	if body == nil {
		return ""
	}
	var out strings.Builder
	for c := body.FirstChild; c != nil; c = c.NextSibling {
		writeFB2Node(&out, c)
	}
	return wrapLooseText(out.String())
}

// writeFB2Node печатает один блочный узел по таблице соответствий.
func writeFB2Node(out *strings.Builder, n *html.Node) {
	if n.Type == html.TextNode {
		if text := strings.TrimSpace(n.Data); text != "" {
			out.WriteString("<p>" + xhtml.EscapeText(text) + "</p>\n")
		}
		return
	}
	if n.Type != html.ElementNode {
		return
	}

	switch n.DataAtom {
	case atom.P, atom.Dt, atom.Dd:
		// Абзац, занятый одной блочной формулой, становится таблицей или
		// собственным блоком: <table> внутрь <p> класть нельзя.
		if tex, ok := displayMathOnly(n); ok {
			writeFB2Math(out, tex)
			return
		}
		writeFB2Paragraphs(out, n, "")
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		out.WriteString("<subtitle>" + xhtml.EscapeText(oneLine(textOf(n))) + "</subtitle>\n")
	case atom.Blockquote:
		out.WriteString("<cite>\n")
		writeFB2Children(out, n)
		out.WriteString("</cite>\n")
	case atom.Li:
		// FB2 не знает списков — честная потеря, записанная в спеке.
		writeFB2ListItem(out, n)
	case atom.Table:
		writeFB2Table(out, n)
	case atom.Img, atom.Script, atom.Style:
		// Картинки в архив не кладутся, ссылка на них повисла бы.
		return
	case atom.Hr:
		out.WriteString("<empty-line/>\n")
	case atom.Pre:
		out.WriteString("<p>" + xhtml.EscapeText(textOf(n)) + "</p>\n")
	default:
		writeFB2Children(out, n)
	}
}

func writeFB2Children(out *strings.Builder, n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		writeFB2Node(out, c)
	}
}

// writeFB2Paragraphs печатает абзац, разрезая его по <br/>: в корпусе так
// размечены стихи, и склейка строк превратила бы их в прозу.
func writeFB2Paragraphs(out *strings.Builder, n *html.Node, prefix string) {
	var inline strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		writeFB2Inline(&inline, c)
	}
	for _, part := range strings.Split(inline.String(), lineBreak) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out.WriteString("<p>" + prefix + part + "</p>\n")
	}
}

// writeFB2ListItem печатает пункт списка. Вложенный список внутри пункта —
// свой блок, а не хвост той же строки: сборка через writeFB2Inline (она не
// знает тегов ul/ol/li и просто спускается в текст) склеила бы текст пункта
// с текстом вложенного без разделителя — «раз» + «вложенный» слиплись бы в
// «развложенный». writeFB2Flow сбрасывает накопленный до вложенного списка
// текст своим абзацем и печатает вложенный <ul>/<ol> обычным обходом — каждый
// его <li> получит свой маркер.
func writeFB2ListItem(out *strings.Builder, n *html.Node) {
	writeFB2Flow(out, n.FirstChild, "• ", nil)
}

// isFB2BlockNode сообщает, требует ли узел собственного блочного элемента FB2
// (список, таблица, цитата, свой <p>…), а не идёт в накопленный инлайновый
// абзац writeFB2Flow.
func isFB2BlockNode(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch n.DataAtom {
	case atom.P, atom.Dt, atom.Dd, atom.Ul, atom.Ol, atom.Table, atom.Blockquote,
		atom.Pre, atom.Hr, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		return true
	}
	return false
}

// writeFB2Flow печатает последовательность соседних узлов, начиная с first, —
// инлайновый текст вперемешку с редкими настоящими блоками (список, таблица).
// И тело примечания (noteBodyOf), и пункт списка (writeFB2ListItem) устроены
// так: печать каждого узла по отдельности через блочный writeFB2Node рвала
// одно предложение на несколько <p> всякий раз, когда посреди текста
// попадался инлайновый элемент вроде <sup> со ссылкой на другое примечание —
// на одном томе так рвались 312 абзацев. Инлайновый прогон копится и
// сбрасывается одним <p> (с prefix перед каждой его строкой — "" для обычного
// текста, "• " для пункта списка), разрезанным по <br/> как обычно; настоящий
// блок печатается сам по себе через writeFB2Node, минуя накопитель. skip,
// если не nil, отбрасывает узел целиком — так noteBodyOf прячет обратную
// ссылку на вызов в тексте, чей текст уже стал заголовком секции.
func writeFB2Flow(out *strings.Builder, first *html.Node, prefix string, skip func(*html.Node) bool) {
	var inline strings.Builder
	flush := func() {
		for _, part := range strings.Split(inline.String(), lineBreak) {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			out.WriteString("<p>" + prefix + part + "</p>\n")
		}
		inline.Reset()
	}
	for c := first; c != nil; c = c.NextSibling {
		if skip != nil && skip(c) {
			continue
		}
		if isFB2BlockNode(c) {
			flush()
			writeFB2Node(out, c)
			continue
		}
		writeFB2Inline(&inline, c)
	}
	flush()
}

// writeFB2Inline печатает строчную разметку внутри абзаца.
func writeFB2Inline(out *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		out.WriteString(xhtml.EscapeText(n.Data))
		return
	case html.ElementNode:
	default:
		return
	}

	switch n.DataAtom {
	case atom.Br:
		out.WriteString(lineBreak)
		return
	case atom.Img, atom.Script, atom.Style:
		return
	case atom.Span:
		if tex, display, ok := mathSpanTeX(n); ok {
			var inner strings.Builder
			node, err := mathNode(tex, display)
			if err == nil {
				if pres := mathPresentation(node); pres != nil && fb2MathInline(&inner, pres) {
					out.WriteString(inner.String())
					return
				}
			}
			// Понижение не вышло — печатаем исходный TeX, а не половину формулы.
			out.WriteString(xhtml.EscapeText(tex))
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			writeFB2Inline(out, c)
		}
	case atom.Annotation:
		// Внутри <semantics> рядом с разметкой лежит исходный TeX. Печатать
		// его вслед за разметкой значит показать формулу дважды — ровно этот
		// дефект и превращал «c+v» в «c+vc+v».
		return
	case atom.Em, atom.I:
		wrapInline(out, n, "emphasis")
	case atom.Strong, atom.B:
		wrapInline(out, n, "strong")
	case atom.Sup:
		wrapInline(out, n, "sup")
	case atom.Sub:
		wrapInline(out, n, "sub")
	case atom.Code, atom.Kbd, atom.Samp:
		wrapInline(out, n, "code")
	case atom.Strike, atom.S, atom.Del:
		wrapInline(out, n, "strikethrough")
	case atom.A:
		writeFB2Link(out, n)
	default:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			writeFB2Inline(out, c)
		}
	}
}

func wrapInline(out *strings.Builder, n *html.Node, tag string) {
	out.WriteString("<" + tag + ">")
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		writeFB2Inline(out, c)
	}
	out.WriteString("</" + tag + ">")
}

func writeFB2Link(out *strings.Builder, n *html.Node) {
	href := attrValue(n, "href")
	var inner strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		writeFB2Inline(&inner, c)
	}

	switch {
	case strings.HasPrefix(href, "#"):
		// Внутренняя ссылка — это сноска. type="note" заставляет читалку
		// открыть её окошком вместо перехода в конец книги.
		out.WriteString(`<a l:href="#` + xhtml.EscapeAttr(fb2ID(strings.TrimPrefix(href, "#"))) +
			`" type="note">` + inner.String() + "</a>")
	case href != "":
		out.WriteString(`<a l:href="` + xhtml.EscapeAttr(href) + `">` + inner.String() + "</a>")
	default:
		out.WriteString(inner.String())
	}
}

func writeFB2Table(out *strings.Builder, n *html.Node) {
	out.WriteString("<table>\n")
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.DataAtom {
			case atom.Tr:
				out.WriteString("<tr>")
				for cell := c.FirstChild; cell != nil; cell = cell.NextSibling {
					if cell.Type != html.ElementNode {
						continue
					}
					tag := "td"
					if cell.DataAtom == atom.Th {
						tag = "th"
					}
					var inner strings.Builder
					for x := cell.FirstChild; x != nil; x = x.NextSibling {
						writeFB2Inline(&inner, x)
					}
					out.WriteString("<" + tag + ">" +
						strings.ReplaceAll(inner.String(), lineBreak, " ") + "</" + tag + ">")
				}
				out.WriteString("</tr>\n")
			default:
				walk(c)
			}
		}
	}
	walk(n)
	out.WriteString("</table>\n")
}

// textOf собирает голый текст поддерева — для заголовков, где разметка не нужна.
func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			b.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// wrapLooseText страхует от пустой секции: FB2 требует, чтобы в section было
// хоть что-то, а страница-заглушка может не дать ни одного блока.
func wrapLooseText(s string) string {
	if strings.TrimSpace(s) == "" {
		return "<empty-line/>\n"
	}
	return s
}
