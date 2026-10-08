package book

import (
	"archive/zip"
	"fmt"
	"io"
	"strings"
	"time"

	"proofreader/pkg/xhtml"
)

// EPUBWriter собирает книгу в EPUB 3 — формат читалок и телефонов.
type EPUBWriter struct{}

func (EPUBWriter) ContentType() string { return "application/epub+zip" }
func (EPUBWriter) Ext() string         { return "epub" }

func (EPUBWriter) Write(w io.Writer, b *Book) error {
	z := zip.NewWriter(w)

	// mimetype обязан быть первой записью архива и лежать без сжатия. При
	// нарушении получается совершенно валидный zip, который часть читалок
	// отказывается открывать, — и понять причину по сообщению невозможно.
	mimetype, err := z.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		return fmt.Errorf("mimetype: %w", err)
	}
	if _, err := io.WriteString(mimetype, "application/epub+zip"); err != nil {
		return fmt.Errorf("mimetype: %w", err)
	}

	// Один XHTML на верхнеуровневую секцию: том на 907 страниц одной
	// простынёй кладёт читалки на телефоне. Секции собираются раньше
	// манифеста: манифест обязан знать, в каких из них оказался MathML.
	type sectionFile struct {
		name    string
		content string
	}
	sections := make([]sectionFile, 0, len(b.Sections))
	mathSections := make([]bool, len(b.Sections))
	for i, s := range b.Sections {
		content := epubSection(s, langOf(b), sectionAnchor(i))
		mathSections[i] = strings.Contains(content, "<math")
		sections = append(sections, sectionFile{
			name:    "OEBPS/" + sectionAnchor(i) + ".xhtml",
			content: content,
		})
	}

	files := []struct{ name, content string }{
		{"META-INF/container.xml", containerXML},
		{"OEBPS/style.css", readingCSS},
		{"OEBPS/title.xhtml", epubTitlePage(b)},
		{"OEBPS/nav.xhtml", epubNav(b)},
		{"OEBPS/toc.ncx", epubNCX(b)},
		{"OEBPS/content.opf", epubOPF(b, mathSections)},
	}
	for _, s := range sections {
		files = append(files, struct{ name, content string }{name: s.name, content: s.content})
	}

	for _, f := range files {
		// Метка времени в заголовке записи не выставляется: zip.Writer пишет
		// нулевую, и архив остаётся побайтово воспроизводимым.
		wr, err := z.CreateHeader(&zip.FileHeader{Name: f.name, Method: zip.Deflate})
		if err != nil {
			return fmt.Errorf("%s: %w", f.name, err)
		}
		if _, err := io.WriteString(wr, f.content); err != nil {
			return fmt.Errorf("%s: %w", f.name, err)
		}
	}

	return z.Close()
}

const containerXML = `<?xml version="1.0" encoding="utf-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>
`

// xhtmlDoc заворачивает тело в документ EPUB-совместимого XHTML.
func xhtmlDoc(lang, title, body string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" xml:lang="` +
		xhtml.EscapeAttr(lang) + `">
<head>
<meta charset="utf-8"/>
<title>` + xhtml.EscapeText(title) + `</title>
<link rel="stylesheet" type="text/css" href="style.css"/>
</head>
<body>
` + body + `</body>
</html>
`
}

func epubTitlePage(b *Book) string {
	var body strings.Builder
	body.WriteString(`<section class="titlepage" epub:type="titlepage">` + "\n")
	for _, line := range TitleLines(b.Meta) {
		if line == "" {
			continue
		}
		body.WriteString("<p>" + xhtml.EscapeText(line) + "</p>\n")
	}
	body.WriteString("</section>\n")
	return xhtmlDoc(langOf(b), oneLine(b.Meta.Title), body.String())
}

// epubSection рендерит верхнеуровневую секцию в свой файл. anchor — якорь
// файла (sectionAnchor(i)): вложенные секции получают от него свои id по той
// же схеме childAnchor, что и оглавление, — иначе ссылки nav.xhtml/toc.ncx на
// них ведут в никуда.
func epubSection(s Section, lang, anchor string) string {
	var body strings.Builder
	writeHTMLSection(&body, s, 1, anchor)
	// Сноски размечаются epub:type — тогда Apple Books и Thorium показывают их
	// всплывашкой, а не уводят читателя в конец книги. Разметка нужна с обеих
	// сторон: epub:type="footnote" на самом примечании и epub:type="noteref"
	// на ссылке-вызове в тексте — читалки решают, показывать ли всплывашку,
	// именно по ссылке.
	content := strings.ReplaceAll(body.String(),
		`<div class="footnotes">`, `<div class="footnotes" epub:type="footnotes">`)
	content = strings.ReplaceAll(content, `class="fn-item`, `epub:type="footnote" class="fn-item`)
	// Ссылка-вызов сноски — это <a href="#fn:...">, отличается от обратной
	// ссылки в теле примечания (<a class="fn-back" href="#fnref:...">) тем,
	// что префикс "#fn:" не совпадает с "#fnref:" ни в одну сторону.
	content = strings.ReplaceAll(content, `<a href="#fn:`, `<a epub:type="noteref" href="#fn:`)
	return xhtmlDoc(lang, oneLine(s.Title), content)
}

func epubNav(b *Book) string {
	var body strings.Builder
	body.WriteString(`<nav epub:type="toc" id="toc"><h1>Содержание</h1>` + "\n<ol>\n")
	for i, s := range b.Sections {
		anchor := sectionAnchor(i)
		file := anchor + ".xhtml"
		body.WriteString(`<li><a href="` + file + `">` + xhtml.EscapeText(oneLine(s.Title)) + "</a>")
		writeNavChildren(&body, s, file, anchor)
		body.WriteString("</li>\n")
	}
	body.WriteString("</ol>\n</nav>\n")
	return xhtmlDoc(langOf(b), "Содержание", body.String())
}

// writeNavChildren печатает вложенные пункты оглавления как ссылки на якорь
// внутри файла верхнеуровневой секции (file#childAnchor), а не как ссылку на
// сам файл: до правки все вложенные пункты вели в начало файла — 1260 из
// 2305 пунктов оглавления корпуса вели не туда, куда обещали. file не
// меняется по всей рекурсии — вложенная секция живёт в файле своего
// верхнеуровневого предка; anchor — якорь секции s, от которого по
// childAnchor строится якорь каждого её ребёнка, той же схемой, что и id в
// writeHTMLSection.
func writeNavChildren(body *strings.Builder, s Section, file, anchor string) {
	var children []Section
	for _, blk := range s.Blocks {
		if blk.Child != nil {
			children = append(children, *blk.Child)
		}
	}
	if len(children) == 0 {
		return
	}
	body.WriteString("\n<ol>\n")
	for i, c := range children {
		childA := childAnchor(anchor, i+1)
		body.WriteString(`<li><a href="` + file + "#" + childA + `">` + xhtml.EscapeText(oneLine(c.Title)) + "</a>")
		writeNavChildren(body, c, file, childA)
		body.WriteString("</li>\n")
	}
	body.WriteString("</ol>\n")
}

// epubNCX — оглавление для читалок, понимающих только EPUB 2. Спекой EPUB 3
// не требуется, но старых читалок в обиходе достаточно.
//
// Рекурсивная: вложенные секции печатаются как navPoint внутри navPoint (NCX
// это допускает), а не только верхний уровень — иначе EPUB 2 читалка видит
// 1045 пунктов из 2305, теряя все вложенные главы корпуса. playOrder — общий
// счётчик на весь документ, а не на каждый уровень отдельно: часть читалок
// использует его для «вперёд/назад» по оглавлению, и порядок обязан быть
// монотонным по всему дереву.
func epubNCX(b *Book) string {
	var out strings.Builder
	out.WriteString(`<?xml version="1.0" encoding="utf-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
<head><meta name="dtb:uid" content="` + xhtml.EscapeAttr(bookUID(b)) + `"/></head>
<docTitle><text>` + xhtml.EscapeText(oneLine(b.Meta.Title)) + `</text></docTitle>
<navMap>
`)
	playOrder := 0
	for i, s := range b.Sections {
		anchor := sectionAnchor(i)
		file := anchor + ".xhtml"
		writeNCXPoint(&out, s, file, anchor, file, &playOrder)
	}
	out.WriteString("</navMap>\n</ncx>\n")
	return out.String()
}

// writeNCXPoint печатает navPoint для секции s (src — куда он ведёт: файл
// целиком у секции верхнего уровня, файл с фрагментом у вложенной) и
// рекурсивно — для её дочерних секций, той же схемой якорей childAnchor, что
// и nav.xhtml/writeHTMLSection.
func writeNCXPoint(out *strings.Builder, s Section, file, anchor, src string, playOrder *int) {
	*playOrder++
	fmt.Fprintf(out, `<navPoint id="nav-%s" playOrder="%d"><navLabel><text>%s</text></navLabel><content src="%s"/>`+"\n",
		anchor, *playOrder, xhtml.EscapeText(oneLine(s.Title)), src)

	childIdx := 0
	for _, blk := range s.Blocks {
		if blk.Child == nil {
			continue
		}
		childIdx++
		childA := childAnchor(anchor, childIdx)
		writeNCXPoint(out, *blk.Child, file, childA, file+"#"+childA, playOrder)
	}
	out.WriteString("</navPoint>\n")
}

func epubOPF(b *Book, mathSections []bool) string {
	var out strings.Builder
	out.WriteString(`<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid">
<metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
<dc:identifier id="bookid">` + xhtml.EscapeText(bookUID(b)) + `</dc:identifier>
<dc:title>` + xhtml.EscapeText(oneLine(b.Meta.Title)) + `</dc:title>
<dc:language>` + xhtml.EscapeText(langOf(b)) + `</dc:language>
`)
	for _, a := range b.Meta.Authors {
		out.WriteString("<dc:creator>" + xhtml.EscapeText(oneLine(a)) + "</dc:creator>\n")
	}
	// joinSourceParts (title.go) — та же склейка, что и на титульной странице:
	// издание в живой базе часто уже оканчивается точкой ("...Сочинения, 2-е
	// изд."), и наивное "Edition + ". " + Volume" даёт двойную точку у 21 из
	// 22 работ. При отсутствии тома (или издания) склейка не оставляет
	// висячего ". " — те же непустые части, что в TitleLines.
	var sourceParts []string
	if b.Meta.Edition != "" {
		sourceParts = append(sourceParts, b.Meta.Edition)
	}
	if b.Meta.Volume != "" {
		sourceParts = append(sourceParts, b.Meta.Volume)
	}
	if len(sourceParts) > 0 {
		out.WriteString("<dc:source>" + xhtml.EscapeText(oneLine(joinSourceParts(sourceParts))) + "</dc:source>\n")
	}
	// dcterms:modified обязателен по спеке EPUB 3. Берётся max(updated_at)
	// страниц, а не время сборки: файл остаётся побайтово воспроизводимым.
	out.WriteString(`<meta property="dcterms:modified">` + modifiedStamp(b) + "</meta>\n")
	out.WriteString("</metadata>\n<manifest>\n")
	out.WriteString(`<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>` + "\n")
	out.WriteString(`<item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>` + "\n")
	out.WriteString(`<item id="css" href="style.css" media-type="text/css"/>` + "\n")
	out.WriteString(`<item id="title" href="title.xhtml" media-type="application/xhtml+xml"/>` + "\n")
	for i := range b.Sections {
		id := sectionAnchor(i)
		// MathML в EPUB 3 обязан быть объявлен в манифесте. Помечаем ровно те
		// секции, где он действительно оказался: неиспользованное объявление
		// epubcheck считает ошибкой наравне с необъявленным использованием.
		props := ""
		if i < len(mathSections) && mathSections[i] {
			props = ` properties="mathml"`
		}
		fmt.Fprintf(&out, `<item id="%s" href="%s.xhtml" media-type="application/xhtml+xml"%s/>`+"\n", id, id, props)
	}
	out.WriteString(`</manifest>
<spine toc="ncx">
<itemref idref="title"/>
`)
	for i := range b.Sections {
		fmt.Fprintf(&out, `<itemref idref="%s"/>`+"\n", sectionAnchor(i))
	}
	out.WriteString("</spine>\n</package>\n")
	return out.String()
}

// bookUID — устойчивый идентификатор книги. Ссылка на читальню годится: она
// уникальна и не меняется от сборки к сборке.
func bookUID(b *Book) string {
	if b.Meta.URL != "" {
		return b.Meta.URL
	}
	return "urn:proofreader:" + oneLine(b.Meta.Title)
}

func modifiedStamp(b *Book) string {
	if b.Meta.Modified.IsZero() {
		return "1970-01-01T00:00:00Z"
	}
	return b.Meta.Modified.UTC().Format(time.RFC3339)
}
