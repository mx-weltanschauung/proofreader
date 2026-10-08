package book

import "sort"

// Node — узел дерева глав на входе сборки. Границы внутренние.
type Node struct {
	Title  string
	Author string
	Start  int
	End    int
	// ChapterID — id главы; переезжает в Section.ChapterID (см. там).
	ChapterID int64
	Children  []Node
}

// BuildSection превращает узел дерева в секцию, чьи блоки покрывают каждую
// страницу диапазона ровно один раз.
//
// Наивная модель «родитель — только заголовок, текст только в листьях» на
// живой базе теряет 97 мест: у 71 родителя есть преамбула (шмуцтитул,
// эпиграф) до первого ребёнка, а между соседями встречаются 26 дыр. Поэтому
// собственные страницы родителя — такой же блок, как вложенная секция, и
// стоят до, между или после детей. Раскладка — общая с раскладкой глав
// верхнего уровня тома (см. ClipChildren ниже).
func BuildSection(n Node, pages Pages) Section {
	sec := Section{Title: n.Title, Author: n.Author, ChapterID: n.ChapterID}

	for _, step := range ClipChildren(n.Children, n.Start, n.End) {
		if step.Gap {
			if own := pages.Range(step.Start, step.End); len(own) > 0 {
				sec.Blocks = append(sec.Blocks, Block{Pages: own})
			}
			continue
		}
		sub := BuildSection(step.Node, pages)
		sec.Blocks = append(sec.Blocks, Block{Child: &sub})
	}
	return sec
}

// ClipStep — один шаг раскладки детей в границы родительского диапазона:
// либо Gap — страницы диапазона, не задетые ни одним ребёнком, либо Node —
// сам ребёнок, обрезанный по границам родителя.
type ClipStep struct {
	Gap        bool
	Start, End int
	Node       Node
}

// ClipChildren раскладывает детей узла в диапазон [start, end] родителя:
// пробелы вперемешку с обрезанными детьми, в порядке чтения, без потери и
// без дублирования ни одной страницы диапазона.
//
// Это общая часть BuildSection (блоки секции — собственные страницы
// вперемешку с вложенными секциями) и сборки тома в
// internal/api/download_source.go (главы верхнего уровня вперемешку с
// страницами, не попавшими ни в одну из них, — тоже самостоятельными
// секциями оглавления, а не блоками общего родителя). Что делать с
// пробелом — решает вызывающий: BuildSection превращает его в блок
// собственных страниц секции и пропускает пустой, вызывающий из
// internal/api даёт пробелу имя и делает из него узел дерева, у которого
// пустых не бывает (диапазон пробела всегда непуст по построению).
//
// Диапазон каждого ребёнка отсекается (клиппинг) курсором и границей
// родителя: start = max(child.Start, cursor), end = min(child.End, end).
// На живой базе 124 пересечения между соседями (120 из них — граница в одну
// страницу: a.end_page = b.start_page, обычная структура книги, не брак
// данных), поэтому без клиппинга страницы дублировались бы. Общая страница
// на границе достаётся первому по порядку чтения соседу. Ребёнок, целиком
// поглощённый предыдущим (start > end после отсечения), шага не создаёт
// вовсе.
func ClipChildren(children []Node, start, end int) []ClipStep {
	// buildChapterTree (internal/repository/chapter_repository.go) складывает
	// детей в порядке ListByWork, то есть по order_number. Курсор ниже идёт по
	// возрастанию страниц, и если эти два порядка разойдутся, он проскочит
	// назад и потеряет страницы. Сортируем по границе, а не полагаемся на
	// порядок вызывающего.
	sorted := append([]Node(nil), children...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Start < sorted[j].Start
	})

	var out []ClipStep
	cursor := start
	for _, child := range sorted {
		// Дети отсортированы по началу: начался за концом диапазона — дальше
		// только такие же. Без этой остановки пробел перед каждым из них
		// уезжал за end, и кусок тома получал пустые секции «Без заглавия»
		// по числу глав после него.
		if child.Start > end {
			break
		}
		if child.Start > cursor {
			out = append(out, ClipStep{Gap: true, Start: cursor, End: child.Start - 1})
			cursor = child.Start
		}

		s, e := max(child.Start, cursor), min(child.End, end)
		if s > e {
			// Ребёнок съеден целиком предыдущими (пересечение или диапазон,
			// вывернутый наизнанку) — шага для него не будет вовсе.
			continue
		}

		clipped := child
		clipped.Start, clipped.End = s, e
		out = append(out, ClipStep{Node: clipped})
		cursor = e + 1
	}
	if cursor <= end {
		out = append(out, ClipStep{Gap: true, Start: cursor, End: end})
	}
	return out
}
