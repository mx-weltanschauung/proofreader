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

const eventsMigrationVersion = 41

// deleteAllEvents удаляет все события (пункты, строки и цели уходят каскадом).
// Шаг вниз 000041 при хоть одном событии отказывает, а репозиторные тесты
// чистят базу в начале, а не в конце, — их события лежат тут до следующего
// прогона, и любой круговой тест, идущий вниз через 41, падал бы на чужом
// отказе. Организации и виды отказа не держат и остаются. Таблицы может не
// быть (база ниже 41) — тогда делать нечего.
func deleteAllEvents(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var has bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.events') IS NOT NULL`).Scan(&has); err != nil {
		t.Fatal(err)
	}
	if !has {
		return
	}
	if _, err := pool.Exec(ctx, `DELETE FROM events`); err != nil {
		t.Fatal(err)
	}
}

// clearDownRefusals снимает всё, что держит отказ шагов вниз выше
// проверяемой миграции: закрытое (000040), события (000041) и заметки читателей (000044). Зовётся в
// каждом круговом тесте сразу после up(), до любого шага вниз. База
// одноразовая по уговору PROOFREADER_TEST_DB_URL.
func clearDownRefusals(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	openAllClosed(ctx, t, pool)
	deleteAllEvents(ctx, t, pool)
	deleteAllMarginNotes(ctx, t, pool)
}

// TestEventsMigrationDownRefuses: шаг вниз 000041 при хоть одном событии
// ОТКАЗЫВАЕТ (снос таблиц унёс бы раскладку повесток) и ничего не меняет;
// когда событий нет — проходит, повторный up — тоже.
//
//	PROOFREADER_MIGRATE_TEST=1 go test ./internal/database/ -run TestEventsMigrationDownRefuses -v
func TestEventsMigrationDownRefuses(t *testing.T) {
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
			AND tablename IN ('event_organizations','event_kinds','events',
			                  'event_sections','event_entries','event_targets')`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	version := func() (uint, bool) {
		v, dirty, err := m.Version()
		if err != nil {
			t.Fatal(err)
		}
		return v, dirty
	}

	up()
	top, _ := version()
	clearDownRefusals(ctx, t, pool)
	if got := tables(); got != 6 {
		t.Fatalf("после up таблиц событий %d, ждали 6", got)
	}

	var orgID, kindID, eventID int64
	if err := pool.QueryRow(ctx, `INSERT INTO event_organizations (slug, title)
		VALUES ('mig-events', 'Организация А') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM events WHERE kind_id IN (SELECT id FROM event_kinds WHERE organization_id = $1)`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM event_kinds WHERE organization_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM event_organizations WHERE id = $1`, orgID)
	})
	if err := pool.QueryRow(ctx, `INSERT INTO event_kinds (organization_id, title)
		VALUES ($1, 'Съезды') RETURNING id`, orgID).Scan(&kindID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO events (kind_id, slug, title)
		VALUES ($1, 'mig-events-1', 'I съезд') RETURNING id`, kindID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}

	// Событие есть — шаг вниз обязан отказать, и отказ обязан прийти из
	// DO-блока down.sql, а не случайно из чего-то ещё.
	err = m.Migrate(eventsMigrationVersion - 1)
	if err == nil {
		t.Fatal("шаг вниз при залитом событии прошёл, ждали отказ")
	}
	if !strings.Contains(err.Error(), "шаг вниз 000041") {
		t.Fatalf("отказ не из DO-блока down.sql: %v", err)
	}
	if got := tables(); got != 6 {
		t.Fatalf("отказ шага вниз изменил схему: таблиц событий %d", got)
	}
	var left int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE id = $1`, eventID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 1 {
		t.Fatalf("отказ шага вниз унёс событие: осталось %d", left)
	}
	// golang-migrate пометил целевую версию грязной до выполнения файла.
	if v, dirty := version(); !dirty {
		t.Fatalf("после отказа версия %d не грязная — golang-migrate повёл себя иначе, чем описано в down.sql", v)
	}
	if err := m.Force(eventsMigrationVersion); err != nil {
		t.Fatal(err)
	}

	// Событий нет — шаг вниз проходит (организация и вид отказа не держат),
	// таблицы уходят, up возвращает их.
	if _, err := pool.Exec(ctx, `DELETE FROM events WHERE id = $1`, eventID); err != nil {
		t.Fatal(err)
	}
	if err := m.Migrate(eventsMigrationVersion - 1); err != nil {
		t.Fatalf("down без событий: %v", err)
	}
	if got := tables(); got != 0 {
		t.Fatalf("после down таблиц событий %d, ждали 0", got)
	}
	up()
	if got := tables(); got != 6 {
		t.Fatalf("после повторного up таблиц событий %d", got)
	}
	if v, dirty := version(); v != top || dirty {
		t.Fatalf("итог: версия %d dirty=%v, ждали %d чистую", v, dirty, top)
	}
}
