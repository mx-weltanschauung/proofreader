package api

import (
	"context"
	"fmt"
	"hash/fnv"
	"html"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"proofreader/internal/models"
	"proofreader/pkg/markdown"
)

// cutScope — область имён сносок одной вклейки. Номер берётся из ПОРЯДКА В
// ТЕКСТЕ разбора, а не из id: id вклейки не виден читателю и меняется при
// пересоздании, а совпадение имени якоря с печатным номером примечания —
// то, на чём стоит решение о редакционных сносках.
func cutScope(ordinal int) string {
	return fmt.Sprintf("v%d-", ordinal)
}

// cutRawSlices — «сырые» (без дотягивания сносок) срезы КАЖДОЙ полосы
// вклейки по её границам, порядок соответствует pages. Общий первый шаг
// renderCut и renderCutTrimmed: подрезка длинной вклейки работает НАД этим
// результатом и обязана случиться раньше withFootnoteDefs (см. renderCutTrimmed).
func cutRawSlices(cut *models.DocumentCut, pages []*models.Page) []string {
	raw := make([]string, len(pages))
	for i, p := range pages {
		start, end := 0, len(p.ContentMarkdown)
		if p.ID == cut.StartPageID {
			start = cut.StartOffset
		}
		if p.ID == cut.EndPageID {
			end = cut.EndOffset
		}
		raw[i] = sliceCut(p.ContentMarkdown, start, end)
	}
	return raw
}

// renderCut собирает одну вклейку: строку источника, полосы среза с
// печатными колонцифрами в поле, аппарат под ними и ссылку в том.
//
// Аппарат нумеруется по самой вклейке: подстрочные — заново «(1)», «(2)»…
// (при счёте по разбору примечания к подрезанным полосам попали бы в список
// без текста, к которому относятся), редакционные держат печатный номер
// тома — это такая же печатная координата, как колонцифра.
//
// Broken() здесь не проверяется намеренно: решает вызывающий в задаче сборки
// целого разбора, а не эта функция — проверка в двух местах разъехалась бы.
func renderCut(r *markdown.Renderer, cut *models.DocumentCut, pages []*models.Page, ordinal, pageOffset int) string {
	raw := cutRawSlices(cut, pages)
	contents := make([]markdown.PageContent, 0, len(pages))
	for i, p := range pages {
		slice := withFootnoteDefs(raw[i], p.ContentMarkdown)
		contents = append(contents, markdown.PageContent{PageNumber: p.PageNumber, Content: slice})
	}

	pageHTML, notes := r.CollectPagesScoped(cutScope(ordinal), contents)

	var b strings.Builder
	fmt.Fprintf(&b, `<div class="document-cut" data-cut-id="%d">`, cut.ID)
	fmt.Fprintf(&b, `<p class="document-cut-source">%s</p>`, html.EscapeString(cut.SourceTitle))
	b.WriteString(`<div class="document-cut-body">`)
	for i, p := range pages {
		fmt.Fprintf(&b,
			`<div class="document-cut-page"><a class="document-cut-folio" href="/works/%d/pages/%d">%s</a>%s</div>`,
			*cut.WorkID, p.PageNumber, folioLabel(p.PageNumber, pageOffset), pageHTML[i])
	}
	b.WriteString(`</div>`)
	b.WriteString(markdown.RenderNotesWith(notes, markdown.NotesOptions{Inline: true}))
	fmt.Fprintf(&b,
		`<p class="document-cut-actions"><a href="/works/%d/pages/%d">Читать в томе</a></p>`,
		*cut.WorkID, pages[0].PageNumber)
	b.WriteString(`</div>`)
	return b.String()
}

// cutTrimChars — порог подрезки в ЗНАКАХ среза. В тикете 09 единицей была
// полоса, потому что меньше полосы вклейки не бывало; теперь бывает, и абзац
// порога не касается вовсе. Число измерено headless Chrome на живом корпусе
// (задача 13, том 177) настоящим renderCut и настоящей вёрсткой
// (documentCut.css/DocumentView.css) — цель «не больше ~10 экранов на
// 1440 px» и вывод: рабочее 4000 держало вклейку у 2.9 экрана, с запасом
// ниже цели, порог поднят до 14000. Протокол с парами «знаков → px»,
// гарнитурой и оговоркой про Georgia — docs/superpowers/plans/2026-09-19-razbor-vkleyka-trim-measurement.md.
const cutTrimChars = 14000

// trimToRunes отдаёт префикс s не длиннее n РУН, не разрезая символ пополам.
func trimToRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

// trimToParagraph обрезает raw до n знаков по границе абзаца: ищет последний
// пустой перенос строки внутри первых n знаков и режет по нему. Абзаца может
// не быть вовсе (машинный текст без пустых строк) — тогда режем ровно по
// границе символа на пороге, руну не разрезая.
func trimToParagraph(raw string, n int) string {
	prefix := trimToRunes(raw, n)
	if idx := strings.LastIndex(prefix, "\n\n"); idx >= 0 {
		return strings.TrimRight(raw[:idx], "\n")
	}
	return prefix
}

// trimCutRaw подрезает сырые (до withFootnoteDefs) срезы полос вклейки до
// threshold знаков суммарно.
//
// Правило дословно из задачи: полосы идут целиком, пока не превышен порог;
// как только превышен — обрыв ПЕРЕД этой полосой; если оборвать не на чем
// (первая же полоса длиннее порога) — обрезка внутри неё по границе абзаца.
// Второй результат — было ли что подрезать; короткая вклейка возвращает
// исходный срез без изменений.
func trimCutRaw(raw []string, threshold int) ([]string, bool) {
	total := 0
	for i, s := range raw {
		n := utf8.RuneCountInString(s)
		if total+n <= threshold {
			total += n
			continue
		}
		if i == 0 {
			return []string{trimToParagraph(s, threshold)}, true
		}
		return raw[:i], true
	}
	return raw, false
}

// renderCutTrimmed показывает первые cutTrimChars знаков среза, обрывая по
// границе полосы, а внутри первой полосы — по границе абзаца. Остаток
// догружается отдельным запросом (маршрут Full), а не едет в браузер
// спрятанным. Короткая вклейка (укладывается в порог целиком) возвращает
// renderCut без изменений и без пометки.
//
// Порядок операций на КАЖДОЙ полосе принципиален и НЕ произволен: сначала
// cutRawSlices (границы вклейки, sliceCut), потом обрезка по порогу, и
// только потом withFootnoteDefs — от уже обрезанного куска, вторым
// аргументом по-прежнему полный текст полосы (определения ищутся в нём, не
// в обрезке). withFootnoteDefs дописывает тела сносок В КОНЕЦ среза: сделай
// наоборот — обрежь готовый срез с уже приклеенными телами, — и обрезка
// снимет именно их, а голая ссылка в этом рендерере не молчит, а съедает
// текст в скобках (контекст задачи 8).
func renderCutTrimmed(r *markdown.Renderer, cut *models.DocumentCut, pages []*models.Page, ordinal, pageOffset int) string {
	raw := cutRawSlices(cut, pages)

	trimmedRaw, wasTrimmed := trimCutRaw(raw, cutTrimChars)
	if !wasTrimmed {
		return renderCut(r, cut, pages, ordinal, pageOffset)
	}
	usedPages := pages[:len(trimmedRaw)]

	contents := make([]markdown.PageContent, len(trimmedRaw))
	for i, s := range trimmedRaw {
		contents[i] = markdown.PageContent{
			PageNumber: usedPages[i].PageNumber,
			// Дотягивание — уже по обрезанному куску s, но по ПОЛНОМУ тексту
			// полосы для поиска определений: страница правится не целиком.
			Content: withFootnoteDefs(s, usedPages[i].ContentMarkdown),
		}
	}

	pageHTML, notes := r.CollectPagesScoped(cutScope(ordinal), contents)

	var b strings.Builder
	fmt.Fprintf(&b, `<div class="document-cut document-cut--trimmed" data-cut-id="%d">`, cut.ID)
	fmt.Fprintf(&b, `<p class="document-cut-source">%s</p>`, html.EscapeString(cut.SourceTitle))
	b.WriteString(`<div class="document-cut-body">`)
	for i, p := range usedPages {
		fmt.Fprintf(&b,
			`<div class="document-cut-page"><a class="document-cut-folio" href="/works/%d/pages/%d">%s</a>%s</div>`,
			*cut.WorkID, p.PageNumber, folioLabel(p.PageNumber, pageOffset), pageHTML[i])
	}
	b.WriteString(`</div>`)
	// Затухание — визуальный намёк, что вклейка обрезана, до кнопки.
	b.WriteString(`<div class="document-cut-fade"></div>`)
	b.WriteString(markdown.RenderNotesWith(notes, markdown.NotesOptions{Inline: true}))
	fmt.Fprintf(&b,
		`<p class="document-cut-actions"><a href="/works/%d/pages/%d">Читать в томе</a> `+
			`<button type="button" class="document-cut-expand" data-cut-id="%d">Развернуть здесь</button></p>`,
		*cut.WorkID, pages[0].PageNumber, cut.ID)
	b.WriteString(`</div>`)
	return b.String()
}

// folioLabel — ПОДПИСЬ полосы: печатная колонцифра. Адрес при этом остаётся
// номером полосы: три счёта разведены решением 15, и у передних листов они
// расходятся. Полоса без колонцифры (обложка, форзац) подписывается сквозным
// счётом — единственное место, где он вылезает наружу, и там он законен.
func folioLabel(pageNumber, pageOffset int) string {
	printed := pageNumber + pageOffset
	if printed < 1 {
		return fmt.Sprintf("б/н, полоса %d", pageNumber)
	}
	return fmt.Sprintf("%d", printed)
}

// cutTagLineRe матчит СТРОКУ (без окружающих пустых строк), состоящую ровно
// из одного тега `<cut id="…">` — возможные горизонтальные пробелы вокруг не
// мешают, а завершающий `\r` (CRLF) отрезается вызывающим до сверки с этим
// регэкспом, а не самим регэкспом.
var cutTagLineRe = regexp.MustCompile(`^[ \t]*<cut\s+id="(\d+)"\s*/?>[ \t]*$`)

// isBlankTextLine — пустая строка markdown: только пробелы/табуляции (и,
// возможно, унаследованный CRLF-хвост).
func isBlankTextLine(line string) bool {
	return strings.TrimSpace(strings.TrimSuffix(line, "\r")) == ""
}

// cutPlaceholderLines — общий разбор тела на плейсхолдеры: id вклеек в
// порядке появления и номера строк, на которых они стоят. Тегом-плейсхолдером
// считается только тег, стоящий ОТДЕЛЬНЫМ АБЗАЦЕМ: абзац в markdown задаёт
// пустая строка вокруг (или начало/конец тела), а не просто перевод строки.
// Тег на своей строке, но без пустой строки до или после, остаётся частью
// соседнего абзаца — gomarkdown проведёт его как встроенный HTML внутри <p>,
// и подстановка блока вклейки порвёт вёрстку (найдено мутацией при ревью
// ветки: построчный регэксп без проверки пустых строк считал такой тег
// блоком и пропускал сохранение).
//
// Разбор идёт по строкам, а не одним регэкспом на всё тело: RE2 (пакет
// regexp) не даёт lookahead/lookbehind, а последовательные плейсхолдеры
// делят одну и ту же разделяющую пустую строку — регэксп, поглощающий её
// матчем первого тега, оставил бы второй без «своей» пустой строки перед
// ним же. \r у каждой строки отрезается персонально, что делает разбор
// устойчивым и к CRLF, и к LF.
//
// Номера строк нужны маскировке (maskCutPlaceholders): обе стороны обязаны
// считать плейсхолдером одно и то же, иначе проверка на сохранении и подстановка
// на чтении разойдутся — и вклейка молча исчезнет из разбора.
func cutPlaceholderLines(lines []string) (ids []int64, at []int) {
	for i, line := range lines {
		trimmed := strings.TrimSuffix(line, "\r")
		m := cutTagLineRe.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}
		beforeOK := i == 0 || isBlankTextLine(lines[i-1])
		afterOK := i == len(lines)-1 || isBlankTextLine(lines[i+1])
		if !beforeOK || !afterOK {
			continue
		}
		id, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			continue
		}
		ids = append(ids, id)
		at = append(at, i)
	}
	return ids, at
}

// cutPlaceholderBlocks отдаёт id вклеек тела в порядке появления — те, что
// стоят собственным абзацем (см. cutPlaceholderLines).
func cutPlaceholderBlocks(body string) []int64 {
	ids, _ := cutPlaceholderLines(strings.Split(body, "\n"))
	return ids
}

// cutAnyTagRe матчит тег `<cut id="…">` ГДЕ УГОДНО в теле, без привязки к
// границам блока — противовес cutPlaceholderBlocks (задача 9, контекст п.1).
// Сравнение числа совпадений с cutPlaceholderBlocks на одном теле и ловит
// тег, не стоящий отдельным абзацем: у отдельно стоящего тега оба счёта
// совпадают, у любого другого — этот насчитает больше. Ловить такой тег на
// чтении поздно: подстановка блока вклейки в разорванную парсером строку
// рвёт вёрстку по построению — это не чинится в assembleDocument, только
// отказом на сохранении.
var cutAnyTagRe = regexp.MustCompile(`<cut\s+id="\d+"\s*/?>`)

// ──── Метка места вклейки ──────────────────────────────────────────────────
//
// Плейсхолдер `<cut id="N">` — САМ сырой HTML, и раньше подстановка искала
// его уже в отрисованном тексте (gomarkdown заворачивал строку в абзац и
// дописывал закрывающий тег: `<p><cut id="7"></cut></p>`). С подавлением
// исполняемого в тексте автора (C1, markdown.Renderer.ForUntrustedAuthor) этот
// путь перестаёт работать по построению: тег пропадает вместе с полезной
// нагрузкой, и вклейки молча исчезли бы из разбора.
//
// Поэтому плейсхолдер снимается ДО рендера (maskCutPlaceholders) и
// заменяется алфавитно-цифровой меткой, которую markdown проводит обычным
// текстом абзаца, а после рендера метка меняется на готовый блок вклейки.
// Метка не может быть подделана автором: её суффикс считается от самого тела
// и проверяется на отсутствие в нём (cutSlotNonce).

// cutSlotToken — метка i-го по счёту плейсхолдера. Только буквы и цифры:
// markdown такую строку не трогает ничем — ни ссылкой, ни выделением, ни
// заголовком, — и она честно приезжает абзацем `<p>метка</p>`.
func cutSlotToken(nonce string, i int) string {
	return "cutslot" + nonce + "x" + strconv.Itoa(i) + "z"
}

// cutSlotNonce подбирает суффикс метки так, чтобы её не было в самом теле.
// Считается от тела (а не от случайного источника) ради ДЕТЕРМИНИРОВАННОГО
// вывода: один и тот же разбор обязан собираться байт в байт одинаково —
// на это опираются и ETag, и золотые снимки тестов.
func cutSlotNonce(body string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(body))
	for {
		nonce := strconv.FormatUint(h.Sum64(), 36)
		if !strings.Contains(body, "cutslot"+nonce) {
			return nonce
		}
		_, _ = h.Write([]byte("x"))
	}
}

// maskCutPlaceholders меняет каждый тег-плейсхолдер на его метку и отдаёт id
// вклеек в том же порядке. Плейсхолдером считается ровно то же, что считает
// проверка на сохранении, — обе стороны зовут cutPlaceholderLines.
//
// Тег, НЕ стоящий отдельным абзацем, здесь не маскируется и после рендера
// исчезает вместе с прочим сырым HTML автора. Это не потеря: такое тело не
// проходит сохранение (validateCutPlaceholders отвергает его отдельным
// сообщением), а раньше подстановка в разорванную парсером строку рвала
// вёрстку — исчезнуть тише, чем сломать.
func maskCutPlaceholders(body, nonce string) (masked string, ids []int64) {
	lines := strings.Split(body, "\n")
	ids, at := cutPlaceholderLines(lines)
	for i, lineNo := range at {
		lines[lineNo] = cutSlotToken(nonce, i)
	}
	return strings.Join(lines, "\n"), ids
}

// cutSlotPattern — метка в уже отрисованном HTML вместе с абзацем, в который
// её завернул парсер. Абзац снимается вместе с меткой: блок вклейки — <div>,
// а <div> внутри <p> закрывает абзац по правилам разбора HTML и рвёт вёрстку.
// Группы вокруг необязательны — если парсер абзаца не поставил, они просто не
// находят совпадения и ничего лишнего не съедают.
func cutSlotPattern(token string) *regexp.Regexp {
	return regexp.MustCompile(`(?s)(?:<p>\s*)?` + regexp.QuoteMeta(token) + `(?:\s*</p>)?`)
}

// cutPlaceholderIDs отдаёт id вклеек в порядке их появления в теле — тех,
// что стоят собственным абзацем (см. cutPlaceholderBlocks). Повтор одного id
// встречается столько раз, сколько стоит в тексте: порядок тут важнее
// уникальности, а повтор отсекает вызывающий.
func cutPlaceholderIDs(body string) []int64 {
	return cutPlaceholderBlocks(body)
}

// assembleDocument собирает разбор ДВУМЯ рендерами.
//
// Текст автора рендерится сам по себе, его сноски собираются в свой блок с
// областью имён "a-" и сквозным счётом по разбору; каждая вклейка рендерится
// отдельно со своей областью; готовые блоки вставляются в отрисованный HTML
// на места плейсхолдеров. Парсер markdown корпусного HTML не касается — иначе
// сноски автора и тома попадают в один список, а это приписывает каждому
// авторство другого.
//
// Асимметрия двух рендеров НЕ случайна и держит C1: текст автора идёт через
// r.ForUntrustedAuthor() (разметку пишет вошедший читатель — подавляются
// сырой HTML, блочные атрибуты и небезопасные схемы ссылок), вклейка — через
// r как есть (корпус, чья ручная разметка законна и обязана доехать до
// читателя побайтово). Обе стороны сторожит
// internal/api/document_author_html_test.go.
func assembleDocument(
	r *markdown.Renderer,
	body string,
	cuts []*models.DocumentCut,
	cutPages map[int64][]*models.Page,
	offsets map[int64]int,
) string {
	byID := map[int64]*models.DocumentCut{}
	for _, c := range cuts {
		byID[c.ID] = c
	}

	// Плейсхолдеры снимаются ДО рендера: сам тег — сырой HTML, и подавление
	// авторского HTML унесло бы его вместе с полезной нагрузкой.
	nonce := cutSlotNonce(body)
	masked, ids := maskCutPlaceholders(body, nonce)

	// Порядок в тексте — он же область имён.
	ordinal := map[int64]int{}
	for i, id := range ids {
		if _, seen := ordinal[id]; !seen {
			ordinal[id] = i + 1
		}
	}

	authorHTML, authorNotes := r.ForUntrustedAuthor().CollectPagesScoped("a-", []markdown.PageContent{
		{PageNumber: 0, Content: masked},
	})

	out := authorHTML[0]
	for i, id := range ids {
		block := `<p class="document-cut document-cut--broken">Вклейка не найдена.</p>`
		if cut, ok := byID[id]; ok {
			block = renderCutState(r, cut, cutPages[id], ordinal[id], offsets)
		}
		// Плейсхолдер на исчезнувшую вклейку: сохранение такого тела не
		// пропускает проверка (задача 9), но рендер обязан не молчать.
		//
		// ReplaceAllLiteralString, а не ReplaceAllString: в блоке вклейки
		// живёт корпусный текст, и `$1` в нём иначе истолковался бы ссылкой
		// на группу и съел бы кусок вёрстки.
		out = cutSlotPattern(cutSlotToken(nonce, i)).ReplaceAllLiteralString(out, block)
	}

	if notes := markdown.RenderNotesWith(authorNotes, markdown.NotesOptions{}); notes != "" {
		out += `<section class="document-author-notes"><h2>Примечания автора</h2>` + notes + `</section>`
	}
	return out
}

// renderCutState печатает вклейку по её состоянию. Три состояния не должны
// выглядеть одинаково: у живой — набор с колонцифрами и своим аппаратом; у
// отвязавшейся идти есть куда (ссылка в том по последней известной полосе),
// хоть текста по уехавшим смещениям уже не показать; у битой — идти
// некуда, только снимок подписи.
//
// renderCut (задача 6) беззащитен НАМЕРЕННО: разыменовывает *cut.WorkID и
// берёт pages[0] без проверок, а отсекать оба случая — дело вызывающего,
// потому что проверка в двух местах разъехалась бы. Здесь она стоит ровно
// один раз, до вызова renderCut.
func renderCutState(r *markdown.Renderer, cut *models.DocumentCut, pages []*models.Page, ordinal int, offsets map[int64]int) string {
	if cut.Broken() || len(pages) == 0 {
		// Битая: источника нет (сняли том/полосы, либо обе), полос тоже нет —
		// вести некуда. Показываем только снимок подписи, без ссылки.
		return fmt.Sprintf(
			`<p class="document-cut document-cut--broken" data-cut-id="%d">%s</p>`,
			cut.ID, html.EscapeString(cut.SourceTitle))
	}
	if cut.Status == models.CutStatusStale {
		// Отвязавшаяся: якорь не нашёлся после правки полосы — прежний текст
		// не показываем (смещения уехали, по ним вырежется чужой кусок), но
		// путь в том ещё жив, ссылка ведёт на последнюю известную полосу.
		return fmt.Sprintf(
			`<p class="document-cut document-cut--stale" data-cut-id="%d">%s `+
				`<a href="/works/%d/pages/%d">Читать в томе</a></p>`,
			cut.ID, html.EscapeString(cut.SourceTitle), *cut.WorkID, pages[0].PageNumber)
	}
	return renderCutTrimmed(r, cut, pages, ordinal, offsets[*cut.WorkID])
}

// assembleDocumentFor собирает разбор целиком: читает вклейки, их полосы и
// смещения печатных колонцифр затронутых томов, после чего зовёт
// assembleDocument. Второй результат — сколько вклеек числится за разбором.
//
// Вынесена из DocumentHandler.View дословно, без правок поведения: с задачи 6
// у сборки два потребителя — живой маршрут /view и страница краулера
// (SEODocumentSource). Второй сборки заводить нельзя, и дело не в
// повторении кода: внутри assembleDocument текст автора идёт через
// r.ForUntrustedAuthor(), и всякий обход этой функции открыл бы те же четыре
// стока заново, только другой дверью — на этот раз в странице, которую
// читает поисковик и мессенджер модератора.
//
// d — строка, УЖЕ сведённая к редакции, которую вправе видеть пришедший
// (documentForViewer): тело берётся из d.MarkdownContent как есть, а решение
// «черновик или одобренное» принимается до вызова, а не внутри.
func assembleDocumentFor(
	ctx context.Context,
	r *markdown.Renderer,
	cutStore DocumentCutStore,
	works WorkStore,
	d *models.Document,
) (string, int, error) {
	// Разбор собирается здесь и сейчас, а не читается снимком: вклейка едет за
	// источником. Кэша у сборки нет намеренно — кэш глав
	// адресуется диапазоном полос, а разбор тянет полосы из разных томов.
	cuts, err := cutStore.ByDocument(ctx, d.ID)
	if err != nil {
		return "", 0, err
	}

	// Полосы каждой вклейки и смещение колонцифры её тома — два рендера
	// (assembleDocument) читают их напрямую, без похода в базу изнутри.
	cutPages := make(map[int64][]*models.Page, len(cuts))
	workIDs := map[int64]bool{}
	for _, c := range cuts {
		pages, err := cutStore.PagesOfCut(ctx, c)
		if err != nil {
			return "", 0, err
		}
		cutPages[c.ID] = pages
		if c.WorkID != nil {
			workIDs[*c.WorkID] = true
		}
	}

	// Смещение печатной колонцифры каждого затронутого тома — по одному
	// запросу на РАЗЛИЧНЫЙ work_id, а не на вклейку: несколько вклеек одного
	// тома делят один offset. Работа, которую не удалось прочитать (снята
	// между чтением вклейки и этим запросом), остаётся с offset 0 —
	// renderCutState всё равно не дойдёт до folioLabel для битой/отвязавшейся
	// вклейки, а для живой это не тот путь, которым том пропадает: WorkID
	// обнуляется в базе тем же ON DELETE SET NULL, что делает Broken().
	offsets := make(map[int64]int, len(workIDs))
	for workID := range workIDs {
		work, err := works.GetByID(ctx, workID)
		if err != nil {
			continue
		}
		offsets[workID] = work.PageOffset
	}

	// Счёт вклеек — по плейсхолдерам ПОКАЗАННОГО тела (d.MarkdownContent), а
	// не len(cuts): ByDocument отдаёт ВСЕ вклейки разбора, включая те, что
	// названы только черновиком, ждущим решения модератора, и не встречаются
	// в d.MarkdownContent вовсе (d здесь — уже сведённая к видимой пришедшему
	// редакции строка, см. documentForViewer). Без этой правки счёт мог
	// печатать больше вклеек, чем текст, который пришедший реально видит,
	// вообще называет. uniqueIDs — тот же дедуп, что у проверки на
	// сохранении (document_handler.go): повтор одного id в теле не должен
	// удваивать счёт.
	cutCount := len(uniqueIDs(cutPlaceholderIDs(d.MarkdownContent)))

	return assembleDocument(r, d.MarkdownContent, cuts, cutPages, offsets), cutCount, nil
}

// cutOrdinal отдаёт порядковый номер вклейки id в теле разбора — ту же
// область имён сносок, что получает эта вклейка при полной сборке
// (assembleDocument). Маршрут догрузки (Full) обязан рендерить тем же
// ordinal: иначе у догруженного блока будет другой префикс сносок, и при
// нескольких длинных вклейках в одном разборе их аппараты столкнутся
// одинаковыми id в DOM.
//
// Второй результат — названа ли вклейка этим телом вообще. Он же служит
// проверкой доступа в Full (I3): тело, по которому считается номер, — та
// редакция, которую вправе видеть ПРИШЕДШИЙ, и вклейка, названная только
// неодобренным черновиком, постороннему не достаётся. Раньше ненайденная
// вклейка молча получала номер 1, и отказать было нечем.
func cutOrdinal(body string, id int64) (ordinal int, named bool) {
	for i, pid := range cutPlaceholderIDs(body) {
		if pid == id {
			return i + 1, true
		}
	}
	return 0, false
}
