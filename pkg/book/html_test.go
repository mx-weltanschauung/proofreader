package book

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestHTMLIsWellFormedXML(t *testing.T) {
	got := writeString(t, HTMLWriter{}, sampleBook())

	// Строгий разбор ловит незакрытые теги и невалидные сущности — то самое,
	// на чём спотыкается EPUB, собранный из того же HTML.
	dec := xml.NewDecoder(strings.NewReader(got))
	dec.Strict = true
	dec.AutoClose = nil
	dec.Entity = map[string]string{}
	for {
		_, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("HTML не разбирается как XML: %v", err)
		}
	}
}

func TestHTMLCarriesTitleTableOfContentsAndText(t *testing.T) {
	got := writeString(t, HTMLWriter{}, sampleBook())

	for _, want := range []string{
		"<title>Пробная книга</title>",
		"Часть первая",
		"Часть вторая",
		"Глава первая",
		"<p>Преамбула.</p>",
		"<p>Текст главы.</p>",
		`href="#sec-1"`,
		`id="sec-1"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("в HTML нет %q", want)
		}
	}
}

// Секция без своей главы (дыра в оглавлении, см. internal/api.topLevelNodes)
// получает от источника честное имя — ни ссылка в <nav class="toc">, ни
// заголовок секции не должны оставаться пустыми.
func TestHTMLRendersUntitledSectionLabel(t *testing.T) {
	got := writeString(t, HTMLWriter{}, untitledSectionBook())

	if !strings.Contains(got, `>`+untitledSectionTitle+`</a>`) {
		t.Errorf("в оглавлении нет ссылки на безымянную секцию %q:\n%s", untitledSectionTitle, got)
	}
	if strings.Contains(got, `href="#sec-1"></a>`) {
		t.Errorf("пустая ссылка оглавления:\n%s", got)
	}
	if strings.Contains(got, "<h2></h2>") {
		t.Errorf("пустой заголовок секции:\n%s", got)
	}
}

func TestHTMLAnchorsPrintedPages(t *testing.T) {
	got := writeString(t, HTMLWriter{}, sampleBook())

	if !strings.Contains(got, `id="p101"`) {
		t.Errorf("нет якоря печатной страницы 101")
	}
	if !strings.Contains(got, "101") {
		t.Errorf("нет видимого маркера страницы")
	}
}

// Примечания секции верхнего уровня ложатся в её конец, а не в конец книги.
func TestHTMLPutsNotesAtEndOfTopSection(t *testing.T) {
	got := writeString(t, HTMLWriter{}, sampleBook())

	notesAt := strings.Index(got, "примечание")
	// Заголовок раздела, а не голое совпадение по названию: строка «Часть
	// вторая» уже встретилась раньше в оглавлении, и поиск без тега <h1>
	// ловил бы её, а не начало самого раздела.
	secondAt := strings.Index(got, "<h1>Часть вторая")
	if notesAt < 0 {
		t.Fatal("примечания потеряны")
	}
	if notesAt > secondAt {
		t.Error("примечания первой части уехали за начало второй")
	}
}

func TestHTMLHasSelfContainedStyles(t *testing.T) {
	got := writeString(t, HTMLWriter{}, sampleBook())

	if !strings.Contains(got, "<style>") {
		t.Error("нет встроенных стилей — файл не самодостаточен")
	}
	if strings.Contains(got, "<link ") || strings.Contains(got, "<script") {
		t.Error("файл тянет внешние ресурсы, офлайн он их не получит")
	}
	if !strings.Contains(got, "prefers-color-scheme") {
		t.Error("нет тёмной темы")
	}
}

func TestHTMLEscapesTitles(t *testing.T) {
	b := sampleBook()
	b.Sections[0].Title = `Маркс & Энгельс <о> "капитале"`

	got := writeString(t, HTMLWriter{}, b)

	if strings.Contains(got, "<о>") {
		t.Error("угловые скобки в заголовке не экранированы")
	}
	if !strings.Contains(got, "&amp;") {
		t.Error("амперсанд в заголовке не экранирован")
	}
}

func TestHTMLIsReproducible(t *testing.T) {
	if writeString(t, HTMLWriter{}, sampleBook()) != writeString(t, HTMLWriter{}, sampleBook()) {
		t.Error("две выгрузки одной книги различаются побайтово")
	}
}

// BodyHTML обязана отдавать ровно то, что HTMLWriter кладёт между <body> и
// </body>. Это единственная страховка от расхождения двух представлений
// одного текста: выгрузки и страницы для краулера (internal/seo).
func TestBodyHTMLMatchesWriterBody(t *testing.T) {
	b := sampleBook()

	var buf bytes.Buffer
	if err := (HTMLWriter{}).Write(&buf, b); err != nil {
		t.Fatalf("HTMLWriter.Write: %v", err)
	}
	full := buf.String()

	const open, close = "<body>\n", "</body>\n"
	i := strings.Index(full, open)
	j := strings.Index(full, close)
	if i < 0 || j < 0 || j < i {
		t.Fatalf("в выходе писателя не нашлось тела между %q и %q", open, close)
	}
	want := full[i+len(open) : j]

	if got := BodyHTML(b); got != want {
		t.Errorf("BodyHTML разошлась с телом писателя\nполучено:\n%s\nожидалось:\n%s", got, want)
	}
}
