package markdown

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Note is one collected note: its anchors, its final display marker, and its
// rendered body. Markers are assigned by the collector for a whole reading
// scope (a chapter or a work), not per page — see CollectPages.
type Note struct {
	AnchorID string // "fn:3-r2"    — jump target for the in-text marker
	RefID    string // "fnref:3-r2" — where the "↩" back-link returns
	Marker   string // "(1)" | "5"  — what the reader sees
	Kind     string // "subscript" | "endnote"
	BodyHTML string
}

// NoteSet holds a reading scope's notes, split by kind. Within each slice the
// order is the order of first appearance in the text.
type NoteSet struct {
	Subscript []Note // page-local editorial/translator notes, renumbered
	Endnote   []Note // volume-level editorial notes, book numbering kept
}

// NotesOptions — как подписать блок сносок.
type NotesOptions struct {
	// Inline: блок стоит внутри чужого текста (под вклейкой в разборе), и
	// подписи разделов не должны становиться узлами оглавления.
	Inline bool
}

// RenderNotes — прежняя форма: заголовками. Зовут глава, элемент подборки,
// поток чтения и четыре писателя скачивания; менять её на месте нельзя,
// иначе переверстается весь корпус.
func RenderNotes(n NoteSet) string {
	return RenderNotesWith(n, NotesOptions{})
}

// RenderNotesWith renders a scope's notes as two sections. A section with no
// items is omitted entirely; an empty NoteSet renders as "". The result is a
// strict-XHTML-normalized fragment (see fragment) — never a wrapped document,
// though RenderNotesWith never puts up a wrapper in the first place;
// normalization is what fragment is doing here.
//
// o.Inline печатает подпись раздела абзацем вместо <h2> — вклейка разбора
// вставляет блок сносок посреди авторского текста, и <h2> «Примечания» там
// встал бы узлом оглавления наравне с заголовками автора.
func RenderNotesWith(n NoteSet, o NotesOptions) string {
	if len(n.Subscript) == 0 && len(n.Endnote) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("<div class=\"footnotes\">\n")
	writeNoteSection(&b, "subscript", "Подстрочные примечания", n.Subscript, o)
	writeNoteSection(&b, "endnote", "Примечания", n.Endnote, o)
	b.WriteString("</div>")
	// Обёртки документа здесь нет — блок собран вручную. Через fragment он
	// идёт ради второго свойства разбора: в телах сносок живёт ручной HTML, и
	// сырой <br> оттуда ломает EPUB. Раньше это делал download_source.go, и
	// однажды примечания шли мимо разбора — см. notesFractionFixture в
	// internal/api/download_source_test.go.
	return fragment(b.String())
}

func writeNoteSection(b *strings.Builder, kind, title string, notes []Note, o NotesOptions) {
	if len(notes) == 0 {
		return
	}
	fmt.Fprintf(b, "<section class=\"notes-group notes-group--%s\">\n", kind)
	if o.Inline {
		// Абзац, не заголовок: под вклейкой этот блок — часть авторского
		// текста, и он не должен добавлять узел в оглавление разбора.
		fmt.Fprintf(b, "<p class=\"notes-group-caption\">%s</p>\n", title)
	} else {
		fmt.Fprintf(b, "<h2>%s</h2>\n", title)
	}
	b.WriteString("<ul class=\"fn-list\">\n")
	for _, it := range notes {
		fmt.Fprintf(b,
			"<li id=\"%s\" class=\"fn-item fn-item--%s\">"+
				"<a class=\"fn-back\" href=\"#%s\">%s</a> %s</li>\n",
			it.AnchorID, it.Kind, it.RefID, it.Marker, it.BodyHTML)
	}
	b.WriteString("</ul>\n</section>\n")
}

// noteSupRe matches an in-text footnote marker AFTER rewriteFootnoteMarkers has
// run, i.e. with the kind class already attached. Groups: kind, fnref name,
// href. Used both to discover the order of first appearance and to substitute
// the scope-wide marker.
var noteSupRe = regexp.MustCompile(
	`<sup class="footnote-ref footnote-ref--(\w+)" id="fnref:([^"]+)"><a href="(#fn:[^"]+)">[^<]*</a></sup>`)

// orphanDefLineRe matches the start of a raw footnote definition line in a
// page's *unprefixed* markdown source, e.g. "[^r9]: orphan body." — only the
// name is captured; the body is never hand-extracted (see scanOrphanDefNames).
var orphanDefLineRe = regexp.MustCompile(`^\[\^([^\]]+)\]:`)

// fenceRe matches the start of a fenced code block delimiter line (three or
// more backticks or tildes), used by scanOrphanDefNames to skip lines inside
// a fence so a line shaped like "[^x]: text" in a code sample is never
// mistaken for a real footnote definition.
var fenceRe = regexp.MustCompile("^(`{3,}|~{3,})")

// scanOrphanDefNames finds footnote definitions in a page's raw markdown that
// have no in-text reference anywhere in `referenced` (the names already
// covered by a normal render — see CollectPages), skipping any line inside a
// fenced code block. Only fence-awareness for the ``` / ~~~ style is
// implemented; a footnote-definition-shaped line inside an indented code
// block (4-space indent, no fence) is not currently excluded — a narrower
// residual gap than the previous implementation, which had none of this
// protection.
func scanOrphanDefNames(content string, referenced map[string]bool) []string {
	var names []string
	seen := map[string]bool{}
	inFence := false
	var fenceChar byte
	fenceLen := 0

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if m := fenceRe.FindString(trimmed); m != "" {
			switch {
			case !inFence:
				inFence = true
				fenceChar = trimmed[0]
				fenceLen = len(m)
			case trimmed[0] == fenceChar && len(m) >= fenceLen:
				inFence = false
			}
			continue
		}
		if inFence {
			continue
		}
		m := orphanDefLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[1]
		if referenced[name] || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// CollectPages — область без имени: глава, работа, поток чтения. Полосы
// внутри одного вызова обязаны иметь различные номера (UNIQUE (work_id,
// page_number)). Разбор эту предпосылку нарушает первым — он собирает полосы
// разных томов, — и потому зовёт CollectPagesScoped с номером вклейки.
func (r *Renderer) CollectPages(pages []PageContent) ([]string, NoteSet) {
	return r.CollectPagesScoped("", pages)
}

// CollectPagesScoped renders pages in order, strips each page's footnote
// block, and assigns display markers for the whole scope: subscript notes
// are renumbered "(1)", "(2)", … in order of first appearance in the text,
// endnotes keep the book number they carry. The same marker map is applied
// to the in-text <sup> and to the note list, so the two cannot drift. The
// returned page HTML is an XHTML fragment, never a wrapped document (see the
// package's contract).
//
// scope namespaces footnote anchors on top of the page number: "" for a
// chapter/work/reading stream (see CollectPages), a per-вклейка prefix for
// разбор — the one place where PageNumber alone is not unique across the
// call, since each volume's pages start at 1 again.
//
// A reference may also sit inside another note's body — e.g. a "Далее в
// рукописи перечёркнуто…" note quoting a passage that itself carries a note
// marker. renderPageNotes strips the footnotes block out of the page HTML, so
// such a <sup> lives only in the containing note's BodyHTML; both passes below
// therefore scan the page's note bodies as well as its main HTML, otherwise the
// note would be mistaken for an orphan and its two markers would drift apart.
//
// A definition with no in-text reference (an orphan) is still returned. A
// subscript orphan is appended after the referenced subscript notes and
// continues the SAME "(N)" counter used for them (never keeps the marker
// derived from its source name), so no two subscript notes in the returned
// NoteSet ever share a marker. An endnote orphan keeps its book number, like
// any other endnote, and is placed by that number: the Endnote slice is sorted
// into ascending book order (see sortEndnotes), since endnote numbering comes
// from the printed volume and a reader scanning «Примечания» for number N
// expects it between N-1 and N+1 whether or not the text reference survived
// OCR.
//
// Precondition: pages must have distinct PageNumbers within the scope
// (true for a chapter or a work, per the DB's UNIQUE(work_id, page_number) —
// see internal/database/migrations). byRef is keyed by "fnref:<scope><page>-<name>"; if
// two pages in the same call shared a (scope, PageNumber) pair, a note from
// one could silently overwrite the other's entry in byRef. Composite pages,
// which assemble ranges across works and so can repeat page numbers, use a
// non-empty scope per вклейка — that is the whole reason this function takes
// one.
func (r *Renderer) CollectPagesScoped(scope string, pages []PageContent) ([]string, NoteSet) {
	pageHTML := make([]string, len(pages))
	byRef := map[string]*Note{}
	var inDefOrder []*Note
	pageNotes := make([][]*Note, len(pages)) // per page, in definition order

	for i, p := range pages {
		html, items := r.renderPageNotes(scope, p.PageNumber, p.Content)
		pageHTML[i] = html
		pageNamePrefix := "fn:" + notePrefix(scope, p.PageNumber)

		referenced := map[string]bool{}
		for _, it := range items {
			referenced[strings.TrimPrefix(it.AnchorID, pageNamePrefix)] = true
		}

		// Orphan recovery: gomarkdown drops a footnote definition from its
		// output entirely when the page has no in-text reference to it, so it
		// never reaches items above. Rather than hand-extracting the body
		// (raw, unrendered, unescaped, single-line-only), re-render the page
		// with one synthetic reference appended per orphan name so each
		// definition goes through the exact same rendering path as a normal
		// one — correct markdown formatting and HTML escaping, multi-line
		// bodies, nested lists, return-link handling all come for free. Only
		// the orphan-named items are kept from that second render; `html`
		// above is untouched and already correct, since an orphan (by
		// definition) never appeared in it.
		if orphanNames := scanOrphanDefNames(p.Content, referenced); len(orphanNames) > 0 {
			var augmented strings.Builder
			augmented.WriteString(p.Content)
			for _, name := range orphanNames {
				fmt.Fprintf(&augmented, "\n\n[^%s]", name)
			}
			_, augmentedItems := r.renderPageNotes(scope, p.PageNumber, augmented.String())

			orphanSet := make(map[string]bool, len(orphanNames))
			for _, name := range orphanNames {
				orphanSet[name] = true
			}
			for _, it := range augmentedItems {
				if orphanSet[strings.TrimPrefix(it.AnchorID, pageNamePrefix)] {
					items = append(items, it)
				}
			}
		}

		for _, it := range items {
			n := &Note{
				AnchorID: it.AnchorID,
				RefID:    it.RefID,
				Marker:   it.Marker,
				Kind:     it.Kind,
				BodyHTML: it.BodyHTML,
			}
			byRef[n.RefID] = n
			inDefOrder = append(inDefOrder, n)
			pageNotes[i] = append(pageNotes[i], n)
		}
	}

	// Pass 1: walk the pages in order, left to right, assigning markers. Within
	// a page the main HTML comes first, then that page's note bodies — a marker
	// printed inside a note body appears, in reading order, with the notes block
	// that follows the page text.
	var subscript, endnote []*Note
	seen := map[string]bool{}
	subCount := 0
	assignMarkers := func(html string) {
		for _, m := range noteSupRe.FindAllStringSubmatch(html, -1) {
			refID := "fnref:" + m[2]
			n, ok := byRef[refID]
			if !ok || seen[refID] {
				continue
			}
			seen[refID] = true
			if n.Kind == "subscript" {
				subCount++
				n.Marker = "(" + strconv.Itoa(subCount) + ")"
				subscript = append(subscript, n)
			} else {
				endnote = append(endnote, n)
			}
		}
	}
	for i, html := range pageHTML {
		assignMarkers(html)
		for _, n := range pageNotes[i] {
			assignMarkers(n.BodyHTML)
		}
	}

	// Orphans are collected after the referenced notes — which is where a
	// subscript orphan then stays; an endnote orphan is moved into its place by
	// book number when the endnote slice is sorted further down.
	//
	// A subscript orphan continues the SAME counter used
	// above for referenced subscript notes — both schemes share one
	// namespace (the parenthesized marker), so keeping the orphan's
	// name-derived marker (e.g. "(9)" from a source name like "r9") could
	// collide with an already-assigned "(9)" from an unrelated referenced
	// note. Renumbering the orphan into the sequence makes "no two
	// subscript notes share a marker" hold across the whole reading scope,
	// referenced or not. Endnote orphans are unaffected: they keep the book
	// number their name carries, same as referenced endnotes, since endnote
	// numbering is not scope-local and must not be reassigned.
	//
	// Note also that an orphan's fn-back link (built from its RefID) points
	// at an "fnref:" anchor that never went into the page HTML — there is no
	// in-text <sup> for a definition with no reference — so the back-link
	// renders but is inert by construction, not a bug.
	for _, n := range inDefOrder {
		if seen[n.RefID] {
			continue
		}
		if n.Kind == "subscript" {
			subCount++
			n.Marker = "(" + strconv.Itoa(subCount) + ")"
			subscript = append(subscript, n)
		} else {
			endnote = append(endnote, n)
		}
	}

	// Pass 2: substitute the assigned markers into every <sup> — in the page
	// text and in the note bodies alike, so the marker a reader sees inside a
	// quoted passage is the same one its list item carries.
	substitute := func(html string) string {
		return noteSupRe.ReplaceAllStringFunc(html, func(s string) string {
			m := noteSupRe.FindStringSubmatch(s)
			n, ok := byRef["fnref:"+m[2]]
			if !ok {
				return s
			}
			return fmt.Sprintf(
				`<sup class="footnote-ref footnote-ref--%s" id="fnref:%s"><a href="%s">%s</a></sup>`,
				m[1], m[2], m[3], n.Marker)
		})
	}
	for i, html := range pageHTML {
		pageHTML[i] = substitute(html)
	}
	for _, n := range inDefOrder {
		n.BodyHTML = substitute(n.BodyHTML)
	}

	// Endnotes read in book order; subscript notes keep the sequence their
	// counter just assigned (already ascending by construction).
	sortEndnotes(endnote)

	var set NoteSet
	for _, n := range subscript {
		set.Subscript = append(set.Subscript, *n)
	}
	for _, n := range endnote {
		set.Endnote = append(set.Endnote, *n)
	}

	// Контракт пакета: наружу — фрагменты. Снятие стоит последним шагом, уже
	// после извлечения сносок и подстановки маркеров: от CompletePage-рендера
	// зависит только извлечение (footnotesBlockRe в renderPageNotes, см. выше);
	// подстановка маркеров идёт по noteSupRe и к обёртке документа отношения
	// не имеет.
	for i, html := range pageHTML {
		pageHTML[i] = fragment(html)
	}

	return pageHTML, set
}

// sortEndnotes orders a scope's endnotes by their book number, ascending. A
// marker that is not a plain number (noteMarker's fallback for an unexpected
// footnote name) has no place in that sequence and goes after the numbered
// ones, keeping its relative order of appearance.
func sortEndnotes(notes []*Note) {
	sort.SliceStable(notes, func(i, j int) bool {
		ni, errI := strconv.Atoi(notes[i].Marker)
		nj, errJ := strconv.Atoi(notes[j].Marker)
		iNumbered, jNumbered := errI == nil, errJ == nil
		if iNumbered && jNumbered {
			return ni < nj
		}
		return iNumbered && !jNumbered
	})
}
