package database

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Обход up → down → up миграции поиска. Запускается отдельно и только по
// PROOFREADER_MIGRATE_TEST=1: тест сносит столбец search_vector, а go test
// гоняет пакеты параллельно — тесты репозитория поиска в это время упали бы.
//
//	PROOFREADER_MIGRATE_TEST=1 go test ./internal/database/ -run TestSearchMigrationRoundTrip -v
//
// searchMigrationVersion — номер миграции поиска. Откат в тесте адресуется
// номером, а не числом шагов, чтобы миграции, встающие поверх, его не ломали.
const searchMigrationVersion = 16

func TestSearchMigrationRoundTrip(t *testing.T) {
	dsn := os.Getenv("PROOFREADER_TEST_DB_URL")
	if dsn == "" || os.Getenv("PROOFREADER_MIGRATE_TEST") != "1" {
		t.Skip("set PROOFREADER_TEST_DB_URL and PROOFREADER_MIGRATE_TEST=1 to run the migration round trip")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	m, err := migrate.New("file://migrations", dsn)
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	t.Cleanup(func() { m.Close() })

	up := func() {
		if err := m.Up(); err != nil && err != migrate.ErrNoChange {
			t.Fatalf("up: %v", err)
		}
	}
	exists := func(sql string, args ...any) bool {
		var ok bool
		if err := pool.QueryRow(ctx, sql, args...).Scan(&ok); err != nil {
			t.Fatalf("check %q: %v", sql, err)
		}
		return ok
	}
	hasColumn := func() bool {
		return exists(`SELECT EXISTS (SELECT 1 FROM information_schema.columns
			WHERE table_name = 'pages' AND column_name = 'search_vector')`)
	}
	hasIndex := func(name string) bool {
		return exists(`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = $1)`, name)
	}
	hasConfig := func() bool {
		return exists(`SELECT EXISTS (SELECT 1 FROM pg_ts_config WHERE cfgname = 'ru')`)
	}

	up()
	if !hasColumn() || !hasIndex("idx_pages_search") || !hasConfig() || hasIndex("idx_pages_content") {
		t.Fatalf("after up: column=%v search=%v config=%v old=%v",
			hasColumn(), hasIndex("idx_pages_search"), hasConfig(), hasIndex("idx_pages_content"))
	}

	// Откат ИМЕННО до 15, а не Steps(-1): пока 000016 была последней, один шаг
	// вниз откатывал её, но над ней уже встала 000017, и один шаг стал
	// откатывать чужую миграцию — проверка ниже смотрела на неснесённые
	// объекты и падала. Явная цель переживает и следующие миграции сверху.
	if err := m.Migrate(searchMigrationVersion - 1); err != nil {
		t.Fatalf("down to %d: %v", searchMigrationVersion-1, err)
	}
	if hasColumn() || hasIndex("idx_pages_search") || hasIndex("idx_chapters_title_search") ||
		hasIndex("idx_index_concepts_title_search") || hasConfig() || !hasIndex("idx_pages_content") {
		t.Fatalf("after down: column=%v config=%v old=%v", hasColumn(), hasConfig(), hasIndex("idx_pages_content"))
	}

	up()
	if !hasColumn() || !hasIndex("idx_chapters_title_search") || !hasIndex("idx_index_concepts_title_search") {
		t.Fatal("after second up: search objects missing")
	}

	// Конфиг ru разбирает текст в ожидаемые леммы — проверяет unaccent+стеммер
	// и снятые стоп-слова (ё→е, «что»/«ещё» не выпадают), но ничего не
	// говорит про сам вычисляемый столбец pages.search_vector.
	wantLexemes := []string{"'что':1", "'дела':2", "'ещ':3", "'елк':4"}
	var lexemes string
	err = pool.QueryRow(ctx, `SELECT to_tsvector('ru', 'Что делать? Ещё ёлка')::text`).Scan(&lexemes)
	if err != nil {
		t.Fatalf("to_tsvector: %v", err)
	}
	for _, want := range wantLexemes {
		if !strings.Contains(lexemes, want) {
			t.Errorf("to_tsvector('ru') = %s, want %s inside", lexemes, want)
		}
	}

	// Настоящая проверка столбца: вставить полосу, ни разу не упомянув
	// search_vector, и прочитать его обратно — GENERATED ALWAYS AS (...)
	// STORED обязан посчитать его сам из content_markdown через конфиг ru.
	// id заведомо вне диапазона реальных данных и убираются в t.Cleanup,
	// чтобы не мешать TRUNCATE в тестах репозитория поиска (задачи 3-4).
	const (
		fixtureUserID = 900000001
		fixtureWorkID = 900000001
		fixturePageID = 900000001
	)
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM pages WHERE id = $1`, fixturePageID); err != nil {
			t.Errorf("cleanup: delete fixture page: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM works WHERE id = $1`, fixtureWorkID); err != nil {
			t.Errorf("cleanup: delete fixture work: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, fixtureUserID); err != nil {
			t.Errorf("cleanup: delete fixture user: %v", err)
		}
	})
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, $3)`,
		fixtureUserID, "search-migration-test@example.invalid", "x"); err != nil {
		t.Fatalf("insert fixture user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO works (id, title, file_path, owner_id) VALUES ($1, $2, $3, $4)`,
		fixtureWorkID, "search migration test work", "/dev/null", fixtureUserID); err != nil {
		t.Fatalf("insert fixture work: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO pages (id, work_id, page_number, content_markdown) VALUES ($1, $2, $3, $4)`,
		fixturePageID, fixtureWorkID, 1, "Что делать? Ещё ёлка"); err != nil {
		t.Fatalf("insert fixture page: %v", err)
	}

	var storedLexemes string
	if err := pool.QueryRow(ctx, `SELECT search_vector::text FROM pages WHERE id = $1`, fixturePageID).Scan(&storedLexemes); err != nil {
		t.Fatalf("read back search_vector: %v", err)
	}
	for _, want := range wantLexemes {
		if !strings.Contains(storedLexemes, want) {
			t.Errorf("pages.search_vector = %s, want %s inside (generated column did not populate from content_markdown on plain INSERT)",
				storedLexemes, want)
		}
	}
}
