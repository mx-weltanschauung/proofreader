package markdown

import (
	"fmt"
	"regexp"
	"strings"
)

// Footnote references stranded inside raw HTML blocks.
//
// The OCR pipeline emits printed tables as raw HTML (marker's output; rowspan
// and colspan mean a markdown table often cannot represent them), and a printed
// footnote marker sometimes sits inside a cell:
//
//	<table>…<td>товар в центнерах[^s1]</td>…</table>
//
//	[^s1]: Английский центнер составляет ¹⁄₂₀ большой тонны. *Ред.*
//
// gomarkdown passes a raw HTML block through verbatim and never parses inline
// markdown inside it, so such a reference is invisible to the footnote
// machinery. Two things follow, both measured before this code was written:
//
//   - the reader sees the literal «[^s1]» instead of a marker — and in a
//     chapter, where names are namespaced per page, the literal leaks the
//     internal form «[^500-s1]»;
//   - the definition is left unreferenced, and gomarkdown drops an unreferenced
//     definition from its output entirely, so on a page the note's body is
//     lost outright.
//
// hoistHTMLBlockNoteRefs repairs both at the source level, before parsing:
//
//  1. every reference inside a raw HTML block is replaced by the exact <sup>
//     markup gomarkdown itself emits for a footnote reference. Being inside an
//     HTML block, that markup survives verbatim — so the marker appears in the
//     right cell, and rewriteFootnoteMarkers/noteSupRe then treat it like any
//     other marker (kind class, scope-wide renumbering in reading order).
//  2. a carrier paragraph holding the same references is inserted right after
//     the block, so gomarkdown still sees each name referenced and renders its
//     definition through the normal path — correct escaping, multi-line bodies
//     and return links all come for free. The carrier is stripped from the
//     rendered HTML by stripNoteRefCarriers.
//
// Step 2 is the same trick CollectPages already uses to recover orphan
// definitions (see the comment above scanOrphanDefNames); here it runs one
// render earlier so the page view is fixed too.
//
// The carrier is inserted immediately after its block rather than at the end of
// the document, so gomarkdown's reference order — and with it the order of the
// definitions in the footnotes block — still matches reading order.

// carrierSentinel marks the synthetic paragraph that carries hoisted
// references through the parse. It must not occur in real text, and never
// reaches the reader: stripNoteRefCarriers removes the whole paragraph.
const carrierSentinel = "fnrefcarrier:2f8e1c"

// htmlBlockStartRe matches a line that opens a raw HTML block — an HTML tag,
// block-level by CommonMark's list, at the start of a line (up to three spaces
// of indent, as CommonMark allows). A tag not on this list is left alone: it
// would be inline HTML inside a paragraph, where gomarkdown parses footnote
// references natively and no hoisting is needed.
var htmlBlockStartRe = regexp.MustCompile(`^ {0,3}</?(?i:table|thead|tbody|tfoot|tr|td|th|caption|colgroup|col|div|dl|dt|dd|ul|ol|li|blockquote|figure|figcaption|section|article|aside|header|footer|nav|main|pre|center|form|fieldset|iframe|h[1-6]|hr|p)(?:[\s/>]|$)`)

// noteRefTokenRe matches one footnote reference token, e.g. "[^s1]" or
// "[^500-83]". A name holds no whitespace and no closing bracket.
var noteRefTokenRe = regexp.MustCompile(`\[\^([^\]\s]+)\]`)

// carrierParaRe matches the rendered carrier paragraph, sups and all.
var carrierParaRe = regexp.MustCompile(`(?s)<p>` + regexp.QuoteMeta(carrierSentinel) + `.*?</p>\n*`)

// noteRefSup builds the in-text reference markup exactly as gomarkdown emits
// it, so rewriteFootnoteMarkers matches and normalises it like any other.
// The marker text is filled in already, which keeps the output sane even for a
// caller that renders without that pass.
func noteRefSup(name string) string {
	text, _ := noteMarker(name)
	return fmt.Sprintf(
		`<sup class="footnote-ref" id="fnref:%s"><a href="#fn:%s">%s</a></sup>`,
		name, name, text)
}

// hoistHTMLBlockNoteRefs rewrites footnote references that sit inside a raw
// HTML block into their final markup and inserts a carrier paragraph after the
// block. Markdown outside raw HTML blocks — including markdown tables and
// fenced code — is returned untouched.
func hoistHTMLBlockNoteRefs(md string) string {
	if !strings.Contains(md, "[^") {
		return md
	}

	lines := strings.Split(md, "\n")
	out := make([]string, 0, len(lines)+2)

	inFence := false
	var fenceChar byte
	fenceLen := 0

	inBlock := false
	var hoisted []string
	seen := map[string]bool{}

	// closeBlock emits the carrier for the block just ended, if it had any
	// references, and resets the per-block state.
	closeBlock := func() {
		if len(hoisted) > 0 {
			var b strings.Builder
			b.WriteString(carrierSentinel)
			for _, name := range hoisted {
				fmt.Fprintf(&b, "[^%s]", name)
			}
			out = append(out, "", b.String())
		}
		inBlock = false
		hoisted = nil
		seen = map[string]bool{}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Fenced code: never touch what is inside, and a fence cannot open
		// while an HTML block is still running (the block ends at a blank
		// line, which a fence line is not).
		if m := fenceRe.FindString(trimmed); m != "" {
			switch {
			case !inFence:
				inFence = true
				fenceChar = trimmed[0]
				fenceLen = len(m)
			case trimmed[0] == fenceChar && len(m) >= fenceLen:
				inFence = false
			}
			out = append(out, line)
			continue
		}
		if inFence {
			out = append(out, line)
			continue
		}

		if !inBlock && htmlBlockStartRe.MatchString(line) {
			inBlock = true
		}

		if inBlock && trimmed == "" {
			// A blank line closes the HTML block. Emit it, then the carrier,
			// then keep the blank separation for whatever follows.
			out = append(out, "")
			closeBlock()
			out = append(out, "")
			continue
		}

		if inBlock {
			line = noteRefTokenRe.ReplaceAllStringFunc(line, func(tok string) string {
				name := noteRefTokenRe.FindStringSubmatch(tok)[1]
				if !seen[name] {
					seen[name] = true
					hoisted = append(hoisted, name)
				}
				return noteRefSup(name)
			})
		}
		out = append(out, line)
	}
	if inBlock {
		closeBlock()
	}

	return strings.Join(out, "\n")
}

// stripNoteRefCarriers removes the rendered carrier paragraphs, leaving only
// the markup hoistHTMLBlockNoteRefs placed inside the HTML block itself.
func stripNoteRefCarriers(html string) string {
	return carrierParaRe.ReplaceAllString(html, "")
}
