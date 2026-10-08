package staticsite

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// runWith собирает фикстуру с настройками состава и отдаёт генератор и
// каталог сборки.
func runWith(t *testing.T, src *fakeSource, set func(g *Generator)) (*Generator, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out")
	g := &Generator{Src: src, Out: out, BaseURL: "https://lib.example.org", BuildDate: "2026-10-07"}
	set(g)
	if err := g.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return g, out
}

func TestBuiltListsCatalogAndFrontMatter(t *testing.T) {
	g, _ := runWith(t, fixture(), func(*Generator) {})
	catalog, all := g.Built()
	if fmt.Sprint(catalog) != "[100]" || fmt.Sprint(all) != "[100 103]" {
		t.Errorf("каталог %v, все %v; хотел [100] и [100 103]", catalog, all)
	}
}

func TestOnlyLeavesOutWorksMissingOnProd(t *testing.T) {
	// Передние листы 103 в списке есть, тома 100 — нет: без тома не идут и они.
	g, out := runWith(t, fixture(), func(g *Generator) { g.Only = map[int64]bool{103: true} })
	for _, p := range []string{vol1, ch10, frontIdx} {
		if exists(out, p) {
			t.Errorf("работы нет на боевом, а в архиве %s", p)
		}
	}
	if strings.Contains(read(t, out, HomeFile), "stalin-t01") {
		t.Error("главная ссылается на том, которого нет на боевом")
	}
	if catalog, all := g.Built(); len(catalog) != 0 || len(all) != 0 {
		t.Errorf("Built = %v, %v — хотел пусто", catalog, all)
	}

	g, out = runWith(t, fixture(), func(g *Generator) { g.Only = map[int64]bool{100: true, 103: true} })
	for _, p := range []string{vol1, ch10, frontIdx} {
		if !exists(out, p) {
			t.Errorf("работа есть на боевом, а %s в архиве нет", p)
		}
	}
	if _, all := g.Built(); fmt.Sprint(all) != "[100 103]" {
		t.Errorf("Built = %v, хотел [100 103]", all)
	}
}

func TestApparatusDropsItsSectionsAndFrontMatter(t *testing.T) {
	// Полоса 4 — вся глава 12: она уходит, глава 10 и безымянное начало
	// остаются; передние листы тома со снятым аппаратом не идут.
	g, out := runWith(t, fixture(), func(g *Generator) { g.Apparatus = map[int64]map[int]bool{100: {4: true}} })
	if exists(out, ch12) {
		t.Error("глава снятого аппарата попала в архив")
	}
	if exists(out, frontIdx) {
		t.Error("передние листы тома со снятым аппаратом попали в архив")
	}
	for _, p := range []string{vol1, ch10, gap1} {
		if !exists(out, p) {
			t.Errorf("тело тома пропало вместе с аппаратом: нет %s", p)
		}
	}
	if strings.Contains(read(t, out, vol1), "#p4") || strings.Contains(read(t, out, ch10), `id="p4"`) {
		t.Error("полоса снятого аппарата осталась в архиве")
	}
	if _, all := g.Built(); fmt.Sprint(all) != "[100]" {
		t.Errorf("Built = %v, хотел [100]", all)
	}
}

func TestApparatusInsideChapterFailsLoudly(t *testing.T) {
	// Полоса 3 — подглава 11 внутри главы 10 (полосы 2—3): вырезать её из
	// главы сборка не умеет и обязана отказать, а не оставить молча.
	g := &Generator{Src: fixture(), Out: filepath.Join(t.TempDir(), "out"), BuildDate: "2026-10-07",
		Apparatus: map[int64]map[int]bool{100: {3: true}}}
	err := g.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "аппарат") {
		t.Fatalf("смешанная глава собралась (ошибка %v)", err)
	}
}

func TestConceptArticlesFollowTheirVolume(t *testing.T) {
	withArticleFrom := func(work int64) *fakeSource {
		src := conceptFixture()
		src.concepts["dialektika"].Articles[0].WorkID = work
		// Понятие-сосед ссылается на «Диалектику»: ссылка живёт, только пока
		// та в архиве.
		src.concepts["anarhizm"].Articles = []ConceptArticle{{
			Links: []ConceptLink{{Kind: "see", Title: "Диалектика", Slug: "dialektika"}},
		}}
		return src
	}
	for name, set := range map[string]func(g *Generator){
		"том снят":           func(g *Generator) { g.Exclude = map[int64]bool{118: true} },
		"снят аппарат":       func(g *Generator) { g.Apparatus = map[int64]map[int]bool{118: {}} },
		"тома нет на боевом": func(g *Generator) { g.Only = map[int64]bool{100: true, 103: true} },
	} {
		t.Run(name, func(t *testing.T) {
			_, out := runWith(t, withArticleFrom(118), set)
			if exists(out, ConceptFile("dialektika")) {
				t.Error("статья ушла с томом, а понятие из одной этой статьи осталось")
			}
			if strings.Contains(read(t, out, ConceptsIndex), "Диалектика") {
				t.Error("понятие без статей осталось в азбуке указателя")
			}
			body := read(t, out, ConceptFile("anarhizm"))
			if strings.Contains(body, ConceptFile("dialektika")) || !strings.Contains(body, "Диалектика") {
				t.Errorf("ссылка «см.» на ушедшее понятие должна стать текстом: %s", body)
			}
		})
	}
	// Статья без тома и статья тома, который в архиве, идут.
	for _, work := range []int64{0, 100} {
		_, out := runWith(t, withArticleFrom(work), func(g *Generator) { g.Exclude = map[int64]bool{118: true} })
		if !exists(out, ConceptFile("dialektika")) {
			t.Errorf("статья тома %d пропала из архива", work)
		}
	}
}
