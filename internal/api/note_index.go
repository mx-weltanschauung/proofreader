package api

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"proofreader/internal/models"
	"proofreader/pkg/markdown"
)

// NoteIndexEntry is one editorial note in the per-work index.
type NoteIndexEntry struct {
	Number       int    `json:"number"`
	TargetPage   int    `json:"target_page"`
	TargetPageID int64  `json:"target_page_id"`
	BodyHTML     string `json:"body_html"`
}

// noteBodyRe matches a footnote definition line: [^N]: body (single line).
var noteBodyRe = regexp.MustCompile(`(?m)^\[\^(\d+)\]:\s*(.+)$`)

// stripParagraph снимает с отрендеренного тела сноски единственную обёртку
// <p>…</p>. Обёртки документа здесь больше нет: markdown.Render отдаёт
// фрагмент (контракт pkg/markdown), и регулярка по <body>, стоявшая тут
// раньше, стала мёртвой.
func stripParagraph(html string) string {
	html = strings.TrimSpace(html)
	html = strings.TrimPrefix(html, "<p>")
	html = strings.TrimSuffix(html, "</p>")
	return strings.TrimSpace(html)
}

// buildNoteIndex scans each page's content_markdown for [^N]: bodies and returns
// one rendered entry per note number (first occurrence wins), sorted by number.
func buildNoteIndex(pages []*models.Page, r *markdown.Renderer) []NoteIndexEntry {
	seen := map[int]bool{}
	var out []NoteIndexEntry
	for _, p := range pages {
		for _, m := range noteBodyRe.FindAllStringSubmatch(p.ContentMarkdown, -1) {
			n, err := strconv.Atoi(m[1])
			if err != nil || seen[n] {
				continue
			}
			seen[n] = true
			out = append(out, NoteIndexEntry{
				Number:       n,
				TargetPage:   p.PageNumber,
				TargetPageID: p.ID,
				BodyHTML:     stripParagraph(r.Render(m[2])),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}
