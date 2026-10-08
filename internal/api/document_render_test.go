package api

import (
	"regexp"
	"strings"
	"testing"

	"proofreader/internal/models"
	"proofreader/pkg/markdown"
)

func cutPage(id int64, number int, text string) *models.Page {
	return &models.Page{ID: id, WorkID: 1, PageNumber: number, ContentMarkdown: text}
}

// Вклейка набирается полосами, у каждой — своя печатная колонцифра в поле, и
// аппарат стоит ПОД вклейкой, а не внизу разбора.
func TestRenderCutCarriesPrintedFolioAndItsOwnNotes(t *testing.T) {
	cut := &models.DocumentCut{
		ID: 3, WorkID: ptrInt64(1),
		Anchor: models.Anchor{
			StartPageID: 10, StartOffset: 0,
			EndPageID: 10, EndOffset: len("Ленин писал[^r1]."),
		},
		Status: models.CutStatusOK,
	}
	pages := []*models.Page{cutPage(10, 4, "Ленин писал[^r1].\n\n[^r1]: примечание внизу полосы")}

	html := renderCut(markdown.NewRenderer(), cut, pages, 2, 232)

	if !strings.Contains(html, "data-cut-id=\"3\"") {
		t.Fatalf("вклейка без своего id: %s", html)
	}
	// Печатная колонцифра — подпись: 4 + 232. Адрес ссылки — номер полосы.
	if !strings.Contains(html, ">236<") {
		t.Fatalf("печатная колонцифра не напечатана: %s", html)
	}
	if !strings.Contains(html, "/works/1/pages/4") {
		t.Fatalf("ссылка в том ведёт не по номеру полосы: %s", html)
	}
	// Аппарат внутри блока вклейки, а не отдельно.
	if !strings.Contains(html, "примечание внизу полосы") {
		t.Fatalf("аппарат вклейки потерян: %s", html)
	}
	// Область имён — номер вклейки.
	if !strings.Contains(html, "fnref:v2-4-r1") {
		t.Fatalf("сноска вклейки без области имён: %s", html)
	}
}

// Срез из середины полосы дотягивает тело сноски, на которое ссылается, —
// иначе ссылка повисает и съедает текст в скобках.
func TestRenderCutPullsFootnoteBodyIntoSlice(t *testing.T) {
	const body = "начало полосы. Ленин писал[^r1]. конец полосы.\n\n[^r1]: тело примечания"
	cut := &models.DocumentCut{
		ID: 4, WorkID: ptrInt64(1),
		Anchor: models.Anchor{
			StartPageID: 10, StartOffset: len("начало полосы. "),
			EndPageID: 10, EndOffset: len("начало полосы. Ленин писал[^r1]."),
		},
		Status: models.CutStatusOK,
	}
	html := renderCut(markdown.NewRenderer(), cut, []*models.Page{cutPage(10, 4, body)}, 1, 0)

	if strings.Contains(html, "начало полосы") || strings.Contains(html, "конец полосы") {
		t.Fatalf("срез взял лишнее: %s", html)
	}
	if !strings.Contains(html, "тело примечания") {
		t.Fatalf("тело сноски не дотянулось в срез: %s", html)
	}
}

// Тело разбора называет вклейки отдельными строками; порядок вклеек — это
// порядок в тексте, он же область имён их сносок.
func TestCutPlaceholderIDsInTextOrder(t *testing.T) {
	body := "Начало.\n\n<cut id=\"7\">\n\nСередина.\n\n<cut id=\"3\">\n\nКонец."
	got := cutPlaceholderIDs(body)
	if len(got) != 2 || got[0] != 7 || got[1] != 3 {
		t.Fatalf("плейсхолдеры %v, ожидались [7 3]", got)
	}
}

// Два рендера, а не один: сноска автора и редакционная не сливаются в один
// список — иначе каждому приписывается авторство другого.
func TestAssembleKeepsAuthorNotesApartFromCorpus(t *testing.T) {
	cut := &models.DocumentCut{
		ID: 7, WorkID: ptrInt64(1),
		Anchor: models.Anchor{StartPageID: 10, StartOffset: 0, EndPageID: 10,
			EndOffset: len("Ленин писал[^566].")},
		Status: models.CutStatusOK, SourceTitle: "Ленин. Что делать?",
	}
	pages := []*models.Page{cutPage(10, 4, "Ленин писал[^566].\n\n[^566]: примечание ИМЛ")}

	body := "Моя мысль[^1].\n\n<cut id=\"7\">\n\n[^1]: примечание автора"
	html := assembleDocument(markdown.NewRenderer(), body,
		[]*models.DocumentCut{cut},
		map[int64][]*models.Page{7: pages},
		map[int64]int{1: 0})

	// Плейсхолдер не должен пережить сборку ни в открытой форме, ни висячим
	// закрывающим тегом, ни пустым <p></p> на его месте (Ruling из контекста
	// задачи 7, пункт 6.2: брифовый регэксп снимал только "<cut id" и
	// оставлял "</cut>" — тест на этом дефекте остался бы зелёным).
	if strings.Contains(html, "<cut id") {
		t.Fatalf("плейсхолдер остался в вёрстке: %s", html)
	}
	if strings.Contains(html, "</cut>") {
		t.Fatalf("закрывающий тег плейсхолдера остался в вёрстке: %s", html)
	}
	if strings.Contains(html, "<p></p>") {
		t.Fatalf("на месте вклейки остался пустой абзац: %s", html)
	}
	if !strings.Contains(html, "примечание автора") || !strings.Contains(html, "примечание ИМЛ") {
		t.Fatalf("потеряна одна из двух сносок: %s", html)
	}
	// Ruling R3 (контекст задачи 7, пункт 5): проверка вида !A && !B из брифа
	// зелена всегда, когда истинно B, — почти ничего не сторожит. Пара
	// однозначных утверждений вместо неё: блок автора НЕ содержит
	// примечание корпуса, блок вклейки его СОДЕРЖИТ.
	authorBlock := html[strings.Index(html, "document-author-notes"):]
	if strings.Contains(authorBlock, "примечание ИМЛ") {
		t.Fatalf("примечание корпуса попало в блок автора: %s", html)
	}
	cutBlock := html[strings.Index(html, `document-cut"`):strings.Index(html, "document-author-notes")]
	if !strings.Contains(cutBlock, "примечание ИМЛ") {
		t.Fatalf("примечание корпуса ушло из блока вклейки: %s", html)
	}
	if strings.Contains(html, "fn:a-0-566") {
		t.Fatalf("редакционное примечание попало в область автора: %s", html)
	}
}

// Отвязавшаяся вклейка не показывает прежний срез: смещения уехали, и по ним
// вырежется чужой кусок.
func TestAssembleShowsStaleCutWithoutText(t *testing.T) {
	cut := &models.DocumentCut{
		ID: 7, WorkID: ptrInt64(1),
		Anchor: models.Anchor{StartPageID: 10, StartOffset: 0, EndPageID: 10, EndOffset: 5},
		Status: models.CutStatusStale, SourceTitle: "Ленин. Что делать? // ПСС, т. 6, с. 236",
	}
	pages := []*models.Page{cutPage(10, 4, "совершенно другой текст полосы")}
	html := assembleDocument(markdown.NewRenderer(), "<cut id=\"7\">",
		[]*models.DocumentCut{cut}, map[int64][]*models.Page{7: pages}, map[int64]int{1: 0})

	if strings.Contains(html, "другой текст") {
		t.Fatalf("отвязавшаяся вклейка показала текст по уехавшим смещениям: %s", html)
	}
	if !strings.Contains(html, "document-cut--stale") || !strings.Contains(html, "Что делать?") {
		t.Fatalf("отвязавшаяся вклейка не названа: %s", html)
	}
	if !strings.Contains(html, "/works/1/pages/4") {
		t.Fatalf("у отвязавшейся вклейки нет пути в том: %s", html)
	}
}

// Длинная вклейка не едет в браузер целиком: макет держал 16 полос в DOM
// вместо 47, и это половина того, ради чего подрезка придумана.
func TestRenderCutTrimsLongCut(t *testing.T) {
	long := strings.Repeat("Очень длинный абзац корпуса. ", 700) // 20 300 знаков, заведомо больше порога 14000
	cut := &models.DocumentCut{
		ID: 9, WorkID: ptrInt64(1),
		Anchor: models.Anchor{StartPageID: 10, StartOffset: 0, EndPageID: 11, EndOffset: 10},
		Status: models.CutStatusOK, SourceTitle: "источник",
	}
	pages := []*models.Page{cutPage(10, 4, long), cutPage(11, 5, long)}

	trimmed := renderCutTrimmed(markdown.NewRenderer(), cut, pages, 1, 0)
	full := renderCut(markdown.NewRenderer(), cut, pages, 1, 0)

	if len(trimmed) >= len(full) {
		t.Fatalf("подрезка не укоротила вклейку: %d против %d", len(trimmed), len(full))
	}
	if !strings.Contains(trimmed, "document-cut--trimmed") {
		t.Fatalf("подрезанная вклейка не помечена: %s", trimmed[:200])
	}
	if !strings.Contains(trimmed, "Развернуть здесь") {
		t.Fatalf("у подрезанной вклейки нет пути к остатку")
	}
}

// Короткая вклейка не подрезается и кнопки не получает.
func TestRenderCutKeepsShortCutWhole(t *testing.T) {
	cut := &models.DocumentCut{
		ID: 9, WorkID: ptrInt64(1),
		Anchor: models.Anchor{StartPageID: 10, StartOffset: 0, EndPageID: 10, EndOffset: 20},
		Status: models.CutStatusOK, SourceTitle: "источник",
	}
	html := renderCutTrimmed(markdown.NewRenderer(), cut,
		[]*models.Page{cutPage(10, 4, "короткий абзац корпуса")}, 1, 0)
	if strings.Contains(html, "document-cut--trimmed") || strings.Contains(html, "Развернуть здесь") {
		t.Fatalf("короткая вклейка подрезана зря: %s", html)
	}
}

// Порядок операций из контекста задачи 8: у длинной вклейки ссылка на сноску
// стоит в показываемой (первой) части среза, а тело этой сноски лежит внизу
// полосы, за порогом обрезки. Обрезать надо ДО дотягивания тела сноски, а
// дотягивать — уже по обрезанному куску: withFootnoteDefs дописывает тело в
// КОНЕЦ среза, и обрезка готового (уже дотянутого) среза снесла бы именно
// его. Фикстура TestRenderCutTrimsLongCut этот порядок не ловит — сносок в
// ней нет вовсе.
func TestRenderCutTrimPreservesFootnoteBodyAcrossThreshold(t *testing.T) {
	filler := strings.Repeat("длинный текст корпуса без пустых строк. ", 500) // 20 000 знаков, >> порога 14000
	body := "Ленин писал[^r1]. " + filler + "\n\n[^r1]: тело сноски за порогом обрезки"
	cut := &models.DocumentCut{
		ID: 11, WorkID: ptrInt64(1),
		Anchor: models.Anchor{StartPageID: 10, StartOffset: 0, EndPageID: 10, EndOffset: len(body)},
		Status: models.CutStatusOK, SourceTitle: "источник",
	}
	pages := []*models.Page{cutPage(10, 4, body)}

	html := renderCutTrimmed(markdown.NewRenderer(), cut, pages, 1, 0)

	if !strings.Contains(html, "document-cut--trimmed") {
		t.Fatalf("вклейка не подрезана вовсе — фикстура не проверяет порядок операций: %s", html[:200])
	}
	if !strings.Contains(html, "тело сноски за порогом обрезки") {
		t.Fatalf("тело сноски потеряно при обрезке — дотягивание случилось раньше подрезки: %s", html)
	}
}

// Битой вклейке идти некуда: полос нет, ссылки нет, есть снимок подписи.
func TestAssembleShowsBrokenCutWithoutLink(t *testing.T) {
	cut := &models.DocumentCut{
		ID: 7, WorkID: nil,
		Anchor: models.Anchor{},
		Status: models.CutStatusOK, SourceTitle: "Ленин. Что делать? // ПСС, т. 6, с. 236",
	}
	html := assembleDocument(markdown.NewRenderer(), "<cut id=\"7\">",
		[]*models.DocumentCut{cut}, map[int64][]*models.Page{}, map[int64]int{})

	if !strings.Contains(html, "document-cut--broken") || !strings.Contains(html, "Что делать?") {
		t.Fatalf("битая вклейка не показана снимком: %s", html)
	}
	if strings.Contains(html, "<a ") {
		t.Fatalf("у битой вклейки есть ссылка, вести ей некуда: %s", html)
	}
}

// I3: вклейки разных работ не должны делить область имён сносок. Совпадение
// печатного номера полосы (12) в двух разных томах, каждая со своей
// сноской [^r1], — ровно тот случай, где общая область имён столкнула бы
// id="fnref:…" двух вклеек в одном DOM, и клик по одной сноске прокрутил
// бы к чужой.
//
// Этот сторож стоит именно на уровне assembleDocument, а не
// pkg/markdown.CollectPagesScoped: соседний тест той функции
// (TestCollectPagesScopedSeparatesSamePageNumbers) зовёт её НАПРЯМУЮ с
// руками заданными "v1-"/"v2-" — то есть проверяет уровень НИЖЕ шва.
// Мутация присвоения `ordinal[id] = i + 1` на `ordinal[id] = 1` в
// assembleDocument (все вклейки получают одну область имён) проходит
// весь набор ./internal/api/ зелёным без этого теста — область имён,
// которую реально назначает assembleDocument по порядку тегов в теле,
// нигде здесь не проверялась.
func TestAssembleGivesEachCutItsOwnFootnoteNamespace(t *testing.T) {
	cut1 := &models.DocumentCut{
		ID: 1, WorkID: ptrInt64(1),
		Anchor: models.Anchor{StartPageID: 10, StartOffset: 0, EndPageID: 10,
			EndOffset: len("Текст первого тома[^r1].")},
		Status: models.CutStatusOK, SourceTitle: "Первый том",
	}
	cut2 := &models.DocumentCut{
		ID: 2, WorkID: ptrInt64(2),
		Anchor: models.Anchor{StartPageID: 20, StartOffset: 0, EndPageID: 20,
			EndOffset: len("Текст второго тома[^r1].")},
		Status: models.CutStatusOK, SourceTitle: "Второй том",
	}
	// Обе полосы под одним и тем же печатным номером 12, но в разных томах —
	// совпадение номера полосы и было бы источником столкновения, если бы
	// область имён не разводила вклейки.
	pages1 := []*models.Page{cutPage(10, 12, "Текст первого тома[^r1].\n\n[^r1]: сноска первого тома")}
	pages2 := []*models.Page{cutPage(20, 12, "Текст второго тома[^r1].\n\n[^r1]: сноска второго тома")}

	body := "Начало.\n\n<cut id=\"1\">\n\nСередина.\n\n<cut id=\"2\">\n\nКонец."
	html := assembleDocument(markdown.NewRenderer(), body,
		[]*models.DocumentCut{cut1, cut2},
		map[int64][]*models.Page{1: pages1, 2: pages2},
		map[int64]int{1: 0, 2: 0})

	fnrefRe := regexp.MustCompile(`id="fnref:([^"]+)"`)
	matches := fnrefRe.FindAllStringSubmatch(html, -1)
	if len(matches) != 2 {
		t.Fatalf("ожидались две ссылки на сноски (по одной на вклейку), получено %d: %s", len(matches), html)
	}
	if matches[0][1] == matches[1][1] {
		t.Fatalf("обе вклейки получили одинаковую область имён сносок %q — сноски столкнутся в DOM: %s",
			matches[0][1], html)
	}
	// Порядок в теле — он же номер области имён: первый тег даёт "v1-",
	// второй "v2-".
	if !strings.Contains(html, `fnref:v1-12-r1`) {
		t.Fatalf("у первой вклейки не та область имён (ожидалась v1-12-r1): %s", html)
	}
	if !strings.Contains(html, `fnref:v2-12-r1`) {
		t.Fatalf("у второй вклейки не та область имён (ожидалась v2-12-r1): %s", html)
	}

	// Отложенная находка задачи 8: догрузка остатка (маршрут Full) обязана
	// рендерить вклейку С ТЕМ ЖЕ ordinal, что назначила бы assembleDocument —
	// иначе у догруженного блока будет другой префикс сносок, чем у уже
	// показанного соседа. cutOrdinal — та же функция, которой пользуется
	// DocumentCutHandler.Full.
	ordinal2, named := cutOrdinal(body, cut2.ID)
	if !named {
		t.Fatal("cutOrdinal не нашёл вклейку, которую называет тело")
	}
	fullHTML := renderCut(markdown.NewRenderer(), cut2, pages2, ordinal2, 0)
	if !strings.Contains(fullHTML, `fnref:v2-12-r1`) {
		t.Fatalf("догрузка (Full) второй вклейки назначила бы другую область имён, чем полная сборка: %s", fullHTML)
	}
}
