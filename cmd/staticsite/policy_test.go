package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestStaticPolicyExcludesClosed — закрытая работа (сама, служебная закрытого
// тома, том закрытого издания) идёт в exclude по признаку локальной базы, без
// всякого списка: так сборка её пропускает, а самопроверка роняет выпуск,
// если она всё же попала. Открытая в exclude не идёт.
//
// База — одноразовая PROOFREADER_TEST_DB_URL, но не её public: ту параллельно
// опустошают тесты internal/repository (TRUNCATE перед каждым тестом), и общие
// строки ломали бы и их, и этот тест. Поэтому тест заводит свою схему —
// копии таблиц, из которых считается закрытость, и представление closed_works,
// снятое с настоящего (pg_get_viewdef), — и ставит её первой в search_path.
func TestStaticPolicyExcludesClosed(t *testing.T) {
	dsn := os.Getenv("PROOFREADER_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set PROOFREADER_TEST_DB_URL to a throwaway DB to run this test")
	}
	ctx := context.Background()
	pool := isolatedPolicyPool(ctx, t, dsn)

	var edition int64
	if err := pool.QueryRow(ctx, `INSERT INTO editions (title, slug, closed) VALUES ('закрытое', 'closed', true) RETURNING id`).
		Scan(&edition); err != nil {
		t.Fatal(err)
	}
	work := func(title string, closed bool, parent, edition *int64) int64 {
		var id int64
		role := "volume"
		if parent != nil {
			role = "front_matter"
		}
		if err := pool.QueryRow(ctx, `INSERT INTO works (title, file_path, language, status, role, owner_id, parent_work_id, edition_id, closed)
			VALUES ($1, '', 'ru', 'draft', $2, 1, $3, $4, $5) RETURNING id`,
			title, role, parent, edition, closed).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	open := work("открытая", false, nil, nil)
	closedSelf := work("закрытая", true, nil, nil)
	childOfClosed := work("листы закрытой", false, &closedSelf, nil)
	inClosedEdition := work("том закрытого издания", false, nil, &edition)

	p, err := loadPolicy(ctx, pool, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.exclude[open] {
		t.Error("открытая работа в exclude")
	}
	for name, id := range map[string]int64{
		"закрытая сама": closedSelf, "служебная закрытого тома": childOfClosed,
		"том закрытого издания": inClosedEdition,
	} {
		if !p.exclude[id] {
			t.Errorf("%s (%d) не в exclude: %v", name, id, p.excludedIDs())
		}
	}
}

// isolatedPolicyPool — пул, у которого works, editions, journals,
// journal_issues и closed_works — свои, в отдельной схеме. Таблицы копируются
// LIKE (без внешних ключей — владелец работ не нужен), представление — его
// настоящим определением.
func isolatedPolicyPool(ctx context.Context, t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	var view string
	if err := admin.QueryRow(ctx, `SELECT pg_get_viewdef('public.closed_works'::regclass)`).Scan(&view); err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("static_policy_%d", time.Now().UnixNano())
	stmts := []string{fmt.Sprintf(`CREATE SCHEMA %s`, schema)}
	for _, table := range []string{"works", "editions", "journals", "journal_issues"} {
		stmts = append(stmts, fmt.Sprintf(`CREATE TABLE %s.%s (LIKE public.%s INCLUDING DEFAULTS)`, schema, table, table))
	}
	for _, s := range stmts {
		if _, err := admin.Exec(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		cleanup, err := pgxpool.New(context.Background(), dsn)
		if err != nil {
			return
		}
		defer cleanup.Close()
		_, _ = cleanup.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA %s CASCADE`, schema))
	})

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ", public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	// Определение снято с public при search_path по умолчанию — имена в нём
	// не уточнены и теперь находят таблицы своей схемы.
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE VIEW %s.closed_works AS %s`, schema, view)); err != nil {
		t.Fatal(err)
	}
	var where string
	if err := pool.QueryRow(ctx, `SELECT n.nspname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.oid = 'closed_works'::regclass`).Scan(&where); err != nil || where != schema {
		t.Fatalf("closed_works резолвится в %q, не в схему теста: %v", where, err)
	}
	return pool
}
