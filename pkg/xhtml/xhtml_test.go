package xhtml

import (
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// Рендерер собран с флагом html.CompletePage, поэтому каждая страница
// приезжает целым документом. Браузер читальни это прощает, EPUB — нет.
const wholeDocument = `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
<html xmlns="http://www.w3.org/1999/xhtml">
<head>
  <title></title>
  <meta name="GENERATOR" content="gomarkdown" />
</head>
<body>

<h1 id="заголовок">Заголовок</h1>

<p>Текст и <br> сырой HTML.</p>

</body>
</html>
`

func TestBodyDropsDocumentWrapper(t *testing.T) {
	got, err := Body(wholeDocument)
	if err != nil {
		t.Fatalf("Body() error = %v", err)
	}
	for _, forbidden := range []string{"<!DOCTYPE", "<html", "<head", "<title", "<body", "GENERATOR"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("выход содержит %q, обёртка документа не снята:\n%s", forbidden, got)
		}
	}
	if !strings.Contains(got, `<h1 id="заголовок">Заголовок</h1>`) {
		t.Errorf("заголовок потерян:\n%s", got)
	}
}

func TestBodyClosesVoidElements(t *testing.T) {
	got, err := Body(wholeDocument)
	if err != nil {
		t.Fatalf("Body() error = %v", err)
	}
	if !strings.Contains(got, "<br/>") {
		t.Errorf("сырой <br> остался незакрытым, XHTML невалиден:\n%s", got)
	}
}

func TestBodyResolvesNamedEntities(t *testing.T) {
	got, err := Body("<html><body><p>а&nbsp;б&amp;в</p></body></html>")
	if err != nil {
		t.Fatalf("Body() error = %v", err)
	}
	// Именованная сущность разобрана в рун; амперсанд экранирован заново.
	if strings.Contains(got, "&nbsp;") {
		t.Errorf("&nbsp; уцелел — в XHTML без DTD он невалиден: %s", got)
	}
	if !strings.Contains(got, " ") {
		t.Errorf("неразрывный пробел потерян: %q", got)
	}
	if !strings.Contains(got, "&amp;") {
		t.Errorf("амперсанд не экранирован: %q", got)
	}
}

func TestBodyEscapesTextAndAttributes(t *testing.T) {
	got, err := Body(`<html><body><p title='а "б" в'>1 &lt; 2 &amp; 3</p></body></html>`)
	if err != nil {
		t.Fatalf("Body() error = %v", err)
	}
	if !strings.Contains(got, "1 &lt; 2 &amp; 3") {
		t.Errorf("текст экранирован неверно: %s", got)
	}
	if !strings.Contains(got, `title="а &quot;б&quot; в"`) {
		t.Errorf("кавычки в атрибуте не экранированы: %s", got)
	}
}

func TestBodyEmptyInput(t *testing.T) {
	got, err := Body("")
	if err != nil {
		t.Fatalf("Body() error = %v", err)
	}
	if got != "" {
		t.Errorf("Body(\"\") = %q, хотел пусто", got)
	}
}

// assertWellFormedXML прогоняет фрагмент через строгий XML-декодер — ровно ту
// проверку, что делает читалка EPUB. Фрагмент оборачивается в синтетический
// корень: <body> отдаёт несколько соседних узлов верхнего уровня (h1, p, ...),
// а XML требует единственный корневой элемент.
func assertWellFormedXML(t *testing.T, fragment string) {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader("<root>" + fragment + "</root>"))
	dec.Strict = true
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("вывод не строгий XML: %v\nфрагмент:\n%s", err, fragment)
		}
	}
}

func TestBodyDropsDisallowedControlCharsInText(t *testing.T) {
	// \x00 парсер вычищает сам ещё до узла текста; \x0B и \x1F — нет, это
	// зона ответственности EscapeText.
	got, err := Body("<html><body><p>раз\x00два\x0Bтри\x1Fчетыре</p></body></html>")
	if err != nil {
		t.Fatalf("Body() error = %v", err)
	}
	for _, c := range []byte{0x00, 0x0B, 0x1F} {
		if strings.IndexByte(got, c) >= 0 {
			t.Errorf("недопустимый управляющий байт %#x уцелел в тексте: %q", c, got)
		}
	}
	if !strings.Contains(got, "<p>раздватричетыре</p>") {
		t.Errorf("окружающий текст испорчен вырезкой управляющих байт: %q", got)
	}
	assertWellFormedXML(t, got)
}

func TestBodyDropsDisallowedControlCharsInAttribute(t *testing.T) {
	got, err := Body("<html><body><p title=\"а\x0Bб\x1Fв\">x</p></body></html>")
	if err != nil {
		t.Fatalf("Body() error = %v", err)
	}
	if !strings.Contains(got, `title="абв"`) {
		t.Errorf("управляющие байты в атрибуте не вырезаны: %q", got)
	}
	assertWellFormedXML(t, got)
}

func TestBodyPreservesTabAndNewlineInText(t *testing.T) {
	got, err := Body("<html><body><p>а\tб\nв</p></body></html>")
	if err != nil {
		t.Fatalf("Body() error = %v", err)
	}
	if !strings.Contains(got, "а\tб\nв") {
		t.Errorf("таб или перевод строки в тексте не сохранились буквально: %q", got)
	}
	assertWellFormedXML(t, got)
}

func TestBodyEncodesTabAndNewlineInAttributeAsCharRefs(t *testing.T) {
	got, err := Body("<html><body><p title=\"а\tб\nв\">x</p></body></html>")
	if err != nil {
		t.Fatalf("Body() error = %v", err)
	}
	if !strings.Contains(got, "title=\"а&#9;б&#10;в\"") {
		t.Errorf("таб/перевод строки в атрибуте не заменены числовыми ссылками: %q", got)
	}
	assertWellFormedXML(t, got)
}

// EscapeText и EscapeAttr — прямые модульные проверки на строке с \r.
// Дойти до \r через Body() нельзя: препроцессинг HTML5-парсера сам сворачивает
// одиночный \r в \n ещё до токенизации (см. WHATWG "preprocessing the input
// stream"), так что через дерево этот байт никогда не долетает буквально.
func TestEscapeTextPreservesTabCRLF(t *testing.T) {
	got := EscapeText("a\tb\rc\nd")
	want := "a\tb\rc\nd"
	if got != want {
		t.Errorf("EscapeText(%q) = %q, хотел %q", "a\tb\rc\nd", got, want)
	}
}

func TestEscapeAttrEncodesTabCRLFAsCharRefs(t *testing.T) {
	got := EscapeAttr("a\tb\rc\nd")
	want := "a&#9;b&#13;c&#10;d"
	if got != want {
		t.Errorf("EscapeAttr(%q) = %q, хотел %q", "a\tb\rc\nd", got, want)
	}
}

func TestEscapeTextDropsDisallowedControlBytes(t *testing.T) {
	var disallowed []byte
	for c := 0; c < 0x20; c++ {
		if c == '\t' || c == '\n' || c == '\r' {
			continue
		}
		disallowed = append(disallowed, byte(c))
	}
	got := EscapeText("a" + string(disallowed) + "b")
	if got != "ab" {
		t.Errorf("EscapeText не вырезал все запрещённые управляющие байты: %q", got)
	}
}

// TestBodyDropsAttributeMangledByParserRecovery — конкретный репродюсер:
// незакрытая кавычка внутри значения атрибута заставляет обработчик ошибок
// HTML5-парсера свернуть остаток в отдельный атрибут с именем "сказала\"\"" —
// не является допустимым XML-именем ни при какой интерпретации.
func TestBodyDropsAttributeMangledByParserRecovery(t *testing.T) {
	got, err := Body(`<html><body><p title="она "сказала"">текст</p></body></html>`)
	if err != nil {
		t.Fatalf("Body() error = %v", err)
	}
	if strings.Contains(got, "сказала") {
		t.Errorf("искалеченный атрибут не вырезан: %q", got)
	}
	if !strings.Contains(got, "текст") {
		t.Errorf("текст элемента потерян вместе с атрибутом: %q", got)
	}
	assertWellFormedXML(t, got)
}

func TestBodyKeepsWellFormedAttributes(t *testing.T) {
	got, err := Body(`<html><body><table><tr><td colspan="2">x</td></tr></table><p title="Заголовок">y</p></body></html>`)
	if err != nil {
		t.Fatalf("Body() error = %v", err)
	}
	if !strings.Contains(got, `colspan="2"`) {
		t.Errorf("корректный атрибут colspan потерян: %q", got)
	}
	if !strings.Contains(got, `title="Заголовок"`) {
		t.Errorf("корректный атрибут с кириллическим значением потерян: %q", got)
	}
	assertWellFormedXML(t, got)
}

// Ниже — прямые проверки WriteNode на сконструированных узлах, в обход
// парсера: имя атрибута с пробелом, кавычкой или знаком равенства обычным
// разбором не получить (тэги эти символы обрывают токенизацию раньше), но
// html.Attribute — обычная структура, и *strings.Builder-сериализатор должен
// защищаться от такого значения независимо от того, как оно возникло.
func writeAttrXHTML(t *testing.T, attr html.Attribute) string {
	t.Helper()
	n := &html.Node{
		Type: html.ElementNode,
		Data: "p",
		Attr: []html.Attribute{attr},
	}
	var b strings.Builder
	WriteNode(&b, n)
	return b.String()
}

func TestWriteXHTMLDropsAttributeNameWithSpace(t *testing.T) {
	got := writeAttrXHTML(t, html.Attribute{Key: "a b", Val: "x"})
	if strings.Contains(got, "a b") {
		t.Errorf("имя атрибута с пробелом не отброшено: %q", got)
	}
}

func TestWriteXHTMLDropsAttributeNameWithQuote(t *testing.T) {
	got := writeAttrXHTML(t, html.Attribute{Key: `a"b`, Val: "x"})
	if strings.Contains(got, `a"b`) {
		t.Errorf("имя атрибута с кавычкой не отброшено: %q", got)
	}
}

func TestWriteXHTMLDropsAttributeNameWithEquals(t *testing.T) {
	got := writeAttrXHTML(t, html.Attribute{Key: "a=b", Val: "x"})
	if strings.Contains(got, "a=b") {
		t.Errorf("имя атрибута со знаком равенства не отброшено: %q", got)
	}
}

func TestWriteXHTMLDropsEmptyAttributeName(t *testing.T) {
	got := writeAttrXHTML(t, html.Attribute{Key: "", Val: "x"})
	want := "<p></p>"
	if got != want {
		t.Errorf("атрибут с пустым именем не отброшен: WriteNode() = %q, хотел %q (паники не было)", got, want)
	}
}

func TestWriteXHTMLKeepsValidNamesWithHyphenDotUnderscore(t *testing.T) {
	for _, key := range []string{"data-note", "xml.lang", "_id"} {
		got := writeAttrXHTML(t, html.Attribute{Key: key, Val: "x"})
		want := `<p ` + key + `="x"></p>`
		if got != want {
			t.Errorf("WriteNode(%s) = %q, хотел %q", key, got, want)
		}
	}
}

func TestWriteXHTMLDropsAttributeWithInvalidNamespace(t *testing.T) {
	got := writeAttrXHTML(t, html.Attribute{Namespace: "a b", Key: "lang", Val: "x"})
	if strings.Contains(got, "lang") {
		t.Errorf("атрибут с недопустимым префиксом пространства имён не отброшен целиком: %q", got)
	}
}

func TestBodyMixedDocumentIsWellFormedXML(t *testing.T) {
	input := `<html><body>` +
		`<h1 id="заголовок">Заголовок</h1>` +
		`<p title="она "сказала"">текст <br> ещё</p>` +
		`<table><tr><td colspan="2" rowspan="3">ячейка</td></tr></table>` +
		`</body></html>`
	got, err := Body(input)
	if err != nil {
		t.Fatalf("Body() error = %v", err)
	}
	assertWellFormedXML(t, got)
}
