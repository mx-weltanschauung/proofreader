package book

import (
	"strings"

	"golang.org/x/net/html"

	"proofreader/pkg/xhtml"
)

// Понижение MathML в теги FictionBook. Схема FB2 математики не знает вовсе:
// в её распоряжении <p>, <table>, <sub>, <sup> — и всё. Поэтому понижаем то,
// что реально встречается в схемах воспроизводства, а неподъёмное честно
// печатаем исходной строкой TeX.

// mathName — имя узла MathML. Чужеродное содержимое HTML5-парсер кладёт в
// Namespace "math", а имя тега держит в Data.
func mathName(n *html.Node) string {
	if n == nil || n.Type != html.ElementNode {
		return ""
	}
	return n.Data
}

func firstMathChild(n *html.Node) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			return c
		}
	}
	return nil
}

// mathPresentation отдаёт презентационную ветвь формулы. Внутри <semantics>
// рядом с ней лежит <annotation encoding="application/x-tex"> с исходным TeX —
// её нужно пропустить, иначе формула печатается дважды.
func mathPresentation(mathNode *html.Node) *html.Node {
	first := firstMathChild(mathNode)
	if first == nil {
		return nil
	}
	if mathName(first) == "semantics" {
		return firstMathChild(first)
	}
	return first
}

// fb2MathInline печатает формулу строкой. Возвращает false, если встретился
// узел, которого понижение не знает: тогда вызывающий откатывается на TeX
// целиком, а не печатает половину.
func fb2MathInline(out *strings.Builder, n *html.Node) bool {
	if n == nil {
		return false
	}
	if n.Type == html.TextNode {
		out.WriteString(xhtml.EscapeText(n.Data))
		return true
	}
	if n.Type != html.ElementNode {
		return true
	}

	switch mathName(n) {
	case "semantics":
		return fb2MathInline(out, firstMathChild(n)) // annotation пропущена
	case "annotation":
		return true // исходный TeX печатать не надо: он уже напечатан разметкой
	case "mrow", "mstyle", "mpadded", "mphantom", "mtd":
		// mtd — ячейка mtable: fb2MathTable зовёт fb2MathInline прямо на
		// узле <mtd>, а не на его детях, так что ячейка тоже должна уметь
		// просто склеить своё содержимое, как mrow.
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if !fb2MathInline(out, c) {
				return false
			}
		}
		return true
	case "mi", "mn", "mo", "mtext":
		text := textOf(n)
		if mathName(n) == "mo" && text == "=" {
			// Знак равенства в итоговой строке схемы воспроизводства читается
			// как предложение («Капитал = 7500») — слитно с соседними числами
			// он превращается в нечитаемое «Капитал=7500». Остальные знаки
			// (+, −, …) в этих формулах пишутся слитно по соглашению корпуса
			// («c+v» — сумма постоянного и переменного капитала), поэтому
			// пробелы добавляются только вокруг «=», а не вокруг любого mo.
			out.WriteString(" " + xhtml.EscapeText(text) + " ")
			return true
		}
		out.WriteString(xhtml.EscapeText(text))
		return true
	case "mspace":
		out.WriteString(" ")
		return true
	case "msup":
		return fb2MathScript(out, n, "", "sup")
	case "msub":
		return fb2MathScript(out, n, "sub", "")
	case "msubsup":
		return fb2MathScript(out, n, "sub", "sup")
	case "mfrac":
		return fb2MathFrac(out, n)
	default:
		return false
	}
}

// fb2MathScript печатает основание с нижним и/или верхним индексом.
// Пустое имя тега означает, что индекса этого рода у узла нет.
func fb2MathScript(out *strings.Builder, n *html.Node, subTag, supTag string) bool {
	parts := mathChildren(n)
	want := 2
	if subTag != "" && supTag != "" {
		want = 3
	}
	if len(parts) != want {
		return false
	}
	if !fb2MathInline(out, parts[0]) {
		return false
	}
	tags := []string{subTag}
	if want == 3 {
		tags = []string{subTag, supTag}
	} else if supTag != "" {
		tags = []string{supTag}
	}
	for i, tag := range tags {
		var inner strings.Builder
		if !fb2MathInline(&inner, parts[i+1]) {
			return false
		}
		out.WriteString("<" + tag + ">" + inner.String() + "</" + tag + ">")
	}
	return true
}

// fb2MathFrac печатает дробь как «a/b». Составной операнд берётся в скобки,
// иначе «a+b/c» соврёт про приоритет.
func fb2MathFrac(out *strings.Builder, n *html.Node) bool {
	parts := mathChildren(n)
	if len(parts) != 2 {
		return false
	}
	for i, part := range parts {
		var inner strings.Builder
		if !fb2MathInline(&inner, part) {
			return false
		}
		text := inner.String()
		if mathIsCompound(part) {
			text = "(" + text + ")"
		}
		if i == 1 {
			out.WriteString("/")
		}
		out.WriteString(text)
	}
	return true
}

// mathIsCompound — операнд из нескольких знаков, которому в строке нужны
// скобки.
func mathIsCompound(n *html.Node) bool {
	switch mathName(n) {
	case "mi", "mn", "mo", "mtext":
		return false
	}
	return len(mathChildren(n)) > 1
}

func mathChildren(n *html.Node) []*html.Node {
	var out []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			out = append(out, c)
		}
	}
	return out
}

// flattenMathRows разворачивает mrow/mstyle-обёртки в списке узлов, отдавая
// их детей на том же уровне. KaTeX группирует \left…\right в свой <mrow> —
// для формулы «I 4000 c + 1000 v + 1000 m = 6000 } Капитал = 7500…» вторая
// mtable лежит рядом с <mrow>[первая mtable, закрывающая \}], а не рядом с
// самими mtable/скобкой напрямую. Без разворачивания fb2MathTable увидел бы
// на этом уровне не «таблица, скобка, таблица», а «mrow, таблица» — и упал
// бы в откат на TeX, потому что fb2MathInline не умеет печатать mtable
// строкой.
func flattenMathRows(items []*html.Node) []*html.Node {
	var out []*html.Node
	for _, item := range items {
		switch mathName(item) {
		case "mrow", "mstyle":
			out = append(out, flattenMathRows(mathChildren(item))...)
		default:
			out = append(out, item)
		}
	}
	return out
}

// fb2MathTable собирает таблицу FB2 из формулы, где есть mtable. Схемы
// воспроизводства устроены так: два массива по бокам и фигурная скобка между
// ними. Каждая mtable даёт свои колонки, одиночный знак между ними — свою
// колонку со знаком в первой строке.
func fb2MathTable(pres *html.Node) (string, bool) {
	var items []*html.Node
	if mathName(pres) == "mtable" {
		// Формула без обрамляющих \left…\right — сама mtable и есть
		// презентационная ветвь целиком (mathChildren(pres) отдал бы её
		// строки <mtr>, а не саму таблицу как элемент списка).
		items = []*html.Node{pres}
	} else {
		items = flattenMathRows(mathChildren(pres))
	}

	type column struct {
		cells []string // по строкам
	}
	var columns []column
	rows := 0
	hasTable := false

	for _, item := range items {
		if mathName(item) == "mtable" {
			hasTable = true
			var col column
			for _, tr := range mathChildren(item) {
				if mathName(tr) != "mtr" {
					return "", false
				}
				var cells []string
				for _, td := range mathChildren(tr) {
					if mathName(td) != "mtd" {
						return "", false
					}
					var inner strings.Builder
					if !fb2MathInline(&inner, td) {
						return "", false
					}
					cells = append(cells, inner.String())
				}
				col.cells = append(col.cells, strings.Join(cells, " "))
			}
			if len(col.cells) > rows {
				rows = len(col.cells)
			}
			columns = append(columns, col)
			continue
		}
		// Не таблица — например, фигурная скобка между массивами.
		var inner strings.Builder
		if !fb2MathInline(&inner, item) {
			return "", false
		}
		if strings.TrimSpace(inner.String()) == "" {
			continue
		}
		columns = append(columns, column{cells: []string{inner.String()}})
	}

	if !hasTable || len(columns) == 0 || rows == 0 {
		// rows==0 значит mtable без единой строки (пустой \begin{array}{l}
		// \end{array}) — <table></table> без <tr> не проходит XSD
		// (SCHEMAV_ELEMENT_CONTENT: Element 'table': Missing child
		// element(s). Expected is (tr)) и валит схему всего тома.
		return "", false
	}

	var out strings.Builder
	out.WriteString("<table>\n")
	for r := 0; r < rows; r++ {
		out.WriteString("<tr>")
		for _, col := range columns {
			cell := ""
			if r < len(col.cells) {
				cell = col.cells[r]
			}
			out.WriteString("<td>" + cell + "</td>")
		}
		out.WriteString("</tr>\n")
	}
	out.WriteString("</table>\n")
	return out.String(), true
}

// writeFB2Math печатает блочную формулу: таблицей, если внутри есть mtable,
// иначе абзацем. Неподъёмная формула печатается исходной строкой TeX.
func writeFB2Math(out *strings.Builder, tex string) {
	fallback := func() {
		out.WriteString("<p>" + xhtml.EscapeText(tex) + "</p>\n")
	}

	node, err := mathNode(tex, true)
	if err != nil {
		fallback()
		return
	}
	pres := mathPresentation(node)
	if pres == nil {
		fallback()
		return
	}
	if table, ok := fb2MathTable(pres); ok {
		out.WriteString(table)
		return
	}
	var inline strings.Builder
	if fb2MathInline(&inline, pres) {
		out.WriteString("<p>" + inline.String() + "</p>\n")
		return
	}
	fallback()
}

// displayMathOnly сообщает, что абзац целиком занят блочной формулой. Такой
// абзац становится таблицей на уровне секции: <table> — блочный элемент FB2,
// внутрь <p> его класть нельзя.
func displayMathOnly(p *html.Node) (string, bool) {
	var only *html.Node
	for c := p.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode && strings.TrimSpace(c.Data) == "" {
			continue
		}
		if only != nil {
			return "", false
		}
		only = c
	}
	tex, display, ok := mathSpanTeX(only)
	if !ok || !display {
		return "", false
	}
	return tex, true
}
