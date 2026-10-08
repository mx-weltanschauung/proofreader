package repository

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

func TestIndexRepository_IncomingLinks(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	indexRepo := NewIndexRepository(pool)

	// Создаём издание и работу-том (ReplaceForEdition пришёл на смену
	// ReplaceForWork в задаче 3 — ввоз указателя теперь идёт по изданию).
	editionID, workID := seedEditionAndWork(t, pool)

	// Создаём два понятия: "Абстрактный труд (тест)" ссылается на "Труд (тест)"
	trud := &models.IndexConcept{
		Title:   "Труд (тест)",
		Slug:    "trud-test-incoming",
		SortKey: "труд (тест)",
	}
	trud.Articles = []*models.IndexArticle{{
		WorkID:     &workID,
		Kind:       models.IndexConceptKindArticle,
		Links:      []*models.IndexConceptLink{},
		References: []*models.IndexReference{},
	}}
	abstraktnyj := &models.IndexConcept{
		Title:   "Абстрактный труд (тест)",
		Slug:    "abstraktnyj-trud-test-incoming",
		SortKey: "абстрактный труд (тест)",
	}
	abstraktnyj.Articles = []*models.IndexArticle{{
		WorkID: &workID,
		Kind:   models.IndexConceptKindArticle,
		Links: []*models.IndexConceptLink{
			{
				TargetTitle: "Труд (тест)",
				Kind:        models.IndexLinkKindSeeAlso,
				OrderNumber: 1,
			},
		},
		References: []*models.IndexReference{},
	}}
	concepts := []*models.IndexConcept{trud, abstraktnyj}

	if _, err := indexRepo.ReplaceForEdition(ctx, editionID, concepts); err != nil {
		t.Fatalf("ReplaceForEdition: %v", err)
	}

	trudConcept, err := indexRepo.GetConceptBySlug(ctx, "trud-test-incoming")
	if err != nil {
		t.Fatalf("GetConceptBySlug(труд): %v", err)
	}
	abstraktnyjConcept, err := indexRepo.GetConceptBySlug(ctx, "abstraktnyj-trud-test-incoming")
	if err != nil {
		t.Fatalf("GetConceptBySlug(абстрактный труд): %v", err)
	}

	// Проверяем обратные отсылки для "Труда (тест)" (целевого понятия)
	// Должна вернуться ровно одна запись: "Абстрактный труд (тест)"
	incoming, err := indexRepo.IncomingLinks(ctx, trudConcept.ID)
	if err != nil {
		t.Fatalf("IncomingLinks для целевого: %v", err)
	}
	if len(incoming) != 1 {
		t.Fatalf("IncomingLinks для целевого: ожидалось 1, получено %d", len(incoming))
	}
	if incoming[0].Slug != "abstraktnyj-trud-test-incoming" {
		t.Errorf("slug = %q, ожидалось abstraktnyj-trud-test-incoming", incoming[0].Slug)
	}
	if incoming[0].Title != "Абстрактный труд (тест)" {
		t.Errorf("title = %q, ожидалось Абстрактный труд (тест)", incoming[0].Title)
	}
	if incoming[0].Kind != models.IndexLinkKindSeeAlso {
		t.Errorf("kind = %q, ожидалось %q", incoming[0].Kind, models.IndexLinkKindSeeAlso)
	}

	// Проверяем обратные отсылки для "Абстрактного труда (тест)" (исходящего понятия)
	// Не должно быть ничего
	incoming2, err := indexRepo.IncomingLinks(ctx, abstraktnyjConcept.ID)
	if err != nil {
		t.Fatalf("IncomingLinks для исходящего: %v", err)
	}
	if len(incoming2) != 0 {
		t.Fatalf("IncomingLinks для исходящего: ожидалось 0, получено %d", len(incoming2))
	}
}

func TestGetConceptBySlugReturnsArticlesWithRubrics(t *testing.T) {
	pool := testPool(t) // существующий помощник файла; без PROOFREADER_TEST_DB_URL пропускает
	repo := NewIndexRepository(pool)
	ctx := context.Background()

	editionID, workID := seedEditionAndWork(t, pool)
	conceptID := seedConcept(t, pool, "абстрактный труд", "abstraktnyj-trud")
	articleID := seedArticle(t, pool, conceptID, editionID, &workID, "Абстрактный труд")
	rubricID := seedRubric(t, pool, articleID, "определение", 1)
	seedReference(t, pool, articleID, &rubricID, 12, 730, 731, 1)
	seedReference(t, pool, articleID, nil, 13, 16, 18, 2)

	c, err := repo.GetConceptBySlug(ctx, "abstraktnyj-trud")
	if err != nil {
		t.Fatalf("GetConceptBySlug: %v", err)
	}
	if len(c.Articles) != 1 {
		t.Fatalf("статей %d, хотели 1", len(c.Articles))
	}
	a := c.Articles[0]
	if a.EditionID != editionID {
		t.Errorf("edition_id %d, хотели %d", a.EditionID, editionID)
	}
	if a.WorkID == nil || *a.WorkID != workID {
		t.Errorf("work_id %v, хотели %d", a.WorkID, workID)
	}
	if len(a.References) != 2 {
		t.Fatalf("адресов %d, хотели 2", len(a.References))
	}
	if a.References[0].Rubric != "определение" {
		t.Errorf("подрубрика первого адреса %q, хотели %q", a.References[0].Rubric, "определение")
	}
	// Адрес без подрубрики — норма: статья несёт собственные адреса до первой
	// подрубрики, и пустая строка тут значит именно это.
	if a.References[1].Rubric != "" {
		t.Errorf("подрубрика второго адреса %q, хотели пустую", a.References[1].Rubric)
	}
}

// TestGetConceptBySlugReturnsNestedRubricPath — задача 8: адрес под вложенной
// подрубрикой (съезд → аспект) обязан отдавать RubricPath длиной 2 в порядке
// «от корня к листу», а Rubric — по-прежнему листом (старые потребители не
// ломаются). Данные заводятся через ReplaceForEdition (тот же путь, что и
// боевой ввоз, задача 7), а не прямой SQL-вставкой в index_rubrics — иначе
// тест проверял бы собственную выдумку о раскладке таблицы, а не то, что
// реально пишет ввоз.
func TestGetConceptBySlugReturnsNestedRubricPath(t *testing.T) {
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	ctx := context.Background()
	editionID, workID := seedEditionAndWork(t, pool)

	const (
		congress = "I съезд РСДРП. 1-3 (13-15) марта 1898 г. Минск"
		aspect   = "значение съезда"
	)
	concepts := []*models.IndexConcept{{
		Title: "КПСС — съезды", Slug: "kpss-sezdy-nested-path", SortKey: "кпсс — съезды",
	}}
	concepts[0].Articles = []*models.IndexArticle{{
		WorkID: &workID,
		Kind:   "article",
		References: []*models.IndexReference{
			{RubricPath: []string{congress, aspect},
				VolumeNumber: 4, PageStart: 174, PageEnd: 174, OrderNumber: 1},
		},
	}}
	if _, err := repo.ReplaceForEdition(ctx, editionID, concepts); err != nil {
		t.Fatalf("ReplaceForEdition: %v", err)
	}

	c, err := repo.GetConceptBySlug(ctx, "kpss-sezdy-nested-path")
	if err != nil {
		t.Fatalf("GetConceptBySlug: %v", err)
	}
	if len(c.Articles) != 1 || len(c.Articles[0].References) != 1 {
		t.Fatalf("неожиданная форма выдачи: %+v", c)
	}
	ref := c.Articles[0].References[0]
	if ref.Rubric != aspect {
		t.Errorf("Rubric (лист) = %q, хотели %q", ref.Rubric, aspect)
	}
	if len(ref.RubricPath) != 2 {
		t.Fatalf("RubricPath длиной %d (%v), хотели 2", len(ref.RubricPath), ref.RubricPath)
	}
	if ref.RubricPath[0] != congress || ref.RubricPath[1] != aspect {
		t.Errorf("RubricPath = %v, хотели [%q, %q] — от корня к листу", ref.RubricPath, congress, aspect)
	}
}

// TestBacklinksFindConceptOfArticleWithoutIndexWork проверяет задачу 6:
// обратные ссылки полосы обязаны брать издание у СТАТЬИ (index_concept_articles),
// а не у работы-указателя (works через index_concepts.work_id) — у статьи,
// ввезённой из внешнего источника без собственного тома в корпусе, work_id
// NULL, и прежний запрос (JOIN works iw ON iw.id = c.work_id) такую статью
// не находил вовсе.
//
// Задача 8, круг правок 1: подрубрика адреса здесь ВЛОЖЕННАЯ (родитель →
// "общая характеристика"), а не плоская — рецензия нашла, что проводка
// ConceptBacklink.RubricPath (JOIN rubric_ancestry в backlinksQuery, задача
// 8) не покрыта ничем: заглушка на месте скана `&b.RubricPath` оставляла
// весь пакет зелёным. Запрос здесь — backlinksQuery, ДРУГОЙ запрос, чем у
// TestGetConceptBySlugReturnsNestedRubricPath (articleReferencesQuery), и
// ломаются они независимо, поэтому тест не дублирует тот.
func TestBacklinksFindConceptOfArticleWithoutIndexWork(t *testing.T) {
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	ctx := context.Background()
	editionID, _ := seedEditionAndWork(t, pool)

	const (
		parentRubric = "раздел (тест)"
		leafRubric   = "общая характеристика"
	)

	conceptID := seedConcept(t, pool, "абсолютизм", "absolyutizm")
	// work_id у статьи NULL: указатель взят с внешнего источника, работы в
	// корпусе нет — и обратная ссылка обязана работать всё равно.
	articleID := seedArticle(t, pool, conceptID, editionID, nil, "Абсолютизм")
	parentID := seedRubric(t, pool, articleID, parentRubric, 1)
	rubricID := seedNestedRubric(t, pool, articleID, parentID, leafRubric, 1)
	seedReference(t, pool, articleID, &rubricID, 2, 108, 110, 1)

	got, err := repo.Backlinks(ctx, editionID, 2, nil, 109)
	if err != nil {
		t.Fatalf("Backlinks: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("обратных ссылок %d, хотели 1", len(got))
	}
	if got[0].Slug != "absolyutizm" || got[0].Rubric != leafRubric {
		t.Errorf("не та ссылка: %+v", got[0])
	}
	if len(got[0].RubricPath) != 2 {
		t.Fatalf("RubricPath длиной %d (%v), хотели 2", len(got[0].RubricPath), got[0].RubricPath)
	}
	if got[0].RubricPath[0] != parentRubric || got[0].RubricPath[1] != leafRubric {
		t.Errorf("RubricPath = %v, хотели [%q, %q] — от корня к листу", got[0].RubricPath, parentRubric, leafRubric)
	}
}

// TestBacklinksIncludeAddressWithoutRubric проверяет вторую половину задачи
// 6: адрес без подрубрики (статья несёт собственные адреса до первой
// подрубрики) законен и обязан попасть в обратные ссылки с пустой строкой в
// Rubric, а не выпасть из результата — это ловит замену LEFT JOIN на JOIN у
// index_rubrics, которую первый тест (у его адреса подрубрика есть) не ловит.
func TestBacklinksIncludeAddressWithoutRubric(t *testing.T) {
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	ctx := context.Background()
	editionID, workID := seedEditionAndWork(t, pool)

	conceptID := seedConcept(t, pool, "материализм", "materializm-backlinks")
	articleID := seedArticle(t, pool, conceptID, editionID, &workID, "Материализм")
	seedReference(t, pool, articleID, nil, 3, 40, 42, 1)

	got, err := repo.Backlinks(ctx, editionID, 3, nil, 41)
	if err != nil {
		t.Fatalf("Backlinks: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("обратных ссылок %d, хотели 1", len(got))
	}
	if got[0].Slug != "materializm-backlinks" || got[0].Rubric != "" {
		t.Errorf("не та ссылка: %+v", got[0])
	}
}

// setArticleDetails доопределяет поля статьи, которые seedArticle оставляет
// значениями по умолчанию (kind='article', markdown='', полосы 0). Нужен
// выводу kind в ListConcepts, где важно различие kind разных статей одного
// понятия (TestListConceptsKindReflectsAnyArticle).
//
// До миграции 000026 этот помощник обслуживал ещё и
// TestGetConceptBySlugBackfillsLegacyFieldsFromFirstArticle — тест на
// обратную совместимость, проверявший, что GetConceptBySlug копирует
// Kind/ArticleMarkdown/SourcePage* первой статьи на само понятие. 000026
// сняла эти поля с IndexConcept вовсе (они принадлежат статье — у понятия с
// несколькими статьями единого значения этих полей просто нет), так что
// копировать стало некуда и незачем; тест снесён вместе с полями, которые он
// проверял.
func setArticleDetails(t *testing.T, pool *pgxpool.Pool, articleID int64, kind, markdown string, pageStart, pageEnd int) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		UPDATE index_concept_articles
		SET kind = $2, article_markdown = $3, source_page_start = $4, source_page_end = $5
		WHERE id = $1
	`, articleID, kind, markdown, pageStart, pageEnd)
	if err != nil {
		t.Fatalf("setArticleDetails: %v", err)
	}
}

// TestListConceptsKindReflectsAnyArticle покрывает вывод kind в
// listConceptsQuery против настоящей базы: понятие считается статьёй, если
// хоть один указатель дал о нём статью, — перенаправление в одном собрании
// не отменяет разбора в другом. Третий случай (смешанное понятие) — тот
// самый, ради которого EXISTS и написан.
func TestListConceptsKindReflectsAnyArticle(t *testing.T) {
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	ctx := context.Background()

	edition1ID, work1ID := seedEditionAndWork(t, pool)

	// Случай 1: единственная статья — redirect.
	redirectOnly := seedConcept(t, pool, "чистый редирект (тест)", "chistyj-redirekt-list-kind")
	redirectArticle := seedArticle(t, pool, redirectOnly, edition1ID, &work1ID, "чистый редирект (тест)")
	setArticleDetails(t, pool, redirectArticle, models.IndexConceptKindRedirect, "", 0, 0)

	// Случай 2: единственная статья — article.
	articleOnly := seedConcept(t, pool, "чистая статья (тест)", "chistaya-statya-list-kind")
	onlyArticle := seedArticle(t, pool, articleOnly, edition1ID, &work1ID, "чистая статья (тест)")
	setArticleDetails(t, pool, onlyArticle, models.IndexConceptKindArticle, "текст", 1, 2)

	// Случай 3: одна статья redirect, другая article — по разным собраниям
	// ((concept_id, edition_id) уникален, второй статье того же понятия в
	// том же собрании быть не может).
	edition2ID, work2ID := seedEditionAndWork(t, pool)
	mixed := seedConcept(t, pool, "смешанное понятие (тест)", "smeshannoe-ponyatie-list-kind")
	mixedRedirect := seedArticle(t, pool, mixed, edition1ID, &work1ID, "смешанное понятие (тест)")
	setArticleDetails(t, pool, mixedRedirect, models.IndexConceptKindRedirect, "", 0, 0)
	mixedArticle := seedArticle(t, pool, mixed, edition2ID, &work2ID, "смешанное понятие (тест)")
	setArticleDetails(t, pool, mixedArticle, models.IndexConceptKindArticle, "текст", 3, 4)

	concepts, err := repo.ListConcepts(ctx, "", "", 100, 0)
	if err != nil {
		t.Fatalf("ListConcepts: %v", err)
	}
	byID := make(map[int64]*models.IndexConcept, len(concepts))
	for _, c := range concepts {
		byID[c.ID] = c
	}

	if got, ok := byID[redirectOnly]; !ok || got.Kind != models.IndexConceptKindRedirect {
		t.Errorf("единственная статья redirect: kind = %+v, хотели %q", got, models.IndexConceptKindRedirect)
	}
	if got, ok := byID[articleOnly]; !ok || got.Kind != models.IndexConceptKindArticle {
		t.Errorf("единственная статья article: kind = %+v, хотели %q", got, models.IndexConceptKindArticle)
	}
	if got, ok := byID[mixed]; !ok || got.Kind != models.IndexConceptKindArticle {
		t.Errorf("смешанное понятие (redirect + article): kind = %+v, хотели %q (article побеждает)", got, models.IndexConceptKindArticle)
	}
}

// TestListConceptsReportsEditionIDs покрывает агрегат edition_ids в
// listConceptsQuery: питоновский публикатор (apply_index.count_existing,
// publish_volume.index_payload) отбирает понятия по этому полю, а не по
// work_id, — без него защиту от усыхания указателя не на чем считать при
// ввозе по изданию. Понятие с двумя статьями в разных изданиях обязано
// вернуть оба id, понятие с одной статьёй — ровно один.
func TestListConceptsReportsEditionIDs(t *testing.T) {
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	ctx := context.Background()

	edition1ID, work1ID := seedEditionAndWork(t, pool)
	edition2ID, work2ID := seedEditionAndWork(t, pool)

	single := seedConcept(t, pool, "одно издание (тест)", "odno-izdanie-edition-ids")
	seedArticle(t, pool, single, edition1ID, &work1ID, "одно издание (тест)")

	both := seedConcept(t, pool, "два издания (тест)", "dva-izdaniya-edition-ids")
	seedArticle(t, pool, both, edition1ID, &work1ID, "два издания (тест)")
	seedArticle(t, pool, both, edition2ID, &work2ID, "два издания (тест)")

	concepts, err := repo.ListConcepts(ctx, "", "", 100, 0)
	if err != nil {
		t.Fatalf("ListConcepts: %v", err)
	}
	byID := make(map[int64]*models.IndexConcept, len(concepts))
	for _, c := range concepts {
		byID[c.ID] = c
	}

	got, ok := byID[single]
	if !ok || len(got.EditionIDs) != 1 || got.EditionIDs[0] != edition1ID {
		t.Errorf("одно издание: edition_ids = %+v, хотели [%d]", got, edition1ID)
	}
	got, ok = byID[both]
	if !ok || len(got.EditionIDs) != 2 {
		t.Fatalf("два издания: edition_ids = %+v, хотели два элемента", got)
	}
	seen := map[int64]bool{got.EditionIDs[0]: true, got.EditionIDs[1]: true}
	if !seen[edition1ID] || !seen[edition2ID] {
		t.Errorf("два издания: edition_ids = %v, хотели [%d %d]", got.EditionIDs, edition1ID, edition2ID)
	}
}

// TestReplaceForEditionDerivesRubricsFromReferences проверяет вывод подрубрик
// из строк адресов при ввозе по изданию: тело запроса не несёт отдельного
// массива подрубрик, сервер выводит их сам, дедуплицируя по нормализованному
// ключу, а не по сырому тексту.
func TestReplaceForEditionDerivesRubricsFromReferences(t *testing.T) {
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	ctx := context.Background()
	editionID, workID := seedEditionAndWork(t, pool)

	concepts := []*models.IndexConcept{{
		Title: "Абстрактный труд", Slug: "abstraktnyj-trud", SortKey: "абстрактный труд",
	}}
	concepts[0].Articles = []*models.IndexArticle{{
		WorkID: &workID,
		Kind:   "article",
		References: []*models.IndexReference{
			{VolumeNumber: 12, PageStart: 730, PageEnd: 731, Rubric: "определение", OrderNumber: 1},
			{VolumeNumber: 13, PageStart: 16, PageEnd: 18, Rubric: "его мера", OrderNumber: 2},
			{VolumeNumber: 13, PageStart: 43, PageEnd: 43, Rubric: "определение", OrderNumber: 3},
			{VolumeNumber: 13, PageStart: 99, PageEnd: 99, Rubric: "", OrderNumber: 4},
		},
	}}

	if _, err := repo.ReplaceForEdition(ctx, editionID, concepts); err != nil {
		t.Fatalf("ReplaceForEdition: %v", err)
	}

	c, err := repo.GetConceptBySlug(ctx, "abstraktnyj-trud")
	if err != nil {
		t.Fatalf("GetConceptBySlug: %v", err)
	}
	if len(c.Articles) != 1 {
		t.Fatalf("статей %d, хотели 1", len(c.Articles))
	}
	refs := c.Articles[0].References
	if len(refs) != 4 {
		t.Fatalf("адресов %d, хотели 4", len(refs))
	}
	// Две подрубрики на четыре адреса: «определение» встретилось дважды и
	// обязано быть ОДНОЙ строкой таблицы, иначе у Ленина 199 тысяч адресов
	// дадут 199 тысяч подрубрик.
	var rubrics int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM index_rubrics ru
		 JOIN index_concept_articles a ON a.id = ru.article_id
		 WHERE a.edition_id = $1`, editionID).Scan(&rubrics); err != nil {
		t.Fatalf("count rubrics: %v", err)
	}
	if rubrics != 2 {
		t.Errorf("подрубрик %d, хотели 2", rubrics)
	}
	if refs[3].RubricID != nil {
		t.Errorf("адрес без подрубрики получил rubric_id %v", refs[3].RubricID)
	}
	// Порядок подрубрик — печатный: «определение» встретилось первым.
	var first string
	if err := pool.QueryRow(ctx,
		`SELECT ru.title FROM index_rubrics ru
		 JOIN index_concept_articles a ON a.id = ru.article_id
		 WHERE a.edition_id = $1 ORDER BY ru.order_number LIMIT 1`, editionID).Scan(&first); err != nil {
		t.Fatalf("first rubric: %v", err)
	}
	if first != "определение" {
		t.Errorf("первая подрубрика %q, хотели %q", first, "определение")
	}
}

// TestReplaceForEditionKeepsCuratorConceptOnReimport проверяет ключ
// переимпорта — (edition_id, title_key) СТАТЬИ, а не title_key самого
// понятия. Прежняя версия этого теста сравнивала ID понятия между двумя
// ввозами одного и того же заголовка — и прошла бы даже под наивной
// реализацией «снести все статьи и пересоздать через resolveConcept(titleKey
// заголовка)», потому что resolveConcept нашёл бы то же самое понятие по
// его собственному title_key заново. Мутация 2 прошлого раунда доказала это
// вживую: снос-и-пересоздание там падало на UNIQUE, а не на ассерте — схема
// убивала мутацию раньше, чем тест успевал её заметить.
//
// Здесь куратор вручную переносит статью на СОВЕРШЕННО ДРУГОЕ понятие (другой
// заголовок, другой title_key, заведено отдельно). Реимпорт того же заголовка
// «Абстрактный труд» обязан найти статью по (edition_id, title_key) СТАТЬИ и
// сохранить её concept_id как есть — понятие с заголовком «Абстрактный труд»
// заново возникнуть не должно, а решение куратора обязано пережить прогон.
func TestReplaceForEditionKeepsCuratorConceptOnReimport(t *testing.T) {
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	ctx := context.Background()
	editionID, workID := seedEditionAndWork(t, pool)

	// Замок против регрессии до голого title_key (мутация раунда правок):
	// статья ДРУГОГО издания с тем же title_key "абстрактный труд", заведена
	// раньше настоящей статьи этого теста — значит её id заведомо меньше.
	// Пока поиск на шаге 2 идёт по (edition_id, title_key), эта статья ни
	// разу не участвует в сценарии ниже: она принадлежит чужому изданию.
	// Если бы проверка деградировала до одного title_key, PostgreSQL без
	// ORDER BY на маленькой таблице возвращает совпадения в физическом
	// порядке — то есть эту, чужую, строку первой, — и правка второго ввоза
	// ушла бы в чужое издание вместо настоящей статьи.
	decoyEditionID, decoyWorkID := seedEditionAndWork(t, pool)
	decoyConceptID := seedConcept(t, pool, "Абстрактный труд (чужое издание)", "abstraktnyj-trud-decoy")
	seedArticle(t, pool, decoyConceptID, decoyEditionID, &decoyWorkID, "Абстрактный труд")

	mk := func(markdown string) []*models.IndexConcept {
		c := &models.IndexConcept{
			Title:   "Абстрактный труд",
			Slug:    "abstraktnyj-trud",
			SortKey: "абстрактный труд",
		}
		c.Articles = []*models.IndexArticle{{WorkID: &workID, Kind: "article", ArticleMarkdown: markdown}}
		return []*models.IndexConcept{c}
	}

	if _, err := repo.ReplaceForEdition(ctx, editionID, mk("первый")); err != nil {
		t.Fatalf("первый ввоз: %v", err)
	}

	// Куратор сводит статью с ДРУГИМ, заранее существующим понятием — с иным
	// заголовком (например, обнаружил, что это дубль статьи из другого
	// указателя того же собрания). Меняем только concept_id статьи, ничего
	// в самой статье.
	otherConceptID := seedConcept(t, pool, "Труд (сведено куратором)", "trud-svedeno-kuratorom")
	if _, err := pool.Exec(ctx, `
		UPDATE index_concept_articles SET concept_id = $1
		WHERE edition_id = $2 AND title_key = $3
	`, otherConceptID, editionID, "абстрактный труд"); err != nil {
		t.Fatalf("переназначение concept_id куратором: %v", err)
	}

	if _, err := repo.ReplaceForEdition(ctx, editionID, mk("второй")); err != nil {
		t.Fatalf("второй ввоз: %v", err)
	}

	// Решение куратора обязано устоять: статья по-прежнему на otherConceptID,
	// а не на новом/старом понятии "Абстрактный труд".
	after, err := repo.GetConceptBySlug(ctx, "trud-svedeno-kuratorom")
	if err != nil {
		t.Fatalf("чтение сведённого понятия после второго ввоза: %v", err)
	}
	if len(after.Articles) != 1 {
		t.Fatalf("статей у сведённого понятия %d, хотели 1", len(after.Articles))
	}
	if after.Articles[0].ArticleMarkdown != "второй" {
		t.Errorf("текст статьи не обновился: %q", after.Articles[0].ArticleMarkdown)
	}

	// Понятие «Абстрактный труд» не должно было завестись заново — если бы
	// сопоставление шло по голому title_key ПОНЯТИЯ (наивная пересборка),
	// второй ввоз нашёл бы или создал понятие с этим заголовком заново,
	// вместо того чтобы вернуться к записи существующей статьи.
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM index_concepts WHERE title_key = $1`, "абстрактный труд").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("понятий с заголовком «Абстрактный труд» стало %d, хотели 0 — решение куратора должно было устоять", count)
	}
}

// TestReplaceForEditionRejectsDuplicateTitleInBatch проверяет пункт 3
// раунда правок: два понятия с одним ключом заголовка В ОДНОМ ввозе —
// дефект разбора (например, склейка переносов дала два варианта одного и
// того же заголовка), а не легитимные данные. Раньше такая пара тихо
// схлопывалась бы в одну статью — вторая по счёту побеждала бы просто как
// последняя запись цикла, и куда делась первая, было бы не видно нигде.
func TestReplaceForEditionRejectsDuplicateTitleInBatch(t *testing.T) {
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	ctx := context.Background()
	editionID, workID := seedEditionAndWork(t, pool)

	// Разное написание — двойной пробел против одинарного — нормализуется в
	// один и тот же title_key, а исходные заголовки остаются разными
	// строками, что и нужно для проверки, что оба названы в ошибке.
	const titleA = "Прибавочная  стоимость"
	const titleB = "прибавочная стоимость"
	concepts := []*models.IndexConcept{
		{Title: titleA, Slug: "pribavochnaya-a", SortKey: titleA, Kind: models.IndexConceptKindArticle},
		{Title: titleB, Slug: "pribavochnaya-b", SortKey: titleB, Kind: models.IndexConceptKindArticle},
	}
	for _, c := range concepts {
		c.Articles = []*models.IndexArticle{{WorkID: &workID}}
	}

	_, err := repo.ReplaceForEdition(ctx, editionID, concepts)
	if err == nil {
		t.Fatal("ожидали ошибку на дубле заголовка в одном ввозе, получили nil")
	}
	if !strings.Contains(err.Error(), titleA) || !strings.Contains(err.Error(), titleB) {
		t.Errorf("ошибка не называет оба заголовка: %v", err)
	}

	// Отказ случается ДО транзакции (сбор ключей идёт раньше tx.Begin) — ни
	// одна статья не должна была записаться.
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM index_concept_articles WHERE edition_id = $1`, editionID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("статей записано %d, хотели 0 — дубль обязан отклонить ввоз целиком", count)
	}
}

// TestReplaceForEditionWarnsOnAmbiguousConceptMatch проверяет пункт 1 раунда
// правок: title_key совпал более чем с одним существующим понятием каталога
// (например, два независимо заведённых понятия с одинаковым заголовком,
// ещё не сведённых куратором). resolveConcept не вправе выбирать за куратора,
// к какому из них прицепить новую статью — обязано завести НОВОЕ понятие и
// сообщить об этом наверх, а не молча слить с первым попавшимся.
func TestReplaceForEditionWarnsOnAmbiguousConceptMatch(t *testing.T) {
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	ctx := context.Background()
	editionID, workID := seedEditionAndWork(t, pool)

	const title = "Прибавочная стоимость"
	titleKey := models.NormalizeIndexTitle(title)

	// Оба прежних понятия обязаны нести СВОЮ статью (в чужих изданиях — не в
	// editionID этого теста, иначе поиск на шаге 2 нашёл бы их напрямую по
	// (edition_id, title_key), и до resolveConcept дело не дошло бы), а не
	// просто существовать голой строкой в index_concepts: понятие без единой
	// статьи — сирота, и шаг 6 того же прогона снёс бы его сам, до всякой
	// проверки на неоднозначность, — тест доказывал бы не то.
	edA, workA := seedEditionAndWork(t, pool)
	edB, workB := seedEditionAndWork(t, pool)
	conceptAID := seedConcept(t, pool, title, "pribavochnaya-stoimost-existing-a")
	conceptBID := seedConcept(t, pool, title, "pribavochnaya-stoimost-existing-b")
	seedArticle(t, pool, conceptAID, edA, &workA, title)
	seedArticle(t, pool, conceptBID, edB, &workB, title)

	concepts := []*models.IndexConcept{{
		Title: title, Slug: "pribavochnaya-stoimost-new", SortKey: title,
		Kind: models.IndexConceptKindArticle,
	}}
	concepts[0].Articles = []*models.IndexArticle{{WorkID: &workID}}

	warnings, err := repo.ReplaceForEdition(ctx, editionID, concepts)
	if err != nil {
		t.Fatalf("ReplaceForEdition: %v", err)
	}

	if len(warnings) != 1 {
		t.Fatalf("предупреждений %d, хотели 1: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], title) {
		t.Errorf("предупреждение не называет заголовок %q: %q", title, warnings[0])
	}

	// Третье, новое понятие — ни к одному из двух прежних статья не
	// прилипла молча.
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM index_concepts WHERE title_key = $1`, titleKey).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Errorf("понятий с этим заголовком стало %d, хотели 3 (два прежних + новое)", count)
	}
}

// TestReplaceForEditionDestroysDroppedArticleAndOrphanedConcept проверяет
// пункт 5 раунда правок — самую рискованную и до этого не покрытую половину
// ветки: удаление статьи, выпавшей из нового ввоза, обязано унести каскадом
// её адреса, подрубрики и исходящие отсылки, а понятие, оставшееся без
// единой статьи, — исчезнуть само. При этом отсылка ТРЕТЬЕЙ СТОРОНЫ — статьи
// ДРУГОГО издания, не участвующего в этом переимпорте вовсе, — на удалённое
// понятие обязана уцелеть строкой (ON DELETE SET NULL, а не CASCADE) с
// обнулённым to_concept_id, а не пропасть вовсе.
//
// Третья сторона обязана жить в ДРУГОМ издании: статья того же издания,
// участвующая в реимпорте, при повторном ввозе сама проходит через ветку
// «обновление найденной статьи», которая сносит и заново заводит её
// собственные исходящие отсылки (см. шаг 2) — новая строка с тем же
// содержимым замаскировала бы отсутствие настоящего каскада ON DELETE SET
// NULL слепком того же результата. Только отсылка издания, которое эта
// транзакция не трогает вовсе, доказывает, что нулевое значение поставил
// внешний ключ, а не наш код.
func TestReplaceForEditionDestroysDroppedArticleAndOrphanedConcept(t *testing.T) {
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	ctx := context.Background()
	editionID, workID := seedEditionAndWork(t, pool)
	thirdPartyEditionID, thirdPartyWorkID := seedEditionAndWork(t, pool)

	const droppedTitle = "Экспроприация экспроприаторов"
	const keptTitle = "Прибавочный продукт"

	dropped := &models.IndexConcept{
		Title: droppedTitle, Slug: "ekspropriatsiya", SortKey: droppedTitle,
	}
	dropped.Articles = []*models.IndexArticle{{
		WorkID: &workID,
		Kind:   models.IndexConceptKindArticle,
		References: []*models.IndexReference{
			{VolumeNumber: 4, PageStart: 10, PageEnd: 11, Rubric: "определение", OrderNumber: 1},
		},
	}}

	if _, err := repo.ReplaceForEdition(ctx, editionID, []*models.IndexConcept{dropped}); err != nil {
		t.Fatalf("первый ввоз (снимаемое понятие): %v", err)
	}

	kept := &models.IndexConcept{
		Title: keptTitle, Slug: "pribavochnyj-produkt", SortKey: keptTitle,
	}
	kept.Articles = []*models.IndexArticle{{
		WorkID: &thirdPartyWorkID,
		Kind:   models.IndexConceptKindArticle,
		Links: []*models.IndexConceptLink{
			{TargetTitle: droppedTitle, Kind: models.IndexLinkKindSeeAlso, OrderNumber: 1},
		},
	}}

	if _, err := repo.ReplaceForEdition(ctx, thirdPartyEditionID, []*models.IndexConcept{kept}); err != nil {
		t.Fatalf("ввоз третьей стороны: %v", err)
	}

	before, err := repo.GetConceptBySlug(ctx, "ekspropriatsiya")
	if err != nil {
		t.Fatalf("чтение удаляемого понятия после первого ввоза: %v", err)
	}
	if len(before.Articles) != 1 || len(before.Articles[0].References) != 1 {
		t.Fatalf("неожиданная форма удаляемого понятия перед сносом: %+v", before)
	}
	droppedConceptID := before.ID
	droppedArticleID := before.Articles[0].ID
	droppedReferenceID := before.Articles[0].References[0].ID
	var droppedRubricCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM index_rubrics WHERE article_id = $1`, droppedArticleID).Scan(&droppedRubricCount); err != nil {
		t.Fatalf("count rubrics before: %v", err)
	}
	if droppedRubricCount != 1 {
		t.Fatalf("подрубрик у удаляемой статьи %d, хотели 1", droppedRubricCount)
	}

	// Отсылка третьей стороны обязана была разрешиться (шаг 7 её собственного
	// ввоза, который проходит по всей таблице целиком) — иначе тест ничего
	// не докажет: to_concept_id должен указывать именно на droppedConceptID.
	var linkToConceptID *int64
	var linkID int64
	if err := pool.QueryRow(ctx, `
		SELECT l.id, l.to_concept_id FROM index_concept_links l
		JOIN index_concept_articles a ON a.id = l.from_article_id
		WHERE a.edition_id = $1 AND a.title_key = $2
	`, thirdPartyEditionID, models.NormalizeIndexTitle(keptTitle)).Scan(&linkID, &linkToConceptID); err != nil {
		t.Fatalf("чтение отсылки перед сносом: %v", err)
	}
	if linkToConceptID == nil || *linkToConceptID != droppedConceptID {
		t.Fatalf("отсылка не разрешилась на снимаемое понятие перед сносом: to_concept_id=%v, хотели %d", linkToConceptID, droppedConceptID)
	}

	// Второй ввоз editionID пуст — «Экспроприация экспроприаторов» выпала из
	// указателя (например, разбор счёл её опечаткой прежнего прогона).
	// thirdPartyEditionID эта транзакция не трогает вовсе.
	if _, err := repo.ReplaceForEdition(ctx, editionID, []*models.IndexConcept{}); err != nil {
		t.Fatalf("второй ввоз: %v", err)
	}

	// Статья снесена.
	var articleCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM index_concept_articles WHERE id = $1`, droppedArticleID).Scan(&articleCount); err != nil {
		t.Fatalf("count article: %v", err)
	}
	if articleCount != 0 {
		t.Errorf("статья %d пережила снос", droppedArticleID)
	}

	// Адрес и подрубрика снесены каскадом вместе со статьёй.
	var referenceCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM index_references WHERE id = $1`, droppedReferenceID).Scan(&referenceCount); err != nil {
		t.Fatalf("count reference: %v", err)
	}
	if referenceCount != 0 {
		t.Errorf("адрес %d пережил снос статьи", droppedReferenceID)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM index_rubrics WHERE article_id = $1`, droppedArticleID).Scan(&droppedRubricCount); err != nil {
		t.Fatalf("count rubrics after: %v", err)
	}
	if droppedRubricCount != 0 {
		t.Errorf("подрубрика статьи %d пережила снос", droppedArticleID)
	}

	// Понятие, оставшееся без единой статьи, снесено само (шаг 6).
	var conceptCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM index_concepts WHERE id = $1`, droppedConceptID).Scan(&conceptCount); err != nil {
		t.Fatalf("count concept: %v", err)
	}
	if conceptCount != 0 {
		t.Errorf("понятие %d осталось сиротой после снятия последней статьи", droppedConceptID)
	}

	// Отсылка третьей стороны (kept) уцелела строкой — ON DELETE SET NULL,
	// а не CASCADE — но её to_concept_id обнулился вместе со снесённым
	// понятием.
	var linkStillThere int
	var afterToConcept *int64
	if err := pool.QueryRow(ctx, `SELECT count(*), (SELECT to_concept_id FROM index_concept_links WHERE id = $1) FROM index_concept_links WHERE id = $1`, linkID).Scan(&linkStillThere, &afterToConcept); err != nil {
		t.Fatalf("чтение отсылки после сноса: %v", err)
	}
	if linkStillThere != 1 {
		t.Fatalf("отсылка %d пропала вовсе, хотя ожидалось ON DELETE SET NULL, а не CASCADE", linkID)
	}
	if afterToConcept != nil {
		t.Errorf("to_concept_id отсылки %d не обнулился: %v", linkID, *afterToConcept)
	}
}

// TestReplaceForEditionPreservesWorkIDOnReimportWithoutIt закрывает главную
// находку раунда правок 1: work_id в index_concept_articles — единственный
// якорь, по которому scripts/takedown.sh находит статьи снимаемого тома
// (apparatus_repository.go). Ни tools/ocr_ingest/parse_index.py, ни
// publish_volume.py его в теле ввоза не шлют вовсе (у ленинского и
// плехановского указателей статьи носит целиком издание, а не работа-том) —
// то есть КАЖДЫЙ настоящий повторный ввоз приходит с work_id = nil. Голое
// присваивание в ветке обновления статьи стирало бы прежнее значение до
// NULL на каждом таком ввозе, и план снятия аппарата тома молча превращался
// бы в «понятий: 0» после первого же переимпорта — без единой ошибки,
// указывающей на причину. Сид через прямой SQL этого не поймает: он
// проставляет work_id напрямую, минуя саму ReplaceForEdition, — здесь
// сценарий гоняется через настоящий код ввоза дважды подряд, вторым разом
// без work_id в теле, ровно как это делают живые клиенты.
func TestReplaceForEditionPreservesWorkIDOnReimportWithoutIt(t *testing.T) {
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	ctx := context.Background()
	editionID, workID := seedEditionAndWork(t, pool)

	mk := func(withWorkID bool) []*models.IndexConcept {
		c := &models.IndexConcept{
			Title: "Прибавочная стоимость", Slug: "pribavochnaya-stoimost", SortKey: "прибавочная стоимость",
		}
		a := &models.IndexArticle{Kind: models.IndexConceptKindArticle}
		if withWorkID {
			a.WorkID = &workID
		}
		c.Articles = []*models.IndexArticle{a}
		return []*models.IndexConcept{c}
	}

	// Первый ввоз — как одноразовая миграция 000025 когда-то застала данные:
	// work_id пришёл и лёг в базу.
	if _, err := repo.ReplaceForEdition(ctx, editionID, mk(true)); err != nil {
		t.Fatalf("первый ввоз (с work_id): %v", err)
	}

	before, err := repo.GetConceptBySlug(ctx, "pribavochnaya-stoimost")
	if err != nil {
		t.Fatalf("чтение после первого ввоза: %v", err)
	}
	if len(before.Articles) != 1 || before.Articles[0].WorkID == nil || *before.Articles[0].WorkID != workID {
		t.Fatalf("work_id после первого ввоза = %+v, хотели %d", before.Articles, workID)
	}

	// Второй ввоз — настоящий повторный прогон publish_volume.py/parse_index.py:
	// то же издание, тот же заголовок статьи (значит UPDATE-ветка, не INSERT),
	// но work_id в теле НЕТ — как и во всех реальных клиентах.
	if _, err := repo.ReplaceForEdition(ctx, editionID, mk(false)); err != nil {
		t.Fatalf("второй ввоз (без work_id): %v", err)
	}

	after, err := repo.GetConceptBySlug(ctx, "pribavochnaya-stoimost")
	if err != nil {
		t.Fatalf("чтение после второго ввоза: %v", err)
	}
	if len(after.Articles) != 1 {
		t.Fatalf("статей после второго ввоза %d, хотели 1", len(after.Articles))
	}
	if after.Articles[0].ID != before.Articles[0].ID {
		t.Fatalf("второй ввоз пересоздал статью (id %d -> %d) вместо UPDATE — сценарий не проверяет находку 2", before.Articles[0].ID, after.Articles[0].ID)
	}
	if after.Articles[0].WorkID == nil {
		t.Fatalf("work_id обнулился на повторном ввозе без него в теле — якорь снятия аппарата тома потерян")
	}
	if *after.Articles[0].WorkID != workID {
		t.Errorf("work_id после второго ввоза = %d, хотели прежний %d", *after.Articles[0].WorkID, workID)
	}
}

// seedEditionAndWorkSeq различает слаги собраний между вызовами: editions.slug
// уникален, а TestListConceptsKindReflectsAnyArticle заводит два собрания в
// одном тесте (смешанное понятие требует разных изданий — см. UNIQUE
// (concept_id, edition_id) у index_concept_articles).
var seedEditionAndWorkSeq int64

// seedEditionAndWork заводит собрание и один том в нём — минимум, нужный
// статье указателя (edition_id обязателен, work_id — нет, но тесты этой
// задачи проверяют случай, когда работа-указатель в корпусе есть).
func seedEditionAndWork(t *testing.T, pool *pgxpool.Pool) (editionID, workID int64) {
	t.Helper()
	ctx := context.Background()
	seq := atomic.AddInt64(&seedEditionAndWorkSeq, 1)

	editions := NewEditionRepository(pool)
	edition := &models.Edition{Title: "проба-статьи", Slug: fmt.Sprintf("proba-articles-%d", seq)}
	if err := editions.Create(ctx, edition); err != nil {
		t.Fatalf("seedEditionAndWork: create edition: %v", err)
	}
	t.Cleanup(func() { _ = editions.Delete(ctx, edition.ID) })

	workRepo := NewWorkRepository(pool)
	work := newVolume(t, workRepo, "проба-статьи-том")
	work.EditionID = &edition.ID
	if err := workRepo.Update(ctx, work); err != nil {
		t.Fatalf("seedEditionAndWork: attach edition: %v", err)
	}

	return edition.ID, work.ID
}

// seedConcept заводит понятие каталога без собственной статьи (work_id
// теперь nullable — см. миграцию 000025).
func seedConcept(t *testing.T, pool *pgxpool.Pool, title, slug string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO index_concepts (title, slug, sort_key, title_key)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, title, slug, title, models.NormalizeIndexTitle(title)).Scan(&id)
	if err != nil {
		t.Fatalf("seedConcept: %v", err)
	}
	return id
}

// seedArticle заводит статью одного указателя о понятии.
func seedArticle(t *testing.T, pool *pgxpool.Pool, conceptID, editionID int64, workID *int64, title string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO index_concept_articles
			(concept_id, edition_id, work_id, title, title_key)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, conceptID, editionID, workID, title, models.NormalizeIndexTitle(title)).Scan(&id)
	if err != nil {
		t.Fatalf("seedArticle: %v", err)
	}
	return id
}

// seedRubric заводит подрубрику внутри статьи.
func seedRubric(t *testing.T, pool *pgxpool.Pool, articleID int64, title string, orderNumber int) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO index_rubrics (article_id, title, title_key, order_number)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, articleID, title, models.NormalizeIndexTitle(title), orderNumber).Scan(&id)
	if err != nil {
		t.Fatalf("seedRubric: %v", err)
	}
	return id
}

// seedNestedRubric заводит подрубрику ВНУТРИ другой подрубрики (parentID) —
// задача 8, вложенность. Отдельная функция, а не необязательный параметр у
// seedRubric: у seedRubric два вызывающих места из времён до вложенности
// (задача 7), и менять её сигнатуру ради одного нового теста незачем.
func seedNestedRubric(t *testing.T, pool *pgxpool.Pool, articleID, parentID int64, title string, orderNumber int) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO index_rubrics (article_id, parent_id, title, title_key, order_number)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, articleID, parentID, title, models.NormalizeIndexTitle(title), orderNumber).Scan(&id)
	if err != nil {
		t.Fatalf("seedNestedRubric: %v", err)
	}
	return id
}

// seedReference заводит адрес статьи, при желании — под подрубрикой.
// concept_id остаётся NOT NULL в index_references (снимается миграцией
// 000026), поэтому берётся у статьи запросом, а не отдельным параметром.
func seedReference(t *testing.T, pool *pgxpool.Pool, articleID int64, rubricID *int64, volumeNumber, pageStart, pageEnd, orderNumber int) int64 {
	t.Helper()
	ctx := context.Background()

	// concept_id снят с index_references миграцией 000026 (задача 11) —
	// адрес держится только через article_id (и, если он под подрубрикой,
	// rubric_id); понятие резолвится JOIN'ом через index_concept_articles.
	var id int64
	err := pool.QueryRow(ctx, `
		INSERT INTO index_references
			(article_id, rubric_id, volume_number, page_start, page_end, order_number)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, articleID, rubricID, volumeNumber, pageStart, pageEnd, orderNumber).Scan(&id)
	if err != nil {
		t.Fatalf("seedReference: %v", err)
	}
	return id
}

// TestTakenSlugsCountsSlugsOfTheEditionBeingImported — находка сквозной
// рецензии ветки. Прежний запрос ВЫВОДИЛ из «занятых» слаги понятий, у
// которых есть статья ввозимого издания. Замысел был в том, что такая статья
// найдётся по паре (издание, ключ заголовка) и слаг из тела не израсходует.
// Но освобождённый слаг достаётся ДРУГОМУ, новому понятию того же ввоза —
// «Капитал» и «капитал» слагифицируются одинаково, — и INSERT падает на
// уникальном индексе index_concepts.slug, роняя ввоз целиком.
func TestTakenSlugsCountsSlugsOfTheEditionBeingImported(t *testing.T) {
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	ctx := context.Background()
	editionID, workID := seedEditionAndWork(t, pool)

	const slug = "kapital-taken-probe"
	conceptID := seedConcept(t, pool, "Капитал (проба занятых)", slug)
	seedArticle(t, pool, conceptID, editionID, &workID, "Капитал (проба занятых)")

	taken, err := repo.TakenSlugs(ctx)
	if err != nil {
		t.Fatalf("TakenSlugs: %v", err)
	}
	if !taken[slug] {
		t.Errorf("слаг %q не назван занятым, хотя понятие с ним существует: ввоз выдаст его новому понятию и упадёт на уникальном индексе", slug)
	}
}

// TestReplaceForEditionRefusesTwoRubricsWithOneKey — спека требует отказа, а
// не склейки: «Две подрубрики с одним title_key в одной статье — отказ ввоза».
// Прежний код просто пропускал повтор ключа (дедупликация нужна: подрубрика
// приходит строкой НА КАЖДОМ адресе), и адреса второй подрубрики молча
// уезжали под названием первой.
func TestReplaceForEditionRefusesTwoRubricsWithOneKey(t *testing.T) {
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	ctx := context.Background()
	editionID, workID := seedEditionAndWork(t, pool)

	// Один ключ (нормализация схлопывает двойной пробел и сводит ё→е), два
	// разных исходных написания — ровно тот случай, который склеивался.
	const rubricA = "сущность  труда"
	const rubricB = "Сущность труда"
	concepts := []*models.IndexConcept{{
		Title: "Труд (проба подрубрик)", Slug: "trud-proba-rubrik", SortKey: "труд (проба подрубрик)",
	}}
	concepts[0].Articles = []*models.IndexArticle{{
		WorkID: &workID,
		References: []*models.IndexReference{
			{VolumeNumber: 1, PageStart: 10, PageEnd: 10, Rubric: rubricA, OrderNumber: 1},
			{VolumeNumber: 1, PageStart: 20, PageEnd: 20, Rubric: rubricB, OrderNumber: 2},
		},
	}}

	_, err := repo.ReplaceForEdition(ctx, editionID, concepts)
	if err == nil {
		t.Fatal("ожидали отказ на двух подрубриках с одним ключом, получили nil")
	}
	if !strings.Contains(err.Error(), rubricA) || !strings.Contains(err.Error(), rubricB) {
		t.Errorf("ошибка не называет оба заголовка подрубрики: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM index_concept_articles WHERE edition_id = $1`, editionID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("статей записано %d, хотели 0 — отказ обязан откатить транзакцию целиком", count)
	}
}

// TestReplaceForEditionAcceptsTheSameRubricRepeatedVerbatim — обратная
// сторона: дедупликация обязана остаться. Подрубрика приходит строкой на
// КАЖДОМ адресе, и десять адресов одной подрубрики — норма, а не дубль.
func TestReplaceForEditionAcceptsTheSameRubricRepeatedVerbatim(t *testing.T) {
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	ctx := context.Background()
	editionID, workID := seedEditionAndWork(t, pool)

	const rubric = "сущность труда"
	concepts := []*models.IndexConcept{{
		Title: "Труд (проба повтора)", Slug: "trud-proba-povtora", SortKey: "труд (проба повтора)",
	}}
	concepts[0].Articles = []*models.IndexArticle{{
		WorkID: &workID,
		References: []*models.IndexReference{
			{VolumeNumber: 1, PageStart: 10, PageEnd: 10, Rubric: rubric, OrderNumber: 1},
			{VolumeNumber: 1, PageStart: 20, PageEnd: 20, Rubric: rubric, OrderNumber: 2},
		},
	}}

	if _, err := repo.ReplaceForEdition(ctx, editionID, concepts); err != nil {
		t.Fatalf("ReplaceForEdition: %v", err)
	}

	var rubrics int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM index_rubrics ru
		JOIN index_concept_articles a ON a.id = ru.article_id
		WHERE a.edition_id = $1
	`, editionID).Scan(&rubrics); err != nil {
		t.Fatalf("count rubrics: %v", err)
	}
	if rubrics != 1 {
		t.Errorf("подрубрик %d, хотели 1 — повтор того же написания это норма", rubrics)
	}
}

// Отсылка, в заголовке-цели которой стоит типографский неразрывный пробел
// (U+00A0), обязана связаться с понятием. Ключ понятия считает Go
// (models.NormalizeIndexTitle: NFC + strings.Fields, режущий по любому
// юникодному пробелу); ключ отсылки до миграции 000029 считало выражение
// SQL, у которого нет ни того ни другого, — и связь молча не возникала.
// В корпусе Маркса таких отсылок нет, в ленинском указателе их 1133.
func TestReplaceForEditionLinksThroughNonBreakingSpace(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	editionID, _ := seedEditionAndWork(t, pool)

	const targetTitle = "Труд\u00a0абстрактный (нбп)"

	target := &models.IndexConcept{
		Title:   "Труд абстрактный (нбп)",
		Slug:    "trud-abstraktnyj-nbp",
		SortKey: "труд абстрактный (нбп)",
	}
	target.Articles = []*models.IndexArticle{{Kind: models.IndexConceptKindArticle}}

	source := &models.IndexConcept{
		Title:   "Абстрактный труд (нбп)",
		Slug:    "abstraktnyj-trud-nbp",
		SortKey: "абстрактный труд (нбп)",
	}
	source.Articles = []*models.IndexArticle{{
		Kind: models.IndexConceptKindArticle,
		Links: []*models.IndexConceptLink{{
			TargetTitle: targetTitle,
			Kind:        models.IndexLinkKindSee,
			OrderNumber: 1,
		}},
	}}

	if _, err := repo.ReplaceForEdition(ctx, editionID,
		[]*models.IndexConcept{target, source}); err != nil {
		t.Fatalf("ReplaceForEdition: %v", err)
	}

	var toConcept *int64
	var storedKey string
	if err := pool.QueryRow(ctx, `
		SELECT to_concept_id, target_title_key
		FROM index_concept_links WHERE target_title = $1
	`, targetTitle).Scan(&toConcept, &storedKey); err != nil {
		t.Fatalf("read link: %v", err)
	}
	if toConcept == nil {
		t.Errorf("отсылка с неразрывным пробелом не связалась с понятием")
	}
	if want := models.NormalizeIndexTitle(targetTitle); storedKey != want {
		t.Errorf("target_title_key = %q, want %q", storedKey, want)
	}
}

// Цель отсылки и заглавие понятия расходятся набором у тире — пробелом и
// видом (в ленинском указателе понятие «Империалистическая война 1914— 1918
// гг.», а отсылки на него — «…1914—1918  гг.»), и отсылка обязана связаться:
// ключ обеих сторон считает один models.NormalizeIndexTitle, а связывание
// (шаг 7) сравнивает ключи. Контроль рядом — цель, расходящаяся пробелом
// МЕЖДУ СЛОВАМИ, связываться не должна: такой пробел ключ различает.
func TestReplaceForEditionLinksAcrossDashSpacing(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	editionID, _ := seedEditionAndWork(t, pool)

	const (
		spacedTarget = "Империалистическая война 1914— 1918 гг. (тст)"
		hyphenTarget = "Империалистическая война 1914 - 1918 гг. (тст)"
		gluedTarget  = "Империалистическаявойна 1914—1918 гг. (тст)"
	)

	target := &models.IndexConcept{
		Title:   "Империалистическая война 1914—1918 гг. (тст)",
		Slug:    "imperialisticheskaya-vojna-1914-1918-gg-tst",
		SortKey: "империалистическая война 1914—1918 гг. (тст)",
	}
	target.Articles = []*models.IndexArticle{{Kind: models.IndexConceptKindArticle}}

	source := &models.IndexConcept{
		Title:   "Защита отечества (тст)",
		Slug:    "zashchita-otechestva-tst",
		SortKey: "защита отечества (тст)",
	}
	var links []*models.IndexConceptLink
	for i, title := range []string{spacedTarget, hyphenTarget, gluedTarget} {
		links = append(links, &models.IndexConceptLink{
			TargetTitle: title, Kind: models.IndexLinkKindSee, OrderNumber: i + 1,
		})
	}
	source.Articles = []*models.IndexArticle{{Kind: models.IndexConceptKindArticle, Links: links}}

	if _, err := repo.ReplaceForEdition(ctx, editionID,
		[]*models.IndexConcept{target, source}); err != nil {
		t.Fatalf("ReplaceForEdition: %v", err)
	}

	var targetID int64
	if err := pool.QueryRow(ctx,
		`SELECT id FROM index_concepts WHERE slug = $1`, target.Slug).Scan(&targetID); err != nil {
		t.Fatalf("read target concept: %v", err)
	}
	linked := func(title string) *int64 {
		t.Helper()
		var to *int64
		if err := pool.QueryRow(ctx,
			`SELECT to_concept_id FROM index_concept_links WHERE target_title = $1`,
			title).Scan(&to); err != nil {
			t.Fatalf("read link %q: %v", title, err)
		}
		return to
	}
	for _, title := range []string{spacedTarget, hyphenTarget} {
		if to := linked(title); to == nil || *to != targetID {
			t.Errorf("отсылка %q не связалась с понятием %d: %v", title, targetID, to)
		}
	}
	if to := linked(gluedTarget); to != nil {
		t.Errorf("отсылка %q связалась с %d, хотя расходится пробелом между словами", gluedTarget, *to)
	}
}

// TestReplaceForEditionAllowsSameRubricKeyUnderDifferentParents — печатная
// вложенность «КПСС — съезды» → съезд → аспект.
//
// «значение съезда» стоит у ШЕСТИ съездов ленинского указателя,
// «организационные вопросы» — тоже у шести, всего 17 таких имён. До
// вложенности это коллизия ключа в одной статье, роняющая ВЕСЬ ввоз; с
// вложенностью — законные разные подрубрики разных родителей.
func TestReplaceForEditionAllowsSameRubricKeyUnderDifferentParents(t *testing.T) {
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	ctx := context.Background()
	editionID, workID := seedEditionAndWork(t, pool)

	const (
		firstCongress  = "I съезд РСДРП. 1-3 (13-15) марта 1898 г. Минск"
		secondCongress = "II съезд РСДРП. 17 (30) июля - 10 (23) августа 1903 г."
		aspect         = "значение съезда"
	)
	concepts := []*models.IndexConcept{{
		Title: "КПСС — съезды", Slug: "kpss-sezdy", SortKey: "кпсс — съезды",
	}}
	concepts[0].Articles = []*models.IndexArticle{{
		WorkID: &workID,
		Kind:   "article",
		References: []*models.IndexReference{
			{RubricPath: []string{firstCongress, aspect},
				VolumeNumber: 4, PageStart: 174, PageEnd: 174, OrderNumber: 1},
			{RubricPath: []string{secondCongress, aspect},
				VolumeNumber: 7, PageStart: 74, PageEnd: 74, OrderNumber: 2},
		},
	}}

	if _, err := repo.ReplaceForEdition(ctx, editionID, concepts); err != nil {
		t.Fatalf("ввоз упал на законной вложенности: %v", err)
	}

	// Подрубрик ровно четыре: два съезда на верхнем уровне и по аспекту под
	// каждым. Три означали бы склейку аспектов в одну строку.
	type rubricRow struct {
		id       int64
		title    string
		key      string
		parent   *int64
		orderNum int
	}
	rows, err := pool.Query(ctx, `
		SELECT ru.id, ru.title, ru.title_key, ru.parent_id, ru.order_number
		FROM index_rubrics ru
		JOIN index_concept_articles a ON a.id = ru.article_id
		WHERE a.edition_id = $1
		ORDER BY ru.id
	`, editionID)
	if err != nil {
		t.Fatalf("read rubrics: %v", err)
	}
	var got []rubricRow
	for rows.Next() {
		var r rubricRow
		if err := rows.Scan(&r.id, &r.title, &r.key, &r.parent, &r.orderNum); err != nil {
			rows.Close()
			t.Fatalf("scan rubric: %v", err)
		}
		got = append(got, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("read rubrics: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("подрубрик %d, хотели 4 (два съезда + по аспекту под каждым): %+v", len(got), got)
	}

	aspectKey := models.NormalizeIndexTitle(aspect)
	byID := map[int64]rubricRow{}
	for _, r := range got {
		byID[r.id] = r
	}
	var aspects []rubricRow
	for _, r := range got {
		if r.key == aspectKey {
			aspects = append(aspects, r)
		}
	}
	if len(aspects) != 2 {
		t.Fatalf("подрубрик %q %d, хотели 2 — по одной под каждым съездом", aspect, len(aspects))
	}
	for _, r := range aspects {
		if r.parent == nil {
			t.Fatalf("подрубрика %q осталась без родителя — вложенности не случилось", r.title)
		}
		if _, ok := byID[*r.parent]; !ok {
			t.Fatalf("родитель %d подрубрики %q не найден среди подрубрик статьи", *r.parent, r.title)
		}
		// order_number считается СРЕДИ ДЕТЕЙ СВОЕГО РОДИТЕЛЯ, а не сквозным
		// по статье: аспект у каждого съезда первый и единственный.
		if r.orderNum != 1 {
			t.Errorf("order_number подрубрики %q = %d, хотели 1 — счёт идёт среди детей родителя", r.title, r.orderNum)
		}
	}
	if *aspects[0].parent == *aspects[1].parent {
		t.Fatalf("оба %q сели под одного родителя %d — адреса второго съезда ушли бы под первый", aspect, *aspects[0].parent)
	}
	parents := map[int64]string{
		*aspects[0].parent: byID[*aspects[0].parent].title,
		*aspects[1].parent: byID[*aspects[1].parent].title,
	}
	for id, title := range parents {
		if title != firstCongress && title != secondCongress {
			t.Errorf("родитель %d называется %q — хотели один из съездов", id, title)
		}
		if p := byID[id].parent; p != nil {
			t.Errorf("съезд %q получил родителя %d — корень пути обязан висеть прямо на статье", title, *p)
		}
	}

	// У каждой подрубрики-аспекта свой адрес, и это адрес своего съезда.
	want := map[string][2]int{
		firstCongress:  {4, 174},
		secondCongress: {7, 74},
	}
	for _, r := range aspects {
		var vol, page int
		if err := pool.QueryRow(ctx, `
			SELECT volume_number, page_start FROM index_references WHERE rubric_id = $1
		`, r.id).Scan(&vol, &page); err != nil {
			t.Fatalf("адрес подрубрики %d (%q под %q): %v", r.id, r.title, byID[*r.parent].title, err)
		}
		parent := byID[*r.parent].title
		if [2]int{vol, page} != want[parent] {
			t.Errorf("под %q адрес т.%d стр.%d, хотели т.%d стр.%d",
				parent, vol, page, want[parent][0], want[parent][1])
		}
	}
}

// TestReplaceForEditionStillRejectsSameRubricKeyUnderSameParent — обратная
// сторона ослабления. Две подрубрики с одним ключом и ОДНИМ родителем
// по-прежнему дефект разбора: адреса второй ушли бы под название первой.
// Ослабление правила касается только РАЗНЫХ родителей.
func TestReplaceForEditionStillRejectsSameRubricKeyUnderSameParent(t *testing.T) {
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	ctx := context.Background()
	editionID, workID := seedEditionAndWork(t, pool)

	// Один ключ (нормализация схлопывает двойной пробел и сводит ё→е), два
	// разных исходных написания, один и тот же родитель.
	const (
		congress = "I съезд РСДРП. 1-3 (13-15) марта 1898 г. Минск"
		aspectA  = "значение  съезда"
		aspectB  = "Значение съезда"
	)
	concepts := []*models.IndexConcept{{
		Title: "КПСС — съезды (проба одного родителя)", Slug: "kpss-sezdy-proba",
		SortKey: "кпсс — съезды (проба)",
	}}
	concepts[0].Articles = []*models.IndexArticle{{
		WorkID: &workID,
		References: []*models.IndexReference{
			{RubricPath: []string{congress, aspectA},
				VolumeNumber: 4, PageStart: 174, PageEnd: 174, OrderNumber: 1},
			{RubricPath: []string{congress, aspectB},
				VolumeNumber: 4, PageStart: 180, PageEnd: 180, OrderNumber: 2},
		},
	}}

	_, err := repo.ReplaceForEdition(ctx, editionID, concepts)
	if err == nil {
		t.Fatal("ожидали отказ на двух подрубриках с одним ключом под одним родителем, получили nil")
	}
	if !strings.Contains(err.Error(), aspectA) || !strings.Contains(err.Error(), aspectB) {
		t.Errorf("ошибка не называет оба написания подрубрики: %v", err)
	}
	// Сообщение обязано назвать родителя: без него «две подрубрики с одним
	// ключом» у вложенного указателя непонятно, где именно искать.
	if !strings.Contains(err.Error(), congress) {
		t.Errorf("ошибка не называет родителя %q: %v", congress, err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM index_concept_articles WHERE edition_id = $1`, editionID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("статей записано %d, хотели 0 — отказ обязан откатить транзакцию целиком", count)
	}
}
