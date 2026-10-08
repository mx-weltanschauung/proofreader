package api

import (
	"fmt"
	"strings"
	"testing"

	"proofreader/internal/models"
)

func vol(number int, part *string, workID int64, offset, maxPage int) models.VolumeLocation {
	return models.VolumeLocation{
		VolumeNumber: number, VolumePart: part,
		WorkID: workID, PageOffset: offset, MaxPage: maxPage,
	}
}

func iref(id int64, volume, start, end int, rubric string, order int) *models.IndexReference {
	return &models.IndexReference{
		ID: id, VolumeNumber: volume, PageStart: start, PageEnd: end,
		Rubric: rubric, OrderNumber: order,
	}
}

// locsFor resolves each reference against a single volume map by volume
// number/part, and re-keys the result by reference ID — the shape
// collectEntries now takes. Production code (conceptAddresses) never builds
// this by flattening several editions' maps into one; it resolves each
// reference against its OWN article's edition map before collectEntries ever
// sees it (see TestFragmentsCoverAllArticlesOfConcept for that part — the
// collision test in concept_stream_test.go). This helper is a single-edition
// test convenience only — every
// fixture in this file uses one volume map, so there is nothing to collide.
func locsFor(refs []*models.IndexReference, vols map[volumeKey]models.VolumeLocation) map[int64]models.VolumeLocation {
	locs := make(map[int64]models.VolumeLocation, len(refs))
	for _, ref := range refs {
		if loc, ok := vols[keyOf(ref.VolumeNumber, ref.VolumePart)]; ok {
			locs[ref.ID] = loc
		}
	}
	return locs
}

func TestReferencePagesExpandsRange(t *testing.T) {
	got := referencePages(iref(1, 12, 730, 731, "определение", 1), vol(12, nil, 14, 0, 800))
	if len(got) != 2 || got[0] != 730 || got[1] != 731 {
		t.Fatalf("страницы %v, ожидались [730 731]", got)
	}
}

func TestReferencePagesAppliesOffsetAndClamp(t *testing.T) {
	// Печатная = page_number + 4, и всего в работе две страницы: печатные
	// 5 и 6 дают 1 и 2, печатные 7 и 8 уходят за MaxPage.
	got := referencePages(iref(1, 12, 5, 8, "определение", 1), vol(12, nil, 40, 4, 2))
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("страницы %v, ожидались [1 2]", got)
	}
}

func TestReferencePagesSkipsUnloadedVolumeAndJunk(t *testing.T) {
	if got := referencePages(iref(1, 12, 0, 0, "определение", 1), vol(12, nil, 14, 0, 800)); got != nil {
		t.Fatalf("нулевой печатный номер дал %v", got)
	}
	// page_end меньше page_start — битый разбор; берём только начало.
	got := referencePages(iref(1, 12, 730, 700, "определение", 1), vol(12, nil, 14, 0, 800))
	if len(got) != 1 || got[0] != 730 {
		t.Fatalf("перевёрнутый диапазон дал %v, ожидалось [730]", got)
	}
}

func TestCollectEntriesKeepsRubricsApart(t *testing.T) {
	vols := buildVolumeMap([]models.VolumeLocation{vol(12, nil, 14, 0, 800)})
	// Живой случай «Абстрактного труда»: одна пара страниц в двух
	// подрубриках. Это разные места страницы, и слить их значит потерять,
	// какое место к какой подрубрике относится.
	refs := []*models.IndexReference{
		iref(1, 12, 730, 731, "определение", 1),
		iref(2, 12, 730, 731, "и конкретный (полезный) труд", 69),
	}

	entries := collectEntries(refs, locsFor(refs, vols), fragmentFilter{}, orderByPage)

	if len(entries) != 2 {
		t.Fatalf("записей %d, ожидалось 2", len(entries))
	}
	for i, want := range []string{"определение", "и конкретный (полезный) труд"} {
		if entries[i].Reference.Rubric != want {
			t.Fatalf("запись %d: подрубрика %q, ожидалась %q", i, entries[i].Reference.Rubric, want)
		}
		if len(entries[i].PageNumbers) != 2 || entries[i].PageNumbers[0] != 730 {
			t.Fatalf("запись %d: страницы %v", i, entries[i].PageNumbers)
		}
		if entries[i].PrintedStart != 730 || entries[i].PrintedEnd != 731 {
			t.Fatalf("запись %d: печатные %d—%d", i, entries[i].PrintedStart, entries[i].PrintedEnd)
		}
	}
}

func TestCollectEntriesPrintedRangeFollowsClamping(t *testing.T) {
	// В работе всего две страницы: печатные 5 и 6 попадают, 7 и 8 — нет.
	// Шапка обязана назвать то, что показано, а не то, что стоит в указателе.
	vols := buildVolumeMap([]models.VolumeLocation{vol(12, nil, 40, 4, 2)})
	refs := []*models.IndexReference{iref(1, 12, 5, 8, "определение", 1)}
	entries := collectEntries(refs, locsFor(refs, vols), fragmentFilter{}, orderByRubric)

	if len(entries) != 1 {
		t.Fatalf("записей %d", len(entries))
	}
	if entries[0].PrintedStart != 5 || entries[0].PrintedEnd != 6 {
		t.Fatalf("печатные %d—%d, ожидались 5—6", entries[0].PrintedStart, entries[0].PrintedEnd)
	}
}

func TestCollectEntriesOrdersByVolumeThenPage(t *testing.T) {
	partII := "II"
	vols := buildVolumeMap([]models.VolumeLocation{
		vol(12, nil, 14, 0, 800),
		vol(25, &partII, 31, 0, 500),
		vol(23, nil, 20, 0, 900),
	})
	inPartII := iref(1, 25, 404, 404, "определение", 1)
	inPartII.VolumePart = &partII
	refs := []*models.IndexReference{
		inPartII,
		iref(2, 23, 52, 52, "определение", 2),
		iref(3, 12, 730, 730, "определение", 3),
		iref(4, 23, 46, 46, "определение", 4),
	}

	entries := collectEntries(refs, locsFor(refs, vols), fragmentFilter{}, orderByPage)

	got := make([][2]int, 0, len(entries))
	for _, e := range entries {
		got = append(got, [2]int{e.Reference.VolumeNumber, e.PrintedStart})
	}
	want := [][2]int{{12, 730}, {23, 46}, {23, 52}, {25, 404}}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("порядок %v, ожидался %v", got, want)
		}
	}
}

func TestCollectEntriesTieBreaksByReferenceOrder(t *testing.T) {
	vols := buildVolumeMap([]models.VolumeLocation{vol(12, nil, 14, 0, 800)})
	// Два адреса на одну страницу: порядок между ними задаёт указатель,
	// иначе поток тасовался бы от запроса к запросу и пагинация двоила бы
	// записи.
	refs := []*models.IndexReference{
		iref(1, 12, 730, 730, "и конкретный (полезный) труд", 69),
		iref(2, 12, 730, 730, "определение", 1),
	}

	entries := collectEntries(refs, locsFor(refs, vols), fragmentFilter{}, orderByPage)

	if len(entries) != 2 || entries[0].Reference.OrderNumber != 1 {
		t.Fatalf("порядок записей: %d, %d", entries[0].Reference.OrderNumber, entries[1].Reference.OrderNumber)
	}
}

func TestCollectEntriesFilters(t *testing.T) {
	partII := "II"
	vols := buildVolumeMap([]models.VolumeLocation{
		vol(12, nil, 14, 0, 800),
		vol(25, &partII, 31, 0, 500),
	})
	inPartII := iref(3, 25, 404, 404, "определение", 3)
	inPartII.VolumePart = &partII
	refs := []*models.IndexReference{
		iref(1, 12, 730, 730, "определение", 1),
		iref(2, 12, 745, 745, "его мера", 2),
		inPartII,
	}

	byRubric := collectEntries(refs, locsFor(refs, vols), fragmentFilter{Rubric: "его мера"}, orderByRubric)
	if len(byRubric) != 1 || byRubric[0].PrintedStart != 745 {
		t.Fatalf("фильтр по подрубрике дал %+v", byRubric)
	}

	byVolume := collectEntries(refs, locsFor(refs, vols), fragmentFilter{Volume: 12, HasVolume: true}, orderByRubric)
	if len(byVolume) != 2 {
		t.Fatalf("фильтр по тому дал %d записей, ожидалось 2", len(byVolume))
	}

	byPart := collectEntries(refs, locsFor(refs, vols), fragmentFilter{Volume: 25, HasVolume: true, VolumePart: "II"}, orderByRubric)
	if len(byPart) != 1 || byPart[0].Reference.VolumeNumber != 25 {
		t.Fatalf("фильтр по части тома дал %+v", byPart)
	}

	none := collectEntries(refs, locsFor(refs, vols), fragmentFilter{Volume: 12, HasVolume: true, VolumePart: "II"}, orderByRubric)
	if len(none) != 0 {
		t.Fatalf("несуществующая часть дала %d записей", len(none))
	}
}

// TestCollectEntriesFiltersByReferenceID: узкий фильтр задачи 13a — сужение
// до одного адреса тем же полем fragmentFilter, что и подрубрика с томом,
// поэтому сочетается с ними теми же правилами (все условия обязаны совпасть).
func TestCollectEntriesFiltersByReferenceID(t *testing.T) {
	vols := buildVolumeMap([]models.VolumeLocation{vol(12, nil, 14, 0, 800)})
	refs := []*models.IndexReference{
		iref(1, 12, 730, 730, "определение", 1),
		iref(2, 12, 745, 745, "его мера", 2),
	}

	byID := collectEntries(refs, locsFor(refs, vols), fragmentFilter{ReferenceID: 2, HasReferenceID: true}, orderByRubric)
	if len(byID) != 1 || byID[0].Reference.ID != 2 {
		t.Fatalf("фильтр по адресу дал %+v", byID)
	}

	// Чужой (не встречающийся в refs) id — пустой список, а не ошибка: адрес
	// и понятие проверяются раздельно, «нет такой записи в этом потоке» —
	// законный ответ на сужение.
	foreign := collectEntries(refs, locsFor(refs, vols), fragmentFilter{ReferenceID: 999, HasReferenceID: true}, orderByRubric)
	if len(foreign) != 0 {
		t.Fatalf("чужой id дал %d записей", len(foreign))
	}

	// Сочетание с подрубрикой: тот же адрес, но не та подрубрика — пусто, обе
	// проверки обязаны совпасть одновременно.
	mismatch := collectEntries(refs, locsFor(refs, vols), fragmentFilter{
		ReferenceID: 2, HasReferenceID: true, Rubric: "определение",
	}, orderByRubric)
	if len(mismatch) != 0 {
		t.Fatalf("адрес не той подрубрики дал %d записей", len(mismatch))
	}
}

func TestCollectEntriesSkipsUnloadedVolumes(t *testing.T) {
	refs := []*models.IndexReference{iref(1, 12, 730, 731, "определение", 1)}
	if entries := collectEntries(refs, map[int64]models.VolumeLocation{}, fragmentFilter{}, orderByRubric); len(entries) != 0 {
		t.Fatalf("незагруженные тома дали %d записей", len(entries))
	}
}

func TestDeepestChapterTitle(t *testing.T) {
	tree := []*models.Chapter{
		{
			ID: 1, Title: "Экономические рукописи", StartPage: 700, EndPage: 800, OrderNumber: 1,
			Children: []*models.Chapter{
				{ID: 2, Title: "Введение", StartPage: 725, EndPage: 740, OrderNumber: 1,
					Children: []*models.Chapter{
						{ID: 3, Title: "Метод политической экономии", StartPage: 730, EndPage: 735, OrderNumber: 1},
					},
				},
				{ID: 4, Title: "Глава о деньгах", StartPage: 741, EndPage: 800, OrderNumber: 2},
			},
		},
	}

	if got := deepestChapterTitle(tree, 730); got != "Метод политической экономии" {
		t.Fatalf("стр. 730 дала %q", got)
	}
	if got := deepestChapterTitle(tree, 727); got != "Введение" {
		t.Fatalf("стр. 727 дала %q", got)
	}
	if got := deepestChapterTitle(tree, 750); got != "Глава о деньгах" {
		t.Fatalf("стр. 750 дала %q", got)
	}
	if got := deepestChapterTitle(tree, 100); got != "" {
		t.Fatalf("страница вне глав дала %q, ожидалась пустая строка", got)
	}
	if got := deepestChapterTitle(nil, 730); got != "" {
		t.Fatalf("работа без глав дала %q", got)
	}
}

func TestDeepestChapterTitleIgnoresBrokenNesting(t *testing.T) {
	// Диапазон потомка вне родительского — битая разметка. Спускаться надо
	// всегда, иначе глава исчезнет молча (та же причина, что в
	// chapterLevelsForPage на фронте).
	tree := []*models.Chapter{
		{ID: 1, Title: "Родитель", StartPage: 10, EndPage: 20,
			Children: []*models.Chapter{{ID: 2, Title: "Потомок", StartPage: 100, EndPage: 110}},
		},
	}
	if got := deepestChapterTitle(tree, 105); got != "Потомок" {
		t.Fatalf("страница вложенной главы дала %q", got)
	}
}

func TestCollectEntriesGroupsByRubricInPrintOrder(t *testing.T) {
	vols := buildVolumeMap([]models.VolumeLocation{
		vol(12, nil, 14, 0, 800),
		vol(23, nil, 20, 0, 900),
	})
	// Живой случай «Абстрактного труда»: «определение» открывает статью, но
	// его адреса разбросаны по томам, и по страницам между ними попадают
	// адреса других подрубрик.
	refs := []*models.IndexReference{
		iref(1, 12, 730, 730, "определение", 1),
		iref(2, 23, 52, 52, "определение", 2),
		iref(3, 12, 700, 700, "как субстанция стоимости", 40),
		iref(4, 23, 46, 46, "как субстанция стоимости", 41),
		iref(5, 12, 745, 745, "и конкретный (полезный) труд", 69),
	}

	entries := collectEntries(refs, locsFor(refs, vols), fragmentFilter{}, orderByRubric)

	got := make([]string, 0, len(entries))
	for _, e := range entries {
		got = append(got, fmt.Sprintf("%s/%d/%d", e.Reference.Rubric, e.Reference.VolumeNumber, e.PrintedStart))
	}
	want := []string{
		"определение/12/730",
		"определение/23/52",
		"как субстанция стоимости/12/700",
		"как субстанция стоимости/23/46",
		"и конкретный (полезный) труд/12/745",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("порядок %v, ожидался %v", got, want)
		}
	}
}

func TestCollectEntriesRubricRankIgnoresFilterAndUnloadedVolumes(t *testing.T) {
	// Том 12 не загружен, и адрес, открывающий подрубрику «определение»,
	// в поток не попадает. Ранг подрубрики всё равно считается по нему:
	// посчитай ранг по уцелевшим адресам — «определение» получило бы ранг 50
	// и уехало бы за «как субстанция стоимости» (40), то есть порядок групп
	// зависел бы от того, какие тома успели загрузить.
	vols := buildVolumeMap([]models.VolumeLocation{vol(23, nil, 20, 0, 900)})
	refs := []*models.IndexReference{
		iref(1, 12, 730, 730, "определение", 1),
		iref(2, 23, 89, 89, "определение", 50),
		iref(3, 23, 46, 46, "как субстанция стоимости", 40),
	}

	entries := collectEntries(refs, locsFor(refs, vols), fragmentFilter{}, orderByRubric)

	if len(entries) != 2 {
		t.Fatalf("записей %d, ожидалось 2", len(entries))
	}
	// По страницам первой шла бы «как субстанция стоимости» (46), по рангу
	// уцелевших адресов — тоже она; правильный порядок даёт только ранг,
	// посчитанный по полному списку.
	if entries[0].Reference.Rubric != "определение" || entries[1].Reference.Rubric != "как субстанция стоимости" {
		t.Fatalf("порядок групп: %q, %q", entries[0].Reference.Rubric, entries[1].Reference.Rubric)
	}
}

func TestCollectEntriesKeepsRubricsContiguousOnEqualRanks(t *testing.T) {
	// Одинаковый order_number у разных подрубрик — битый разбор указателя.
	// Группы обязаны остаться цельными, иначе поток снова перемешается.
	vols := buildVolumeMap([]models.VolumeLocation{vol(12, nil, 14, 0, 800)})
	refs := []*models.IndexReference{
		iref(1, 12, 700, 700, "б", 1),
		iref(2, 12, 710, 710, "а", 1),
		iref(3, 12, 720, 720, "б", 1),
	}

	entries := collectEntries(refs, locsFor(refs, vols), fragmentFilter{}, orderByRubric)

	got := []string{entries[0].Reference.Rubric, entries[1].Reference.Rubric, entries[2].Reference.Rubric}
	want := []string{"а", "б", "б"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("группы %v, ожидались %v", got, want)
		}
	}
}

// irefPath — адрес с явным путём подрубрик. Плоский Rubric заполняется
// листом пути: так его отдаёт articleReferencesQuery (COALESCE(ru.title,''))
// — лист, а не корень, — и тест обязан воспроизводить форму ответа
// репозитория, а не удобную выдумку.
func irefPath(id int64, volume, start, end int, order int, path ...string) *models.IndexReference {
	leaf := ""
	if len(path) > 0 {
		leaf = path[len(path)-1]
	}
	return &models.IndexReference{
		ID: id, VolumeNumber: volume, PageStart: start, PageEnd: end,
		Rubric: leaf, RubricPath: path, OrderNumber: order,
	}
}

func TestCollectEntriesFiltersByRubricPathPrefix(t *testing.T) {
	vols := buildVolumeMap([]models.VolumeLocation{vol(6, nil, 14, 0, 800)})
	// Живой случай статьи 2698: «значение съезда» стоит и под II, и под III
	// съездом, а у самого II съезда есть собственные адреса.
	refs := []*models.IndexReference{
		irefPath(1, 6, 100, 100, 1, "II съезд РСДРП"),
		irefPath(2, 6, 110, 110, 2, "II съезд РСДРП", "значение съезда"),
		irefPath(3, 6, 120, 120, 3, "III съезд РСДРП", "значение съезда"),
	}
	locs := locsFor(refs, vols)

	whole := collectEntries(refs, locs, fragmentFilter{
		RubricPath: []string{"II съезд РСДРП"}, HasRubricPath: true,
	}, orderByRubric)
	if len(whole) != 2 {
		t.Fatalf("весь раздел дал %d записей, ожидалось 2 (свой адрес + аспект)", len(whole))
	}

	leaf := collectEntries(refs, locs, fragmentFilter{
		RubricPath: []string{"II съезд РСДРП", "значение съезда"}, HasRubricPath: true,
	}, orderByRubric)
	if len(leaf) != 1 || leaf[0].Reference.ID != 2 {
		t.Fatalf("аспект дал %+v, ожидался один адрес 2", leaf)
	}
}

// Старый плоский фильтр обязан вести себя ровно как вёл: выбор листа
// «значение съезда» зачерпывает его из ВСЕХ съездов. Это не дефект, а
// сохранённая совместимость — чужие закладки продолжают открываться.
func TestCollectEntriesFlatRubricStillMatchesLeafAcrossParents(t *testing.T) {
	vols := buildVolumeMap([]models.VolumeLocation{vol(6, nil, 14, 0, 800)})
	refs := []*models.IndexReference{
		irefPath(1, 6, 100, 100, 1, "II съезд РСДРП", "значение съезда"),
		irefPath(2, 6, 120, 120, 2, "III съезд РСДРП", "значение съезда"),
		irefPath(3, 6, 130, 130, 3, "III съезд РСДРП", "о Бунде"),
	}

	got := collectEntries(refs, locsFor(refs, vols), fragmentFilter{Rubric: "значение съезда"}, orderByRubric)
	if len(got) != 2 {
		t.Fatalf("плоский фильтр дал %d записей, ожидалось 2 из двух разных съездов", len(got))
	}
}

// При обоих параметрах сразу побеждает путь — правило названо веткой кода, а
// не порядком проверок. Лист второго адреса намеренно НЕ совпадает с
// плоским фильтром: если бы совпадал, мутант, снимающий победу пути (else
// if → if), давал бы тот же результат, что и верный код, — тест обязан
// отличать «путь выиграл» от «оба условия совпали случайно».
func TestCollectEntriesRubricPathBeatsFlatRubric(t *testing.T) {
	vols := buildVolumeMap([]models.VolumeLocation{vol(6, nil, 14, 0, 800)})
	refs := []*models.IndexReference{
		irefPath(1, 6, 100, 100, 1, "II съезд РСДРП", "значение съезда"),
		irefPath(2, 6, 120, 120, 2, "III съезд РСДРП", "организационные вопросы"),
	}

	got := collectEntries(refs, locsFor(refs, vols), fragmentFilter{
		Rubric:     "значение съезда",
		RubricPath: []string{"III съезд РСДРП"}, HasRubricPath: true,
	}, orderByRubric)
	if len(got) != 1 || got[0].Reference.ID != 2 {
		t.Fatalf("путь не победил плоское имя: %+v", got)
	}
}

// Адрес без пути вовсе (44 244 таких в издании 4) под путевой фильтр не
// попадает — и не должен: он не под этой подрубрикой.
func TestCollectEntriesRubriclessRefMissesPathFilter(t *testing.T) {
	vols := buildVolumeMap([]models.VolumeLocation{vol(6, nil, 14, 0, 800)})
	refs := []*models.IndexReference{iref(1, 6, 100, 100, "", 1)}

	got := collectEntries(refs, locsFor(refs, vols), fragmentFilter{
		RubricPath: []string{"II съезд РСДРП"}, HasRubricPath: true,
	}, orderByRubric)
	if len(got) != 0 {
		t.Fatalf("безрубричный адрес прошёл путевой фильтр: %+v", got)
	}
}

// Порядок групп обязан совпадать с раскладкой статьи: съезды идут в печатном
// порядке, внутри съезда — свои адреса, потом аспекты, и одинаковый лист под
// разными съездами В ОДИН БЛОК НЕ СЛИВАЕТСЯ. До этой правки все «значение
// съезда» получали один ранг и вставали подряд, независимо от съезда.
func TestCollectEntriesOrdersByRubricPathNotLeaf(t *testing.T) {
	vols := buildVolumeMap([]models.VolumeLocation{vol(6, nil, 14, 0, 800)})
	refs := []*models.IndexReference{
		irefPath(1, 6, 100, 100, 1, "II съезд РСДРП"),
		irefPath(2, 6, 110, 110, 2, "II съезд РСДРП", "значение съезда"),
		irefPath(3, 6, 115, 115, 3, "II съезд РСДРП", "о Бунде"),
		irefPath(4, 6, 200, 200, 4, "III съезд РСДРП", "значение съезда"),
		irefPath(5, 6, 210, 210, 5, "III съезд РСДРП", "о Бунде"),
	}

	entries := collectEntries(refs, locsFor(refs, vols), fragmentFilter{}, orderByRubric)

	got := make([]string, 0, len(entries))
	for _, e := range entries {
		got = append(got, strings.Join(rubricPathOf(e.Reference), " / "))
	}
	want := []string{
		"II съезд РСДРП",
		"II съезд РСДРП / значение съезда",
		"II съезд РСДРП / о Бунде",
		"III съезд РСДРП / значение съезда",
		"III съезд РСДРП / о Бунде",
	}
	if len(got) != len(want) {
		t.Fatalf("записей %d, ожидалось %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("порядок %v, ожидался %v", got, want)
		}
	}
}

// Безрубричные адреса (44 244 в издании 4) встают на месте ПЕРВОГО
// ПОЯВЛЕНИЯ, а не в начало и не в конец: ровно так их кладёт buildLevel на
// фронте, и разойтись этим двум раскладкам нельзя. Наивное «пустой путь
// раньше всех» ломает это молча.
func TestCollectEntriesRubriclessGroupKeepsFirstAppearance(t *testing.T) {
	vols := buildVolumeMap([]models.VolumeLocation{vol(6, nil, 14, 0, 800)})
	refs := []*models.IndexReference{
		irefPath(1, 6, 100, 100, 1, "определение"),
		iref(2, 6, 110, 110, "", 2),
		irefPath(3, 6, 120, 120, 3, "его мера"),
	}

	entries := collectEntries(refs, locsFor(refs, vols), fragmentFilter{}, orderByRubric)

	got := []string{
		entries[0].Reference.Rubric, entries[1].Reference.Rubric, entries[2].Reference.Rubric,
	}
	want := []string{"определение", "", "его мера"}
	if len(got) != len(want) {
		t.Fatalf("записей %d, ожидалось %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("группы %v, ожидались %v", got, want)
		}
	}
}

// Собственные адреса раздела идут ПЕРЕД его вложенными подрубриками
// безусловно — даже когда в указателе свой адрес напечатан ПОЗЖЕ аспекта.
// Так кладёт их buildLevel на фронте (refs рисуются до children), и
// разойтись этим двум раскладкам нельзя.
//
// Отдельной ветки в compare под этот исход нет: он держится двумя свойствами
// однословарного ranks — ранг префикса не может быть больше ранга его
// продолжения (считается по надмножеству адресов), а при ничьей пустое
// название (путь, кончившийся на этом уровне) проигрывает тай-брейку любому
// непустому. Оба свойства разобраны в комментарии у тай-брейка в compare.
//
// В живом корпусе такого случая сегодня нет: у всех семи съездов с детьми
// свой адрес печатается раньше аспектов (замер 22.09.2026), — поэтому
// ранжирование по order_number давало бы верный ответ и без этих двух
// свойств. Мутация, ломающая любое из них (см. отчёт задачи, круг правок 3),
// эту фикстуру всё равно роняет.
func TestCollectEntriesOwnRefsPrecedeNestedEvenWhenPrintedLater(t *testing.T) {
	vols := buildVolumeMap([]models.VolumeLocation{vol(6, nil, 14, 0, 800)})
	refs := []*models.IndexReference{
		irefPath(1, 6, 110, 110, 1, "II съезд РСДРП", "значение съезда"),
		irefPath(2, 6, 100, 100, 2, "II съезд РСДРП"),
	}

	entries := collectEntries(refs, locsFor(refs, vols), fragmentFilter{}, orderByRubric)

	got := make([]string, 0, len(entries))
	for _, e := range entries {
		got = append(got, strings.Join(rubricPathOf(e.Reference), " / "))
	}
	want := []string{"II съезд РСДРП", "II съезд РСДРП / значение съезда"}
	if len(got) != len(want) {
		t.Fatalf("записей %d, ожидалось %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("порядок %v, ожидался %v", got, want)
		}
	}
}
