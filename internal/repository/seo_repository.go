package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/pkg/slug"
)

// CatalogRow — одна строка каталога для карты сайта: адрес и время правки, и
// ничего больше. Заголовки, авторы и текст карте не нужны, а на корпусе в
// десятки тысяч глав каждый лишний столбец — лишние мегабайты в ответе.
//
// Заполненные поля зависят от метода: у работ, глав и изданий — ID (у глав
// ещё WorkID и WorkSlug), у понятий и подборок — Slug. Адреса из них строит
// internal/seo/sitemap.go: репозиторий про URL читальни не знает.
type CatalogRow struct {
	ID     int64
	WorkID int64
	Slug   string
	// WorkSlug — слаг тома, которому принадлежит глава: адрес главы несёт
	// оба хвоста, и добирать его отдельным запросом на 11 775 глав нельзя.
	WorkSlug  string
	UpdatedAt time.Time
	// AuthorNickname и PublishedAt заполняются только у подборок — карте сайта
	// и общей карточке нужно отличить сотруднические подборки от читательских
	// (видны по ссылке, но не в списке) и черновики (не видны вовсе). Фильтр
	// живёт в internal/seo, а не в SQL здесь: подставной каталог тестов пакета
	// seo не исполняет запросов, и фильтр в SQL был бы невидим тесту на
	// мутацию.
	AuthorNickname string
	PublishedAt    *time.Time
}

// SEORepository — запросы каталога, нужные только карте сайта. Отдельным
// репозиторием, а не пятью методами в чужих: потребности у карты свои, и
// собранные в одном файле они читаются целиком.
type SEORepository struct {
	pool *pgxpool.Pool
}

func NewSEORepository(pool *pgxpool.Pool) *SEORepository {
	return &SEORepository{pool: pool}
}

// indexableWorkRoles — роли, попадающие в индекс. Служебные передние листы
// (front_matter) скрыты из каталога и достижимы только с карточки своего
// тома, поэтому в выдаче им делать нечего. Предисловие к изданию
// (edition_front_matter) читатель видит на странице издания — оно остаётся.
const indexableWorkRoles = `('volume', 'edition_front_matter')`

// scanFunc — функция разбора одной строки результата. Связь между колонками
// SQL и разбором теперь хранится в типе, а не в совпадении ярлыков: это
// предотвращает ошибки, когда число колонок совпадёт, но форма разбора окажется
// для другого запроса.
type scanFunc func(pgx.Rows) (CatalogRow, error)

func (r *SEORepository) query(ctx context.Context, what, sql string, scan scanFunc) ([]CatalogRow, error) {
	rows, err := r.pool.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("каталог %s: %w", what, err)
	}
	defer rows.Close()

	// Не nil, а пустой слайс: пустой каталог — это карта без адресов, а не
	// отсутствие карты.
	out := []CatalogRow{}
	for rows.Next() {
		row, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("каталог %s, разбор строки: %w", what, err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("каталог %s: %w", what, err)
	}
	return out, nil
}

// Ингредиенты слага добираются к строке работы так же, как в scanWork
// (work_repository.go): role и precedes_volume — для передних листов
// собрания (edition_front_matter), у которых номер тома не свой, а «перед
// каким томом стоит»; title — запасной вариант, когда собрания нет вовсе.
func (r *SEORepository) Works(ctx context.Context) ([]CatalogRow, error) {
	return r.query(ctx, "работы", `
		SELECT works.id, works.updated_at, works.role, works.precedes_volume, works.title,
		       `+workSlugColumns+`
		FROM works
		WHERE works.role IN `+indexableWorkRoles+`
		ORDER BY works.id`, func(rows pgx.Rows) (CatalogRow, error) {
		var row CatalogRow
		var role, title, slugEdition string
		var precedesVolume, slugVolume *int
		var slugPart *string
		err := rows.Scan(&row.ID, &row.UpdatedAt, &role, &precedesVolume, &title,
			&slugEdition, &slugVolume, &slugPart)
		if err != nil {
			return row, err
		}
		row.Slug = workSlugFrom(role, slugEdition, slugVolume, slugPart, precedesVolume, title)
		return row, nil
	})
}

// Chapters добирает и слаг самой главы (из её заголовка), и слаг тома, к
// которому она принадлежит: адрес главы несёт оба хвоста, а тянуть слаг тома
// отдельным запросом на 11 775 строк нельзя.
//
// Ингредиенты слага считаются один раз на работу через CTE work_slugs, а не
// подзапросом на каждую из 11 821 строки главы: подзапрос искал издание в
// среднем 146 раз на одну и ту же работу (11 821 глава / 81 работа), и это
// была замеренная регрессия в 26 раз (109.4 мс против 4.2 мс без слагов).
// MATERIALIZED обязателен: без него Postgres вправе развернуть CTE обратно и
// вернуть подзапросы в построчный режим, отменив весь выигрыш. Внутри CTE
// работа зовётся works (как в scanWork), поэтому workSlugColumns подставляется
// как есть, без подмены префикса на алиас — лишняя копия выражения по-прежнему
// под запретом (см. searchWorkSlugColumns, volumeMapSlugColumns,
// workSummariesSlugColumns).
func (r *SEORepository) Chapters(ctx context.Context) ([]CatalogRow, error) {
	return r.query(ctx, "главы", `
		WITH work_slugs AS MATERIALIZED (
			SELECT works.id, works.role, works.precedes_volume, works.title,
			       `+workSlugColumns+`
			FROM works
			WHERE works.role IN `+indexableWorkRoles+`
		)
		SELECT c.id, c.work_id, c.updated_at, c.title,
		       ws.role, ws.precedes_volume, ws.title,
		       ws.slug_edition, ws.slug_volume, ws.slug_part
		FROM chapters c
		JOIN work_slugs ws ON ws.id = c.work_id
		ORDER BY c.work_id, c.id`, func(rows pgx.Rows) (CatalogRow, error) {
		var row CatalogRow
		var chapterTitle, role, workTitle, slugEdition string
		var precedesVolume, slugVolume *int
		var slugPart *string
		err := rows.Scan(&row.ID, &row.WorkID, &row.UpdatedAt, &chapterTitle,
			&role, &precedesVolume, &workTitle,
			&slugEdition, &slugVolume, &slugPart)
		if err != nil {
			return row, err
		}
		row.Slug = slug.Chapter(chapterTitle)
		row.WorkSlug = workSlugFrom(role, slugEdition, slugVolume, slugPart, precedesVolume, workTitle)
		return row, nil
	})
}

func (r *SEORepository) Editions(ctx context.Context) ([]CatalogRow, error) {
	return r.query(ctx, "издания", `
		SELECT id, updated_at, COALESCE(NULLIF(url_slug, ''), slug)
		FROM editions ORDER BY id`, func(rows pgx.Rows) (CatalogRow, error) {
		var row CatalogRow
		err := rows.Scan(&row.ID, &row.UpdatedAt, &row.Slug)
		return row, err
	})
}

// Concepts отдаёт только статьи. Перенаправление («Труд — см. Абстрактный
// труд») собственного текста не имеет, и в индексе это была бы пустая
// страница.
//
// kind снят с index_concepts миграцией 000026 (задача 11) — это свойство
// статьи (index_concept_articles), а не понятия: понятие считается статьёй,
// если хоть один указатель дал о нём статью, тем же правилом EXISTS, что и
// listConceptsQuery в index_repository.go — расходиться эти два места не
// должны.
func (r *SEORepository) Concepts(ctx context.Context) ([]CatalogRow, error) {
	return r.query(ctx, "понятия", `
		SELECT c.slug, c.updated_at FROM index_concepts c
		WHERE EXISTS (
		    SELECT 1 FROM index_concept_articles a
		    WHERE a.concept_id = c.id AND a.kind = 'article'
		)
		ORDER BY c.slug`, func(rows pgx.Rows) (CatalogRow, error) {
		var row CatalogRow
		err := rows.Scan(&row.Slug, &row.UpdatedAt)
		return row, err
	})
}

// ConceptShelfRow — строка витрины понятий для нейросетей (/concepts.md).
type ConceptShelfRow struct {
	Slug, Title, SortKey string
	// Places — адресов во всех статьях понятия.
	Places int
}

// ConceptShelf — понятия-статьи по алфавиту с числом адресов, одним
// запросом: витрина печатает все ~2400 строк, и счёт по понятию стоил бы
// столько же запросов.
func (r *SEORepository) ConceptShelf(ctx context.Context) ([]ConceptShelfRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.slug, c.title, c.sort_key, count(ref.id)
		FROM index_concepts c
		JOIN index_concept_articles a ON a.concept_id = c.id
		LEFT JOIN index_references ref ON ref.article_id = a.id
		GROUP BY c.id
		HAVING bool_or(a.kind = 'article')
		ORDER BY c.sort_key, c.slug`)
	if err != nil {
		return nil, fmt.Errorf("витрина понятий: %w", err)
	}
	defer rows.Close()
	out := []ConceptShelfRow{}
	for rows.Next() {
		var row ConceptShelfRow
		if err := rows.Scan(&row.Slug, &row.Title, &row.SortKey, &row.Places); err != nil {
			return nil, fmt.Errorf("витрина понятий, разбор строки: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("витрина понятий: %w", err)
	}
	return out, nil
}

// Collections отдаёт ВСЕ подборки без фильтра — включая читательские и
// черновики, — в отличие от CollectionRepository.List (тот отбор уже
// применяет). Отбор публичных (сотруднических, опубликованных) сюда
// намеренно не перенесён: он живёт в internal/seo (publicCollectionRows),
// одной функцией на карту сайта и общую карточку, чтобы они не разъехались
// — и виден фиктивному Catalog в тестах пакета seo, где сломанный фильтр
// ловит мутационный тест. SQL-фильтр здесь стал бы вторым, независимым от
// первого, и со временем разошёлся бы с ним молча.
func (r *SEORepository) Collections(ctx context.Context) ([]CatalogRow, error) {
	return r.query(ctx, "подборки", `SELECT slug, author_nickname, published_at, updated_at FROM collections ORDER BY slug`, func(rows pgx.Rows) (CatalogRow, error) {
		var row CatalogRow
		err := rows.Scan(&row.Slug, &row.AuthorNickname, &row.PublishedAt, &row.UpdatedAt)
		return row, err
	})
}

// Documents отдаёт ВСЕ разборы без фильтра — черновики и снятые тоже, — тем
// же приёмом, что и Collections: отбор публичных (только published_at IS NOT
// NULL, БЕЗ учёта author_nickname — читательский разбор считается наравне с
// сотрудническим, см. isPublicDocumentRow в internal/seo) живёт в
// internal/seo, а не здесь, чтобы фиктивный Catalog тестов пакета seo видел
// его и ловил тест на мутацию.
func (r *SEORepository) Documents(ctx context.Context) ([]CatalogRow, error) {
	return r.query(ctx, "разборы", `SELECT slug, author_nickname, published_at, updated_at FROM documents ORDER BY slug`, func(rows pgx.Rows) (CatalogRow, error) {
		var row CatalogRow
		err := rows.Scan(&row.Slug, &row.AuthorNickname, &row.PublishedAt, &row.UpdatedAt)
		return row, err
	})
}
