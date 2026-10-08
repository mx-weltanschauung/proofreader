package api

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"proofreader/pkg/markdown"
)

// pageBlock — один блок полосы: байтовые границы в content_markdown и
// готовый HTML.
//
// Обе стороны произведены ОДНИМ разрезом, поэтому сопоставлять их не надо:
// подборщик показывает html, а вклеивает start/end — карты «отрисованный
// текст → байты markdown» в проекте нет и строить её незачем.
type pageBlock struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Kind  string `json:"kind"`
	HTML  string `json:"html"`
}

var (
	blockHeadingRe    = regexp.MustCompile(`^#{1,6}\s`)
	blockListPrefixRe = regexp.MustCompile(`^([-*+]|\d+[.)])\s+`)
	blockWordCharRe   = regexp.MustCompile(`\p{L}|\p{N}`)
	blockFootnoteRe   = regexp.MustCompile(`^\[\^[^\]\s]+\]:`)
)

// looksLikeList — маркер плюс пробел ещё не пункт списка: после них обязано
// идти настоящее содержимое, а не ещё один такой же символ. Простого \S
// после пробела недостаточно — разделитель сцены "*      *" (живой пример:
// том 6 Ленина, стр. 52 и другие) сам состоит из непробельных знаков и прошёл
// бы такую проверку; требуем букву или цифру где-то после маркера.
func looksLikeList(first string) bool {
	loc := blockListPrefixRe.FindStringIndex(first)
	if loc == nil {
		return false
	}
	return blockWordCharRe.MatchString(first[loc[1]:])
}

// blockKind — вид блока по его ПЕРВОЙ строке. Нужен подборщику: сноски он не
// предлагает (их дотягивает withFootnoteDefs сам), заголовок и стих
// показывает иначе, чем абзац.
func blockKind(first string) string {
	switch {
	case blockFootnoteRe.MatchString(first):
		return "footnote"
	case blockHeadingRe.MatchString(first):
		return "heading"
	case strings.HasPrefix(first, ">"):
		return "quote"
	case looksLikeList(first):
		return "list"
	}
	return "paragraph"
}

// lineAt отдаёт строку, начинающуюся с pos (без завершающего \n), и позицию
// начала следующей строки.
func lineAt(s string, pos int) (line string, next int) {
	nl := strings.IndexByte(s[pos:], '\n')
	if nl < 0 {
		return s[pos:], len(s)
	}
	return s[pos : pos+nl], pos + nl + 1
}

// splitPageBlocks режет полосу на блоки по ПУСТЫМ строкам, не по переводам:
// абзац в markdown задаёт именно пустая строка, и разрез по переводу отдал бы
// автору одну строку стиха вместо строфы (стих в корпусе размечен blockquote
// с обратной косой в конце строки).
//
// Границы — байтовые смещения в исходной строке, пригодные для sliceCut как
// есть. Блоки НЕ покрывают полосу целиком: пустые строки между ними в границы
// не входят.
func splitPageBlocks(md string) []pageBlock {
	var out []pageBlock
	pos := 0
	for pos < len(md) {
		// Пропустить пустые строки (в т.ч. состоящие из пробелов/табов).
		for pos < len(md) {
			line, next := lineAt(md, pos)
			if strings.TrimSpace(line) != "" {
				break
			}
			pos = next
		}
		if pos >= len(md) {
			break
		}
		start, end, first := pos, pos, ""
		for pos < len(md) {
			line, next := lineAt(md, pos)
			if strings.TrimSpace(line) == "" {
				break
			}
			trimmed := strings.TrimSuffix(line, "\r")
			if first == "" {
				first = strings.TrimSpace(trimmed)
			}
			end = pos + len(trimmed)
			pos = next
		}
		out = append(out, pageBlock{Start: start, End: end, Kind: blockKind(first)})
	}
	return out
}

// Blocks отдаёт полосу, разрезанную на блоки, с готовым HTML у каждого —
// поверхность выбора для подборщика вклейки. Публичен, как и остальное чтение
// полосы: тот же текст уже отдаётся целиком по /pages/{pageId}.
//
// Блоки-определения сносок (Kind == "footnote") в ответ не попадают: их HTML
// всегда получался бы пустым — CollectPagesScoped вынимает тело сноски из
// рендера, а для блока, который И ЕСТЬ это тело, отдавать больше нечего.
// Контракт «у каждого блока — границы и готовый HTML» иначе не выполнялся бы
// на восьми процентах корпуса (доля вида footnote в сверке). Автор тело
// сноски всё равно получает: withFootnoteDefs дотягивает его к блоку со
// ссылкой при рендере — отдельным блоком для вклейки оно не нужно.
// blockKind по-прежнему различает footnote — разрезке нужно знать, что
// пропустить.
func (h *PageHandler) Blocks(w http.ResponseWriter, r *http.Request) {
	page, ok := pageOfWork(w, r, h.pageRepo)
	if !ok {
		return
	}

	all := splitPageBlocks(page.ContentMarkdown)
	blocks := make([]pageBlock, 0, len(all))
	for _, b := range all {
		if b.Kind != "footnote" {
			blocks = append(blocks, b)
		}
	}
	for i := range blocks {
		raw := page.ContentMarkdown[blocks[i].Start:blocks[i].End]
		// Дотягивание тел сносок обязательно ДО рендера: голая ссылка [^1] в
		// этом рендерере не молчит, а съедает текст в скобках следом за ней.
		// Сами тела при этом в HTML блока не попадают — CollectPagesScoped
		// вынимает блок сносок из основного HTML, и мы его отбрасываем:
		// подборщику аппарат не показывают, его дотянет уже сама вклейка.
		html, _ := h.renderer.CollectPagesScoped(
			fmt.Sprintf("blk%d-", i),
			[]markdown.PageContent{{
				PageNumber: page.PageNumber,
				Content:    withFootnoteDefs(raw, page.ContentMarkdown),
			}})
		blocks[i].HTML = html[0]
	}

	writeJSONStatus(w, http.StatusOK, map[string]any{"blocks": blocks})
}
