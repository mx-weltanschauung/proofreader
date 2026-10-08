package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Пустую базу и владельца (id = 1) готовит testPool — см. main_test.go.
func newSEOTestRepo(t *testing.T) (*SEORepository, *pgxpool.Pool, context.Context) {
	t.Helper()
	pool := testPool(t)
	return NewSEORepository(pool), pool, context.Background()
}

// Служебные передние листы (role = front_matter) в каталоге не показываются и
// в карте сайта им места нет. Предисловие к изданию (edition_front_matter)
// читатель видит — оно остаётся.
func TestSEOWorksExcludeFrontMatterOnly(t *testing.T) {
	repo, pool, ctx := newSEOTestRepo(t)

	if _, err := pool.Exec(ctx, `
		INSERT INTO editions (id, title, slug) VALUES (1, 'Издание', 'izdanie');
		INSERT INTO works (id, title, author, file_path, owner_id, role, parent_work_id, edition_id)
		VALUES
		  (10, 'Том 1',            'Автор', 'f', 1, 'volume',               NULL, 1),
		  (11, 'Передние листы',   'Автор', 'f', 1, 'front_matter',           10, NULL),
		  (12, 'Предисловие',      'Автор', 'f', 1, 'edition_front_matter', NULL, 1)`); err != nil {
		t.Fatalf("подготовка данных: %v", err)
	}

	rows, err := repo.Works(ctx)
	if err != nil {
		t.Fatalf("Works: %v", err)
	}
	got := map[int64]bool{}
	for _, r := range rows {
		got[r.ID] = true
	}
	if !got[10] || !got[12] {
		t.Errorf("том и предисловие к изданию обязаны быть в карте, получено %v", got)
	}
	if got[11] {
		t.Errorf("служебные передние листы попали в карту сайта")
	}
}

// Главы служебной работы уходят вместе с ней: индексировать главу страницы,
// которой нет в каталоге, незачем.
func TestSEOChaptersFollowTheirWork(t *testing.T) {
	repo, pool, ctx := newSEOTestRepo(t)

	if _, err := pool.Exec(ctx, `
		INSERT INTO works (id, title, author, file_path, owner_id, role, parent_work_id)
		VALUES (10, 'Том 1', 'Автор', 'f', 1, 'volume', NULL),
		       (11, 'Листы', 'Автор', 'f', 1, 'front_matter', 10);
		INSERT INTO chapters (id, work_id, title, type, order_number, start_page, end_page)
		VALUES (100, 10, 'Глава тома', 'chapter', 1, 1, 5),
		       (101, 11, 'Глава листов', 'chapter', 1, 1, 2)`); err != nil {
		t.Fatalf("подготовка данных: %v", err)
	}

	rows, err := repo.Chapters(ctx)
	if err != nil {
		t.Fatalf("Chapters: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ожидалась одна глава, получено %d: %+v", len(rows), rows)
	}
	if rows[0].ID != 100 || rows[0].WorkID != 10 {
		t.Errorf("ожидалась глава 100 работы 10, получено id=%d work=%d", rows[0].ID, rows[0].WorkID)
	}
}

// Перенаправления указателя («Труд — см. Абстрактный труд») собственной
// статьи не имеют: индексировать нечего.
func TestSEOConceptsSkipRedirects(t *testing.T) {
	repo, pool, ctx := newSEOTestRepo(t)

	if _, err := pool.Exec(ctx, `
		INSERT INTO editions (id, title, slug) VALUES (1, 'Издание', 'izdanie');
		INSERT INTO works (id, title, author, file_path, owner_id, edition_id)
		VALUES (10, 'Том', 'Автор', 'f', 1, 1);
		-- kind снят с index_concepts миграцией 000026 (задача 11) — статьёй
		-- понятие делает наличие строки в index_concept_articles с
		-- kind='article'. «Труд» — перенаправление, и реальный ввоз (import
		-- всегда заводит статью для КАЖДОГО понятия) даёт ему свою статью с
		-- kind='redirect' — не отсутствие статьи вовсе. Раунд правок 1,
		-- находка 3: прежняя фикстура давала «Труду» ноль статей, и запрос
		-- отфильтровывал его самим EXISTS, не проверяя kind ни разу —
		-- убери условие a.kind = 'article' из SQL, и тест остался бы
		-- зелёным. Здесь оба понятия имеют статью, различающую их только
		-- kind'ом, — так тест ловит именно фильтр по kind.
		WITH c AS (
			INSERT INTO index_concepts (title, slug, sort_key, title_key)
			VALUES ('Абстрактный труд', 'abstraktnyj-trud', 'абстрактный труд', 'абстрактный труд'),
			       ('Труд',             'trud',             'труд',             'труд')
			RETURNING id, slug
		)
		INSERT INTO index_concept_articles (concept_id, edition_id, work_id, title, title_key, kind)
		SELECT id, 1, 10, 'Абстрактный труд', 'абстрактный труд', 'article'
		FROM c WHERE slug = 'abstraktnyj-trud'
		UNION ALL
		SELECT id, 1, 10, 'Труд', 'труд', 'redirect'
		FROM c WHERE slug = 'trud'`); err != nil {
		t.Fatalf("подготовка данных: %v", err)
	}

	rows, err := repo.Concepts(ctx)
	if err != nil {
		t.Fatalf("Concepts: %v", err)
	}
	if len(rows) != 1 || rows[0].Slug != "abstraktnyj-trud" {
		t.Fatalf("ожидалось одно понятие-статья, получено %+v", rows)
	}
}

// Витрина .md: только понятия-статьи, по sort_key, с числом адресов.
func TestSEOConceptShelfCountsPlacesSkipsRedirects(t *testing.T) {
	repo, pool, ctx := newSEOTestRepo(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO editions (id, title, slug) VALUES (1, 'Издание', 'izdanie');
		INSERT INTO works (id, title, author, file_path, owner_id, edition_id)
		VALUES (10, 'Том', 'Автор', 'f', 1, 1);
		INSERT INTO index_concepts (id, title, slug, sort_key, title_key) VALUES
		  (1, 'Труд', 'trud', 'труд', 'труд'),
		  (2, 'Абстрактный труд', 'abstraktnyj-trud', 'абстрактный труд', 'абстрактный труд'),
		  (3, 'Ящик', 'yashchik', 'ящик', 'ящик');
		INSERT INTO index_concept_articles (id, concept_id, edition_id, work_id, title, title_key, kind) VALUES
		  (1, 1, 1, 10, 'Труд', 'труд', 'redirect'),
		  (2, 2, 1, 10, 'Абстрактный труд', 'абстрактный труд', 'article'),
		  (3, 3, 1, 10, 'Ящик', 'ящик', 'article');
		INSERT INTO index_references (article_id, volume_number, page_start, page_end, order_number) VALUES
		  (2, 23, 46, 47, 1), (2, 13, 16, 18, 2)`); err != nil {
		t.Fatalf("подготовка данных: %v", err)
	}
	rows, err := repo.ConceptShelf(ctx)
	if err != nil {
		t.Fatalf("ConceptShelf: %v", err)
	}
	if len(rows) != 2 || rows[0].Slug != "abstraktnyj-trud" || rows[0].Places != 2 ||
		rows[1].Slug != "yashchik" || rows[1].Places != 0 || rows[0].Title != "Абстрактный труд" {
		t.Fatalf("получено %+v", rows)
	}
}
