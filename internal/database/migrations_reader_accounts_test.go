package database

import (
	"context"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Обход up → down → up миграции читательских учётных записей. Как и
// TestSearchMigrationRoundTrip, запускается отдельно и только по
// PROOFREADER_MIGRATE_TEST=1: тест сносит и восстанавливает enum user_role и
// столбцы users, а go test гоняет пакеты параллельно.
//
//	PROOFREADER_MIGRATE_TEST=1 go test ./internal/database/ -run TestReaderAccountsMigrationRoundTrip -v
//
// readerAccountsMigrationVersion — номер миграции читательских учёток.
// Откат адресуется номером, а не числом шагов, по той же причине, что у
// searchMigrationVersion — миграции сверху не должны его сбивать.
const readerAccountsMigrationVersion = 24

// TestReaderAccountsMigrationRoundTrip проверяет по существу находку
// рецензии: откат этой миграции обязан УДАЛЯТЬ читательские строки, а не
// повышать их до editor — повышение выдало бы читателю права правки полос
// без его согласия. Заодно проверяет побочный эффект удаления: подборка
// читателя не удаляется вместе с ним (collections.owner_id — ON DELETE SET
// NULL, заведено 000009 и этой миграцией не тронуто).
func TestReaderAccountsMigrationRoundTrip(t *testing.T) {
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
	up()

	const (
		fixtureReaderID     = 900000101
		fixtureCollectionID = 900000101
	)
	t.Cleanup(func() {
		// Строка читателя может уже не существовать (это и есть предмет
		// проверки) — DELETE по id молчит, если строки нет.
		_, _ = pool.Exec(ctx, `DELETE FROM collections WHERE id = $1`, fixtureCollectionID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, fixtureReaderID)
	})

	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, password_hash, role, nickname, signup_ip_hash)
		 VALUES ($1, 'x', 'reader', 'миграционный-читатель', 'stub')`,
		fixtureReaderID); err != nil {
		t.Fatalf("insert fixture reader: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO collections (id, title, slug, owner_id, author_nickname)
		 VALUES ($1, 'Подборка миграционного теста', 'migration-round-trip', $2, 'миграционный-читатель')`,
		fixtureCollectionID, fixtureReaderID); err != nil {
		t.Fatalf("insert fixture collection: %v", err)
	}

	if err := m.Migrate(readerAccountsMigrationVersion - 1); err != nil {
		t.Fatalf("down to %d: %v", readerAccountsMigrationVersion-1, err)
	}

	var readerCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = $1`, fixtureReaderID).Scan(&readerCount); err != nil {
		t.Fatalf("count reader after down: %v", err)
	}
	if readerCount != 0 {
		t.Errorf("после отката читатель id=%d всё ещё существует — откат обязан удалять читателей, а не переносить их в другую роль", fixtureReaderID)
	}

	var editorPromotions int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE id = $1 AND role = 'editor'`, fixtureReaderID,
	).Scan(&editorPromotions); err != nil {
		t.Fatalf("count promoted: %v", err)
	}
	if editorPromotions != 0 {
		t.Errorf("читатель id=%d повышен до editor на откате — это и есть находка рецензии, которую эта миграция обязана была закрыть", fixtureReaderID)
	}

	// Подборка обязана пережить владельца: ON DELETE SET NULL, а не каскад.
	var ownerIsNull bool
	if err := pool.QueryRow(ctx,
		`SELECT owner_id IS NULL FROM collections WHERE id = $1`, fixtureCollectionID,
	).Scan(&ownerIsNull); err != nil {
		t.Fatalf("collection survived: %v", err)
	}
	if !ownerIsNull {
		t.Error("owner_id подборки не обнулился после удаления читателя")
	}

	// Обратный ход: up() обязан провести через enum без 'reader' и снова
	// разрешить читателей, не спотыкаясь о уже удалённую тестовую строку.
	up()
	var hasReaderValue bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_enum e JOIN pg_type t ON t.oid = e.enumtypid
			WHERE t.typname = 'user_role' AND e.enumlabel = 'reader')`,
	).Scan(&hasReaderValue); err != nil {
		t.Fatalf("check enum: %v", err)
	}
	if !hasReaderValue {
		t.Error("после повторного up значение 'reader' не вернулось в user_role")
	}
}
