package repository

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"proofreader/internal/models"
)

// Снятие аппарата — операция над настоящей схемой: она опирается на каскады
// (подглава по parent_id, версии и вырезки по page_id, служебный ребёнок по
// parent_work_id) и на диапазоны глав. Подделкой такое не проверить — отсюда
// интеграционный тест. Без PROOFREADER_TEST_DB_URL он пропускается.
func apparatusFixture(t *testing.T, works *WorkRepository, chapters *ChapterRepository, pages *PageRepository) *models.Work {
	t.Helper()
	ctx := context.Background()
	vol := newVolume(t, works, "проба-аппарат")

	// Полосы 1—10: 1—6 тело, 7—10 аппарат.
	for n := 1; n <= 10; n++ {
		p := &models.Page{
			WorkID: vol.ID, PageNumber: n,
			PreviewPath:     "works/proba/pages/page_" + strconv.Itoa(n) + ".png",
			ContentMarkdown: "полоса " + strconv.Itoa(n),
			Status:          models.PageStatusNotProofread,
		}
		if err := pages.Create(ctx, p); err != nil {
			t.Fatalf("create page %d: %v", n, err)
		}
	}

	body := &models.Chapter{
		WorkID: vol.ID, Title: "Настоящая работа", Type: "chapter",
		OrderNumber: 1, StartPage: 1, EndPage: 6,
	}
	if err := chapters.Create(ctx, body); err != nil {
		t.Fatalf("create body chapter: %v", err)
	}
	notes := &models.Chapter{
		WorkID: vol.ID, Title: "Примечания", Type: "chapter",
		OrderNumber: 2, StartPage: 7, EndPage: 10,
	}
	if err := chapters.Create(ctx, notes); err != nil {
		t.Fatalf("create apparatus chapter: %v", err)
	}
	if !notes.IsApparatus {
		t.Fatalf("глава «Примечания» не размечена аппаратом — фикстура ничего не проверит")
	}
	// Подглава внутри аппарата: своим заголовком она не опознаётся, признак
	// приходит наследованием, и снос обязан унести её вместе с родителем.
	sub := &models.Chapter{
		WorkID: vol.ID, ParentID: &notes.ID, Title: "К главе первой", Type: "chapter",
		OrderNumber: 1, StartPage: 7, EndPage: 8,
	}
	if err := chapters.Create(ctx, sub); err != nil {
		t.Fatalf("create subchapter: %v", err)
	}
	if !sub.IsApparatus {
		t.Fatalf("подглава аппарата не унаследовала признак — фикстура неверна")
	}
	return vol
}

func TestApparatusRemoveTakesRangesAndLeavesBody(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	works := NewWorkRepository(pool)
	chapters := NewChapterRepository(pool)
	pages := NewPageRepository(pool)
	apparatus := NewApparatusRepository(pool)

	vol := apparatusFixture(t, works, chapters, pages)

	plan, err := apparatus.Plan(ctx, vol.ID)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.PageCount != 4 {
		t.Errorf("план насчитал %d полос аппарата, ожидалось 4 (7—10)", plan.PageCount)
	}
	if plan.ChapterCount != 2 {
		t.Errorf("план насчитал %d глав аппарата, ожидалось 2 (глава и подглава)", plan.ChapterCount)
	}
	if len(plan.Chapters) != 1 || plan.Chapters[0].Title != "Примечания" {
		t.Errorf("в списке плана %+v, ожидалась одна глава верхнего уровня", plan.Chapters)
	}
	if len(plan.StoragePaths) != 4 {
		t.Errorf("план собрал %d ключей превью, ожидалось 4", len(plan.StoragePaths))
	}

	// Щель, найденная замером на живом корпусе: снятие идёт по диапазонам
	// глав, и полосы вне всех глав остаются. В фикстуре главы кончаются на
	// 10-й полосе, поэтому здесь ноль; тест ниже добавляет полосу за ними.
	if plan.PagesOutsideChapters != 0 {
		t.Errorf("план насчитал %d полос вне глав, ожидался 0", plan.PagesOutsideChapters)
	}

	done, err := apparatus.Remove(ctx, vol.ID)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if done.PageCount != plan.PageCount || done.ChapterCount != plan.ChapterCount {
		t.Errorf("отчёт о сносе (%d полос, %d глав) разошёлся с планом (%d, %d)",
			done.PageCount, done.ChapterCount, plan.PageCount, plan.ChapterCount)
	}

	left, err := pages.ListByWork(ctx, vol.ID)
	if err != nil {
		t.Fatalf("ListByWork: %v", err)
	}
	if len(left) != 6 {
		t.Fatalf("после сноса осталось %d полос, ожидалось 6", len(left))
	}
	for _, p := range left {
		if p.PageNumber > 6 {
			t.Errorf("полоса аппарата %d пережила снос", p.PageNumber)
		}
	}

	tree, err := chapters.ListByWork(ctx, vol.ID)
	if err != nil {
		t.Fatalf("ListByWork глав: %v", err)
	}
	if len(tree) != 1 || tree[0].Title != "Настоящая работа" {
		t.Errorf("после сноса остались главы %+v, ожидалась одна — тело", tree)
	}
}

// PageNumbers — то же правило, что у сноса: полосы 7—10 фикстуры, и ровно
// столько, сколько насчитал план. Статическая читальня вырезает по нему
// аппарат из архива, и расхождение с Remove вернуло бы снятое на люди.
func TestApparatusPageNumbersMatchPlan(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	works := NewWorkRepository(pool)
	chapters := NewChapterRepository(pool)
	pages := NewPageRepository(pool)
	apparatus := NewApparatusRepository(pool)

	vol := apparatusFixture(t, works, chapters, pages)

	got, err := apparatus.PageNumbers(ctx, vol.ID)
	if err != nil {
		t.Fatalf("PageNumbers: %v", err)
	}
	if fmt.Sprint(got) != "[7 8 9 10]" {
		t.Errorf("полосы аппарата %v, ожидались [7 8 9 10]", got)
	}
	plan, err := apparatus.Plan(ctx, vol.ID)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(got) != plan.PageCount {
		t.Errorf("PageNumbers дал %d полос, план — %d", len(got), plan.PageCount)
	}
}

// Повторный снос — не ошибка, но и не «успех»: план пуст, и вызывающий обязан
// увидеть это сам. Отказывает вслух обработчик, репозиторий только отвечает.
func TestApparatusRemoveOnCleanVolumeIsEmpty(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	works := NewWorkRepository(pool)
	apparatus := NewApparatusRepository(pool)

	vol := newVolume(t, works, "проба-без-аппарата")

	plan, err := apparatus.Plan(ctx, vol.ID)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !plan.Empty() {
		t.Errorf("план на томе без аппарата не пуст: %+v", plan)
	}

	done, err := apparatus.Remove(ctx, vol.ID)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !done.Empty() {
		t.Errorf("снос на томе без аппарата отчитался о работе: %+v", done)
	}
}

func TestApparatusPlanOfMissingWorkErrs(t *testing.T) {
	pool := testPool(t)
	apparatus := NewApparatusRepository(pool)
	if _, err := apparatus.Plan(context.Background(), -1); err == nil {
		t.Error("план несуществующего тома не вернул ошибку")
	}
}

// Полосы, не накрытые ни одной главой, снятие аппарата не тронет — и план
// обязан сказать об этом числом. Не придирчивость: на живом ленинском томе 45
// такими оказались двенадцать последних полос, «Содержание» и колофон, то
// есть аппарат чистой воды. Невидимый остаток и есть худший исход снятия.
func TestApparatusPlanCountsPagesOutsideEveryChapter(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	works := NewWorkRepository(pool)
	chapters := NewChapterRepository(pool)
	pages := NewPageRepository(pool)
	apparatus := NewApparatusRepository(pool)

	vol := apparatusFixture(t, works, chapters, pages)

	// Полосы 11 и 12 — за последней главой, как «Содержание» в хвосте тома.
	for _, n := range []int{11, 12} {
		p := &models.Page{
			WorkID: vol.ID, PageNumber: n,
			ContentMarkdown: "## СОДЕРЖАНИЕ",
			Status:          models.PageStatusNotProofread,
		}
		if err := pages.Create(ctx, p); err != nil {
			t.Fatalf("create page %d: %v", n, err)
		}
	}

	plan, err := apparatus.Plan(ctx, vol.ID)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.PagesOutsideChapters != 2 {
		t.Errorf("план насчитал %d полос вне глав, ожидалось 2", plan.PagesOutsideChapters)
	}
	if plan.PageCount != 4 {
		t.Errorf("полосы вне глав попали в счёт снимаемых: %d вместо 4", plan.PageCount)
	}

	if _, err := apparatus.Remove(ctx, vol.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	left, err := pages.ListByWork(ctx, vol.ID)
	if err != nil {
		t.Fatalf("ListByWork: %v", err)
	}
	var outside int
	for _, p := range left {
		if p.PageNumber > 10 {
			outside++
		}
	}
	if outside != 2 {
		t.Errorf("после снятия за главами осталось %d полос, ожидалось 2", outside)
	}
}

// Глава-аппарат может сидеть подглавой внутри НАСТОЯЩЕЙ работы: префиксное
// правило классификатора срабатывает на любой глубине, и «Приложение» внутри
// «Теорий прибавочной стоимости» помечено аппаратом — а это сто полос текста
// самого Маркса. По корпусу таких глав 39.
//
// Снос берёт их (условие смотрит на is_apparatus, а не на уровень), поэтому
// план ОБЯЗАН их показывать. Первая версия перечисляла только parent_id IS
// NULL: оператор увидел бы один «Предметный указатель», подтвердил бы снятие и
// необратимо снёс бы сто полос тела. Это худший исход, какой у рычага есть.
//
// Раньше такие главы делал классификатор, и корпус набрал их 40 штук на 344
// полосы; правило сужено, разметка снята миграцией 000021. Остался один путь —
// ручная пометка, и она оправдывает эту строку плана ровно так же.
func TestApparatusPlanNamesApparatusNestedInRealWork(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	works := NewWorkRepository(pool)
	chapters := NewChapterRepository(pool)
	pages := NewPageRepository(pool)
	apparatus := NewApparatusRepository(pool)

	vol := apparatusFixture(t, works, chapters, pages)

	// «Приложение» внутри настоящей работы: родитель аппаратом НЕ помечен.
	body, err := chapters.ListByWork(ctx, vol.ID)
	if err != nil {
		t.Fatalf("ListByWork: %v", err)
	}
	var realWorkID int64
	for _, c := range body {
		if c.Title == "Настоящая работа" {
			realWorkID = c.ID
		}
	}
	if realWorkID == 0 {
		t.Fatal("в фикстуре нет настоящей работы")
	}
	// Признак ставится ЯВНО, рукой. С 15.09.2026 классификатор такую главу не
	// метит — по заголовку аппарат опознаётся только у главы верхнего уровня
	// (models.IsApparatusTitle), и ровно эта разметка была снята с корпуса
	// миграцией 000021 как ошибочная. Но ручная пометка вглубь осталась
	// законной: PUT /works/{id}/chapters/{id} принимает is_apparatus, и Create
	// уважает выставленное значение. Пока она возможна, план обязан такие
	// главы называть — иначе снос унесёт их молча.
	nested := &models.Chapter{
		WorkID: vol.ID, ParentID: &realWorkID, Title: "Приложение", Type: "chapter",
		OrderNumber: 1, StartPage: 4, EndPage: 6, IsApparatus: true,
	}
	if err := chapters.Create(ctx, nested); err != nil {
		t.Fatalf("create nested: %v", err)
	}
	if !nested.IsApparatus {
		t.Fatal("ручная пометка не доехала до базы — тест ничего не проверит")
	}

	plan, err := apparatus.Plan(ctx, vol.ID)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	found := false
	for _, ch := range plan.Chapters {
		if ch.ID != nested.ID {
			continue
		}
		found = true
		if ch.ParentTitle != "Настоящая работа" {
			t.Errorf("план не сказал, внутри чего она сидит: %q", ch.ParentTitle)
		}
	}
	if !found {
		t.Fatalf("план не назвал главу-аппарат внутри настоящей работы; в плане: %+v", plan.Chapters)
	}
	// И её полосы обязаны быть в счёте снимаемых — иначе число врёт.
	if plan.PageCount != 7 {
		t.Errorf("план насчитал %d полос, ожидалось 7 (4—10)", plan.PageCount)
	}
}

// Обратная половина: подглава ВНУТРИ аппарата отдельной строкой не печатается —
// она уходит вместе с родителем, и у ленинского тома план иначе был бы на сотню
// строк.
func TestApparatusPlanFoldsSubchaptersOfApparatus(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	works := NewWorkRepository(pool)
	chapters := NewChapterRepository(pool)
	pages := NewPageRepository(pool)
	apparatus := NewApparatusRepository(pool)

	vol := apparatusFixture(t, works, chapters, pages)

	plan, err := apparatus.Plan(ctx, vol.ID)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Chapters) != 1 || plan.Chapters[0].Title != "Примечания" {
		t.Fatalf("в плане %+v, ожидались одни «Примечания»", plan.Chapters)
	}
	if plan.Chapters[0].Subchapters != 1 {
		t.Errorf("подглава не посчитана: %d", plan.Chapters[0].Subchapters)
	}
	if plan.Chapters[0].ParentTitle != "" {
		t.Errorf("у главы верхнего уровня проставлен родитель: %q", plan.Chapters[0].ParentTitle)
	}
}

// Сторож проводки: Create обязан спросить классификатор про МЕСТО главы, а не
// только про заголовок.
//
// models.IsApparatusTitle проверена таблицей без базы, но эта проверка зелена и
// тогда, когда оркестратор передаёт topLevel неверно — скажем, константой.
// Здесь проверяется сам шов: одно и то же слово даёт разный ответ наверху и
// внутри работы.
//
// Слово-образец — «Указатель имен». Раньше здесь стояли «Приложения», но после
// 000022 они не аппарат ни на какой глубине и шва больше не показывают: тест
// стал бы зелёным при любом ответе классификатора. Образцом обязано быть
// слово, ОСТАВШЕЕСЯ в models.apparatusPrefixes, иначе сторож сторожит пустоту.
// Заодно ниже проверено и само сужение — «Приложения» наверху больше не
// аппарат.
func TestChapterCreateAsksClassifierAboutDepth(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	works := NewWorkRepository(pool)
	chapters := NewChapterRepository(pool)
	pages := NewPageRepository(pool)

	vol := apparatusFixture(t, works, chapters, pages)

	top := &models.Chapter{
		WorkID: vol.ID, Title: "Указатель имен", Type: "chapter",
		OrderNumber: 90, StartPage: 4, EndPage: 5,
	}
	if err := chapters.Create(ctx, top); err != nil {
		t.Fatalf("create top-level: %v", err)
	}
	if !top.IsApparatus {
		t.Error("«Указатель имен» главой верхнего уровня не размечены аппаратом")
	}

	nested := &models.Chapter{
		WorkID: vol.ID, ParentID: &top.ID, Title: "Указатель имен", Type: "chapter",
		OrderNumber: 91, StartPage: 4, EndPage: 4,
	}
	if err := chapters.Create(ctx, nested); err != nil {
		t.Fatalf("create nested under apparatus: %v", err)
	}
	if !nested.IsApparatus {
		t.Error("подглава внутри аппарата не унаследовала признак")
	}

	// То же слово внутри настоящей работы — не аппарат: по заголовку подглава
	// признак не получает вовсе, а родитель его не несёт.
	body, err := chapters.ListByWork(ctx, vol.ID)
	if err != nil {
		t.Fatalf("ListByWork: %v", err)
	}
	var realWorkID int64
	for _, c := range body {
		if c.Title == "Настоящая работа" {
			realWorkID = c.ID
		}
	}
	if realWorkID == 0 {
		t.Fatal("в фикстуре нет настоящей работы")
	}
	inBody := &models.Chapter{
		WorkID: vol.ID, ParentID: &realWorkID, Title: "Указатель имен", Type: "chapter",
		OrderNumber: 92, StartPage: 4, EndPage: 4,
	}
	if err := chapters.Create(ctx, inBody); err != nil {
		t.Fatalf("create nested in real work: %v", err)
	}
	if inBody.IsApparatus {
		t.Error("«Указатель имен» подглавой внутри настоящей работы помечены аппаратом — правило снова работает на любой глубине")
	}

	// Сужение 000022 на этом же шве: «Приложения» наверху — текст автора, а не
	// аппарат. Проверяется здесь, а не только таблицей в internal/models,
	// потому что признак ставит Create, и вернуть слово в список можно было бы
	// незаметно для табличного теста, если бы тот отстал.
	appendix := &models.Chapter{
		WorkID: vol.ID, Title: "Приложения", Type: "chapter",
		OrderNumber: 93, StartPage: 4, EndPage: 5,
	}
	if err := chapters.Create(ctx, appendix); err != nil {
		t.Fatalf("create top-level appendix: %v", err)
	}
	if appendix.IsApparatus {
		t.Error("«Приложения» главой верхнего уровня помечены аппаратом — сужение 000022 отменено")
	}
}

// Снятие тома снимает статью предметного указателя, пришедшую ИЗ этого тома
// (index_concept_articles.work_id), и вместе с ней — понятие, если у него не
// осталось ни одной другой статьи; но не трогает понятие, у которого есть
// статья от ДРУГОГО тома/издания — её снятие было бы чужой потерей.
//
// Регрессия: миграция 000026 (задача 11) сняла с index_concepts колонки
// work_id/kind, а Remove/apparatusPlan до этого теста читали и писали именно
// их старой SQL-строкой — go build и go vet такое не ловят, это просто
// строка. До этого теста ни один тест пакета не заводил ни одной строки в
// index_concepts/index_concept_articles при проверке ApparatusRepository —
// баг был найден только вычитыванием кода, а не прогоном тестов.
func TestApparatusRemoveDropsOwnConceptArticleKeepsForeign(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	works := NewWorkRepository(pool)
	chapters := NewChapterRepository(pool)
	pages := NewPageRepository(pool)
	apparatus := NewApparatusRepository(pool)
	editions := NewEditionRepository(pool)

	vol := apparatusFixture(t, works, chapters, pages)

	edition := &models.Edition{Title: "проба-аппарат-издание", Slug: "proba-apparatus-edition"}
	if err := editions.Create(ctx, edition); err != nil {
		t.Fatalf("create edition: %v", err)
	}
	t.Cleanup(func() { _ = editions.Delete(ctx, edition.ID) })
	vol.EditionID = &edition.ID
	if err := works.Update(ctx, vol); err != nil {
		t.Fatalf("attach edition to volume: %v", err)
	}

	// Понятие с одной-единственной статьёй — пришедшей из снимаемого тома.
	// У него не должно остаться ни одной статьи после снятия, и само понятие
	// обязано исчезнуть как осиротевшее.
	onlyHere := seedConcept(t, pool, "Абстрактный труд", "abstraktnyj-trud-apparatus")
	seedArticle(t, pool, onlyHere, edition.ID, &vol.ID, "Абстрактный труд")

	// Понятие с двумя статьями: одна из снимаемого тома, другая — из чужого
	// издания без work_id вовсе (внешний источник). Понятие обязано выжить, и
	// чужая статья — тоже; снимается только своя.
	shared := seedConcept(t, pool, "Прибавочная стоимость", "pribavochnaya-stoimost-apparatus")
	seedArticle(t, pool, shared, edition.ID, &vol.ID, "Прибавочная стоимость")

	otherEdition := &models.Edition{Title: "проба-чужое-издание", Slug: "proba-foreign-edition"}
	if err := editions.Create(ctx, otherEdition); err != nil {
		t.Fatalf("create foreign edition: %v", err)
	}
	t.Cleanup(func() { _ = editions.Delete(ctx, otherEdition.ID) })
	foreignArticle := seedArticle(t, pool, shared, otherEdition.ID, nil, "Прибавочная стоимость")

	// Понятие вовсе не связанное со снимаемым томом — контроль, что снос его
	// не задевает вообще.
	untouched := seedConcept(t, pool, "Товарный фетишизм", "tovarnyj-fetishizm-apparatus")
	seedArticle(t, pool, untouched, otherEdition.ID, nil, "Товарный фетишизм")

	plan, err := apparatus.Plan(ctx, vol.ID)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.ConceptCount != 2 {
		t.Errorf("план насчитал %d статей указателя своего тома, ожидалось 2", plan.ConceptCount)
	}

	if _, err := apparatus.Remove(ctx, vol.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	var onlyHereGone bool
	if err := pool.QueryRow(ctx,
		`SELECT NOT EXISTS(SELECT 1 FROM index_concepts WHERE id = $1)`, onlyHere,
	).Scan(&onlyHereGone); err != nil {
		t.Fatalf("check onlyHere: %v", err)
	}
	if !onlyHereGone {
		t.Errorf("понятие %d без статей вне снятого тома пережило снос — осиротевшая уборка не сработала", onlyHere)
	}

	var sharedLeft bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM index_concepts WHERE id = $1)`, shared,
	).Scan(&sharedLeft); err != nil {
		t.Fatalf("check shared: %v", err)
	}
	if !sharedLeft {
		t.Errorf("понятие %d со статьёй в другом издании было снесено вместе с томом", shared)
	}

	var sharedArticleCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM index_concept_articles WHERE concept_id = $1`, shared,
	).Scan(&sharedArticleCount); err != nil {
		t.Fatalf("count shared articles: %v", err)
	}
	if sharedArticleCount != 1 {
		t.Errorf("у понятия %d осталось %d статей, ожидалась 1 (чужая)", shared, sharedArticleCount)
	}

	var foreignArticleLeft bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM index_concept_articles WHERE id = $1)`, foreignArticle,
	).Scan(&foreignArticleLeft); err != nil {
		t.Fatalf("check foreign article: %v", err)
	}
	if !foreignArticleLeft {
		t.Errorf("статья %d из чужого издания снесена вместе с томом", foreignArticle)
	}

	var untouchedLeft bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM index_concepts WHERE id = $1)`, untouched,
	).Scan(&untouchedLeft); err != nil {
		t.Fatalf("check untouched: %v", err)
	}
	if !untouchedLeft {
		t.Errorf("понятие %d, никак не связанное со снимаемым томом, пропало", untouched)
	}
}

// Раунд правок 1, находка 1: уборка осиротевших понятий обязана быть
// ограничена тем, что снос статей этого тома реально задел, а не всей
// таблицей index_concepts. Понятие без единой статьи заводится штатно —
// здесь оно получается тем же способом, каким это бывает в жизни: у статьи
// издание обязательно (NOT NULL), и удаление издания каскадом сносит все его
// статьи, оставляя понятие сиротой до следующего ввоза. Такое понятие не
// имеет никакого отношения к снимаемому тому, и снос аппарата чужого тома не
// вправе унести его заодно.
func TestApparatusRemoveDoesNotSweepUnrelatedOrphanConcept(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	works := NewWorkRepository(pool)
	chapters := NewChapterRepository(pool)
	pages := NewPageRepository(pool)
	apparatus := NewApparatusRepository(pool)
	editions := NewEditionRepository(pool)

	vol := apparatusFixture(t, works, chapters, pages)

	edition := &models.Edition{Title: "проба-аппарат-издание-2", Slug: "proba-apparatus-edition-2"}
	if err := editions.Create(ctx, edition); err != nil {
		t.Fatalf("create edition: %v", err)
	}
	t.Cleanup(func() { _ = editions.Delete(ctx, edition.ID) })
	vol.EditionID = &edition.ID
	if err := works.Update(ctx, vol); err != nil {
		t.Fatalf("attach edition to volume: %v", err)
	}

	// Понятие этого тома — снос обязан унести именно его.
	own := seedConcept(t, pool, "Прибавочный продукт", "pribavochnyj-produkt-apparatus")
	seedArticle(t, pool, own, edition.ID, &vol.ID, "Прибавочный продукт")

	// Понятие-сирота от СОВСЕМ ДРУГОГО, уже удалённого издания: заведено до
	// удаления издания, статья ушла каскадом, понятие осталось без единой
	// статьи. Со снимаемым томом не связано никак.
	strandedEdition := &models.Edition{Title: "проба-исчезнувшее-издание", Slug: "proba-vanished-edition"}
	if err := editions.Create(ctx, strandedEdition); err != nil {
		t.Fatalf("create stranded edition: %v", err)
	}
	stranded := seedConcept(t, pool, "Меновая стоимость", "menovaya-stoimost-apparatus")
	seedArticle(t, pool, stranded, strandedEdition.ID, nil, "Меновая стоимость")
	if err := editions.Delete(ctx, strandedEdition.ID); err != nil {
		t.Fatalf("delete stranded edition: %v", err)
	}
	var strandedIsOrphan bool
	if err := pool.QueryRow(ctx,
		`SELECT NOT EXISTS(SELECT 1 FROM index_concept_articles WHERE concept_id = $1)`, stranded,
	).Scan(&strandedIsOrphan); err != nil {
		t.Fatalf("check stranded is orphan: %v", err)
	}
	if !strandedIsOrphan {
		t.Fatal("удаление издания не оставило понятие сиротой — фикстура не воспроизводит штатный случай")
	}

	if _, err := apparatus.Remove(ctx, vol.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	var ownGone bool
	if err := pool.QueryRow(ctx,
		`SELECT NOT EXISTS(SELECT 1 FROM index_concepts WHERE id = $1)`, own,
	).Scan(&ownGone); err != nil {
		t.Fatalf("check own: %v", err)
	}
	if !ownGone {
		t.Errorf("понятие %d снимаемого тома пережило снос", own)
	}

	var strandedLeft bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM index_concepts WHERE id = $1)`, stranded,
	).Scan(&strandedLeft); err != nil {
		t.Fatalf("check stranded: %v", err)
	}
	if !strandedLeft {
		t.Errorf("понятие-сирота %d от чужого удалённого издания снесено сносом другого тома", stranded)
	}
}

func TestApparatusPlanListsHumanRecordingKeys(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	w := seedWorkWithPages(t, pool, "том", "текст", "примечания")
	body := seedChapter(t, pool, w, "Работа", 1, 1)
	notes := seedChapter(t, pool, w, "Примечания", 2, 2)
	if _, err := pool.Exec(ctx, `UPDATE chapters SET is_apparatus = true WHERE id = $1`, notes); err != nil {
		t.Fatal(err)
	}
	// Неразмеченная подглава размеченной главы: каскад по parent_id уносит и её.
	child := seedChapter(t, pool, w, "Подглава", 2, 2)
	if _, err := pool.Exec(ctx, `UPDATE chapters SET parent_id = $1 WHERE id = $2`, notes, child); err != nil {
		t.Fatal(err)
	}
	recs := NewAudioRecordingRepository(pool)
	inBody, inNotes, inChild := newRecording(w, body, 1), newRecording(w, notes, 2), newRecording(w, child, 3)
	for _, rec := range []*models.AudioRecording{inBody, inNotes, inChild} {
		if err := recs.CreateRecording(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	plan, err := NewApparatusRepository(pool).Plan(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.AudioPaths) != 2 || plan.AudioPaths[0] != inNotes.S3Key || plan.AudioPaths[1] != inChild.S3Key {
		t.Errorf("AudioPaths = %v, ждали [%s %s]", plan.AudioPaths, inNotes.S3Key, inChild.S3Key)
	}
}

// Синтез аппарата не озвучивает (раскладка tracks.py пропускает поддеревья
// is_apparatus), но дорожка, задевшая полосу аппарата, — законный след
// прошлой раскладки или ручной разметки после синтеза. Снятие обязано унести
// её строку и отдать ключ объекта: иначе звук полос, снятых по письму,
// отдавался бы вечно. Дорожка тела и чужой том с теми же номерами полос не
// задеты.
func TestApparatusRemoveDropsTracksOverlappingApparatusPages(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	w := seedWorkWithPages(t, pool, "том", "раз", "два", "три", "четыре")
	seedChapter(t, pool, w, "Работа", 1, 2)
	notes := seedChapter(t, pool, w, "Примечания", 3, 4)
	if _, err := pool.Exec(ctx, `UPDATE chapters SET is_apparatus = true WHERE id = $1`, notes); err != nil {
		t.Fatal(err)
	}
	other := seedWorkWithPages(t, pool, "чужой том", "раз", "два", "три", "четыре")

	audioRepo := NewAudioRepository(pool)
	body := trackFor(w, []string{"раз"}, 1, 1)
	across := trackFor(w, []string{"два", "три"}, 2, 3) // тело и аппарат разом
	inNotes := trackFor(w, []string{"четыре"}, 4, 4)
	if _, _, err := audioRepo.RegisterTracks(ctx, w, []models.AudioTrack{body, across, inNotes}); err != nil {
		t.Fatal(err)
	}
	foreign := trackFor(other, []string{"три"}, 3, 3)
	if _, _, err := audioRepo.RegisterTracks(ctx, other, []models.AudioTrack{foreign}); err != nil {
		t.Fatal(err)
	}

	apparatus := NewApparatusRepository(pool)
	plan, err := apparatus.Plan(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.TrackPaths) != 2 {
		t.Errorf("план: TrackPaths = %v, ждали две дорожки", plan.TrackPaths)
	}

	done, err := apparatus.Remove(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if len(done.TrackPaths) != 2 || done.TrackPaths[0] != across.S3Key || done.TrackPaths[1] != inNotes.S3Key {
		t.Errorf("TrackPaths = %v, ждали [%s %s]", done.TrackPaths, across.S3Key, inNotes.S3Key)
	}
	left, err := audioRepo.ListTracks(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].S3Key != body.S3Key {
		t.Errorf("после снятия дорожки тома: %+v, ждали одну дорожку тела", left)
	}
	if list, _ := audioRepo.ListTracks(ctx, other); len(list) != 1 {
		t.Errorf("снятие задело дорожку чужого тома: %+v", list)
	}
}

// Тикет 06: правка полосы аппарата в ту же секунду, что снятие аппарата.
// SaveEdit берёт полосу раньше дорожек (UPDATE pages, затем пометка stale),
// поэтому и снятие обязано запирать полосы раньше, чем сносить дорожки, —
// иначе встречный порядок даёт взаимную блокировку, и Postgres обрывает одну
// сторону (40P01). Транзакция ниже повторяет порядок SaveEdit по шагам:
// одним вызовом SaveEdit не остановить посередине.
func TestApparatusRemoveDoesNotDeadlockWithPageEdit(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	w := seedWorkWithPages(t, pool, "том", "раз", "два", "три", "четыре")
	seedChapter(t, pool, w, "Работа", 1, 2)
	notes := seedChapter(t, pool, w, "Примечания", 3, 4)
	if _, err := pool.Exec(ctx, `UPDATE chapters SET is_apparatus = true WHERE id = $1`, notes); err != nil {
		t.Fatal(err)
	}
	audioRepo := NewAudioRepository(pool)
	if _, _, err := audioRepo.RegisterTracks(ctx, w, []models.AudioTrack{trackFor(w, []string{"четыре"}, 4, 4)}); err != nil {
		t.Fatal(err)
	}

	edit, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer edit.Rollback(ctx) //nolint:errcheck // после Commit это no-op
	if _, err := edit.Exec(ctx,
		`UPDATE pages SET content_markdown = 'правка' WHERE work_id = $1 AND page_number = 4`, w); err != nil {
		t.Fatal(err)
	}

	editPID := backendPID(t, ctx, edit)
	removed := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		_, err := NewApparatusRepository(pool).Remove(ctx, w)
		removed <- err
		close(finished)
	}()
	// Снятие встало на замок правки — только теперь правка идёт к дорожкам.
	if waitBlockedBy(t, ctx, editPID, finished) == 0 {
		t.Fatal("снятие аппарата так и не встало на замок правки")
	}

	if _, err := edit.Exec(ctx,
		`UPDATE audio_tracks SET stale = true WHERE work_id = $1 AND 4 BETWEEN start_page AND end_page`, w); err != nil {
		t.Fatalf("правка полосы оборвана: %v", err)
	}
	if err := edit.Commit(ctx); err != nil {
		t.Fatalf("правка полосы не закоммитилась: %v", err)
	}
	if err := <-removed; err != nil {
		t.Fatalf("снятие аппарата оборвано: %v", err)
	}
	if left, _ := audioRepo.ListTracks(ctx, w); len(left) != 0 {
		t.Errorf("дорожка аппарата пережила снятие: %+v", left)
	}
}
