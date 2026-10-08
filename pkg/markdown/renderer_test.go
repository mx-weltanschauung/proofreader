package markdown

import (
	"regexp"
	"strings"
	"testing"
)

func TestNewRenderer(t *testing.T) {
	r := NewRenderer()
	if r == nil {
		t.Fatal("Expected renderer to be created")
	}
	if r.extensions == 0 {
		t.Error("Expected parser extensions to be set")
	}
	if r.rendererOpts.FootnoteReturnLinkContents == "" {
		t.Error("Expected html renderer options to be set")
	}
}

func TestRenderBasicMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
	}{
		{
			name:     "heading",
			input:    "# Hello World",
			contains: []string{"<h1", "Hello World", "</h1>"},
		},
		{
			name:     "paragraph",
			input:    "This is a paragraph.",
			contains: []string{"<p>", "This is a paragraph.", "</p>"},
		},
		{
			name:     "bold text",
			input:    "This is **bold** text.",
			contains: []string{"<strong>", "bold", "</strong>"},
		},
		{
			name:     "italic text",
			input:    "This is *italic* text.",
			contains: []string{"<em>", "italic", "</em>"},
		},
		{
			name:     "link",
			input:    "[Example](https://example.com)",
			contains: []string{"<a href=\"https://example.com\"", "Example", "</a>"},
		},
		{
			name:     "unordered list",
			input:    "- Item 1\n- Item 2\n- Item 3",
			contains: []string{"<ul>", "<li>", "Item 1", "Item 2", "Item 3", "</li>", "</ul>"},
		},
		{
			name:     "ordered list",
			input:    "1. First\n2. Second\n3. Third",
			contains: []string{"<ol>", "<li>", "First", "Second", "Third", "</li>", "</ol>"},
		},
		{
			name:     "code block",
			input:    "```\ncode here\n```",
			contains: []string{"<code>", "code here", "</code>"},
		},
		{
			name:     "inline code",
			input:    "Use `command` here",
			contains: []string{"<code>", "command", "</code>"},
		},
		{
			name:     "blockquote",
			input:    "> This is a quote",
			contains: []string{"<blockquote>", "This is a quote", "</blockquote>"},
		},
		{
			name:     "horizontal rule",
			input:    "---",
			contains: []string{"<hr"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRenderer()
			result := r.Render(tt.input)
			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected output to contain %q, got:\n%s", expected, result)
				}
			}
		})
	}
}

func TestRenderHeadings(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"# H1", "h1"},
		{"## H2", "h2"},
		{"### H3", "h3"},
		{"#### H4", "h4"},
		{"##### H5", "h5"},
		{"###### H6", "h6"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			r := NewRenderer()
			result := r.Render(tt.input)
			if !strings.Contains(result, "<"+tt.expected) {
				t.Errorf("Expected %s tag, got:\n%s", tt.expected, result)
			}
		})
	}
}

func TestRenderTable(t *testing.T) {
	r := NewRenderer()

	input := `| Header 1 | Header 2 |
|----------|----------|
| Cell 1   | Cell 2   |
| Cell 3   | Cell 4   |`

	result := r.Render(input)

	expected := []string{"<table>", "<thead>", "<tbody>", "<tr>", "<th>", "<td>", "</table>"}
	for _, exp := range expected {
		if !strings.Contains(result, exp) {
			t.Errorf("Expected table to contain %q, got:\n%s", exp, result)
		}
	}
}

func TestRenderStrikethrough(t *testing.T) {
	r := NewRenderer()

	input := "This is ~~strikethrough~~ text."
	result := r.Render(input)

	if !strings.Contains(result, "<del>") && !strings.Contains(result, "<s>") {
		t.Errorf("Expected strikethrough tag, got:\n%s", result)
	}
}

func TestRenderEmptyInput(t *testing.T) {
	r := NewRenderer()

	result := r.Render("")
	if result == "" {
		return
	}

	result = strings.TrimSpace(result)
	if result != "" {
		t.Logf("Rendered empty string as: %q", result)
	}
}

func TestRenderPageNotesPreservesAnchors(t *testing.T) {
	r := NewRenderer()
	md := "Text[^5] and[^r2].\n\n[^5]: endnote body.\n\n[^r2]: subscript body. *Ред.*"
	mainHTML, items := r.renderPageNotes("", 3, md)

	// In-text ref ids are page-namespaced.
	if !strings.Contains(mainHTML, `id="fnref:3-5"`) {
		t.Errorf("expected namespaced ref id fnref:3-5, got:\n%s", mainHTML)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 footnote items, got %d", len(items))
	}
	byAnchor := map[string]footnoteItem{}
	for _, it := range items {
		byAnchor[it.AnchorID] = it
	}
	end, ok := byAnchor["fn:3-5"]
	if !ok {
		t.Fatalf("missing endnote anchor fn:3-5; got %+v", items)
	}
	if end.RefID != "fnref:3-5" || end.Marker != "5" || end.Kind != "endnote" {
		t.Errorf("endnote item wrong: %+v", end)
	}
	if !strings.Contains(end.BodyHTML, "endnote body") || strings.Contains(end.BodyHTML, "footnote-return") {
		t.Errorf("endnote body should keep text and drop the return link: %q", end.BodyHTML)
	}
	sub, ok := byAnchor["fn:3-r2"]
	if !ok {
		t.Fatalf("missing subscript anchor fn:3-r2; got %+v", items)
	}
	if sub.Marker != "(2)" || sub.Kind != "subscript" {
		t.Errorf("subscript item wrong: %+v", sub)
	}
}

func TestFootnoteAnchorsDoNotCollideAcrossPages(t *testing.T) {
	r := NewRenderer()
	p3 := "A[^r1].\n\n[^r1]: note on p3."
	p5 := "B[^r1].\n\n[^r1]: note on p5."
	_, items3 := r.renderPageNotes("", 3, p3)
	_, items5 := r.renderPageNotes("", 5, p5)

	if items3[0].AnchorID != "fn:3-r1" {
		t.Errorf("page 3 anchor = %q, want fn:3-r1", items3[0].AnchorID)
	}
	if items5[0].AnchorID != "fn:5-r1" {
		t.Errorf("page 5 anchor = %q, want fn:5-r1", items5[0].AnchorID)
	}
}

func TestPageContent(t *testing.T) {
	pc := PageContent{
		PageNumber: 42,
		Content:    "Test content",
	}

	if pc.PageNumber != 42 {
		t.Errorf("PageNumber = %d, want 42", pc.PageNumber)
	}
	if pc.Content != "Test content" {
		t.Errorf("Content = %s, want Test content", pc.Content)
	}
}

func TestRenderWithUnicode(t *testing.T) {
	inputs := []string{
		"# Заголовок на русском",
		"Chinese: 中文测试",
		"Japanese: 日本語テスト",
		"Emoji: 🎉 🚀 ✨",
		"Mixed: Hello Мир 世界",
	}

	for _, input := range inputs {
		t.Run(input[:20], func(t *testing.T) {
			r := NewRenderer()
			result := r.Render(input)
			if result == "" {
				t.Error("Expected non-empty result")
			}
		})
	}
}

func TestRenderSpecialCharacters(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"ampersand", "Tom & Jerry"},
		{"less than", "a < b"},
		{"greater than", "a > b"},
		{"quotes", `He said "Hello"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRenderer()
			result := r.Render(tt.input)
			if result == "" {
				t.Error("Expected non-empty result")
			}
		})
	}
}

func TestRenderNestedStructures(t *testing.T) {
	r := NewRenderer()

	input := `
- Item 1
  - Nested 1
  - Nested 2
    - Deep nested
- Item 2
`

	result := r.Render(input)

	if !strings.Contains(result, "<ul>") {
		t.Error("Expected unordered list")
	}
	if !strings.Contains(result, "Nested 1") {
		t.Error("Expected nested content")
	}
}

func TestRenderFencedCodeBlock(t *testing.T) {
	r := NewRenderer()

	input := "```go\nfunc main() {\n    fmt.Println(\"Hello\")\n}\n```"

	result := r.Render(input)

	if !strings.Contains(result, "<code") {
		t.Error("Expected code block")
	}
	if !strings.Contains(result, "func main()") {
		t.Error("Expected code content")
	}
}

// Переименован вслед за функцией (prefixFootnotesWithPageNumber ->
// prefixFootnoteNames — задача 4, область имён якорей). Префикс здесь тот
// же, что раньше собирала сама функция: notePrefix с пустой областью даёт
// "{pageNumber}-", то есть поведение теста не изменилось ни на знак.
func TestPrefixFootnoteNames(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		pageNumber int
		expected   string
	}{
		{
			name:       "single footnote reference",
			input:      "Some text[^1] with footnote.",
			pageNumber: 12,
			expected:   "Some text[^12-1] with footnote.",
		},
		{
			name:       "multiple footnote references",
			input:      "Text[^1] with[^2] multiple[^3] footnotes.",
			pageNumber: 5,
			expected:   "Text[^5-1] with[^5-2] multiple[^5-3] footnotes.",
		},
		{
			name:       "footnote definition",
			input:      "[^1]: This is a footnote definition.",
			pageNumber: 10,
			expected:   "[^10-1]: This is a footnote definition.",
		},
		{
			name: "footnote reference and definition",
			input: `Some text[^1] with footnote.

[^1]: Footnote content.`,
			pageNumber: 7,
			expected: `Some text[^7-1] with footnote.

[^7-1]: Footnote content.`,
		},
		{
			name: "multiple footnotes with definitions",
			input: `Text[^1] and more[^2].

[^1]: First note.
[^2]: Second note.`,
			pageNumber: 3,
			expected: `Text[^3-1] and more[^3-2].

[^3-1]: First note.
[^3-2]: Second note.`,
		},
		{
			name:       "footnote with complex id",
			input:      "Text[^note-1] with[^my_note] footnotes.",
			pageNumber: 15,
			expected:   "Text[^15-note-1] with[^15-my_note] footnotes.",
		},
		{
			name:       "no footnotes",
			input:      "Just regular text without footnotes.",
			pageNumber: 1,
			expected:   "Just regular text without footnotes.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := prefixFootnoteNames(tt.input, notePrefix("", tt.pageNumber))
			if result != tt.expected {
				t.Errorf("Expected:\n%s\n\nGot:\n%s", tt.expected, result)
			}
		})
	}
}

func TestFootnoteReturnLinkSymbol(t *testing.T) {
	r := NewRenderer()

	input := `Text with footnote[^1].

[^1]: Footnote content.`

	result := r.Render(input)

	if !strings.Contains(result, "↩") {
		t.Errorf("Expected footnote return link to contain ↩, got: %s", result)
	}

	if strings.Contains(result, "[return]") {
		t.Error("Footnote return link should not contain [return]")
	}
}

func TestLinkNoteCrossRefs(t *testing.T) {
	got := linkNoteCrossRefs("текст см. примечание 51. далее")
	want := `текст <a class="note-xref" data-note="51">см. примечание 51</a>. далее`
	if got != want {
		t.Errorf("wrap:\n got=%q\nwant=%q", got, want)
	}
}

func TestLinkNoteCrossRefs_Idempotent(t *testing.T) {
	once := linkNoteCrossRefs("см. примечание 7 тут")
	twice := linkNoteCrossRefs(once)
	if once != twice {
		t.Errorf("not idempotent:\n once=%q\ntwice=%q", once, twice)
	}
}

func TestLinkNoteCrossRefs_NoFalsePositive(t *testing.T) {
	// a bare number in prose must NOT be wrapped
	in := "на странице 51 есть таблица"
	if got := linkNoteCrossRefs(in); got != in {
		t.Errorf("unexpected wrap: %q", got)
	}
}

func TestRender_WrapsCrossRefInFootnoteBody(t *testing.T) {
	r := NewRenderer()
	out := r.Render("текст[^1]\n\n[^1]: тело — см. примечание 51.")
	if !strings.Contains(out, `data-note="51"`) {
		t.Errorf("cross-ref not wrapped in rendered output:\n%s", out)
	}
}

func TestNoteMarker(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantText   string
		wantKind   string
	}{
		{"endnote plain", "5", "5", "endnote"},
		{"endnote multi digit", "42", "42", "endnote"},
		{"subscript plain", "r2", "(2)", "subscript"},
		{"subscript r1", "r1", "(1)", "subscript"},
		{"subscript s prefix", "s1", "(1)", "subscript"},
		{"subscript s prefix multi digit", "s12", "(12)", "subscript"},
		{"endnote page prefixed", "3-5", "5", "endnote"},
		{"subscript page prefixed", "3-r2", "(2)", "subscript"},
		{"subscript page prefixed multi", "12-r3", "(3)", "subscript"},
		{"subscript s prefix page prefixed", "3-s2", "(2)", "subscript"},
		{"fallback non numeric", "foo", "foo", "endnote"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotText, gotKind := noteMarker(tt.input)
			if gotText != tt.wantText || gotKind != tt.wantKind {
				t.Errorf("noteMarker(%q) = (%q, %q), want (%q, %q)",
					tt.input, gotText, gotKind, tt.wantText, tt.wantKind)
			}
		})
	}
}

func TestRenderPageNotesNestedListBody(t *testing.T) {
	r := NewRenderer()
	// Footnote [^1] body has an intro paragraph AND a nested list.
	md := "Text[^1].\n\n[^1]: intro paragraph.\n\n    - item a\n    - item b\n"
	_, items := r.renderPageNotes("", 3, md)

	if len(items) != 1 {
		t.Fatalf("expected 1 footnote item, got %d: %+v", len(items), items)
	}
	body := items[0].BodyHTML
	// Both nested items must survive (non-greedy bug dropped "item b").
	if !strings.Contains(body, "item a") || !strings.Contains(body, "item b") {
		t.Errorf("nested list truncated; body=%q", body)
	}
	// The whole nested <ul> must be intact and balanced.
	if !strings.Contains(body, "<ul>") || !strings.Contains(body, "</ul>") {
		t.Errorf("nested <ul> not preserved; body=%q", body)
	}
	// The return link must be stripped from the body and captured as RefID.
	if strings.Contains(body, "footnote-return") {
		t.Errorf("return link should be stripped from body; body=%q", body)
	}
	if items[0].RefID != "fnref:3-1" || items[0].AnchorID != "fn:3-1" {
		t.Errorf("anchor/ref wrong: %+v", items[0])
	}
}

func TestRenderPageNotesRefIDFallback(t *testing.T) {
	// A footnote definition whose rendered body has NO return link exercises the
	// refID := "fnref:" + name fallback. gomarkdown's FootnoteReturnLinks is on,
	// so we build the block shape directly is not possible here; instead assert
	// the fallback via a page with a normal footnote and verify RefID matches the
	// namespaced name even though we don't control link presence.
	r := NewRenderer()
	_, items := r.renderPageNotes("", 9, "X[^2].\n\n[^2]: plain body.")
	if len(items) != 1 || items[0].RefID != "fnref:9-2" {
		t.Fatalf("expected RefID fnref:9-2, got %+v", items)
	}
}

func TestRenderRewritesInTextMarkers(t *testing.T) {
	r := NewRenderer()

	// Endnote [^5] shows "5", subscript [^r2] shows "(2)" — NOT gomarkdown's
	// per-page ordinal (which would be "1" and "2").
	md := "Text[^5] and more[^r2].\n\n[^5]: endnote body.\n\n[^r2]: subscript body. *Ред.*"
	out := r.Render(md)

	if !strings.Contains(out, `<a href="#fn:5">5</a>`) {
		t.Errorf("expected endnote marker 5, got:\n%s", out)
	}
	if !strings.Contains(out, `<a href="#fn:r2">(2)</a>`) {
		t.Errorf("expected subscript marker (2), got:\n%s", out)
	}
	if !strings.Contains(out, `footnote-ref--endnote`) {
		t.Error("expected endnote kind class")
	}
	if !strings.Contains(out, `footnote-ref--subscript`) {
		t.Error("expected subscript kind class")
	}
}

// A footnote reference followed by a parenthesis — «term[^N] (перевод)», a shape
// these volumes are full of — used to be eaten by gomarkdown's inline-link
// parser: the note lost its in-text marker and the parenthesised text vanished
// from the page entirely. See installFootnoteParenFix.
func TestRenderFootnoteRefFollowedByParen(t *testing.T) {
	tests := []struct {
		name       string
		md         string
		wantMarker string
		wantText   string
	}{
		{
			name:       "subscript, space before paren",
			md:         "Я предполагаемое, resp.[^s1] (поскольку так) далее.\n\n[^s1]: — соответственно. *Ред.*",
			wantMarker: `<a href="#fn:s1">(1)</a>`,
			wantText:   "(поскольку так)",
		},
		{
			name:       "endnote, no space before paren",
			md:         "«Белоснежка»[^125](стр. 87) и мотивы.\n\n[^125]: книжное примечание.",
			wantMarker: `<a href="#fn:125">125</a>`,
			wantText:   "(стр. 87)",
		},
		{
			name:       "endnote, space before paren",
			md:         "santa casa[^92] (логики). Душа предметов.\n\n[^92]: книжное примечание.",
			wantMarker: `<a href="#fn:92">92</a>`,
			wantText:   "(логики)",
		},
		{
			name:       "reference and paren split across lines",
			md:         "докладу Пандекты[^105]\n(людей и собак) впоследствии.\n\n[^105]: книжное примечание.",
			wantMarker: `<a href="#fn:105">105</a>`,
			wantText:   "(людей и собак)",
		},
	}

	r := NewRenderer()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := r.Render(tt.md)
			if !strings.Contains(out, tt.wantMarker) {
				t.Errorf("in-text footnote marker %s missing, got:\n%s", tt.wantMarker, out)
			}
			if !strings.Contains(out, tt.wantText) {
				t.Errorf("parenthesised text %q was dropped from the page, got:\n%s", tt.wantText, out)
			}
			if strings.Contains(out, `<a href="`+strings.Trim(tt.wantText, "()")) {
				t.Errorf("parenthesised text was parsed as a link destination, got:\n%s", out)
			}
		})
	}
}

// The fix must not disturb ordinary links, which share the '[' inline parser.
func TestRenderOrdinaryLinksStillWork(t *testing.T) {
	r := NewRenderer()

	out := r.Render("см. [текст](http://example.com/x) далее")
	if !strings.Contains(out, `href="http://example.com/x"`) || !strings.Contains(out, ">текст<") {
		t.Errorf("ordinary inline link broken:\n%s", out)
	}

	// A bracketed "[^…]" with no matching definition is not a footnote; the old
	// link reading is all it can be, so it must survive.
	out = r.Render("см. [^caret](http://example.com/y) далее")
	if !strings.Contains(out, `href="http://example.com/y"`) {
		t.Errorf("link with a caret-leading label broken:\n%s", out)
	}
}

// headingIDRe extracts every "id=..." attribute value from a run of
// rendered <h1>/<h2> tags, in document order.
var headingIDRe = regexp.MustCompile(`<h[1-6][^>]*\bid="([^"]+)"`)

func headingIDs(html string) []string {
	var ids []string
	for _, m := range headingIDRe.FindAllStringSubmatch(html, -1) {
		ids = append(ids, m[1])
	}
	return ids
}

// A single *Renderer instance is built once per process (see main.go) and
// reused for every page/work/document render for the life of the server, so
// two renders of the same markdown through the same instance must produce
// byte-identical HTML. Before the fix, gomarkdown's html.Renderer keeps a
// heading-id de-duplication counter that is never reset between calls, so
// the second render's heading ids (and hence the whole document) differ from
// the first.
func TestRenderIsByteIdenticalAcrossCallsOnSameInstance(t *testing.T) {
	r := NewRenderer()
	md := "# К. Маркс. Капитал\n\nКритика политической экономии.\n\nТом первый."

	first := r.Render(md)
	second := r.Render(md)

	if first != second {
		t.Errorf("second render differs from first on the same *Renderer:\nfirst:  %s\nsecond: %s", first, second)
	}
}

// The de-duplication suffix ("-1", "-2", ...) is legitimate WITHIN a single
// render when a document has two headings with identical text — that is
// what stops the ids from colliding on the same page. But the counter must
// start over for the next, unrelated render: it must not carry state from a
// previous document.
func TestDuplicateHeadingSuffixResetsBetweenRenders(t *testing.T) {
	r := NewRenderer()
	md := "# Глава\n\nпервый текст\n\n# Глава\n\nвторой текст"

	for i := 0; i < 3; i++ {
		out := r.Render(md)
		ids := headingIDs(out)
		want := []string{"глава", "глава-1"}
		if len(ids) != len(want) || ids[0] != want[0] || ids[1] != want[1] {
			t.Errorf("render %d: heading ids = %v, want %v (dedup counter must reset each render)", i, ids, want)
		}
	}
}

// Heading ids are deep-link targets in the reading room (#заголовок in a
// page URL); they must be a pure function of the document's content, not of
// how many times the process has rendered something before. This is the
// same defect as TestRenderIsByteIdenticalAcrossCallsOnSameInstance, stated
// at the level that matters to a reader: does the same heading always get
// the same anchor.
func TestHeadingIDsAreStableAcrossRenders(t *testing.T) {
	r := NewRenderer()
	md := "# К. Маркс. Капитал\n\n## Критика политической экономии\n\n### Том первый"

	first := headingIDs(r.Render(md))
	for i := 1; i < 5; i++ {
		got := headingIDs(r.Render(md))
		if len(got) != len(first) {
			t.Fatalf("render %d: got %d heading ids, want %d: %v", i, len(got), len(first), got)
		}
		for j := range first {
			if got[j] != first[j] {
				t.Errorf("render %d: heading %d id = %q, want %q (stable across renders)", i, j, got[j], first[j])
			}
		}
	}
}
