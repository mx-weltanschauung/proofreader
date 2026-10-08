package staticsite

import (
	"strings"
	"testing"
)

func conceptFixture() *fakeSource {
	src := fixture()
	src.conceptList = []ConceptEntry{{Slug: "dialektika", Title: "Диалектика"}, {Slug: "anarhizm", Title: "Анархизм"}}
	src.concepts = map[string]*Concept{
		"anarhizm": {Title: "Анархизм"},
		"dialektika": {Title: "Диалектика", Articles: []ConceptArticle{{
			Edition: "И. В. Сталин. Сочинения",
			HTML:    "<p>Статья о <strong>методе</strong>.</p>",
			Refs: []ConceptRef{
				{Label: "т. 1, с. 2", WorkID: 100, Page: 2},
				{Path: []string{"Метод"}, Label: "т. 1, с. 3", WorkID: 100, Page: 3, Uncertain: true},
				{Path: []string{"Метод", "и материализм"}, Label: "т. 1, с. 99", WorkID: 100, Page: 99},
				{Path: []string{"Метод"}, Label: "т. 9, с. 5", Note: "в черновике"},
			},
			Links: []ConceptLink{
				{Kind: "see", Title: "Анархизм", Slug: "anarhizm"},
				{Kind: "see_also", Title: "Материализм", Slug: "materializm"},
			},
		}}},
	}
	return src
}

func TestConceptLinksToPageAnchor(t *testing.T) {
	out := build(t, conceptFixture())
	p := ConceptFile("dialektika")
	body := read(t, out, p)
	mustContain(t, "понятие", body,
		"<h1>Диалектика</h1>",
		"<p>Статья о <strong>методе</strong>.</p>",
		`<a href="../works/100-stalin-t01/10-anarhizm-ili-socializm.html#p2">т. 1, с. 2</a>`,
		`<span class="rubric">Метод</span>`,
		`<a href="../works/100-stalin-t01/10-anarhizm-ili-socializm.html#p3">т. 1, с. 3?</a>`,
		`<span class="rubric">и материализм</span>`,
		`См.: <a href="../`+ConceptFile("anarhizm")+`">Анархизм</a>`,
		"См. также: Материализм")
	if n := strings.Count(body, `<span class="rubric">Метод</span>`); n != 1 {
		t.Errorf("подрубрика «Метод» напечатана %d раз — адреса под одной подрубрикой разъехались", n)
	}
}

func TestConceptRefToMissingPageIsText(t *testing.T) {
	out := build(t, conceptFixture())
	body := read(t, out, ConceptFile("dialektika"))
	// Под подрубрикой «и материализм» один адрес — он обязан стоять голым
	// текстом, а не внутри <a> (куда бы ссылка ни вела).
	if !strings.Contains(body, `<p class="refs">т. 1, с. 99</p>`) {
		t.Errorf("адрес на отсутствующую полосу должен остаться текстом без ссылки")
	}
	mustContain(t, "неразрешённый адрес", body, "т. 9, с. 5 (в черновике)")
}

func TestConceptIndexByLetter(t *testing.T) {
	out := build(t, conceptFixture())
	idx := read(t, out, ConceptsIndex)
	mustContain(t, "азбука", idx,
		`<h2 id="l-1">Д</h2>`, `<h2 id="l-2">А</h2>`,
		`<a href="../`+ConceptFile("dialektika")+`">Диалектика</a>`)
}
