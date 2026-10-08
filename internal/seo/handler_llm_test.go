package seo

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"proofreader/pkg/book"
)

const llmChapter = "/works/1-lenin-t42/chapters/10-gosudarstvo-i-revolyuciya"

func TestHandlerServesChapterText(t *testing.T) {
	rec := get(t, handlerFor(textSource()), llmChapter+".md", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type: %q", ct)
	}
	if got := rec.Header().Get("X-Robots-Tag"); got != "noindex" {
		t.Errorf("X-Robots-Tag: %q", got)
	}
	if got, want := rec.Header().Get("Link"), `<https://lib.example.org`+llmChapter+`>; rel="canonical"`; got != want {
		t.Errorf("Link: %q, ожидалось %q", got, want)
	}
	if !strings.Contains(rec.Body.String(), "Классовое общество и государство.") {
		t.Errorf("нет текста главы:\n%s", rec.Body.String())
	}
}

// Кэш общий у HTML и текста: запись обязана помнить свой тип, иначе второй
// заход за .md получил бы text/html, а HTML после .md — text/plain.
func TestHandlerCachedTextKeepsContentType(t *testing.T) {
	h := handlerFor(textSource())
	for i, c := range []struct{ path, ct string }{
		{llmChapter + ".md", "text/plain; charset=utf-8"},
		{llmChapter, "text/html; charset=utf-8"},
		{llmChapter + ".md", "text/plain; charset=utf-8"},
		{llmChapter, "text/html; charset=utf-8"},
	} {
		rec := get(t, h, c.path, nil)
		if got := rec.Header().Get("Content-Type"); got != c.ct {
			t.Errorf("заход %d %s: Content-Type %q, ожидалось %q", i+1, c.path, got, c.ct)
		}
	}
	// HTML-страница не получает заголовков текста.
	if rec := get(t, h, llmChapter, nil); rec.Header().Get("X-Robots-Tag") != "" || rec.Header().Get("Link") != "" {
		t.Error("HTML-страница получила заголовки текста")
	}
}

func TestHandlerRedirectsNonCanonicalText(t *testing.T) {
	src := longSource()
	h := handlerFor(src)
	for _, c := range []struct{ path, want string }{
		{"/works/1/chapters/10.md", llmChapter + ".md"},
		{"/works/1/chapters/10/part-2.md", llmChapter + "/part-2.md"},
		{llmChapter + "/part-1.md", llmChapter + ".md"},
		{"/works/1.md", "/works/1-lenin-t42.md"},
	} {
		rec := get(t, h, c.path, nil)
		if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != c.want {
			t.Errorf("%s: код %d, Location %q, ожидалось 301 на %q", c.path, rec.Code, rec.Header().Get("Location"), c.want)
		}
	}
}

func TestHandlerServesTextParts(t *testing.T) {
	h := handlerFor(longSource())
	for _, p := range []string{".md", "/part-2.md", "/part-3.md"} {
		if rec := get(t, h, llmChapter+p, nil); rec.Code != http.StatusOK {
			t.Errorf("%s: код %d", p, rec.Code)
		}
	}
	if rec := get(t, h, llmChapter+"/part-4.md", nil); rec.Code != http.StatusNotFound {
		t.Errorf("часть 4 из 3: код %d, ожидался 404", rec.Code)
	}
}

// Одна часть стоит рендера всей главы (книга, markdown, разрез), поэтому
// первый рендер кладёт в кэш ВСЕ части: сборщик, читающий главу в 28 частей
// подряд, иначе делал бы 28 полных рендеров и занимал слот на каждом.
func TestHandlerRendersChapterTextOnceForAllParts(t *testing.T) {
	src := longSource()
	books := src.Books.(*fakeBooks)
	h := handlerFor(src)
	if rec := get(t, h, llmChapter+"/part-2.md", nil); rec.Code != http.StatusOK {
		t.Fatalf("часть 2: код %d", rec.Code)
	}
	for _, p := range []string{".md", "/part-2.md", "/part-3.md"} {
		if rec := get(t, h, llmChapter+p, nil); rec.Code != http.StatusOK {
			t.Errorf("%s: код %d", p, rec.Code)
		}
	}
	if n := atomic.LoadInt32(&books.calls); n != 1 {
		t.Errorf("глава собиралась %d раз, ожидался один рендер на все части", n)
	}
}

// Номер за пределами главы — 404, и повтор того же номера не стоит нового
// рендера: /part-99.md длинной главы иначе был бы полным рендером на каждый
// заход.
func TestHandlerCachesMissingPart(t *testing.T) {
	src := longSource()
	books := src.Books.(*fakeBooks)
	h := handlerFor(src)
	for i := 0; i < 2; i++ {
		if rec := get(t, h, llmChapter+"/part-9.md", nil); rec.Code != http.StatusNotFound {
			t.Errorf("заход %d: часть 9 из 3 — код %d, ожидался 404", i+1, rec.Code)
		}
	}
	if n := atomic.LoadInt32(&books.calls); n != 1 {
		t.Errorf("глава собиралась %d раз, ожидался один рендер", n)
	}
}

// Части, положенные «соседом», снимает тот же сброс, что и страницы: иначе
// снятие тома оставляло бы в кэше текст всех частей, кроме запрошенной.
func TestHandlerPurgeDropsNeighborParts(t *testing.T) {
	src := longSource()
	books := src.Books.(*fakeBooks)
	h := handlerFor(src)
	get(t, h, llmChapter+"/part-2.md", nil)
	h.Purge()
	if rec := get(t, h, llmChapter+"/part-3.md", nil); rec.Code != http.StatusOK {
		t.Fatalf("часть 3 после сброса: код %d", rec.Code)
	}
	if n := atomic.LoadInt32(&books.calls); n != 2 {
		t.Errorf("после сброса глава собиралась %d раз всего, ожидалось 2", n)
	}
	// Повторный рендер снова разложил все части.
	get(t, h, llmChapter+".md", nil)
	if n := atomic.LoadInt32(&books.calls); n != 2 {
		t.Errorf("часть 1 после повторного рендера собрана заново (%d рендеров)", n)
	}
}

// purgingBooks снимает том посреди рендера: сброс приходит между снятием
// метки поколения и записью частей в кэш.
type purgingBooks struct {
	inner BookSource
	purge func()
}

func (p *purgingBooks) Chapter(ctx context.Context, workID, chapterID int64) (*book.Book, error) {
	p.purge()
	return p.inner.Chapter(ctx, workID, chapterID)
}

// Все части кладутся с меткой, снятой ДО рендера: рендер, начатый до снятия
// тома, не должен оставить в кэше ни одной части (см.
// TestPurgeBeatsARenderThatStartedBeforeIt для страниц).
func TestHandlerPurgeDuringRenderStoresNoParts(t *testing.T) {
	src := longSource()
	inner := src.Books.(*fakeBooks)
	h := handlerFor(src)
	src.Books = &purgingBooks{inner: inner, purge: func() { h.Purge() }}

	if rec := get(t, h, llmChapter+"/part-2.md", nil); rec.Code != http.StatusOK {
		t.Fatalf("часть 2: код %d", rec.Code)
	}
	for _, p := range []string{".md", "/part-2.md", "/part-3.md"} {
		if _, ok := h.cache.get(llmChapter + p); ok {
			t.Errorf("%s: часть легла в кэш из рендера, начатого до сброса", p)
		}
	}
	if rec := get(t, h, llmChapter+"/part-9.md", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("часть 9: код %d", rec.Code)
	}
	if _, ok := h.cache.get(llmChapter + "/part-9.md"); ok {
		t.Error("отказ по части 9 лёг в кэш из рендера, начатого до сброса")
	}
}

// Часть из кэша, положенная рендером соседней, отдаётся ровно так же, как
// при прямом рендере: тип, noindex, canonical, ETag и тело.
func TestHandlerNeighborPartMatchesDirectRender(t *testing.T) {
	for _, p := range []string{".md", "/part-3.md"} {
		direct := get(t, handlerFor(longSource()), llmChapter+p, nil)

		h := handlerFor(longSource())
		get(t, h, llmChapter+"/part-2.md", nil)
		neighbor := get(t, h, llmChapter+p, nil)

		if direct.Code != http.StatusOK || neighbor.Code != http.StatusOK {
			t.Fatalf("%s: коды %d и %d", p, direct.Code, neighbor.Code)
		}
		if !reflect.DeepEqual(direct.Header(), neighbor.Header()) {
			t.Errorf("%s: заголовки разошлись:\nпрямой  %v\nсосед   %v", p, direct.Header(), neighbor.Header())
		}
		if direct.Body.String() != neighbor.Body.String() {
			t.Errorf("%s: тело разошлось", p)
		}
		if neighbor.Header().Get("X-Robots-Tag") != "noindex" || neighbor.Header().Get("Link") == "" ||
			neighbor.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
			t.Errorf("%s: у части из кэша нет заголовков текста: %v", p, neighbor.Header())
		}
	}
}

func TestHandlerMalformedTextPathIs404(t *testing.T) {
	h := handlerFor(textSource())
	for _, path := range []string{
		llmChapter + "/part-0.md",
		llmChapter + "/part-x.md",
		llmChapter + "/part-.md",
		llmChapter + "/extra.md",
		"/works/abc.md",
		"/editions/7.md",
		"/works/1-lenin-t42/pages/5.md",
	} {
		if rec := get(t, h, path, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: код %d, ожидался 404", path, rec.Code)
		}
	}
}

func TestHandlerMissingTextIsGone(t *testing.T) {
	h := handlerFor(textSource())
	for _, path := range []string{
		"/works/1/chapters/999999.md",
		"/works/999999.md",
		// Глава 10 принадлежит тому 1: под номером тома 2 её текста нет.
		"/works/2/chapters/10.md",
	} {
		if rec := get(t, h, path, nil); rec.Code != http.StatusGone {
			t.Errorf("%s: код %d, ожидался 410", path, rec.Code)
		}
	}
}

// Текст главы стоит как рендер главы и идёт через ограничитель; оглавление
// тома и llms.txt — лёгкие.
func TestHandlerChapterTextIsHeavy(t *testing.T) {
	h := handlerFor(llmVolumeSource())
	for i := 0; i < cap(h.heavy); i++ {
		h.heavy <- struct{}{}
	}
	if rec := get(t, h, "/works/1-lenin-t42.md", nil); rec.Code != http.StatusOK {
		t.Errorf("оглавление тома при занятых слотах: код %d", rec.Code)
	}
	if rec := get(t, h, "/llms.txt", nil); rec.Code != http.StatusOK {
		t.Errorf("llms.txt при занятых слотах: код %d", rec.Code)
	}

	h2 := handlerFor(textSource())
	for i := 0; i < cap(h2.heavy); i++ {
		h2.heavy <- struct{}{}
	}
	if rec := get(t, h2, llmChapter+".md", nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("текст главы при занятых слотах: код %d, ожидался 503", rec.Code)
	}
}

func TestHandlerServesLLMsTxt(t *testing.T) {
	rec := get(t, handlerFor(llmVolumeSource()), "/llms.txt", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type: %q", ct)
	}
	if rec.Header().Get("X-Robots-Tag") != "" || rec.Header().Get("Link") != "" {
		t.Error("у llms.txt не должно быть noindex и canonical")
	}
	if !strings.HasPrefix(rec.Body.String(), "# Читальня") {
		t.Errorf("тело:\n%s", rec.Body.String())
	}
}
