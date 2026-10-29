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

// deleteAllMarginNotes удаляет все заметки читателей: шаг вниз 000045 при
// хоть одной отказывает. Таблицы может не быть (база ниже 45) — тогда
// делать нечего.
func deleteAllMarginNotes(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var has bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.margin_notes') IS NOT NULL`).Scan(&has); err != nil {
		t.Fatal(err)
	}
	if !has {
		return
	}
	if _, err := pool.Exec(ctx, `DELETE FROM margin_notes`); err != nil {
		t.Fatal(err)
	}
}

// TestMarginNotesMigrationDownRefuses: шаг вниз 000045 при хоть одной заметке
// ОТКАЗЫВАЕТ и ничего не меняет; без заметок проходит, повторный up — тоже.
//
//	PROOFREADER_MIGRATE_TEST=1 go test ./internal/database/ -run TestMarginNotesMigrationDownRefuses -v
func TestMarginNotesMigrationDownRefuses(t *testing.T) {
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
	hasTable := func() bool {
		var has bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass('public.margin_notes') IS NOT NULL`).Scan(&has); err != nil {
			t.Fatal(err)
		}
		return has
	}

	up()
	top, _, _ := m.Version()
	if top < 45 {
		t.Fatalf("после up версия %d, ждали не ниже 45", top)
	}
	clearDownRefusals(ctx, t, pool)
	if !hasTable() {
		t.Fatal("после up таблицы margin_notes нет")
	}

	// Заметка о всём томе: нужны читатель и том.
	var userID, workID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, role, nickname)
		VALUES ('mig-margin-notes@example.invalid', 'x', 'reader', 'mig-margin-notes') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID) })
	if err := pool.QueryRow(ctx, `INSERT INTO works (title, file_path, owner_id) VALUES ('mig-margin-notes', '', $1) RETURNING id`, userID).Scan(&workID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM works WHERE id = $1`, workID) })
	if _, err := pool.Exec(ctx, `INSERT INTO margin_notes (owner_id, work_id, target, body)
		VALUES ($1, $2, 'work', 'заметка')`, userID, workID); err != nil {
		t.Fatal(err)
	}

	// Проверки формы: цитата без якоря и глава без заглавия отвергаются.
	if _, err := pool.Exec(ctx, `INSERT INTO margin_notes (owner_id, work_id, target, body)
		VALUES ($1, $2, 'quote', 'x')`, userID, workID); err == nil {
		t.Fatal("цитата без номеров и якоря прошла")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO margin_notes (owner_id, work_id, target, body)
		VALUES ($1, $2, 'chapter', 'x')`, userID, workID); err == nil {
		t.Fatal("глава без заглавия прошла")
	}

	err = m.Steps(-1)
	if err == nil {
		t.Fatal("шаг вниз при залитой заметке прошёл, ждали отказ")
	}
	if !strings.Contains(err.Error(), "шаг вниз 000045") {
		t.Fatalf("отказ не из DO-блока down.sql: %v", err)
	}
	if !hasTable() {
		t.Fatal("отказ шага вниз снёс таблицу margin_notes")
	}
	var left int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM margin_notes WHERE owner_id = $1`, userID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 1 {
		t.Fatalf("отказ шага вниз унёс заметку: осталось %d", left)
	}
	if v, dirty, _ := m.Version(); !dirty {
		t.Fatalf("после отказа версия %d не грязная — golang-migrate повёл себя иначе, чем описано в down.sql", v)
	}
	if err := m.Force(int(top)); err != nil {
		t.Fatal(err)
	}

	deleteAllMarginNotes(ctx, t, pool)
	if err := m.Steps(-1); err != nil {
		t.Fatalf("down без заметок: %v", err)
	}
	if hasTable() {
		t.Fatal("после down таблица margin_notes осталась")
	}
	up()
	if !hasTable() {
		t.Fatal("после повторного up таблицы margin_notes нет")
	}
	if v, dirty, _ := m.Version(); v != top || dirty {
		t.Fatalf("итог: версия %d dirty=%v, ждали %d чистую", v, dirty, top)
	}
}
