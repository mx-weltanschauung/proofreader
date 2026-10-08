package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"proofreader/internal/models"
	"proofreader/pkg/markdown"
)

// C1. Тело разбора пишет читатель, и до этой правки рендерер пропускал сырой
// HTML насквозь: SkipHTML не стоял, санитайзера в дереве нет. Путь целиком —
// читатель кладёт в тело `<img src=x onerror=…>`, отправляет на проверку,
// модератор ОБЯЗАН открыть предпросмотр (mayReadDraft нарочно отдаёт ему
// черновик), и скрипт исполняется под его токеном из localStorage. В
// предпросмотре полезной нагрузки не видно (битая картинка), так что
// «просмотрел и одобрил» выносит её на людей.
//
// До этой ветки разборы писал только сотрудник, и сырой HTML был безопасен по
// СОСТАВУ АВТОРОВ, а не по коду. Проверяется то, что уезжает клиенту.

// authorHTMLPayload — тело разбора со всеми четырьмя формами, какими
// исполняемый HTML доезжает до браузера модератора: блочный <script>,
// встроенная картинка с обработчиком события, ссылка с javascript: и
// обработчиком, блочный <iframe>.
const authorHTMLPayload = "Начало разбора.\n\n" +
	"<script>alert(1)</script>\n\n" +
	"Текст с <img src=x onerror=\"alert(2)\"> картинкой.\n\n" +
	"<a href=\"javascript:alert(3)\" onclick=\"alert(4)\">ссылка</a>\n\n" +
	"<iframe src=\"//evil.example\"></iframe>\n\n" +
	"Конец разбора.\n"

// authorNativePayload — то же самое, но РОДНЫМИ для markdown средствами, без
// единого сырого тега.
//
// Урок повторной рецензии: первая редакция этого файла перечисляла верные
// приметы (`onclick`, `javascript:`), но подавала их ТОЛЬКО сырым HTML —
// сторож был зелен по неверной причине. Подавление сырого HTML закрывает
// вектор, а не находку: «тело разбора пишет читатель, и оно исполняется в
// браузере модератора».
//
// Два стока, замеренные на живом рендерере, а не выведенные из флагов:
//
//   - parser.Attributes вешает на ЛЮБОЙ блок произвольные пары
//     (`{onmouseover="…" style="…"}` строкой перед блоком) — проверено на
//     абзаце, заголовке, цитате и блоке кода. Это бесклик: абзац со
//     `style="position:fixed;inset:0"` растягивается на весь экран, и
//     модератору довольно провести мышью по предпросмотру, который он обязан
//     открыть;
//   - без html.Safelink `[текст](javascript:…)` уезжает в href как есть;
//   - картинка `![](https://чужой/x.gif)` — не исполнение, а МАЯЧОК: браузер
//     модератора сходит на чужой хост при открытии предпросмотра, и
//     обязательный просмотр превращается в отметку «такой-то читал такой-то
//     текст в такую-то минуту», выданную тому, кого рассматривают.
const authorNativePayload = "Начало разбора.\n\n" +
	"{onmouseover=\"alert(1)\" style=\"position:fixed;inset:0;z-index:99\"}\n" +
	"Безобидный с виду абзац.\n\n" +
	"{onclick=\"alert(2)\"}\n" +
	"# Заголовок разбора\n\n" +
	"{onmouseover=\"alert(3)\"}\n" +
	"> Цитата из тома\n\n" +
	"{onload=\"alert(4)\"}\n" +
	"```\nблок кода\n```\n\n" +
	"[ссылка](javascript:alert(5))\n\n" +
	"![маячок](https://evil.example/track.gif)\n\n" +
	"[текст ![вложенный маячок](https://evil.example/p.gif)](https://example.org)\n\n" +
	"Конец разбора.\n"

// По чему судим — см. unsafeMarkupIn (executable_markup_probe_test.go):
// готовая вёрстка РАЗБИРАЕТСЯ, и претензии предъявляются к разметке, а не к
// подстрокам. Проверяется свойство («в выводе есть исполняемое»), а не способ
// починки.

func TestAssembleStripsRawHTMLFromAuthorBody(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"сырым HTML", authorHTMLPayload},
		{"родными средствами markdown", authorNativePayload},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := assembleDocument(markdown.NewRenderer(), tc.body,
				nil, map[int64][]*models.Page{}, map[int64]int{})

			if bad := unsafeMarkupIn(got); len(bad) > 0 {
				t.Errorf("исполняемое уехало клиенту: %v\n%s", bad, got)
			}
			// Подавление не вправе съедать сам разбор.
			if !strings.Contains(got, "Начало разбора") || !strings.Contains(got, "Конец разбора") {
				t.Fatalf("подавление съело текст автора:\n%s", got)
			}
		})
	}
}

// Контрольная группа к тесту выше: подавление бьёт по исполняемому, а не по
// разметке вообще. Без неё «починка», выкинувшая из авторского текста всё
// подряд, прошла бы зелёной.
func TestAuthorMarkdownKeepsHarmlessMarkup(t *testing.T) {
	const body = "*курсив*, **жирный** и [обычная ссылка](https://example.org).\n\n" +
		"# Заголовок\n\n" +
		"> Цитата\n\n" +
		"| а | б |\n| --- | --- |\n| 1 | 2 |\n\n" +
		"Текст до ![выброшенная](https://example.org/p.gif) текст после.\n\n" +
		"Сноска автора[^1].\n\n[^1]: тело сноски\n"

	got := assembleDocument(markdown.NewRenderer(), body,
		nil, map[int64][]*models.Page{}, map[int64]int{})

	for _, want := range []string{
		"<em>курсив</em>", "<strong>жирный</strong>",
		`href="https://example.org"`, "<h1", "<blockquote>",
		"<table>", "<td", "тело сноски",
		// Картинка выброшена, а текст ВОКРУГ неё — нет: подавление снимает
		// подресурс, а не абзац, в котором он стоял.
		"Текст до", "текст после.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("подавление задело безобидную разметку, нет %q:\n%s", want, got)
		}
	}
	if bad := unsafeMarkupIn(got); len(bad) > 0 {
		t.Errorf("в безобидной фикстуре осталось недоверенное: %v\n%s", bad, got)
	}
}

// Тот же вход, но через ЖИВОЙ обработчик View: тест выше держит сборщик, этот
// — поле html_content ответа, то самое, что DocumentView.tsx ставит через
// dangerouslySetInnerHTML.
func TestDocumentViewResponseCarriesNoExecutableAuthorHTML(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"сырым HTML", authorHTMLPayload},
		{"родными средствами markdown", authorNativePayload},
	} {
		t.Run(tc.name, func(t *testing.T) { viewCarriesNothingExecutable(t, tc.body) })
	}
}

func viewCarriesNothingExecutable(t *testing.T, body string) {
	t.Helper()
	doc := &models.Document{
		ID: 7, Slug: "razbor", Title: "Разбор", MarkdownContent: body,
		PublishedTitle: "Разбор", PublishedMarkdown: body,
		PublishedAt: timePtr(time.Now()), WasPublished: true,
		ReviewStatus: models.DocumentApproved, AuthorNickname: "чтец",
		OwnerID: ptrInt64(5),
	}
	h := newTestDocumentHandler(t, doc)

	rr := doGetVars(t, h.View, "/api/documents/чтец/razbor/view", map[string]string{"nickname": "чтец", "slug": "razbor"}, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("ожидался 200, получен %d: %s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("ответ не разобрался: %v", err)
	}
	content, _ := got["html_content"].(string)
	if content == "" {
		t.Fatalf("html_content пуст: %s", rr.Body.String())
	}
	if bad := unsafeMarkupIn(content); len(bad) > 0 {
		t.Errorf("html_content несёт исполняемое: %v\n%s", bad, content)
	}
}

// corpusCutMarkdown — кусок корпуса с разметкой, которая в томах законна и
// обязана пережить правку C1 нетронутой: ручная HTML-таблица (marker так
// печатает таблицы с rowspan), сноска и курсив.
const corpusCutMarkdown = "Ленин писал[^r1] так:\n\n" +
	"<table><tr><td>товар<br>в центнерах</td><td>1901</td></tr></table>\n\n" +
	"*Разрядка* в тексте.\n\n[^r1]: примечание внизу полосы"

// Шов C1, названный рецензией: плейсхолдер `<cut id="N">` ищется уже в
// ОТРИСОВАННОМ HTML. Подави сырой HTML в лоб — и тег исчезнет (или приедет
// экранированным) вместе с полезной нагрузкой, а вклейки молча пропадут из
// разбора.
//
// Тест держит обе половины разом: сырой HTML автора подавлен, вклейка на
// месте, а её вывод — ПОБАЙТОВО тот же, что даёт renderCutTrimmed сам по
// себе. Корпусная разметка законна (в дереве есть pkg/mathml, и корпус её
// знает), и правка C1 не вправе её тронуть.
func TestAssembleSplicesCutByteForByteBesideStrippedAuthorHTML(t *testing.T) {
	cut := &models.DocumentCut{
		ID: 7, WorkID: ptrInt64(1),
		Anchor: models.Anchor{StartPageID: 10, StartOffset: 0, EndPageID: 10,
			EndOffset: len(corpusCutMarkdown)},
		Status: models.CutStatusOK, SourceTitle: "Ленин. Что делать?",
	}
	pages := []*models.Page{cutPage(10, 4, corpusCutMarkdown)}

	body := "Моя мысль.\n\n<script>alert(1)</script>\n\n<cut id=\"7\">\n\nПосле вклейки.\n"
	got := assembleDocument(markdown.NewRenderer(), body,
		[]*models.DocumentCut{cut},
		map[int64][]*models.Page{7: pages},
		map[int64]int{1: 232})

	want := renderCutTrimmed(markdown.NewRenderer(), cut, pages, 1, 232)
	if !strings.Contains(got, want) {
		t.Fatalf("вывод вклейки изменился побайтово.\nждали:\n%s\n\nполучили:\n%s", want, got)
	}
	if strings.Contains(got, "<cut id") || strings.Contains(got, "&lt;cut") {
		t.Fatalf("плейсхолдер пережил сборку (сырым или экранированным): %s", got)
	}
	if strings.Contains(got, "<script") {
		t.Fatalf("сырой HTML автора уехал рядом с живой вклейкой: %s", got)
	}
	if !strings.Contains(got, "Моя мысль") || !strings.Contains(got, "После вклейки") {
		t.Fatalf("текст автора вокруг вклейки потерян: %s", got)
	}
}

// Золотой снимок вывода вклейки: тот же вход — тот же байт в байт HTML, и
// подавление авторского HTML на него не влияет никак. Проверяет РОВНО
// обязательное ограничение рецензии («затронуть надо только текст автора»)
// на двух независимых входах: полная сборка и одиночный рендер.
func TestCutOutputUnaffectedByAuthorHTMLSuppression(t *testing.T) {
	cut := &models.DocumentCut{
		ID: 3, WorkID: ptrInt64(1),
		Anchor: models.Anchor{StartPageID: 10, StartOffset: 0, EndPageID: 10,
			EndOffset: len(corpusCutMarkdown)},
		Status: models.CutStatusOK, SourceTitle: "Ленин. Что делать?",
	}
	pages := []*models.Page{cutPage(10, 4, corpusCutMarkdown)}

	solo := renderCut(markdown.NewRenderer(), cut, pages, 1, 232)

	// Ручная таблица корпуса и её <br> обязаны доехать до читателя как есть:
	// это печатная таблица тома, а не текст, который пишет читатель.
	for _, want := range []string{"<table>", "<td>", "товар", "1901", "fnref:v1-4-r1"} {
		if !strings.Contains(solo, want) {
			t.Fatalf("корпусная разметка вклейки пострадала, нет %q:\n%s", want, solo)
		}
	}

	assembled := assembleDocument(markdown.NewRenderer(), "<cut id=\"3\">",
		[]*models.DocumentCut{cut},
		map[int64][]*models.Page{3: pages},
		map[int64]int{1: 232})
	if !strings.Contains(assembled, solo) {
		t.Fatalf("сборка отдала вклейку не побайтово.\nодиночный рендер:\n%s\n\nсборка:\n%s", solo, assembled)
	}
}
