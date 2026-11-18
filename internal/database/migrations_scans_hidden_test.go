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

// Опустить схему после скрытия сканов означало бы открыть изображения без
// административного действия либо назвать их снятие полным закрытием.
func TestScansHiddenMigrationDownRefusesHistory(t *testing.T) {
	dsn := os.Getenv("PROOFREADER_TEST_DB_URL")
	if dsn == "" || os.Getenv("PROOFREADER_MIGRATE_TEST") != "1" {
		t.Skip("requires a disposable PROOFREADER_TEST_DB_URL and PROOFREADER_MIGRATE_TEST=1")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	m, err := migrate.New("file://migrations", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = m.Close() })
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatal(err)
	}
	clearDownRefusals(ctx, t, pool)
	var columns int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_schema='public' AND column_name='scans_hidden'
		AND table_name IN ('works','chapters','editions','journals')`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if columns != 4 {
		t.Fatalf("scans_hidden columns=%d; want 4", columns)
	}
	var owner, work int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, role)
		VALUES ('scans-down@test.local', 'x', 'administrator') RETURNING id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO works (title, file_path, owner_id, scans_hidden)
		VALUES ('scans down test', '', $1, true) RETURNING id`, owner).Scan(&work); err != nil {
		t.Fatal(err)
	}
	refuseDown := func(label string) {
		t.Helper()
		if err := m.Migrate(45); err == nil {
			t.Fatalf("%s: down succeeded; hidden scans would be exposed", label)
		}
		var flag bool
		if err := pool.QueryRow(ctx, `SELECT scans_hidden FROM works WHERE id=$1`, work).Scan(&flag); err != nil || flag != (label == "flag") {
			t.Fatalf("%s: flag=%v err=%v", label, flag, err)
		}
		if err := m.Force(46); err != nil {
			t.Fatal(err)
		}
	}
	refuseDown("flag")
	if _, err := pool.Exec(ctx, `UPDATE works SET scans_hidden=false WHERE id=$1`, work); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO closures
		(entity_kind, entity_id, title_snapshot, closed, reason, actor_name, scope)
		VALUES ('work', $1, 'scans down test', false, 'opened', 'test', 'scans')`, work); err != nil {
		t.Fatal(err)
	}
	refuseDown("history")
	if _, err := pool.Exec(ctx, `DELETE FROM closures WHERE scope='scans' AND entity_id=$1`, work); err != nil {
		t.Fatal(err)
	}
	if err := m.Migrate(45); err != nil {
		t.Fatalf("down after removing scan history: %v", err)
	}
	if err := m.Up(); err != nil {
		t.Fatalf("restore migration: %v", err)
	}
}
