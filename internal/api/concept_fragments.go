package api

import (
	"sort"
	"strings"

	"proofreader/internal/models"
)

// entrySlot is one entry of a concept's stream: a single index address,
// resolved to the pages of a loaded volume. Bodies are not fetched yet.
//
// Единица потока — адрес, а не страница: подрубрика («определение», «как
// субстанция стоимости») — ось чтения, и слияние двух адресов, попавших на
// одни страницы, потеряло бы, какое место страницы к какой подрубрике
// относится. Цена принята сознательно: страница может встретиться в потоке
// дважды.
type entrySlot struct {
	Reference    *models.IndexReference
	WorkID       int64
	PageNumbers  []int
	PrintedStart int
	PrintedEnd   int
}

// fragmentFilter narrows the reference stream. An empty filter accepts all references.
type fragmentFilter struct {
	Rubric string
	// RubricPath сужает поток по ПУТИ подрубрики, сравнением по префиксу:
	// путь длины 1 накрывает весь раздел (881 адрес у «II съезда РСДРП»),
	// длины 2 — один аспект внутри него. Плоское поле Rubric остаётся рядом
	// и не трогается: по нему приходят чужие закладки со старым ?rubric=,
	// и сравнение по листу для них обязано остаться тем же, каким было.
	RubricPath    []string
	HasRubricPath bool
	Volume        int
	HasVolume     bool
	VolumePart    string
	// ReferenceID narrows to one address (task 13a) — a stream of at most one
	// entry, used to re-fetch a single record after its cuts were saved.
	//
	// Здесь же, а не отдельной проверкой в обработчике: total считается по
	// тому же fragmentFilter до пагинации (см. Fragments), и отдельная
	// проверка снаружи разошлась бы с ним при сочетании с прочими фильтрами.
	// Адрес чужого понятия просто не встретится среди c.References — пустой
	// поток без специального кода для этого случая.
	ReferenceID    int64
	HasReferenceID bool
}

// matches reports whether a reference passes the filter.
func (f fragmentFilter) matches(ref *models.IndexReference) bool {
	if f.HasReferenceID && ref.ID != f.ReferenceID {
		return false
	}
	// Путь победил плоское имя — это правило, а не порядок проверок, поэтому
	// стоит else if, а не два независимых условия.
	if f.HasRubricPath {
		if !hasPathPrefix(rubricPathOf(ref), f.RubricPath) {
			return false
		}
	} else if f.Rubric != "" && ref.Rubric != f.Rubric {
		return false
	}
	if !f.HasVolume {
		return true
	}
	if ref.VolumeNumber != f.Volume {
		return false
	}
	part := ""
	if ref.VolumePart != nil {
		part = *ref.VolumePart
	}
	return part == f.VolumePart
}

// rubricPathOf — путь подрубрик адреса от корня к листу, тем же правилом,
// каким его строит pathOf на фронте (frontend/src/utils/conceptReferences.ts).
//
// Откат на плоский Rubric нужен не «старым ответам сервера» — здесь сервер и
// есть источник, — а тому, чтобы правило совпадало с фронтовым звено в
// звено: порядок групп в потоке сверяется с раскладкой статьи, и разойтись
// этим двум функциям нельзя.
func rubricPathOf(ref *models.IndexReference) []string {
	if len(ref.RubricPath) > 0 {
		return ref.RubricPath
	}
	if ref.Rubric != "" {
		return []string{ref.Rubric}
	}
	return nil
}

// hasPathPrefix — накрывает ли prefix путь path. Пустой prefix накрывает всё;
// вызывающий обязан не ставить HasRubricPath при пустом пути (см.
// parseRubricPath), иначе фильтр молча перестанет сужать.
func hasPathPrefix(path, prefix []string) bool {
	if len(prefix) > len(path) {
		return false
	}
	for i := range prefix {
		if path[i] != prefix[i] {
			return false
		}
	}
	return true
}

// referencePages converts a reference's printed page range into work page numbers.
//
// Указатель адресует ПЕЧАТНЫЕ колонцифры, страницы работы нумерованы по скану,
// разница — page_offset тома: page_number = printed - offset. Страницы вне границ
// тома (page < 1 или page > MaxPage) отсеиваются; диапазон берётся для печатной нумерации.
func referencePages(ref *models.IndexReference, loc models.VolumeLocation) []int {
	if ref.PageStart < 1 {
		return nil
	}
	// Перевёрнутый диапазон бывает от битого разбора указателя; берём начало,
	// а не молчаливо пустой результат — адрес существует.
	end := ref.PageEnd
	if end < ref.PageStart {
		end = ref.PageStart
	}

	var pages []int
	for printed := ref.PageStart; printed <= end; printed++ {
		page := printed - loc.PageOffset
		if page < 1 || page > loc.MaxPage {
			continue
		}
		pages = append(pages, page)
	}
	return pages
}

// entryOrder is the reading order of the stream.
//
// Нулевое значение — порядок по подрубрикам: это и дефолт ручки, и то, чем
// статья указателя устроена на бумаге. Порядок по томам оставлен режимом
// сквозного чтения.
type entryOrder int

const (
	orderByRubric entryOrder = iota
	orderByPage
)

// pathRankKey — ключ пути в карте рангов. Разделитель NUL: Postgres не
// принимает NUL в text вовсе, поэтому в заголовке подрубрики его быть не
// может и склейка двух разных путей в один ключ невозможна. Тот же приём,
// что у rubricPathKey в internal/repository/index_repository.go.
func pathRankKey(path []string) string {
	return strings.Join(path, "\x00")
}

// rubricRanking — ранги групп подрубрик, по которым выстраивается порядок
// чтения. Ранг группы — наименьший order_number среди попавших в неё
// адресов, то есть место её ПЕРВОГО ПОЯВЛЕНИЯ в печатной статье: так же
// ставит группы buildLevel на фронте, и разойтись этим двум раскладкам
// нельзя.
//
// Ключ — префикс пути. Пустой ключ ранжируют ТОЛЬКО адреса без подрубрики
// вовсе (их 44 244 в издании 4): их группа не «раньше всех» и не «позже
// всех», она стоит в общем ряду верхнего уровня по первому появлению,
// наравне с именованными.
type rubricRanking struct {
	ranks map[string]int
}

// rubricRanks ranks each subrubric branch by its first appearance in the
// printed article — the smallest order_number among the addresses carrying it.
//
// Считается по полному списку адресов понятия, до фильтров и до отсева
// незагруженных томов: иначе порядок групп ездил бы от фильтра по тому и от
// загрузки очередного тома, то есть от вещей, к устройству статьи отношения
// не имеющих.
//
// Свойство «порядок групп потока = раскладка статьи» заявлено для потока БЕЗ
// фильтра тома; под `?volume=` ранги остаются посчитанными по полному
// набору (см. выше), а панель статьи группирует уже отфильтрованный —
// расхождение порядка в этом случае законно, не регрессия.
func rubricRanks(refs []*models.IndexReference) rubricRanking {
	r := rubricRanking{ranks: make(map[string]int, len(refs))}
	for _, ref := range refs {
		path := rubricPathOf(ref)
		for i := 0; i <= len(path); i++ {
			// Пустой ключ ранжируют только безрубричные адреса; у пути со
			// звеньями нулевой префикс пропускается.
			if i == 0 && len(path) > 0 {
				continue
			}
			key := pathRankKey(path[:i])
			if rank, seen := r.ranks[key]; !seen || ref.OrderNumber < rank {
				r.ranks[key] = ref.OrderNumber
			}
		}
	}
	return r
}

// groupRank — ранг группы, в которую путь попадает на уровне depth.
func (r rubricRanking) groupRank(path []string, depth int) int {
	if depth < len(path) {
		return r.ranks[pathRankKey(path[:depth+1])]
	}
	return r.ranks[pathRankKey(path)]
}

// compare сравнивает два пути поуровнево. Возвращает <0, 0 или >0.
//
// На каждом уровне группы идут по первому появлению — по рангу
// (groupRank). Это единственное явное правило; порядок «свои адреса узла
// раньше вложенных подрубрик» (тот, что buildLevel на фронте держит
// отдельной веткой — refs рисуются раньше children безусловно) здесь
// получается САМ, без отдельной ветки — см. комментарий у тай-брейка ниже.
//
// Звено пути пустым не бывает (title подрубрики в базе NOT NULL и непуст, а
// rubricPathOf отдаёт nil при пустом Rubric), поэтому пустая строка на
// уровне означает ровно одно: путь на этом уровне кончился.
func (r rubricRanking) compare(a, b []string) int {
	for depth := 0; ; depth++ {
		ta, tb := titleAt(a, depth), titleAt(b, depth)
		if ta == tb {
			if ta == "" {
				return 0
			}
			continue
		}
		ra, rb := r.groupRank(a, depth), r.groupRank(b, depth)
		if ra != rb {
			if ra < rb {
				return -1
			}
			return 1
		}
		// Равные ранги бывают в двух разных случаях, и сравнение названий
		// закрывает оба.
		//
		// Первый — битый разбор указателя, два соседних раздела с одним
		// order_number: без сравнения названий группы разъехались бы по
		// страницам, а поток ради этого и переупорядочивается.
		//
		// Второй — СОБСТВЕННЫЕ адреса узла против его вложенных подрубрик.
		// buildLevel на фронте кладёт первые в refs, вторые в children, и
		// рисуются refs всегда раньше children, независимо от order_number;
		// здесь тот же порядок получается сам, без отдельной ветки, и держат
		// его два свойства разом. Первое: ранг префикса не может быть больше
		// ранга его продолжения — он считается по надмножеству адресов,
		// поэтому либо строго меньше (и свой адрес впереди по рангу), либо
		// ничья. Второе: при ничьей путь, кончившийся на этом уровне, несёт
		// здесь пустое название, а пустая строка меньше любого непустого
		// заголовка. Оба свойства НЕСУЩИЕ: правка, которая перевернёт это
		// сравнение или начнёт считать ранги по неполному набору адресов,
		// молча поставит свои адреса раздела после его аспектов. Сторожит
		// это TestCollectEntriesOwnRefsPrecedeNestedEvenWhenPrintedLater.
		if ta < tb {
			return -1
		}
		return 1
	}
}

func titleAt(path []string, depth int) string {
	if depth < len(path) {
		return path[depth]
	}
	return ""
}

// collectEntries turns a concept's addresses into stream entries, sorted in
// the requested reading order.
//
// locs — местоположение каждого адреса по ID ссылки (models.IndexReference.ID),
// уже разрешённое ВЫЗЫВАЮЩИМ строго против издания собственной статьи адреса
// (см. conceptAddresses). Здесь это не пересчитывается и не резолвится по
// номеру тома: общий словарь по номеру тома подменил бы том — собрания
// нумеруют тома независимо, и том 23 есть и у Маркса, и у Ленина. Адрес,
// которого нет в locs, пропускается — это тот же исход, что и «незагруженный
// том» (см. TestCollectEntriesSkipsUnloadedVolumes).
func collectEntries(refs []*models.IndexReference, locs map[int64]models.VolumeLocation, f fragmentFilter, order entryOrder) []entrySlot {
	entries := make([]entrySlot, 0, len(refs))
	for _, ref := range refs {
		if !f.matches(ref) {
			continue
		}
		loc, ok := locs[ref.ID]
		if !ok {
			continue
		}
		if slot, ok := slotFor(ref, loc); ok {
			entries = append(entries, slot)
		}
	}
	sortEntries(entries, rubricRanks(refs), order)
	return entries
}

// slotFor — адрес, разрешённый в полосы своего тома. false — ни одной полосы
// адреса в томе нет (за концом тома, битый номер).
func slotFor(ref *models.IndexReference, loc models.VolumeLocation) (entrySlot, bool) {
	pages := referencePages(ref, loc)
	if len(pages) == 0 {
		return entrySlot{}, false
	}
	return entrySlot{
		Reference:   ref,
		WorkID:      loc.WorkID,
		PageNumbers: pages,
		// Печатные границы — по фактически показанным страницам, а не по
		// тому, что стоит в указателе: у тома может не хватать хвоста.
		PrintedStart: pages[0] + loc.PageOffset,
		PrintedEnd:   pages[len(pages)-1] + loc.PageOffset,
	}, true
}

// sortEntries выстраивает записи в порядке чтения. ranking считается по
// ПОЛНОМУ списку адресов понятия (см. rubricRanks), а не по entries.
func sortEntries(entries []entrySlot, ranking rubricRanking, order entryOrder) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i].Reference, entries[j].Reference
		if order == orderByRubric {
			if cmp := ranking.compare(rubricPathOf(a), rubricPathOf(b)); cmp != 0 {
				return cmp < 0
			}
		}
		if a.VolumeNumber != b.VolumeNumber {
			return a.VolumeNumber < b.VolumeNumber
		}
		if pa, pb := partOf(a.VolumePart), partOf(b.VolumePart); pa != pb {
			return pa < pb
		}
		if entries[i].PrintedStart != entries[j].PrintedStart {
			return entries[i].PrintedStart < entries[j].PrintedStart
		}
		// Два адреса на одну страницу: порядок задаёт указатель, иначе поток
		// тасовался бы от запроса к запросу и пагинация двоила бы записи.
		return a.OrderNumber < b.OrderNumber
	})
}

// partOf normalizes a volume part for comparison.
// A volume without a part (nil) sorts before any part (empty string < non-empty).
func partOf(part *string) string {
	if part == nil {
		return ""
	}
	return *part
}

// deepestChapterTitle returns the title of the deepest chapter whose page range covers the page.
//
// Принадлежность главе — производная от start_page/end_page, а не хранимая
// связь: pages.chapter_id в проекте ничем не заполняется. Спускаемся всегда,
// даже если родитель не совпал: при битой разметке потомок иначе исчез бы
// молча (близнец chapterLevelsForPage из frontend/src/hooks/useChaptersForPage.ts).
func deepestChapterTitle(tree []*models.Chapter, pageNumber int) string {
	if ch := deepestChapterOf(tree, pageNumber); ch != nil {
		return ch.Title
	}
	return ""
}

// deepestChapterOf — сама глава, а не только её название: шапке записи нужны
// id и слаг, чтобы название стало ссылкой. nil — страница вне всех глав.
func deepestChapterOf(tree []*models.Chapter, pageNumber int) *models.Chapter {
	ch, _ := deepestChapter(tree, pageNumber, 0)
	return ch
}

func deepestChapter(nodes []*models.Chapter, pageNumber, depth int) (*models.Chapter, int) {
	var best *models.Chapter
	bestDepth := -1
	for _, ch := range nodes {
		if ch.StartPage <= pageNumber && pageNumber <= ch.EndPage && depth > bestDepth {
			best, bestDepth = ch, depth
		}
		if found, d := deepestChapter(ch.Children, pageNumber, depth+1); d > bestDepth {
			best, bestDepth = found, d
		}
	}
	return best, bestDepth
}
