package book

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"
)

func TestFB2IsWellFormedXML(t *testing.T) {
	got := writeString(t, FB2Writer{}, sampleBook())

	if err := xml.Unmarshal([]byte(got), new(interface{})); err != nil {
		t.Fatalf("FB2 не разбирается как XML: %v", err)
	}
}

func TestFB2CarriesDescription(t *testing.T) {
	got := writeString(t, FB2Writer{}, sampleBook())

	var parsed struct {
		Description struct {
			TitleInfo struct {
				BookTitle string `xml:"book-title"`
				Lang      string `xml:"lang"`
				Authors   []struct {
					First string `xml:"first-name"`
					Last  string `xml:"last-name"`
				} `xml:"author"`
				Annotation struct {
					Paragraphs []string `xml:"p"`
				} `xml:"annotation"`
			} `xml:"title-info"`
			DocumentInfo struct {
				SrcURL string `xml:"src-url"`
				ID     string `xml:"id"`
			} `xml:"document-info"`
		} `xml:"description"`
	}
	if err := xml.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("разбор FB2: %v", err)
	}

	ti := parsed.Description.TitleInfo
	if ti.BookTitle != "Пробная книга" {
		t.Errorf("book-title = %q", ti.BookTitle)
	}
	if ti.Lang != "ru" {
		t.Errorf("lang = %q, хотел ru", ti.Lang)
	}
	if len(ti.Authors) != 1 || ti.Authors[0].Last != "Маркс" || ti.Authors[0].First != "К." {
		t.Errorf("автор разобран неверно: %+v", ti.Authors)
	}
	if len(ti.Annotation.Paragraphs) == 0 {
		t.Error("аннотация пуста — выходные данные потеряны")
	}
	if parsed.Description.DocumentInfo.SrcURL != "https://lib.example.org/works/1" {
		t.Errorf("src-url = %q", parsed.Description.DocumentInfo.SrcURL)
	}
	if parsed.Description.DocumentInfo.ID == "" {
		t.Error("пустой id документа")
	}
}

// Схема FB2 задаёт порядок document-info: author, program-used?, date,
// src-url*, src-ocr?, id, version. xml.Unmarshal в структуру этот порядок не
// проверяет — сериализация в правильные поля пройдёт, даже если элементы в
// файле идут не по схеме, — поэтому тест ищет позиции тегов в самой строке.
func TestFB2DocumentInfoElementOrder(t *testing.T) {
	got := writeString(t, FB2Writer{}, sampleBook())

	start := strings.Index(got, "<document-info>")
	end := strings.Index(got, "</document-info>")
	if start < 0 || end < 0 || end < start {
		t.Fatalf("нет document-info:\n%s", got)
	}
	section := got[start:end]

	authorAt := strings.Index(section, "<author>")
	dateAt := strings.Index(section, "<date ")
	srcURLAt := strings.Index(section, "<src-url>")
	idAt := strings.Index(section, "<id>")
	versionAt := strings.Index(section, "<version>")
	if authorAt < 0 || dateAt < 0 || srcURLAt < 0 || idAt < 0 || versionAt < 0 {
		t.Fatalf("не все элементы document-info найдены:\n%s", section)
	}
	if !(authorAt < dateAt && dateAt < srcURLAt && srcURLAt < idAt && idAt < versionAt) {
		t.Errorf("порядок элементов document-info нарушает схему FB2 (author, date, src-url, id, version):\n%s", section)
	}
}

// document-info/date обязателен по схеме FB2. При пустом Meta.Modified
// печатаем эпоху, а не пропускаем элемент.
func TestFB2DocumentInfoDateAlwaysPresent(t *testing.T) {
	b := sampleBook()
	b.Meta.Modified = time.Time{}

	got := writeString(t, FB2Writer{}, b)

	start := strings.Index(got, "<document-info>")
	end := strings.Index(got, "</document-info>")
	if start < 0 || end < 0 || end < start {
		t.Fatalf("нет document-info:\n%s", got)
	}
	if !strings.Contains(got[start:end], "<date value=") {
		t.Errorf("document-info требует date, а его нет при пустом Meta.Modified:\n%s", got[start:end])
	}
}

// title-info требует author+ (хотя бы один). Подборки (Task 10) могут
// отдать пустой список авторов.
func TestFB2TitleInfoRequiresAuthor(t *testing.T) {
	b := sampleBook()
	b.Meta.Authors = nil

	got := writeString(t, FB2Writer{}, b)

	start := strings.Index(got, "<title-info>")
	end := strings.Index(got, "</title-info>")
	if start < 0 || end < 0 || end < start {
		t.Fatalf("нет title-info:\n%s", got)
	}
	if !strings.Contains(got[start:end], "<author>") {
		t.Errorf("title-info требует author+, а элемента нет при пустом Meta.Authors:\n%s", got[start:end])
	}
}

func TestFB2NestsSections(t *testing.T) {
	got := writeString(t, FB2Writer{}, sampleBook())

	firstAt := strings.Index(got, "Часть первая")
	chapterAt := strings.Index(got, "Глава первая")
	secondAt := strings.Index(got, "Часть вторая")
	if firstAt < 0 || chapterAt < 0 || secondAt < 0 {
		t.Fatalf("секции потеряны:\n%s", got)
	}
	if !(firstAt < chapterAt && chapterAt < secondAt) {
		t.Error("порядок секций нарушен: вложенная глава должна идти внутри своей части")
	}
}

// Секция без своей главы (дыра в оглавлении, см. internal/api.topLevelNodes)
// получает от источника честное имя — <title><p> не должен остаться пустым.
func TestFB2RendersUntitledSectionLabel(t *testing.T) {
	got := writeString(t, FB2Writer{}, untitledSectionBook())

	if !strings.Contains(got, "<title><p>"+untitledSectionTitle+"</p></title>") {
		t.Errorf("в FB2 нет заголовка безымянной секции %q:\n%s", untitledSectionTitle, got)
	}
	if strings.Contains(got, "<title><p></p></title>") {
		t.Errorf("пустой заголовок секции в FB2:\n%s", got)
	}
}

func TestFB2MarksPrintedPages(t *testing.T) {
	got := writeString(t, FB2Writer{}, sampleBook())

	if !strings.Contains(got, "<subtitle>[101]</subtitle>") {
		t.Errorf("нет маркера печатной страницы:\n%s", got)
	}
}

// Каждая ссылка type="note" обязана находить свою секцию в body name="notes".
// Висячая сноска роняет читалки молча.
func TestFB2NoteLinksResolve(t *testing.T) {
	b := sampleBook()
	b.Sections[0].Blocks[0].Pages[0].HTML =
		`<p>Текст<sup class="footnote-ref" id="fnref:1-1"><a href="#fn:1-1">1</a></sup>.</p>`
	b.Sections[0].NotesHTML =
		`<div class="footnotes"><ul><li id="fn:1-1" class="fn-item">тело примечания</li></ul></div>`

	got := writeString(t, FB2Writer{}, b)

	var parsed struct {
		Bodies []struct {
			Name     string `xml:"name,attr"`
			Sections []struct {
				ID string `xml:"id,attr"`
			} `xml:"section"`
		} `xml:"body"`
	}
	if err := xml.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("разбор FB2: %v", err)
	}

	noteIDs := map[string]bool{}
	for _, body := range parsed.Bodies {
		if body.Name != "notes" {
			continue
		}
		for _, s := range body.Sections {
			noteIDs[s.ID] = true
		}
	}
	if len(noteIDs) == 0 {
		t.Fatalf("нет body name=\"notes\":\n%s", got)
	}

	for _, ref := range noteRefTargets(got) {
		if !noteIDs[ref] {
			t.Errorf("ссылка type=\"note\" на %q не находит секции примечания", ref)
		}
	}
}

// noteRefTargets вытаскивает цели всех ссылок type="note".
func noteRefTargets(fb2 string) []string {
	var out []string
	rest := fb2
	for {
		i := strings.Index(rest, `type="note"`)
		if i < 0 {
			return out
		}
		tagStart := strings.LastIndex(rest[:i], "<")
		tag := rest[tagStart : i+len(`type="note"`)]
		if j := strings.Index(tag, `l:href="#`); j >= 0 {
			target := tag[j+len(`l:href="#`):]
			if k := strings.Index(target, `"`); k >= 0 {
				out = append(out, target[:k])
			}
		}
		rest = rest[i+len(`type="note"`):]
	}
}

// Двоеточие в id зарезервировано под пространства имён XML. CollectPages
// выдаёт якоря вида fn:1-1, и часть читалок на них спотыкается.
func TestFB2StripsColonsFromIDs(t *testing.T) {
	b := sampleBook()
	b.Sections[0].NotesHTML =
		`<div class="footnotes"><ul><li id="fn:1-1" class="fn-item">тело</li></ul></div>`

	got := writeString(t, FB2Writer{}, b)

	if strings.Contains(got, `id="fn:1-1"`) {
		t.Error("двоеточие уцелело в id примечания")
	}
	if !strings.Contains(got, `id="fn_1-1"`) {
		t.Errorf("нет вычищенного id:\n%s", got)
	}
}

// Стихи в корпусе размечаются переводом строки через `\`, который рендерится
// в <br/>. Склейка строк пробелом превратила бы стихи в прозу.
func TestFB2BreaksParagraphOnLineBreak(t *testing.T) {
	b := sampleBook()
	b.Sections[0].Blocks[0].Pages[0].HTML = `<p>Первая строка<br/>Вторая строка</p>`

	got := writeString(t, FB2Writer{}, b)

	if !strings.Contains(got, "<p>Первая строка</p>") {
		t.Errorf("первая строка стиха не стала своим абзацем:\n%s", got)
	}
	if !strings.Contains(got, "<p>Вторая строка</p>") {
		t.Errorf("вторая строка стиха не стала своим абзацем:\n%s", got)
	}
}

func TestFB2MapsInlineElements(t *testing.T) {
	b := sampleBook()
	b.Sections[0].Blocks[0].Pages[0].HTML =
		`<p><em>курсив</em> <strong>жирно</strong> <sup>верх</sup> <sub>низ</sub></p>` +
			`<blockquote><p>цитата</p></blockquote>`

	got := writeString(t, FB2Writer{}, b)

	for _, want := range []string{
		"<emphasis>курсив</emphasis>",
		"<strong>жирно</strong>",
		"<sup>верх</sup>",
		"<sub>низ</sub>",
		"<cite>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("нет %q:\n%s", want, got)
		}
	}
}

// Честная потеря: FB2 не знает списков, они уезжают в абзацы с маркером.
func TestFB2FlattensLists(t *testing.T) {
	b := sampleBook()
	b.Sections[0].Blocks[0].Pages[0].HTML = `<ul><li>первый</li><li>второй</li></ul>`

	got := writeString(t, FB2Writer{}, b)

	if !strings.Contains(got, "• первый") || !strings.Contains(got, "• второй") {
		t.Errorf("список не разложен в абзацы:\n%s", got)
	}
	if strings.Contains(got, "<ul>") || strings.Contains(got, "<li>") {
		t.Errorf("теги списка уцелели, FB2 их не знает:\n%s", got)
	}
}

// Вложенный список — не просто потеря структуры (это допустимо по спеке),
// а раньше терял и разделитель между текстом пункта и текстом вложенного:
// "раз" + "вложенный" склеивались в "развложенный". Каждый уровень обязан
// стать своим абзацем со своим маркером.
func TestFB2SeparatesNestedListItems(t *testing.T) {
	b := sampleBook()
	b.Sections[0].Blocks[0].Pages[0].HTML = `<ul><li>раз<ul><li>вложенный</li></ul></li></ul>`

	got := writeString(t, FB2Writer{}, b)

	if !strings.Contains(got, "<p>• раз</p>") {
		t.Errorf("текст внешнего пункта не стал своим абзацем:\n%s", got)
	}
	if !strings.Contains(got, "<p>• вложенный</p>") {
		t.Errorf("вложенный пункт не стал своим абзацем со своим маркером:\n%s", got)
	}
	if strings.Contains(got, "развложенный") {
		t.Errorf("текст пунктов списка склеился без разделителя:\n%s", got)
	}
}

func TestFB2DropsImages(t *testing.T) {
	b := sampleBook()
	b.Sections[0].Blocks[0].Pages[0].HTML = `<p>до<img src="x.png" alt="скан"/>после</p>`

	got := writeString(t, FB2Writer{}, b)

	if strings.Contains(got, "x.png") {
		t.Errorf("картинка не выброшена — файла в архиве нет, ссылка повиснет:\n%s", got)
	}
}

func TestFB2IsReproducible(t *testing.T) {
	if writeString(t, FB2Writer{}, sampleBook()) != writeString(t, FB2Writer{}, sampleBook()) {
		t.Error("две сборки одной книги различаются побайтово")
	}
}

// TestFB2NoteTitleIsReaderMarkerForEndnote проверяет, что заголовок секции
// примечания — маркер, который читатель видел и нажал в тексте (книжный
// номер сноски), а не внутренний якорь. sampleBook() несёт ровно такое
// примечание: id="fn:1-1" (после диамбигуации якорь мог бы стать
// "fn:s0-1-1" и подобным — заголовком от этого быть не должно), а
// a.fn-back — "1".
func TestFB2NoteTitleIsReaderMarkerForEndnote(t *testing.T) {
	got := writeString(t, FB2Writer{}, sampleBook())

	if !strings.Contains(got, "<title><p>1</p></title>\n<p>примечание</p>") {
		t.Errorf("заголовок секции примечания — не маркер обратной ссылки:\n%s", got)
	}
	if strings.Contains(got, "<title><p>1-1</p></title>") {
		t.Errorf("заголовок секции примечания остался внутренним якорем:\n%s", got)
	}
}

// TestFB2NoteTitleIsReaderMarkerForSubscript — то же для подстрочного
// примечания: маркер там не книжный номер, а «бегущий» (1), (2) в пределах
// страницы (Note.Marker, pkg/markdown/notes.go).
func TestFB2NoteTitleIsReaderMarkerForSubscript(t *testing.T) {
	b := sampleBook()
	b.Sections[0].NotesHTML = `<div class="footnotes">` +
		`<section class="notes-group notes-group--subscript">` +
		`<h2>Подстрочные примечания</h2><ul class="fn-list">` +
		`<li id="fn:s0-2-1" class="fn-item fn-item--subscript">` +
		`<a class="fn-back" href="#fnref:s0-2-1">(2)</a> перевод редактора</li>` +
		`</ul></section></div>`

	got := writeString(t, FB2Writer{}, b)

	if !strings.Contains(got, "<title><p>(2)</p></title>\n<p>перевод редактора</p>") {
		t.Errorf("заголовок секции подстрочного примечания — не маркер (2):\n%s", got)
	}
}

// TestFB2NoteWithoutBackLinkHasNoTitle — старая или упрощённая разметка без
// a.fn-back не даёт маркера восстановиться. title у section — необязательный
// элемент (minOccurs="0" в схеме FB2), поэтому в этом случае секция
// примечания остаётся без заголовка, а не печатает внутренний якорь как
// заголовок.
func TestFB2NoteWithoutBackLinkHasNoTitle(t *testing.T) {
	b := sampleBook()
	b.Sections[0].NotesHTML =
		`<div class="footnotes"><ul><li id="fn:1-1" class="fn-item">тело примечания</li></ul></div>`

	got := writeString(t, FB2Writer{}, b)

	if strings.Contains(got, `<section id="fn_1-1"><title>`) {
		t.Errorf("секция примечания без маркера не должна получать заголовок:\n%s", got)
	}
	if !strings.Contains(got, `<section id="fn_1-1">`+"\n"+"<p>тело примечания</p>") {
		t.Errorf("тело примечания пропало вместе с заголовком:\n%s", got)
	}
}

// TestFB2NoteBodyKeepsSentenceIntactAcrossInlineSup — реальный случай с тома
// 23: тело примечания ссылается на другое примечание через <sup> посреди
// предложения (не через a.fn-back — та ссылка уже отфильтрована и проверена
// отдельно). Раньше noteBodyOf печатал каждый прямой узел <li> как свой
// блок — текст до <sup>, текст внутри <sup> и текст после расходились на три
// отдельных <p>, и голое число вроде «3» повисало отдельной строкой между
// половинками одного предложения. Должен остаться один <p> со ссылкой внутри
// него.
func TestFB2NoteBodyKeepsSentenceIntactAcrossInlineSup(t *testing.T) {
	b := sampleBook()
	b.Sections[0].NotesHTML = `<div class="footnotes"><ul>` +
		`<li id="fn:1-1" class="fn-item">` +
		`<a class="fn-back" href="#fnref:1-1">1</a> ` +
		`Начало предложения` +
		`<sup class="footnote-ref" id="fnref:1-2"><a href="#fn:1-2">3</a></sup>` +
		`, продолжение и конец.</li>` +
		`</ul></div>`

	got := writeString(t, FB2Writer{}, b)

	want := "<p>Начало предложения<sup><a l:href=\"#fn_1-2\" type=\"note\">3</a></sup>, продолжение и конец.</p>"
	if !strings.Contains(got, want) {
		t.Errorf("предложение с внутренней ссылкой распалось на несколько <p>:\nхотел подстроку: %s\nполучил:\n%s", want, got)
	}
	if strings.Count(got, "<p>3</p>") != 0 {
		t.Errorf("ссылка на другое примечание оторвалась в голый абзац с числом:\n%s", got)
	}
}

// TestFB2NoteBodyKeepsInlineMarkup — em/strong внутри тела примечания не
// должны провоцировать разрыв на несколько <p> и должны сохраниться как
// разметка, а не потеряться.
func TestFB2NoteBodyKeepsInlineMarkup(t *testing.T) {
	b := sampleBook()
	b.Sections[0].NotesHTML = `<div class="footnotes"><ul>` +
		`<li id="fn:1-1" class="fn-item">` +
		`<a class="fn-back" href="#fnref:1-1">1</a> ` +
		`Текст с <em>курсивом</em> и <strong>полужирным</strong> внутри.</li>` +
		`</ul></div>`

	got := writeString(t, FB2Writer{}, b)

	want := "<p>Текст с <emphasis>курсивом</emphasis> и <strong>полужирным</strong> внутри.</p>"
	if !strings.Contains(got, want) {
		t.Errorf("инлайновая разметка внутри примечания не сохранилась одним <p>:\nхотел подстроку: %s\nполучил:\n%s", want, got)
	}
}

// TestFB2NoteBodyKeepsNestedListAsOwnParagraphs — вложенный список внутри
// примечания (редкий, но встречается — см. схемную ревизию) не должен
// проглатываться в накопленный текст: он остаётся отдельными абзацами со
// своим маркером, как и везде в writeFB2ListItem/writeFB2Flow.
func TestFB2NoteBodyKeepsNestedListAsOwnParagraphs(t *testing.T) {
	b := sampleBook()
	b.Sections[0].NotesHTML = `<div class="footnotes"><ul>` +
		`<li id="fn:1-1" class="fn-item">` +
		`<a class="fn-back" href="#fnref:1-1">1</a> ` +
		`Вступление к перечню:` +
		`<ul><li>первый</li><li>второй</li></ul>` +
		`Заключение.</li>` +
		`</ul></div>`

	got := writeString(t, FB2Writer{}, b)

	for _, want := range []string{
		"<p>Вступление к перечню:</p>",
		"<p>• первый</p>",
		"<p>• второй</p>",
		"<p>Заключение.</p>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("не нашёл %q в:\n%s", want, got)
		}
	}
}

// TestFB2NoteBodyBreakOnLineBreak — <br/> внутри примечания по-прежнему режет
// абзац на несколько <p>, как и везде в writeFB2Paragraphs/writeFB2ListItem:
// фикс инлайнового накопления не должен был затронуть это поведение.
func TestFB2NoteBodyBreakOnLineBreak(t *testing.T) {
	b := sampleBook()
	b.Sections[0].NotesHTML = `<div class="footnotes"><ul>` +
		`<li id="fn:1-1" class="fn-item">` +
		`<a class="fn-back" href="#fnref:1-1">1</a> ` +
		`Первая строка.<br/>Вторая строка.</li>` +
		`</ul></div>`

	got := writeString(t, FB2Writer{}, b)

	if !strings.Contains(got, "<p>Первая строка.</p>\n<p>Вторая строка.</p>") {
		t.Errorf("<br/> в примечании перестал резать абзац:\n%s", got)
	}
}
