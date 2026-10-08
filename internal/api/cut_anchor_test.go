package api

import (
	"strings"
	"testing"

	"proofreader/internal/models"
)

// Якорный слой обязан работать над общей структурой, а не над вырезкой
// понятия: вторая машина (вклейка разбора) переякоривается тем же кодом.
func TestReanchorMovesBothEndsOnOnePage(t *testing.T) {
	const after = "ВСТАВКА В НАЧАЛО. начало. Ленин писал так. конец."

	a := models.Anchor{
		StartPageID: 7,
		StartOffset: len("начало. "),
		EndPageID:   7,
		EndOffset:   len("начало. Ленин писал так."),
		HeadQuote:   "Ленин",
		TailQuote:   "так.",
	}

	start, end, ok := reanchor(a, 7, after)
	if !ok {
		t.Fatal("якорь не нашёлся в изменённом тексте")
	}
	if got := after[start:end]; got != "Ленин писал так." {
		t.Fatalf("переякорилось на %q, ожидалось %q", got, "Ленин писал так.")
	}
}

// Пропавшая цитата — не паника и не «нашлось по старым смещениям», а честное
// «не нашлось»: владелец переведёт запись в своё состояние отвязанности.
func TestReanchorReportsMissWhenQuoteGone(t *testing.T) {
	a := models.Anchor{
		StartPageID: 7, StartOffset: 0,
		EndPageID: 7, EndOffset: 5,
		HeadQuote: "Ленин", TailQuote: "Ленин",
	}
	if _, _, ok := reanchor(a, 7, "здесь этих слов нет"); ok {
		t.Fatal("промах якоря объявлен успехом")
	}
}

// Вырезка отдаёт свой якорь общей структурой — иначе общий слой пришлось бы
// учить обеим таблицам.
func TestIndexFragmentExposesAnchor(t *testing.T) {
	f := &models.IndexFragment{
		StartPageID: 1, StartOffset: 2, EndPageID: 3, EndOffset: 4,
		HeadQuote: "г", TailQuote: "х", StartHash: "aa", EndHash: "bb",
	}
	a := f.Anchor()
	if a.StartPageID != 1 || a.StartOffset != 2 || a.EndPageID != 3 || a.EndOffset != 4 ||
		a.HeadQuote != "г" || a.TailQuote != "х" || a.StartHash != "aa" || a.EndHash != "bb" {
		t.Fatalf("якорь вырезки собран неверно: %+v", a)
	}
}

// TestReanchorSamePageTailSearchStartsAfterHead — хвостовая цитата случайно
// совпадает с текстом ДО головы (в филлере). Прежнее смещение конца стоит
// близко к этому ложному вхождению: если поиск хвоста не обрезать от уже
// найденного начала, а искать по всей странице, ложное вхождение перетянет
// near на себя и переякоривание не найдёт настоящий кусок вовсе (конец
// окажется левее начала — сработает финальная подстраховка).
//
// Тест TestReanchorMovesBothEndsOnOnePage мутацию в этой строке не ловит:
// в его тексте хвостовая цитата встречается только один раз, и порядок
// поиска на единственное вхождение не влияет.
func TestReanchorSamePageTailSearchStartsAfterHead(t *testing.T) {
	const text = "так. филлер текст побольше голова хвост так. конец"

	a := models.Anchor{
		StartPageID: 7, StartOffset: strings.Index(text, "голова"),
		EndPageID: 7, EndOffset: 10,
		HeadQuote: "голова", TailQuote: "так.",
	}

	start, end, ok := reanchor(a, 7, text)
	if !ok {
		t.Fatal("переякоривание не удалось, хотя обе цитаты есть в тексте")
	}
	if got := text[start:end]; got != "голова хвост так." {
		t.Fatalf("переякорилось на %q, ожидалось %q", got, "голова хвост так.")
	}
}
