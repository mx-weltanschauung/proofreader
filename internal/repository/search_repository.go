package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
	"proofreader/pkg/slug"
)

// ErrSearchTimeout — запрос упёрся в statement_timeout. Обработчик отвечает
// на него 503, а не 500: читателю есть что сделать — уточнить запрос.
var ErrSearchTimeout = errors.New("search timed out")

// searchStatementTimeout — потолок одного поискового SQL. Боевой сервер —
// два ядра; одно частое слово не должно занимать их надолго.
const searchStatementTimeout = "5s"

const catalogLimit = 20

// volumePagesPreview — сколько полос с отрывками едет в строке тома на первом
// экране. Цена запроса — ts_headline, читающий content_markdown каждой
// показанной полосы: на частом слове это число, умноженное на количество
// томов корпуса (127), и растить его дальше без замера нельзя.
const volumePagesPreview = 3

// idList — nil-срез в пустой: см. оговорку про cardinality(NULL) у
// scopeCondition.
func idList(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}
	return ids
}

// SearchRepository — полнотекстовый поиск на Postgres FTS (конфиг ru,
// миграция 000016). Единственная реализация api.SearchStore; замена движка
// меняет этот файл и ничего больше.
type SearchRepository struct {
	pool *pgxpool.Pool
}

// NewSearchRepository creates a new search repository.
func NewSearchRepository(pool *pgxpool.Pool) *SearchRepository {
	return &SearchRepository{pool: pool}
}

// begin открывает транзакцию с локальным statement_timeout: SET LOCAL живёт
// только внутри неё и не трогает соединение пула.
func (r *SearchRepository) begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin search transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '"+searchStatementTimeout+"'"); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("failed to set statement_timeout: %w", err)
	}
	return tx, nil
}

// wrapSearchErr переводит отмену по таймауту (SQLSTATE 57014) в сентинел.
func wrapSearchErr(op string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "57014" {
		return fmt.Errorf("%s: %w", op, ErrSearchTimeout)
	}
	return fmt.Errorf("%s: %w", op, err)
}

const termsQuery = `SELECT websearch_to_tsquery('ru', $1)::text`

// termsIn — леммы разобранного запроса. Всегда non-nil: termsFromTSQuery
// возвращает nil на пустом разборе (одни знаки препинания или одни
// исключения), а срезы в JSON-ответах обязаны быть `[]`, не `null` — оба
// вызывающих (Terms и Search) отдают этот срез читателю как есть.
func termsIn(ctx context.Context, tx pgx.Tx, text string) ([]string, error) {
	var tsquery string
	if err := tx.QueryRow(ctx, termsQuery, text).Scan(&tsquery); err != nil {
		return nil, wrapSearchErr("failed to parse search query", err)
	}
	terms := termsFromTSQuery(tsquery)
	if terms == nil {
		terms = []string{}
	}
	return terms, nil
}

// Terms — леммы разобранного запроса без исключённых. Пустой срез означает
// «после разбора ничего не осталось» (одни знаки препинания или одни
// исключения); обработчик отвечает на это 400.
func (r *SearchRepository) Terms(ctx context.Context, q models.SearchQuery) ([]string, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) // откат после Commit безвреден
	terms, err := termsIn(ctx, tx, q.Text)
	if err != nil {
		return nil, err
	}
	return terms, tx.Commit(ctx)
}

// searchWorkSlugColumns — три ингредиента слага тома (см. workSlugColumns,
// work_repository.go), с алиасом w вместо works: и searchChaptersQuery, и
// searchVolumesQuery зовут работу этим алиасом. Подстановкой, а не второй
// рукописной копией — см. workSummariesSlugColumns (edition_repository.go).
var searchWorkSlugColumns = strings.ReplaceAll(workSlugColumns, "works.", "w.")

// scopeConditionFor — область поиска одним выражением на все запросы выдачи.
// $2 — собрания, $3 — тома; оба всегда bigint[], пустые означают «вся
// читальня».
//
// editionExpr — то, чем в конкретном запросе берётся собрание строки. Он
// параметр, а НЕ повод развести правило на две копии: главы, понятия и тома
// обязаны формулировать область одинаково, и подстановка одного выражения —
// тот же приём, каким живёт workSummariesQueryTemplate.
//
// coalesce(cardinality(...), 0), а не голый cardinality: pgx кодирует nil-срез
// как NULL, cardinality(NULL) — NULL, и всё условие становится NULL, то есть
// ложью. Выдача тогда пуста при пустой области — ровно наоборот тому, что
// нужно.
//
// Тома разворачиваются в сам том и его передние листы (parent_work_id):
// читатель, отметивший том, ждёт его титул, содержание и предисловие внутри
// области. Собрание служебной работы по-прежнему берётся у родителя.
func scopeConditionFor(editionExpr string) string {
	return `
	  AND (
	    (coalesce(cardinality($2::bigint[]), 0) = 0
	     AND coalesce(cardinality($3::bigint[]), 0) = 0)
	    OR ` + editionExpr + ` = ANY($2::bigint[])
	    OR w.id = ANY($3::bigint[])
	    OR w.parent_work_id = ANY($3::bigint[])
	  )`
}

// Собрание строки, у которой работа есть всегда: главы, тома, полосы тома.
const workEditionExpr = `coalesce(w.edition_id, p.edition_id)`

var scopeCondition = scopeConditionFor(workEditionExpr)

// Каталог и тома. Собрание служебной работы (передних листов) — у родителя:
// своего edition_id у неё нет, а искать «в собрании» её полосы должно.
var searchChaptersQuery = `
	WITH q AS (SELECT websearch_to_tsquery('ru', $1) AS tsq)
	SELECT c.id, c.title, c.is_apparatus, w.id, w.title,
	       coalesce(nullif(w.shelf_label, ''), p.shelf_label, ''),
	       coalesce(w.volume_number, p.volume_number),
	       coalesce(w.volume_part, p.volume_part),
	       coalesce(e.title, ''),
	       w.role, w.precedes_volume, ` + searchWorkSlugColumns + `
	FROM chapters c
	CROSS JOIN q
	JOIN works w ON w.id = c.work_id
	LEFT JOIN works p ON p.id = w.parent_work_id
	LEFT JOIN editions e ON e.id = coalesce(w.edition_id, p.edition_id)
	WHERE to_tsvector('ru', c.title) @@ q.tsq
	` + scopeCondition + `
	ORDER BY c.is_apparatus, ts_rank(to_tsvector('ru', c.title), q.tsq) DESC, c.id
	LIMIT $4
`

// Как и searchChaptersQuery: понятие служебной работы (front_matter) своего
// edition_id не хранит, собрание — у родителя. Все три каталожных запроса
// (главы, понятия, тома) обязаны формулировать это правило одинаково.
//
// work_id понятия сняла миграция 000026 (задача 11) — это свойство статьи
// (index_concept_articles), не понятия: у понятия бывает несколько статей по
// разным изданиям (каждая — свой work_id, может быть и NULL для источника без
// собственной работы-указателя), поэтому область поиска проверяется через
// EXISTS по каждой статье в отдельности, а не единственным JOIN, — попадания
// хотя бы одной статьи в область достаточно, чтобы понятие вошло в выдачу.
//
// Соединение с works — ЛЕВОЕ, и собрание берётся с запасным a.edition_id:
// у статьи внешнего указателя (ленинского) работы нет вовсе, внутреннее
// соединение выбрасывало её из выдачи целиком, а левое без запасного дало бы
// NULL = ANY(...) — то есть ложь. Область по ТОМУ такую статью по-прежнему не
// ловит, и это названная спекой граница, а не забытый случай.
var searchConceptsQuery = `
	WITH q AS (SELECT websearch_to_tsquery('ru', $1) AS tsq)
	SELECT ic.slug, ic.title
	FROM index_concepts ic
	CROSS JOIN q
	WHERE to_tsvector('ru', ic.title) @@ q.tsq
	  AND EXISTS (
	    SELECT 1
	    FROM index_concept_articles a
	    LEFT JOIN works w ON w.id = a.work_id
	    LEFT JOIN works p ON p.id = w.parent_work_id
	    WHERE a.concept_id = ic.id
	    ` + scopeConditionFor(`coalesce(w.edition_id, p.edition_id, a.edition_id)`) + `
	  )
	ORDER BY ts_rank(to_tsvector('ru', ic.title), q.tsq) DESC, ic.sort_key, ic.id
	LIMIT $4
`

// Аппарат считается на лету по диапазонам глав с is_apparatus: диапазоны —
// в номерах полос, как в GetPageRange. Счёт по томам GIN даёт почти даром;
// ранг не считается нигде, кроме названий (спека, «Отвергнуто»).
var searchVolumesQuery = `
	WITH q AS (SELECT websearch_to_tsquery('ru', $1) AS tsq),
	hits AS (
	  SELECT pg.work_id, pg.page_number
	  FROM pages pg CROSS JOIN q
	  WHERE pg.search_vector @@ q.tsq
	),
	flagged AS (
	  SELECT h.work_id,
	         EXISTS (SELECT 1 FROM chapters c
	                 WHERE c.work_id = h.work_id AND c.is_apparatus
	                   AND h.page_number BETWEEN c.start_page AND c.end_page) AS in_apparatus
	  FROM hits h
	)
	SELECT w.id, w.title, coalesce(w.author, ''),
	       coalesce(nullif(w.shelf_label, ''), p.shelf_label, ''),
	       coalesce(w.volume_number, p.volume_number),
	       coalesce(w.volume_part, p.volume_part),
	       w.role, w.parent_work_id, w.numbering_style,
	       coalesce(w.edition_id, p.edition_id), coalesce(e.title, ''),
	       w.precedes_volume, ` + searchWorkSlugColumns + `,
	       (count(*) FILTER (WHERE NOT f.in_apparatus))::int AS text_hits,
	       (count(*) FILTER (WHERE f.in_apparatus))::int     AS apparatus_hits
	FROM flagged f
	JOIN works w ON w.id = f.work_id
	LEFT JOIN works p ON p.id = w.parent_work_id
	LEFT JOIN editions e ON e.id = coalesce(w.edition_id, p.edition_id)
	WHERE true
	` + scopeCondition + `
	GROUP BY w.id, p.id, e.id
	ORDER BY text_hits DESC, coalesce(w.edition_id, p.edition_id),
	         coalesce(w.volume_number, p.volume_number), w.id
`

// Search — первый экран: каталог (до 20 глав и 20 понятий) и все тома с
// совпадениями. Пустой разбор возвращает результат с пустыми Terms и без
// походов за каталогом.
func (r *SearchRepository) Search(ctx context.Context, q models.SearchQuery) (*models.SearchResult, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	res := &models.SearchResult{
		Query:    q.Text,
		Chapters: []models.SearchChapter{},
		Concepts: []models.SearchConcept{},
		Volumes:  []models.SearchVolume{},
	}
	terms, err := termsIn(ctx, tx, q.Text)
	if err != nil {
		return nil, err
	}
	res.Terms = terms
	if len(terms) == 0 {
		return res, tx.Commit(ctx)
	}

	rows, err := tx.Query(ctx, searchChaptersQuery, q.Text, idList(q.EditionIDs), idList(q.WorkIDs), catalogLimit)
	if err != nil {
		return nil, wrapSearchErr("failed to search chapters", err)
	}
	for rows.Next() {
		var c models.SearchChapter
		var shelf string
		var num *int
		var part *string
		var role string
		var precedesVolume *int
		var slugEdition string
		var slugVolume *int
		var slugPart *string
		if err := rows.Scan(&c.ID, &c.Title, &c.IsApparatus, &c.WorkID, &c.WorkTitle,
			&shelf, &num, &part, &c.EditionTitle,
			&role, &precedesVolume, &slugEdition, &slugVolume, &slugPart); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan chapter hit: %w", err)
		}
		c.VolumeLabel = volumeLabel(shelf, num, part)
		c.Slug = slug.Chapter(c.Title)
		c.WorkSlug = workSlugFrom(role, slugEdition, slugVolume, slugPart, precedesVolume, c.WorkTitle)
		res.Chapters = append(res.Chapters, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, wrapSearchErr("failed to read chapter hits", err)
	}

	rows, err = tx.Query(ctx, searchConceptsQuery, q.Text, idList(q.EditionIDs), idList(q.WorkIDs), catalogLimit)
	if err != nil {
		return nil, wrapSearchErr("failed to search concepts", err)
	}
	for rows.Next() {
		var c models.SearchConcept
		if err := rows.Scan(&c.Slug, &c.Title); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan concept hit: %w", err)
		}
		res.Concepts = append(res.Concepts, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, wrapSearchErr("failed to read concept hits", err)
	}

	rows, err = tx.Query(ctx, searchVolumesQuery, q.Text, idList(q.EditionIDs), idList(q.WorkIDs))
	if err != nil {
		return nil, wrapSearchErr("failed to search volumes", err)
	}
	for rows.Next() {
		var v models.SearchVolume
		var shelf string
		var num *int
		var part *string
		var precedesVolume *int
		var slugEdition string
		var slugVolume *int
		var slugPart *string
		if err := rows.Scan(&v.WorkID, &v.Title, &v.Author, &shelf, &num, &part,
			&v.Role, &v.ParentWorkID, &v.NumberingStyle, &v.EditionID, &v.EditionTitle,
			&precedesVolume, &slugEdition, &slugVolume, &slugPart,
			&v.TextHits, &v.ApparatusHits); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan volume hit: %w", err)
		}
		v.VolumeLabel = volumeLabel(shelf, num, part)
		v.WorkSlug = workSlugFrom(v.Role, slugEdition, slugVolume, slugPart, precedesVolume, v.Title)
		// Полосы приезжают отдельным запросом ниже; до него — пустой непустой
		// срез, потому что клиент зовёт по нему .map, а json пишет nil как null.
		v.Pages = []models.SearchPage{}
		// Только текст: это число печатается заголовком «В тексте — N полос»
		// над строками томов, а строка тома показывает свой text_hits. Пока
		// сюда прибавлялся и аппарат, заголовок обещал на живом корпусе 823
		// полосы над строками, дающими в сумме 736. Аппарат назван в строке
		// отдельно («, N в аппарате») и в заголовок не входит.
		res.TotalHits += v.TextHits
		res.Volumes = append(res.Volumes, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, wrapSearchErr("failed to read volume hits", err)
	}

	if err := r.attachVolumePages(ctx, tx, q, res.Volumes); err != nil {
		return nil, err
	}

	return res, tx.Commit(ctx)
}

// Отрывок строится по оригинальному тексту: unaccent сидит в конфиге, поэтому
// «Ёлка» подсвечивается по запросу «елка», а буквы автора не подменяются.
// Границы — chr(1)/chr(2), разделитель фрагментов chr(3) (cleanSnippet
// превращает его в « … »). HTML в ответе нет.
//
// Колонки и join главы вынесены в константы: одну и ту же SearchPage
// собирают два запроса — полосы одного тома (SearchPages) и тройка полос в
// строке тома на первом экране, — и форма у них обязана быть одна. Порядок
// колонок здесь и порядок чтения в scanPageHit — одно целое.
const pageHitColumns = `pg.page_number,
	       pg.page_number + w.page_offset,
	       ch.title, ch.id,
	       EXISTS (SELECT 1 FROM chapters c
	               WHERE c.work_id = pg.work_id AND c.is_apparatus
	                 AND pg.page_number BETWEEN c.start_page AND c.end_page),
	       ts_headline('ru', pg.content_markdown, q.tsq,
	         'MaxFragments=2, MaxWords=25, MinWords=10, StartSel=' || chr(1)
	         || ', StopSel=' || chr(2) || ', FragmentDelimiter=' || chr(3))`

// Самая глубокая глава, накрывающая полосу, одной выборкой: заголовок и id
// обязаны приехать из ОДНОЙ строки, иначе выдача подпишет главу одним
// названием, а ссылку построит на другую.
const pageHitChapterJoin = `
	LEFT JOIN LATERAL (
	  SELECT c.id, c.title FROM chapters c
	  WHERE c.work_id = pg.work_id AND pg.page_number BETWEEN c.start_page AND c.end_page
	  ORDER BY c.end_page - c.start_page, c.id LIMIT 1
	) ch ON true`

// chapterCondition — сужение до выбранных глав. Пустой список означает «весь
// том», поэтому условие начинается с проверки длины (та же оговорка про
// cardinality(NULL), что и у scopeCondition).
//
// c.work_id = pg.work_id обязателен: он отсекает главу другого тома,
// подставленную в адрес руками. Без него подделанный адрес расширял бы
// область на чужой том.
const chapterCondition = `
	  AND (coalesce(cardinality($3::bigint[]), 0) = 0
	       OR EXISTS (SELECT 1 FROM chapters c
	                  WHERE c.id = ANY($3::bigint[]) AND c.work_id = pg.work_id
	                    AND pg.page_number BETWEEN c.start_page AND c.end_page))`

const searchPagesQuery = `
	WITH q AS (SELECT websearch_to_tsquery('ru', $1) AS tsq)
	SELECT ` + pageHitColumns + `
	FROM pages pg
	CROSS JOIN q
	JOIN works w ON w.id = pg.work_id` + pageHitChapterJoin + `
	WHERE pg.work_id = $2 AND pg.search_vector @@ q.tsq` + chapterCondition + `
	ORDER BY pg.page_number
	LIMIT $4 OFFSET $5
`

// Первые volumePagesPreview полос КАЖДОГО совпавшего тома, одним запросом на
// всю выдачу. Фильтр собрания повторяет searchVolumesQuery: считать отрывки
// для тома, которого в выдаче не будет, — впустую прочитанный
// content_markdown, а это вся цена запроса.
//
// Отдельным запросом, а не веткой агрегата томов: общий с ним скан hits
// стоит 10 мс на самом частом слове корпуса, а слияние двух форм ответа в
// одну строку стоило бы читаемости обоих.
var searchVolumePagesQuery = `
	WITH q AS (SELECT websearch_to_tsquery('ru', $1) AS tsq),
	hits AS (
	  SELECT pg.work_id, pg.page_number
	  FROM pages pg CROSS JOIN q
	  WHERE pg.search_vector @@ q.tsq
	),
	volumes AS (
	  SELECT DISTINCT h.work_id
	  FROM hits h
	  JOIN works w ON w.id = h.work_id
	  LEFT JOIN works p ON p.id = w.parent_work_id
	  WHERE true
	  ` + scopeCondition + `
	),
	top AS (
	  SELECT v.work_id, t.page_number
	  FROM volumes v
	  JOIN LATERAL (
	    SELECT h.page_number FROM hits h
	    WHERE h.work_id = v.work_id
	    ORDER BY h.page_number
	    LIMIT $4
	  ) t ON true
	)
	SELECT pg.work_id, ` + pageHitColumns + `
	FROM top
	CROSS JOIN q
	JOIN pages pg ON pg.work_id = top.work_id AND pg.page_number = top.page_number
	JOIN works w ON w.id = pg.work_id` + pageHitChapterJoin + `
	ORDER BY pg.work_id, pg.page_number
`

// scanPageHit читает полосу в порядке колонок pageHitColumns; before — те
// колонки, что стоят перед ними в конкретном запросе (work_id у выдачи по
// томам).
func scanPageHit(rows pgx.Rows, before ...any) (models.SearchPage, error) {
	var p models.SearchPage
	var raw string
	dest := append(before[:len(before):len(before)],
		&p.PageNumber, &p.PrintedNumber, &p.ChapterTitle, &p.ChapterID, &p.IsApparatus, &raw)
	if err := rows.Scan(dest...); err != nil {
		return p, err
	}
	p.Snippet = cleanSnippet(raw)
	return p, nil
}

// facetLimit — сколько глав едет в фасете. У ленинского тома 660 глав
// верхнего уровня (каждая — письмо на одну-две полосы), и частое слово
// попадает в сотни из них: полный список там и бесполезен, и дорог.
const facetLimit = 30

// Фасет глав тома. Внутренний LATERAL повторяет правило pageHitChapterJoin
// (самая узкая накрывающая глава, при равенстве — меньший id) — это одно и то
// же решение, и разъезд между ним и подписью полосы ловит
// TestSearchPagesFacetMatchesPageAttribution.
//
// count(*) OVER () считается уже после группировки, то есть даёт число ГЛАВ с
// попаданиями, а не число полос: ради него не нужен второй запрос.
//
// Область глав сюда НЕ передаётся намеренно: фасет описывает том целиком.
const searchPagesFacetQuery = `
	WITH q AS (SELECT websearch_to_tsquery('ru', $1) AS tsq),
	hits AS (
	  SELECT pg.page_number
	  FROM pages pg CROSS JOIN q
	  WHERE pg.work_id = $2 AND pg.search_vector @@ q.tsq
	)
	SELECT ch.id, ch.title, count(*)::int AS hits, (count(*) OVER ())::int AS total
	FROM hits h
	JOIN LATERAL (
	  SELECT c.id, c.title FROM chapters c
	  WHERE c.work_id = $2 AND h.page_number BETWEEN c.start_page AND c.end_page
	  ORDER BY c.end_page - c.start_page, c.id LIMIT 1
	) ch ON true
	GROUP BY ch.id, ch.title
	ORDER BY hits DESC, min(h.page_number), ch.id
	LIMIT $3
`

const searchPagesCountQuery = `
	WITH q AS (SELECT websearch_to_tsquery('ru', $1) AS tsq)
	SELECT count(*)::int FROM pages pg CROSS JOIN q
	WHERE pg.work_id = $2 AND pg.search_vector @@ q.tsq` + chapterCondition + `
`

// SearchPages — полосы одного тома в порядке чтения. Неизвестный том — пустой
// результат, не ошибка: 404 тут нечего сообщать читателю.
func (r *SearchRepository) SearchPages(
	ctx context.Context, q models.SearchQuery, workID int64, chapterIDs []int64, limit, offset int,
) (*models.SearchPagesResult, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	res := &models.SearchPagesResult{Query: q.Text, Pages: []models.SearchPage{}}
	terms, err := termsIn(ctx, tx, q.Text)
	if err != nil {
		return nil, err
	}
	res.Terms = terms
	if len(terms) == 0 {
		return res, tx.Commit(ctx)
	}

	if err := tx.QueryRow(ctx, searchPagesCountQuery, q.Text, workID, idList(chapterIDs)).Scan(&res.Total); err != nil {
		return nil, wrapSearchErr("failed to count page hits", err)
	}
	rows, err := tx.Query(ctx, searchPagesQuery, q.Text, workID, idList(chapterIDs), limit, offset)
	if err != nil {
		return nil, wrapSearchErr("failed to search pages", err)
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanPageHit(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan page hit: %w", err)
		}
		res.Pages = append(res.Pages, p)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapSearchErr("failed to read page hits", err)
	}
	rows.Close()

	res.Chapters = []models.SearchChapterFacet{}
	// Находка 4 итоговой рецензии: фасет описывает том ЦЕЛИКОМ (условие
	// запроса — только work_id, без limit/offset), поэтому его значение не
	// меняется от страницы к странице — «Ещё» просит следующее окно ПОЛОС, а
	// не пересчитывает область поиска. Клиент это уже учитывал (loadMore в
	// Search.tsx подменяет только pages, поля фасета из повторного ответа не
	// читает), но сервер честно пересчитывал те же 30 строк на каждый клик
	// «Ещё» — пропуск при offset > 0 не сужает то, ПО ЧЕМУ считается фасет
	// (по-прежнему весь том, а не окно загруженных полос, как требует спека,
	// раздел 5) — он лишь не повторяет один и тот же подсчёт впустую.
	if offset == 0 {
		frows, err := tx.Query(ctx, searchPagesFacetQuery, q.Text, workID, facetLimit)
		if err != nil {
			return nil, wrapSearchErr("failed to build chapter facet", err)
		}
		defer frows.Close()
		for frows.Next() {
			var c models.SearchChapterFacet
			var total int
			if err := frows.Scan(&c.ID, &c.Title, &c.Hits, &total); err != nil {
				return nil, fmt.Errorf("failed to scan chapter facet: %w", err)
			}
			res.ChaptersTotal = total
			res.Chapters = append(res.Chapters, c)
		}
		if err := frows.Err(); err != nil {
			return nil, wrapSearchErr("failed to read chapter facet", err)
		}
		frows.Close()
	}

	return res, tx.Commit(ctx)
}

// attachVolumePages дописывает каждому тому выдачи первые несколько его полос
// с отрывками. Пустая выдача запроса не стоит: без томов показывать нечего.
func (r *SearchRepository) attachVolumePages(
	ctx context.Context, tx pgx.Tx, q models.SearchQuery, volumes []models.SearchVolume,
) error {
	if len(volumes) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, searchVolumePagesQuery, q.Text, idList(q.EditionIDs), idList(q.WorkIDs), volumePagesPreview)
	if err != nil {
		return wrapSearchErr("failed to search volume pages", err)
	}
	defer rows.Close()
	byWork := make(map[int64][]models.SearchPage, len(volumes))
	for rows.Next() {
		var workID int64
		p, err := scanPageHit(rows, &workID)
		if err != nil {
			return fmt.Errorf("failed to scan volume page hit: %w", err)
		}
		byWork[workID] = append(byWork[workID], p)
	}
	if err := rows.Err(); err != nil {
		return wrapSearchErr("failed to read volume page hits", err)
	}
	rows.Close()
	for i := range volumes {
		if pages := byWork[volumes[i].WorkID]; pages != nil {
			volumes[i].Pages = pages
		}
	}
	return nil
}
