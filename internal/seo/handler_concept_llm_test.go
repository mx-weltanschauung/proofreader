package seo

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"proofreader/internal/repository"
	"proofreader/pkg/book"
)

const llmConcept = "/concepts/abstraktnyj-trud"

func TestHandlerServesConceptText(t *testing.T) {
	rec := get(t, handlerFor(conceptTextSource(conceptBookOf(100))), llmConcept+".md", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type: %q", ct)
	}
	if rec.Header().Get("X-Robots-Tag") != "noindex" {
		t.Errorf("X-Robots-Tag: %q", rec.Header().Get("X-Robots-Tag"))
	}
	if got, want := rec.Header().Get("Link"), "<"+conceptURL+`>; rel="canonical"`; got != want {
		t.Errorf("Link: %q, ожидалось %q", got, want)
	}
	if !strings.Contains(rec.Body.String(), "Понятие «Абстрактный труд»") {
		t.Errorf("тело:\n%s", rec.Body.String())
	}
}

func TestHandlerConceptTextPartsOneRender(t *testing.T) {
	src := conceptTextSource(conceptBookOf(50000))
	books := src.ConceptBooks.(*fakeConceptBooks)
	h := handlerFor(src)
	for _, p := range []string{"/part-2.md", ".md", "/part-3.md", "/part-2.md"} {
		if rec := get(t, h, llmConcept+p, nil); rec.Code != http.StatusOK {
			t.Errorf("%s: код %d", p, rec.Code)
		}
	}
	if n := atomic.LoadInt32(&books.calls); n != 1 {
		t.Errorf("понятие собиралось %d раз, ожидался один рендер на все части", n)
	}
	// Номер вне понятия частями не покрыт и рендерится своим ходом (как
	// /part-9.md у главы) — поэтому после счёта, а не до.
	if rec := get(t, h, llmConcept+"/part-4.md", nil); rec.Code != http.StatusNotFound {
		t.Errorf("часть 4 из 3: код %d, ожидался 404", rec.Code)
	}
}

func TestHandlerConceptTextFirstPartRedirects(t *testing.T) {
	rec := get(t, handlerFor(conceptTextSource(conceptBookOf(100))), llmConcept+"/part-1.md", nil)
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != llmConcept+".md" {
		t.Errorf("код %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestHandlerMissingConceptTextIsGone(t *testing.T) {
	if rec := get(t, handlerFor(conceptTextSource(conceptBookOf(100))), "/concepts/net-takogo.md", nil); rec.Code != http.StatusGone {
		t.Errorf("код %d, ожидался 410", rec.Code)
	}
}

func TestHandlerMalformedConceptTextIs404(t *testing.T) {
	h := handlerFor(conceptTextSource(conceptBookOf(100)))
	for _, p := range []string{llmConcept + "/part-0.md", llmConcept + "/part-x.md", llmConcept + "/extra/part-2.md", "/concepts//part-2.md"} {
		if rec := get(t, h, p, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: код %d, ожидался 404", p, rec.Code)
		}
	}
}

// Сброс кэша (снятие тома зовёт Purge) уносит и тексты понятий: понятие
// собирает полосы многих томов, и адресно его не найти.
func TestHandlerPurgeDropsConceptText(t *testing.T) {
	src := conceptTextSource(conceptBookOf(100))
	books := src.ConceptBooks.(*fakeConceptBooks)
	h := handlerFor(src)
	get(t, h, llmConcept+".md", nil)
	h.Purge()
	get(t, h, llmConcept+".md", nil)
	if n := atomic.LoadInt32(&books.calls); n != 2 {
		t.Errorf("после сброса понятие не пересобрано: рендеров %d", n)
	}
}

func TestHandlerServesConceptShelf(t *testing.T) {
	src := shelfSource([]repository.ConceptShelfRow{{Slug: "part-2", Title: "Подложное", SortKey: "п", Places: 1}})
	src.ConceptBooks = &fakeConceptBooks{}
	h := handlerFor(src)
	rec := get(t, h, "/concepts.md", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "# Предметный указатель") {
		t.Fatalf("/concepts.md: код %d\n%s", rec.Code, rec.Body.String())
	}
	// Часть 2 витрины из одной части — 404, а не понятие со слагом part-2.
	if rec := get(t, h, "/concepts/part-2.md", nil); rec.Code != http.StatusNotFound {
		t.Errorf("/concepts/part-2.md: код %d, ожидался 404", rec.Code)
	}
	if n := atomic.LoadInt32(&src.ConceptBooks.(*fakeConceptBooks).calls); n != 0 {
		t.Errorf("витрина ушла в сборку понятия: %d", n)
	}
	if rec := get(t, h, "/concepts/part-1.md", nil); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/concepts.md" {
		t.Errorf("/concepts/part-1.md: код %d Location %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestHandlerConceptShelfIsLight(t *testing.T) {
	src := conceptTextSource(conceptBookOf(100))
	src.Catalog = &fakeCatalog{conceptShelf: []repository.ConceptShelfRow{{Slug: "a", Title: "А", SortKey: "а"}}}
	h := handlerFor(src)
	for i := 0; i < cap(h.heavy); i++ {
		h.heavy <- struct{}{}
	}
	if rec := get(t, h, "/concepts.md", nil); rec.Code != http.StatusOK {
		t.Errorf("витрина при занятых слотах: код %d", rec.Code)
	}
	if rec := get(t, h, llmConcept+".md", nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("понятие при занятых слотах: код %d, ожидался 503", rec.Code)
	}
}

func TestHandlerConceptTextIsHeavy(t *testing.T) {
	h := handlerFor(conceptTextSource(conceptBookOf(100)))
	for i := 0; i < cap(h.heavy); i++ {
		h.heavy <- struct{}{}
	}
	if rec := get(t, h, llmConcept+".md", nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("текст понятия при занятых слотах: код %d, ожидался 503", rec.Code)
	}
}

// bigConceptSource — понятие на сто частей по ~140 КБ: в десять раз больше
// textPartsBudget (по одной полосе в часть, полоса не дробится).
func bigConceptSource() *Source {
	cb := conceptBookOf(70000)
	pages := make([]book.Page, 100)
	for i := range pages {
		pages[i] = book.Page{Internal: i + 1, Printed: i + 1, Markdown: strings.Repeat("т", 70000)}
	}
	cb.Book.Sections = []book.Section{{Title: "место", Blocks: []book.Block{{Pages: pages}}}}
	cb.Rubrics = nil
	cb.Places, cb.Present, cb.Pages = 1, 1, 100
	return conceptTextSource(cb)
}

// Длинный текст не вытесняет кэш целиком: один рендер кладёт окно частей в
// пределах textPartsBudget — запрошенную и идущие за ней вперёд, а не
// предыдущие и не все сто.
func TestHandlerConceptTextPartsBudgetKeepsWindowForward(t *testing.T) {
	src := bigConceptSource()
	books := src.ConceptBooks.(*fakeConceptBooks)
	h := handlerFor(src)
	for _, p := range []string{"/part-51.md", "/part-52.md", "/part-53.md", "/part-51.md"} {
		if rec := get(t, h, llmConcept+p, nil); rec.Code != http.StatusOK {
			t.Fatalf("%s: код %d", p, rec.Code)
		}
	}
	if n := atomic.LoadInt32(&books.calls); n != 1 {
		t.Fatalf("после запрошенной и следующих собрано %d раз, ожидался один рендер", n)
	}
	if rec := get(t, h, llmConcept+"/part-100.md", nil); rec.Code != http.StatusOK {
		t.Fatalf("часть 100: код %d", rec.Code)
	}
	if n := atomic.LoadInt32(&books.calls); n != 2 {
		t.Errorf("часть далеко за окном вышла из кэша: собрано %d раз, ожидалось 2 (бюджет исчерпан)", n)
	}
	if rec := get(t, h, llmConcept+"/part-2.md", nil); rec.Code != http.StatusOK {
		t.Fatalf("часть 2: код %d", rec.Code)
	}
	if n := atomic.LoadInt32(&books.calls); n != 3 {
		t.Errorf("часть 2 при запросе 51 не должна лежать в кэше: собрано %d раз, ожидалось 3", n)
	}
}

// Значение — ровно то, что кладёт в адрес страница понятия (пример из жизни).
const rubricParam = "?rubric_path=%25D0%25BE%25D0%25BF%25D1%2580%25D0%25B5%25D0%25B4%25D0%25B5%25D0%25BB%25D0%25B5%25D0%25BD%25D0%25B8%25D0%25B5"

func TestHandlerConceptTextRubricIsSeparateText(t *testing.T) {
	src := conceptTextSource(conceptBookOf(50000))
	books := src.ConceptBooks.(*fakeConceptBooks)
	h := handlerFor(src)

	whole := get(t, h, llmConcept+".md", nil)
	part := get(t, h, llmConcept+"/part-2.md"+rubricParam, nil)
	first := get(t, h, llmConcept+".md"+rubricParam, nil)
	if whole.Code != http.StatusOK || part.Code != http.StatusOK || first.Code != http.StatusOK {
		t.Fatalf("коды %d %d %d", whole.Code, part.Code, first.Code)
	}
	if strings.Contains(whole.Body.String(), "Только подрубрика") ||
		!strings.Contains(first.Body.String(), "Только подрубрика «определение»") {
		t.Error("подрубрика и понятие целиком смешались в кэше")
	}
	// Два текста — две сборки; часть 1 подрубрики пришла из кэша её части 2.
	if n := atomic.LoadInt32(&books.calls); n != 2 {
		t.Errorf("сборок %d, ожидалось 2", n)
	}
	if got := books.rubrics[1]; len(got) != 1 || got[0] != "определение" {
		t.Errorf("сборка получила подрубрику %v", got)
	}
	if got := part.Header().Get("Link"); got != "<"+conceptURL+rubricParam+`>; rel="canonical"` {
		t.Errorf("Link: %q", got)
	}
}

func TestHandlerConceptTextRubricRedirectsFirstPart(t *testing.T) {
	rec := get(t, handlerFor(conceptTextSource(conceptBookOf(100))), llmConcept+"/part-1.md"+rubricParam, nil)
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != llmConcept+".md"+rubricParam {
		t.Errorf("код %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestHandlerConceptTextBadRubricIs404(t *testing.T) {
	h := handlerFor(conceptTextSource(conceptBookOf(100)))
	unknown := rubricQuery([]string{fakeNoRubric})
	for _, p := range []string{
		llmConcept + ".md" + unknown,         // нет такой подрубрики
		llmConcept + ".md?rubric_path=%25ZZ", // не разбирается
		"/concepts.md" + rubricParam,         // у витрины подрубрик нет
		llmChapter + ".md" + rubricParam,     // у главы тоже
	} {
		if rec := get(t, h, p, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: код %d, ожидался 404", p, rec.Code)
		}
	}
	if rec := get(t, h, llmConcept+".md"+unknown, nil); !strings.Contains(rec.Body.String(), "подрубрики") {
		t.Errorf("отказ без объяснения: %q", rec.Body.String())
	}
}

// Страница понятия не кодирует «(» внутри звена (encodeURIComponent), канон
// сервера кодирует (QueryEscape). Оба написания — одна подрубрика: 200 без
// лишнего 301 и одна сборка на двоих.
func TestHandlerConceptTextRubricEncodingIsNormalised(t *testing.T) {
	src := conceptTextSource(conceptBookOf(100))
	books := src.ConceptBooks.(*fakeConceptBooks)
	h := handlerFor(src)
	jsInner := "%D0%9C%D0%B0%D1%80%D0%BA%D1%81%20(%D0%BE%20%D0%9B%D0%B5%D0%BD%D0%B8%D0%BD%D0%B5)"
	js := "?" + url.Values{"rubric_path": {jsInner}}.Encode()
	canon := rubricQuery([]string{"Маркс (о Ленине)"})
	if js == canon {
		t.Fatal("тест бессмыслен: написания совпали")
	}
	for _, q := range []string{js, canon} {
		if rec := get(t, h, llmConcept+".md"+q, nil); rec.Code != http.StatusOK {
			t.Errorf("%s: код %d, ожидался 200", q, rec.Code)
		}
	}
	if n := atomic.LoadInt32(&books.calls); n != 1 {
		t.Errorf("сборок %d, ожидалась одна на оба написания", n)
	}
}

// MCP просит текст подрубрики через Text путём с канонической строкой
// запроса — тот же разбор, что у краулера, и та же запись кэша.
func TestTextServesConceptRubric(t *testing.T) {
	src := conceptTextSource(conceptBookOf(100))
	books := src.ConceptBooks.(*fakeConceptBooks)
	h := handlerFor(src)
	path := llmConcept + ".md" + RubricQuery([]string{"определение"})
	res, err := h.Text(context.Background(), path, tryHeavy)
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != path || !strings.Contains(res.Body, "Только подрубрика «определение»") {
		t.Errorf("путь %q, тело:\n%s", res.Path, res.Body)
	}
	if rec := get(t, h, path, nil); rec.Code != http.StatusOK {
		t.Fatalf("краулер: код %d", rec.Code)
	}
	if n := atomic.LoadInt32(&books.calls); n != 1 {
		t.Errorf("сборок %d, ожидалась одна на MCP и краулера", n)
	}
}

// Отказ по подрубрике ложится в кэш: перебор выдуманных подрубрик не должен
// стоить сборки на каждый заход.
func TestHandlerConceptTextUnknownRubricIsCached(t *testing.T) {
	src := conceptTextSource(conceptBookOf(100))
	books := src.ConceptBooks.(*fakeConceptBooks)
	h := handlerFor(src)
	unknown := rubricQuery([]string{fakeNoRubric})
	for i := 0; i < 2; i++ {
		rec := get(t, h, llmConcept+".md"+unknown, nil)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "подрубрики") {
			t.Errorf("заход %d: код %d, тело %q", i+1, rec.Code, rec.Body.String())
		}
	}
	if n := atomic.LoadInt32(&books.calls); n != 1 {
		t.Errorf("сборок %d, ожидалась одна", n)
	}
}
