package staticsite

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"proofreader/internal/staticsite/sitecheck"
)

func TestVolumeEscapesSubchapterTitles(t *testing.T) {
	src := fixture()
	src.volumes[100].Sections[1].Blocks[1].Child.Title = "Метод & <b>жирно</b>"
	out := build(t, src)
	for _, p := range []string{vol1, ch10} {
		body := read(t, out, p)
		if !strings.Contains(body, "Метод &amp; &lt;b&gt;жирно&lt;/b&gt;") || strings.Contains(body, "<b>жирно</b>") {
			t.Errorf("%s: заглавие подглавы не экранировано", p)
		}
	}
}

func TestSiteCSSKeepsAuthorEmphasisInQuotes(t *testing.T) {
	css, err := assetFS.ReadFile(assetCSS)
	if err != nil {
		t.Fatal(err)
	}
	// Курсив в корпусе — выделение автора; курсив всей цитаты его прячет
	// (так же решено в SPA, frontend/src/pages/PageView.css).
	if regexp.MustCompile(`blockquote\s*\{[^}]*italic`).Match(css) {
		t.Error("цитата набрана курсивом целиком — выделение автора внутри неё пропадает")
	}
}

func TestCopyNamesRightsAndSource(t *testing.T) {
	out := build(t, fixture())
	mustContain(t, "подвал", read(t, out, HomeFile),
		`<a href="https://lib.example.org/legal#copyright">Права на тексты и снятие по жалобе</a>`)
	mustContain(t, "PROCHTI.txt", read(t, out, "PROCHTI.txt"),
		"https://lib.example.org/legal#copyright")
}

func TestRunRefusesNonEmptyOut(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "x"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	g := &Generator{Src: fixture(), Out: out}
	if err := g.Run(context.Background()); err == nil {
		t.Fatal("генератор писал в непустой каталог")
	}
}

func TestHomeAndEditionLinkVolumes(t *testing.T) {
	out := build(t, fixture())
	home := read(t, out, HomeFile)
	mustContain(t, "главная", home,
		`<a href="editions/stalin/index.html">И. В. Сталин. Сочинения</a>`,
		`<a href="works/100-stalin-t01/index.html">Том 1</a>`,
		`<link rel="stylesheet" href="assets/site.css">`,
		`<footer class="site"><p>Статическая копия читальни, собрана 2026-10-06.`,
		`href="https://lib.example.org/"`)
	ed := read(t, out, "editions/stalin/index.html")
	mustContain(t, "издание", ed,
		`<a href="../../works/100-stalin-t01/index.html">Том 1</a>`,
		`<link rel="stylesheet" href="../../assets/site.css">`,
		`Избранное: <a href="../../works/100-stalin-t01/10-anarhizm-ili-socializm.html#ch-11">Диалектический метод</a>`)
}

func TestAssetsAreLocal(t *testing.T) {
	out := build(t, fixture())
	for _, p := range []string{assetCSS, assetSearch, assetSearchPage, "assets/fonts/Literata-Regular.ttf",
		"assets/fonts/LICENSE-Literata.txt", "PROCHTI.txt"} {
		if !exists(out, p) {
			t.Errorf("нет %s", p)
		}
	}
	if readme := read(t, out, "PROCHTI.txt"); !strings.Contains(readme, "https://lib.example.org") || !strings.Contains(readme, "2026-10-06") {
		t.Errorf("PROCHTI.txt без адреса или даты:\n%s", readme)
	}
}

const (
	vol1     = "works/100-stalin-t01/index.html"
	ch10     = "works/100-stalin-t01/10-anarhizm-ili-socializm.html"
	ch12     = "works/100-stalin-t01/12-glava-i-kursiv-i.html"
	gap1     = "works/100-stalin-t01/vne-glav-1.html"
	frontIdx = "works/103-stalin-t01-front/index.html"
)

func TestVolumeSplitsIntoFiles(t *testing.T) {
	out := build(t, fixture())
	text := read(t, out, ch10)
	mustContain(t, "глава 10", text,
		`<h1 id="ch-10">Анархизм или социализм?</h1>`,
		`<h2 id="ch-11">Диалектический метод</h2>`,
		`id="p2"`, `id="p3"`, "Начало.", "Метод.",
		`<div class="text" data-pagefind-body>`,
		`data-pagefind-filter="издание[data-value]" data-value="И. В. Сталин. Сочинения"`,
		`<a href="#ch-11">Диалектический метод</a>`,
		`<a rel="prev" href="../../works/100-stalin-t01/vne-glav-1.html">`,
		`<a rel="next" href="../../`+ch12+`">`,
		`<nav class="crumbs"><a href="../../editions/stalin/index.html">И. В. Сталин. Сочинения</a> › <a href="../../works/100-stalin-t01/index.html">`,
		`href="https://lib.example.org/works/100-stalin-t01/chapters/10-anarhizm-ili-socializm"`)
	if strings.Contains(text, "Конец.") {
		t.Error("в файл главы 10 попал текст соседней главы")
	}
	if !exists(out, gap1) {
		t.Error("нет файла полос вне глав")
	}
	if exists(out, "works/100-stalin-t01/13-pustaya.html") {
		t.Error("секция без полос получила файл")
	}
}

func TestVolumeIndexListsFilesAndFrontMatter(t *testing.T) {
	out := build(t, fixture())
	idx := read(t, out, vol1)
	mustContain(t, "карточка тома", idx,
		`<a href="../../works/100-stalin-t01/vne-glav-1.html">«Без заглавия»</a> <span class="pages">с. 1</span>`,
		`<a href="../../works/100-stalin-t01/10-anarhizm-ili-socializm.html">Анархизм или социализм?</a> <span class="pages">с. 2—3</span>`,
		`<a href="../../works/100-stalin-t01/10-anarhizm-ili-socializm.html#ch-11">Диалектический метод</a>`,
		`<a href="../../works/103-stalin-t01-front/index.html">`,
		`Полос: 4. Вычитано людьми: 1, машиной: 2.`)
	if !exists(out, frontIdx) || !exists(out, "works/103-stalin-t01-front/vne-glav-1.html") {
		t.Error("передние листы не собраны")
	}
	if strings.Contains(read(t, out, HomeFile), "103-stalin-t01-front") {
		t.Error("передние листы попали на главную — на боевом они только с карточки тома")
	}
}

func TestVolumeEscapesTitles(t *testing.T) {
	out := build(t, fixture())
	idx := read(t, out, vol1)
	if !strings.Contains(idx, "Глава &amp; &lt;i&gt;курсив&lt;/i&gt;") {
		t.Errorf("заглавие с & и < не экранировано в оглавлении тома")
	}
	if strings.Contains(idx, "<i>курсив</i>") {
		t.Errorf("разметка из заглавия попала в страницу как разметка")
	}
	mustContain(t, "заголовок вкладки", read(t, out, ch12), "<title>Глава &amp; &lt;i&gt;курсив&lt;/i&gt; — ")
}

func TestEmptyVolumeLeavesNoTrace(t *testing.T) {
	out := build(t, fixture())
	for _, p := range []string{"works/101-stalin-t02/index.html", "works/102-stalin-t03/index.html"} {
		if exists(out, p) {
			t.Errorf("том без полос получил карточку %s", p)
		}
	}
	for _, p := range []string{HomeFile, "editions/stalin/index.html"} {
		if body := read(t, out, p); strings.Contains(body, "stalin-t02") || strings.Contains(body, "stalin-t03") {
			t.Errorf("%s ссылается на том без полос", p)
		}
	}
}

func TestFixtureBuildPassesSiteCheck(t *testing.T) {
	src := extrasFixture()
	c := conceptFixture()
	src.conceptList, src.concepts = c.conceptList, c.concepts
	out := build(t, src)
	problems, err := sitecheck.Run(sitecheck.Options{
		Dir: out, AllowedExternal: "https://lib.example.org/",
		Expected: map[int64][]int{100: {1, 2, 3, 4}, 103: {1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Error(p)
	}
}
