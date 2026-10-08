package book

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"proofreader/pkg/mathml"
	"proofreader/pkg/xhtml"
)

// mathClassMarker — по этой подстроке дешёвой проверкой отсекаются страницы
// без формул: их в корпусе подавляющее большинство, и разбирать их дерево
// незачем.
const mathClassMarker = `class="math`

// mathSpanTeX распознаёт спан формулы, оставленный markdown-рендерером
// (pkg/markdown/renderer.go, расширение parser.MathJax): блочная формула
// приезжает как <span class="math display">\[…\]</span>, строчная — как
// <span class="math inline">\(…\)</span>.
func mathSpanTeX(n *html.Node) (tex string, display bool, ok bool) {
	if n == nil || n.Type != html.ElementNode || n.DataAtom != atom.Span {
		return "", false, false
	}
	if !strings.Contains(attrValue(n, "class"), "math") {
		return "", false, false
	}
	raw := strings.TrimSpace(textOf(n))
	switch {
	case strings.HasPrefix(raw, `\[`) && strings.HasSuffix(raw, `\]`):
		return strings.TrimSpace(raw[2 : len(raw)-2]), true, true
	case strings.HasPrefix(raw, `\(`) && strings.HasSuffix(raw, `\)`):
		return strings.TrimSpace(raw[2 : len(raw)-2]), false, true
	}
	return "", false, false
}

// mathNode рендерит формулу и разбирает результат в узел дерева. Контекст
// разбора — span: внутри строчного элемента HTML5-парсер кладёт math как
// чужеродное содержимое и сохраняет xmlns, без которого EPUB невалиден.
func mathNode(tex string, display bool) (*html.Node, error) {
	rendered, err := mathml.Render(tex, display)
	if err != nil {
		return nil, err
	}
	ctx := &html.Node{Type: html.ElementNode, Data: "span", DataAtom: atom.Span}
	nodes, err := html.ParseFragment(strings.NewReader(rendered), ctx)
	if err != nil {
		return nil, fmt.Errorf("разбор MathML: %w", err)
	}
	for _, n := range nodes {
		if n.Type == html.ElementNode && n.Data == "math" {
			return n, nil
		}
	}
	return nil, fmt.Errorf("в разобранном MathML нет элемента math")
}

// withMathML заменяет содержимое спанов формул настоящим MathML. Сломанная
// формула остаётся исходным LaTeX и наверх не всплывает: скачивание тома не
// должно падать из-за одной опечатки.
func withMathML(fragment string) string {
	if !strings.Contains(fragment, mathClassMarker) {
		return fragment
	}
	doc, err := html.Parse(strings.NewReader(fragment))
	if err != nil {
		return fragment
	}
	body := xhtml.FindBody(doc)
	if body == nil {
		return fragment
	}
	replaceMathSpans(body)

	var out strings.Builder
	for c := body.FirstChild; c != nil; c = c.NextSibling {
		xhtml.WriteNode(&out, c)
	}
	return out.String()
}

func replaceMathSpans(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		tex, display, ok := mathSpanTeX(c)
		if !ok {
			replaceMathSpans(c)
			continue
		}
		node, err := mathNode(tex, display)
		if err != nil {
			continue // остаётся исходный LaTeX
		}
		for c.FirstChild != nil {
			c.RemoveChild(c.FirstChild)
		}
		c.AppendChild(node)
	}
}
