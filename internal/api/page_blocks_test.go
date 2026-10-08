package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"proofreader/internal/models"
	"proofreader/pkg/markdown"
)

// Смещения БАЙТОВЫЕ по content_markdown — тем же счётом, каким режет
// sliceCut и каким считает Anchor. Текст кириллический намеренно: на
// латинице байты и руны совпадают, и тест на подмене счёта остался бы
// зелёным.
func TestSplitPageBlocksOffsetsAreBytes(t *testing.T) {
	md := "Первый абзац.\n\n## Заголовок\n\nВторой абзац."
	blocks := splitPageBlocks(md)
	if len(blocks) != 3 {
		t.Fatalf("ожидалось 3 блока, получено %d: %+v", len(blocks), blocks)
	}
	for i, want := range []string{"Первый абзац.", "## Заголовок", "Второй абзац."} {
		got := md[blocks[i].Start:blocks[i].End]
		if got != want {
			t.Errorf("блок %d: границы дают %q, ожидалось %q", i, got, want)
		}
	}
	if blocks[1].Kind != "heading" {
		t.Errorf("заголовок опознан как %q", blocks[1].Kind)
	}
	if blocks[0].Kind != "paragraph" {
		t.Errorf("абзац опознан как %q", blocks[0].Kind)
	}
}

func TestSplitPageBlocksKinds(t *testing.T) {
	md := strings.Join([]string{
		"# Заглавие", "",
		"> Стих строкой\\", "> второй строкой", "",
		"- первый пункт", "- второй пункт", "",
		"Обычный абзац.", "",
		"[^1]: тело сноски.",
	}, "\n")
	blocks := splitPageBlocks(md)
	want := []string{"heading", "quote", "list", "paragraph", "footnote"}
	if len(blocks) != len(want) {
		t.Fatalf("ожидалось %d блоков, получено %d: %+v", len(want), len(blocks), blocks)
	}
	for i, k := range want {
		if blocks[i].Kind != k {
			t.Errorf("блок %d: вид %q, ожидался %q", i, blocks[i].Kind, k)
		}
	}
}

// Многострочный блок — ОДИН блок: абзац в markdown задаёт пустая строка, а
// не перевод строки. Разрежь по переводу — и автор, выбравший строфу,
// получит вклейку в одну строку стиха.
func TestSplitPageBlocksKeepsMultilineBlockWhole(t *testing.T) {
	md := "Первая строка\nвторая строка\nтретья строка\n\nДругой абзац."
	blocks := splitPageBlocks(md)
	if len(blocks) != 2 {
		t.Fatalf("ожидалось 2 блока, получено %d: %+v", len(blocks), blocks)
	}
	if md[blocks[0].Start:blocks[0].End] != "Первая строка\nвторая строка\nтретья строка" {
		t.Errorf("многострочный блок разрезан: %q", md[blocks[0].Start:blocks[0].End])
	}
}

// Пустая полоса не должна давать блоков нулевой длины: вырожденная вклейка
// (end == start) принимается API и уходит в stale при первой же правке.
func TestSplitPageBlocksSkipsEmpty(t *testing.T) {
	for _, md := range []string{"", "\n", "\n\n\n", "   \n\n\t\n"} {
		if blocks := splitPageBlocks(md); len(blocks) != 0 {
			t.Errorf("на %q получено %d блоков", md, len(blocks))
		}
	}
}

// Разделитель сцены "*      *" (звёздочка — пробелы — звёздочка) — не пункт
// списка: список без содержимого после маркера — не список, а строка,
// которая просто начинается со звёздочки. Живой пример — том 6 Ленина
// (стр. 52, 151, 171, 348, 416, 431, 448), найден сверкой фикс-раунда 1
// (docs/…/task-8-report.md, «Фикс-раунд 1»). Запасной ответ — "paragraph":
// отдельного вида «разделитель» в системе нет и заводить его ради двух
// десятков строк на корпус не стоит.
func TestSplitPageBlocksSceneBreakIsParagraph(t *testing.T) {
	md := "Первый абзац главы.\n\n*      *\n\nВторой абзац после разрыва сцены."
	blocks := splitPageBlocks(md)
	if len(blocks) != 3 {
		t.Fatalf("ожидалось 3 блока, получено %d: %+v", len(blocks), blocks)
	}
	if got := md[blocks[1].Start:blocks[1].End]; got != "*      *" {
		t.Fatalf("средний блок — не разделитель: %q", got)
	}
	if blocks[1].Kind != "paragraph" {
		t.Errorf("разделитель сцены опознан как %q, ожидался paragraph", blocks[1].Kind)
	}
}

// Ссылка на сноску без её определения не молчит: рендерер съедает текст в
// скобках следом за ней. Блок берётся из середины полосы, а тело сноски
// стоит внизу — без дотягивания предпросмотр показал бы автору текст,
// которого в полосе нет.
func TestPageBlocksHTMLKeepsTextAfterFootnoteRef(t *testing.T) {
	// Пробел (не слово) между "]" и "(" обязателен: ровно это сочетание
	// гомарк принимает за начало инлайн-ссылки и без определения сноски
	// съедает "(скобками)" в href несуществующей ссылки
	// (installFootnoteParenFix, pkg/markdown/renderer.go, footnoteRefEnd).
	// Слово между ними ("и (скобками)") эту ветку не задевает вовсе и не
	// проверяет дотягивание — так и было в первой редакции этого теста.
	page := &models.Page{ID: 1, WorkID: 1, PageNumber: 5,
		ContentMarkdown: "Текст со ссылкой[^1] (скобками) дальше.\n\n[^1]: тело сноски."}

	h := &PageHandler{
		pageRepo: &fakePageStore{
			getByIDFn: func(_ context.Context, id int64) (*models.Page, error) {
				return page, nil
			},
		},
		renderer: markdown.NewRenderer(),
	}

	rr := doGetVars(t, h.Blocks, "/api/works/1/pages/1/blocks",
		map[string]string{"workId": "1", "pageId": "1"}, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("код %d, тело %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Blocks []pageBlock `json:"blocks"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if len(got.Blocks) == 0 {
		t.Fatal("блоков нет")
	}
	if !strings.Contains(got.Blocks[0].HTML, "(скобками)") {
		t.Fatalf("текст после ссылки съеден: %q", got.Blocks[0].HTML)
	}
	// Аппарат подборщику не показывают: тело сноски в HTML блока не едет.
	if strings.Contains(got.Blocks[0].HTML, "тело сноски") {
		t.Fatalf("тело сноски попало в блок: %q", got.Blocks[0].HTML)
	}
}

// Блоки-определения сносок ответ маршрута не отдаёт вовсе: CollectPagesScoped
// вынимает тело сноски из HTML, и для блока, который И ЕСТЬ определение,
// HTML получался бы пустым — контракт «границы + готовый HTML» на восьми
// процентах корпуса (вид footnote в сверке) не выполнялся бы. Тело сноски
// автор всё равно получает — оно дотягивается withFootnoteDefs к блоку со
// ссылкой (см. TestPageBlocksHTMLKeepsTextAfterFootnoteRef выше); отдельным
// блоком для вклейки оно не нужно и не годится.
//
// Проверяем ОТВЕТ МАРШРУТА, а не голую splitPageBlocks: дефект был именно в
// обработчике (пустой html[0] после рендера), рецензия нашла его прогоном
// настоящего обработчика, а не рассуждением.
func TestPageBlocksHandlerOmitsFootnoteDefinitions(t *testing.T) {
	page := &models.Page{ID: 1, WorkID: 1, PageNumber: 9,
		ContentMarkdown: strings.Join([]string{
			"Первый абзац тела.",
			"",
			"Второй абзац тела со ссылкой[^1].",
			"",
			"[^1]: Тело первой сноски.",
			"",
			"[^2]: Тело второй сноски, по ссылке не использованной.",
		}, "\n")}

	h := &PageHandler{
		pageRepo: &fakePageStore{
			getByIDFn: func(_ context.Context, id int64) (*models.Page, error) {
				return page, nil
			},
		},
		renderer: markdown.NewRenderer(),
	}

	rr := doGetVars(t, h.Blocks, "/api/works/1/pages/1/blocks",
		map[string]string{"workId": "1", "pageId": "1"}, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("код %d, тело %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Blocks []pageBlock `json:"blocks"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}

	if len(got.Blocks) != 2 {
		t.Fatalf("ожидалось 2 блока тела (без двух сносок), получено %d: %+v", len(got.Blocks), got.Blocks)
	}
	for i, b := range got.Blocks {
		if b.Kind == "footnote" {
			t.Errorf("блок %d: вид footnote попал в ответ маршрута", i)
		}
		if b.HTML == "" {
			t.Errorf("блок %d (%q): пустой HTML", i, b.Kind)
		}
	}
}

// Определение сноски ПОСРЕДИ полосы (не внизу) не должно портить выбор:
// автор берёт блок до него и блок после, а байтовый диапазон между их
// границами обязан накрывать пропущенное определение целиком — оно просто
// не выбирается ОТДЕЛЬНО, но не выпадает из диапазона, охватывающего оба
// соседних блока. Опускание footnote-блоков из ответа НЕ смещает Start/End
// соседей — splitPageBlocks режет их независимо друг от друга, разрез не
// меняется тем, что клиент один из результатов не увидит.
func TestSplitPageBlocksFootnoteInMiddleStaysCoveredByNeighborRange(t *testing.T) {
	md := strings.Join([]string{
		"Абзац до сноски.",
		"",
		"[^1]: Тело сноски посреди полосы.",
		"",
		"Абзац после сноски.",
	}, "\n")

	all := splitPageBlocks(md)
	if len(all) != 3 {
		t.Fatalf("ожидалось 3 блока (до/сноска/после), получено %d: %+v", len(all), all)
	}
	before, mid, after := all[0], all[1], all[2]
	if before.Kind != "paragraph" || mid.Kind != "footnote" || after.Kind != "paragraph" {
		t.Fatalf("неожиданные виды блоков: %+v", all)
	}

	// Диапазон «от начала блока ДО до конца блока ПОСЛЕ» — то, что получил бы
	// автор, выбравший оба соседних блока сразу, — обязан включать байты
	// пропущенного определения сноски целиком.
	span := md[before.Start:after.End]
	if !strings.Contains(span, md[mid.Start:mid.End]) {
		t.Fatalf("диапазон соседей не накрывает срединную сноску: %q не содержит %q",
			span, md[mid.Start:mid.End])
	}
	if !strings.Contains(span, "Тело сноски посреди полосы.") {
		t.Fatalf("текст сноски выпал из диапазона соседей: %q", span)
	}
}

// Windows-перевод строки (\r\n): проверено вручную ещё в фикс-раунде 2, что
// код это уже терпит (trimmed := strings.TrimSuffix(line, "\r")) — тест
// закрепляет поведение на будущее, а не ловит сегодняшний дефект. Многострочный
// блок с \r\n внутри остаётся ОДНИМ блоком, а хвостовой \r перед пустой
// строкой-разделителем в границы блока не попадает.
func TestSplitPageBlocksHandlesCRLF(t *testing.T) {
	md := "Первая строка\r\nвторая строка\r\n\r\nДругой абзац."
	blocks := splitPageBlocks(md)
	if len(blocks) != 2 {
		t.Fatalf("ожидалось 2 блока, получено %d: %+v", len(blocks), blocks)
	}
	want := "Первая строка\r\nвторая строка"
	if got := md[blocks[0].Start:blocks[0].End]; got != want {
		t.Errorf("блок с CRLF: граница даёт %q, ожидалось %q", got, want)
	}
	if got := md[blocks[1].Start:blocks[1].End]; got != "Другой абзац." {
		t.Errorf("второй блок: %q", got)
	}
}

// Блок из одного печатного знака — живой пример корпуса (напр. одинокое "?"
// или "*" на отдельной строке). Проверено сверкой фикс-раунда 1: такой блок
// не роняет разрезку и не путается со списком (нет пробела после знака —
// looksLikeList не совпадёт вовсе). Тест закрепляет уже верное поведение.
func TestSplitPageBlocksSingleCharacterBlock(t *testing.T) {
	md := "Абзац перед знаком.\n\n*\n\nАбзац после знака."
	blocks := splitPageBlocks(md)
	if len(blocks) != 3 {
		t.Fatalf("ожидалось 3 блока, получено %d: %+v", len(blocks), blocks)
	}
	mid := blocks[1]
	if got := md[mid.Start:mid.End]; got != "*" {
		t.Fatalf("средний блок не одинокий знак: %q", got)
	}
	if mid.End-mid.Start != 1 {
		t.Errorf("границы блока из одного знака не в один байт: %d", mid.End-mid.Start)
	}
	if mid.Kind != "paragraph" {
		t.Errorf("одинокий знак опознан как %q, ожидался paragraph", mid.Kind)
	}
}
