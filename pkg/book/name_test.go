package book

import (
	"mime"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestContentDispositionCarriesBothNames(t *testing.T) {
	got := ContentDisposition("work-47", "Капитал. Книга первая", "epub")

	if !strings.HasPrefix(got, "attachment; ") {
		t.Errorf("нет attachment: %q", got)
	}
	if !strings.Contains(got, `filename="work-47.epub"`) {
		t.Errorf("нет ASCII-запаски: %q", got)
	}
	// Кодировка в filename* по RFC 8187 регистронезависима, и Go пишет её
	// строчными — сверено запуском mime.FormatMediaType, а не по памяти.
	if !strings.Contains(strings.ToLower(got), "filename*=utf-8''") {
		t.Errorf("нет UTF-8-имени: %q", got)
	}
}

// Заголовок обязан разбираться штатным разборщиком, иначе браузер молча
// возьмёт ASCII-запаску вместо человеческого имени.
func TestContentDispositionParsesBackToHumanName(t *testing.T) {
	got := ContentDisposition("work-47", "Капитал. Книга первая", "epub")

	_, params, err := mime.ParseMediaType(got)
	if err != nil {
		t.Fatalf("заголовок не разбирается: %v (%q)", err, got)
	}
	if params["filename"] != "Капитал. Книга первая.epub" {
		t.Errorf("разобранное имя = %q, хотел «Капитал. Книга первая.epub»", params["filename"])
	}
}

func TestContentDispositionStripsPathAndControlCharacters(t *testing.T) {
	got := ContentDisposition("work-1", "а/б\\в:г*д?е\"ж<з>и|к\nл", "md")

	_, params, err := mime.ParseMediaType(got)
	if err != nil {
		t.Fatalf("заголовок не разбирается: %v", err)
	}
	name := params["filename"]
	for _, bad := range []string{"/", "\\", ":", "*", "?", `"`, "<", ">", "|", "\n"} {
		if strings.Contains(name, bad) {
			t.Errorf("в имени уцелел %q: %q", bad, name)
		}
	}
}

// Обрезка идёт по границе символа. На кириллице (2 байта на букву) обрезка по
// байту рассекает букву пополам и даёт невалидный UTF-8.
func TestContentDispositionTruncatesOnRuneBoundary(t *testing.T) {
	long := strings.Repeat("я", 300)

	got := ContentDisposition("work-1", long, "fb2")

	_, params, err := mime.ParseMediaType(got)
	if err != nil {
		t.Fatalf("заголовок не разбирается: %v", err)
	}
	name := strings.TrimSuffix(params["filename"], ".fb2")
	if !utf8.ValidString(name) {
		t.Errorf("имя обрезано по байту, UTF-8 сломан: %q", name)
	}
	if n := utf8.RuneCountInString(name); n > 100 {
		t.Errorf("длина имени = %d символов, хотел не больше 100", n)
	}
}

func TestContentDispositionFallsBackWhenNameIsEmpty(t *testing.T) {
	got := ContentDisposition("collection-abc", "   ", "html")

	_, params, err := mime.ParseMediaType(got)
	if err != nil {
		t.Fatalf("заголовок не разбирается: %v", err)
	}
	if params["filename"] != "collection-abc.html" {
		t.Errorf("имя = %q, хотел запаску collection-abc.html", params["filename"])
	}
}

// mime.FormatMediaType кодирует в filename* только при наличии не-ASCII
// символов — на чисто ASCII имени он молча пишет второй filename=, и
// заголовок перестаёт разбираться. В корпусе 227 глав названы латиницей
// или римскими цифрами (см. TestContentDispositionParsesRomanNumeralTitle) —
// это не редкий крайний случай, а обычное имя главы.
func TestContentDispositionParsesASCIIHumanName(t *testing.T) {
	got := ContentDisposition("work-1", "Introduction to Philosophy", "epub")

	_, params, err := mime.ParseMediaType(got)
	if err != nil {
		t.Fatalf("заголовок не разбирается: %v (%q)", err, got)
	}
	if params["filename"] != "Introduction to Philosophy.epub" {
		t.Errorf("разобранное имя = %q, хотел %q", params["filename"], "Introduction to Philosophy.epub")
	}
}

// Реальный случай из корпуса: главы часто называются римскими цифрами.
func TestContentDispositionParsesRomanNumeralTitle(t *testing.T) {
	got := ContentDisposition("chapter-1", "I.", "md")

	_, params, err := mime.ParseMediaType(got)
	if err != nil {
		t.Fatalf("заголовок не разбирается: %v (%q)", err, got)
	}
	if params["filename"] != "I..md" {
		t.Errorf("разобранное имя = %q, хотел %q", params["filename"], "I..md")
	}
}

func TestContentDispositionParsesMixedAlphabetName(t *testing.T) {
	got := ContentDisposition("work-2", "Marx: Капитал", "epub")

	_, params, err := mime.ParseMediaType(got)
	if err != nil {
		t.Fatalf("заголовок не разбирается: %v (%q)", err, got)
	}
	// sanitizeFileName заменяет ":" пробелом и схлопывает пробелы.
	if params["filename"] != "Marx Капитал.epub" {
		t.Errorf("разобранное имя = %q, хотел %q", params["filename"], "Marx Капитал.epub")
	}
}

// Пробел, запятая, точка с запятой и двойная кавычка — обычные символы в
// библиографических названиях и одновременно спецсимволы грамматики этого
// заголовка. Двойная кавычка режется sanitizeFileName; остальные три должны
// пройти сквозь round-trip как есть.
func TestContentDispositionParsesNameWithHeaderSpecialCharacters(t *testing.T) {
	got := ContentDisposition("work-3", `Соч., т. 23; "Капитал"`, "fb2")

	_, params, err := mime.ParseMediaType(got)
	if err != nil {
		t.Fatalf("заголовок не разбирается: %v (%q)", err, got)
	}
	want := "Соч., т. 23; Капитал.fb2"
	if params["filename"] != want {
		t.Errorf("разобранное имя = %q, хотел %q", params["filename"], want)
	}
}
