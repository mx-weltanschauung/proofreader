package book

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fb2SchemaValidatorPath — путь к обёртке над lxml, которая знает, где
// вендорена схема (testdata/fb2schema). Определяется через runtime.Caller,
// а не через рабочий каталог: `go test` запускает тесты пакета из его
// собственной директории, но полагаться на это не нужно, когда путь можно
// получить надёжно.
func fb2SchemaValidatorPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не удалось определить путь к тестовому файлу")
	}
	return filepath.Join(filepath.Dir(thisFile), "testdata", "fb2schema", "validate.py")
}

// requireFB2SchemaValidator пропускает тест там, где нет python3-lxml —
// схемная проверка так не работает нигде, но остальной пакет должен
// собираться и тестироваться и без неё.
func requireFB2SchemaValidator(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 не найден — схемная проверка FB2 пропущена")
	}
	if err := exec.Command("python3", "-c", "import lxml.etree").Run(); err != nil {
		t.Skip("python3-lxml не установлен — схемная проверка FB2 пропущена")
	}
}

// validateFB2Schema прогоняет got через настоящую FictionBook.xsd (lxml,
// XML Schema валидация). Это сильнее xml.Unmarshal, которым проверяют себя
// остальные тесты пакета: тот требует только well-formedness, а не согласия
// со схемой — sectionType, например, требует xs:choice между вложенными
// <section> и листовым содержимым, и Unmarshal в структуру эту разницу
// молча проглатывает.
func validateFB2Schema(t *testing.T, got string) {
	t.Helper()
	requireFB2SchemaValidator(t)

	cmd := exec.Command("python3", fb2SchemaValidatorPath(t))
	cmd.Stdin = strings.NewReader(got)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Errorf("FB2 не проходит схемную проверку (%v):\n%s\n\nвывод валидатора:\n%s",
			err, got, stderr.String())
	}
}

// fb2Page — минимальная страница-фикстура для тестов формы дерева.
func fb2Page(internal, printed int) Page {
	return Page{Internal: internal, Printed: printed, HTML: "<p>текст " + string(rune('0'+printed%10)) + ".</p>"}
}

// leafSection — секция без вложенных детей, только собственные страницы.
func leafSection(title string, pages ...Page) Section {
	return Section{Title: title, Blocks: []Block{{Pages: pages}}}
}

func bookWithTopSection(s Section) *Book {
	return &Book{
		Meta:     Meta{Title: "Схемная проверка", Authors: []string{"Автор"}, Lang: "ru"},
		Sections: []Section{s},
	}
}

// Схема FB2 (sectionType) — xs:choice: <section> содержит либо только
// вложенные <section>, либо только листовое содержимое (p/subtitle/cite/…),
// никогда оба сразу. У секции с преамбулой перед первым ребёнком блоки
// перемежаются (BuildSection, pkg/book/tree.go) — 71 глава живого корпуса
// устроена именно так.
func TestFB2SchemaValidatesPreambleBeforeChild(t *testing.T) {
	s := Section{
		Title: "Родитель",
		Blocks: []Block{
			{Pages: []Page{fb2Page(1, 101)}},
			{Child: ptrSection(leafSection("Ребёнок", fb2Page(2, 102)))},
		},
	}
	validateFB2Schema(t, writeString(t, FB2Writer{}, bookWithTopSection(s)))
}

// Дыра между соседями (12 глав живого корпуса) — та же перемежающаяся форма
// блоков, только собственные страницы стоят между двумя детьми, а не перед
// первым.
func TestFB2SchemaValidatesGapBetweenChildren(t *testing.T) {
	s := Section{
		Title: "Родитель",
		Blocks: []Block{
			{Child: ptrSection(leafSection("Первый", fb2Page(1, 101)))},
			{Pages: []Page{fb2Page(2, 102)}},
			{Child: ptrSection(leafSection("Второй", fb2Page(3, 103)))},
		},
	}
	validateFB2Schema(t, writeString(t, FB2Writer{}, bookWithTopSection(s)))
}

// Хвост после последнего ребёнка (6 глав живого корпуса).
func TestFB2SchemaValidatesTailAfterChild(t *testing.T) {
	s := Section{
		Title: "Родитель",
		Blocks: []Block{
			{Child: ptrSection(leafSection("Ребёнок", fb2Page(1, 101)))},
			{Pages: []Page{fb2Page(2, 102)}},
		},
	}
	validateFB2Schema(t, writeString(t, FB2Writer{}, bookWithTopSection(s)))
}

// Все три формы сразу: преамбула, дыра, хвост — вперемешку с двумя детьми.
func TestFB2SchemaValidatesPreambleGapAndTailTogether(t *testing.T) {
	s := Section{
		Title: "Родитель",
		Blocks: []Block{
			{Pages: []Page{fb2Page(1, 101)}},
			{Child: ptrSection(leafSection("Первый", fb2Page(2, 102)))},
			{Pages: []Page{fb2Page(3, 103)}},
			{Child: ptrSection(leafSection("Второй", fb2Page(4, 104)))},
			{Pages: []Page{fb2Page(5, 105)}},
		},
	}
	validateFB2Schema(t, writeString(t, FB2Writer{}, bookWithTopSection(s)))
}

// Секция только с собственными страницами — форма уже валидна без обёртки,
// проверяем, что фикс её не ломает.
func TestFB2SchemaValidatesOnlyOwnPages(t *testing.T) {
	s := leafSection("Только страницы", fb2Page(1, 101), fb2Page(2, 102))
	validateFB2Schema(t, writeString(t, FB2Writer{}, bookWithTopSection(s)))
}

// Секция только с детьми, без собственных страниц — тоже уже валидная форма.
func TestFB2SchemaValidatesOnlyChildren(t *testing.T) {
	s := Section{
		Title: "Только дети",
		Blocks: []Block{
			{Child: ptrSection(leafSection("Первый", fb2Page(1, 101)))},
			{Child: ptrSection(leafSection("Второй", fb2Page(2, 102)))},
		},
	}
	validateFB2Schema(t, writeString(t, FB2Writer{}, bookWithTopSection(s)))
}

// Смешанная форма три уровня в глубину: у деда есть и текст, и ребёнок,
// у которого тоже есть и текст, и свой ребёнок-лист.
func TestFB2SchemaValidatesThreeLevelsDeep(t *testing.T) {
	grandchild := leafSection("Внук", fb2Page(3, 103))
	child := Section{
		Title: "Ребёнок",
		Blocks: []Block{
			{Pages: []Page{fb2Page(2, 102)}},
			{Child: &grandchild},
		},
	}
	s := Section{
		Title: "Дед",
		Blocks: []Block{
			{Pages: []Page{fb2Page(1, 101)}},
			{Child: &child},
		},
	}
	validateFB2Schema(t, writeString(t, FB2Writer{}, bookWithTopSection(s)))
}

// sampleBook() сама по себе — секция "Часть первая" с преамбулой перед
// вложенной "Глава первая": ровно форма, ломавшая схему до фикса.
func TestFB2SchemaValidatesSampleBook(t *testing.T) {
	validateFB2Schema(t, writeString(t, FB2Writer{}, sampleBook()))
}

// Обёртка не должна появляться там, где перемежающихся блоков нет: у секции
// с одними только детьми не должно быть лишней анонимной <section> вокруг
// каждого ребёнка.
func TestFB2DoesNotWrapWhenBlocksAreNotMixed(t *testing.T) {
	s := Section{
		Title: "Только дети",
		Blocks: []Block{
			{Child: ptrSection(leafSection("Первый", fb2Page(1, 101)))},
			{Child: ptrSection(leafSection("Второй", fb2Page(2, 102)))},
		},
	}
	got := writeString(t, FB2Writer{}, bookWithTopSection(s))

	// Три секции всего ожидается: верхняя ("Только дети") + два листовых
	// ребёнка. Обёртка добавила бы четвёртую.
	if n := strings.Count(got, "<section>\n"); n != 3 {
		t.Errorf("лишняя обёртка вокруг детей без перемежающихся блоков: %d <section>, хотел 3:\n%s", n, got)
	}
}

func ptrSection(s Section) *Section { return &s }
