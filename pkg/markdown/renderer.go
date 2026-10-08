// Package markdown рендерит разметку полос, глав и документов в HTML и
// собирает аппарат сносок.
//
// Контракт пакета: наружу не выходит ни одного целого документа — только
// XHTML-фрагменты. Обёртка html.CompletePage, которую ставят конструкторы
// рендерера, — внутреннее устройство: на неё опирается извлечение сносок
// (footnotesBlockRe), и снимается она на каждом публичном выходе помощником
// fragment. Публичных выходов пять — Render, CollectPages,
// CollectPagesScoped, RenderNotes, RenderNotesWith, — и все они перечислены
// поимённо в сторожевом тесте TestPublicOutputsCarryNoDocumentWrapper.
// Появился шестой — его место там же.
package markdown

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
)

var (
	noteXrefRe = regexp.MustCompile(`см\.\s+примечани[ея]\s+(\d+)`)
	// strips a previous wrap so the function is idempotent
	noteXrefUnwrapRe = regexp.MustCompile(
		`<a class="note-xref" data-note="\d+">(см\.\s+примечани[ея]\s+\d+)</a>`)
)

// linkNoteCrossRefs wraps «см. примечание N» references in a clickable anchor.
func linkNoteCrossRefs(htmlStr string) string {
	htmlStr = noteXrefUnwrapRe.ReplaceAllString(htmlStr, "$1")
	return noteXrefRe.ReplaceAllStringFunc(htmlStr, func(m string) string {
		n := noteXrefRe.FindStringSubmatch(m)[1]
		return fmt.Sprintf(`<a class="note-xref" data-note="%s">%s</a>`, n, m)
	})
}

// footnoteRefRe matches gomarkdown's in-text footnote reference so the ordinal
// can be replaced by the real marker. gomarkdown emits, e.g.:
//   <sup class="footnote-ref" id="fnref:r2"><a href="#fn:r2">2</a></sup>
var footnoteRefRe = regexp.MustCompile(
	`<sup class="footnote-ref" id="fnref:([^"]+)"><a href="(#fn:[^"]+)">[^<]*</a></sup>`)

// rewriteFootnoteMarkers replaces gomarkdown's per-document ordinal in each
// in-text footnote reference with the real marker (endnote number / subscript
// "(k)") and tags the <sup> with a kind class for styling.
func rewriteFootnoteMarkers(htmlStr string) string {
	return footnoteRefRe.ReplaceAllStringFunc(htmlStr, func(m string) string {
		sub := footnoteRefRe.FindStringSubmatch(m)
		name, href := sub[1], sub[2]
		text, kind := noteMarker(name)
		return fmt.Sprintf(
			`<sup class="footnote-ref footnote-ref--%s" id="fnref:%s"><a href="%s">%s</a></sup>`,
			kind, name, href, text)
	})
}

// Renderer handles markdown rendering with extensions
// Renderer holds the configuration needed to render markdown to HTML.
// It deliberately does NOT hold a *html.Renderer instance: gomarkdown's
// html.Renderer keeps a heading-id de-duplication counter
// (see vendor/github.com/gomarkdown/markdown/html/renderer.go) that lives
// for the renderer's whole lifetime and is never reset. A single *Renderer
// is built once per process and reused for every page/work/document render
// (see cmd/server/main.go), so sharing one html.Renderer across calls would
// let that counter climb forever — heading ids (and hence the whole
// document, byte for byte) would then drift between requests for identical
// content, breaking the ETag it is served under and any deep link to a
// heading. gomarkdown exposes no reset for the counter, so each render
// builds a fresh html.Renderer from rendererOpts instead (see render) —
// negligible cost next to parsing+rendering a page, see the report for the
// measurement.
type Renderer struct {
	extensions   parser.Extensions
	rendererOpts html.RendererOptions
}

// NewRenderer creates a new markdown renderer with all supported extensions
func NewRenderer() *Renderer {
	// Configure parser with all extensions from gomarkdown
	extensions := parser.CommonExtensions |
		parser.AutoHeadingIDs |
		parser.NoEmptyLineBeforeBlock |
		parser.Tables |
		parser.FencedCode |
		parser.Autolink |
		parser.Strikethrough |
		parser.DefinitionLists |
		parser.Footnotes |
		parser.HeadingIDs |
		parser.AutoHeadingIDs |
		parser.Titleblock |
		parser.OrderedListStart |
		parser.Attributes |
		parser.SuperSubscript |
		parser.MathJax

	// Configure HTML renderer
	htmlFlags := html.CommonFlags |
		html.HrefTargetBlank |
		html.FootnoteReturnLinks |
		html.CompletePage |
		html.UseXHTML

	opts := html.RendererOptions{
		Flags:                      htmlFlags,
		FootnoteReturnLinkContents: "↩",
	}

	return &Renderer{
		extensions:   extensions,
		rendererOpts: opts,
	}
}

// ForUntrustedAuthor отдаёт КОПИЮ рендерера для разметки, которую пишет
// ВОШЕДШИЙ ЧИТАТЕЛЬ, а не конвейер распознавания.
//
// Нужна ровно одному потребителю — тексту автора разбора
// (internal/api.assembleDocument). Готовый HTML уезжает в браузер модератора
// через dangerouslySetInnerHTML, политики безопасности содержимого (CSP) в
// дереве нет, а токен сотрудника лежит в хранилище браузера: всё, что тут
// проедет, исполнится под тем, кто ОБЯЗАН открыть предпросмотр, чтобы решить
// судьбу правки.
//
// Закрываются ЧЕТЫРЕ стока, и все четыре замерены на живом рендерере, а не
// выведены из чтения флагов:
//
//  1. html.SkipHTML — сырой HTML: блочный (`<script>…`, `<iframe>…`) целиком,
//     встроенный (`<img onerror=…>`) тегами, текст между ними остаётся.
//  2. Снятое parser.Attributes — блочные атрибуты. Это расширение вешает на
//     ЛЮБОЙ блок произвольные пары строкой `{ключ="значение"}` перед ним:
//     замерено на абзаце, заголовке, цитате, таблице и блоке кода, выходит
//     `<p onmouseover="…" style="…">`. Сток опаснее первого, потому что
//     БЕСКЛИКОВЫЙ: абзац со `style="position:fixed;inset:0"` растягивается на
//     весь экран, и модератору довольно провести мышью. Синтаксис этот в
//     корпусе не используется (у полос и вклеек расширение остаётся), а
//     читателю он не нужен вовсе.
//  3. html.Safelink — схемы ссылок. Без флага `[текст](javascript:alert(1))`
//     уезжает в href дословно; с ним небезопасная схема href не получает, а
//     http/https/mailto проходят как раньше.
//  4. html.SkipImages — картинки. Этот сток НЕ про исполнение: `javascript:`
//     в src картинки не исполняет ни один современный браузер. Он про
//     МАЯЧОК: `![](https://чужой/x.gif)` в теле разбора заставляет браузер
//     модератора сходить на чужой хост, и обязательный просмотр превращается
//     в отметку «такой-то сотрудник читал такой-то текст в такую-то минуту»,
//     выданную тому, кого рассматривают. Недоверенный вход — это «может
//     навредить», а не только «может исполнить». Возможности при этом не
//     теряется: загрузить картинку читателю нечем, маршрута нет и ветка его
//     не предполагала, — отнимается щель, а не работающая вещь. Флаг нужен
//     именно здесь: html.Safelink до src картинки не достаёт вовсе
//     (imageEnter его не спрашивает), а SkipHTML касается только сырых тегов.
//     Замерено: картинка выбрасывается целиком, в том числе внутри ссылки, а
//     окружающий текст, обычные ссылки и выделение остаются.
//
// Первая редакция этого метода звалась WithoutRawHTML и закрывала только
// первый сток — вектор, а не находку. Переименование намеренное: имя по
// СВОЙСТВУ ВХОДА («автор, которому не доверяем»), а не по одному приёму, —
// иначе пятый сток снова закроют мимо. Нашёл пятый — ему место здесь же,
// рядом с этими четырьмя, а не в примечании.
//
// Корпус сюда не заходит и заходить не должен: тома несут ручные HTML-таблицы
// (marker печатает так таблицы с rowspan), и их разметка законна. Поэтому это
// копия, а не правка общего рендерера, — вклейка, глава, полоса, скачивание и
// страница краулеру продолжают идти прежним путём байт в байт.
//
// Копия поверхностная и это безопасно: правятся только extensions и
// rendererOpts.Flags (оба — значения), а ссылочные поля html.RendererOptions
// (Head, Comments, RenderNodeHook) у конструкторов пакета не заполняются
// вовсе и здесь только переносятся.
func (r *Renderer) ForUntrustedAuthor() *Renderer {
	safe := *r
	safe.extensions &^= parser.Attributes
	safe.rendererOpts.Flags |= html.SkipHTML | html.Safelink | html.SkipImages
	return &safe
}

// newParser builds a parser with our extensions and the footnote/parenthesis
// workaround installed. Every parse of page or document markdown must go
// through it — see installFootnoteParenFix.
func newParser(extensions parser.Extensions) *parser.Parser {
	p := parser.NewWithExtensions(extensions)
	installFootnoteParenFix(p)
	return p
}

// footnoteRefEnd reports the index just past the "]" of a footnote reference
// "[^name]" starting at offset, but only when that reference is followed —
// after any run of whitespace, exactly as gomarkdown's link parser skips it —
// by a "(". Those are the references installFootnoteParenFix has to protect;
// every other "[" is left to the stock parser untouched.
func footnoteRefEnd(data []byte, offset int) (int, bool) {
	if offset+2 >= len(data) || data[offset] != '[' || data[offset+1] != '^' {
		return 0, false
	}
	i := offset + 2
	for i < len(data) && data[i] != ']' && data[i] != '[' && data[i] != '\n' {
		i++
	}
	if i >= len(data) || data[i] != ']' || i == offset+2 { // unterminated or empty name
		return 0, false
	}
	end := i + 1
	j := end
	for j < len(data) && parser.IsSpace(data[j]) {
		j++
	}
	if j >= len(data) || data[j] != '(' {
		return 0, false
	}
	return end, true
}

// installFootnoteParenFix works around a gomarkdown defect: its link parser
// skips whitespace after the closing "]" and, seeing a "(" next, parses the
// whole construct as an inline link — even when the brackets held a footnote
// reference. So «resp.[^s1] (поскольку …)» renders as
// <a href="поскольку …"></a>: the note loses its in-text marker (CollectPages
// then takes the definition for an orphan and dumps it at the end of the list)
// AND the parenthesised text disappears from the page. Scanned volumes are full
// of «термин[^N] (перевод)», so the shape is common, not exotic.
//
// The guard hands the stock parser a slice that stops at the "]", so the "("
// is out of its reach and it takes the deferred-footnote path it should have
// taken. If that yields nothing — no definition with this name, i.e. not a
// footnote after all — the original call is repeated on the full slice so a
// genuine link is still parsed as before.
func installFootnoteParenFix(p *parser.Parser) {
	var base parser.InlineParser
	base = p.RegisterInline('[', func(p *parser.Parser, data []byte, offset int) (int, ast.Node) {
		if end, ok := footnoteRefEnd(data, offset); ok {
			if consumed, node := base(p, data[:end], offset); consumed > 0 {
				return consumed, node
			}
		}
		return base(p, data, offset)
	})
}

// Render отдаёт XHTML-фрагмент разметки — содержимое <body> без обёртки
// документа. Контракт пакета: наружу целых документов не выходит.
func (r *Renderer) Render(markdownText string) string {
	return fragment(r.render(markdownText))
}

// RenderWithNotes — рендер одной полосы или её куска вместе со сносками: блок
// сносок собирается той же разметкой, что у главы (RenderNotesWith), а не
// остаётся <ol> gomarkdown. Нумерованный список нумерует браузер — подряд,
// по порядку ссылок, — и примечание тома 27 читалось «1», а подстрочное (1)
// — «2».
//
// Маркеры не перенумеровываются, в отличие от CollectPagesScoped: они
// берутся из имени сноски (noteMarker), то есть совпадают с печатной
// полосой. Вырезка понятия, начатая с середины полосы, показывает (3), а не
// (1), — ровно то, что читатель найдёт на странице тома.
//
// scope разводит якоря там, где в одном DOM стоят несколько кусков с одних и
// тех же полос: запись потока понятия зовёт с приставкой своего адреса.
// Приставка обязана проходить pagePrefixRe — буква, цифры, дефис.
//
// Определение без ссылки (сирота) отбрасывается, как и в Render: подбирать
// сирот умеет только CollectPagesScoped.
func (r *Renderer) RenderWithNotes(scope string, pageNumber int, markdownText string) string {
	mainHTML, items := r.renderPageNotes(scope, pageNumber, markdownText)

	var subscript, endnote []*Note
	for _, it := range items {
		n := &Note{AnchorID: it.AnchorID, RefID: it.RefID, Marker: it.Marker, Kind: it.Kind, BodyHTML: it.BodyHTML}
		if n.Kind == "subscript" {
			subscript = append(subscript, n)
		} else {
			endnote = append(endnote, n)
		}
	}
	sortEndnotes(endnote)

	var set NoteSet
	for _, n := range subscript {
		set.Subscript = append(set.Subscript, *n)
	}
	for _, n := range endnote {
		set.Endnote = append(set.Endnote, *n)
	}
	return fragment(mainHTML) + RenderNotesWith(set, NotesOptions{Inline: true})
}

// render — тот же рендер, но целым документом: с DOCTYPE, <head> и <body>.
//
// Приватный намеренно. Обёртка нужна извлечению сносок: footnotesBlockRe
// (ниже) жадным .* опирается на то, что последний </div> в тексте —
// собственный закрывающий div блока сносок, а это верно только в
// CompletePage-рендере. Единственный зовущий — renderPageNotes.
func (r *Renderer) render(markdownText string) string {
	p := newParser(r.extensions)
	doc := p.Parse([]byte(hoistHTMLBlockNoteRefs(markdownText)))
	// A fresh html.Renderer per call, not a shared one — see the Renderer
	// doc comment: gomarkdown's heading-id de-dup counter never resets.
	hr := html.NewRenderer(r.rendererOpts)
	output := stripNoteRefCarriers(string(markdown.Render(doc, hr)))
	return rewriteFootnoteMarkers(linkNoteCrossRefs(output))
}

// footnoteItem is one extracted footnote definition with its anchor preserved.
type footnoteItem struct {
	AnchorID string // e.g. "fn:3-r2" — jump target
	RefID    string // e.g. "fnref:3-r2" — where the ↩ back-link returns
	Marker   string // "5" | "(2)"
	Kind     string // "endnote" | "subscript"
	BodyHTML string // <li> inner HTML without the trailing return link
}

var (
	// Greedy .* so the block ends at the footnotes div's own closing </div>
	// (the last </div> in a per-page CompletePage render) rather than a </div>
	// that may appear inside a footnote body.
	footnotesBlockRe = regexp.MustCompile(`(?s)<div class="footnotes">.*</div>`)
	// Start of one footnote definition. Only definition <li> carry id="fn:";
	// nested list <li> in a body do not, so these are safe split boundaries.
	footnoteItemStartRe = regexp.MustCompile(`<li id="(fn:[^"]+)">`)
	footnoteReturnRe    = regexp.MustCompile(
		`\s*<a class="footnote-return" href="#(fnref:[^"]+)">.*?</a>\s*$`)
)

// renderPageNotes renders one page's markdown, namespacing footnote ids
// with the область (scope, для главы — пустая) плюс номер полосы, чтобы
// якоря не сталкивались ни внутри главы, ни между вклейками разных томов в
// разборе, и возвращает основной HTML (блок сносок вырезан) плюс извлечённые
// элементы. Приватная нарочно: маркеры footnoteItem, которые она отдаёт,
// локальны для полосы, поэтому вызывающие мимо CollectPages/CollectPagesScoped
// увидели бы неверную нумерацию.
func (r *Renderer) renderPageNotes(scope string, pageNumber int, markdownText string) (mainHTML string, items []footnoteItem) {
	prefixed := prefixFootnoteNames(markdownText, notePrefix(scope, pageNumber))
	fullHTML := r.render(prefixed)

	block := footnotesBlockRe.FindString(fullHTML)
	mainHTML = footnotesBlockRe.ReplaceAllString(fullHTML, "")

	if block == "" {
		return mainHTML, nil
	}

	// Split the block into footnote items on their <li id="fn:NAME"> starts.
	// Each item body runs to the next such start (or the block end); the item's
	// own closing </li> is the last </li> before that boundary, so nested list
	// items inside the body stay balanced and intact.
	starts := footnoteItemStartRe.FindAllStringSubmatchIndex(block, -1)
	for i, loc := range starts {
		anchorID := block[loc[2]:loc[3]] // "fn:3-r2"
		bodyStart := loc[1]              // just past the "<li id=...>"
		bodyEnd := len(block)
		if i+1 < len(starts) {
			bodyEnd = starts[i+1][0] // start of the next item's <li
		}
		body := block[bodyStart:bodyEnd]
		if idx := strings.LastIndex(body, "</li>"); idx >= 0 {
			body = body[:idx] // drop this item's own closing tag + trailing markup
		}

		name := strings.TrimPrefix(anchorID, "fn:")
		refID := "fnref:" + name
		if rm := footnoteReturnRe.FindStringSubmatch(body); rm != nil {
			refID = rm[1]
			body = footnoteReturnRe.ReplaceAllString(body, "")
		}

		marker, kind := noteMarker(name)
		items = append(items, footnoteItem{
			AnchorID: anchorID,
			RefID:    refID,
			Marker:   marker,
			Kind:     kind,
			BodyHTML: strings.TrimSpace(body),
		})
	}
	return mainHTML, items
}

// PageContent represents a page's content for rendering
type PageContent struct {
	PageNumber int
	Content    string
}

// prefixFootnoteNames разводит имена сносок по области: к каждому имени
// приписывается префикс, чтобы якоря не столкнулись между полосами (внутри
// главы — номер полосы) и между вклейками разных томов (номер вклейки плюс
// номер полосы).
func prefixFootnoteNames(markdown string, prefix string) string {
	footnoteRefRegex := regexp.MustCompile(`\[\^([^\]]+)\]`)
	return footnoteRefRegex.ReplaceAllStringFunc(markdown, func(match string) string {
		matches := footnoteRefRegex.FindStringSubmatch(match)
		if len(matches) != 2 {
			return match
		}
		return fmt.Sprintf("[^%s%s]", prefix, matches[1])
	})
}

// notePrefix — префикс имён сносок одной полосы внутри области. У главы
// (пустая scope) даёт прежнюю форму "12-"; у вклейки разбора — "v2-12-".
func notePrefix(scope string, pageNumber int) string {
	return fmt.Sprintf("%s%d-", scope, pageNumber)
}

// pagePrefixRe снимает префикс области с имени сноски: "12-" у главы,
// "v2-12-" у вклейки разбора, "a-0-" у текста автора разбора.
var pagePrefixRe = regexp.MustCompile(`^(?:[a-z]\d*-)?\d+-`)

// subscriptNameRe matches a subscript (page-local author/editorial) note
// name: "r"+digits (a page-local translator/editor note) or "s"+digits (a
// pass3 editorial asterisk note — resolve_footnotes_v2.py emits these for
// printed "*"/"**"/etc. markers, and they are page-local subscript notes
// too, just sourced from a different pass).
var subscriptNameRe = regexp.MustCompile(`^[rs](\d+)$`)

// noteMarker maps a footnote name to its display marker and kind.
// Endnote names are digits ("5" -> "5"); subscript names are "r"+digits or
// "s"+digits ("r2"/"s2" -> "(2)"). An optional leading "{page}-" prefix is
// stripped first.
func noteMarker(name string) (text string, kind string) {
	base := pagePrefixRe.ReplaceAllString(name, "")
	if m := subscriptNameRe.FindStringSubmatch(base); m != nil {
		return "(" + m[1] + ")", "subscript"
	}
	return base, "endnote"
}

