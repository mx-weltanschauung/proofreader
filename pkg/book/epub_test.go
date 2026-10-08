package book

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"regexp"
	"strings"
	"testing"
)

func epubOf(t *testing.T, b *Book) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	if err := (EPUBWriter{}).Write(&buf, b); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("EPUB не открывается как zip: %v", err)
	}
	return r
}

func epubFile(t *testing.T, r *zip.Reader, name string) string {
	t.Helper()
	for _, f := range r.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("открыть %s: %v", name, err)
		}
		defer rc.Close()
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			t.Fatalf("прочитать %s: %v", name, err)
		}
		return buf.String()
	}
	t.Fatalf("в архиве нет %s", name)
	return ""
}

// Первый подвох: mimetype обязан быть первой записью и лежать без сжатия.
// Иначе часть читалок отказывается открывать файл — при полностью валидном zip.
func TestEPUBMimetypeIsFirstAndStored(t *testing.T) {
	r := epubOf(t, sampleBook())

	if len(r.File) == 0 {
		t.Fatal("архив пуст")
	}
	first := r.File[0]
	if first.Name != "mimetype" {
		t.Errorf("первая запись = %q, хотел mimetype", first.Name)
	}
	if first.Method != zip.Store {
		t.Errorf("mimetype сжат (method=%d), хотел Store", first.Method)
	}
	if got := epubFile(t, r, "mimetype"); got != "application/epub+zip" {
		t.Errorf("mimetype = %q", got)
	}
}

func TestEPUBHasRequiredParts(t *testing.T) {
	r := epubOf(t, sampleBook())

	for _, name := range []string{
		"META-INF/container.xml",
		"OEBPS/content.opf",
		"OEBPS/nav.xhtml",
		"OEBPS/toc.ncx",
		"OEBPS/style.css",
		"OEBPS/title.xhtml",
	} {
		epubFile(t, r, name) // падает сам, если файла нет
	}
}

// Второй подвох: один XHTML на верхнеуровневую секцию. Том на 907 страниц
// одним файлом кладёт читалки на телефоне.
func TestEPUBSplitsTopLevelSectionsIntoFiles(t *testing.T) {
	r := epubOf(t, sampleBook())

	first := epubFile(t, r, "OEBPS/sec-1.xhtml")
	second := epubFile(t, r, "OEBPS/sec-2.xhtml")

	if !strings.Contains(first, "Часть первая") {
		t.Error("первая секция не в своём файле")
	}
	if strings.Contains(first, "Часть вторая") {
		t.Error("вторая секция попала в файл первой — книга не разбита")
	}
	if !strings.Contains(second, "Часть вторая") {
		t.Error("вторая секция не в своём файле")
	}
	// Вложенная глава остаётся внутри файла своей верхнеуровневой секции.
	if !strings.Contains(first, "Глава первая") {
		t.Error("вложенная глава потеряна")
	}
}

// Третий подвох: dcterms:modified обязателен по спеке EPUB 3. Берётся
// max(updated_at) страниц, не время сборки — иначе теряется воспроизводимость.
func TestEPUBDeclaresModifiedFromContent(t *testing.T) {
	opf := epubFile(t, epubOf(t, sampleBook()), "OEBPS/content.opf")

	if !strings.Contains(opf, `property="dcterms:modified"`) {
		t.Fatal("нет dcterms:modified — читалки по спеке вправе отвергнуть книгу")
	}
	if !strings.Contains(opf, "2026-08-14T00:00:00Z") {
		t.Errorf("dcterms:modified не из max(updated_at):\n%s", opf)
	}
}

// dc:source склеивает Edition и Volume той же функцией, что и титул
// (joinSourceParts, title.go). 21 из 22 работ живой базы хранят издание,
// уже оканчивающееся точкой ("...Сочинения, 2-е изд.") — наивная склейка
// "Edition + ". " + Volume" даёт двойную точку.
func TestEPUBSourceAvoidsDoubledPeriod(t *testing.T) {
	b := sampleBook()
	b.Meta.Edition = "К. Маркс и Ф. Энгельс. Сочинения, 2-е изд."
	b.Meta.Volume = "Том 23"

	opf := epubFile(t, epubOf(t, b), "OEBPS/content.opf")

	if strings.Contains(opf, "изд.. Том") {
		t.Errorf("dc:source содержит двойную точку:\n%s", opf)
	}
	if !strings.Contains(opf, "<dc:source>К. Маркс и Ф. Энгельс. Сочинения, 2-е изд. Том 23</dc:source>") {
		t.Errorf("dc:source собран неверно:\n%s", opf)
	}
}

// Работа с изданием без номера тома — легальное состояние живой базы.
// Наивная склейка "Edition + ". " + Volume" при пустом Volume оставляет
// висячее ". " на конце.
func TestEPUBSourceOmitsDanglingSeparatorWithoutVolume(t *testing.T) {
	b := sampleBook()
	b.Meta.Edition = "Сочинения"
	b.Meta.Volume = ""

	opf := epubFile(t, epubOf(t, b), "OEBPS/content.opf")

	if strings.Contains(opf, "Сочинения. <") || strings.Contains(opf, "Сочинения.<") {
		t.Errorf("dc:source оставляет висячий разделитель без тома:\n%s", opf)
	}
	if !strings.Contains(opf, "<dc:source>Сочинения</dc:source>") {
		t.Errorf("dc:source собран неверно:\n%s", opf)
	}
}

func TestEPUBManifestListsEverySectionFile(t *testing.T) {
	r := epubOf(t, sampleBook())
	opf := epubFile(t, r, "OEBPS/content.opf")

	var parsed struct {
		Manifest struct {
			Items []struct {
				Href string `xml:"href,attr"`
				ID   string `xml:"id,attr"`
			} `xml:"item"`
		} `xml:"manifest"`
		Spine struct {
			Refs []struct {
				IDRef string `xml:"idref,attr"`
			} `xml:"itemref"`
		} `xml:"spine"`
	}
	if err := xml.Unmarshal([]byte(opf), &parsed); err != nil {
		t.Fatalf("content.opf не разбирается: %v", err)
	}

	inManifest := map[string]bool{}
	ids := map[string]bool{}
	for _, it := range parsed.Manifest.Items {
		inManifest[it.Href] = true
		ids[it.ID] = true
	}
	for _, href := range []string{"nav.xhtml", "toc.ncx", "style.css", "title.xhtml", "sec-1.xhtml", "sec-2.xhtml"} {
		if !inManifest[href] {
			t.Errorf("в манифесте нет %s", href)
		}
	}
	if len(parsed.Spine.Refs) < 3 {
		t.Errorf("в spine %d записей, хотел титул и две секции", len(parsed.Spine.Refs))
	}
	for _, ref := range parsed.Spine.Refs {
		if !ids[ref.IDRef] {
			t.Errorf("spine ссылается на idref=%q, которого нет в манифесте", ref.IDRef)
		}
	}
}

func TestEPUBSectionFilesAreWellFormedXML(t *testing.T) {
	r := epubOf(t, sampleBook())

	for _, name := range []string{"OEBPS/title.xhtml", "OEBPS/nav.xhtml", "OEBPS/sec-1.xhtml", "OEBPS/sec-2.xhtml"} {
		content := epubFile(t, r, name)
		if err := xml.Unmarshal([]byte(content), new(interface{})); err != nil {
			t.Errorf("%s не разбирается как XML: %v", name, err)
		}
		// Обёртка постраничного рендера обязана быть снята ещё в CollectPages
		// (pkg/markdown) — Page.HTML приезжает сюда уже фрагментом.
		if strings.Contains(content, "<!DOCTYPE html PUBLIC") || strings.Contains(content, "GENERATOR") {
			t.Errorf("%s содержит обёртку постраничного документа", name)
		}
	}
}

// navHrefRe ловит ссылки оглавления вида <a href="...">Заголовок</a> — как в
// nav.xhtml, так и переиспользуется для сверки с toc.ncx ниже.
var navHrefRe = regexp.MustCompile(`<a href="([^"]+)">([^<]+)</a>`)

// Проверка структурная, не по подстроке: у вложенного пункта («Глава
// первая») href обязан нести фрагмент (#sec-1-1), и в файле, на который он
// указывает, обязан быть элемент с таким id. До правки вложенные ссылки
// переиспользовали href родителя без фрагмента вовсе — 1260 вложенных глав
// корпуса вели туда же, куда и их часть.
func TestEPUBNavListsSections(t *testing.T) {
	r := epubOf(t, sampleBook())
	nav := epubFile(t, r, "OEBPS/nav.xhtml")

	for _, want := range []string{`epub:type="toc"`, "sec-1.xhtml", "sec-2.xhtml", "Часть первая", "Глава первая"} {
		if !strings.Contains(nav, want) {
			t.Errorf("в навигации нет %q", want)
		}
	}

	foundFragment := false
	for _, m := range navHrefRe.FindAllStringSubmatch(nav, -1) {
		href := m[1]
		file, frag, hasFrag := strings.Cut(href, "#")
		if !hasFrag {
			continue // ссылка на секцию верхнего уровня целиком — фрагмент не нужен
		}
		foundFragment = true
		content := epubFile(t, r, "OEBPS/"+file)
		if !strings.Contains(content, `id="`+frag+`"`) {
			t.Errorf("nav.xhtml ссылается на %s, но в %s нет id=%q — ссылка ведёт в никуда", href, file, frag)
		}
	}
	if !foundFragment {
		t.Fatal("в nav.xhtml нет ни одной ссылки на вложенную секцию (с фрагментом) — тест ничего не проверил")
	}
}

// Секция без своей главы (дыра в оглавлении, см. internal/api.topLevelNodes)
// получает от источника честное имя — ни nav.xhtml (EPUB 3), ни toc.ncx
// (EPUB 2) не должны показать пустой пункт оглавления.
func TestEPUBRendersUntitledSectionLabel(t *testing.T) {
	b := untitledSectionBook()
	r := epubOf(t, b)
	nav := epubFile(t, r, "OEBPS/nav.xhtml")
	ncx := epubFile(t, r, "OEBPS/toc.ncx")

	if !strings.Contains(nav, ">"+untitledSectionTitle+"</a>") {
		t.Errorf("в nav.xhtml нет ссылки на безымянную секцию %q:\n%s", untitledSectionTitle, nav)
	}
	if strings.Contains(nav, `.xhtml"></a>`) {
		t.Errorf("пустая ссылка в nav.xhtml:\n%s", nav)
	}
	if !strings.Contains(ncx, ">"+untitledSectionTitle+"</text>") {
		t.Errorf("в toc.ncx нет пункта безымянной секции %q:\n%s", untitledSectionTitle, ncx)
	}
	if strings.Contains(ncx, "<text></text>") {
		t.Errorf("пустой пункт в toc.ncx:\n%s", ncx)
	}
}

// Оглавления EPUB 2 (toc.ncx) и EPUB 3 (nav.xhtml) — две проекции одного и
// того же дерева. Если они разойдутся, читалка без поддержки nav.xhtml
// покажет не то оглавление, что читалка с её поддержкой.
func TestEPUBNavAndNCXAgreeOnOrder(t *testing.T) {
	r := epubOf(t, sampleBook())
	nav := epubFile(t, r, "OEBPS/nav.xhtml")
	ncx := epubFile(t, r, "OEBPS/toc.ncx")

	var navTitles []string
	for _, m := range navHrefRe.FindAllStringSubmatch(nav, -1) {
		navTitles = append(navTitles, m[2])
	}

	ncxAll := regexp.MustCompile(`<text>([^<]+)</text>`).FindAllStringSubmatch(ncx, -1)
	if len(ncxAll) == 0 {
		t.Fatal("в toc.ncx нет ни одного <text>")
	}
	// Первый <text> — заголовок книги (docTitle), не пункт оглавления.
	var ncxTitles []string
	for _, m := range ncxAll[1:] {
		ncxTitles = append(ncxTitles, m[1])
	}

	if len(navTitles) != len(ncxTitles) {
		t.Fatalf("nav.xhtml перечисляет %d пунктов, toc.ncx — %d: две навигации разошлись\nnav: %v\nncx: %v",
			len(navTitles), len(ncxTitles), navTitles, ncxTitles)
	}
	for i := range navTitles {
		if navTitles[i] != ncxTitles[i] {
			t.Errorf("пункт %d: nav.xhtml = %q, toc.ncx = %q", i, navTitles[i], ncxTitles[i])
		}
	}
}

// Разметка всплывашки нужна с обеих сторон: epub:type="noteref" на
// ссылке-вызове в тексте и epub:type="footnote" на самом примечании. Читалка
// решает, показывать ли всплывашку, именно по ссылке — разметка одной только
// цели (как было раньше) ничего не меняет в поведении открытия сноски.
func TestEPUBMarksFootnotesForPopups(t *testing.T) {
	b := sampleBook()
	// sampleBook() несёт примечание (NotesHTML), но не ссылку-вызов в тексте —
	// добавляем её здесь, не трогая общий фикстур.
	b.Sections[0].Blocks[0].Pages[0].HTML = `<p>Текст со сноской` +
		`<sup class="footnote-ref footnote-ref--endnote" id="fnref:1-1">` +
		`<a href="#fn:1-1">1</a></sup>.</p>`

	first := epubFile(t, epubOf(t, b), "OEBPS/sec-1.xhtml")

	refRe := regexp.MustCompile(`<a epub:type="noteref" href="#([^"]+)">`)
	refs := refRe.FindAllStringSubmatch(first, -1)
	if len(refs) == 0 {
		t.Fatal(`ссылка-вызов не размечена epub:type="noteref" — читалка уведёт читателя в конец книги вместо всплывашки`)
	}
	for _, m := range refs {
		target := m[1]
		bodyRe := regexp.MustCompile(`id="` + regexp.QuoteMeta(target) + `"[^>]*epub:type="footnote"`)
		if !bodyRe.MatchString(first) {
			t.Errorf("ссылка ведёт на %q, но там нет epub:type=\"footnote\" — цель не резолвится как примечание", target)
		}
	}
}

func TestEPUBIsReproducible(t *testing.T) {
	var a, b bytes.Buffer
	if err := (EPUBWriter{}).Write(&a, sampleBook()); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := (EPUBWriter{}).Write(&b, sampleBook()); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("две сборки одной книги различаются побайтово")
	}
}

// epubFiles разбирает готовый EPUB в отображение «имя записи → содержимое».
func epubFiles(t *testing.T, b *Book) map[string]string {
	t.Helper()
	var buf bytes.Buffer
	if err := (EPUBWriter{}).Write(&buf, b); err != nil {
		t.Fatalf("EPUB: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	out := make(map[string]string, len(zr.File))
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		out[f.Name] = string(data)
	}
	return out
}

func TestEPUBОбъявляетMathMLТолькоГдеОнЕсть(t *testing.T) {
	// В sampleBook() две верхнеуровневые секции: «Часть первая» (sec-1) и
	// «Часть вторая» (sec-2). Формулу кладём только в первую.
	b := sampleBook()
	b.Sections[0].Blocks[0].Pages[0].HTML = mathPageHTML

	files := epubFiles(t, b)

	opf := files["OEBPS/content.opf"]
	if !strings.Contains(opf, `id="sec-1" href="sec-1.xhtml" media-type="application/xhtml+xml" properties="mathml"`) {
		t.Errorf("секция с формулой не помечена properties=\"mathml\":\n%s", opf)
	}
	if strings.Contains(opf, `id="sec-2" href="sec-2.xhtml" media-type="application/xhtml+xml" properties="mathml"`) {
		t.Error("секция без формул помечена properties=\"mathml\" — epubcheck считает это ошибкой")
	}
}
