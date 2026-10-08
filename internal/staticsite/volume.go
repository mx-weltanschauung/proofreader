package staticsite

import (
	"context"
	"fmt"
	"strings"

	"proofreader/internal/models"
	"proofreader/internal/seo"
	"proofreader/pkg/book"
	"proofreader/pkg/slug"
)

// workResult — отрисованная работа: её файлы и передние листы.
type workResult struct {
	work    *models.Work
	edition *models.Edition
	parent  *workResult // у передних листов — их том
	summary *models.VolumeSummary
	index   string
	files   []textFile
	front   []*workResult
}

// textFile — файл с текстом: глава верхнего уровня или полосы вне глав.
type textFile struct {
	path     string
	section  book.Section
	from, to int // печатные границы
}

// workWithFrontMatter рисует том, затем его передние листы, затем карточки
// обоих: карточке тома нужны ссылки на карточки передних листов.
func (g *Generator) workWithFrontMatter(ctx context.Context, w *models.Work, ed *models.Edition, summary *models.VolumeSummary) (*workResult, error) {
	res, err := g.renderWork(ctx, w, ed, nil)
	if err != nil || res == nil {
		return res, err
	}
	res.summary = summary
	fronts, err := g.Src.FrontMatter(ctx, w.ID)
	if err != nil {
		return nil, fmt.Errorf("передние листы работы %d: %w", w.ID, err)
	}
	for _, f := range fronts {
		fr, err := g.renderWork(ctx, f, ed, res)
		if err != nil {
			return nil, err
		}
		if fr == nil {
			continue
		}
		res.front = append(res.front, fr)
		if err := g.writeWorkIndex(fr); err != nil {
			return nil, err
		}
	}
	return res, g.writeWorkIndex(res)
}

// renderWork режет книгу работы на файлы — по секции верхнего уровня на файл
// — и пишет их. nil без ошибки — у работы нет ни одной полосы.
func (g *Generator) renderWork(ctx context.Context, w *models.Work, ed *models.Edition, parent *workResult) (*workResult, error) {
	if g.Exclude[w.ID] {
		g.logf("работа %d исключена из архива", w.ID)
		return nil, nil
	}
	if g.Only != nil && !g.Only[w.ID] {
		g.logf("работы %d нет на боевом — в архив не идёт", w.ID)
		return nil, nil
	}
	if parent != nil && g.Apparatus[parent.work.ID] != nil {
		g.logf("передние листы %d: у тома %d снят аппарат — в архив не идут", w.ID, parent.work.ID)
		return nil, nil
	}
	b, err := g.Src.Volume(ctx, w.ID)
	if err != nil {
		return nil, fmt.Errorf("работа %d: %w", w.ID, err)
	}
	if b == nil {
		return nil, nil
	}
	if pages := g.Apparatus[w.ID]; pages != nil {
		if b, err = withoutApparatus(b, pages); err != nil {
			return nil, fmt.Errorf("работа %d: %w", w.ID, err)
		}
	}
	res := &workResult{work: w, edition: ed, parent: parent, index: WorkIndex(w.ID, w.Slug)}
	for _, s := range b.Sections {
		first, last, ok := sectionBounds(s)
		if !ok {
			continue
		}
		p := GapFile(w.ID, w.Slug, first.Internal)
		if s.ChapterID != 0 {
			p = ChapterFile(w.ID, w.Slug, s.ChapterID, slug.Chapter(s.Title))
		}
		res.files = append(res.files, textFile{path: p, section: s, from: first.Printed, to: last.Printed})
	}
	if len(res.files) == 0 {
		return nil, nil
	}

	g.works[w.ID] = res.index
	if parent == nil {
		g.catalog = append(g.catalog, w.ID)
		g.addTitle("work", w.Title, editionTitle(ed), res.index)
	}
	for _, f := range res.files {
		g.register(w, f.path, f.section, true)
	}
	for i := range res.files {
		if err := g.writeTextFile(res, i); err != nil {
			return nil, err
		}
	}
	g.logf("работа %d: %d файлов", w.ID, len(res.files))
	return res, nil
}

func editionTitle(ed *models.Edition) string {
	if ed == nil {
		return ""
	}
	return ed.Title
}

// sectionBounds — первая и последняя полоса секции в порядке чтения.
func sectionBounds(s book.Section) (first, last book.Page, ok bool) {
	var walk func(s book.Section)
	walk = func(s book.Section) {
		for _, b := range s.Blocks {
			if b.Child != nil {
				walk(*b.Child)
				continue
			}
			for _, p := range b.Pages {
				if !ok {
					first, ok = p, true
				}
				last = p
			}
		}
	}
	walk(s)
	return first, last, ok
}

// register заносит в карты сборки полосы и главы секции: по ним ссылаются
// указатель, подборки, избранное и поиск.
func (g *Generator) register(w *models.Work, file string, s book.Section, top bool) {
	if s.ChapterID != 0 {
		target := file
		if !top {
			target = file + "#" + book.ChapterAnchor(s.ChapterID)
		}
		g.chapters[s.ChapterID] = target
		g.addTitle("chapter", s.Title, w.Title, target)
	}
	for _, b := range s.Blocks {
		if b.Child != nil {
			g.register(w, file, *b.Child, false)
			continue
		}
		for _, p := range b.Pages {
			key := pageKey{w.ID, p.Internal}
			if _, seen := g.pages[key]; !seen {
				g.pages[key] = pageLoc{file: file, printed: p.Printed}
			}
		}
	}
}

// topOf — том, к которому относится работа (сама работа или её родитель).
func topOf(res *workResult) *workResult {
	if res.parent != nil {
		return res.parent
	}
	return res
}

func workCrumbs(res *workResult, leaf bool) []crumb {
	var cs []crumb
	if ed := topOf(res).edition; ed != nil {
		cs = append(cs, crumb{ed.Title, EditionIndex(editionSlug(ed))})
	}
	if res.parent != nil {
		cs = append(cs, crumb{res.parent.work.Title, res.parent.index})
	}
	if leaf {
		cs = append(cs, crumb{res.work.Title, res.index})
	}
	return cs
}

func (g *Generator) writeTextFile(res *workResult, i int) error {
	f := res.files[i]
	w := res.work
	var b strings.Builder
	fmt.Fprintf(&b, `<p class="work-line"><a href="%s">%s</a>, с. %s</p>`+"\n",
		Esc(rel(f.path, res.index)), Esc(OneLine(w.Title)), printedSpan(f.from, f.to))

	var toc strings.Builder
	writeTocItems(&toc, f.section, "")
	if toc.Len() > 0 {
		b.WriteString(`<nav class="toc"><p>Содержание</p>` + "\n" + toc.String() + "</nav>\n")
	}

	b.WriteString(`<div class="text" data-pagefind-body>` + "\n")
	top := topOf(res)
	if top.edition != nil {
		fmt.Fprintf(&b, `<span data-pagefind-filter="издание[data-value]" data-value="%s"></span>`+"\n", Esc(OneLine(top.edition.Title)))
	}
	fmt.Fprintf(&b, `<span data-pagefind-filter="том[data-value]" data-value="%s"></span>`+"\n", Esc(OneLine(top.work.Title)))
	// Подпись тома в выдаче: глав «Предисловие» и «Примечания» в корпусе по
	// полторы сотни, и без тома результат не узнать.
	fmt.Fprintf(&b, `<span data-pagefind-meta="volume[data-value]" data-value="%s"></span>`+"\n", Esc(OneLine(top.work.Title)))
	// Номер полосы в отрывок выдачи не нужен («74 ПРОЕКТЫ…»).
	text := strings.ReplaceAll(cleanCorpusHTML(book.SectionHTML(f.section)),
		`<span class="page-marker" id=`, `<span class="page-marker" data-pagefind-ignore id=`)
	b.WriteString(text)
	b.WriteString("</div>\n")

	b.WriteString(`<nav class="prevnext">`)
	if i > 0 {
		p := res.files[i-1]
		fmt.Fprintf(&b, `<a rel="prev" href="%s">← %s</a>`, Esc(rel(f.path, p.path)), Esc(OneLine(p.section.Title)))
	}
	if i+1 < len(res.files) {
		n := res.files[i+1]
		fmt.Fprintf(&b, `<a rel="next" href="%s">%s →</a>`, Esc(rel(f.path, n.path)), Esc(OneLine(n.section.Title)))
	}
	b.WriteString("</nav>\n")

	prod := seo.PagePath(w.ID, w.Slug, firstInternal(f.section))
	if f.section.ChapterID != 0 {
		prod = seo.ChapterPath(w.ID, w.Slug, f.section.ChapterID, slug.Chapter(f.section.Title))
	}
	return g.write(f.path, g.page(shell{
		path:   f.path,
		title:  f.section.Title + " — " + w.Title,
		crumbs: workCrumbs(res, true),
		body:   b.String(),
		prod:   prod,
	}))
}

func firstInternal(s book.Section) int {
	first, _, _ := sectionBounds(s)
	return first.Internal
}

// writeTocItems печатает подглавы секции ссылками. prefix — путь файла
// секции относительно текущей страницы; пусто — секция на этой же странице.
func writeTocItems(b *strings.Builder, s book.Section, prefix string) {
	var kids []book.Section
	for _, blk := range s.Blocks {
		if blk.Child != nil && blk.Child.ChapterID != 0 {
			kids = append(kids, *blk.Child)
		}
	}
	if len(kids) == 0 {
		return
	}
	b.WriteString("<ul>\n")
	for _, k := range kids {
		fmt.Fprintf(b, `<li><a href="%s">%s</a>`, Esc(prefix+"#"+book.ChapterAnchor(k.ChapterID)), Esc(OneLine(k.Title)))
		writeTocItems(b, k, prefix)
		b.WriteString("</li>\n")
	}
	b.WriteString("</ul>\n")
}

func (g *Generator) writeWorkIndex(res *workResult) error {
	w := res.work
	var b strings.Builder
	fmt.Fprintf(&b, "<h1>%s</h1>\n", Esc(OneLine(w.Title)))
	if a := OneLine(w.Author); a != "" {
		fmt.Fprintf(&b, `<p class="author">%s</p>`+"\n", Esc(a))
	}
	if s := res.summary; s != nil {
		fmt.Fprintf(&b, `<p class="proofread">Полос: %d. Вычитано людьми: %d, машиной: %d.</p>`+"\n",
			s.PagesTotal, s.PagesByStatus[string(models.PageStatusProofread)],
			s.PagesByStatus[string(models.PageStatusMachineProofread)])
	}
	b.WriteString(`<nav class="toc"><h2>Содержание</h2>` + "\n<ul>\n")
	for _, f := range res.files {
		target := rel(res.index, f.path)
		fmt.Fprintf(&b, `<li><a href="%s">%s</a> <span class="pages">с. %s</span>`,
			Esc(target), Esc(OneLine(f.section.Title)), printedSpan(f.from, f.to))
		writeTocItems(&b, f.section, target)
		b.WriteString("</li>\n")
	}
	b.WriteString("</ul>\n</nav>\n")
	if len(res.front) > 0 {
		b.WriteString("<h2>Предваряющие материалы</h2>\n<ul>\n")
		for _, fr := range res.front {
			fmt.Fprintf(&b, `<li><a href="%s">%s</a></li>`+"\n", Esc(rel(res.index, fr.index)), Esc(OneLine(fr.work.Title)))
		}
		b.WriteString("</ul>\n")
	}
	return g.write(res.index, g.page(shell{
		path: res.index, title: w.Title, crumbs: workCrumbs(res, false),
		body: b.String(), prod: seo.WorkPath(w.ID, w.Slug),
	}))
}
