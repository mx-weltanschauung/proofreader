package seo

import (
	"strings"
	"testing"
)

// В названиях работ корпуса встречаются кавычки и угловые скобки
// («„Левые" о войне», «<Заметки>»). Не экранированные, они рвут атрибут
// content и уносят с собой остальные теги.
func TestRenderEscapesTitleAndDescription(t *testing.T) {
	out := Render(&Doc{
		Title:       `"Левые" о <войне> & мире`,
		Description: `Он сказал: "нет"`,
		Canonical:   "https://lib.example.org/works/1",
		OGType:      "book",
		Robots:      RobotsIndex,
	})

	if strings.Contains(out, `content="Он сказал: "нет""`) {
		t.Error("кавычка в описании не экранирована — атрибут разорван")
	}
	for _, bad := range []string{"<войне>", `"Левые"`} {
		if strings.Contains(out, bad) {
			t.Errorf("в выходе осталось неэкранированное %q", bad)
		}
	}
	if !strings.Contains(out, "&#34;Левые&#34; о &lt;войне&gt; &amp; мире") {
		t.Errorf("заголовок экранирован не полностью:\n%s", out)
	}
}

// Пустой ImageURL означает «карточки нет»: og:image с пустым значением
// Телеграм показывает как битую картинку, а не как её отсутствие.
func TestRenderOmitsImageTagsWhenNoImage(t *testing.T) {
	out := Render(&Doc{Title: "Том", Robots: RobotsIndex})

	for _, tag := range []string{"og:image", "twitter:card"} {
		if strings.Contains(out, tag) {
			t.Errorf("без картинки тега %s быть не должно", tag)
		}
	}
}

// JSON-LD вставляется внутрь <script>. Закрыть его изнутри значением нельзя:
// encoding/json превращает < в \u003c.
func TestRenderJSONLDCannotEscapeScript(t *testing.T) {
	out := Render(&Doc{
		Title:  "Том",
		Robots: RobotsIndex,
		JSONLD: map[string]string{"name": "</script><img src=x onerror=alert(1)>"},
	})

	if strings.Contains(out, "</script><img") {
		t.Error("значение JSON-LD вышло из тега script")
	}
	if !strings.Contains(out, `<script type="application/ld+json">`) {
		t.Error("блок JSON-LD не напечатан")
	}
	if !strings.Contains(out, `\u003c/script\u003e`) {
		t.Errorf("угловые скобки в значении JSON-LD не заэкранированы:\n%s", out)
	}
}

func TestRenderNoIndexRobots(t *testing.T) {
	out := Render(&Doc{Title: "Полоса 42", Robots: RobotsNoIndex})
	if !strings.Contains(out, `<meta name="robots" content="noindex,follow">`) {
		t.Errorf("не напечатан noindex:\n%s", out)
	}
}

// Описание берётся из текста главы: разметку надо снять, пробелы свести,
// сущности развернуть, а резать — по границе слова и по знакам, а не по
// байтам: на кириллице байтовая резка рвёт букву пополам.
func TestExcerptStripsMarkupAndCutsOnWordBoundary(t *testing.T) {
	got := Excerpt("<p>Государство&nbsp;и\n   революция</p><p>Учение марксизма о государстве</p>", 30)

	if strings.ContainsAny(got, "<>") {
		t.Errorf("разметка не снята: %q", got)
	}
	if !strings.HasPrefix(got, "Государство и революция") {
		t.Errorf("текст искажён: %q", got)
	}
	if strings.HasSuffix(got, " …") || !strings.HasSuffix(got, "…") {
		t.Errorf("обрезка должна кончаться отбивкой без пробела перед ней: %q", got)
	}
	if n := len([]rune(got)); n > 31 {
		t.Errorf("обрезка длиннее предела: %d знаков в %q", n, got)
	}
}

func TestExcerptShortTextKeptWhole(t *testing.T) {
	if got := Excerpt("<h1>Апрельские тезисы</h1>", 200); got != "Апрельские тезисы" {
		t.Errorf("короткий текст не должен обрезаться, получено %q", got)
	}
}

// Одинокая угловая скобка — знак сравнения, не начало тега. Её нельзя терять.
func TestExcerptPreservesComparisonSigns(t *testing.T) {
	got := Excerpt("Показано, что 5 < 10 > 3 при любом основании", 200)
	if !strings.Contains(got, "10") {
		t.Errorf("число 10 потеряно: %q", got)
	}
	if !strings.Contains(got, "3") {
		t.Errorf("число 3 потеряно: %q", got)
	}
}

// Незакрытый тег не съедает весь остаток строки — угловая скобка сама по себе
// может быть мусором распознавания.
func TestExcerptHandlesUnclosedBracket(t *testing.T) {
	got := Excerpt("Обрыв на <незакрытом теге", 200)
	if !strings.Contains(got, "Обрыв") {
		t.Errorf("текст перед скобкой потеряна: %q", got)
	}
	if !strings.Contains(got, "теге") {
		t.Errorf("текст после скобки съеден: %q", got)
	}
}

func TestRenderPrintsAlternateOnlyWhenSet(t *testing.T) {
	with := Render(&Doc{Title: "Т", Alternate: "https://x/a.md"})
	if !strings.Contains(with, `<link rel="alternate" type="text/plain" href="https://x/a.md">`) {
		t.Errorf("нет rel=alternate:\n%s", with)
	}
	if without := Render(&Doc{Title: "Т"}); strings.Contains(without, `rel="alternate"`) {
		t.Errorf("rel=alternate без Alternate:\n%s", without)
	}
}

func TestRenderDeclaresOPDSCatalog(t *testing.T) {
	out := Render(&Doc{Title: "Глава", Robots: RobotsIndex})
	head := out[:strings.Index(out, "</head>")]
	if !strings.Contains(head, `<link rel="related" type="application/atom+xml;profile=opds-catalog;kind=navigation" href="/opds"`) {
		t.Fatalf("в шапке нет ссылки на каталог OPDS:\n%s", head)
	}
}
