package markdown

import (
	"strings"
	"testing"
)

// Печатное примечание часто идёт в несколько абзацев. Markdown обрывает
// определение сноски на первой пустой строке, поэтому абзац-продолжение
// записывается с отступом в четыре пробела (так пишут ocr_ingest/note_body.py и
// fix_leaked_note_body.py). Тест держит вторую половину договорённости: такое
// определение целиком остаётся примечанием и не подмешивается в текст страницы.
func TestCollectPagesMultiParagraphNoteStaysInTheNote(t *testing.T) {
	r := NewRenderer()
	pages := []PageContent{{
		PageNumber: 7,
		Content: "Читателям «Tribune»[^2] о причинах.\n\n" +
			"[^2]: *«Tribune»* — сокращённое название газеты.\n\n" +
			"    Редакция в ряде случаев допускала вольное обращение со статьями.\n",
	}}

	html, notes := r.CollectPages(pages)

	if len(notes.Endnote) != 1 {
		t.Fatalf("want 1 endnote, got %d: %+v", len(notes.Endnote), notes.Endnote)
	}
	body := notes.Endnote[0].BodyHTML
	if !strings.Contains(body, "Редакция в ряде случаев") {
		t.Errorf("continuation paragraph missing from the note body:\n%s", body)
	}
	if strings.Count(body, "<p>") != 2 {
		t.Errorf("want two paragraphs inside the note, got:\n%s", body)
	}
	if strings.Contains(html[0], "Редакция в ряде случаев") {
		t.Errorf("continuation paragraph leaked into the page text:\n%s", html[0])
	}
}

func TestRenderNotesEmpty(t *testing.T) {
	if got := RenderNotes(NoteSet{}); got != "" {
		t.Errorf("empty NoteSet must render as empty string, got %q", got)
	}
}

func TestRenderNotesTwoSections(t *testing.T) {
	out := RenderNotes(NoteSet{
		Subscript: []Note{
			{AnchorID: "fn:3-r2", RefID: "fnref:3-r2", Marker: "(1)", Kind: "subscript", BodyHTML: "Боюсь данайцев"},
		},
		Endnote: []Note{
			{AnchorID: "fn:3-1", RefID: "fnref:3-1", Marker: "1", Kind: "endnote", BodyHTML: "Заметки о новейшей"},
		},
	})

	for _, want := range []string{
		`<div class="footnotes">`,
		`<section class="notes-group notes-group--subscript">`,
		"<h2>Подстрочные примечания</h2>",
		`<li id="fn:3-r2" class="fn-item fn-item--subscript">`,
		`<a class="fn-back" href="#fnref:3-r2">(1)</a>`,
		"Боюсь данайцев",
		`<section class="notes-group notes-group--endnote">`,
		"<h2>Примечания</h2>",
		`<li id="fn:3-1" class="fn-item fn-item--endnote">`,
		`<a class="fn-back" href="#fnref:3-1">1</a>`,
		"Заметки о новейшей",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	// Постраничной группировки быть не должно.
	if strings.Contains(out, "<h3>") || strings.Contains(out, "Страница") {
		t.Errorf("output must not group by page:\n%s", out)
	}

	// Подстрочные идут раньше редакционных.
	if strings.Index(out, "notes-group--subscript") > strings.Index(out, "notes-group--endnote") {
		t.Errorf("subscript section must come first:\n%s", out)
	}
}

func TestRenderNotesSkipsEmptySection(t *testing.T) {
	onlySub := RenderNotes(NoteSet{
		Subscript: []Note{{AnchorID: "fn:3-r1", RefID: "fnref:3-r1", Marker: "(1)", Kind: "subscript", BodyHTML: "x"}},
	})
	if strings.Contains(onlySub, "notes-group--endnote") || strings.Contains(onlySub, "<h2>Примечания</h2>") {
		t.Errorf("empty endnote section must be omitted:\n%s", onlySub)
	}

	onlyEnd := RenderNotes(NoteSet{
		Endnote: []Note{{AnchorID: "fn:3-1", RefID: "fnref:3-1", Marker: "1", Kind: "endnote", BodyHTML: "y"}},
	})
	if strings.Contains(onlyEnd, "notes-group--subscript") {
		t.Errorf("empty subscript section must be omitted:\n%s", onlyEnd)
	}
	if !strings.Contains(onlyEnd, "<h2>Примечания</h2>") {
		t.Errorf("endnote section must be present:\n%s", onlyEnd)
	}
}

func TestCollectPagesRenumbersSubscriptsAcrossPages(t *testing.T) {
	r := NewRenderer()
	pages := []PageContent{
		{PageNumber: 3, Content: "Alpha[^r2].\n\n[^r2]: first sub."},
		{PageNumber: 9, Content: "Beta[^r2].\n\n[^r2]: second sub."},
		{PageNumber: 11, Content: "Gamma[^r1].\n\n[^r1]: third sub."},
	}
	pageHTML, notes := r.CollectPages(pages)

	if len(pageHTML) != 3 {
		t.Fatalf("expected 3 page HTML blobs, got %d", len(pageHTML))
	}
	if len(notes.Subscript) != 3 {
		t.Fatalf("expected 3 subscript notes, got %d: %+v", len(notes.Subscript), notes.Subscript)
	}
	if len(notes.Endnote) != 0 {
		t.Errorf("expected no endnotes, got %+v", notes.Endnote)
	}

	wantMarkers := []string{"(1)", "(2)", "(3)"}
	for i, want := range wantMarkers {
		if notes.Subscript[i].Marker != want {
			t.Errorf("note %d marker = %q, want %q", i, notes.Subscript[i].Marker, want)
		}
	}

	// Тот же маркер стоит и в тексте.
	for i, want := range wantMarkers {
		if !strings.Contains(pageHTML[i], ">"+want+"</a>") {
			t.Errorf("page %d in-text marker %q missing:\n%s", i, want, pageHTML[i])
		}
	}

	// Якоря остались постранично уникальными.
	if notes.Subscript[0].AnchorID != "fn:3-r2" || notes.Subscript[1].AnchorID != "fn:9-r2" {
		t.Errorf("anchors must stay page-namespaced: %+v", notes.Subscript)
	}
}

func TestCollectPagesKeepsEndnoteNumbers(t *testing.T) {
	r := NewRenderer()
	pages := []PageContent{
		{PageNumber: 3, Content: "A[^7] B[^r1].\n\n[^7]: endnote seven.\n\n[^r1]: sub one."},
		{PageNumber: 4, Content: "C[^12].\n\n[^12]: endnote twelve."},
	}
	pageHTML, notes := r.CollectPages(pages)

	if len(notes.Endnote) != 2 {
		t.Fatalf("expected 2 endnotes, got %+v", notes.Endnote)
	}
	if notes.Endnote[0].Marker != "7" || notes.Endnote[1].Marker != "12" {
		t.Errorf("endnote markers must keep book numbering, got %q and %q",
			notes.Endnote[0].Marker, notes.Endnote[1].Marker)
	}
	if len(notes.Subscript) != 1 || notes.Subscript[0].Marker != "(1)" {
		t.Errorf("subscript counter must not be advanced by endnotes: %+v", notes.Subscript)
	}
	if !strings.Contains(pageHTML[0], ">7</a>") {
		t.Errorf("endnote marker 7 missing in text:\n%s", pageHTML[0])
	}
}

func TestCollectPagesStripsFootnoteBlocks(t *testing.T) {
	r := NewRenderer()
	pageHTML, _ := r.CollectPages([]PageContent{
		{PageNumber: 3, Content: "Text[^r1].\n\n[^r1]: the note body."},
	})
	if strings.Contains(pageHTML[0], "the note body") {
		t.Errorf("footnote body must not stay in page HTML:\n%s", pageHTML[0])
	}
	if strings.Contains(pageHTML[0], `class="footnotes"`) {
		t.Errorf("footnotes block must be stripped:\n%s", pageHTML[0])
	}
}

func TestCollectPagesOrphanDefinition(t *testing.T) {
	r := NewRenderer()
	// Страница 5: определение [^r9] есть, ссылки на него в тексте нет.
	pages := []PageContent{
		{PageNumber: 5, Content: "Only text.\n\n[^r9]: orphan body.\n"},
		{PageNumber: 6, Content: "Ref[^r1].\n\n[^r1]: linked body."},
	}
	_, notes := r.CollectPages(pages)

	var linked, orphan *Note
	for i := range notes.Subscript {
		switch notes.Subscript[i].AnchorID {
		case "fn:6-r1":
			linked = &notes.Subscript[i]
		case "fn:5-r9":
			orphan = &notes.Subscript[i]
		}
	}
	if linked == nil {
		t.Fatalf("linked note missing: %+v", notes.Subscript)
	}
	// Связанное определение нумеруется первым по порядку появления в тексте.
	if linked.Marker != "(1)" {
		t.Errorf("linked note marker = %q, want (1)", linked.Marker)
	}
	if orphan == nil {
		t.Fatalf("orphan definition must still be listed: %+v", notes.Subscript)
	}
	// Осиротевшее определение продолжает ОБЩИЙ счётчик, а не хранит маркер,
	// произведённый из своего имени ("r9" -> "(9)") — иначе оно могло бы
	// столкнуться с уже занятым номером. См. finding 2.
	if orphan.Marker != "(2)" {
		t.Errorf("orphan marker = %q, want (2) (continuing the sequence)", orphan.Marker)
	}
}

// TestCollectPagesOrphanMarkerNeverCollidesWithAssigned reproduces the exact
// review defect: an orphan's name-derived marker duplicating an
// already-assigned subscript marker. Two notes ended up both labelled "(2)"
// because the orphan (source name "r2") kept "(2)" while a later-appearing
// referenced note ("r3") also renumbered to "(2)". After the fix every
// subscript marker in the returned NoteSet must be distinct, and the orphan
// must continue the sequence rather than repeat one.
func TestCollectPagesOrphanMarkerNeverCollidesWithAssigned(t *testing.T) {
	r := NewRenderer()
	pages := []PageContent{
		{PageNumber: 6, Content: "First[^r1]. Third[^r3].\n\n" +
			"[^r1]: Первое.\n\n[^r3]: Третье примечание.\n\n[^r2]: Второе примечание.\n"},
	}
	_, notes := r.CollectPages(pages)

	seenMarkers := map[string]bool{}
	var orphanMarker string
	for _, n := range notes.Subscript {
		if seenMarkers[n.Marker] {
			t.Fatalf("duplicate subscript marker %q in %+v", n.Marker, notes.Subscript)
		}
		seenMarkers[n.Marker] = true
		if n.AnchorID == "fn:6-r2" {
			orphanMarker = n.Marker
		}
	}
	if len(notes.Subscript) != 3 {
		t.Fatalf("expected 3 subscript notes, got %d: %+v", len(notes.Subscript), notes.Subscript)
	}
	// Two referenced notes take (1) and (2) in text order; the orphan must
	// continue to (3), not repeat the referenced "(2)".
	if orphanMarker != "(3)" {
		t.Errorf("orphan marker = %q, want (3) (continuing the sequence, not repeating (2))", orphanMarker)
	}
}

// TestCollectPagesReferenceInsideAnotherNoteBody covers a reference that sits
// inside another note's body — real in the corpus: a "Далее в рукописи
// перечёркнуто…" subscript note quotes a passage that itself carries a note
// marker. Such a <sup> is rendered inside the footnotes block, which
// renderPageNotes strips out of the page HTML, so scanning page HTML alone
// never sees it: the note was treated as an orphan (pushed to the tail of its
// section and renumbered there) while the marker printed inside the body kept
// gomarkdown's page-local number — the two drifted apart.
func TestCollectPagesReferenceInsideAnotherNoteBody(t *testing.T) {
	r := NewRenderer()
	pages := []PageContent{
		{PageNumber: 5, Content: "One[^r1].\n\n[^r1]: first, see also[^r2] here.\n\n[^r2]: second body.\n"},
		{PageNumber: 6, Content: "Two[^r1].\n\n[^r1]: page six note.\n"},
	}
	_, notes := r.CollectPages(pages)

	if len(notes.Subscript) != 3 {
		t.Fatalf("expected 3 subscript notes, got %d: %+v", len(notes.Subscript), notes.Subscript)
	}
	// A note referenced from inside another note's body is referenced, so it
	// takes its number where that reference appears — right after the notes of
	// the page whose body carries it — not at the orphan tail.
	want := []struct{ anchor, marker string }{
		{"fn:5-r1", "(1)"},
		{"fn:5-r2", "(2)"},
		{"fn:6-r1", "(3)"},
	}
	for i, w := range want {
		if notes.Subscript[i].AnchorID != w.anchor || notes.Subscript[i].Marker != w.marker {
			t.Errorf("note %d = (%s, %s), want (%s, %s)",
				i, notes.Subscript[i].AnchorID, notes.Subscript[i].Marker, w.anchor, w.marker)
		}
	}

	// The marker printed inside the containing body must be the same one the
	// list shows for that note — one map for both places, no drift.
	var host *Note
	for i := range notes.Subscript {
		if notes.Subscript[i].AnchorID == "fn:5-r1" {
			host = &notes.Subscript[i]
		}
	}
	if host == nil {
		t.Fatalf("host note fn:5-r1 missing: %+v", notes.Subscript)
	}
	if !strings.Contains(host.BodyHTML, `id="fnref:5-r2"`) {
		t.Fatalf("host body lost the nested reference: %q", host.BodyHTML)
	}
	if !strings.Contains(host.BodyHTML, ">(2)</a>") {
		t.Errorf("nested marker inside body must be the scope marker (2), got %q", host.BodyHTML)
	}
}

// TestCollectPagesEndnotesSortedByBookNumber pins the reading order of the
// «Примечания» section: endnote numbering belongs to the book, so the section
// must run in ascending book order regardless of the order the references
// happen to appear in the text — and regardless of whether a definition has a
// reference at all. Before the fix an unreferenced definition was appended
// after every referenced one, so the list read 3, 6, 8, 7 and looked like
// note 7 had been skipped.
func TestCollectPagesEndnotesSortedByBookNumber(t *testing.T) {
	r := NewRenderer()
	pages := []PageContent{
		{PageNumber: 15, Content: "A[^3].\n\n[^3]: three."},
		{PageNumber: 22, Content: "B[^6].\n\n[^6]: six."},
		{PageNumber: 32, Content: "No reference here.\n\n[^7]: seven, unlinked."},
		{PageNumber: 34, Content: "C[^8].\n\n[^8]: eight."},
	}
	_, notes := r.CollectPages(pages)

	var got []string
	for _, n := range notes.Endnote {
		got = append(got, n.Marker)
	}
	if strings.Join(got, ",") != "3,6,7,8" {
		t.Errorf("endnote order = %v, want [3 6 7 8] (ascending book order)", got)
	}
}

// A name that is neither a book number nor a subscript name still has to land
// somewhere deterministic — after the numbered notes — without panicking.
func TestCollectPagesEndnotesSortNonNumericLast(t *testing.T) {
	r := NewRenderer()
	pages := []PageContent{
		{PageNumber: 3, Content: "A[^x1] B[^2].\n\n[^x1]: odd name.\n\n[^2]: two."},
	}
	_, notes := r.CollectPages(pages)
	if len(notes.Endnote) != 2 {
		t.Fatalf("expected 2 endnotes, got %+v", notes.Endnote)
	}
	if notes.Endnote[0].Marker != "2" || notes.Endnote[1].Marker != "x1" {
		t.Errorf("non-numeric endnote must sort last, got %q then %q",
			notes.Endnote[0].Marker, notes.Endnote[1].Marker)
	}
}

func TestCollectPagesEmpty(t *testing.T) {
	r := NewRenderer()
	pageHTML, notes := r.CollectPages(nil)
	if len(pageHTML) != 0 {
		t.Errorf("expected no page HTML, got %d", len(pageHTML))
	}
	if RenderNotes(notes) != "" {
		t.Errorf("expected empty notes render")
	}
}

func TestCollectPagesOrphanBodyRendersMarkdown(t *testing.T) {
	r := NewRenderer()
	pages := []PageContent{
		{PageNumber: 5, Content: "Only text.\n\n[^r9]: an *emphasized* word.\n"},
	}
	_, notes := r.CollectPages(pages)
	if len(notes.Subscript) != 1 {
		t.Fatalf("expected 1 subscript note, got %+v", notes.Subscript)
	}
	body := notes.Subscript[0].BodyHTML
	if !strings.Contains(body, "<em>emphasized</em>") {
		t.Errorf("orphan body must render markdown emphasis as HTML, got %q", body)
	}
	if strings.Contains(body, "*emphasized*") {
		t.Errorf("orphan body must not contain literal markdown asterisks, got %q", body)
	}
}

func TestCollectPagesOrphanBodyEscapesHTML(t *testing.T) {
	r := NewRenderer()
	pages := []PageContent{
		{PageNumber: 5, Content: "Only text.\n\n[^r9]: 5 < 10 & true.\n"},
	}
	_, notes := r.CollectPages(pages)
	if len(notes.Subscript) != 1 {
		t.Fatalf("expected 1 subscript note, got %+v", notes.Subscript)
	}
	body := notes.Subscript[0].BodyHTML
	if strings.Contains(body, "5 < 10") {
		t.Errorf("orphan body must escape a raw '<', got %q", body)
	}
	if !strings.Contains(body, "&amp;") {
		t.Errorf("orphan body must escape a raw '&' as &amp;, got %q", body)
	}
}

func TestCollectPagesOrphanBodyMultiLine(t *testing.T) {
	r := NewRenderer()
	pages := []PageContent{
		{PageNumber: 5, Content: "Only text.\n\n[^r9]: first line of the note\n    continues on a second line.\n"},
	}
	_, notes := r.CollectPages(pages)
	if len(notes.Subscript) != 1 {
		t.Fatalf("expected 1 subscript note, got %+v", notes.Subscript)
	}
	body := notes.Subscript[0].BodyHTML
	if !strings.Contains(body, "first line of the note") || !strings.Contains(body, "continues on a second line") {
		t.Errorf("multi-line orphan body must survive intact, got %q", body)
	}
}

func TestCollectPagesOrphanIgnoresFencedCode(t *testing.T) {
	r := NewRenderer()
	pages := []PageContent{
		{PageNumber: 5, Content: "Some text.\n\n```\n[^r9]: not a real definition, just an example\n```\n"},
	}
	_, notes := r.CollectPages(pages)
	if len(notes.Subscript) != 0 || len(notes.Endnote) != 0 {
		t.Errorf("a footnote-shaped line inside a fenced code block must not be collected as an orphan: %+v", notes)
	}
}

// Regression: «term[^s1] (перевод)» was parsed as an inline link, so the note
// never got an in-text <sup>. CollectPages then took it for an orphan and
// dumped it at the end of the subscript list — a whole run of "extra" notes
// after the last real one, out of page order. Both notes here must come back in
// reading order with sequential markers.
func TestCollectPagesFootnoteBeforeParenIsNotOrphan(t *testing.T) {
	r := NewRenderer()
	pages := []PageContent{
		{PageNumber: 10, Content: "Первое[^s1] далее.\n\n[^s1]: — первое. *Ред.*"},
		{PageNumber: 11, Content: "resp.[^s1] (поскольку так) далее.\n\n[^s1]: — второе. *Ред.*"},
	}

	html, notes := r.CollectPages(pages)

	if len(notes.Subscript) != 2 {
		t.Fatalf("want 2 subscript notes, got %d: %+v", len(notes.Subscript), notes.Subscript)
	}
	if notes.Subscript[1].AnchorID != "fn:11-s1" || notes.Subscript[1].Marker != "(2)" {
		t.Errorf("note on page 11 must follow page 10 in reading order, got %+v", notes.Subscript[1])
	}
	if !strings.Contains(html[1], `id="fnref:11-s1"`) {
		t.Errorf("page 11 lost its in-text marker:\n%s", html[1])
	}
	if !strings.Contains(html[1], "(поскольку так)") {
		t.Errorf("page 11 lost the parenthesised text:\n%s", html[1])
	}
}

// Разбор — первое место, где рядом встают полосы РАЗНЫХ томов, а номера
// полос у каждого тома начинаются с единицы. Без области имён том 3 с. 12 и
// том 19 с. 12 дают один якорь fn:12-r1, и ссылка ведёт в чужое примечание.
// Комментарий CollectPages предупреждал об этом задолго до разбора.
func TestCollectPagesScopedSeparatesSamePageNumbers(t *testing.T) {
	r := NewRenderer()
	first, _ := r.CollectPagesScoped("v1-", []PageContent{
		{PageNumber: 12, Content: "Маркс писал[^r1].\n\n[^r1]: примечание первого тома"},
	})
	second, _ := r.CollectPagesScoped("v2-", []PageContent{
		{PageNumber: 12, Content: "Ленин писал[^r1].\n\n[^r1]: примечание второго тома"},
	})

	if !strings.Contains(first[0], "fnref:v1-12-r1") {
		t.Fatalf("первая вклейка без своей области имён: %s", first[0])
	}
	if !strings.Contains(second[0], "fnref:v2-12-r1") {
		t.Fatalf("вторая вклейка без своей области имён: %s", second[0])
	}
}

// Область имён не должна ломать разбор имени: подстрочная остаётся
// подстрочной и печатается «(1)», а не «v2-12-r1».
func TestScopedNamesStillParseAsSubscriptAndEndnote(t *testing.T) {
	r := NewRenderer()
	_, notes := r.CollectPagesScoped("v2-", []PageContent{
		{PageNumber: 12, Content: "Текст[^r1] и ссылка[^566].\n\n[^r1]: подстрочное\n\n[^566]: редакционное"},
	})
	if len(notes.Subscript) != 1 || notes.Subscript[0].Marker != "(1)" {
		t.Fatalf("подстрочные: %+v", notes.Subscript)
	}
	if len(notes.Endnote) != 1 || notes.Endnote[0].Marker != "566" {
		t.Fatalf("редакционные: %+v", notes.Endnote)
	}
}

// Прежний вызов не меняет поведения ни на знак: у главы область пустая.
func TestCollectPagesKeepsBareAnchors(t *testing.T) {
	r := NewRenderer()
	html, _ := r.CollectPages([]PageContent{
		{PageNumber: 12, Content: "Текст[^r1].\n\n[^r1]: примечание"},
	})
	if !strings.Contains(html[0], "fnref:12-r1") {
		t.Fatalf("якорь главы изменился: %s", html[0])
	}
}

// Блок сносок под вклейкой стоит ВНУТРИ авторского текста. <h2> там
// становится узлом оглавления разбора наравне с заголовками автора, и между
// ними встали бы три-четыре «Примечания» от корпуса. Понижение до <h4> не
// лечит — тоже узел.
func TestRenderNotesInlineHasNoHeading(t *testing.T) {
	set := NoteSet{Subscript: []Note{{
		AnchorID: "fn:v1-12-r1", RefID: "fnref:v1-12-r1",
		Marker: "(1)", Kind: "subscript", BodyHTML: "тело",
	}}}

	inline := RenderNotesWith(set, NotesOptions{Inline: true})
	if strings.Contains(inline, "<h2") || strings.Contains(inline, "<h3") || strings.Contains(inline, "<h4") {
		t.Fatalf("блок под вклейкой несёт заголовок: %s", inline)
	}
	if !strings.Contains(inline, "Подстрочные примечания") {
		t.Fatalf("блок под вклейкой потерял подпись: %s", inline)
	}
}

// Прежняя форма не меняется: RenderNotes одна на главу, элемент подборки,
// поток чтения и четырёх писателей скачивания.
func TestRenderNotesKeepsHeadingByDefault(t *testing.T) {
	set := NoteSet{Endnote: []Note{{
		AnchorID: "fn:12-566", RefID: "fnref:12-566",
		Marker: "566", Kind: "endnote", BodyHTML: "тело",
	}}}
	if !strings.Contains(RenderNotes(set), "<h2>Примечания</h2>") {
		t.Fatalf("заголовок главы изменился: %s", RenderNotes(set))
	}
}
