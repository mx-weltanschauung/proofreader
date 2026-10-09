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

const journalsMigrationVersion = 38

// TestJournalsMigrationRoundTrip: up даёт четыре таблицы и роль номера;
// шаг вниз при залитом номере ОТКАЗЫВАЕТ и ничего не меняет; на пустых
// таблицах вниз проходит, повторный up — тоже.
//
//	PROOFREADER_MIGRATE_TEST=1 go test ./internal/database/ -run TestJournalsMigrationRoundTrip -v
func TestJournalsMigrationRoundTrip(t *testing.T) {
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
	tables := func() int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_tables WHERE schemaname = 'public'
			AND tablename IN ('journals','journal_issues','persons','article_credits')`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	up()
	if got := tables(); got != 4 {
		t.Fatalf("после up таблиц %d, ждали 4", got)
	}
	// Номер с работой: шаг вниз обязан отказать.
	var ownerID, journalID, workID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM users ORDER BY id LIMIT 1`).Scan(&ownerID); err != nil {
		// resetDB репозиторных тестов усекает users — пропуск молча обнулил бы
		// проверку отказа, поэтому заводим пользователя сами.
		if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, role)
			VALUES ('mig-probe@proofreader.local', 'x', 'editor') RETURNING id`).Scan(&ownerID); err != nil {
			t.Fatalf("нет пользователя для owner_id и завести не вышло: %v", err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, ownerID) })
	}
	if err := pool.QueryRow(ctx, `INSERT INTO journals (slug, title) VALUES ('mig-probe', 'Проба') RETURNING id`).Scan(&journalID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO works (title, author, language, country, file_path, status, role, owner_id)
		VALUES ('Проба, 1925, № 1', '', 'ru', '', '', 'draft', 'journal_issue', $1) RETURNING id`, ownerID).Scan(&workID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO journal_issues (journal_id, year, number_from, number_to, label, work_id)
		VALUES ($1, 1925, 1, 1, '1', $2)`, journalID, workID); err != nil {
		t.Fatal(err)
	}
	// Отказ должен прийти именно из DO-блока down.sql: без него шаг вниз тоже
	// упал бы, но на CHECK по works.role — случайно, а не по замыслу.
	err = m.Migrate(journalsMigrationVersion - 1)
	if err == nil {
		t.Fatal("шаг вниз при залитом номере прошёл, ждали отказ")
	}
	if !strings.Contains(err.Error(), "шаг вниз 000038") {
		t.Fatalf("отказ не из DO-блока down.sql: %v", err)
	}
	if got := tables(); got != 4 {
		t.Fatalf("отказ шага вниз изменил схему: таблиц %d", got)
	}
	// golang-migrate пометил версию грязной до выполнения файла — снимаем.
	if err := m.Force(journalsMigrationVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM works WHERE id = $1`, workID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM journals WHERE id = $1`, journalID); err != nil {
		t.Fatal(err)
	}
	if err := m.Migrate(journalsMigrationVersion - 1); err != nil {
		t.Fatalf("down на пустых таблицах: %v", err)
	}
	if got := tables(); got != 0 {
		t.Fatalf("после down таблиц %d, ждали 0", got)
	}
	up()
	if got := tables(); got != 4 {
		t.Fatalf("после повторного up таблиц %d", got)
	}
}
