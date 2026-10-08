package staticsite

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"proofreader/internal/models"
	"proofreader/pkg/book"
)

func (g *Generator) writeConcepts(ctx context.Context) error {
	list, err := g.Src.ConceptList(ctx)
	if err != nil {
		return fmt.Errorf("список понятий: %w", err)
	}
	// Сначала все понятия читаются и сводятся к статьям сборки: понятие, у
	// которого не осталось ни одной, не пишется, и ссылки «см.» соседей должны
	// знать это заранее (g.concepts).
	var kept []ConceptEntry
	loaded := map[string]*Concept{}
	for _, c := range list {
		concept, err := g.Src.Concept(ctx, c.Slug)
		if err != nil {
			return fmt.Errorf("понятие %q: %w", c.Slug, err)
		}
		if concept == nil {
			return fmt.Errorf("понятие %q есть в списке, но не читается", c.Slug)
		}
		var arts []ConceptArticle
		for _, a := range concept.Articles {
			if g.articleInBuild(a.WorkID) {
				arts = append(arts, a)
			}
		}
		if len(arts) == 0 && len(concept.Articles) > 0 {
			g.logf("понятие %q: все статьи — из томов вне архива", c.Slug)
			continue
		}
		concept.Articles = arts
		loaded[c.Slug] = concept
		kept = append(kept, c)
	}
	for _, c := range kept {
		g.concepts[c.Slug] = true
	}
	for _, c := range kept {
		concept := loaded[c.Slug]
		p := ConceptFile(c.Slug)
		if err := g.write(p, g.page(shell{
			path:   p,
			title:  concept.Title + " — Предметный указатель",
			crumbs: []crumb{{"Предметный указатель", ConceptsIndex}},
			body:   g.conceptBody(p, concept),
			prod:   "/concepts/" + c.Slug,
		})); err != nil {
			return err
		}
		g.addTitle("concept", concept.Title, "Предметный указатель", p)
	}
	return g.writeConceptIndex(kept)
}

// articleInBuild — идёт ли в архив статья указателя из тома workID. Статья
// уходит вместе со своим томом: том вне сборки (нет на боевом, снят по
// жалобе) или со снятым аппаратом уносит её с собой — указатель тот же
// печатный объект, что и том. Статья без тома (workID 0) идёт всегда.
func (g *Generator) articleInBuild(workID int64) bool {
	switch {
	case workID == 0:
		return true
	case g.Exclude[workID], g.Apparatus[workID] != nil:
		return false
	case g.Only != nil && !g.Only[workID]:
		return false
	}
	return true
}

func (g *Generator) writeConceptIndex(list []ConceptEntry) error {
	var letters []string
	groups := map[string][]ConceptEntry{}
	for _, c := range list {
		r, _ := utf8.DecodeRuneInString(c.Title)
		l := string(unicode.ToUpper(r))
		if _, ok := groups[l]; !ok {
			letters = append(letters, l)
		}
		groups[l] = append(groups[l], c)
	}
	var b strings.Builder
	b.WriteString("<h1>Предметный указатель</h1>\n<p>")
	for i, l := range letters {
		fmt.Fprintf(&b, `<a href="#l-%d">%s</a> `, i+1, Esc(l))
	}
	b.WriteString("</p>\n")
	for i, l := range letters {
		fmt.Fprintf(&b, `<h2 id="l-%d">%s</h2>`+"\n<ul>\n", i+1, Esc(l))
		for _, c := range groups[l] {
			fmt.Fprintf(&b, `<li><a href="%s">%s</a></li>`+"\n", Esc(rel(ConceptsIndex, ConceptFile(c.Slug))), Esc(OneLine(c.Title)))
		}
		b.WriteString("</ul>\n")
	}
	return g.write(ConceptsIndex, g.page(shell{path: ConceptsIndex, title: "Предметный указатель", body: b.String(), prod: "/concepts"}))
}

func (g *Generator) conceptBody(p string, c *Concept) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<h1>%s</h1>\n", Esc(OneLine(c.Title)))
	for _, a := range c.Articles {
		if len(c.Articles) > 1 && a.Edition != "" {
			fmt.Fprintf(&b, "<h2>%s</h2>\n", Esc(OneLine(a.Edition)))
		}
		if a.HTML != "" {
			b.WriteString(`<div class="article">` + a.HTML + "</div>\n")
		}
		g.writeRubrics(&b, p, buildRubricTree(a.Refs))
		g.writeConceptLinks(&b, p, a.Links)
	}
	return b.String()
}

// rubricNode — подрубрика: адреса прямо под ней и вложенные подрубрики в
// порядке первого появления.
type rubricNode struct {
	title    string
	refs     []ConceptRef
	children []*rubricNode
	byTitle  map[string]*rubricNode
}

func buildRubricTree(refs []ConceptRef) *rubricNode {
	root := &rubricNode{byTitle: map[string]*rubricNode{}}
	for _, r := range refs {
		n := root
		for _, t := range r.Path {
			child, ok := n.byTitle[t]
			if !ok {
				child = &rubricNode{title: t, byTitle: map[string]*rubricNode{}}
				n.byTitle[t] = child
				n.children = append(n.children, child)
			}
			n = child
		}
		n.refs = append(n.refs, r)
	}
	return root
}

func (g *Generator) writeRubrics(b *strings.Builder, p string, n *rubricNode) {
	if len(n.refs) > 0 {
		b.WriteString(`<p class="refs">` + g.refsHTML(p, n.refs) + "</p>\n")
	}
	if len(n.children) == 0 {
		return
	}
	b.WriteString(`<ul class="rubrics">` + "\n")
	for _, c := range n.children {
		fmt.Fprintf(b, `<li><span class="rubric">%s</span>`, Esc(OneLine(c.title)))
		g.writeRubrics(b, p, c)
		b.WriteString("</li>\n")
	}
	b.WriteString("</ul>\n")
}

// refsHTML — адреса через точку с запятой. Ссылкой становится только адрес,
// чья полоса есть в сборке: адрес на том вне читальни или на номер, которого
// в томе нет (дыра скана), остаётся текстом.
func (g *Generator) refsHTML(p string, refs []ConceptRef) string {
	parts := make([]string, 0, len(refs))
	for _, r := range refs {
		label := Esc(r.Label)
		if r.Uncertain {
			label += "?"
		}
		if loc, ok := g.pages[pageKey{r.WorkID, r.Page}]; ok && r.WorkID != 0 {
			label = fmt.Sprintf(`<a href="%s">%s</a>`, Esc(rel(p, loc.file+"#"+book.PageAnchor(loc.printed))), label)
		}
		if r.Note != "" {
			label += " (" + Esc(OneLine(r.Note)) + ")"
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, "; ")
}

func (g *Generator) writeConceptLinks(b *strings.Builder, p string, links []ConceptLink) {
	for _, kind := range []struct{ key, label string }{
		{models.IndexLinkKindSee, "См.: "}, {models.IndexLinkKindSeeAlso, "См. также: "},
	} {
		var items []string
		for _, l := range links {
			if l.Kind != kind.key {
				continue
			}
			if l.Slug != "" && g.concepts[l.Slug] {
				items = append(items, fmt.Sprintf(`<a href="%s">%s</a>`, Esc(rel(p, ConceptFile(l.Slug))), Esc(OneLine(l.Title))))
				continue
			}
			items = append(items, Esc(OneLine(l.Title)))
		}
		if len(items) > 0 {
			b.WriteString(`<p class="see">` + kind.label + strings.Join(items, ", ") + "</p>\n")
		}
	}
}
