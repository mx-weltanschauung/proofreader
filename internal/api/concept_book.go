package api

import (
	"context"
	"fmt"
	"strings"

	"proofreader/internal/models"
	"proofreader/internal/seo"
	"proofreader/pkg/book"
)

// Понятие указателя книгой — для текста нейросетям (/concepts/{слаг}.md,
// internal/seo/render_concept_llm.go). Спека —
// docs/superpowers/specs/2026-10-02-concept-for-llm-design.md.
//
// Порядок мест — порядок потока понятия по подрубрикам (sortEntries), но в
// книгу входят и адреса, которых поток не показывает: том не загружен, полос
// нет. Модель, не увидевшая адреса, сочла бы, что его нет и в указателе.
// Полоса печатается один раз: под вторым адресом — ссылка на первый (треть
// адресных полос корпуса повторяется внутри своего понятия). Вырезки не
// читаются: .md отдаёт полосы целиком.

// ConceptIndex — то, что сборке нужно от указателя.
type ConceptIndex interface {
	GetConceptBySlug(ctx context.Context, slug string) (*models.IndexConcept, error)
	VolumeMap(ctx context.Context, editionID int64) ([]models.VolumeLocation, error)
}

// ConceptPageStore — тела полос пачкой, по одному запросу на том.
type ConceptPageStore interface {
	GetPagesByNumbers(ctx context.Context, workID int64, numbers []int) ([]*models.Page, error)
}

type ConceptBookSource struct {
	index    ConceptIndex
	pages    ConceptPageStore
	chapters ChapterTreeStore
	baseURL  string
}

// referenceVolume — «т. 43» или «т. 45, 2»: так том назван в самом указателе.
func referenceVolume(ref *models.IndexReference) string {
	label := fmt.Sprintf("т. %d", ref.VolumeNumber)
	if ref.VolumePart != nil && *ref.VolumePart != "" {
		label += ", " + *ref.VolumePart
	}
	return label
}

func NewConceptBookSource(index ConceptIndex, pages ConceptPageStore, chapters ChapterTreeStore, baseURL string) *ConceptBookSource {
	return &ConceptBookSource{index: index, pages: pages, chapters: chapters, baseURL: strings.TrimSuffix(baseURL, "/")}
}

// conceptWork — полосы и главы одного тома понятия.
type conceptWork struct {
	pages    map[int]*models.Page
	chapters []*models.Chapter
}

type pageKey struct {
	work int64
	page int
}

func (s *ConceptBookSource) Concept(ctx context.Context, slug string, rubric []string) (*seo.ConceptBook, error) {
	c, err := s.index.GetConceptBySlug(ctx, slug)
	if err != nil && !seo.IsNotFound(err) {
		return nil, fmt.Errorf("понятие %q: %w", slug, err)
	}
	if err != nil || c == nil {
		return nil, fmt.Errorf("%w: понятие %q", seo.ErrNotFound, slug)
	}
	all, locs, err := conceptAddresses(ctx, s.index, c)
	if err != nil {
		return nil, fmt.Errorf("тома понятия %q: %w", slug, err)
	}
	// Подрубрика сужает по префиксу пути — тем же hasPathPrefix, что
	// fragmentFilter потока понятия: ссылка со страницы понятия и её .md
	// обязаны показать одни и те же адреса.
	inRubric := func(ref *models.IndexReference) bool {
		return len(rubric) == 0 || hasPathPrefix(rubricPathOf(ref), rubric)
	}
	var refs []*models.IndexReference
	for _, ref := range all {
		if inRubric(ref) {
			refs = append(refs, ref)
		}
	}
	if len(rubric) > 0 && len(refs) == 0 {
		return nil, fmt.Errorf("%w: понятие %q, подрубрика %q", seo.ErrNoSuchRubric, slug, rubric)
	}

	slots := make(map[int64]entrySlot, len(refs))
	for _, ref := range refs {
		if loc, ok := locs[ref.ID]; ok {
			if slot, ok := slotFor(ref, loc); ok {
				slots[ref.ID] = slot
			}
		}
	}
	works, err := s.loadWorks(ctx, slots)
	if err != nil {
		return nil, err
	}

	cb := &seo.ConceptBook{
		Concept:  c,
		Book:     &book.Book{Meta: book.Meta{Title: c.Title, Lang: "ru"}},
		Places:   len(refs),
		Modified: c.UpdatedAt,
	}
	// Статья без адресов (отсылка «см. …» другого указателя) раздела в
	// книге не получает: пустой заголовок издания без мест сбивал бы модель.
	// Под издание заворачивается, только когда статей с адресами несколько.
	var placed []*models.IndexArticle
	for _, a := range c.Articles {
		for _, ref := range a.References {
			if inRubric(ref) {
				placed = append(placed, a)
				break
			}
		}
	}
	b := &conceptBuilder{src: s, cb: cb, locs: locs, works: works, seen: map[pageKey]string{}}
	for _, a := range placed {
		root := b.article(a, slots, inRubric)
		if len(placed) > 1 {
			cb.Book.Sections = append(cb.Book.Sections, book.Section{Title: a.EditionTitle, Blocks: root.Blocks})
			continue
		}
		for _, blk := range root.Blocks {
			cb.Book.Sections = append(cb.Book.Sections, *blk.Child)
		}
	}
	return cb, nil
}

// loadWorks — тела полос и деревья глав всех томов понятия: один запрос
// полос и один глав на том, а не на адрес (у самого большого понятия 3479
// адресов).
func (s *ConceptBookSource) loadWorks(ctx context.Context, slots map[int64]entrySlot) (map[int64]conceptWork, error) {
	numbers := map[int64]map[int]bool{}
	for _, slot := range slots {
		if numbers[slot.WorkID] == nil {
			numbers[slot.WorkID] = map[int]bool{}
		}
		for _, n := range slot.PageNumbers {
			numbers[slot.WorkID][n] = true
		}
	}
	out := make(map[int64]conceptWork, len(numbers))
	for workID, set := range numbers {
		wanted := make([]int, 0, len(set))
		for n := range set {
			wanted = append(wanted, n)
		}
		pages, err := s.pages.GetPagesByNumbers(ctx, workID, wanted)
		if err != nil {
			return nil, fmt.Errorf("полосы тома %d: %w", workID, err)
		}
		chapters, err := s.chapters.ListByWorkHierarchical(ctx, workID)
		if err != nil {
			return nil, fmt.Errorf("главы тома %d: %w", workID, err)
		}
		byNumber := make(map[int]*models.Page, len(pages))
		for _, p := range pages {
			byNumber[p.PageNumber] = p
		}
		out[workID] = conceptWork{pages: byNumber, chapters: chapters}
	}
	return out, nil
}

// conceptBuilder раскладывает адреса статьи по секциям подрубрик.
type conceptBuilder struct {
	src     *ConceptBookSource
	cb      *seo.ConceptBook
	locs    map[int64]models.VolumeLocation
	works   map[int64]conceptWork
	seen    map[pageKey]string // полоса, уже напечатанная, → подпись её места
	pageSeq int                // полос в книге до текущей
	addr    int                // адресов в книге до текущего
	// nodes и rubricAt — секции и строки оглавления текущей статьи по ключу
	// пути (pathRankKey).
	nodes    map[string]*book.Section
	rubricAt map[string]int
}

// article — секции одной статьи под корнем-заглушкой: все блоки корня —
// вложенные секции (подрубрики и безрубричные адреса). keep — адреса,
// попавшие под подрубрику; ранги групп при этом считаются по ВСЕМ адресам
// статьи, как в потоке, иначе порядок ездил бы от фильтра.
func (b *conceptBuilder) article(a *models.IndexArticle, slots map[int64]entrySlot, keep func(*models.IndexReference) bool) *book.Section {
	b.nodes, b.rubricAt = map[string]*book.Section{}, map[string]int{}
	root := &book.Section{}

	entries := make([]entrySlot, 0, len(a.References))
	for _, ref := range a.References {
		if !keep(ref) {
			continue
		}
		slot, ok := slots[ref.ID]
		if !ok {
			// Тома нет или полос нет — место остаётся, без текста. Границы
			// печатные, как в указателе.
			end := ref.PageEnd
			if end < ref.PageStart {
				end = ref.PageStart
			}
			slot = entrySlot{Reference: ref, PrintedStart: ref.PageStart, PrintedEnd: end}
		}
		entries = append(entries, slot)
	}
	sortEntries(entries, rubricRanks(a.References), orderByRubric)

	for _, e := range entries {
		path := rubricPathOf(e.Reference)
		parent := b.node(root, a.EditionTitle, path)
		sec, first := b.place(e)
		parent.Blocks = append(parent.Blocks, book.Block{Child: sec})
		// Адреса без подрубрики — одна строка оглавления с пустым путём,
		// заводится на первом таком адресе (путь пуст — ключ ""): места
		// считаются в сводке, и без строки их не нашла бы ни модель, ни
		// номер части.
		keys := make([]string, 0, len(path)+1)
		if len(path) == 0 {
			if _, ok := b.rubricAt[""]; !ok {
				b.cb.Rubrics = append(b.cb.Rubrics, seo.ConceptRubric{Edition: a.EditionTitle, FirstPage: -1})
				b.rubricAt[""] = len(b.cb.Rubrics) - 1
			}
			keys = append(keys, "")
		}
		for i := 1; i <= len(path); i++ {
			keys = append(keys, pathRankKey(path[:i]))
		}
		for _, key := range keys {
			r := &b.cb.Rubrics[b.rubricAt[key]]
			r.Places++
			if !containsInt(r.Volumes, e.Reference.VolumeNumber) {
				r.Volumes = append(r.Volumes, e.Reference.VolumeNumber)
			}
			if r.FirstPage < 0 && first >= 0 {
				r.FirstPage = first
			}
		}
	}
	return root
}

// node — секция подрубрики по пути; недостающие звенья заводятся при первом
// появлении — вместе со строкой оглавления.
func (b *conceptBuilder) node(root *book.Section, edition string, path []string) *book.Section {
	parent := root
	for i := range path {
		key := pathRankKey(path[:i+1])
		n, ok := b.nodes[key]
		if !ok {
			n = &book.Section{Title: path[i]}
			parent.Blocks = append(parent.Blocks, book.Block{Child: n})
			b.nodes[key] = n
			b.cb.Rubrics = append(b.cb.Rubrics, seo.ConceptRubric{
				Edition: edition, Path: append([]string(nil), path[:i+1]...), FirstPage: -1,
			})
			b.rubricAt[key] = len(b.cb.Rubrics) - 1
		}
		parent = n
	}
	return parent
}

// place — секция одного адреса и порядковый номер её первой полосы (-1 —
// полос нет).
func (b *conceptBuilder) place(e entrySlot) (*book.Section, int) {
	ref := e.Reference
	label := referenceVolume(ref)
	if _, inLibrary := b.locs[ref.ID]; !inLibrary {
		return &book.Section{Title: label + ", с. " + printedSpan(e.PrintedStart, e.PrintedEnd) + " — тома нет в читальне"}, -1
	}
	loc := b.locs[ref.ID]
	w := b.works[e.WorkID]
	var present []*models.Page
	for _, n := range e.PageNumbers {
		if p, ok := w.pages[n]; ok {
			present = append(present, p)
		}
	}
	if len(present) == 0 {
		return &book.Section{Title: label + ", с. " + printedSpan(e.PrintedStart, e.PrintedEnd) + " — этих страниц нет в читальне"}, -1
	}

	b.addr++
	b.cb.Present++
	first, last := present[0].PageNumber, present[len(present)-1].PageNumber
	span := label + ", с. " + printedSpan(first+loc.PageOffset, last+loc.PageOffset)
	title := "[" + span + "](" + b.src.baseURL + seo.PagePath(loc.WorkID, loc.WorkSlug, first) + ")"
	if ch := deepestChapterOf(w.chapters, first); ch != nil {
		title += " — " + ch.Title
	}
	where := span
	if ref.Rubric != "" {
		where = "«" + ref.Rubric + "», " + span
	}

	firstSeq := b.pageSeq
	pages := make([]book.Page, 0, len(present))
	for _, p := range present {
		key := pageKey{e.WorkID, p.PageNumber}
		md := ""
		if prev, dup := b.seen[key]; dup {
			md = "Текст страницы приведён выше: " + prev + "."
		} else {
			b.seen[key] = where
			b.cb.Pages++
			// Префикс адреса: печатная 46 двух томов иначе дала бы два
			// определения [^46-1] в одном документе.
			md = rewriteFootnoteAnchorsMarkdown(p.ContentMarkdown, fmt.Sprintf("a%d-", b.addr))
		}
		if p.UpdatedAt.After(b.cb.Modified) {
			b.cb.Modified = p.UpdatedAt
		}
		pages = append(pages, book.Page{Internal: p.PageNumber, Printed: p.PageNumber + loc.PageOffset, Markdown: md})
		b.pageSeq++
	}
	return &book.Section{Title: title, Blocks: []book.Block{{Pages: pages}}}, firstSeq
}

// printedSpan — «46» или «46—47».
func printedSpan(from, to int) string {
	if to <= from {
		return fmt.Sprint(from)
	}
	return fmt.Sprintf("%d—%d", from, to)
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
