package api

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"

	"proofreader/internal/models"
)

// anchorQuoteLen — потолок цитаты-якоря в байтах. Ста двадцати хватает,
// чтобы фраза была уникальной на странице, и мало, чтобы правка где-то
// рядом её не задела.
const anchorQuoteLen = 120

// pageHash returns the sha256 of a page body, hex-encoded.
func pageHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// quoteOf takes the anchor quote from one end of a cut: the head when
// fromHead, the tail otherwise.
//
// Обрезка идёт по границе руны: цитата, разрубленная посреди буквы, потом
// никогда не найдётся в тексте.
func quoteOf(text string, start, end int, fromHead bool) string {
	cut := sliceCut(text, start, end)
	if len(cut) <= anchorQuoteLen {
		return cut
	}
	if fromHead {
		limit := anchorQuoteLen
		for limit > 0 && !utf8.RuneStart(cut[limit]) {
			limit--
		}
		return cut[:limit]
	}
	from := len(cut) - anchorQuoteLen
	for from < len(cut) && !utf8.RuneStart(cut[from]) {
		from++
	}
	return cut[from:]
}

// findQuote returns the occurrence of quote nearest to near.
//
// Повторяющаяся фраза на странице — не редкость («и наоборот»), и выбор
// ближайшего вхождения не даёт вырезке перепрыгнуть на чужое.
func findQuote(text, quote string, near int) (int, bool) {
	if quote == "" {
		return 0, false
	}

	best, found := -1, false
	for from := 0; ; {
		idx := strings.Index(text[from:], quote)
		if idx < 0 {
			break
		}
		at := from + idx
		if !found || abs(at-near) < abs(best-near) {
			best, found = at, true
		}
		from = at + 1
		if from >= len(text) {
			break
		}
	}
	return best, found
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// reanchor пересчитывает границы куска против нового текста полосы.
//
// Правится одна полоса, а кусок может держаться за две: трогаем ту сторону,
// что стоит на этой полосе, и оставляем вторую как есть.
//
// Возвращает РЕШЕНИЕ, а не состояние: статус ставит владелец своим словарём
// (machine|confirmed|stale у вырезки, ok|stale у вклейки). Общий словарь
// протёк бы «confirmed» во вклейки, где подтверждать нечего.
func reanchor(a models.Anchor, pageID int64, text string) (int, int, bool) {
	start, end := a.StartOffset, a.EndOffset

	if a.StartPageID == pageID {
		at, ok := findQuote(text, a.HeadQuote, a.StartOffset)
		if !ok {
			return 0, 0, false
		}
		start = at
	}

	if a.EndPageID == pageID {
		// Хвост ищется от начала куска вперёд, когда обе стороны на одной
		// полосе: иначе конец мог бы уехать выше начала.
		from := 0
		if a.StartPageID == pageID {
			from = start
		}
		at, ok := findQuote(text[from:], a.TailQuote, a.EndOffset-from-len(a.TailQuote))
		if !ok {
			return 0, 0, false
		}
		end = from + at + len(a.TailQuote)
	}

	// Подстраховка, а не то, что держит инвариант: хвост ищется от уже
	// найденного начала, а пустую цитату findQuote отвергает заранее.
	if a.StartPageID == pageID && a.EndPageID == pageID && end <= start {
		return 0, 0, false
	}
	return start, end, true
}
