package book

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// sampleBook — маленькая книга со всеми интересными случаями: две секции
// верхнего уровня, вложенность, преамбула, кириллица в заголовке.
func sampleBook() *Book {
	inner := Section{
		Title: "Глава первая",
		Blocks: []Block{{Pages: []Page{
			{Internal: 2, Printed: 102, Markdown: "Текст главы.", HTML: "<p>Текст главы.</p>"},
		}}},
	}
	return &Book{
		Meta: Meta{
			Title:     "Пробная книга",
			Authors:   []string{"К. Маркс"},
			Edition:   "Сочинения",
			Volume:    "Том 1",
			PageFrom:  101,
			PageTo:    103,
			URL:       "https://lib.example.org/works/1",
			Modified:  time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC),
			Lang:      "ru",
			Proofread: Proofread{Total: 3, Human: 3},
		},
		Sections: []Section{
			{
				Title: "Часть первая",
				Blocks: []Block{
					{Pages: []Page{{Internal: 1, Printed: 101, Markdown: "Преамбула.", HTML: "<p>Преамбула.</p>"}}},
					{Child: &inner},
				},
				NotesHTML: `<div class="footnotes">` +
					`<section class="notes-group notes-group--endnote">` +
					`<h2>Примечания</h2><ul class="fn-list">` +
					`<li id="fn:1-1" class="fn-item fn-item--endnote">` +
					`<a class="fn-back" href="#fnref:1-1">1</a> примечание</li>` +
					`</ul></section></div>`,
			},
			{
				Title:  "Часть вторая",
				Author: "Ф. Энгельс",
				Blocks: []Block{{Pages: []Page{
					{Internal: 3, Printed: 103, Markdown: "Хвост.", HTML: "<p>Хвост.</p>"},
				}}},
			},
		},
	}
}

func writeString(t *testing.T, wr Writer, b *Book) string {
	t.Helper()
	var buf bytes.Buffer
	if err := wr.Write(&buf, b); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	return buf.String()
}

// untitledSectionTitle — буквальное повторение internal/api.untitledSectionTitle:
// pkg/book не знает о internal/api (не должен — правило слоя), поэтому
// константа для проверки «непустого имени безымянной секции в оглавлении»
// продублирована тут, а не импортирована.
const untitledSectionTitle = "«Без заглавия»"

// untitledSectionBook — sampleBook с первой секцией верхнего уровня,
// переименованной в то самое имя, которым internal/api.topLevelNodes
// подписывает страницы вне глав (найденную ревизией #2 дыру в оглавлении).
// Используется всеми четырьмя писателями, чтобы убедиться: ни один не
// заводит для такой секции пустую подпись ссылки/заголовка.
func untitledSectionBook() *Book {
	b := sampleBook()
	b.Sections[0].Title = untitledSectionTitle
	return b
}

func TestForFormatKnowsMarkdown(t *testing.T) {
	w, ok := ForFormat("md")
	if !ok {
		t.Fatal("формат md не найден")
	}
	if w.Ext() != "md" {
		t.Errorf("Ext() = %q, хотел md", w.Ext())
	}
	if !strings.HasPrefix(w.ContentType(), "text/markdown") {
		t.Errorf("ContentType() = %q, хотел text/markdown", w.ContentType())
	}
}

func TestForFormatRejectsUnknown(t *testing.T) {
	if _, ok := ForFormat("docx"); ok {
		t.Error("неизвестный формат принят")
	}
}

func TestMarkdownCarriesTitleAndText(t *testing.T) {
	got := writeString(t, MarkdownWriter{}, sampleBook())

	for _, want := range []string{
		"# Пробная книга",
		"Источник: Сочинения. Том 1, с. 101—103",
		"## Часть первая",
		"### Глава первая",
		"Преамбула.",
		"Текст главы.",
		"Хвост.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("в выгрузке нет %q:\n%s", want, got)
		}
	}
}

// TitleLines несёт название второй строкой следом за "# {Title}" — без
// вычёркивания книга открывалась бы им дважды подряд.
func TestMarkdownTitleAppearsOnce(t *testing.T) {
	got := writeString(t, MarkdownWriter{}, sampleBook())

	if n := strings.Count(got, "Пробная книга"); n != 1 {
		t.Errorf("название встречается %d раз(а), хотел 1:\n%s", n, got)
	}
}

// Секция без своей главы (дыра в оглавлении, см. internal/api.topLevelNodes)
// получает от источника честное имя, а не пустую строку — заголовок markdown
// не должен превращаться в голое "## ".
func TestMarkdownRendersUntitledSectionLabel(t *testing.T) {
	got := writeString(t, MarkdownWriter{}, untitledSectionBook())

	if !strings.Contains(got, "## "+untitledSectionTitle) {
		t.Errorf("в выгрузке нет заголовка безымянной секции %q:\n%s", untitledSectionTitle, got)
	}
	if strings.Contains(got, "## \n") {
		t.Errorf("пустой заголовок секции в выгрузке:\n%s", got)
	}
}

// Уровень заголовка растёт с вложенностью: часть — ##, её глава — ###.
func TestMarkdownNestsHeadingLevels(t *testing.T) {
	got := writeString(t, MarkdownWriter{}, sampleBook())

	partAt := strings.Index(got, "## Часть первая")
	chapterAt := strings.Index(got, "### Глава первая")
	if partAt < 0 || chapterAt < 0 || chapterAt < partAt {
		t.Errorf("вложенность заголовков нарушена:\n%s", got)
	}
}

func TestMarkdownMarksPrintedPages(t *testing.T) {
	got := writeString(t, MarkdownWriter{}, sampleBook())

	if !strings.Contains(got, "[101]") || !strings.Contains(got, "[103]") {
		t.Errorf("нет маркеров печатных страниц:\n%s", got)
	}
	// Внутренние номера в выгрузку не едут — они деталь базы.
	if strings.Contains(got, "[1]\n") {
		t.Errorf("в выгрузку попал внутренний номер страницы:\n%s", got)
	}
}

func TestMarkdownShowsAuthorOverride(t *testing.T) {
	got := writeString(t, MarkdownWriter{}, sampleBook())

	if !strings.Contains(got, "Ф. Энгельс") {
		t.Errorf("автор секции потерян:\n%s", got)
	}
}

// Побайтовая воспроизводимость: на ней держится ETag.
func TestMarkdownIsReproducible(t *testing.T) {
	first := writeString(t, MarkdownWriter{}, sampleBook())
	second := writeString(t, MarkdownWriter{}, sampleBook())

	if first != second {
		t.Error("две выгрузки одной книги различаются побайтово")
	}
}

// footnoteBook — отдельная фикстура для тестов переименования сносок.
// sampleBook() уже используется задачами 6-8 как общий фикстур — трогать
// его нельзя, чтобы не сдвинуть зелёные тесты соседних задач без причины.
func footnoteBook(pages ...Page) *Book {
	return &Book{
		Meta: Meta{Title: "Заметки", Lang: "ru"},
		Sections: []Section{
			{
				Title:  "Раздел",
				Blocks: []Block{{Pages: pages}},
			},
		},
	}
}

// Две страницы книги обычно определяют одноимённые сноски независимо друг
// от друга — [^1] на одной странице и [^1] на другой значат разное. Простая
// склейка текста страниц столкнула бы оба определения в одном документе;
// писатель обязан развести их по странице.
func TestMarkdownPrefixesFootnotesPerPage(t *testing.T) {
	got := writeString(t, MarkdownWriter{}, footnoteBook(
		Page{Internal: 1, Printed: 101, Markdown: "Первая мысль[^1].\n\n[^1]: Первое примечание."},
		Page{Internal: 2, Printed: 102, Markdown: "Вторая мысль[^1].\n\n[^1]: Второе примечание."},
	))

	for _, want := range []string{
		"Первая мысль[^101-1].",
		"[^101-1]: Первое примечание.",
		"Вторая мысль[^102-1].",
		"[^102-1]: Второе примечание.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("в выгрузке нет %q:\n%s", want, got)
		}
	}
	// Исходное, не привязанное к странице имя сноски остаться не должно —
	// иначе ссылка со второй страницы могла бы случайно попасть на
	// определение с первой.
	if strings.Contains(got, "[^1]") {
		t.Errorf("остался непереименованный (не постраничный) идентификатор сноски:\n%s", got)
	}
}

// Имя сноски — не всегда число: в корпусе встречаются "[^r2]"/"[^s3]"
// (постраничные примечания переводчика и редакторские отметки-астериски).
func TestMarkdownPrefixesNonNumericFootnoteName(t *testing.T) {
	got := writeString(t, MarkdownWriter{}, footnoteBook(
		Page{Internal: 1, Printed: 55, Markdown: "Слово[^r2].\n\n[^r2]: Примечание переводчика."},
	))

	if !strings.Contains(got, "Слово[^55-r2].") || !strings.Contains(got, "[^55-r2]: Примечание переводчика.") {
		t.Errorf("не-числовое имя сноски переименовано неверно:\n%s", got)
	}
}

// "[^1]", показанный внутри отгороженного блока кода как пример разметки, —
// не настоящая сноска и трогать его нельзя.
func TestMarkdownLeavesFootnoteInFencedCodeUntouched(t *testing.T) {
	got := writeString(t, MarkdownWriter{}, footnoteBook(
		Page{Internal: 1, Printed: 10, Markdown: "Пример разметки:\n\n```\n[^1] — вот так выглядит сноска.\n```\n"},
	))

	if !strings.Contains(got, "[^1] — вот так выглядит сноска.") {
		t.Errorf("текст внутри блока кода изменился:\n%s", got)
	}
	if strings.Contains(got, "[^10-1]") {
		t.Errorf("сноска внутри блока кода была переименована:\n%s", got)
	}
}

// Ссылка без определения и определение без ссылки — реальность корпуса
// (сноска потерялась при OCR, или её текст не сохранился). Писатель не
// должен ни падать, ни портить то, что есть.
func TestMarkdownFootnoteMismatchDoesNotCrash(t *testing.T) {
	got := writeString(t, MarkdownWriter{}, footnoteBook(
		Page{Internal: 1, Printed: 20, Markdown: "Ссылка есть[^1], определения нет.\n\n[^2]: Определение есть, ссылки нет."},
	))

	if !strings.Contains(got, "Ссылка есть[^20-1], определения нет.") {
		t.Errorf("непарная ссылка не переименована:\n%s", got)
	}
	if !strings.Contains(got, "[^20-2]: Определение есть, ссылки нет.") {
		t.Errorf("непарное определение не переименовано:\n%s", got)
	}
}

func TestOneLine(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"crlf", "Строка один\r\nСтрока два", "Строка один Строка два"},
		{"одинокий CR", "Строка один\rСтрока два", "Строка один Строка два"},
		{"таб", "Строка\tодин", "Строка один"},
		{"подряд идущие пробелы", "Слово    слово", "Слово слово"},
		{"пробел по краям", "  Пробелы по краям  ", "Пробелы по краям"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := oneLine(c.in); got != c.want {
				t.Errorf("oneLine(%q) = %q, хотел %q", c.in, got, c.want)
			}
		})
	}
}

// Текст MarkdownWithPageStarts — ровно то, что пишет MarkdownWriter: это
// одна и та же книга для скачивания и для нейросети.
func TestMarkdownWithPageStartsMatchesWriter(t *testing.T) {
	b := sampleBook()
	text, _ := MarkdownWithPageStarts(b)
	if want := writeString(t, MarkdownWriter{}, b); text != want {
		t.Fatalf("текст разошёлся с MarkdownWriter:\n--- got\n%s\n--- want\n%s", text, want)
	}
}

// Заголовок секции пишется ПЕРЕД её первой полосой. Начало полосы, стоящей
// сразу за заголовками, указывает на первый из них: иначе разрез по нему
// оставил бы заголовок в хвосте предыдущего куска, отдельно от текста.
func TestMarkdownPageStartsIncludeLeadingHeadings(t *testing.T) {
	text, starts := MarkdownWithPageStarts(sampleBook())
	if len(starts) != 3 {
		t.Fatalf("начал полос %d, ожидалось 3: %+v", len(starts), starts)
	}
	wantPrefix := []string{
		"## Часть первая\n\n[101]\n\n",
		"### Глава первая\n\n[102]\n\n",
		"## Часть вторая\n\n*Ф. Энгельс*\n\n[103]\n\n",
	}
	wantPrinted := []int{101, 102, 103}
	for i, st := range starts {
		if st.Printed != wantPrinted[i] {
			t.Errorf("полоса %d: Printed %d, ожидалось %d", i, st.Printed, wantPrinted[i])
		}
		if !strings.HasPrefix(text[st.Offset:], wantPrefix[i]) {
			t.Errorf("полоса %d начинается не с заголовка:\n%q", i, text[st.Offset:min(len(text), st.Offset+60)])
		}
	}
}

// Полоса без заголовка перед собой начинается ровно на своём маркере.
func TestMarkdownPageStartWithoutHeadingIsMarker(t *testing.T) {
	b := &Book{Meta: Meta{Title: "Т"}, Sections: []Section{{
		Title: "С",
		Blocks: []Block{{Pages: []Page{
			{Internal: 1, Printed: 1, Markdown: "Один."},
			{Internal: 2, Printed: 2, Markdown: "Два."},
		}}},
	}}}
	text, starts := MarkdownWithPageStarts(b)
	if len(starts) != 2 {
		t.Fatalf("начал %d, ожидалось 2", len(starts))
	}
	if !strings.HasPrefix(text[starts[1].Offset:], "[2]\n\nДва.") {
		t.Errorf("вторая полоса начинается не с маркера: %q", text[starts[1].Offset:])
	}
}
