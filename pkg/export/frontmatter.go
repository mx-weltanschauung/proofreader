// Package export renders the whole corpus — works, their pages and chapters —
// as a zip of markdown files meant to be committed to git. Byte-for-byte
// reproducibility is the point: the same data must always produce the same
// archive, so nothing here depends on wall-clock time or map iteration order.
package export

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"proofreader/internal/models"
)

// fmField is one frontmatter line. value is already rendered: quoted() for text
// coming from the database, plain for numbers, dates and enum values.
type fmField struct {
	key   string
	value string
}

// quoted renders s as a double-quoted YAML scalar. Text from the database may
// contain a colon or a quote, either of which breaks a plain scalar — and a
// raw newline or carriage return would break the frontmatter line itself, so
// those are flattened via oneLine before quoting.
func quoted(s string) string {
	s = oneLine(s)
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// renderFrontmatter renders fields between --- markers, in the given order.
// A field with an empty value is dropped: an absent line beats "null".
func renderFrontmatter(fields []fmField) string {
	var b strings.Builder
	b.WriteString("---\n")
	for _, f := range fields {
		if f.value == "" {
			continue
		}
		b.WriteString(f.key)
		b.WriteString(": ")
		b.WriteString(f.value)
		b.WriteString("\n")
	}
	b.WriteString("---\n")
	return b.String()
}

// normalizeBody makes page text byte-stable: every line-ending variant (CRLF,
// bare CR, LF) becomes LF, and the tail is exactly one newline (none at all
// when there is no text). A bare CR is treated as a line break too — it is a
// valid old-Mac line ending, not a character that belongs mid-line in output.
func normalizeBody(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	return s + "\n"
}

// oneLine collapses every line-ending variant (CRLF, bare CR, LF) into a
// space so a title cannot break a single-line construct — a frontmatter
// field, a table cell, or a chapters.md list entry.
func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// cell prepares text for a markdown table cell.
func cell(s string) string {
	return strings.ReplaceAll(oneLine(s), "|", `\|`)
}

// renderWorksIndex renders works.md — the top-level index of the export.
// works must already be ordered by the caller.
func renderWorksIndex(works []*models.Work) string {
	var b strings.Builder
	b.WriteString("| id | Название | Автор | Статус |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, w := range works {
		fmt.Fprintf(&b, "| %d | %s | %s | %s |\n", w.ID, cell(w.Title), cell(w.Author), w.Status)
	}
	return b.String()
}

// renderWorkFile renders works/NNNN/work.md.
func renderWorkFile(w *models.Work) string {
	date := ""
	if w.PublicationDate != nil {
		date = w.PublicationDate.UTC().Format("2006-01-02")
	}
	edition, volume, part := "", "", ""
	if w.EditionID != nil {
		edition = strconv.FormatInt(*w.EditionID, 10)
	}
	if w.VolumeNumber != nil {
		volume = strconv.Itoa(*w.VolumeNumber)
	}
	if w.VolumePart != nil {
		part = quoted(*w.VolumePart)
	}
	shelfLabel := ""
	if w.ShelfLabel != "" {
		shelfLabel = quoted(w.ShelfLabel)
	}
	description := ""
	if w.Description != "" {
		description = quoted(w.Description)
	}
	return renderFrontmatter([]fmField{
		{"id", strconv.FormatInt(w.ID, 10)},
		{"title", quoted(w.Title)},
		{"author", quoted(w.Author)},
		{"language", quoted(w.Language)},
		{"country", quoted(w.Country)},
		{"status", string(w.Status)},
		{"edition_id", edition},
		{"volume_number", volume},
		{"volume_part", part},
		{"page_offset", strconv.Itoa(w.PageOffset)},
		{"shelf_label", shelfLabel},
		{"description", description},
		{"role", w.Role},
		{"numbering_style", w.NumberingStyle},
		{"publication_date", date},
	})
}

// renderPageFile renders works/NNNN/pages/NNNN.md.
//
// Deliberately no chapter_id here: chapters are regularly recreated wholesale
// (the toc-chapters skill), which reassigns every chapter's id without
// touching any page text. A chapter_id field would turn that into a diff
// across every page file in the work — exactly the noise the export format is
// meant to avoid. The page-to-chapter link lives in chapters.md instead, via
// each chapter's page range.
func renderPageFile(p *models.Page) string {
	fm := renderFrontmatter([]fmField{
		{"page_number", strconv.Itoa(p.PageNumber)},
		{"status", string(p.Status)},
	})
	return fm + normalizeBody(p.ContentMarkdown)
}

// renderChaptersFile renders works/NNNN/chapters.md as a nested list.
func renderChaptersFile(roots []*models.Chapter) string {
	var b strings.Builder
	writeChapterNodes(&b, roots, 0)
	return b.String()
}

func writeChapterNodes(b *strings.Builder, nodes []*models.Chapter, depth int) {
	sorted := make([]*models.Chapter, len(nodes))
	copy(sorted, nodes)
	// Sorted here rather than trusted from the repository: order_number is the
	// intended order, id only breaks ties so the output cannot wobble.
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].OrderNumber != sorted[j].OrderNumber {
			return sorted[i].OrderNumber < sorted[j].OrderNumber
		}
		return sorted[i].ID < sorted[j].ID
	})
	for _, c := range sorted {
		fmt.Fprintf(b, "%s- [%d] %s — %s, %d–%d\n",
			strings.Repeat("  ", depth), c.ID, oneLine(c.Title), c.Type, c.StartPage, c.EndPage)
		writeChapterNodes(b, c.Children, depth+1)
	}
}
