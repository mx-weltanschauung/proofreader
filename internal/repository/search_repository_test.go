package repository

import (
	"context"
	"strings"
	"testing"

	"proofreader/internal/models"
)

// Пустую базу и владельца (id = 1) готовит testPool — см. main_test.go.
// База должна стоять на миграции 000016 (столбец search_vector).
func newSearchTestRepo(t *testing.T) (*SearchRepository, context.Context) {
	t.Helper()
	pool := testPool(t)
	ctx := context.Background()
	// Передние листы (11) нумеруются римскими от обложки: page_offset −1,
	// numbering_style roman — тот самый случай, который прячет офсет вида +10.
	// Собрание А: том 1 (с передними листами) и том 2 с ручной подписью;
	// собрание Б: один том. Полосы 4–5 тома 1 лежат в «Примечаниях» (аппарат).
	// Том В (40) вне обоих собраний: пара вложенных глав ([1,10] снаружи,
	// [3,3] внутри) над словом «вложенности», которого нет больше нигде в
	// фикстуре — специально, чтобы не задеть счётчики других тестов ни по
	// одному из существующих запросов ("Гегеля", "елка"/"ёлка", "что
	// делать", "Народничество").
	if _, err := pool.Exec(ctx, `
		INSERT INTO editions (id, title, slug) VALUES (1, 'Собрание А', 'a'), (2, 'Собрание Б', 'b');
		INSERT INTO works (id, title, author, file_path, owner_id, role, parent_work_id, edition_id, volume_number, shelf_label, page_offset, numbering_style)
		VALUES
		  (10, 'Том 1',          'Автор', 'f', 1, 'volume',       NULL, 1,    1,    '',            0,  'arabic'),
		  (11, 'Передние листы', 'Автор', 'f', 1, 'front_matter', 10,   NULL, NULL, '',            -1, 'roman'),
		  (20, 'Том 2',          'Автор', 'f', 1, 'volume',       NULL, 1,    2,    'Что делать?', 10, 'arabic'),
		  (30, 'Том Б',          'Другой','f', 1, 'volume',       NULL, 2,    1,    '',            0,  'arabic'),
		  (40, 'Том В',          'Третий','f', 1, 'volume',       NULL, NULL, NULL, '',            0,  'arabic');
		INSERT INTO chapters (id, work_id, title, type, order_number, start_page, end_page, is_apparatus)
		VALUES
		  (100, 10, 'Что делать?',        'chapter', 1, 1,  3,  false),
		  (101, 10, 'Примечания',         'chapter', 2, 4,  5,  true),
		  (102, 10, 'Гегель в примечаниях','chapter', 3, 4,  5,  true),
		  (200, 20, 'О Гегеле',           'chapter', 1, 1,  2,  false),
		  (300, 40, 'Внешняя глава',      'chapter', 1, 1,  10, false),
		  (301, 40, 'Внутренняя глава',   'chapter', 2, 3,  3,  false);
		INSERT INTO pages (work_id, page_number, content_markdown) VALUES
		  (10, 1, 'Ёлка стояла в лесу. Что делать?'),
		  (10, 2, '**Гегеля** читали все[^1]'),
		  (10, 3, 'Про партию'),
		  (10, 4, 'Примечание про Гегеля'),
		  (10, 5, 'ещё раз о ёлке'),
		  (11, 1, 'Гегель на титуле'),
		  (20, 1, 'Гегелем восхищались'),
		  (20, 2, 'ничего'),
		  (30, 1, 'Гегель в собрании Б'),
		  (40, 3, 'Разбор вложенности глав'),
		  -- Четыре полосы с уникальным словом: единственное место фикстуры, где
		  -- совпадений в томе больше тройки, — на нём проверяется потолок и
		  -- отбор «первые по порядку чтения». Слово не встречается больше нигде,
		  -- поэтому счётчики прочих тестов оно не трогает.
		  (40, 1, 'Полоса про многополосность'),
		  (40, 2, 'Ещё многополосность'),
		  (40, 4, 'Опять многополосность'),
		  (40, 5, 'И снова многополосность');
		-- Понятие каталога больше не хранит work_id/kind (миграция 000026,
		-- задача 11) — это свойства статьи (index_concept_articles), заводим
		-- статью для каждого понятия отдельным INSERT'ом, привязанной к тому
		-- же тому, каким она была раньше как понятие. У «Народничества»
		-- work_id — служебная работа (11, Передние листы) без своего
		-- edition_id: article.edition_id всё равно обязан быть НЕ NULL,
		-- поэтому берём собрание родителя (1) — так и наследование edition_id
		-- проверяет TestSearchRepository_ConceptInheritsEditionFromParent.
		WITH c AS (
		  INSERT INTO index_concepts (title, slug, sort_key, title_key) VALUES
		    ('Гегель, Георг', 'gegel',          'гегель, георг', 'гегель, георг'),
		    ('Партия',        'partiya',        'партия',        'партия'),
		    ('Народничество', 'narodnichestvo', 'народничество', 'народничество'),
		    ('Кооперация',    'kooperaciya',    'кооперация',    'кооперация')
		  RETURNING id, slug
		)
		INSERT INTO index_concept_articles (concept_id, edition_id, work_id, title, title_key, kind)
		SELECT c.id, a.edition_id, a.work_id, a.title, a.title_key, 'article'
		FROM c JOIN (VALUES
		    ('gegel',          1::bigint, 10::bigint,   'Гегель, Георг', 'гегель, георг'),
		    ('partiya',        2::bigint, 30::bigint,   'Партия',        'партия'),
		    ('narodnichestvo', 1::bigint, 11::bigint,   'Народничество', 'народничество'),
		    -- Статья ВНЕШНЕГО указателя: собрание есть, работы-указателя нет.
		    -- Так лягут все 2819 статей ленинского указателя — Справочного тома
		    -- в корпусе нет и не будет.
		    ('kooperaciya',    1::bigint, NULL::bigint, 'Кооперация',    'кооперация')
		  ) AS a(slug, edition_id, work_id, title, title_key) ON a.slug = c.slug`); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return NewSearchRepository(pool), ctx
}

func TestSearchRepository_Terms(t *testing.T) {
	repo, ctx := newSearchTestRepo(t)
	cases := map[string][]string{
		"Гегеля":             {"гегел"},
		"партия -меньшевики": {"парт"},
		"...":                nil,
	}
	for in, want := range cases {
		got, err := repo.Terms(ctx, models.SearchQuery{Text: in})
		if err != nil {
			t.Fatalf("Terms(%q): %v", in, err)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("Terms(%q) = %v, want %v", in, got, want)
		}
		if got == nil {
			t.Errorf("Terms(%q) = nil, want a non-nil (possibly empty) slice for JSON []", in)
		}
	}
}

func TestSearchRepository_SearchVolumesAndCatalog(t *testing.T) {
	repo, ctx := newSearchTestRepo(t)
	res, err := repo.Search(ctx, models.SearchQuery{Text: "Гегеля"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if strings.Join(res.Terms, ",") != "гегел" {
		t.Errorf("terms = %v", res.Terms)
	}
	// Морфология: «Гегеля» находит Гегеля, Гегель, Гегелем. Порядок — по
	// text_hits, при равенстве полка: том 1, его передние листы, том 2, том Б.
	var ids []int64
	for _, v := range res.Volumes {
		ids = append(ids, v.WorkID)
	}
	if want := []int64{10, 11, 20, 30}; !equalIDs(ids, want) {
		t.Fatalf("volumes = %v, want %v", ids, want)
	}
	v10 := res.Volumes[0]
	if v10.TextHits != 1 || v10.ApparatusHits != 1 {
		t.Errorf("том 1: text=%d apparatus=%d, want 1/1", v10.TextHits, v10.ApparatusHits)
	}
	if v10.VolumeLabel != "т. 1" || v10.EditionTitle != "Собрание А" {
		t.Errorf("том 1: label=%q edition=%q", v10.VolumeLabel, v10.EditionTitle)
	}
	v11 := res.Volumes[1]
	if v11.Role != "front_matter" || v11.ParentWorkID == nil || *v11.ParentWorkID != 10 ||
		v11.VolumeLabel != "т. 1" || v11.EditionID == nil || *v11.EditionID != 1 {
		t.Errorf("передние листы наследуют том и собрание родителя: %+v", v11)
	}
	if res.Volumes[2].VolumeLabel != "Что делать?" {
		t.Errorf("ручная подпись бьёт номер: %q", res.Volumes[2].VolumeLabel)
	}
	// total_hits — заголовок «В тексте — N полос» над строками томов, каждая
	// из которых печатает свой text_hits: заголовок обязан быть суммой ровно
	// этих чисел. Аппарат в него не входит — он назван в строке отдельно
	// («, N в аппарате»). Тут: 1 (том 1) + 1 (передние листы) + 1 (том 2) +
	// 1 (том Б) = 4, при том что полос с совпадением всего 5 — пятая в
	// аппарате тома 1.
	sum := 0
	for _, v := range res.Volumes {
		sum += v.TextHits
	}
	if res.TotalHits != sum {
		t.Errorf("total_hits = %d, а строки томов дают в тексте %d", res.TotalHits, sum)
	}
	if res.TotalHits != 4 {
		t.Errorf("total_hits = %d, want 4", res.TotalHits)
	}
	// Среди совпавших глав неаппаратные идут первыми (ORDER BY c.is_apparatus):
	// глава 200 («О Гегеле») — не аппарат, глава 102 («Гегель в примечаниях») — аппарат.
	if len(res.Chapters) != 2 || res.Chapters[0].ID != 200 || res.Chapters[0].WorkTitle != "Том 2" ||
		res.Chapters[0].VolumeLabel != "Что делать?" || res.Chapters[0].IsApparatus ||
		res.Chapters[1].ID != 102 || !res.Chapters[1].IsApparatus {
		t.Errorf("chapters = %+v", res.Chapters)
	}
	if len(res.Concepts) != 1 || res.Concepts[0].Slug != "gegel" {
		t.Errorf("concepts = %+v", res.Concepts)
	}
}

// Понятие, привязанное к служебной работе (role = front_matter), у себя не
// хранит edition_id — оно наследуется от родительского тома. Все три
// каталожных запроса (главы, понятия, тома) обязаны считать это правило
// одинаково; до фикса эта проверка проваливалась (0 совпадений) на
// searchConceptsQuery, которая фильтровала по голому w.edition_id.
func TestSearchRepository_ConceptInheritsEditionFromParent(t *testing.T) {
	repo, ctx := newSearchTestRepo(t)
	edition := int64(1)
	res, err := repo.Search(ctx, models.SearchQuery{Text: "Народничество", EditionIDs: []int64{edition}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Concepts) != 1 || res.Concepts[0].Slug != "narodnichestvo" {
		t.Errorf("понятие служебной работы не наследует собрание родителя: %+v", res.Concepts)
	}
}

// То же правило, что и выше, — но на томах, а не на понятиях: searchVolumesQuery
// и searchVolumePagesQuery делят с searchConceptsQuery один и тот же
// scopeCondition (шаблон scopeConditionFor), и обязаны наследовать собрание
// служебной работы у родителя одинаково с ней. Без этого теста подмена
// editionExpr в scopeCondition (например, снятие запасного p.edition_id) не
// ловится ни одним прогоном: ConceptInheritsEditionFromParent проверяет
// только Concepts, которые формулируют область отдельным вызовом
// scopeConditionFor, а не общим scopeCondition.
func TestSearchRepository_VolumeInheritsEditionFromParent(t *testing.T) {
	repo, ctx := newSearchTestRepo(t)
	res, err := repo.Search(ctx, models.SearchQuery{Text: "Гегеля", EditionIDs: []int64{1}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var foundFrontMatter bool
	for _, v := range res.Volumes {
		if v.WorkID == 30 {
			t.Errorf("чужое собрание просочилось: %+v", v)
		}
		if v.WorkID == 11 {
			foundFrontMatter = true
		}
	}
	if !foundFrontMatter {
		t.Errorf("передние листы (без своего edition_id) не наследуют собрание родителя: %+v", res.Volumes)
	}
}

func TestSearchRepository_EditionFilterCutsEverything(t *testing.T) {
	repo, ctx := newSearchTestRepo(t)
	edition := int64(2)
	res, err := repo.Search(ctx, models.SearchQuery{Text: "Гегеля", EditionIDs: []int64{edition}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Volumes) != 1 || res.Volumes[0].WorkID != 30 || res.TotalHits != 1 {
		t.Errorf("volumes = %+v total=%d", res.Volumes, res.TotalHits)
	}
	if len(res.Chapters) != 0 || len(res.Concepts) != 0 {
		t.Errorf("каталог чужого собрания просочился: %+v %+v", res.Chapters, res.Concepts)
	}
}

// Понятие, чья статья не привязана к работе-указателю, обязано находиться
// каталожным поиском — и без области, и в области своего собрания. До правки
// запрос понятий соединялся с works ВНУТРЕННИМ соединением по
// index_concept_articles.work_id, а ввоз это поле не проставляет никогда: из
// выдачи выпадало бы первое же издание, ввезённое после ветки 1, — ленинское
// в том числе.
func TestSearchRepository_ConceptWithoutIndexWorkIsFound(t *testing.T) {
	repo, ctx := newSearchTestRepo(t)

	for _, q := range []models.SearchQuery{
		{Text: "Кооперация"},
		{Text: "Кооперация", EditionIDs: []int64{1}},
	} {
		res, err := repo.Search(ctx, q)
		if err != nil {
			t.Fatalf("Search(%+v): %v", q, err)
		}
		if len(res.Concepts) != 1 || res.Concepts[0].Slug != "kooperaciya" {
			t.Errorf("Search(%+v): понятие без работы-указателя не найдено: %+v",
				q, res.Concepts)
		}
	}

	// Чужое собрание его не показывает — область продолжает работать.
	res, err := repo.Search(ctx, models.SearchQuery{Text: "Кооперация", EditionIDs: []int64{2}})
	if err != nil {
		t.Fatalf("Search(собрание 2): %v", err)
	}
	if len(res.Concepts) != 0 {
		t.Errorf("понятие чужого собрания просочилось: %+v", res.Concepts)
	}
}

// Названная спекой граница, а не дефект: область по ТОМУ статью без работы не
// ловит. Тянуть том через index_references.volume_number значило бы резолвить
// адреса на каждый поисковый запрос. Тест держит границу на месте, чтобы она
// не уехала молча в любую сторону.
func TestSearchRepository_ConceptWithoutWorkIsOutsideWorkScope(t *testing.T) {
	repo, ctx := newSearchTestRepo(t)
	res, err := repo.Search(ctx, models.SearchQuery{Text: "Кооперация", WorkIDs: []int64{10}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Concepts) != 0 {
		t.Errorf("область по тому неожиданно поймала статью без работы: %+v", res.Concepts)
	}
}

func TestSearchRepository_PhraseWithoutStopWords(t *testing.T) {
	repo, ctx := newSearchTestRepo(t)
	res, err := repo.Search(ctx, models.SearchQuery{Text: `"что делать"`})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Volumes) != 1 || res.Volumes[0].WorkID != 10 || res.Volumes[0].TextHits != 1 {
		t.Errorf("фраза из стоп-слов не нашлась в тексте: %+v", res.Volumes)
	}
	if len(res.Chapters) != 1 || res.Chapters[0].ID != 100 {
		t.Errorf("фраза из стоп-слов не нашлась в главах: %+v", res.Chapters)
	}
}

func TestSearchRepository_YoEqualsYe(t *testing.T) {
	repo, ctx := newSearchTestRepo(t)
	for _, q := range []string{"елка", "ёлка"} {
		res, err := repo.Search(ctx, models.SearchQuery{Text: q})
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		if len(res.Volumes) != 1 || res.Volumes[0].TextHits != 1 || res.Volumes[0].ApparatusHits != 1 {
			t.Errorf("Search(%q): volumes = %+v, want том 1 text=1 apparatus=1", q, res.Volumes)
		}
	}
}

func TestSearchRepository_EmptyQueryHasNoTerms(t *testing.T) {
	repo, ctx := newSearchTestRepo(t)
	res, err := repo.Search(ctx, models.SearchQuery{Text: "..."})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Terms) != 0 || len(res.Volumes) != 0 {
		t.Errorf("пустой разбор должен дать пустой ответ: %+v", res)
	}
	if res.Terms == nil {
		t.Errorf("Terms == nil, want a non-nil empty slice (JSON [] not null)")
	}
}

func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSearchRepository_PagesInReadingOrderWithSnippets(t *testing.T) {
	repo, ctx := newSearchTestRepo(t)
	res, err := repo.SearchPages(ctx, models.SearchQuery{Text: "Гегеля"}, 10, nil, 50, 0)
	if err != nil {
		t.Fatalf("SearchPages: %v", err)
	}
	if res.Total != 2 || len(res.Pages) != 2 {
		t.Fatalf("total=%d pages=%d, want 2/2", res.Total, len(res.Pages))
	}
	p2, p4 := res.Pages[0], res.Pages[1]
	if p2.PageNumber != 2 || p4.PageNumber != 4 {
		t.Errorf("порядок чтения нарушен: %d, %d", p2.PageNumber, p4.PageNumber)
	}
	if p2.ChapterTitle == nil || *p2.ChapterTitle != "Что делать?" || p2.IsApparatus {
		t.Errorf("полоса 2: chapter=%v apparatus=%v", p2.ChapterTitle, p2.IsApparatus)
	}
	if p4.ChapterTitle == nil || *p4.ChapterTitle != "Примечания" || !p4.IsApparatus {
		t.Errorf("полоса 4: chapter=%v apparatus=%v", p4.ChapterTitle, p4.IsApparatus)
	}
	if p2.ChapterID == nil || *p2.ChapterID != 100 || p4.ChapterID == nil || *p4.ChapterID != 101 {
		t.Errorf("chapter_id полос 2 и 4 = %v, %v, want 100 и 101", p2.ChapterID, p4.ChapterID)
	}
	// Метки совпадения — управляющие символы; маркеры markdown сняты.
	if !strings.Contains(p2.Snippet, "\x01Гегеля\x02") || strings.Contains(p2.Snippet, "*") ||
		strings.Contains(p2.Snippet, "[^1]") {
		t.Errorf("snippet = %q", p2.Snippet)
	}
	if strings.Join(res.Terms, ",") != "гегел" {
		t.Errorf("terms = %v", res.Terms)
	}
}

func TestSearchRepository_PagesLimitOffsetAndPrintedNumber(t *testing.T) {
	repo, ctx := newSearchTestRepo(t)
	res, err := repo.SearchPages(ctx, models.SearchQuery{Text: "Гегеля"}, 10, nil, 1, 1)
	if err != nil {
		t.Fatalf("SearchPages: %v", err)
	}
	if res.Total != 2 || len(res.Pages) != 1 || res.Pages[0].PageNumber != 4 {
		t.Errorf("limit/offset: total=%d pages=%+v", res.Total, res.Pages)
	}
	// Том 2 имеет page_offset 10: печатный номер = 1 + 10.
	res, err = repo.SearchPages(ctx, models.SearchQuery{Text: "Гегеля"}, 20, nil, 50, 0)
	if err != nil {
		t.Fatalf("SearchPages: %v", err)
	}
	if len(res.Pages) != 1 || res.Pages[0].PrintedNumber != 11 {
		t.Errorf("printed_number = %+v, want 11", res.Pages)
	}
}

func TestSearchRepository_PagesHighlightYoInOriginalText(t *testing.T) {
	repo, ctx := newSearchTestRepo(t)
	res, err := repo.SearchPages(ctx, models.SearchQuery{Text: "елка"}, 10, nil, 50, 0)
	if err != nil {
		t.Fatalf("SearchPages: %v", err)
	}
	if len(res.Pages) != 2 || !strings.Contains(res.Pages[0].Snippet, "\x01Ёлка\x02") {
		t.Errorf("буква автора подменена или совпадение не помечено: %+v", res.Pages)
	}
}

// Проверяет само правило «chapter_title — самая глубокая (наименьшая по
// диапазону) глава»: глава 301 («Внутренняя глава», [3,3]) вложена строго
// внутри главы 300 («Внешняя глава», [1,10]) над той же полосой 3 тома 40 —
// в отличие от глав 101/102 в фикстуре, чьи диапазоны совпадают буквально
// и решаются исключительно тай-брейком по id, эта пара имеет РАЗНУЮ ширину,
// поэтому проверяет именно сравнение по ширине, а не только тай-брейк.
func TestSearchRepository_PagesChapterTitleUsesDeepestNestedChapter(t *testing.T) {
	repo, ctx := newSearchTestRepo(t)
	res, err := repo.SearchPages(ctx, models.SearchQuery{Text: "вложенности"}, 40, nil, 50, 0)
	if err != nil {
		t.Fatalf("SearchPages: %v", err)
	}
	if len(res.Pages) != 1 {
		t.Fatalf("pages = %+v, want 1", res.Pages)
	}
	p := res.Pages[0]
	if p.ChapterTitle == nil || *p.ChapterTitle != "Внутренняя глава" {
		t.Errorf("chapter_title = %v, want узкую вложенную главу «Внутренняя глава», а не внешнюю", p.ChapterTitle)
	}
	// Заголовок и id обязаны приехать из ОДНОЙ выбранной главы: по ним выдача
	// строит ссылку, и разъехавшаяся пара увела бы читателя в чужую главу.
	if p.ChapterID == nil || *p.ChapterID != 301 {
		t.Errorf("chapter_id = %v, want 301 — id той же вложенной главы", p.ChapterID)
	}
	if p.IsApparatus {
		t.Errorf("полоса не в аппарате: is_apparatus = %v", p.IsApparatus)
	}
}

func TestSearchRepository_PagesUnknownWorkIsEmptyNotNil(t *testing.T) {
	repo, ctx := newSearchTestRepo(t)
	res, err := repo.SearchPages(ctx, models.SearchQuery{Text: "Гегеля"}, 999, nil, 50, 0)
	if err != nil {
		t.Fatalf("SearchPages: %v", err)
	}
	if res.Total != 0 || res.Pages == nil || len(res.Pages) != 0 {
		t.Errorf("want total 0 and empty non-nil pages, got %+v", res)
	}
}

// printed_number — сырая арифметика page_number + page_offset, и только она:
// римскую форму и «колонцифры нет» считает клиент общей для всей читальни
// printedFolio (frontend/src/utils/folio.ts), потому что numbering_style —
// свойство работы, а не полосы. Передние листы (работа 11) нумеруются
// римскими от ненумерованной обложки: page_offset −1, и полоса 1 даёт
// печатный 0 — то есть «б/н», а не «с. 0». Единственным офсетом фикстуры был
// +10, у которого сырое число и печатная колонцифра совпадают по форме, и
// расхождение было незаметно.
func TestSearchRepository_PagesPrintedNumberIsRawOffsetArithmetic(t *testing.T) {
	repo, ctx := newSearchTestRepo(t)
	res, err := repo.SearchPages(ctx, models.SearchQuery{Text: "Гегеля"}, 11, nil, 50, 0)
	if err != nil {
		t.Fatalf("SearchPages: %v", err)
	}
	if len(res.Pages) != 1 {
		t.Fatalf("pages = %+v, want 1", res.Pages)
	}
	p := res.Pages[0]
	if p.PageNumber != 1 || p.PrintedNumber != 0 {
		t.Errorf("page_number=%d printed_number=%d, want 1/0 (1 + (-1))", p.PageNumber, p.PrintedNumber)
	}
}

// Первый экран показывает не только счёт по томам, но и по нескольку полос с
// отрывками — той же формы SearchPage, что и выдача внутри тома.
func TestSearchRepository_VolumesCarryPagesWithSnippets(t *testing.T) {
	repo, ctx := newSearchTestRepo(t)
	res, err := repo.Search(ctx, models.SearchQuery{Text: "Гегеля"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	byWork := map[int64]models.SearchVolume{}
	for _, v := range res.Volumes {
		byWork[v.WorkID] = v
		if v.Pages == nil {
			t.Errorf("том %d: pages == nil, want непустой срез (JSON [] вместо null)", v.WorkID)
		}
	}
	// Том 1: две совпавшие полосы — 2 (текст) и 4 (аппарат), в порядке чтения.
	v10 := byWork[10]
	if len(v10.Pages) != 2 || v10.Pages[0].PageNumber != 2 || v10.Pages[1].PageNumber != 4 {
		t.Fatalf("том 1: pages = %+v, want полосы 2 и 4 в порядке чтения", v10.Pages)
	}
	p2, p4 := v10.Pages[0], v10.Pages[1]
	// Отрывок, глава и признак аппарата обязаны совпадать с тем, что отдаёт
	// SearchPages по тому же тому: читатель видит одни и те же полосы до и
	// после перехода в том, и разъехаться им нельзя.
	if !strings.Contains(p2.Snippet, "\x01Гегеля\x02") || strings.Contains(p2.Snippet, "*") {
		t.Errorf("snippet полосы 2 = %q", p2.Snippet)
	}
	if p2.ChapterID == nil || *p2.ChapterID != 100 || p2.IsApparatus {
		t.Errorf("полоса 2: chapter_id=%v apparatus=%v, want 100/false", p2.ChapterID, p2.IsApparatus)
	}
	if p4.ChapterTitle == nil || *p4.ChapterTitle != "Примечания" || !p4.IsApparatus {
		t.Errorf("полоса 4: chapter=%v apparatus=%v, want «Примечания»/true", p4.ChapterTitle, p4.IsApparatus)
	}
	// Передние листы: printed_number — сырая арифметика (1 + (-1) = 0), а
	// numbering_style едет рядом, чтобы клиент напечатал «б/н», а не «с. 0».
	v11 := byWork[11]
	if len(v11.Pages) != 1 || v11.Pages[0].PrintedNumber != 0 {
		t.Fatalf("передние листы: pages = %+v, want одну полосу с printed_number 0", v11.Pages)
	}
	if v11.NumberingStyle != "roman" || byWork[10].NumberingStyle != "arabic" {
		t.Errorf("numbering_style: передние листы %q, том 1 %q", v11.NumberingStyle, byWork[10].NumberingStyle)
	}
	// Полосы чужого тома в список не попадают.
	if len(byWork[20].Pages) != 1 || byWork[20].Pages[0].PageNumber != 1 {
		t.Errorf("том 2: pages = %+v, want только свою полосу 1", byWork[20].Pages)
	}
}

// Потолок: сколько бы полос ни совпало, в списке томов их не больше трёх, и
// это первые три по порядку чтения — продолжение по ссылке «все N полос»
// читается как продолжение, а не как другой список.
func TestSearchRepository_VolumePagesCappedAtThreeInReadingOrder(t *testing.T) {
	repo, ctx := newSearchTestRepo(t)
	res, err := repo.Search(ctx, models.SearchQuery{Text: "многополосность"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Volumes) != 1 || res.Volumes[0].WorkID != 40 {
		t.Fatalf("volumes = %+v, want только том В", res.Volumes)
	}
	v := res.Volumes[0]
	if v.TextHits != 4 {
		t.Errorf("text_hits = %d, want 4 — счёт по тому не режется потолком показа", v.TextHits)
	}
	var nums []int
	for _, p := range v.Pages {
		nums = append(nums, p.PageNumber)
	}
	if len(nums) != 3 || nums[0] != 1 || nums[1] != 2 || nums[2] != 4 {
		t.Errorf("pages = %v, want первые три по порядку чтения: 1, 2, 4", nums)
	}
}

// Фильтр собрания режет и полосы: считать отрывки для тома, которого в выдаче
// не будет, — впустую прочитанный content_markdown, а это вся цена запроса.
func TestSearchRepository_VolumePagesRespectEditionFilter(t *testing.T) {
	repo, ctx := newSearchTestRepo(t)
	edition := int64(2)
	res, err := repo.Search(ctx, models.SearchQuery{Text: "Гегеля", EditionIDs: []int64{edition}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Volumes) != 1 || res.Volumes[0].WorkID != 30 {
		t.Fatalf("volumes = %+v", res.Volumes)
	}
	if len(res.Volumes[0].Pages) != 1 || res.Volumes[0].Pages[0].PageNumber != 1 {
		t.Errorf("pages = %+v, want одну полосу тома Б", res.Volumes[0].Pages)
	}
}

// Область из списка томов (не собраний): без области находятся оба тома,
// с областью из одного — только он. Проверяет само условие scopeCondition, а
// не мост editionFilter задачи 1, который такую область принять не мог.
func TestSearchScopeFiltersVolumes(t *testing.T) {
	pool := testPool(t) // существующий помощник пакета; пропускает тест без PROOFREADER_TEST_DB_URL
	repo := NewSearchRepository(pool)
	ctx := context.Background()

	// Фикстура: два тома в разных собраниях, слово есть в обоих.
	a := seedWorkWithPages(t, pool, "Том А", "прибавочная стоимость")
	b := seedWorkWithPages(t, pool, "Том Б", "прибавочная стоимость")

	all, err := repo.Search(ctx, models.SearchQuery{Text: "прибавочная"})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Volumes) < 2 {
		t.Fatalf("без области ожидались оба тома, пришло %d", len(all.Volumes))
	}

	only, err := repo.Search(ctx, models.SearchQuery{Text: "прибавочная", WorkIDs: []int64{a}})
	if err != nil {
		t.Fatal(err)
	}
	if len(only.Volumes) != 1 || only.Volumes[0].WorkID != a {
		t.Fatalf("область из одного тома дала %d томов: %+v", len(only.Volumes), only.Volumes)
	}
	_ = b
}

// Область «этот том» обязана включать его передние листы: читатель, отметивший
// том, ждёт от него титул, содержание и предисловие внутри области.
func TestSearchScopeIncludesFrontMatterChildren(t *testing.T) {
	pool := testPool(t)
	repo := NewSearchRepository(pool)
	ctx := context.Background()

	parent := seedWorkWithPages(t, pool, "Том с передними листами", "ничего")
	child := seedChildWorkWithPages(t, pool, parent, "Передние листы", "предисловие редакции")

	res, err := repo.Search(ctx, models.SearchQuery{Text: "предисловие", WorkIDs: []int64{parent}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range res.Volumes {
		if v.WorkID == child {
			found = true
		}
	}
	if !found {
		t.Fatal("передние листы тома не попали в область «этот том»")
	}
}

// Список глав сужает выдачу внутри тома, и условие обязано стоять и в
// выдаче, и в счётчике одновременно — иначе «Ещё» разъезжается с итогом.
func TestSearchPagesNarrowsToChapters(t *testing.T) {
	pool := testPool(t)
	repo := NewSearchRepository(pool)
	ctx := context.Background()

	work := seedWorkWithPages(t, pool, "Том", "партия", "партия", "партия")
	first := seedChapter(t, pool, work, "Первая", 1, 1)
	_ = seedChapter(t, pool, work, "Вторая", 2, 3)

	all, err := repo.SearchPages(ctx, models.SearchQuery{Text: "партия"}, work, nil, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != 3 {
		t.Fatalf("без глав total = %d, ожидалось 3", all.Total)
	}

	one, err := repo.SearchPages(ctx, models.SearchQuery{Text: "партия"}, work, []int64{first}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if one.Total != 1 || len(one.Pages) != 1 {
		t.Fatalf("глава дала total=%d pages=%d, ожидалось 1 и 1", one.Total, len(one.Pages))
	}
}

// Глава чужого тома, подставленная в адрес руками, не расширяет область:
// c.work_id = pg.work_id в chapterCondition отсекает её.
func TestSearchPagesIgnoresForeignChapter(t *testing.T) {
	pool := testPool(t)
	repo := NewSearchRepository(pool)
	ctx := context.Background()

	mine := seedWorkWithPages(t, pool, "Мой том", "партия")
	other := seedWorkWithPages(t, pool, "Чужой том", "партия")
	foreign := seedChapter(t, pool, other, "Чужая глава", 1, 1)

	res, err := repo.SearchPages(ctx, models.SearchQuery{Text: "партия"}, mine, []int64{foreign}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 0 || len(res.Pages) != 0 {
		t.Fatalf("чужая глава расширила область: total=%d pages=%d", res.Total, len(res.Pages))
	}
}

func TestSearchPagesFacetCountsWholeVolume(t *testing.T) {
	pool := testPool(t)
	repo := NewSearchRepository(pool)
	ctx := context.Background()

	work := seedWorkWithPages(t, pool, "Том", "партия", "партия", "партия")
	first := seedChapter(t, pool, work, "Первая", 1, 1)
	second := seedChapter(t, pool, work, "Вторая", 2, 3)

	// Фасет считается по всему тому, а не по окну и не по уже выбранной главе.
	res, err := repo.SearchPages(ctx, models.SearchQuery{Text: "партия"}, work, []int64{first}, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Chapters) != 2 {
		t.Fatalf("фасет = %+v, ожидались обе главы", res.Chapters)
	}
	byID := map[int64]int{}
	for _, c := range res.Chapters {
		byID[c.ID] = c.Hits
	}
	if byID[first] != 1 || byID[second] != 2 {
		t.Fatalf("счёт попаданий неверен: %+v", res.Chapters)
	}
	if res.Chapters[0].ID != second {
		t.Fatalf("порядок не по убыванию попаданий: %+v", res.Chapters)
	}
}

// Находка 4 итоговой рецензии: фасет — свойство ТОМА, а не окна полос
// (см. TestSearchPagesFacetCountsWholeVolume), поэтому на «Ещё» (offset > 0)
// его не нужно пересчитывать заново — клиент всё равно не читает эти поля из
// повторного ответа (loadMore в Search.tsx подменяет только pages). Тест
// проверяет именно это: тот же запрос с offset=0 против offset>0 — фасет
// пуст и total равен нулю ровно на второй странице, а не «фасет вообще не
// работает».
func TestSearchPagesFacetSkippedOnSubsequentPage(t *testing.T) {
	pool := testPool(t)
	repo := NewSearchRepository(pool)
	ctx := context.Background()

	work := seedWorkWithPages(t, pool, "Том", "партия", "партия", "партия")
	seedChapter(t, pool, work, "Первая", 1, 1)
	seedChapter(t, pool, work, "Вторая", 2, 3)

	first, err := repo.SearchPages(ctx, models.SearchQuery{Text: "партия"}, work, nil, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Chapters) != 2 {
		t.Fatalf("первая страница: фасет = %+v, ожидались обе главы", first.Chapters)
	}
	if first.ChaptersTotal != 2 {
		t.Fatalf("первая страница: chapters_total = %d, ожидалось 2", first.ChaptersTotal)
	}

	second, err := repo.SearchPages(ctx, models.SearchQuery{Text: "партия"}, work, nil, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Chapters) != 0 {
		t.Fatalf("вторая страница: фасет = %+v, ожидался пустой (не пересчитан)", second.Chapters)
	}
	if second.ChaptersTotal != 0 {
		t.Fatalf("вторая страница: chapters_total = %d, ожидалось 0", second.ChaptersTotal)
	}
	// Список полос второй страницы фасетом не заменяется — сам поиск
	// продолжает работать постранично, пропуск касается только фасета.
	if second.Total != first.Total || len(second.Pages) != 1 {
		t.Fatalf("вторая страница полос сломана: %+v", second)
	}
}

func TestSearchPagesFacetMatchesPageAttribution(t *testing.T) {
	pool := testPool(t)
	repo := NewSearchRepository(pool)
	ctx := context.Background()

	// Вложенные главы с общим краем: фасет и подпись полосы обязаны выбрать
	// одну и ту же — самую узкую накрывающую.
	work := seedWorkWithPages(t, pool, "Том", "партия")
	_ = seedChapter(t, pool, work, "Широкая", 1, 10)
	narrow := seedChapter(t, pool, work, "Узкая", 1, 1)

	res, err := repo.SearchPages(ctx, models.SearchQuery{Text: "партия"}, work, nil, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 1 || res.Pages[0].ChapterID == nil || *res.Pages[0].ChapterID != narrow {
		t.Fatalf("подпись полосы: %+v", res.Pages)
	}
	if len(res.Chapters) != 1 || res.Chapters[0].ID != narrow {
		t.Fatalf("фасет разошёлся с подписью полосы: %+v", res.Chapters)
	}
}
