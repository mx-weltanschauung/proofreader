package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

// IndexRepository handles subject-index concept data access.
type IndexRepository struct {
	pool *pgxpool.Pool
}

// NewIndexRepository creates a new index repository.
func NewIndexRepository(pool *pgxpool.Pool) *IndexRepository {
	return &IndexRepository{pool: pool}
}

// LinkWithTarget is a link plus the slug of its target, when the target exists.
type LinkWithTarget struct {
	models.IndexConceptLink
	TargetSlug string `json:"target_slug,omitempty"`
}

// pluralizeVyrezok склоняет «вырезка» по числу для предупреждения о потере
// вырезок при переимпорте: 1 вырезку, 2—4 вырезки, остальное (включая
// 11—14) — вырезок.
func pluralizeVyrezok(n int) string {
	if n < 0 {
		n = -n
	}
	mod100 := n % 100
	mod10 := n % 10
	switch {
	case mod100 >= 11 && mod100 <= 14:
		return "вырезок"
	case mod10 == 1:
		return "вырезку"
	case mod10 >= 2 && mod10 <= 4:
		return "вырезки"
	default:
		return "вырезок"
	}
}

// rubricPathOf — путь подрубрик адреса: названия, как они пришли, и их
// нормализованные ключи.
//
// RubricPath непуст → берётся он; иначе путь строится из одноэлементного
// Rubric; иначе адрес висит прямо на статье (пустой путь). Два разборщика
// шлют одно и то же тело, и марксов (parse_index.py) о вложенности не знает —
// плоский случай обязан работать без единой правки на его стороне.
//
// Звено с пустым ключом выбрасывается: подрубрикой пустая строка не является,
// а дырой в середине пути она сделала бы ребёнка внуком собственного деда.
func rubricPathOf(ref *models.IndexReference) (titles, keys []string) {
	raw := ref.RubricPath
	if len(raw) == 0 {
		raw = []string{ref.Rubric}
	}
	for _, title := range raw {
		key := models.NormalizeIndexTitle(title)
		if key == "" {
			continue
		}
		titles = append(titles, title)
		keys = append(keys, key)
	}
	return titles, keys
}

// rubricPathKey склеивает ключи пути в ключ карты подрубрик. Разделитель —
// NUL: в заголовке из тела ввоза его быть не может, такую строку Postgres не
// принял бы и в саму колонку. Пустой путь даёт пустую строку — это корзина
// корней, и с настоящим путём она не сталкивается: у настоящего есть хотя бы
// одно непустое звено.
func rubricPathKey(keys []string) string {
	return strings.Join(keys, "\x00")
}

// rubricParentDescription называет родителя в сообщении об отказе: без него
// «две подрубрики с одним ключом» у вложенного указателя не говорит, где
// именно искать.
func rubricParentDescription(parents []string) string {
	if len(parents) == 0 {
		return "прямо в статье"
	}
	return fmt.Sprintf("под подрубрикой %q", strings.Join(parents, " → "))
}

// ReplaceForEdition replaces the index articles of one edition with the given
// set, in a single transaction. It returns warnings that don't block the
// import but need a human's eyes (currently: ambiguous concept resolution).
//
// Сопоставление идёт по (edition_id, title_key), а не «снести и вставить»:
// concept_id, проставленный куратором при сведении статей разных указателей,
// обязан пережить повторный прогон — ручное решение, не переживающее
// переимпорт, бесполезно.
func (r *IndexRepository) ReplaceForEdition(ctx context.Context, editionID int64, concepts []*models.IndexConcept) ([]string, error) {
	// Ключи собираются ДО транзакции: они нужны и сносу устаревших статей
	// (первый шаг), и проверке дублей — двум заголовкам с одним ключом в
	// одном теле запроса нельзя дать съесть друг друга молча (см. ниже).
	incomingKeys := make([]string, 0, len(concepts))
	seenTitles := make(map[string]string, len(concepts))
	for _, c := range concepts {
		titleKey := models.NormalizeIndexTitle(c.Title)
		if first, dup := seenTitles[titleKey]; dup {
			return nil, fmt.Errorf("два понятия с одним ключом заголовка в одном ввозе: %q и %q — это дефект разбора, а не данные, тихо потерять одно из них нельзя", first, c.Title)
		}
		seenTitles[titleKey] = c.Title
		incomingKeys = append(incomingKeys, titleKey)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin index import: %w", err)
	}
	defer tx.Rollback(ctx)

	// Статья, исчезнувшая из указателя целиком, уносит свои вырезки, и
	// пересадить их некуда: адресов у неё после сноса не остаётся ни в каком
	// виде. Считаем ДО сноса и называем вслух — тихая потеря подтверждённой
	// человеком вырезки худшее, что этот ввоз может сделать.
	doomed, err := tx.Query(ctx, `
		SELECT a.title, count(f.id)
		FROM index_concept_articles a
		JOIN index_references r ON r.article_id = a.id
		JOIN index_fragments f ON f.reference_id = r.id
		WHERE a.edition_id = $1 AND NOT (a.title_key = ANY($2))
		GROUP BY a.title
		ORDER BY a.title
	`, editionID, incomingKeys)
	if err != nil {
		return nil, fmt.Errorf("failed to count doomed fragments: %w", err)
	}
	var doomedWarnings []string
	for doomed.Next() {
		var title string
		var n int
		if err := doomed.Scan(&title, &n); err != nil {
			doomed.Close()
			return nil, fmt.Errorf("failed to scan doomed fragments: %w", err)
		}
		doomedWarnings = append(doomedWarnings, fmt.Sprintf(
			"статья %q исчезла из указателя и унесла %d %s — пересадить их некуда, адресов у неё не остаётся",
			title, n, pluralizeVyrezok(n)))
	}
	doomed.Close()
	if err := doomed.Err(); err != nil {
		return nil, fmt.Errorf("failed to read doomed fragments: %w", err)
	}

	// 1. Статьи издания, которых в новом наборе нет, — сносятся ПЕРВЫМ шагом,
	// по множеству входящих ключей, а не последним по факту невостребованности
	// в цикле ниже. Иначе легитимный переимпорт бьётся об UNIQUE: пока старая
	// строка жива, INSERT новой с тем же (edition_id, title_key) или тем же
	// (concept_id, edition_id) — потому что понятия ещё не развязаны —
	// натыкается на ограничение раньше, чем до старой строки дойдёт снос.
	// title_key = ANY(пустой массив) ложно для всех строк, поэтому пустой
	// набор input (incomingKeys обязан быть не-nil) сносит статьи издания
	// целиком — это ожидаемо: пустой ввоз — пустой указатель издания.
	if _, err := tx.Exec(ctx, `
		DELETE FROM index_concept_articles
		WHERE edition_id = $1 AND NOT (title_key = ANY($2))
	`, editionID, incomingKeys); err != nil {
		return nil, fmt.Errorf("failed to drop stale articles: %w", err)
	}

	warnings := doomedWarnings

	// Адреса копятся и уезжают одним CopyFrom после цикла: построчная вставка
	// 199 тыс. ленинских адресов стоит 10.3 с против 0.44 с у CopyFrom
	// (замер спеки), а весь обработчик обязан уложиться в дедлайн записи.
	// rubric_id известен только внутри цикла, после вставки подрубрик ЭТОЙ
	// статьи, — отсюда накопитель, а не копирование прямо из article.References.
	refRows := make([][]any, 0, 1024)

	// Вырезки, снятые с адресов до их сноса; сажаются обратно после CopyFrom.
	var rescued []rescuedFragment

	for _, c := range concepts {
		titleKey := models.NormalizeIndexTitle(c.Title)

		// Поля статьи приходят на c.Articles[0]: понятие каталога само по
		// себе их не несёт с 000026, а ввозится ровно одна статья на
		// понятие за раз (index_handler.go оборачивает плоский DTO в
		// Articles перед вызовом ReplaceForEdition).
		var article models.IndexArticle
		if len(c.Articles) > 0 {
			article = *c.Articles[0]
		}
		workID, sourceURL := article.WorkID, article.SourceURL

		// 2. Статья этого издания с таким ключом (переживших снос шага 1
		// ровно столько, сколько входящих ключей, — дублей быть не может).
		var articleID, conceptID int64
		err := tx.QueryRow(ctx, `
			SELECT id, concept_id FROM index_concept_articles
			WHERE edition_id = $1 AND title_key = $2
		`, editionID, titleKey).Scan(&articleID, &conceptID)
		switch {
		case err == nil:
			// нашлась — понятие сохраняем как есть
		case errors.Is(err, pgx.ErrNoRows):
			var ambiguous bool
			conceptID, ambiguous, err = r.resolveConcept(ctx, tx, c, titleKey)
			if err != nil {
				return nil, err
			}
			if ambiguous {
				warnings = append(warnings, fmt.Sprintf(
					"несколько понятий каталога уже носят заголовок %q — заведено новое, слияние решает куратор вручную",
					c.Title))
			}
			articleID = 0
		default:
			return nil, fmt.Errorf("failed to look up article %q: %w", c.Title, err)
		}

		if articleID == 0 {
			err = tx.QueryRow(ctx, `
				INSERT INTO index_concept_articles
					(concept_id, edition_id, work_id, source_url, title, title_key,
					 article_markdown, kind, source_page_start, source_page_end)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
				RETURNING id
			`, conceptID, editionID, workID, sourceURL, c.Title, titleKey,
				article.ArticleMarkdown, article.Kind, article.SourcePageStart, article.SourcePageEnd).Scan(&articleID)
			if err != nil {
				return nil, fmt.Errorf("failed to insert article %q: %w", c.Title, err)
			}
		} else {
			// COALESCE($2, work_id), не голое присваивание: work_id — якорь,
			// которым scripts/takedown.sh находит статьи снимаемого тома
			// (apparatus_repository.go), а ни parse_index.py, ни
			// publish_volume.py его в теле не шлют (раунд правок 1, находка
			// 2 — главная находка ревью). Голое присваивание писало бы NULL
			// поверх прежнего значения при КАЖДОМ повторном ввозе издания —
			// план снятия аппарата тома молча превращался бы в «понятий: 0»
			// после первого же переимпорта. Прежний код (ReplaceForWork,
			// до a4cac952) так сломать было нельзя: workID брался из URL
			// тома, а не из тела, и присваивался всегда одинаково. Явно
			// присланное значение (не NULL) по-прежнему побеждает — это
			// осознанный путь для будущего клиента и для переноса статьи
			// на другую работу.
			if _, err := tx.Exec(ctx, `
				UPDATE index_concept_articles
				SET work_id = COALESCE($2, work_id), source_url = $3, title = $4,
				    article_markdown = $5, kind = $6, source_page_start = $7, source_page_end = $8
				WHERE id = $1
			`, articleID, workID, sourceURL, c.Title, article.ArticleMarkdown, article.Kind,
				article.SourcePageStart, article.SourcePageEnd); err != nil {
				return nil, fmt.Errorf("failed to update article %q: %w", c.Title, err)
			}
			// Г. Вырезки снимаются ДО сноса: index_fragments висят на
			// index_references с ON DELETE CASCADE, и DELETE ниже уносит их
			// каскадом — в том числе подтверждённые человеком
			// (status='confirmed'). Возвращаются после CopyFrom по ключу
			// адреса (index_fragment_transplant.go).
			got, err := rescueFragments(ctx, tx, articleID, c.Title)
			if err != nil {
				return nil, err
			}
			rescued = append(rescued, got...)

			// Адреса, подрубрики и отсылки статьи заменяются целиком: они
			// производные от разбора, править их в читальне нечем. Вырезки
			// каскад по-прежнему уносит — их сняли строкой выше и посадят
			// обратно после CopyFrom.
			if _, err := tx.Exec(ctx, `DELETE FROM index_references WHERE article_id = $1`, articleID); err != nil {
				return nil, fmt.Errorf("failed to clear references of %q: %w", c.Title, err)
			}
			if _, err := tx.Exec(ctx, `DELETE FROM index_rubrics WHERE article_id = $1`, articleID); err != nil {
				return nil, fmt.Errorf("failed to clear rubrics of %q: %w", c.Title, err)
			}
			if _, err := tx.Exec(ctx, `DELETE FROM index_concept_links WHERE from_article_id = $1`, articleID); err != nil {
				return nil, fmt.Errorf("failed to clear links of %q: %w", c.Title, err)
			}
		}

		// 3. Подрубрики — из путей, названных строками адресов, в порядке
		// первого появления. Дерево, а не плоский список: в печати статья
		// «КПСС — съезды» трёхуровневая (статья → съезд → аспект), и
		// родитель вставляется раньше ребёнка.
		//
		// Дедупликация обязательна: подрубрика приходит строкой НА КАЖДОМ
		// адресе, и десять адресов одной подрубрики — норма. Но «тот же ключ
		// при другом написании» — не повтор, а две разные подрубрики, чьи
		// ключи столкнулись, и молчаливый пропуск второй увёл бы её адреса
		// под название первой. Спека: «Две подрубрики с одним title_key в
		// одной статье — отказ ввоза, а не склейка… упасть на этом лучше,
		// чем показать понятие с удвоенной подрубрикой». Различаем как и у
		// дубля заголовка понятия выше — ошибкой, называющей оба написания.
		//
		// Правило уникальности живёт СРЕДИ ДЕТЕЙ ОДНОГО РОДИТЕЛЯ, а не в
		// статье целиком: «значение съезда» стоит у СЕМИ съездов
		// ленинского указателя, и прежнее правило уронило бы весь ввоз
		// целиком. Поэтому ключ карты — весь путь, а не ключ листа.
		rubricIDs := map[string]int64{}
		rubricTitles := map[string]string{}
		// childCount — сколько детей уже заведено у родителя (ключ — путь
		// родителя, пустая строка у корня). order_number подрубрики
		// считается среди своих братьев, а не сквозным по статье.
		childCount := map[string]int{}
		for _, ref := range article.References {
			titles, keys := rubricPathOf(ref)
			var parentID *int64
			for i, key := range keys {
				parentPath := rubricPathKey(keys[:i])
				path := rubricPathKey(keys[:i+1])
				if id, seen := rubricIDs[path]; seen {
					if first := rubricTitles[path]; first != titles[i] {
						return nil, fmt.Errorf("понятие %q: две подрубрики с одним ключом заголовка %s: %q и %q — это дефект разбора, а не данные, адреса второй ушли бы под название первой",
							c.Title, rubricParentDescription(titles[:i]), first, titles[i])
					}
					parentID = &id
					continue
				}
				childCount[parentPath]++
				var id int64
				if err := tx.QueryRow(ctx, `
					INSERT INTO index_rubrics (article_id, parent_id, title, title_key, order_number)
					VALUES ($1, $2, $3, $4, $5) RETURNING id
				`, articleID, parentID, titles[i], key, childCount[parentPath]).Scan(&id); err != nil {
					return nil, fmt.Errorf("failed to insert rubric %q of %q: %w", titles[i], c.Title, err)
				}
				rubricIDs[path] = id
				rubricTitles[path] = titles[i]
				parentID = &id
			}
		}

		// 4. Адреса. concept_id снят миграцией 000026 — адрес держится только
		// article_id, понятие резолвится через него. Строки копятся; вставка
		// одним CopyFrom идёт после цикла.
		for _, ref := range article.References {
			var rubricID *int64
			if _, keys := rubricPathOf(ref); len(keys) > 0 {
				// Адрес висит на ЛИСТЕ пути: промежуточные звенья —
				// заголовки, своих адресов у них нет.
				id := rubricIDs[rubricPathKey(keys)]
				rubricID = &id
			}
			refRows = append(refRows, []any{
				articleID, rubricID, ref.VolumeNumber, ref.VolumePart,
				ref.PageStart, ref.PageEnd, ref.OrderNumber, ref.IsUncertain, ref.Note,
			})
		}

		// 5. Отсылки. from_concept_id снят миграцией 000026 — отсылка держится
		// только from_article_id. target_title_key считается ЗДЕСЬ, в Go, той
		// же функцией, что ключ понятия: у SQL нет ни NFC, ни резки по
		// неразрывному пробелу, и расхождение молча рвало связь (миграция
		// 000029).
		for _, l := range article.Links {
			if _, err := tx.Exec(ctx, `
				INSERT INTO index_concept_links
					(from_article_id, target_title, target_title_key, kind, order_number)
				VALUES ($1, $2, $3, $4, $5)
			`, articleID, l.TargetTitle, models.NormalizeIndexTitle(l.TargetTitle),
				l.Kind, l.OrderNumber); err != nil {
				return nil, fmt.Errorf("failed to insert link for %q: %w", c.Title, err)
			}
		}
	}

	// 4б. Адреса — одним CopyFrom. 0.44 с против 10.3 с построчно на
	// ленинском объёме; порядок строк сохраняется, id раздаёт та же
	// последовательность.
	if len(refRows) > 0 {
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"index_references"},
			[]string{"article_id", "rubric_id", "volume_number", "volume_part",
				"page_start", "page_end", "order_number", "is_uncertain", "note"},
			pgx.CopyFromRows(refRows)); err != nil {
			return nil, fmt.Errorf("failed to copy references: %w", err)
		}
	}

	// 4в. Вырезки возвращаются на новые адреса по ключу. CopyFrom
	// идентификаторов не отдаёт, поэтому карта читается обратно — но только
	// по статьям, у которых вырезки были.
	transplanted, err := transplantFragments(ctx, tx, rescued)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, transplanted...)

	// 6. Понятие, оставшееся без единой статьи, — мусор. Остаётся ПОСЛЕДНИМ
	// шагом, как и раньше: понятие, осиротевшее в начале прогона, может ещё
	// быть подобрано более поздней входящей статьёй через поиск по title_key
	// (resolveConcept) — снести его раньше значило бы уничтожить сведение
	// куратора, которому полагалось уцелеть.
	if _, err := tx.Exec(ctx, `
		DELETE FROM index_concepts c
		WHERE NOT EXISTS (SELECT 1 FROM index_concept_articles a WHERE a.concept_id = c.id)
	`); err != nil {
		return nil, fmt.Errorf("failed to drop orphan concepts: %w", err)
	}

	// 7. Отсылки связываются по нормализованному заголовку, по всей таблице:
	// «см. Труд» из указателя А—М должна найти «Труд» из указателя Н—Я.
	//
	// Обе стороны сравнения пишет models.NormalizeIndexTitle — ключ понятия
	// при его заведении, ключ отсылки при её вставке. Выражения нормализации
	// в SQL не осталось, и разойтись им больше негде (миграция 000029).
	if _, err := tx.Exec(ctx, `
		UPDATE index_concept_links l
		SET to_concept_id = c.id
		FROM index_concepts c
		WHERE c.title_key = l.target_title_key
	`); err != nil {
		return nil, fmt.Errorf("failed to link concepts: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit index import: %w", err)
	}
	return warnings, nil
}

// resolveConcept finds or creates the catalog concept for an article. The
// second return value reports ambiguity: title_key matched more than one
// existing concept, and the caller must warn instead of silently choosing.
//
// Одно совпадение по ключу — сводим автоматически; несколько — заводим новое
// понятие и НЕ выбираем за куратора, к которому из одноимённых цеплять
// (title_key понятия не уникален намеренно).
func (r *IndexRepository) resolveConcept(ctx context.Context, tx pgx.Tx, c *models.IndexConcept, titleKey string) (int64, bool, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM index_concepts WHERE title_key = $1 LIMIT 2`, titleKey)
	if err != nil {
		return 0, false, fmt.Errorf("failed to look up concept %q: %w", c.Title, err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, false, fmt.Errorf("failed to scan concept id: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	// Усечённая выборка выглядит как ровно одно совпадение и включила бы
	// автосведение там, где спецификация требует нового понятия и
	// предупреждения — проверка обязательна именно здесь, а не только для
	// симметрии со сборкой doomed-статей.
	if err := rows.Err(); err != nil {
		return 0, false, fmt.Errorf("failed to read concept matches %q: %w", c.Title, err)
	}
	if len(ids) == 1 {
		return ids[0], false, nil
	}

	// Ноль совпадений — обычное новое понятие; больше одного — сводить
	// самовольно нельзя, заводим новое и сигналим наверх (ambiguous=true),
	// чтобы куратор слил вручную, если решит, что это одно понятие.
	//
	// work_id понятию не проставляется вовсе (с 000026 у него такой колонки
	// нет): работа, если она есть, принадлежит статье, а не понятию каталога.
	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO index_concepts (title, slug, sort_key, title_key)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, c.Title, c.Slug, c.SortKey, titleKey).Scan(&id); err != nil {
		return 0, false, fmt.Errorf("failed to insert concept %q: %w", c.Title, err)
	}
	return id, len(ids) > 1, nil
}

const conceptBySlugQuery = `
	SELECT id, title, slug, sort_key, title_key, created_at, updated_at
	FROM index_concepts
	WHERE slug = $1
`

// rubricAncestryCTE — путь от корня до КАЖДОЙ подрубрики, общий для всех
// запросов чтения, которым он нужен. Два столбца: path — заголовки, как они
// напечатаны (для показа читателю), key_path — их нормализованные ключи (для
// index_fragment_transplant.go, который сопоставляет адреса при переимпорте
// и не должен знать написание, только ключ).
//
// Написан РЕКУРСИВНЫМ намеренно, хотя сегодняшняя глубина данных равна двум
// (статья → съезд → аспект): два простых LEFT JOIN на parent_id закрыли бы
// сегодняшний случай, но молча обрезали бы третий уровень, когда он
// появится, — а молчаливая обрезка хуже отказа.
//
// Якорь (`parent_id IS NULL`) обходит index_rubrics ЦЕЛИКОМ, без разбора по
// статьям — единственный потребитель, которому это на деле нужно,
// backlinksQuery: та ищет по тому/полосе, не по одной статье, и заранее не
// знает, чьи рубрики попадут в выдачу. Для запросов, читающих путь рубрики
// ПО ОДНОЙ СТАТЬЕ, есть суженный вариант — rubricAncestryByArticleCTE ниже.
const rubricAncestryCTE = `
	WITH RECURSIVE rubric_ancestry AS (
		SELECT id, parent_id,
		       ARRAY[title]::text[] AS path,
		       ARRAY[title_key]::text[] AS key_path
		FROM index_rubrics
		WHERE parent_id IS NULL
		UNION ALL
		SELECT child.id, child.parent_id,
		       anc.path || child.title,
		       anc.key_path || child.title_key
		FROM index_rubrics child
		JOIN rubric_ancestry anc ON anc.id = child.parent_id
	)
`

// rubricAncestryByArticleCTE — тот же обход, но с якорем, суженным до
// подрубрик ОДНОЙ статьи ($1 = article_id, тот же параметр, что несёт и
// внешний запрос — Postgres допускает ссылаться на $1 больше одного раза).
//
// Безопасно инвариантом, а не предположением: ни у одной подрубрики
// article_id не расходится с article_id родителя (сверено прямым SQL по
// всему корпусу), так что обход и без того не выходил за пределы дерева
// этой статьи — сужение якоря не роняет ни одного узла, а лишь не даёт
// планировщику каждый раз обходить index_rubrics ЦЕЛИКОМ ради одной статьи.
// Рекурсивный шаг фильтр не повторяет ровно по той же причине: он и так
// наследует article_id через JOIN на уже отфильтрованный якорь.
//
// Нужен там, где путь рубрики читается ПО ОДНОЙ СТАТЬЕ внутри цикла импорта
// на 2464 статьи (ReplaceForEdition): articleReferencesQuery здесь и обе
// функции index_fragment_transplant.go (rescueFragments, newReferenceKeys).
// backlinksQuery на него не переведён — та не ограничена одной статьёй и
// использует rubricAncestryCTE выше.
const rubricAncestryByArticleCTE = `
	WITH RECURSIVE rubric_ancestry AS (
		SELECT id, parent_id,
		       ARRAY[title]::text[] AS path,
		       ARRAY[title_key]::text[] AS key_path
		FROM index_rubrics
		WHERE parent_id IS NULL AND article_id = $1
		UNION ALL
		SELECT child.id, child.parent_id,
		       anc.path || child.title,
		       anc.key_path || child.title_key
		FROM index_rubrics child
		JOIN rubric_ancestry anc ON anc.id = child.parent_id
	)
`

const conceptArticlesQuery = `
	SELECT a.id, a.concept_id, a.edition_id, COALESCE(e.title, ''), a.work_id,
	       a.source_url, a.title, a.title_key, a.article_markdown, a.kind,
	       a.source_page_start, a.source_page_end, a.created_at, a.updated_at
	FROM index_concept_articles a
	LEFT JOIN editions e ON e.id = a.edition_id
	WHERE a.concept_id = $1
	ORDER BY a.edition_id, a.id
`

// articleReferencesQuery тянет адреса статьи вместе с НАЗВАНИЕМ подрубрики
// (лист, для старых потребителей) и её ПУТЁМ от корня (rubric_ancestry,
// задача 8) — подрубрика хранится строкой таблицы со ссылкой на родителя, а
// путь по данным сегодня может доходить до второго уровня. LEFT JOIN —
// потому что адрес без подрубрики законен, тогда путь пуст.
const articleReferencesQuery = rubricAncestryByArticleCTE + `
	SELECT r.id, r.article_id, r.rubric_id, r.volume_number, r.volume_part,
	       r.page_start, r.page_end, COALESCE(ru.title, ''), r.order_number,
	       r.is_uncertain, r.note, COALESCE(anc.path, ARRAY[]::text[])
	FROM index_references r
	LEFT JOIN index_rubrics ru ON ru.id = r.rubric_id
	LEFT JOIN rubric_ancestry anc ON anc.id = r.rubric_id
	WHERE r.article_id = $1
	ORDER BY r.order_number
`

// GetConceptBySlug retrieves a concept by slug with its articles, each with
// its references (ordered by order_number) and the rubric title of each.
//
// Kind не заполняется здесь: он вычисляется агрегатом только в
// listConceptsQuery (списку он нужен для значка «см.» в навигаторе), а
// страница понятия показывает Kind каждой статьи отдельно
// (ConceptView.tsx: article.kind), не понятия целиком.
func (r *IndexRepository) GetConceptBySlug(ctx context.Context, slug string) (*models.IndexConcept, error) {
	var c models.IndexConcept
	err := r.pool.QueryRow(ctx, conceptBySlugQuery, slug).Scan(
		&c.ID, &c.Title, &c.Slug, &c.SortKey, &c.TitleKey, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("concept not found")
		}
		return nil, fmt.Errorf("failed to get concept: %w", err)
	}

	articles, err := r.conceptArticles(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	c.Articles = articles

	return &c, nil
}

func (r *IndexRepository) conceptArticles(ctx context.Context, conceptID int64) ([]*models.IndexArticle, error) {
	rows, err := r.pool.Query(ctx, conceptArticlesQuery, conceptID)
	if err != nil {
		return nil, fmt.Errorf("failed to list concept articles: %w", err)
	}
	var articles []*models.IndexArticle
	for rows.Next() {
		var a models.IndexArticle
		if err := rows.Scan(
			&a.ID, &a.ConceptID, &a.EditionID, &a.EditionTitle, &a.WorkID,
			&a.SourceURL, &a.Title, &a.TitleKey, &a.ArticleMarkdown, &a.Kind,
			&a.SourcePageStart, &a.SourcePageEnd, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan concept article: %w", err)
		}
		articles = append(articles, &a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read concept articles: %w", err)
	}

	// Адреса тянутся ПОСЛЕ закрытия первого курсора: pgx не даёт запускать
	// второй запрос на том же соединении, пока первый не вычитан до конца.
	for _, a := range articles {
		refs, err := r.articleReferences(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		a.References = refs
	}
	return articles, nil
}

func (r *IndexRepository) articleReferences(ctx context.Context, articleID int64) ([]*models.IndexReference, error) {
	rows, err := r.pool.Query(ctx, articleReferencesQuery, articleID)
	if err != nil {
		return nil, fmt.Errorf("failed to list references: %w", err)
	}
	defer rows.Close()

	var refs []*models.IndexReference
	for rows.Next() {
		var ref models.IndexReference
		if err := rows.Scan(
			&ref.ID, &ref.ArticleID, &ref.RubricID, &ref.VolumeNumber, &ref.VolumePart,
			&ref.PageStart, &ref.PageEnd, &ref.Rubric, &ref.OrderNumber,
			&ref.IsUncertain, &ref.Note, &ref.RubricPath,
		); err != nil {
			return nil, fmt.Errorf("failed to scan reference: %w", err)
		}
		refs = append(refs, &ref)
	}
	return refs, rows.Err()
}

const articleLinksQuery = `
	SELECT l.id, l.from_article_id, l.to_concept_id, l.target_title, l.kind, l.order_number,
	       COALESCE(t.slug, '')
	FROM index_concept_links l
	LEFT JOIN index_concepts t ON t.id = l.to_concept_id
	WHERE l.from_article_id = $1
	ORDER BY l.order_number
`

// ArticleLinks retrieves the "см." / "см. также" links originating from one
// index article, along with the slug of the resolved target (empty if
// unresolved). Отсылки принадлежат статье, а не понятию каталога — у
// понятия с несколькими статьями у каждой свой набор.
func (r *IndexRepository) ArticleLinks(ctx context.Context, articleID int64) ([]LinkWithTarget, error) {
	rows, err := r.pool.Query(ctx, articleLinksQuery, articleID)
	if err != nil {
		return nil, fmt.Errorf("failed to list article links: %w", err)
	}
	defer rows.Close()

	var links []LinkWithTarget
	for rows.Next() {
		var l LinkWithTarget
		err := rows.Scan(
			&l.ID, &l.FromArticleID, &l.ToConceptID, &l.TargetTitle, &l.Kind, &l.OrderNumber,
			&l.TargetSlug,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan article link: %w", err)
		}
		links = append(links, l)
	}

	return links, nil
}

// IncomingLink is a link pointing AT a concept: the title and slug of the source.
type IncomingLink struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
	Kind  string `json:"kind"`
}

const incomingLinksQuery = `
	SELECT c.slug, c.title, l.kind
	FROM index_concept_links l
	JOIN index_concept_articles a ON a.id = l.from_article_id
	JOIN index_concepts c ON c.id = a.concept_id
	WHERE l.to_concept_id = $1
	ORDER BY c.sort_key, l.order_number
`

// IncomingLinks retrieves the "см." / "см. также" links that point AT a concept.
// Печатная статья показывает только исходящие отсылки, обратное направление
// в данных есть (to_concept_id заполняется при импорте) и именно оно говорит
// читателю, какие понятия ведут сюда.
func (r *IndexRepository) IncomingLinks(ctx context.Context, conceptID int64) ([]IncomingLink, error) {
	rows, err := r.pool.Query(ctx, incomingLinksQuery, conceptID)
	if err != nil {
		return nil, fmt.Errorf("failed to list incoming links: %w", err)
	}
	defer rows.Close()

	var links []IncomingLink
	for rows.Next() {
		var l IncomingLink
		if err := rows.Scan(&l.Slug, &l.Title, &l.Kind); err != nil {
			return nil, fmt.Errorf("failed to scan incoming link: %w", err)
		}
		links = append(links, l)
	}

	return links, nil
}

// listConceptsQuery отдаёт kind как свойство статьи, а не понятия: понятие
// считается статьёй, если хоть один указатель дал о нём статью — перенаправление
// в одном собрании не отменяет разбора в другом.
//
// edition_ids — агрегат изданий, у которых есть статья об этом понятии.
// НЕ ТРОГАЙ молча: питоновский публикатор конвейера
// (tools/ocr_ingest/apply_index.py:count_existing,
// tools/ocr_ingest/publish_volume.py:index_payload/survey_index) считает и
// отбирает понятия ровно по этому полю, а не по work_id — без него защита от
// усыхания указателя при ввозе по изданию не на чем считать. Эту константу в
// ветке уже правили дважды (kind по EXISTS, затем work_id nullable), и задача
// 11 будет её трогать снова — если меняешь список колонок, проверь, что
// agrегат и сканирование edition_ids ниже пережили правку.
const listConceptsQuery = `
	SELECT c.id, c.title, c.slug, c.sort_key, c.title_key,
	       CASE WHEN EXISTS (
	           SELECT 1 FROM index_concept_articles a
	           WHERE a.concept_id = c.id AND a.kind = 'article'
	       ) THEN 'article' ELSE 'redirect' END,
	       c.created_at, c.updated_at,
	       COALESCE((SELECT array_agg(DISTINCT a.edition_id)
	                 FROM index_concept_articles a WHERE a.concept_id = c.id), '{}') AS edition_ids
	FROM index_concepts c
	WHERE ($1 = '' OR c.title ILIKE '%' || $1 || '%')
	  AND ($2 = '' OR c.sort_key LIKE $2 || '%')
	ORDER BY c.sort_key, c.id
	LIMIT $3 OFFSET $4
`

// ListConcepts retrieves a page of concepts for the index navigator,
// optionally filtered by a title substring and/or a sort-key prefix
// (first letter). article_markdown is intentionally left empty: the
// navigator doesn't need it, and it is by far the heaviest column.
func (r *IndexRepository) ListConcepts(ctx context.Context, query, letter string, limit, offset int) ([]*models.IndexConcept, error) {
	rows, err := r.pool.Query(ctx, listConceptsQuery, query, letter, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list concepts: %w", err)
	}
	defer rows.Close()

	var concepts []*models.IndexConcept
	for rows.Next() {
		var c models.IndexConcept
		err := rows.Scan(
			&c.ID, &c.Title, &c.Slug, &c.SortKey, &c.TitleKey, &c.Kind,
			&c.CreatedAt, &c.UpdatedAt, &c.EditionIDs,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan concept: %w", err)
		}
		concepts = append(concepts, &c)
	}

	return concepts, nil
}

// volumeMapSlugColumns — три ингредиента слага тома (см. workSlugColumns,
// work_repository.go), с алиасом w вместо works: volumeMapQuery зовёт работу
// так же. Подстановкой, а не рукописной копией — см. workSummariesSlugColumns
// (edition_repository.go).
var volumeMapSlugColumns = strings.ReplaceAll(workSlugColumns, "works.", "w.")

// GROUP BY w.id — по первичному ключу, поэтому Postgres допускает в SELECT
// любые не агрегированные выражения над столбцами works (функциональная
// зависимость), в том числе workSlugColumns с его коррелированными
// подзапросами: searchVolumesQuery в этом же файле пакета делает ровно то же
// самое (GROUP BY w.id, p.id, e.id + searchWorkSlugColumns) и работает —
// проверено на живых данных, отдельного CTE не потребовалось.
var volumeMapQuery = `
	SELECT w.volume_number, w.volume_part, w.id, w.page_offset,
	       w.role, w.precedes_volume, w.title, ` + volumeMapSlugColumns + `,
	       COALESCE(MAX(p.page_number), 0)
	FROM works w
	LEFT JOIN pages p ON p.work_id = w.id
	WHERE w.edition_id = $1 AND w.volume_number IS NOT NULL
	GROUP BY w.id
`

// VolumeMap retrieves the volume/work layout of an edition, for resolving
// printed page references to a work + page_number.
func (r *IndexRepository) VolumeMap(ctx context.Context, editionID int64) ([]models.VolumeLocation, error) {
	rows, err := r.pool.Query(ctx, volumeMapQuery, editionID)
	if err != nil {
		return nil, fmt.Errorf("failed to list volume map: %w", err)
	}
	defer rows.Close()

	var locations []models.VolumeLocation
	for rows.Next() {
		var loc models.VolumeLocation
		var role string
		var precedesVolume *int
		var title string
		var slugEdition string
		var slugVolume *int
		var slugPart *string
		err := rows.Scan(
			&loc.VolumeNumber, &loc.VolumePart, &loc.WorkID, &loc.PageOffset,
			&role, &precedesVolume, &title,
			&slugEdition, &slugVolume, &slugPart,
			&loc.MaxPage,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan volume location: %w", err)
		}
		loc.WorkSlug = workSlugFrom(role, slugEdition, slugVolume, slugPart, precedesVolume, title)
		locations = append(locations, loc)
	}

	return locations, nil
}

const backlinksQuery = rubricAncestryCTE + `
	SELECT c.id, c.slug, c.title, COALESCE(ru.title, ''), r.page_start, r.page_end,
	       COALESCE(anc.path, ARRAY[]::text[])
	FROM index_references r
	JOIN index_concept_articles a ON a.id = r.article_id
	JOIN index_concepts c ON c.id = a.concept_id
	LEFT JOIN index_rubrics ru ON ru.id = r.rubric_id
	LEFT JOIN rubric_ancestry anc ON anc.id = r.rubric_id
	WHERE r.volume_number = $2
	  AND r.volume_part IS NOT DISTINCT FROM $3
	  AND r.page_start <= $4 AND r.page_end >= $4
	  AND a.edition_id = $1
	ORDER BY c.sort_key, r.order_number
`

// Backlinks retrieves the concepts referenced on a given printed page of a
// volume within an edition. volumePart is compared with IS NOT DISTINCT FROM
// because it may be NULL, and "= NULL" never matches.
func (r *IndexRepository) Backlinks(ctx context.Context, editionID int64, volumeNumber int, volumePart *string, printedPage int) ([]models.ConceptBacklink, error) {
	rows, err := r.pool.Query(ctx, backlinksQuery, editionID, volumeNumber, volumePart, printedPage)
	if err != nil {
		return nil, fmt.Errorf("failed to list backlinks: %w", err)
	}
	defer rows.Close()

	var backlinks []models.ConceptBacklink
	for rows.Next() {
		var b models.ConceptBacklink
		err := rows.Scan(&b.ConceptID, &b.Slug, &b.Title, &b.Rubric, &b.PageStart, &b.PageEnd, &b.RubricPath)
		if err != nil {
			return nil, fmt.Errorf("failed to scan backlink: %w", err)
		}
		backlinks = append(backlinks, b)
	}

	return backlinks, nil
}

// takenSlugsQuery — ВСЕ слаги каталога, без исключений.
//
// Исключение слагов ввозимого издания (до сквозной рецензии ветки) исходило
// из верного наблюдения: статья, уже существующая в этом издании, найдётся по
// паре (edition_id, title_key), и слаг из тела запроса не израсходует вовсе —
// её понятие сохранит прежний. Но освобождённый слаг достаётся не ей: его
// берёт ДРУГОЕ, новое понятие того же ввоза, чей слаг совпал (обычное дело —
// «Капитал» и «капитал» слагифицируются одинаково), и INSERT в index_concepts
// падает на уникальном индексе слага, роняя ввоз целиком. Лишний суффикс у
// слага, который в итоге никому не достался, — цена несравнимо меньшая.
const takenSlugsQuery = `SELECT slug FROM index_concepts`

// TakenSlugs retrieves every concept slug in use, for the import handler to
// resolve collisions against.
func (r *IndexRepository) TakenSlugs(ctx context.Context) (map[string]bool, error) {
	rows, err := r.pool.Query(ctx, takenSlugsQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to list taken slugs: %w", err)
	}
	defer rows.Close()

	slugs := make(map[string]bool)
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, fmt.Errorf("failed to scan slug: %w", err)
		}
		slugs[slug] = true
	}

	return slugs, nil
}
