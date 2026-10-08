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

// audioMigrationVersion — номер аудиомиграции; откат адресуется номером,
// как у readerAccountsMigrationVersion.
const audioMigrationVersion = 35

// TestAudioMigrationRoundTrip: up даёт три таблицы и частичный индекс
// незавершённых заявок, down их снимает, повторный up проходит.
//
//	PROOFREADER_MIGRATE_TEST=1 go test ./internal/database/ -run TestAudioMigrationRoundTrip -v
func TestAudioMigrationRoundTrip(t *testing.T) {
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
			AND tablename IN ('audio_queue','audio_tracks','audio_recordings')`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	up()
	if got := tables(); got != 3 {
		t.Fatalf("после up таблиц %d, ждали 3", got)
	}
	var idx int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_indexes
		WHERE indexname = 'audio_queue_open_key' AND indexdef LIKE '%WHERE%'`).Scan(&idx); err != nil || idx != 1 {
		t.Fatalf("частичного индекса незавершённых заявок нет: %d, %v", idx, err)
	}
	if err := m.Migrate(audioMigrationVersion - 1); err != nil {
		t.Fatalf("down: %v", err)
	}
	if got := tables(); got != 0 {
		t.Fatalf("после down таблиц %d, ждали 0", got)
	}
	up()
	if got := tables(); got != 3 {
		t.Fatalf("после повторного up таблиц %d", got)
	}
}
