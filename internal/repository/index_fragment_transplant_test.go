package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

// leninPayload — одно понятие с одним адресом под подрубрикой. Тело ввоза
// строится функцией, а не константой: переимпорт обязан прогнать РОВНО ТО ЖЕ
// тело второй раз, и общая на два вызова структура со срезами внутри дала бы
// ложную зелень, если бы ввоз её перезаписал.
func leninPayload(rubric string) []*models.IndexConcept {
	c := &models.IndexConcept{
		Title:   "Кооперация (вырезки)",
		Slug:    "kooperaciya-vyrezki",
		SortKey: "кооперация (вырезки)",
	}
	c.Articles = []*models.IndexArticle{{
		Kind: models.IndexConceptKindArticle,
		References: []*models.IndexReference{{
			Rubric:       rubric,
			VolumeNumber: 31,
			PageStart:    92,
			PageEnd:      93,
			OrderNumber:  1,
		}},
	}}
	return []*models.IndexConcept{c}
}

// seedPageID заводит полосу тома и возвращает её идентификатор: вырезка
// висит на pages с ON DELETE CASCADE, поддельным id её не завести.
func seedPageID(t *testing.T, pool *pgxpool.Pool, workID int64, number int) int64 {
	t.Helper()
	p := &models.Page{
		WorkID: workID, PageNumber: number,
		ContentMarkdown: "полоса для вырезки",
		Status:          models.PageStatusNotProofread,
	}
	if err := NewPageRepository(pool).Create(context.Background(), p); err != nil {
		t.Fatalf("seedPageID: %v", err)
	}
	return p.ID
}

// Вырезка, подтверждённая человеком, обязана пережить переимпорт издания.
// index_fragments висят на index_references с ON DELETE CASCADE, а ввоз
// адреса пересоздаёт — приёмка ветки 1 измерила: 2 вырезки до переимпорта,
// 0 после. Пока вырезок две, цена мала; после ленинского ввоза переимпорт
// станет рутиной.
func TestReplaceForEditionTransplantsFragmentsOnReimport(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	editionID, workID := seedEditionAndWork(t, pool)
	pageID := seedPageID(t, pool, workID, 92)

	if _, err := repo.ReplaceForEdition(ctx, editionID, leninPayload("— о кооперации")); err != nil {
		t.Fatalf("первый ввоз: %v", err)
	}

	var refID int64
	if err := pool.QueryRow(ctx, `
		SELECT r.id FROM index_references r
		JOIN index_concept_articles a ON a.id = r.article_id
		WHERE a.edition_id = $1
	`, editionID).Scan(&refID); err != nil {
		t.Fatalf("адрес после первого ввоза: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO index_fragments
			(reference_id, order_number, start_page_id, start_offset,
			 end_page_id, end_offset, head_quote, tail_quote,
			 start_hash, end_hash, status)
		VALUES ($1, 1, $2, 10, $2, 120, 'начало вырезки', 'конец вырезки',
		        repeat('a', 64), repeat('b', 64), 'confirmed')
	`, refID, pageID); err != nil {
		t.Fatalf("завести вырезку: %v", err)
	}

	warnings, err := repo.ReplaceForEdition(ctx, editionID, leninPayload("— о кооперации"))
	if err != nil {
		t.Fatalf("переимпорт: %v", err)
	}

	var count int
	var status, head string
	if err := pool.QueryRow(ctx, `
		SELECT count(*), coalesce(max(f.status), ''), coalesce(max(f.head_quote), '')
		FROM index_fragments f
		JOIN index_references r ON r.id = f.reference_id
		JOIN index_concept_articles a ON a.id = r.article_id
		WHERE a.edition_id = $1
	`, editionID).Scan(&count, &status, &head); err != nil {
		t.Fatalf("вырезки после переимпорта: %v", err)
	}
	if count != 1 {
		t.Fatalf("вырезок после переимпорта = %d, want 1 (переимпорт их унёс)", count)
	}
	if status != "confirmed" || head != "начало вырезки" {
		t.Errorf("вырезка пересажена испорченной: status=%q head=%q", status, head)
	}
	if len(warnings) != 0 {
		t.Errorf("пересадка без потерь не должна предупреждать: %v", warnings)
	}
}

// Переименованная подрубрика — вырезка места не находит. Она обязана быть
// НАЗВАНА, а не исчезнуть молча: восстановить её человек сможет, узнать о
// потере из тишины — нет.
func TestReplaceForEditionNamesFragmentLostToRenamedRubric(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	editionID, workID := seedEditionAndWork(t, pool)
	pageID := seedPageID(t, pool, workID, 92)

	if _, err := repo.ReplaceForEdition(ctx, editionID, leninPayload("— о кооперации")); err != nil {
		t.Fatalf("первый ввоз: %v", err)
	}
	var refID int64
	if err := pool.QueryRow(ctx, `
		SELECT r.id FROM index_references r
		JOIN index_concept_articles a ON a.id = r.article_id
		WHERE a.edition_id = $1
	`, editionID).Scan(&refID); err != nil {
		t.Fatalf("адрес: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO index_fragments
			(reference_id, order_number, start_page_id, start_offset,
			 end_page_id, end_offset, head_quote, tail_quote,
			 start_hash, end_hash, status)
		VALUES ($1, 1, $2, 10, $2, 120, 'начало', 'конец',
		        repeat('a', 64), repeat('b', 64), 'confirmed')
	`, refID, pageID); err != nil {
		t.Fatalf("завести вырезку: %v", err)
	}

	warnings, err := repo.ReplaceForEdition(ctx, editionID,
		leninPayload("— о кооперации и кредите"))
	if err != nil {
		t.Fatalf("переимпорт: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM index_fragments f
		JOIN index_references r ON r.id = f.reference_id
		JOIN index_concept_articles a ON a.id = r.article_id
		WHERE a.edition_id = $1
	`, editionID).Scan(&count); err != nil {
		t.Fatalf("вырезки: %v", err)
	}
	if count != 0 {
		t.Errorf("вырезка неожиданно нашла место у переименованной подрубрики: %d", count)
	}
	if len(warnings) != 1 {
		t.Fatalf("потеря вырезки не названа: warnings = %v", warnings)
	}
	// "— о кооперации" — СТАРОЕ имя подрубрики (какой её видела вырезка ДО
	// переимпорта), а не новое ("— о кооперации и кредите"): круг правок 1
	// задачи 8 сменил RubricKey (нормализованный, NUL-склеенный при
	// вложенности — нечитаемый) на TitlePath человеку, и это как раз то
	// место, где путаница читателя стоила бы дороже всего — сообщение о
	// потере обязано назвать заголовок, который реально стоял в указателе.
	for _, want := range []string{"Кооперация (вырезки)", "31", "92", "— о кооперации"} {
		if !strings.Contains(warnings[0], want) {
			t.Errorf("предупреждение не называет %q: %s", want, warnings[0])
		}
	}
}

// TestReplaceForEditionNamesFragmentLostUnderNestedRubricWithReadablePath —
// задача 8, круг правок 1, находка 2: предупреждение о потерянной вырезке
// печатало `f.Key.RubricKey` — НОРМАЛИЗОВАННЫЕ ключи подрубрик, склеенные
// NUL-байтом (`\x00`) при вложенности, то есть буквально нечитаемую строку
// человеку. Правка протянула `anc.path` (заголовки, не ключи) как
// `TitlePath` и печатает его через `rubricParentDescription` (путь через
// " → ", тем же приёмом, что и у отказа ввоза при дубле ключа подрубрики).
//
// Тест заводит ВЛОЖЕННЫЙ адрес (родитель → лист), переименовывает ЛИСТ при
// переимпорте — адрес пропадает, вырезка ищет место и не находит — и
// проверяет, что предупреждение называет путь ЧЕЛОВЕЧЕСКИ, через "→", а не
// NUL-байтом: с прежним кодом здесь стояло бы
// "параграф (тест)\x00первый пункт (тест)" без единого пробела и стрелки,
// подстрочная проверка на "параграф (тест) → первый пункт (тест)" на нём не
// прошла бы — что и служит мутационной проверкой этого теста (см. отчёт).
func TestReplaceForEditionNamesFragmentLostUnderNestedRubricWithReadablePath(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	editionID, workID := seedEditionAndWork(t, pool)
	pageID := seedPageID(t, pool, workID, 92)

	const parent = "параграф (тест)"
	nestedPayload := func(leaf string) []*models.IndexConcept {
		c := &models.IndexConcept{
			Title:   "Вложенная потеря (вырезки)",
			Slug:    "vlozhennaya-poterya-vyrezki",
			SortKey: "вложенная потеря (вырезки)",
		}
		c.Articles = []*models.IndexArticle{{
			Kind: models.IndexConceptKindArticle,
			References: []*models.IndexReference{{
				RubricPath:   []string{parent, leaf},
				VolumeNumber: 31, PageStart: 92, PageEnd: 93, OrderNumber: 1,
			}},
		}}
		return []*models.IndexConcept{c}
	}

	if _, err := repo.ReplaceForEdition(ctx, editionID, nestedPayload("первый пункт (тест)")); err != nil {
		t.Fatalf("первый ввоз: %v", err)
	}
	var refID int64
	if err := pool.QueryRow(ctx, `
		SELECT r.id FROM index_references r
		JOIN index_concept_articles a ON a.id = r.article_id
		WHERE a.edition_id = $1
	`, editionID).Scan(&refID); err != nil {
		t.Fatalf("адрес: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO index_fragments
			(reference_id, order_number, start_page_id, start_offset,
			 end_page_id, end_offset, head_quote, tail_quote,
			 start_hash, end_hash, status)
		VALUES ($1, 1, $2, 10, $2, 120, 'начало', 'конец',
		        repeat('a', 64), repeat('b', 64), 'confirmed')
	`, refID, pageID); err != nil {
		t.Fatalf("завести вырезку: %v", err)
	}

	// Лист переименован, родитель — нет: старый ключ ("параграф (тест)" +
	// "первый пункт (тест)") не найдётся среди новых.
	warnings, err := repo.ReplaceForEdition(ctx, editionID, nestedPayload("второй пункт (тест)"))
	if err != nil {
		t.Fatalf("переимпорт: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("потеря вырезки не названа: warnings = %v", warnings)
	}
	if want := parent + " → первый пункт (тест)"; !strings.Contains(warnings[0], want) {
		t.Errorf("предупреждение не называет путь по-человечески (%q): %s", want, warnings[0])
	}
	if strings.Contains(warnings[0], "\x00") {
		t.Errorf("предупреждение протекло NUL-склеенным ключом: %s", warnings[0])
	}
}

// TestReplaceForEditionTransplantsFragmentsByFullRubricPathNotLeafAlone —
// задача 8, находка задачи 7: refKey строился из ЛИСТА подрубрики
// (title_key), а после вложенности два адреса с одинаковым листом под
// РАЗНЫМИ родителями и одинаковым печатным адресом (том/страницы) склеивались
// в один ключ. newReferenceKeys дедуплицирует ключ «первый выигрывает», и обе
// вырезки садились на ОДИН новый адрес — вторая молча теряла свою.
//
// Тест заводит ровно такую пару: два адреса статьи, лист подрубрики один и
// тот же («общий аспект»), родители разные, том/страницы совпадают. Каждому
// достаётся своя вырезка (различимая по head_quote). Переимпорт с тем же
// телом обязан вернуть КАЖДУЮ вырезку на адрес под ЕЁ родителем, а не слить
// обе на одного из них.
func TestReplaceForEditionTransplantsFragmentsByFullRubricPathNotLeafAlone(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	editionID, workID := seedEditionAndWork(t, pool)
	pageID := seedPageID(t, pool, workID, 92)

	const (
		parentA = "родитель один"
		parentB = "родитель два"
		aspect  = "общий аспект"
	)
	payload := func() []*models.IndexConcept {
		c := &models.IndexConcept{
			Title:   "Коллизия листа (вырезки)",
			Slug:    "kollizia-lista-vyrezki",
			SortKey: "коллизия листа (вырезки)",
		}
		c.Articles = []*models.IndexArticle{{
			Kind: models.IndexConceptKindArticle,
			References: []*models.IndexReference{
				{RubricPath: []string{parentA, aspect},
					VolumeNumber: 31, PageStart: 92, PageEnd: 93, OrderNumber: 1},
				{RubricPath: []string{parentB, aspect},
					VolumeNumber: 31, PageStart: 92, PageEnd: 93, OrderNumber: 2},
			},
		}}
		return []*models.IndexConcept{c}
	}

	if _, err := repo.ReplaceForEdition(ctx, editionID, payload()); err != nil {
		t.Fatalf("первый ввоз: %v", err)
	}

	type refRow struct {
		id     int64
		parent string
	}
	readRefsByParent := func() []refRow {
		t.Helper()
		rows, err := pool.Query(ctx, `
			SELECT r.id, par.title
			FROM index_references r
			JOIN index_rubrics ru ON ru.id = r.rubric_id
			JOIN index_rubrics par ON par.id = ru.parent_id
			JOIN index_concept_articles a ON a.id = r.article_id
			WHERE a.edition_id = $1
			ORDER BY r.order_number
		`, editionID)
		if err != nil {
			t.Fatalf("адреса по родителю: %v", err)
		}
		defer rows.Close()
		var out []refRow
		for rows.Next() {
			var rr refRow
			if err := rows.Scan(&rr.id, &rr.parent); err != nil {
				t.Fatalf("scan адреса: %v", err)
			}
			out = append(out, rr)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("чтение адресов: %v", err)
		}
		return out
	}

	before := readRefsByParent()
	if len(before) != 2 {
		t.Fatalf("адресов до пересадки %d, хотели 2: %+v", len(before), before)
	}
	if before[0].parent != parentA || before[1].parent != parentB {
		t.Fatalf("родители перепутаны: %+v", before)
	}

	insertFragment := func(refID int64, offset int, quote string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			INSERT INTO index_fragments
				(reference_id, order_number, start_page_id, start_offset,
				 end_page_id, end_offset, head_quote, tail_quote,
				 start_hash, end_hash, status)
			VALUES ($1, 1, $2, $3, $2, $3+50, $4, 'конец',
			        repeat('a', 64), repeat('b', 64), 'confirmed')
		`, refID, pageID, offset, quote); err != nil {
			t.Fatalf("завести вырезку %q: %v", quote, err)
		}
	}
	insertFragment(before[0].id, 10, "цитата под "+parentA)
	insertFragment(before[1].id, 200, "цитата под "+parentB)

	warnings, err := repo.ReplaceForEdition(ctx, editionID, payload())
	if err != nil {
		t.Fatalf("переимпорт: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("пересадка без переименований не должна предупреждать: %v", warnings)
	}

	rows, err := pool.Query(ctx, `
		SELECT par.title, f.head_quote
		FROM index_fragments f
		JOIN index_references r ON r.id = f.reference_id
		JOIN index_rubrics ru ON ru.id = r.rubric_id
		JOIN index_rubrics par ON par.id = ru.parent_id
		JOIN index_concept_articles a ON a.id = r.article_id
		WHERE a.edition_id = $1
		ORDER BY par.title
	`, editionID)
	if err != nil {
		t.Fatalf("вырезки после переимпорта: %v", err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var parent, quote string
		if err := rows.Scan(&parent, &quote); err != nil {
			t.Fatalf("scan вырезки: %v", err)
		}
		got[parent] = quote
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("чтение вырезок: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("после переимпорта вырезок под разными родителями %d, хотели 2 (одна под каждым): %+v", len(got), got)
	}
	if got[parentA] != "цитата под "+parentA {
		t.Errorf("под %q вырезка %q, хотели %q — коллизия листа увела чужую цитату", parentA, got[parentA], "цитата под "+parentA)
	}
	if got[parentB] != "цитата под "+parentB {
		t.Errorf("под %q вырезка %q, хотели %q — коллизия листа увела чужую цитату", parentB, got[parentB], "цитата под "+parentB)
	}
}

// Список колонок CopyFrom обязан совпадать с порядком значений в строке.
// Ошибка здесь не падает и не видна ниоткуда: Postgres молча принимает
// перепутанные местами колонки одного типа, и 199 тыс. ленинских адресов
// уезжают в базу вывернутыми. Проверяется КАЖДАЯ колонка списка, потому что
// пересадка вырезок читает ту же таблицу обратно и на согласованно
// перепутанной паре остаётся зелёной.
//
// Тест дописан сверх брифа: прицельная мутация (местами переставлены
// "page_start" и "page_end" в списке колонок) пережила весь пакет.
func TestReplaceForEditionCopiesReferenceColumnsInOrder(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	editionID, _ := seedEditionAndWork(t, pool)

	part := "II"
	concepts := leninPayload("— о кооперации")
	concepts[0].Articles[0].References[0] = &models.IndexReference{
		Rubric:       "— о кооперации",
		VolumeNumber: 31,
		VolumePart:   &part,
		PageStart:    92,
		PageEnd:      93,
		OrderNumber:  7,
		IsUncertain:  true,
		Note:         "печатная опечатка",
	}

	if _, err := repo.ReplaceForEdition(ctx, editionID, concepts); err != nil {
		t.Fatalf("ввоз: %v", err)
	}

	var volume, pageStart, pageEnd, order int
	var volumePart, note, rubric string
	var uncertain bool
	if err := pool.QueryRow(ctx, `
		SELECT r.volume_number, COALESCE(r.volume_part, ''), r.page_start, r.page_end,
		       r.order_number, r.is_uncertain, r.note, COALESCE(ru.title, '')
		FROM index_references r
		JOIN index_concept_articles a ON a.id = r.article_id
		LEFT JOIN index_rubrics ru ON ru.id = r.rubric_id
		WHERE a.edition_id = $1
	`, editionID).Scan(&volume, &volumePart, &pageStart, &pageEnd, &order,
		&uncertain, &note, &rubric); err != nil {
		t.Fatalf("адрес после ввоза: %v", err)
	}

	if volume != 31 || volumePart != "II" || pageStart != 92 || pageEnd != 93 ||
		order != 7 || !uncertain || note != "печатная опечатка" ||
		rubric != "— о кооперации" {
		t.Errorf("адрес уехал вывернутым: том=%d часть=%q стр.=%d—%d порядок=%d сомнит.=%v примеч.=%q подрубрика=%q",
			volume, volumePart, pageStart, pageEnd, order, uncertain, note, rubric)
	}
}

// Статья, исчезнувшая из указателя целиком, уносит вырезки безвозвратно:
// адресов у неё после сноса не остаётся ни в каком виде, пересаживать не на
// что. Потеря обязана быть НАЗВАНА — молчание здесь равно тихому
// уничтожению подтверждённой человеком работы.
//
// Тест дописан сверх брифа: прицельная мутация (тело цикла doomedWarnings
// заменено на `_, _ = title, n`) пережила весь пакет — ветка счёта обречённых
// вырезок не была покрыта ничем.
func TestReplaceForEditionNamesFragmentsOfVanishedArticle(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := NewIndexRepository(pool)
	editionID, workID := seedEditionAndWork(t, pool)
	pageID := seedPageID(t, pool, workID, 92)

	if _, err := repo.ReplaceForEdition(ctx, editionID, leninPayload("— о кооперации")); err != nil {
		t.Fatalf("первый ввоз: %v", err)
	}
	var refID int64
	if err := pool.QueryRow(ctx, `
		SELECT r.id FROM index_references r
		JOIN index_concept_articles a ON a.id = r.article_id
		WHERE a.edition_id = $1
	`, editionID).Scan(&refID); err != nil {
		t.Fatalf("адрес: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO index_fragments
			(reference_id, order_number, start_page_id, start_offset,
			 end_page_id, end_offset, head_quote, tail_quote,
			 start_hash, end_hash, status)
		VALUES ($1, 1, $2, 10, $2, 120, 'начало', 'конец',
		        repeat('a', 64), repeat('b', 64), 'confirmed')
	`, refID, pageID); err != nil {
		t.Fatalf("завести вырезку: %v", err)
	}

	// Второй ввоз не знает прежнего понятия вовсе — статья уходит целиком.
	other := leninPayload("— о кредите")
	other[0].Title = "Кредит (вырезки)"
	other[0].Slug = "kredit-vyrezki"
	other[0].SortKey = "кредит (вырезки)"

	warnings, err := repo.ReplaceForEdition(ctx, editionID, other)
	if err != nil {
		t.Fatalf("переимпорт: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM index_fragments f
		JOIN index_references r ON r.id = f.reference_id
		JOIN index_concept_articles a ON a.id = r.article_id
		WHERE a.edition_id = $1
	`, editionID).Scan(&count); err != nil {
		t.Fatalf("вырезки: %v", err)
	}
	if count != 0 {
		t.Errorf("вырезки ушедшей статьи уцелели: %d", count)
	}
	if len(warnings) != 1 {
		t.Fatalf("потеря вырезок ушедшей статьи не названа: warnings = %v", warnings)
	}
	for _, want := range []string{"Кооперация (вырезки)", "унесла 1 вырезку"} {
		if !strings.Contains(warnings[0], want) {
			t.Errorf("предупреждение не называет %q: %s", want, warnings[0])
		}
	}
}
