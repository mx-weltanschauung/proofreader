package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

// Полка издания: предваряющие работы идут первыми, между собой — по тому,
// перед которым стоят. Полку отдаёт ListWorkSummaries (её и зовёт
// edition_handler.ListWorks) — не ListWorks, у которой свой, не читаемый
// хендлером метод и своя, но текстуально идентичная сортировка.
func TestListWorkSummariesPutsEditionFrontMatterFirst(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	works := NewWorkRepository(pool)
	editions := NewEditionRepository(pool)

	edition := &models.Edition{Title: "проба", Slug: "proba-efm"}
	if err := editions.Create(ctx, edition); err != nil {
		t.Fatalf("create edition: %v", err)
	}
	t.Cleanup(func() { _ = editions.Delete(ctx, edition.ID) })

	volume := newVolume(t, works, "том 1")
	volume.EditionID = &edition.ID
	one := 1
	volume.VolumeNumber = &one
	if err := works.Update(ctx, volume); err != nil {
		t.Fatalf("update volume: %v", err)
	}

	// Второе предисловие — к письмам, стоит перед томом 27. Проверяет
	// взаимный порядок двух предваряющих работ: precedes_volume=27 больше
	// precedes_volume=1, значит она должна остаться второй, а не запутаться
	// с volume_number обычных томов.
	lettersPreface := newVolume(t, works, "предисловие к письмам")
	lettersPreface.EditionID = &edition.ID
	lettersPreface.Role = models.WorkRoleEditionFrontMatter
	precedes27 := 27
	lettersPreface.PrecedesVolume = &precedes27
	if err := works.Update(ctx, lettersPreface); err != nil {
		t.Fatalf("update letters preface: %v", err)
	}

	preface := newVolume(t, works, "предисловие ко второму изданию")
	preface.EditionID = &edition.ID
	preface.Role = models.WorkRoleEditionFrontMatter
	precedes := 1
	preface.PrecedesVolume = &precedes
	if err := works.Update(ctx, preface); err != nil {
		t.Fatalf("update preface: %v", err)
	}

	list, err := editions.ListWorkSummaries(ctx, edition.ID)
	if err != nil {
		t.Fatalf("list work summaries: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("ожидались три работы, получено %d", len(list))
	}
	if list[0].ID != preface.ID {
		t.Fatalf("предисловие перед томом 1 должно стоять первым, а первым стоит %d", list[0].ID)
	}
	if list[0].Role != models.WorkRoleEditionFrontMatter {
		t.Fatalf("роль не доехала из запроса: %q", list[0].Role)
	}
	if list[0].PrecedesVolume == nil || *list[0].PrecedesVolume != 1 {
		t.Fatalf("precedes_volume не доехал: %v", list[0].PrecedesVolume)
	}
	if list[1].ID != lettersPreface.ID {
		t.Fatalf("предисловие перед томом 27 должно стоять вторым, а вторым стоит %d", list[1].ID)
	}
	if list[1].PrecedesVolume == nil || *list[1].PrecedesVolume != 27 {
		t.Fatalf("precedes_volume второго предисловия не доехал: %v", list[1].PrecedesVolume)
	}
	if list[2].ID != volume.ID {
		t.Fatalf("том должен стоять последним, а последним стоит %d", list[2].ID)
	}
}

// В общем каталоге предваряющих работ нет: он список книг, а не всего подряд.
func TestListCatalogueSkipsEditionFrontMatter(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	works := NewWorkRepository(pool)
	editions := NewEditionRepository(pool)

	edition := &models.Edition{Title: "проба", Slug: "proba-efm-cat"}
	if err := editions.Create(ctx, edition); err != nil {
		t.Fatalf("create edition: %v", err)
	}
	t.Cleanup(func() { _ = editions.Delete(ctx, edition.ID) })

	preface := newVolume(t, works, "предисловие вне каталога")
	preface.EditionID = &edition.ID
	preface.Role = models.WorkRoleEditionFrontMatter
	if err := works.Update(ctx, preface); err != nil {
		t.Fatalf("update preface: %v", err)
	}

	list, err := works.List(ctx, 500, 0, nil, nil)
	if err != nil {
		t.Fatalf("list works: %v", err)
	}
	for _, w := range list {
		if w.ID == preface.ID {
			t.Fatalf("предваряющая работа попала в каталог")
		}
	}
}

// chapter кладёт главу верхнего уровня работы. Протяжённость задаётся парой
// страниц, потому что именно из неё считается объём: (end - start + 1).
func chapter(t *testing.T, pool *pgxpool.Pool, workID int64, title string, start, end int) {
	t.Helper()
	chapters := NewChapterRepository(pool)
	c := &models.Chapter{
		WorkID: workID, Title: title, Type: "chapter",
		OrderNumber: start, StartPage: start, EndPage: end,
	}
	if err := chapters.Create(context.Background(), c); err != nil {
		t.Fatalf("create chapter %q: %v", title, err)
	}
	t.Cleanup(func() { _ = chapters.Delete(context.Background(), c.ID) })
}

// Аппарат тома — примечания и указатели — крупнее любой настоящей работы: в
// 45 томах Ленина «Примечания» стоят главой верхнего уровня. Называть том по
// ним нельзя, и в знаменатель доли они входить не должны.
func TestListWorkSummariesExcludesApparatus(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	works := NewWorkRepository(pool)
	editions := NewEditionRepository(pool)

	edition := &models.Edition{Title: "проба-аппарат", Slug: "proba-apparat"}
	if err := editions.Create(ctx, edition); err != nil {
		t.Fatalf("create edition: %v", err)
	}
	t.Cleanup(func() { _ = editions.Delete(ctx, edition.ID) })

	volume := newVolume(t, works, "том с аппаратом")
	volume.EditionID = &edition.ID
	one := 1
	volume.VolumeNumber = &one
	if err := works.Update(ctx, volume); err != nil {
		t.Fatalf("update volume: %v", err)
	}

	// Аппарат крупнее работы: 200 страниц против 100. Без отсева он победил бы
	// и стал подписью тома.
	chapter(t, pool, volume.ID, "Примечания", 301, 500)
	chapter(t, pool, volume.ID, "Указатель имен", 501, 600)
	chapter(t, pool, volume.ID, "ЧТО ДЕЛАТЬ?", 1, 100)
	chapter(t, pool, volume.ID, "ПИСЬМО В РЕДАКЦИЮ", 101, 150)

	got, err := editions.ListWorkSummaries(ctx, edition.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("томов %d, ожидался 1", len(got))
	}
	s := got[0]

	// chapters_total считает все главы верхнего уровня — это структурный
	// счётчик тома, он аппарат видит.
	if s.ChaptersTotal != 4 {
		t.Errorf("ChaptersTotal = %d, ожидалось 4", s.ChaptersTotal)
	}
	if len(s.TopChapters) != 2 {
		t.Fatalf("TopChapters = %+v, ожидались две неаппаратные работы", s.TopChapters)
	}
	if s.TopChapters[0].Title != "ЧТО ДЕЛАТЬ?" {
		t.Errorf("первая работа = %q, ожидалась «ЧТО ДЕЛАТЬ?»", s.TopChapters[0].Title)
	}
	if s.TopChapters[0].Pages != 100 {
		t.Errorf("страниц у первой = %d, ожидалось 100", s.TopChapters[0].Pages)
	}
	// Знаменатель — 150 (100 + 50), а не 350: аппарат в него не входит.
	if want := 100.0 / 150.0; s.TopChapters[0].Share < want-0.001 || s.TopChapters[0].Share > want+0.001 {
		t.Errorf("доля первой = %v, ожидалось %v (знаменатель без аппарата)", s.TopChapters[0].Share, want)
	}
	if s.TopChapters[1].Title != "ПИСЬМО В РЕДАКЦИЮ" {
		t.Errorf("вторая работа = %q", s.TopChapters[1].Title)
	}
}

// Список ограничен четырьмя: карточка тома показывает столько, а тянуть из
// базы все сто тридцать пять глав тома 23 ради четырёх строк незачем.
func TestListWorkSummariesReturnsFourLargestInOrder(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	works := NewWorkRepository(pool)
	editions := NewEditionRepository(pool)

	edition := &models.Edition{Title: "проба-четвёрка", Slug: "proba-chetverka"}
	if err := editions.Create(ctx, edition); err != nil {
		t.Fatalf("create edition: %v", err)
	}
	t.Cleanup(func() { _ = editions.Delete(ctx, edition.ID) })

	volume := newVolume(t, works, "том из шести работ")
	volume.EditionID = &edition.ID
	one := 1
	volume.VolumeNumber = &one
	if err := works.Update(ctx, volume); err != nil {
		t.Fatalf("update volume: %v", err)
	}

	// Кладём вперемешку, чтобы порядок в ответе задавался объёмом, а не
	// порядком вставки и не order_number.
	chapter(t, pool, volume.ID, "работа 30", 1, 30)
	chapter(t, pool, volume.ID, "работа 60", 31, 90)
	chapter(t, pool, volume.ID, "работа 10", 91, 100)
	chapter(t, pool, volume.ID, "работа 50", 101, 150)
	chapter(t, pool, volume.ID, "работа 20", 151, 170)
	chapter(t, pool, volume.ID, "работа 40", 171, 210)

	got, err := editions.ListWorkSummaries(ctx, edition.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	titles := make([]string, 0, len(got[0].TopChapters))
	for _, c := range got[0].TopChapters {
		titles = append(titles, c.Title)
	}
	want := []string{"работа 60", "работа 50", "работа 40", "работа 30"}
	if len(titles) != len(want) {
		t.Fatalf("TopChapters = %v, ожидались четыре крупнейшие %v", titles, want)
	}
	for i := range want {
		if titles[i] != want[i] {
			t.Fatalf("TopChapters = %v, ожидалось %v", titles, want)
		}
	}
}

// Том без глав — обычное состояние сразу после загрузки PDF. Список должен
// быть пуст, а не сорваться на делении на ноль.
func TestListWorkSummariesEmptyForVolumeWithoutChapters(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	works := NewWorkRepository(pool)
	editions := NewEditionRepository(pool)

	edition := &models.Edition{Title: "проба-пусто", Slug: "proba-pusto"}
	if err := editions.Create(ctx, edition); err != nil {
		t.Fatalf("create edition: %v", err)
	}
	t.Cleanup(func() { _ = editions.Delete(ctx, edition.ID) })

	volume := newVolume(t, works, "том без глав")
	volume.EditionID = &edition.ID
	one := 1
	volume.VolumeNumber = &one
	if err := works.Update(ctx, volume); err != nil {
		t.Fatalf("update volume: %v", err)
	}

	got, err := editions.ListWorkSummaries(ctx, edition.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got[0].TopChapters) != 0 {
		t.Errorf("TopChapters = %+v, ожидался пустой список", got[0].TopChapters)
	}
}

// Главная просит сводки всех собраний разом. Тяжёлые CTE запроса считаются по
// всему корпусу независимо от того, одно собрание спрашивают или все, поэтому
// один общий вызов стоит ровно столько же, сколько был один из четырёх. Сводки
// при этом обязаны совпадать с поединичными — иначе полка на главной и
// страница собрания разошлись бы в цифрах.
func TestListAllWorkSummariesMatchesPerEdition(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	works := NewWorkRepository(pool)
	editions := NewEditionRepository(pool)

	first := &models.Edition{Title: "проба-все-1", Slug: "proba-vse-1"}
	if err := editions.Create(ctx, first); err != nil {
		t.Fatalf("create edition: %v", err)
	}
	t.Cleanup(func() { _ = editions.Delete(ctx, first.ID) })

	second := &models.Edition{Title: "проба-все-2", Slug: "proba-vse-2"}
	if err := editions.Create(ctx, second); err != nil {
		t.Fatalf("create edition: %v", err)
	}
	t.Cleanup(func() { _ = editions.Delete(ctx, second.ID) })

	attach := func(edition *models.Edition, title string, number int) *models.Work {
		volume := newVolume(t, works, title)
		volume.EditionID = &edition.ID
		volume.VolumeNumber = &number
		if err := works.Update(ctx, volume); err != nil {
			t.Fatalf("update volume %q: %v", title, err)
		}
		return volume
	}

	one := attach(first, "все-том-1", 1)
	chapter(t, pool, one.ID, "Работа первая", 1, 40)
	chapter(t, pool, one.ID, "Примечания", 41, 90)
	attach(first, "все-том-2", 2)
	third := attach(second, "все-том-3", 1)
	chapter(t, pool, third.ID, "Работа третья", 1, 20)

	// Работа вне собраний в общий ответ попадать не должна: полка её не
	// рисует, а места в ответе она занимала бы наравне с томами.
	loose := newVolume(t, works, "все-без-собрания")
	_ = loose

	all, err := editions.ListAllWorkSummaries(ctx)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}

	byEdition := map[int64][]*models.VolumeSummary{}
	for _, s := range all {
		if s.EditionID == nil {
			t.Fatalf("в общем ответе работа без собрания: %q", s.Title)
		}
		byEdition[*s.EditionID] = append(byEdition[*s.EditionID], s)
	}

	for _, edition := range []*models.Edition{first, second} {
		want, err := editions.ListWorkSummaries(ctx, edition.ID)
		if err != nil {
			t.Fatalf("list %d: %v", edition.ID, err)
		}
		got := byEdition[edition.ID]
		if len(got) != len(want) {
			t.Fatalf("собрание %d: томов %d, ожидалось %d", edition.ID, len(got), len(want))
		}
		for i := range want {
			// Порядок внутри собрания обязан совпасть: полка рисует корешки
			// в том порядке, в каком их отдал запрос.
			if got[i].ID != want[i].ID {
				t.Errorf("собрание %d, место %d: том %d, ожидался %d",
					edition.ID, i, got[i].ID, want[i].ID)
			}
			if got[i].PagesTotal != want[i].PagesTotal {
				t.Errorf("том %d: PagesTotal = %d, ожидалось %d",
					got[i].ID, got[i].PagesTotal, want[i].PagesTotal)
			}
			if got[i].ChaptersTotal != want[i].ChaptersTotal {
				t.Errorf("том %d: ChaptersTotal = %d, ожидалось %d",
					got[i].ID, got[i].ChaptersTotal, want[i].ChaptersTotal)
			}
			if len(got[i].TopChapters) != len(want[i].TopChapters) {
				t.Errorf("том %d: TopChapters = %+v, ожидалось %+v",
					got[i].ID, got[i].TopChapters, want[i].TopChapters)
			}
		}
	}
}

// Что считается аппаратом, решает классификатор (TestIsApparatusTitle держит
// его смысл без базы). Здесь проверяется дорога целиком: заголовок → признак в
// chapters.is_apparatus при создании → фильтр в запросе сводок. И отдельно —
// ручная поправка: спорный случай человек правит, и правка переживает запрос.
func TestListWorkSummariesApparatusColumn(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	works := NewWorkRepository(pool)
	editions := NewEditionRepository(pool)
	chapters := NewChapterRepository(pool)

	edition := &models.Edition{Title: "проба-признак", Slug: "proba-priznak"}
	if err := editions.Create(ctx, edition); err != nil {
		t.Fatalf("create edition: %v", err)
	}
	t.Cleanup(func() { _ = editions.Delete(ctx, edition.ID) })

	// Три тома: аппарат, работа с похожим началом заголовка и аппарат, который
	// человек объявил работой.
	titles := []string{"Примечания", "Указания читателю", "Указатель имен"}
	made := make([]*models.Chapter, len(titles))
	for i, title := range titles {
		volume := newVolume(t, works, "том-"+title)
		volume.EditionID = &edition.ID
		number := i + 1
		volume.VolumeNumber = &number
		if err := works.Update(ctx, volume); err != nil {
			t.Fatalf("update volume %q: %v", title, err)
		}
		c := &models.Chapter{
			WorkID: volume.ID, Title: title, Type: "chapter",
			OrderNumber: 1, StartPage: 1, EndPage: 10,
		}
		if err := chapters.Create(ctx, c); err != nil {
			t.Fatalf("create chapter %q: %v", title, err)
		}
		t.Cleanup(func() { _ = chapters.Delete(context.Background(), c.ID) })
		made[i] = c
	}

	if !made[0].IsApparatus {
		t.Error("«Примечания» создались не аппаратом")
	}
	if made[1].IsApparatus {
		t.Error("«Указания читателю» создались аппаратом")
	}

	// Поправка человека: третий том объявлен работой вопреки заголовку.
	made[2].IsApparatus = false
	if err := chapters.Update(ctx, made[2]); err != nil {
		t.Fatalf("update chapter: %v", err)
	}

	got, err := editions.ListWorkSummaries(ctx, edition.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != len(titles) {
		t.Fatalf("томов %d, ожидалось %d", len(got), len(titles))
	}
	// Порядок ответа — по volume_number, то есть тот же, что у titles.
	if len(got[0].TopChapters) != 0 {
		t.Errorf("«Примечания»: TopChapters = %+v, ожидался аппарат (пусто)", got[0].TopChapters)
	}
	if len(got[1].TopChapters) != 1 {
		t.Errorf("«Указания читателю»: TopChapters = %+v, ожидалась одна работа", got[1].TopChapters)
	}
	if len(got[2].TopChapters) != 1 {
		t.Errorf("поправленный «Указатель имен»: TopChapters = %+v, ожидалась одна работа — "+
			"ручная поправка не доехала до запроса", got[2].TopChapters)
	}
}

// Сколько томов в собрании ПО ПЛАНУ. Полка пишет «45 из 55», и второе число
// брать больше неоткуда: у Ленина и Маркса—Энгельса плановый объём не записан
// нигде — ни в заголовке, ни в данных, — а у остальных стоит в заголовке
// словами, откуда его пришлось бы угадывать разбором строки.
func TestEditionRoundTripsVolumesPlanned(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	editions := NewEditionRepository(pool)

	planned := 55
	edition := &models.Edition{Title: "проба", Slug: "proba-planned", VolumesPlanned: &planned}
	if err := editions.Create(ctx, edition); err != nil {
		t.Fatalf("create edition: %v", err)
	}
	t.Cleanup(func() { _ = editions.Delete(ctx, edition.ID) })

	got, err := editions.GetByID(ctx, edition.ID)
	if err != nil {
		t.Fatalf("get edition: %v", err)
	}
	if got.VolumesPlanned == nil {
		t.Fatalf("VolumesPlanned = nil, ожидалось 55")
	}
	if *got.VolumesPlanned != 55 {
		t.Fatalf("VolumesPlanned = %d, ожидалось 55", *got.VolumesPlanned)
	}
}

// Собрание, у которого плановый объём неизвестен, остаётся без него: подпись
// полки тогда просто не называет второго числа, а не пишет «45 из 0».
func TestEditionWithoutPlanKeepsVolumesPlannedEmpty(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	editions := NewEditionRepository(pool)

	edition := &models.Edition{Title: "проба", Slug: "proba-no-plan"}
	if err := editions.Create(ctx, edition); err != nil {
		t.Fatalf("create edition: %v", err)
	}
	t.Cleanup(func() { _ = editions.Delete(ctx, edition.ID) })

	got, err := editions.GetByID(ctx, edition.ID)
	if err != nil {
		t.Fatalf("get edition: %v", err)
	}
	if got.VolumesPlanned != nil {
		t.Fatalf("VolumesPlanned = %d, ожидалась пустота", *got.VolumesPlanned)
	}
}

// Плановый объём правится вместе с остальным собранием: он приезжает из
// выходных данных издания, и человеку случается вписать его позже.
func TestEditionUpdateSavesVolumesPlanned(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	editions := NewEditionRepository(pool)

	edition := &models.Edition{Title: "проба", Slug: "proba-plan-update"}
	if err := editions.Create(ctx, edition); err != nil {
		t.Fatalf("create edition: %v", err)
	}
	t.Cleanup(func() { _ = editions.Delete(ctx, edition.ID) })

	planned := 24
	edition.VolumesPlanned = &planned
	if err := editions.Update(ctx, edition); err != nil {
		t.Fatalf("update edition: %v", err)
	}

	got, err := editions.GetByID(ctx, edition.ID)
	if err != nil {
		t.Fatalf("get edition: %v", err)
	}
	if got.VolumesPlanned == nil || *got.VolumesPlanned != 24 {
		t.Fatalf("VolumesPlanned = %v, ожидалось 24", got.VolumesPlanned)
	}
}
