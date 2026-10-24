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

const closedChaptersMigrationVersion = 42

// TestClosedChaptersMigrationDownRefuses: шаг вниз 000042 при хоть одной
// закрытой главе ОТКАЗЫВАЕТ (снос столбца открыл бы её полосы всем) и ничего
// не меняет; когда закрытых глав нет — проходит (столбец, журнал, оба
// представления и частичный индекс уходят), повторный up возвращает всё.
//
//	PROOFREADER_MIGRATE_TEST=1 go test ./internal/database/ -run TestClosedChaptersMigrationDownRefuses -v
func TestClosedChaptersMigrationDownRefuses(t *testing.T) {
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
	// objects — сколько из пяти предметов миграции есть в схеме: столбец
	// chapters.closed, таблица closures, два представления, частичный индекс.
	objects := func() int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM information_schema.columns
			  WHERE table_schema = 'public' AND table_name = 'chapters' AND column_name = 'closed')
			+ (to_regclass('public.closures') IS NOT NULL)::int
			+ (to_regclass('public.closed_chapters') IS NOT NULL)::int
			+ (to_regclass('public.closed_pages') IS NOT NULL)::int
			+ (to_regclass('public.chapters_closed_idx') IS NOT NULL)::int`).Scan(&n); err != nil {
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
	clearDownRefusals(ctx, t, pool)
	top, _ := version()
	if got := objects(); got != 5 {
		t.Fatalf("после up предметов 000042 %d, ждали 5", got)
	}

	var ownerID, workID, chapterID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM users ORDER BY id LIMIT 1`).Scan(&ownerID); err != nil {
		if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, role)
			VALUES ('mig-closed-ch@proofreader.local', 'x', 'editor') RETURNING id`).Scan(&ownerID); err != nil {
			t.Fatalf("нет пользователя для owner_id и завести не вышло: %v", err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, ownerID) })
	}
	if err := pool.QueryRow(ctx, `INSERT INTO works (title, file_path, owner_id)
		VALUES ('Проба закрытой главы', '', $1) RETURNING id`, ownerID).Scan(&workID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM works WHERE id = $1`, workID) })
	if err := pool.QueryRow(ctx, `INSERT INTO chapters (work_id, title, type, order_number, start_page, end_page, closed)
		VALUES ($1, 'Речь', 'chapter', 1, 1, 2, true) RETURNING id`, workID).Scan(&chapterID); err != nil {
		t.Fatal(err)
	}

	err = m.Migrate(closedChaptersMigrationVersion - 1)
	if err == nil {
		t.Fatal("глава закрыта, а шаг вниз прошёл — ждали отказ")
	}
	if !strings.Contains(err.Error(), "шаг вниз 000042") {
		t.Fatalf("отказ не из DO-блока down.sql: %v", err)
	}
	if got := objects(); got != 5 {
		t.Fatalf("отказ шага вниз изменил схему: предметов 000042 %d", got)
	}
	var closed bool
	if err := pool.QueryRow(ctx, `SELECT closed FROM chapters WHERE id = $1`, chapterID).Scan(&closed); err != nil || !closed {
		t.Fatalf("отказ шага вниз тронул главу: closed=%v err=%v", closed, err)
	}
	if v, dirty := version(); !dirty {
		t.Fatalf("после отказа версия %d не грязная — golang-migrate повёл себя иначе, чем описано в down.sql", v)
	}
	if err := m.Force(closedChaptersMigrationVersion); err != nil {
		t.Fatal(err)
	}

	// Закрытых глав нет — шаг вниз проходит, предметы уходят, up их возвращает.
	if _, err := pool.Exec(ctx, `UPDATE chapters SET closed = false WHERE id = $1`, chapterID); err != nil {
		t.Fatal(err)
	}
	if err := m.Migrate(closedChaptersMigrationVersion - 1); err != nil {
		t.Fatalf("down без закрытых глав: %v", err)
	}
	if got := objects(); got != 0 {
		t.Fatalf("после down предметов 000042 %d, ждали 0", got)
	}
	up()
	if got := objects(); got != 5 {
		t.Fatalf("после повторного up предметов 000042 %d", got)
	}
	if v, dirty := version(); v != top || dirty {
		t.Fatalf("итог: версия %d dirty=%v, ждали %d чистую", v, dirty, top)
	}
}
