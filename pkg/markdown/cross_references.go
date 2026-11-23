package markdown

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	nethtml "golang.org/x/net/html"
)

const citationSpace = `[\s\x{00a0}\x{202f}]`

var (
	citationAddress      = regexp.MustCompile(`(?i)(?:см\.` + citationSpace + `+)?(?:т\.` + citationSpace + `*|том` + citationSpace + `+)([0-9]{1,9}),` + citationSpace + `*(?:с\.|стр\.)` + citationSpace + `*([0-9]{1,9})`)
	citationContinuation = regexp.MustCompile(`(?i)^(?:[0-9\p{L}]|` + citationSpace + `*[-–—−,]` + citationSpace + `*[0-9]|` + citationSpace + `+и` + citationSpace + `+(?:[0-9]|др\.))`)
	editionNumberPattern = `(?:^|[^\p{L}\p{N}])([0-9]{1,9})(?:[-‐‑–]?е)?` + citationSpace + `+изд(?:ание|ания|\.)`
	citationEdition      = regexp.MustCompile(`(?i)` + editionNumberPattern + citationSpace + `*[,.:]?` + citationSpace + `*$`)
)

// BibliographicReference — запись с одним томом и одной печатной страницей
// в видимом тексте. Ordinal считается отдельно для тела полосы и каждой сноски.
type BibliographicReference struct {
	Ordinal       int
	Quote         string
	Context       string
	Volume        int
	PrintedPage   int
	EditionNumber int
}

type citationTextPiece struct {
	startInRun  int
	endInRun    int
	startInHTML int
}

// LinkBibliographicReferences обходит видимый текст XHTML-фрагмента.
// Инспектор возвращает id подтверждённой ссылки или 0. Теги, существующие
// ссылки, номера сносок и код сохраняются побайтно.
func LinkBibliographicReferences(fragment string, inspect func(BibliographicReference) int64) string {
	z := nethtml.NewTokenizer(strings.NewReader(fragment))
	var out, run strings.Builder
	var excluded []string
	var pieces []citationTextPiece
	ordinal, emitted, offset := 0, 0, 0

	flush := func() {
		text := run.String()
		next := 0
		for _, match := range citationAddress.FindAllStringSubmatchIndex(text, -1) {
			start, end := match[0], match[1]
			for pieces[next].endInRun <= start {
				next++
			}
			piece := pieces[next]
			// Inline-форматирование может нести название издания или хвост
			// диапазона. Сам адрес оборачиваем только внутри одного узла.
			if end > piece.endInRun || !citationBoundary(text[:start], text[end:]) {
				continue
			}
			volume, _ := strconv.Atoi(text[match[2]:match[3]])
			printedPage, _ := strconv.Atoi(text[match[4]:match[5]])
			if volume < 1 || printedPage < 1 {
				continue
			}
			edition := 0
			if m := citationEdition.FindStringSubmatch(html.UnescapeString(text[:start])); m != nil {
				edition, _ = strconv.Atoi(m[1])
			}
			ordinal++
			id := inspect(BibliographicReference{
				Ordinal: ordinal, Quote: html.UnescapeString(text[start:end]), Context: citationContext(text, start, end),
				Volume: volume, PrintedPage: printedPage, EditionNumber: edition,
			})
			if id > 0 {
				startHTML := piece.startInHTML + start - piece.startInRun
				out.WriteString(fragment[emitted:startHTML])
				fmt.Fprintf(&out, `<a class="bibliographic-reference" href="/api/cross-references/%d/open">%s</a>`, id, text[start:end])
				emitted = startHTML + end - start
			}
		}
		pieces = pieces[:0]
		run.Reset()
	}

	for token := z.Next(); token != nethtml.ErrorToken; token = z.Next() {
		raw := z.Raw()
		switch token {
		case nethtml.StartTagToken, nethtml.EndTagToken, nethtml.SelfClosingTagToken:
			name, _ := z.TagName()
			tag := string(name)
			switch tag {
			case "a", "code", "pre", "sup", "style", "script", "textarea", "template", "svg", "math":
				flush()
				if token == nethtml.StartTagToken {
					excluded = append(excluded, tag)
				} else if token == nethtml.EndTagToken && len(excluded) > 0 && excluded[len(excluded)-1] == tag {
					excluded = excluded[:len(excluded)-1]
				}
			case "p", "br", "hr", "div", "section", "aside", "article", "ul", "ol", "li", "dl", "dt", "dd",
				"table", "tr", "td", "th", "blockquote", "figure", "figcaption", "h1", "h2", "h3", "h4", "h5", "h6":
				flush()
			}
		case nethtml.TextToken:
			if len(excluded) == 0 {
				start := run.Len()
				run.Write(raw)
				pieces = append(pieces, citationTextPiece{startInRun: start, endInRun: run.Len(), startInHTML: offset})
			}
		}
		offset += len(raw)
	}
	flush()
	if emitted == 0 {
		return fragment
	}
	out.WriteString(fragment[emitted:])
	return out.String()
}

func citationBoundary(before, after string) bool {
	r, _ := utf8.DecodeLastRuneInString(html.UnescapeString(before))
	return !unicode.IsLetter(r) && !unicode.IsNumber(r) &&
		!citationContinuation.MatchString(html.UnescapeString(after))
}

func citationContext(raw string, start, end int) string {
	before := []rune(html.UnescapeString(raw[:start]))
	after := []rune(html.UnescapeString(raw[end:]))
	const radius = 120
	if len(before) > radius {
		before = append([]rune{'…'}, before[len(before)-radius:]...)
	}
	if len(after) > radius {
		after = append(after[:radius], '…')
	}
	return strings.Join(strings.Fields(string(before)+html.UnescapeString(raw[start:end])+string(after)), " ")
}
