package book

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// -update-anchor-golden переписывает testdata/anchor_golden.html. Снимок
// снимается один раз, ДО появления ChapterID, и дальше сторожит, что
// выгрузки (секции без ChapterID) печатаются байт в байт прежними.
var updateAnchorGolden = flag.Bool("update-anchor-golden", false, "переписать testdata/anchor_golden.html")

// anchorFixture — глава с подглавой и безымянный хвост тома.
func anchorFixture() *Book {
	child := Section{Title: "Подглава", Blocks: []Block{
		{Pages: []Page{{Internal: 3, Printed: 103, HTML: "<p>Три.</p>"}}},
	}}
	return &Book{
		Meta: Meta{Title: "Том", Lang: "ru"},
		Sections: []Section{
			{Title: "Глава", Blocks: []Block{
				{Pages: []Page{{Internal: 2, Printed: 102, HTML: "<p>Два.</p>"}}},
				{Child: &child},
			}},
			{Title: "«Без заглавия»", Blocks: []Block{
				{Pages: []Page{{Internal: 4, Printed: 104, HTML: "<p>Четыре.</p>"}}},
			}},
		},
	}
}

func TestBodyHTMLWithoutChapterIDsIsUnchanged(t *testing.T) {
	got := BodyHTML(anchorFixture())
	golden := filepath.Join("testdata", "anchor_golden.html")
	if *updateAnchorGolden {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("нет снимка: %v (снять: go test ./pkg/book -run TestBodyHTMLWithoutChapterIDsIsUnchanged -update-anchor-golden)", err)
	}
	if got != string(want) {
		t.Errorf("вёрстка секций без ChapterID изменилась — выгрузки больше не байт в байт.\nбыло:\n%s\nстало:\n%s", want, got)
	}
	if !strings.Contains(got, `id="sec-1-1"`) {
		t.Errorf("в снимке нет позиционного якоря подглавы sec-1-1:\n%s", got)
	}
}

// withChapterIDs — та же фикстура, но с id глав: так её видит статическая
// читальня (DownloadSource.WithChapterAnchors).
func withChapterIDs() *Book {
	b := anchorFixture()
	b.Sections[0].ChapterID = 10
	b.Sections[0].Blocks[1].Child.ChapterID = 11
	return b
}

func TestChapterIDBecomesHeadingAnchor(t *testing.T) {
	got := BodyHTML(withChapterIDs())
	for _, want := range []string{
		`<h1 id="ch-10">Глава</h1>`,
		`<h2 id="ch-11">Подглава</h2>`,
		`<a href="#ch-10">Глава</a>`,
		`<a href="#ch-11">Подглава</a>`,
		// Безымянный хвост id главы не несёт и остаётся позиционным.
		`<section id="sec-2">`,
		`<a href="#sec-2">`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("нет %q в:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{`id="sec-1"`, `id="sec-1-1"`} {
		if strings.Contains(got, unwanted) {
			t.Errorf("у главы с ChapterID остался позиционный якорь %q:\n%s", unwanted, got)
		}
	}
}

func TestSectionHTMLPrintsOnlyTheSection(t *testing.T) {
	got := SectionHTML(withChapterIDs().Sections[0])
	for _, want := range []string{`<h1 id="ch-10">Глава</h1>`, "Два.", `<h2 id="ch-11">Подглава</h2>`, "Три.", `id="p102"`, `id="p103"`} {
		if !strings.Contains(got, want) {
			t.Errorf("нет %q в:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"titlepage", "<nav", "Четыре."} {
		if strings.Contains(got, unwanted) {
			t.Errorf("SectionHTML напечатал лишнее %q:\n%s", unwanted, got)
		}
	}
}

func TestBuildSectionCarriesChapterID(t *testing.T) {
	pages := Pages{{Internal: 1, Printed: 1}, {Internal: 2, Printed: 2}}
	sec := BuildSection(Node{Title: "Глава", ChapterID: 5, Start: 1, End: 2, Children: []Node{
		{Title: "Подглава", ChapterID: 6, Start: 2, End: 2},
	}}, pages)
	if sec.ChapterID != 5 {
		t.Errorf("ChapterID секции = %d, хотел 5", sec.ChapterID)
	}
	if len(sec.Blocks) != 2 || sec.Blocks[1].Child == nil || sec.Blocks[1].Child.ChapterID != 6 {
		t.Fatalf("подглава потеряла ChapterID: %+v", sec.Blocks)
	}
}

func TestAnchorHelpers(t *testing.T) {
	if got := ChapterAnchor(42); got != "ch-42" {
		t.Errorf("ChapterAnchor(42) = %q", got)
	}
	if got := PageAnchor(105); got != "p105" {
		t.Errorf("PageAnchor(105) = %q", got)
	}
}
