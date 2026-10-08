package api

import (
	"regexp"
	"sort"
	"strings"

	"proofreader/internal/models"
)

// cutRange is one page's share of a cut: the page and the byte offsets to
// take from its content_markdown.
type cutRange struct {
	PageID     int64
	PageNumber int
	Start      int
	End        int
}

// cutRanges splits a cut into per-page pieces.
//
// Вырезка непрерывна: от (start_page, start_offset) до (end_page, end_offset),
// промежуточные страницы целиком. Страницы приходят отсортированными по
// номеру. Страница, которой не оказалось в наборе, молча пропускается — она
// могла исчезнуть между выборкой вырезок и выборкой тел; это касается только
// самой отсутствующей страницы, а не всех, что лежат между ней и другим
// концом вырезки — те всё равно внутри вырезки и должны быть отданы целиком.
func cutRanges(f *models.IndexFragment, pages []*models.Page) []cutRange {
	startIdx, endIdx := -1, -1
	for i, p := range pages {
		if p.ID == f.StartPageID {
			startIdx = i
		}
		if p.ID == f.EndPageID {
			endIdx = i
		}
	}

	if startIdx == -1 && endIdx == -1 {
		// Ни начало, ни конец вырезки не найдены в наборе — про промежуток
		// сказать нечего.
		return nil
	}
	if startIdx == -1 {
		// Страница начала пропала, но набор отсортирован по номеру: первая
		// оставшаяся страница уже внутри вырезки (конец найдётся позже неё).
		startIdx = 0
	}
	if endIdx == -1 {
		// Симметрично: страница конца пропала, вырезка доходит до последней
		// оставшейся страницы набора.
		endIdx = len(pages) - 1
	}

	var out []cutRange
	for i := startIdx; i <= endIdx; i++ {
		p := pages[i]
		start, end := 0, len(p.ContentMarkdown)
		if p.ID == f.StartPageID {
			start = f.StartOffset
		}
		if p.ID == f.EndPageID {
			end = f.EndOffset
		}
		out = append(out, cutRange{p.ID, p.PageNumber, start, end})
	}
	return out
}

// sliceCut takes text[start:end], clamping both ends.
//
// Смещения переживают правку страницы не всегда (вырезка могла уехать в
// stale и всё же попасть сюда), поэтому срез обязан не паниковать.
func sliceCut(text string, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end > len(text) {
		end = len(text)
	}
	if start >= end {
		return ""
	}
	return text[start:end]
}

var (
	// Ссылка на сноску в тексте: [^r1], [^12]. Двоеточие после скобки —
	// необязательное, поэтому этот же шаблон берёт имя и из заголовка
	// определения ([^r1]:), а не только из чистой ссылки: footnoteDefRe
	// требует двоеточие сразу в начале строки, а вырезка может начинаться
	// не с начала строки. Совпавшие так имена не задваиваются — те, что уже
	// определены в срезе, отсеивает inSlice ниже.
	footnoteRefRe = regexp.MustCompile(`\[\^([^\]\s]+)\](?::)?`)
	footnoteDefRe = regexp.MustCompile(`(?m)^\[\^([^\]\s]+)\]:`)
)

// withFootnoteDefs appends the definitions of the footnotes a cut references
// but does not contain.
//
// Тело сноски стоит внизу страницы, вырезка берётся из середины: без
// дотягивания ссылка [^r1] повисает. Определения, уже попавшие в срез,
// не дублируются.
func withFootnoteDefs(slice, page string) string {
	defined := map[string]string{}
	locs := footnoteDefRe.FindAllStringSubmatchIndex(page, -1)
	for i, loc := range locs {
		name := page[loc[2]:loc[3]]
		end := len(page)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		defined[name] = strings.TrimRight(page[loc[0]:end], "\n")
	}

	inSlice := map[string]bool{}
	for _, m := range footnoteDefRe.FindAllStringSubmatch(slice, -1) {
		inSlice[m[1]] = true
	}

	var missing []string
	seen := map[string]bool{}
	for _, m := range footnoteRefRe.FindAllStringSubmatch(slice, -1) {
		name := m[1]
		if inSlice[name] || seen[name] {
			continue
		}
		body, ok := defined[name]
		if !ok {
			// Ссылка без определения — битая разметка страницы, а не повод
			// ломать выдачу вырезки.
			continue
		}
		seen[name] = true
		missing = append(missing, body)
	}

	if len(missing) == 0 {
		return slice
	}
	return slice + "\n\n" + strings.Join(missing, "\n\n")
}

// pageChunk is a piece of a page: text plus whether it lies inside a cut.
type pageChunk struct {
	Text   string
	Inside bool
}

// pageChunks splits a page into alternating outside/inside pieces along the
// given spans.
//
// Страница приходит клиенту уже разрезанной: подсветка получается точной без
// сопоставления смещений с DOM. Перекрывшиеся вырезки сливаются — читателю
// это одна подсвеченная область, и граница между ними резала бы текст на
// пустом месте.
func pageChunks(text string, spans [][2]int) []pageChunk {
	normalized := make([][2]int, 0, len(spans))
	for _, s := range spans {
		start, end := s[0], s[1]
		if start < 0 {
			start = 0
		}
		if end > len(text) {
			end = len(text)
		}
		if start >= end {
			continue
		}
		normalized = append(normalized, [2]int{start, end})
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i][0] < normalized[j][0] })

	merged := make([][2]int, 0, len(normalized))
	for _, s := range normalized {
		if n := len(merged); n > 0 && s[0] <= merged[n-1][1] {
			if s[1] > merged[n-1][1] {
				merged[n-1][1] = s[1]
			}
			continue
		}
		merged = append(merged, s)
	}

	var out []pageChunk
	at := 0
	for _, s := range merged {
		if s[0] > at {
			out = append(out, pageChunk{Text: text[at:s[0]]})
		}
		out = append(out, pageChunk{Text: text[s[0]:s[1]], Inside: true})
		at = s[1]
	}
	if at < len(text) {
		out = append(out, pageChunk{Text: text[at:]})
	}
	if len(out) == 0 {
		out = append(out, pageChunk{Text: text})
	}
	return out
}
