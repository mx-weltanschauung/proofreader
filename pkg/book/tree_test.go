package book

import (
	"reflect"
	"testing"
)

// pagesFor собирает Pages на внутренние номера from..to. Печатный номер
// отличается на offset — ровно как works.page_offset в базе.
func pagesFor(from, to, offset int) Pages {
	var ps Pages
	for i := from; i <= to; i++ {
		ps = append(ps, Page{Internal: i, Printed: i + offset})
	}
	return ps
}

// collectInternal обходит секцию и собирает внутренние номера всех страниц в
// порядке появления — включая страницы вложенных секций.
func collectInternal(s Section) []int {
	var out []int
	for _, b := range s.Blocks {
		if b.Child != nil {
			out = append(out, collectInternal(*b.Child)...)
			continue
		}
		for _, p := range b.Pages {
			out = append(out, p.Internal)
		}
	}
	return out
}

// Главный инвариант: каждая страница диапазона попадает ровно в одну секцию.
// Дерево нарочно содержит все три случая, встречающиеся в корпусе:
// преамбулу до первого ребёнка (71 случай), дыру между соседями (26 случаев)
// и хвост после последнего ребёнка.
func TestBuildSectionCoversEveryPageExactlyOnce(t *testing.T) {
	root := Node{
		Title: "Том", Start: 1, End: 20,
		Children: []Node{
			{Title: "Глава 1", Start: 4, End: 8},
			{Title: "Глава 2", Start: 12, End: 16},
		},
	}

	got := collectInternal(BuildSection(root, pagesFor(1, 20, 0)))

	want := make([]int, 0, 20)
	for i := 1; i <= 20; i++ {
		want = append(want, i)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("покрытие страниц = %v, хотел %v", got, want)
	}
}

func TestBuildSectionKeepsPreamble(t *testing.T) {
	root := Node{
		Title: "Том", Start: 1, End: 10,
		Children: []Node{{Title: "Глава", Start: 3, End: 10}},
	}

	sec := BuildSection(root, pagesFor(1, 10, 0))

	if len(sec.Blocks) != 2 {
		t.Fatalf("блоков = %d, хотел 2 (преамбула и глава)", len(sec.Blocks))
	}
	if sec.Blocks[0].Child != nil {
		t.Fatalf("первым блоком идёт вложенная секция, а не преамбула")
	}
	if got := collectInternal(Section{Blocks: sec.Blocks[:1]}); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Errorf("преамбула = %v, хотел [1 2]", got)
	}
}

func TestBuildSectionFillsGapBetweenSiblings(t *testing.T) {
	root := Node{
		Title: "Том", Start: 1, End: 9,
		Children: []Node{
			{Title: "Первая", Start: 1, End: 3},
			{Title: "Вторая", Start: 7, End: 9},
		},
	}

	sec := BuildSection(root, pagesFor(1, 9, 0))

	if len(sec.Blocks) != 3 {
		t.Fatalf("блоков = %d, хотел 3 (глава, дыра, глава)", len(sec.Blocks))
	}
	if sec.Blocks[1].Child != nil {
		t.Fatalf("вторым блоком идёт секция, а не страницы дыры")
	}
	if got := collectInternal(Section{Blocks: sec.Blocks[1:2]}); !reflect.DeepEqual(got, []int{4, 5, 6}) {
		t.Errorf("дыра = %v, хотел [4 5 6]", got)
	}
}

// buildChapterTree в репозитории раскладывает детей в порядке ListByWork, а не
// по start_page. Если эти порядки разойдутся, наивный курсор пропустит
// страницы. Сортировка обязана быть внутри BuildSection.
func TestBuildSectionSortsChildrenByStartPage(t *testing.T) {
	root := Node{
		Title: "Том", Start: 1, End: 10,
		Children: []Node{
			{Title: "Вторая", Start: 6, End: 10},
			{Title: "Первая", Start: 1, End: 5},
		},
	}

	sec := BuildSection(root, pagesFor(1, 10, 0))

	got := collectInternal(sec)
	want := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("покрытие = %v, хотел %v", got, want)
	}
	if sec.Blocks[0].Child == nil || sec.Blocks[0].Child.Title != "Первая" {
		t.Errorf("первой идёт не «Первая», дети не отсортированы")
	}
}

func TestBuildSectionNestsGrandchildren(t *testing.T) {
	root := Node{
		Title: "Том", Start: 1, End: 10,
		Children: []Node{{
			Title: "Часть", Start: 1, End: 10,
			Children: []Node{{Title: "Глава", Start: 5, End: 10}},
		}},
	}

	sec := BuildSection(root, pagesFor(1, 10, 0))

	part := sec.Blocks[0].Child
	if part == nil {
		t.Fatalf("часть не стала вложенной секцией")
	}
	if part.Blocks[1].Child == nil || part.Blocks[1].Child.Title != "Глава" {
		t.Fatalf("глава не вложена в часть")
	}
	// Точное сравнение, а не счёт: дублирование одной страницы и потеря другой
	// дают то же число 10, но неверный порядок/состав.
	got := collectInternal(sec)
	want := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("покрытие = %v, хотел %v", got, want)
	}
}

// Пустой блок не создаётся: у главы, начинающейся ровно на первой странице
// родителя, преамбулы нет.
func TestBuildSectionSkipsEmptyBlocks(t *testing.T) {
	root := Node{
		Title: "Том", Start: 1, End: 5,
		Children: []Node{{Title: "Глава", Start: 1, End: 5}},
	}

	sec := BuildSection(root, pagesFor(1, 5, 0))

	if len(sec.Blocks) != 1 {
		t.Errorf("блоков = %d, хотел 1 (пустая преамбула не нужна)", len(sec.Blocks))
	}
}

// Страницы, которых нет в базе (дыра в нумерации), не выдумываются.
func TestBuildSectionToleratesMissingPages(t *testing.T) {
	pages := Pages{{Internal: 1, Printed: 1}, {Internal: 4, Printed: 4}}
	root := Node{Title: "Том", Start: 1, End: 4}

	got := collectInternal(BuildSection(root, pages))

	if !reflect.DeepEqual(got, []int{1, 4}) {
		t.Errorf("покрытие = %v, хотел [1 4]", got)
	}
}

func TestPagesRange(t *testing.T) {
	pages := pagesFor(1, 10, 100)

	got := pages.Range(3, 5)

	want := []Page{
		{Internal: 3, Printed: 103},
		{Internal: 4, Printed: 104},
		{Internal: 5, Printed: 105},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Range(3, 5) = %v, хотел %v", got, want)
	}
}

// ---- Пересечения соседей (клиппинг) ----
//
// На живой базе 124 пересечения между соседями, из них 120 — ровно на одну
// страницу: a.end_page = b.start_page, глава кончается на той же странице,
// с которой начинается следующая. Это обычная структура книги, а не брак
// данных, поэтому курсор обязан отсекать (клиппинг) диапазон каждого
// ребёнка, а не полагаться на то, что диапазоны не пересекаются.

// Пересечение [1,5] и [3,8]: без клиппинга страницы 3-5 попали бы в обе
// главы. С клиппингом каждая страница ровно в одной.
func TestBuildSectionClipsOverlappingSiblings(t *testing.T) {
	root := Node{
		Title: "Том", Start: 1, End: 10,
		Children: []Node{
			{Title: "Первая", Start: 1, End: 5},
			{Title: "Вторая", Start: 3, End: 8},
		},
	}

	got := collectInternal(BuildSection(root, pagesFor(1, 10, 0)))
	want := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("покрытие = %v, хотел %v", got, want)
	}
}

// Реальная форма 120 из 124 пересечений: соседние главы делят ровно одну
// страницу (a.end_page = b.start_page). Общая страница должна достаться
// первой по порядку чтения главе — иначе либо дублируется, либо теряется.
func TestBuildSectionSharedBoundaryGoesToFirstSibling(t *testing.T) {
	root := Node{
		Title: "Том", Start: 1, End: 80,
		Children: []Node{
			{Title: "Первая", Start: 1, End: 50},
			{Title: "Вторая", Start: 50, End: 80},
		},
	}

	sec := BuildSection(root, pagesFor(1, 80, 0))

	got := collectInternal(sec)
	want := make([]int, 0, 80)
	for i := 1; i <= 80; i++ {
		want = append(want, i)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("покрытие = %v, хотел %v", got, want)
	}

	first := sec.Blocks[0].Child
	second := sec.Blocks[1].Child
	if first == nil || second == nil {
		t.Fatalf("ожидал две вложенные секции")
	}
	if got := collectInternal(*first); !reflect.DeepEqual(got, appendRange(1, 50)) {
		t.Errorf("первая глава = %v, хотел страницы 1..50 включая границу", got)
	}
	if got := collectInternal(*second); !reflect.DeepEqual(got, appendRange(51, 80)) {
		t.Errorf("вторая глава = %v, хотел страницы 51..80, без границы", got)
	}
}

// appendRange — вспомогательная последовательность from..to включительно.
func appendRange(from, to int) []int {
	out := make([]int, 0, to-from+1)
	for i := from; i <= to; i++ {
		out = append(out, i)
	}
	return out
}

// 21 пара в корпусе делит не одну страницу, а полный старт — три и более
// соседей могут начинаться на одной странице (шмуцтитул с несколькими
// подряд идущими заголовками). Клиппинг обязан развести их по порядку.
func TestBuildSectionThreeSiblingsShareStart(t *testing.T) {
	root := Node{
		Title: "Том", Start: 1, End: 20,
		Children: []Node{
			{Title: "Первая", Start: 5, End: 10},
			{Title: "Вторая", Start: 5, End: 15},
			{Title: "Третья", Start: 5, End: 20},
		},
	}

	got := collectInternal(BuildSection(root, pagesFor(1, 20, 0)))
	want := appendRange(1, 20)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("покрытие = %v, хотел %v", got, want)
	}
}

// Грандребёнок, чей диапазон выходит за границы собственного родителя
// (Часть кончается на 10-й, Глава просит до 15-й), обязан быть отсечён по
// границе именно своего родителя, а не корня — иначе он отъест страницы у
// следующей секции корневого уровня (СледующаяЧасть), которой они на самом
// деле принадлежат.
func TestBuildSectionClipsChildToItsOwnParent(t *testing.T) {
	root := Node{
		Title: "Том", Start: 1, End: 20,
		Children: []Node{
			{
				Title: "Часть", Start: 1, End: 10,
				Children: []Node{{Title: "Глава", Start: 5, End: 15}},
			},
			{Title: "СледующаяЧасть", Start: 11, End: 20},
		},
	}

	sec := BuildSection(root, pagesFor(1, 20, 0))

	got := collectInternal(sec)
	want := appendRange(1, 20)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("покрытие = %v, хотел %v", got, want)
	}

	part := sec.Blocks[0].Child
	if part == nil {
		t.Fatalf("часть не стала вложенной секцией")
	}
	if got := collectInternal(*part); !reflect.DeepEqual(got, appendRange(1, 10)) {
		t.Errorf("часть = %v, хотел страницы 1..10 (глава отсечена по 10-й)", got)
	}

	next := sec.Blocks[1].Child
	if next == nil {
		t.Fatalf("следующая часть не стала вложенной секцией")
	}
	if got := collectInternal(*next); !reflect.DeepEqual(got, appendRange(11, 20)) {
		t.Errorf("следующая часть = %v, хотел страницы 11..20 целиком, не отъеденные главой", got)
	}
}

// Перевёрнутый диапазон (Start > End) в корпусе сегодня не встречается, но
// клиппинг обязан пережить его между двумя нормальными соседями, не потеряв
// и не задвоив ни одной страницы.
func TestBuildSectionToleratesInvertedRangeBetweenSiblings(t *testing.T) {
	root := Node{
		Title: "Том", Start: 1, End: 10,
		Children: []Node{
			{Title: "Первая", Start: 1, End: 3},
			{Title: "Перевёрнутая", Start: 5, End: -100},
			{Title: "Третья", Start: 6, End: 10},
		},
	}

	got := collectInternal(BuildSection(root, pagesFor(1, 10, 0)))
	want := appendRange(1, 10)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("покрытие = %v, хотел %v", got, want)
	}
}

// Ребёнок, чей диапазон целиком поглощён предыдущим соседом (после
// клиппинга start > end), не создаёт блок вовсе — ни с страницами, ни
// пустой вложенной секцией.
func TestBuildSectionChildFullyConsumedIsSkipped(t *testing.T) {
	root := Node{
		Title: "Том", Start: 1, End: 10,
		Children: []Node{
			{Title: "Первая", Start: 1, End: 10},
			{Title: "Поглощённая", Start: 3, End: 7},
		},
	}

	sec := BuildSection(root, pagesFor(1, 10, 0))

	if len(sec.Blocks) != 1 {
		t.Fatalf("блоков = %d, хотел 1 (поглощённая глава не создаёт блок)", len(sec.Blocks))
	}
	got := collectInternal(sec)
	want := appendRange(1, 10)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("покрытие = %v, хотел %v", got, want)
	}
}

// assertBlocksExclusive проверяет главное свойство Block: ровно одно из
// Pages/Child непусто, никогда оба и никогда ни одного. Пустой блок с
// собственными страницами уже отсеивается в BuildSection (len(own) > 0), а
// вложенная секция всегда получает ненулевой указатель — это фиксирует
// гарантию там, где она на самом деле важна: у писателей форматов (Task 3+),
// которым придётся switch'ить по этому полю.
func assertBlocksExclusive(t *testing.T, s Section) {
	t.Helper()
	for _, b := range s.Blocks {
		hasPages := len(b.Pages) > 0
		hasChild := b.Child != nil
		if hasPages == hasChild {
			t.Errorf("блок %+v: должно быть ровно одно из Pages/Child, а не %v/%v", b, hasPages, hasChild)
		}
		if hasChild {
			assertBlocksExclusive(t, *b.Child)
		}
	}
}

func TestBuildSectionBlocksHaveExactlyOnePayload(t *testing.T) {
	root := Node{
		Title: "Том", Start: 1, End: 20,
		Children: []Node{{
			Title: "Часть", Start: 4, End: 20,
			Children: []Node{
				{Title: "Глава 1", Start: 6, End: 10},
				{Title: "Глава 2", Start: 14, End: 18},
			},
		}},
	}

	sec := BuildSection(root, pagesFor(1, 20, 0))
	assertBlocksExclusive(t, sec)
}

// Дети, начинающиеся после конца диапазона, не дают ни шага, ни пробела:
// пробел за пределами [start, end] — это страницы, которых не просили.
func TestClipChildrenStopsAtRangeEnd(t *testing.T) {
	steps := ClipChildren([]Node{
		{Title: "a", Start: 5, End: 639},
		{Title: "b", Start: 337, End: 600},
		{Title: "c", Start: 640, End: 700},
	}, 5, 5)
	if len(steps) != 1 || steps[0].Gap || steps[0].Node.Title != "a" || steps[0].Node.Start != 5 || steps[0].Node.End != 5 {
		t.Fatalf("шаги %+v, ожидался один «a» 5—5", steps)
	}
	// Пробел перед ребёнком, начавшимся за концом диапазона, обрезается концом.
	steps = ClipChildren([]Node{{Title: "x", Start: 2, End: 3}, {Title: "y", Start: 9, End: 12}}, 2, 6)
	if len(steps) != 2 || steps[0].Node.Title != "x" || !steps[1].Gap || steps[1].Start != 4 || steps[1].End != 6 {
		t.Errorf("шаги %+v, ожидались «x» 2—3 и пробел 4—6", steps)
	}
}
