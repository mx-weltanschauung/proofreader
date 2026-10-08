package api

import (
	"strings"
	"testing"

	"proofreader/internal/models"
	"proofreader/pkg/markdown"
)

// Подстрочные сноски (имя вида "r2") нумеруются сквозняком по всему
// переданному диапазону — на этом стоит нынешнее чтение главы, и вынос
// общего блока не должен этого менять.
func TestRenderPageRangeNumbersSubscriptsAcrossTheRange(t *testing.T) {
	r := markdown.NewRenderer()
	pages := []*models.Page{
		{PageNumber: 3, ContentMarkdown: "Альфа[^r2].\n\n[^r2]: первая."},
		{PageNumber: 9, ContentMarkdown: "Бета[^r1].\n\n[^r1]: вторая."},
	}

	html, notes := renderPageRange(r, pages)

	if len(html) != 2 {
		t.Fatalf("HTML страниц = %d, ожидалось 2", len(html))
	}
	if len(notes.Subscript) != 2 {
		t.Fatalf("подстрочных сносок = %d, ожидалось 2: %+v", len(notes.Subscript), notes.Subscript)
	}
	if notes.Subscript[0].Marker != "(1)" || notes.Subscript[1].Marker != "(2)" {
		t.Errorf("маркеры = %q, %q; ожидались (1), (2)",
			notes.Subscript[0].Marker, notes.Subscript[1].Marker)
	}
	// Якоря остаются постранично уникальными.
	if notes.Subscript[0].AnchorID != "fn:3-r2" {
		t.Errorf("якорь = %q, ожидался fn:3-r2", notes.Subscript[0].AnchorID)
	}
	if !strings.Contains(html[0], ">(1)</a>") {
		t.Errorf("маркер (1) не попал в текст страницы:\n%s", html[0])
	}
}

// Пустой диапазон — не ошибка: у главы может не быть страниц.
func TestRenderPageRangeEmpty(t *testing.T) {
	html, notes := renderPageRange(markdown.NewRenderer(), nil)

	if len(html) != 0 {
		t.Errorf("HTML страниц = %d, ожидалось 0", len(html))
	}
	if len(notes.Subscript) != 0 || len(notes.Endnote) != 0 {
		t.Errorf("ожидался пустой набор сносок, получено %+v", notes)
	}
}
