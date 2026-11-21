// PDF: каждая исходная полоса занимает ровно один лист A4.
package book

import (
	"embed"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-pdf/fpdf"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"proofreader/internal/site"
	"proofreader/pkg/markdown"
)

type tocItem struct {
	Title     string
	PageIndex int
	Depth     int
}

type run struct {
	Text    string
	Style   string
	Target  string
	Anchor  string
	SupKind string
}

type contentBlock struct {
	Kind     string
	ID       string
	Runs     []run
	NoteKind string
	Caption  bool
	Cells    []tableCell
	Cols     int
}

type tableCell struct {
	Text string
	Runs []run
}

type movedNotes struct {
	Page   int
	Source string
	Blocks []contentBlock
}

var spaceRe = regexp.MustCompile(`\s+`)
var texCmdRe = regexp.MustCompile(`\\(?:textit|textbf|text|mathrm)\{([^{}]*)\}`)
var texFracRe = regexp.MustCompile(`\\frac\{([^{}]*)\}\{([^{}]*)\}`)
var texOversetRe = regexp.MustCompile(`\\overset\{([^{}]*)\}\{([^{}]*)\}`)
var texOverbraceRe = regexp.MustCompile(`\\overbrace\{([^{}]*)\}`)

func attribute(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func pdfTextOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.ElementNode && x.Data == "br" {
			b.WriteByte(' ')
			return
		}
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return spaceRe.ReplaceAllString(b.String(), " ")
}

// readableMath сохраняет формулу текстом, когда графический набор TeX
// недоступен. Неизвестные команды остаются видимыми, а не пропадают.
func readableMath(s string) string {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "\\("), "\\[")
	s = strings.TrimSuffix(strings.TrimSuffix(s, "\\)"), "\\]")
	for i := 0; i < 8; i++ {
		before := s
		s = texCmdRe.ReplaceAllString(s, "$1")
		s = texFracRe.ReplaceAllString(s, "($1)/($2)")
		s = texOversetRe.ReplaceAllString(s, "$2^$1")
		s = texOverbraceRe.ReplaceAllString(s, "($1)")
		if s == before {
			break
		}
	}
	s = strings.ReplaceAll(s, `\times`, "×")
	s = strings.ReplaceAll(s, `\quad`, "  ")
	s = strings.ReplaceAll(s, `\left`, "")
	s = strings.ReplaceAll(s, `\right`, "")
	s = strings.ReplaceAll(s, `\%`, "%")
	s = strings.ReplaceAll(s, `\begin{cases}`, "{")
	s = strings.ReplaceAll(s, `\end{cases}`, "}")
	s = strings.ReplaceAll(s, `\\`, "; ")
	s = strings.ReplaceAll(s, "{", "")
	s = strings.ReplaceAll(s, "}", "")
	return strings.TrimSpace(s)
}

func collectRuns(n *html.Node, style, link, supKind string, out *[]run) {
	if n.Type == html.TextNode {
		s := spaceRe.ReplaceAllString(n.Data, " ")
		if s != "" {
			*out = append(*out, run{Text: s, Style: style, Target: link, SupKind: supKind})
		}
		return
	}
	if n.Type != html.ElementNode {
		return
	}
	if id := attribute(n, "id"); strings.HasPrefix(id, "fnref:") {
		*out = append(*out, run{Anchor: id})
	}
	if n.Data == "table" {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			collectRuns(c, style, link, supKind, out)
		}
		return
	}
	if n.Data == "tr" {
		first := true
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode || (c.Data != "td" && c.Data != "th") {
				continue
			}
			if !first {
				*out = append(*out, run{Text: " | "})
			}
			collectRuns(c, style, link, supKind, out)
			first = false
		}
		*out = append(*out, run{Text: "\n"})
		return
	}
	switch n.Data {
	case "strong", "b":
		if !strings.Contains(style, "B") {
			style += "B"
		}
	case "em", "i":
		if !strings.Contains(style, "I") {
			style += "I"
		}
	case "a":
		if h := attribute(n, "href"); strings.HasPrefix(h, "#") {
			link = strings.TrimPrefix(h, "#")
		}
	case "sup":
		classes := attribute(n, "class")
		if strings.Contains(classes, "footnote-ref--subscript") {
			supKind = "subscript"
		} else if strings.Contains(classes, "footnote-ref--endnote") {
			supKind = "endnote"
		}
	case "span":
		if strings.Contains(attribute(n, "class"), "math") {
			*out = append(*out, run{Text: readableMath(pdfTextOf(n)), Style: style, SupKind: supKind})
			return
		}
	case "img":
		*out = append(*out, run{Text: attribute(n, "alt"), Style: style, SupKind: supKind})
		return
	case "br":
		*out = append(*out, run{Text: "\n", Style: style, SupKind: supKind})
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectRuns(c, style, link, supKind, out)
	}
}

func blocksFrom(fragment string) ([]contentBlock, error) {
	ctx := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), ctx)
	if err != nil {
		return nil, err
	}
	var out []contentBlock
	var walk func(*html.Node, string)
	walk = func(n *html.Node, noteKind string) {
		if n.Type != html.ElementNode {
			return
		}
		kind := n.Data
		if kind == "section" {
			classes := attribute(n, "class")
			if strings.Contains(classes, "notes-group--endnote") {
				noteKind = "endnote"
			} else if strings.Contains(classes, "notes-group--subscript") {
				noteKind = "subscript"
			}
		}
		if kind == "table" {
			var rows []*html.Node
			var findRows func(*html.Node)
			findRows = func(x *html.Node) {
				if x.Type == html.ElementNode && x.Data == "tr" {
					rows = append(rows, x)
					return
				}
				for c := x.FirstChild; c != nil; c = c.NextSibling {
					findRows(c)
				}
			}
			findRows(n)
			var table []contentBlock
			cols := 0
			for _, row := range rows {
				var cells []tableCell
				for c := row.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
						var runs []run
						collectRuns(c, "", "", "", &runs)
						cells = append(cells, tableCell{Text: strings.TrimSpace(pdfTextOf(c)), Runs: runs})
					}
				}
				if len(cells) > cols {
					cols = len(cells)
				}
				table = append(table, contentBlock{Kind: "table", Cells: cells, NoteKind: noteKind})
			}
			for i := range table {
				table[i].Cols = cols
			}
			out = append(out, table...)
			return
		}
		if kind == "p" || kind == "li" || kind == "pre" || kind == "blockquote" || kind == "dt" || kind == "dd" ||
			(len(kind) == 2 && kind[0] == 'h' && kind[1] >= '1' && kind[1] <= '6') {
			var runs []run
			collectRuns(n, "", "", "", &runs)
			if kind == "li" && !strings.Contains(attribute(n, "class"), "fn-item") {
				runs = append([]run{{Text: "•  "}}, runs...)
			}
			out = append(out, contentBlock{Kind: kind, ID: attribute(n, "id"), Runs: runs,
				NoteKind: noteKind, Caption: strings.Contains(attribute(n, "class"), "notes-group-caption")})
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, noteKind)
		}
	}
	for _, n := range nodes {
		walk(n, "")
	}
	return out, nil
}

//go:embed pdf_font/*.ttf
var pdfFonts embed.FS

func newPDF(meta Meta) *fpdf.Fpdf {
	p := fpdf.New("P", "mm", "A4", "")
	p.SetMargins(18, 14, 18)
	p.SetAutoPageBreak(false, 0)
	for _, f := range []struct{ style, file string }{
		{"", "DejaVuSerif.ttf"}, {"B", "DejaVuSerif-Bold.ttf"},
		{"I", "DejaVuSerif-Italic.ttf"}, {"BI", "DejaVuSerif-BoldItalic.ttf"},
	} {
		data, err := pdfFonts.ReadFile("pdf_font/" + f.file)
		if err != nil {
			p.SetError(err)
			return p
		}
		p.AddUTF8FontFromBytes("Serif", f.style, data)
	}
	p.SetTitle(meta.Title, true)
	p.SetAuthor(strings.Join(meta.Authors, ", "), true)
	d := meta.Modified
	if d.IsZero() {
		d = time.Unix(0, 0).UTC()
	}
	p.SetCreationDate(d.UTC())
	p.SetModificationDate(d.UTC())
	p.SetCatalogSort(true)
	return p
}

// fpdf.Write converts the whole UTF-8 input to []rune for each character.
// Keeping calls short avoids quadratic work on long paragraphs and notes.
func writeText(p *fpdf.Fpdf, line float64, s string, linkID int) {
	write := func(part string) {
		if linkID != 0 {
			p.WriteLinkID(line, part, linkID)
		} else {
			p.Write(line, part)
		}
	}
	const maxRunes = 32
	if utf8.RuneCountInString(s) <= maxRunes {
		write(s)
		return
	}
	runes := []rune(s)
	for start := 0; start < len(runes); {
		end := start + maxRunes
		if end >= len(runes) {
			write(string(runes[start:]))
			break
		}
		cut := end
		for cut > start && runes[cut-1] != ' ' && runes[cut-1] != '\n' {
			cut--
		}
		if cut == start {
			cut = end
			for cut < len(runes) && runes[cut-1] != ' ' && runes[cut-1] != '\n' {
				cut++
			}
		}
		write(string(runes[start:cut]))
		start = cut
	}
}

func writeNoteMarker(p *fpdf.Fpdf, line, fontPt float64, r run, linkID int) {
	style := r.Style
	if !strings.Contains(style, "B") {
		style += "B"
	}
	p.SetFont("Serif", style, fontPt*0.72)
	red, green, blue := p.GetTextColor()
	if r.SupKind == "subscript" {
		p.SetTextColor(150, 84, 32)
	} else {
		p.SetTextColor(29, 78, 142)
	}
	x, y := p.GetXY()
	if x+p.GetStringWidth(r.Text) > 192-2*p.GetCellMargin() {
		x, y = 18, y+line
	}
	p.SetXY(x, y-fontPt*0.35278*0.36)
	writeText(p, line, r.Text, linkID)
	p.SetXY(p.GetX(), y)
	p.SetTextColor(red, green, blue)
}

func drawPage(p *fpdf.Fpdf, n int, source string, blocks []contentBlock, pt float64, targets map[string]int) float64 {
	p.AddPage()
	p.SetFont("Serif", "", 8)
	p.SetXY(18, 13)
	if source != "" {
		p.CellFormat(145, 4, fitHeader(p, source, 145), "", 0, "L", false, 0, "")
		p.SetXY(163, 13)
	}
	p.CellFormat(29, 4, strconv.Itoa(n), "", 0, "R", false, 0, "")
	p.SetXY(18, 22)
	return drawBlocks(p, blocks, pt, targets)
}

func fitHeader(p *fpdf.Fpdf, source string, width float64) string {
	if p.GetStringWidth(source) <= width {
		return source
	}
	runes := []rune(source)
	for len(runes) > 0 && p.GetStringWidth(string(runes)+"…") > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

func drawBlocks(p *fpdf.Fpdf, blocks []contentBlock, pt float64, targets map[string]int) float64 {
	start := p.GetY()
	for _, b := range blocks {
		if b.ID != "" && targets[b.ID] != 0 {
			p.SetLink(targets[b.ID], p.GetY(), -1)
		}
		if b.Kind == "table" {
			drawTableRow(p, b, pt, targets)
			continue
		}
		if len(b.Runs) == 0 {
			continue
		}
		mult := 1.0
		gap := 0.48
		if strings.HasPrefix(b.Kind, "h") {
			mult = 1.24
			gap = 0.8
		}
		if b.Kind == "li" {
			gap = 0.26
		}
		if b.Kind == "table" {
			mult = 0.92
			gap = 0.22
		}
		if b.Kind == "pre" {
			mult = 0.9
		}
		line := pt * mult * 0.35278 * 1.26
		p.SetX(18)
		for _, r := range b.Runs {
			if r.Anchor != "" && targets[r.Anchor] != 0 {
				p.SetLink(targets[r.Anchor], p.GetY(), -1)
			}
			if r.Text == "" {
				continue
			}
			if r.SupKind != "" {
				writeNoteMarker(p, line, pt*mult, r, targets[r.Target])
			} else {
				p.SetFont("Serif", r.Style, pt*mult)
				writeText(p, line, r.Text, targets[r.Target])
			}
		}
		p.Ln(line + pt*0.35278*gap)
	}
	return p.GetY() - start
}

func writeTOC(p *fpdf.Fpdf, title string, toc []tocItem, pages []pdfPage, links []int, appendixLink int) {
	p.AddPage()
	p.Bookmark("Содержание", 0, 22)
	p.SetFont("Serif", "B", 16)
	p.SetXY(18, 25)
	for _, line := range p.SplitText(title, 174) {
		p.CellFormat(174, 8, line, "", 1, "", false, 0, "")
	}
	p.Ln(5)
	p.SetFont("Serif", "B", 15)
	p.CellFormat(174, 7, "Содержание", "", 1, "", false, 0, "")
	p.Ln(4)
	for _, item := range toc {
		indent := math.Min(float64(item.Depth), 4) * 3
		p.SetFont("Serif", "", 10.2)
		lines := p.SplitText(item.Title, 154-indent)
		if len(lines) == 0 {
			lines = []string{item.Title}
		}
		const lineHeight = 6.2
		if p.GetY()+float64(len(lines))*lineHeight+1.2 > 277 {
			p.AddPage()
			p.SetXY(18, 20)
		}
		linkID := links[item.PageIndex]
		for i, line := range lines {
			p.SetX(18 + indent)
			p.CellFormat(158-indent, lineHeight, line, "", 0, "", false, linkID, "")
			if i == len(lines)-1 {
				p.CellFormat(12, lineHeight, strconv.Itoa(pages[item.PageIndex].Printed), "", 1, "R", false, linkID, "")
			} else {
				p.Ln(lineHeight)
			}
		}
		p.Ln(1.2)
	}
	if appendixLink != 0 {
		if p.GetY()+8 > 277 {
			p.AddPage()
			p.SetXY(18, 20)
		}
		p.SetFont("Serif", "", 10.2)
		p.SetX(18)
		p.CellFormat(170, 7, "Примечания", "", 1, "", false, appendixLink, "")
	}
}

func splitEndnotes(blocks []contentBlock) (body, notes []contentBlock) {
	for _, b := range blocks {
		if b.NoteKind == "endnote" {
			if !b.Caption {
				notes = append(notes, b)
			}
		} else {
			body = append(body, b)
		}
	}
	return body, notes
}

func writeAppendix(p *fpdf.Fpdf, groups []movedNotes, targets map[string]int, link int) int {
	if len(groups) == 0 {
		return 0
	}
	before := p.PageNo()
	p.SetAutoPageBreak(true, 18)
	p.SetHeaderFunc(func() {
		p.SetFont("Serif", "", 8)
		p.SetXY(18, 13)
		p.Cell(0, 4, "Примечания")
		p.SetXY(18, 22)
	})
	p.AddPage()
	p.SetLink(link, 22, p.PageNo())
	p.Bookmark("Примечания", 0, 22)
	for _, group := range groups {
		if p.GetY() > 258 {
			p.AddPage()
		}
		p.Bookmark(fmt.Sprintf("К странице %d %s", group.Page, group.Source), 1, p.GetY())
		p.SetFont("Serif", "B", 11)
		p.SetX(18)
		p.CellFormat(174, 7, fmt.Sprintf("Примечания к странице %d %s", group.Page, group.Source), "", 1, "", false, 0, "")
		p.Ln(1)
		for _, b := range group.Blocks {
			if p.GetY() > 262 {
				p.AddPage()
			}
			drawBlocks(p, []contentBlock{b}, 9, targets)
		}
		p.Ln(3)
	}
	return p.PageNo() - before
}

// pdfPage keeps the ordinal position separate from the printed number. A
// collection may contain two sources with the same printed page number.
type pdfPage struct {
	Printed  int
	Markdown string
	Source   string
}

type pdfPlan struct {
	pages []pdfPage
	toc   []tocItem
}

func planPDF(b *Book) pdfPlan {
	var plan pdfPlan
	var visit func(Section, int, string)
	visit = func(s Section, depth int, source string) {
		start := len(plan.pages)
		tocAt := len(plan.toc)
		title := s.Title
		if depth == 0 && b.Meta.PageFrom == 0 && b.Meta.PageTo == 0 {
			title = source
		}
		plan.toc = append(plan.toc, tocItem{Title: title, PageIndex: start, Depth: depth})
		for _, block := range s.Blocks {
			if block.Child != nil {
				visit(*block.Child, depth+1, source)
				continue
			}
			for _, page := range block.Pages {
				markdown := page.Markdown
				if page.RawMarkdown != "" {
					markdown = page.RawMarkdown
				}
				plan.pages = append(plan.pages, pdfPage{Printed: page.Printed, Markdown: markdown, Source: source})
			}
		}
		if len(plan.pages) == start {
			plan.toc = append(plan.toc[:tocAt], plan.toc[tocAt+1:]...)
		}
	}
	for i, section := range b.Sections {
		source := ""
		if b.Meta.PageFrom == 0 && b.Meta.PageTo == 0 {
			source = fmt.Sprintf("%d. %s", i+1, section.Title)
			if section.Author != "" {
				source += " — " + section.Author
			}
		} else if len(b.Sections) > 1 {
			source = section.Title
		}
		visit(section, 0, source)
	}
	return plan
}

var tablePartRe = regexp.MustCompile(`\s+|\S+`)

type tableToken struct {
	Text, Style, SupKind, Target, Anchor string
	X, Width                             float64
	Line                                 int
}

func layoutTableCell(p *fpdf.Fpdf, cell tableCell, width, font float64) ([]tableToken, int) {
	var tokens []tableToken
	x, line, pendingAnchor := 0.0, 0, ""
	place := func(part string, r run, size float64) {
		p.SetFont("Serif", r.Style, size)
		w := p.GetStringWidth(part)
		tokens = append(tokens, tableToken{Text: part, Style: r.Style, SupKind: r.SupKind,
			Target: r.Target, Anchor: pendingAnchor, X: x, Width: w, Line: line})
		pendingAnchor = ""
		x += w
	}
	for _, r := range cell.Runs {
		if r.Anchor != "" {
			pendingAnchor = r.Anchor
		}
		if r.Text == "" {
			continue
		}
		size := font
		if r.SupKind != "" {
			size = font * 0.72
		}
		p.SetFont("Serif", r.Style, size)
		for _, part := range tablePartRe.FindAllString(r.Text, -1) {
			if strings.TrimSpace(part) == "" {
				if x == 0 {
					continue
				}
				space := " "
				if x+p.GetStringWidth(space) > width {
					line++
					x = 0
					continue
				}
				place(space, r, size)
				continue
			}
			letters := []rune(part)
			for len(letters) > 0 {
				word := string(letters)
				if x > 0 && x+p.GetStringWidth(word) > width {
					line++
					x = 0
				}
				cut := len(letters)
				if p.GetStringWidth(word) > width {
					cut = 1
					for cut < len(letters) && p.GetStringWidth(string(letters[:cut+1])) <= width {
						cut++
					}
				}
				place(string(letters[:cut]), r, size)
				letters = letters[cut:]
				if len(letters) > 0 {
					line++
					x = 0
				}
			}
		}
	}
	return tokens, line + 1
}

func drawTableRow(p *fpdf.Fpdf, b contentBlock, pt float64, targets map[string]int) {
	if b.Cols == 0 {
		return
	}
	width := 174 / float64(b.Cols)
	font := math.Max(4, pt*0.85)
	lineHeight := font * 0.35278 * 1.3
	cells := make([][]tableToken, b.Cols)
	maxLines := 1
	for i := 0; i < b.Cols; i++ {
		if i >= len(b.Cells) {
			continue
		}
		var n int
		cells[i], n = layoutTableCell(p, b.Cells[i], width-3, font)
		if n > maxLines {
			maxLines = n
		}
	}
	height := float64(maxLines)*lineHeight + 3
	x, y := 18.0, p.GetY()
	oldMargin := p.GetCellMargin()
	p.SetCellMargin(0)
	defer p.SetCellMargin(oldMargin)
	for i := 0; i < b.Cols; i++ {
		cx := x + float64(i)*width
		p.Rect(cx, y, width, height, "D")
		for _, tok := range cells[i] {
			size := font
			if tok.SupKind != "" {
				size = font * 0.72
			}
			p.SetFont("Serif", tok.Style, size)
			ty := y + 1.5 + float64(tok.Line)*lineHeight
			if tok.Anchor != "" && targets[tok.Anchor] != 0 {
				p.SetLink(targets[tok.Anchor], ty, -1)
			}
			if tok.SupKind != "" {
				ty -= font * 0.35278 * 0.28
				if tok.SupKind == "subscript" {
					p.SetTextColor(150, 84, 32)
				} else {
					p.SetTextColor(29, 78, 142)
				}
			}
			p.SetXY(cx+1.5+tok.X, ty)
			p.CellFormat(tok.Width, lineHeight, tok.Text, "", 0, "", false, targets[tok.Target], "")
			if tok.SupKind != "" {
				p.SetTextColor(0, 0, 0)
			}
		}
	}
	p.SetXY(18, y+height+0.4)
}

func pdfTitleSource(b *Book) string {
	if SourceLine(b.Meta) != "" {
		return ""
	}
	if b.Meta.PageFrom == 0 && b.Meta.PageTo == 0 {
		return fmt.Sprintf("Источники: %d материалов; перечень — в содержании.", len(b.Sections))
	}
	return "Источник: текст работы в читальне."
}

func drawTitle(p *fpdf.Fpdf, b *Book) {
	m := b.Meta
	p.AddPage()
	p.Bookmark("Титульный лист", 0, 22)
	p.SetXY(18, 26)
	p.SetFont("Serif", "B", 17)
	for _, line := range p.SplitText(m.Title, 174) {
		p.CellFormat(174, 10, line, "", 1, "", false, 0, "")
	}
	p.Ln(9)
	p.SetFont("Serif", "", 11)
	p.MultiCell(174, 7, "Файл сгенерирован из читальни «"+site.Name()+"».", "", "L", false)
	p.Ln(5)
	if source := pdfTitleSource(b); source != "" {
		p.SetFont("Serif", "", 10)
		p.MultiCell(174, 6.5, source, "", "L", false)
		p.Ln(3)
	}
	for _, line := range TitleLines(m) {
		if line == "" {
			p.Ln(5)
			continue
		}
		p.SetFont("Serif", "", 10)
		if p.GetY() > 275 {
			p.AddPage()
			p.SetXY(18, 22)
		}
		for _, part := range p.SplitText(line, 174) {
			link := ""
			if m.URL != "" && strings.Contains(line, m.URL) {
				link = m.URL
			}
			if p.GetY() > 275 {
				p.AddPage()
				p.SetXY(18, 22)
			}
			p.CellFormat(174, 6.5, part, "", 1, "", false, 0, link)
		}
	}
	if m.Proofread.Total == 0 {
		p.SetFont("Serif", "", 10)
		p.Ln(5)
		p.MultiCell(174, 6.5, "Вычитка: в выгрузке нет непустых полос.", "", "L", false)
	}
}

// PDFWriter writes a title, linked contents, one A4 sheet per source page,
// and an appendix for endnotes. Fonts are embedded in the binary for Docker.
type PDFWriter struct{}

func (PDFWriter) ContentType() string { return "application/pdf" }
func (PDFWriter) Ext() string         { return "pdf" }
func (PDFWriter) Write(w io.Writer, b *Book) error {
	plan := planPDF(b)
	renderer := markdown.NewRenderer()
	body := make([][]contentBlock, len(plan.pages))
	var moved []movedNotes
	all := make([][]contentBlock, len(plan.pages))
	for i, page := range plan.pages {
		parsed, err := blocksFrom(renderer.RenderWithNotes("", page.Printed, page.Markdown))
		if err != nil {
			return fmt.Errorf("полоса %d: %w", i+1, err)
		}
		prefix := fmt.Sprintf("p%d:", i)
		for j := range parsed {
			if parsed[j].ID != "" {
				parsed[j].ID = prefix + parsed[j].ID
			}
			for k := range parsed[j].Runs {
				if parsed[j].Runs[k].Target != "" {
					parsed[j].Runs[k].Target = prefix + parsed[j].Runs[k].Target
				}
				if parsed[j].Runs[k].Anchor != "" {
					parsed[j].Runs[k].Anchor = prefix + parsed[j].Runs[k].Anchor
				}
			}
			for c := range parsed[j].Cells {
				for k := range parsed[j].Cells[c].Runs {
					r := &parsed[j].Cells[c].Runs[k]
					if r.Target != "" {
						r.Target = prefix + r.Target
					}
					if r.Anchor != "" {
						r.Anchor = prefix + r.Anchor
					}
				}
			}
		}
		all[i] = parsed
		pageBody, notes := splitEndnotes(parsed)
		body[i] = pageBody
		if len(notes) > 0 {
			moved = append(moved, movedNotes{Page: page.Printed, Source: page.Source, Blocks: notes})
		}
	}
	pre := newPDF(b.Meta)
	if err := pre.Error(); err != nil {
		return err
	}
	heights := make([]float64, len(plan.pages))
	for i, page := range plan.pages {
		heights[i] = drawPage(pre, page.Printed, page.Source, body[i], 10, nil)
	}
	final := newPDF(b.Meta)
	if err := final.Error(); err != nil {
		return err
	}
	links := make([]int, len(plan.pages))
	for i := range links {
		links[i] = final.AddLink()
	}
	targets := map[string]int{}
	for _, blocks := range all {
		for _, block := range blocks {
			if block.ID != "" {
				targets[block.ID] = final.AddLink()
			}
			for _, run := range block.Runs {
				if run.Anchor != "" {
					targets[run.Anchor] = final.AddLink()
				}
			}
			for _, cell := range block.Cells {
				for _, run := range cell.Runs {
					if run.Anchor != "" {
						targets[run.Anchor] = final.AddLink()
					}
				}
			}
		}
	}
	appendixLink := 0
	if len(moved) > 0 {
		appendixLink = final.AddLink()
	}
	drawTitle(final, b)
	writeTOC(final, b.Meta.Title, plan.toc, plan.pages, links, appendixLink)
	byPage := map[int][]tocItem{}
	for _, item := range plan.toc {
		byPage[item.PageIndex] = append(byPage[item.PageIndex], item)
	}
	prevLevel := 0
	for i, page := range plan.pages {
		pt := 10.0
		if heights[i] > 255 {
			lo, hi := 2.0, 10.0
			for j := 0; j < 9; j++ {
				mid := (lo + hi) / 2
				if drawPage(pre, page.Printed, page.Source, body[i], mid, nil) <= 253 {
					lo = mid
				} else {
					hi = mid
				}
			}
			pt = lo
		}
		h := drawPage(final, page.Printed, page.Source, body[i], pt, targets)
		if h > 255 {
			return fmt.Errorf("полоса %d выходит за A4 (%.1f мм)", i+1, h)
		}
		final.SetLink(links[i], 0, final.PageNo())
		for _, item := range byPage[i] {
			level := item.Depth
			if level > prevLevel+1 {
				level = prevLevel + 1
			}
			final.Bookmark(item.Title, level, 22)
			prevLevel = level
		}
	}
	writeAppendix(final, moved, targets, appendixLink)
	if err := final.Error(); err != nil {
		return err
	}
	return final.Output(w)
}
