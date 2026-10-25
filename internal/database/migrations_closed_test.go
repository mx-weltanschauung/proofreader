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

const closedMigrationVersion = 40

// openAllClosed снимает признак «закрыт» со всех работ, изданий и журналов.
// Зовётся в каждом круговом тесте миграций сразу после up(), до любого шага
// вниз: репозиторные тесты чистят базу в начале, а не в конце, и их закрытые
// строки лежат тут до следующего прогона — а шаг вниз 000040 при закрытом
// отказывает, оставляя базу «39, dirty», и чужой круговой тест (звук,
// журналы, читатели, поиск) падал бы на отказе, к нему не относящемся. База
// одноразовая по уговору PROOFREADER_TEST_DB_URL, открывать в ней можно всё.
// Столбца closed может не быть (база ниже 40) — такая таблица пропускается.
func openAllClosed(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	for _, table := range []string{"works", "editions", "journals", "chapters"} {
		var has bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1 AND column_name = 'closed')`, table).Scan(&has); err != nil {
			t.Fatal(err)
		}
		if !has {
			continue
		}
		if _, err := pool.Exec(ctx, `UPDATE `+table+` SET closed = false WHERE closed`); err != nil {
			t.Fatal(err)
		}
	}
}

// TestClosedMigrationDownRefuses: шаг вниз 000040 при закрытой работе,
// издании или журнале ОТКАЗЫВАЕТ (снос столбцов открыл бы их всем) и ничего
// не меняет; когда закрытого нет — проходит, повторный up — тоже.
//
//	PROOFREADER_MIGRATE_TEST=1 go test ./internal/database/ -run TestClosedMigrationDownRefuses -v
func TestClosedMigrationDownRefuses(t *testing.T) {
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
	columns := func() int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
			WHERE table_schema = 'public' AND column_name = 'closed'
			  AND table_name IN ('works','editions','journals')`).Scan(&n); err != nil {
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
	if got := columns(); got != 3 {
		t.Fatalf("после up столбцов closed %d, ждали 3", got)
	}
	// Чужое закрытое держало бы отказ за этот тест — открываем его, иначе
	// каждая проверка ниже ничего не говорит. Чужие события держали бы отказ
	// миграции сверху (000041), через которую идёт шаг вниз, — их тоже нет.
	clearDownRefusals(ctx, t, pool)
	top, _ := version()

	var ownerID, workID, editionID, journalID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM users ORDER BY id LIMIT 1`).Scan(&ownerID); err != nil {
		// Репозиторные тесты усекают users — заводим пользователя сами, иначе
		// проверка отказа молча обнулилась бы.
		if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, role)
			VALUES ('mig-closed@proofreader.local', 'x', 'editor') RETURNING id`).Scan(&ownerID); err != nil {
			t.Fatalf("нет пользователя для owner_id и завести не вышло: %v", err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, ownerID) })
	}
	if err := pool.QueryRow(ctx, `INSERT INTO works (title, author, language, country, file_path, status, owner_id)
		VALUES ('Проба закрытого', '', 'ru', '', '', 'draft', $1) RETURNING id`, ownerID).Scan(&workID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO editions (title, slug) VALUES ('Проба', 'mig-closed') RETURNING id`).Scan(&editionID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO journals (slug, title) VALUES ('mig-closed', 'Проба') RETURNING id`).Scan(&journalID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM works WHERE id = $1`, workID)
		_, _ = pool.Exec(ctx, `DELETE FROM editions WHERE id = $1`, editionID)
		_, _ = pool.Exec(ctx, `DELETE FROM journals WHERE id = $1`, journalID)
	})

	// Каждая из трёх сущностей порознь держит отказ: условие в DO-блоке —
	// три EXISTS, и каждое обязано работать само.
	for _, c := range []struct{ name, close, open string }{
		{"работа", `UPDATE works SET closed = true WHERE id = $1`, `UPDATE works SET closed = false WHERE id = $1`},
		{"издание", `UPDATE editions SET closed = true WHERE id = $1`, `UPDATE editions SET closed = false WHERE id = $1`},
		{"журнал", `UPDATE journals SET closed = true WHERE id = $1`, `UPDATE journals SET closed = false WHERE id = $1`},
	} {
		id := map[string]int64{"работа": workID, "издание": editionID, "журнал": journalID}[c.name]
		if _, err := pool.Exec(ctx, c.close, id); err != nil {
			t.Fatal(err)
		}
		err := m.Migrate(closedMigrationVersion - 1)
		if err == nil {
			t.Fatalf("%s закрыт(о), а шаг вниз прошёл — ждали отказ", c.name)
		}
		if !strings.Contains(err.Error(), "шаг вниз 000040") {
			t.Fatalf("%s: отказ не из DO-блока down.sql: %v", c.name, err)
		}
		if got := columns(); got != 3 {
			t.Fatalf("%s: отказ шага вниз изменил схему: столбцов closed %d", c.name, got)
		}
		// golang-migrate пометил целевую версию грязной до выполнения файла.
		if v, dirty := version(); !dirty {
			t.Fatalf("%s: после отказа версия %d не грязная — golang-migrate повёл себя иначе, чем описано в down.sql", c.name, v)
		}
		if err := m.Force(closedMigrationVersion); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, c.open, id); err != nil {
			t.Fatal(err)
		}
	}

	// Закрытого нет — шаг вниз проходит, столбцы уходят, up возвращает их.
	if err := m.Migrate(closedMigrationVersion - 1); err != nil {
		t.Fatalf("down без закрытого: %v", err)
	}
	if got := columns(); got != 0 {
		t.Fatalf("после down столбцов closed %d, ждали 0", got)
	}
	up()
	if got := columns(); got != 3 {
		t.Fatalf("после повторного up столбцов closed %d", got)
	}
	// up возвращает базу на верх цепочки, а не на 40: миграции сверху
	// (000041 и дальше) шаг вниз снял вместе с 000040.
	if v, dirty := version(); v != top || dirty {
		t.Fatalf("итог: версия %d dirty=%v, ждали %d чистую", v, dirty, top)
	}
}
