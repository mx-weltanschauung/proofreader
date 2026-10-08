package api

import (
	"strings"
	"testing"
	"unicode/utf8"

	"proofreader/internal/models"
)

func page(id int64, number int, text string) *models.Page {
	return &models.Page{ID: id, PageNumber: number, ContentMarkdown: text}
}

func TestSliceCutClampsBounds(t *testing.T) {
	text := "абвгд"
	if got := sliceCut(text, 0, len(text)); got != text {
		t.Fatalf("полный срез дал %q", got)
	}
	// Смещения переживают правку страницы не всегда; срез обязан не паниковать.
	if got := sliceCut(text, 2, 9999); got != text[2:] {
		t.Fatalf("конец за пределами дал %q", got)
	}
	if got := sliceCut(text, -5, 4); got != text[:4] {
		t.Fatalf("отрицательное начало дало %q", got)
	}
	if got := sliceCut(text, 8, 2); got != "" {
		t.Fatalf("перевёрнутый срез дал %q", got)
	}
}

func TestCutRangesWithinOnePage(t *testing.T) {
	f := &models.IndexFragment{
		StartPageID: 9143, StartOffset: 10,
		EndPageID: 9143, EndOffset: 40,
	}
	got := cutRanges(f, []*models.Page{page(9143, 730, strings.Repeat("x", 100))})

	if len(got) != 1 {
		t.Fatalf("частей %d, ожидалась 1", len(got))
	}
	if got[0].Start != 10 || got[0].End != 40 || got[0].PageNumber != 730 {
		t.Fatalf("часть: %+v", got[0])
	}
}

func TestCutRangesAcrossPages(t *testing.T) {
	// Мысль переходит с 730-й на 731-ю: у первой берётся хвост, у последней —
	// голова, промежуточные целиком.
	f := &models.IndexFragment{
		StartPageID: 9143, StartOffset: 60,
		EndPageID: 9145, EndOffset: 25,
	}
	pages := []*models.Page{
		page(9143, 730, strings.Repeat("a", 100)),
		page(9144, 731, strings.Repeat("b", 100)),
		page(9145, 732, strings.Repeat("c", 100)),
	}

	got := cutRanges(f, pages)

	if len(got) != 3 {
		t.Fatalf("частей %d, ожидалось 3", len(got))
	}
	if got[0].Start != 60 || got[0].End != 100 {
		t.Fatalf("первая часть: %+v", got[0])
	}
	if got[1].Start != 0 || got[1].End != 100 {
		t.Fatalf("средняя часть: %+v", got[1])
	}
	if got[2].Start != 0 || got[2].End != 25 {
		t.Fatalf("последняя часть: %+v", got[2])
	}
}

func TestCutRangesSkipsMissingPages(t *testing.T) {
	// Страница исчезла между выборкой вырезок и выборкой тел.
	f := &models.IndexFragment{StartPageID: 9143, StartOffset: 0, EndPageID: 9145, EndOffset: 10}
	got := cutRanges(f, []*models.Page{page(9145, 732, strings.Repeat("c", 100))})

	if len(got) != 1 || got[0].PageNumber != 732 || got[0].End != 10 {
		t.Fatalf("части: %+v", got)
	}
}

func TestCutRangesKeepsMiddlePageWhenStartPageMissing(t *testing.T) {
	// Страница начала (100) пропала из набора, но 101-я лежит между началом
	// и концом вырезки целиком и не должна пропасть вместе с ней.
	f := &models.IndexFragment{
		StartPageID: 100, StartOffset: 5,
		EndPageID: 102, EndOffset: 20,
	}
	pages := []*models.Page{
		page(101, 6, strings.Repeat("m", 50)),
		page(102, 7, strings.Repeat("n", 50)),
	}

	got := cutRanges(f, pages)

	if len(got) != 2 {
		t.Fatalf("частей %d, ожидалось 2: %+v", len(got), got)
	}
	if got[0].PageID != 101 || got[0].Start != 0 || got[0].End != 50 {
		t.Fatalf("промежуточная страница не отдана целиком: %+v", got[0])
	}
	if got[1].PageID != 102 || got[1].Start != 0 || got[1].End != 20 {
		t.Fatalf("последняя часть: %+v", got[1])
	}
}

func TestWithFootnoteDefsPullsMissingBodies(t *testing.T) {
	pageText := "Начало страницы.\n\nВ тексте есть сноска[^r1] и ещё одна[^2].\n\n" +
		"[^r1]: * Примечание редакции *Ред.*\n[^2]: Второе примечание."
	slice := "В тексте есть сноска[^r1] и ещё одна[^2]."

	got := withFootnoteDefs(slice, pageText)

	if !strings.Contains(got, "[^r1]: * Примечание редакции") {
		t.Fatalf("тело [^r1] не дотянуто: %q", got)
	}
	if !strings.Contains(got, "[^2]: Второе примечание.") {
		t.Fatalf("тело [^2] не дотянуто: %q", got)
	}
	if !strings.HasPrefix(got, slice) {
		t.Fatalf("срез испорчен: %q", got)
	}
}

func TestWithFootnoteDefsDoesNotDuplicate(t *testing.T) {
	pageText := "Текст[^1].\n\n[^1]: Тело."
	// Определение уже внутри среза — дотягивать нечего.
	slice := "Текст[^1].\n\n[^1]: Тело."

	got := withFootnoteDefs(slice, pageText)

	if strings.Count(got, "[^1]: Тело.") != 1 {
		t.Fatalf("тело продублировано: %q", got)
	}
}

func TestWithFootnoteDefsIgnoresUnknownMarkers(t *testing.T) {
	// Ссылка есть, определения на странице нет — это битая разметка страницы,
	// а не повод ломать выдачу вырезки.
	got := withFootnoteDefs("Текст[^нет].", "Текст[^нет].")
	if got != "Текст[^нет]." {
		t.Fatalf("срез изменён: %q", got)
	}
}

func TestWithFootnoteDefsKeepsMultilineBody(t *testing.T) {
	pageText := "Текст[^1].\n\n[^1]: Первая строка\n    вторая строка тела.\n\n[^2]: Чужое."
	got := withFootnoteDefs("Текст[^1].", pageText)

	if !strings.Contains(got, "вторая строка тела.") {
		t.Fatalf("продолжение тела потеряно: %q", got)
	}
	if strings.Contains(got, "Чужое.") {
		t.Fatalf("прихвачено чужое тело: %q", got)
	}
}

func TestPageChunksSplitsAroundSpan(t *testing.T) {
	text := "до вырезки|внутри вырезки|после вырезки"
	start := strings.Index(text, "внутри")
	end := start + len("внутри вырезки")

	got := pageChunks(text, [][2]int{{start, end}})

	if len(got) != 3 {
		t.Fatalf("кусков %d, ожидалось 3: %+v", len(got), got)
	}
	if got[0].Inside || !got[1].Inside || got[2].Inside {
		t.Fatalf("флаги: %+v", got)
	}
	if got[1].Text != "внутри вырезки" {
		t.Fatalf("средний кусок %q", got[1].Text)
	}
}

func TestPageChunksWholePageIsOneChunk(t *testing.T) {
	got := pageChunks("страница целиком", nil)
	if len(got) != 1 || got[0].Inside {
		t.Fatalf("без вырезок: %+v", got)
	}
	// Вырезка во всю страницу — тоже один кусок, но внутренний.
	full := pageChunks("страница целиком", [][2]int{{0, len("страница целиком")}})
	if len(full) != 1 || !full[0].Inside {
		t.Fatalf("вырезка во всю страницу: %+v", full)
	}
}

func TestPageChunksMergesOverlappingSpans(t *testing.T) {
	text := "аааббввв"
	// Две вырезки перекрылись — читателю всё равно, это одна подсвеченная
	// область; иначе граница между ними резала бы текст на пустом месте.
	// Смещения считаются через len() рун-групп, а не литералами: кириллица
	// занимает 2 байта на символ, и {0,5}/{3,8} в байтах приходятся на
	// середину руны, а не на границу "ааа|бб|ввв", как выглядит по буквам.
	firstEnd := len("ааабб")
	secondStart := len("ааа")
	got := pageChunks(text, [][2]int{{0, firstEnd}, {secondStart, len(text)}})

	if len(got) != 1 || !got[0].Inside || got[0].Text != text {
		t.Fatalf("перекрытие: %+v", got)
	}
}

func TestPageChunksMergesTouchingSpans(t *testing.T) {
	// Отрезки не перекрываются, а ровно соприкасаются концом с началом —
	// именно на этом висит "<=" (а не "<") в условии слияния. Не проверить
	// это отдельно от перекрытия значило бы не заметить, если "<=" когда-то
	// молча превратится в "<".
	text := "1234567890"
	got := pageChunks(text, [][2]int{{0, 5}, {5, 10}})

	if len(got) != 1 || !got[0].Inside || got[0].Text != text {
		t.Fatalf("соприкосновение: %+v", got)
	}
}

func TestPageChunksOrdersUnsortedSpans(t *testing.T) {
	text := "111222333444"
	got := pageChunks(text, [][2]int{{9, 12}, {0, 3}})

	if len(got) != 3 {
		t.Fatalf("кусков %d: %+v", len(got), got)
	}
	if !got[0].Inside || got[1].Inside || !got[2].Inside {
		t.Fatalf("флаги: %+v", got)
	}
}

func TestPageChunksKeepsCyrillicRuneBoundaries(t *testing.T) {
	// Смещения — байтовые: кириллица занимает 2 байта на символ, и срез,
	// съехавший на середину руны, дал бы невалидный UTF-8 в куске. Литералы
	// в спане нарочно не круглые (не кратные 2), чтобы ошибка в арифметике
	// байт/рун не совпала бы случайно с границей руны.
	text := "абвгдеёжзи"
	start := len("абв")
	end := len("абвгде")
	got := pageChunks(text, [][2]int{{start, end}})

	if len(got) != 3 {
		t.Fatalf("кусков %d, ожидалось 3: %+v", len(got), got)
	}
	joined := ""
	for _, c := range got {
		if !utf8.ValidString(c.Text) {
			t.Fatalf("кусок рвёт руну: %q (% x)", c.Text, c.Text)
		}
		joined += c.Text
	}
	if joined != text {
		t.Fatalf("склейка кусков дала %q, ожидалось %q", joined, text)
	}
	if got[1].Text != "где" {
		t.Fatalf("средний кусок %q, ожидалось %q", got[1].Text, "где")
	}
}

func TestPageChunksClampsBrokenSpans(t *testing.T) {
	text := "короткий"
	got := pageChunks(text, [][2]int{{4, 9999}, {-3, 2}})

	// Ни один кусок не должен выйти за текст и ни один не должен быть пустым.
	joined := ""
	for _, c := range got {
		if c.Text == "" {
			t.Fatalf("пустой кусок: %+v", got)
		}
		joined += c.Text
	}
	if joined != text {
		t.Fatalf("склейка кусков дала %q", joined)
	}
}
