package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
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

	p, err := loadPolicy(ctx, pool, "", "", "")
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
// journal_issues, pages, chapters и представления закрытого — свои, в отдельной схеме. Таблицы копируются
// LIKE (без внешних ключей — владелец работ не нужен), представление — его
// настоящим определением.
func isolatedPolicyPool(ctx context.Context, t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	views := map[string]string{}
	for _, v := range []string{"closed_works", "closed_chapters", "closed_pages"} {
		var def string
		if err := admin.QueryRow(ctx, `SELECT pg_get_viewdef($1::regclass)`, "public."+v).Scan(&def); err != nil {
			t.Fatal(err)
		}
		views[v] = def
	}
	schema := fmt.Sprintf("static_policy_%d", time.Now().UnixNano())
	stmts := []string{fmt.Sprintf(`CREATE SCHEMA %s`, schema)}
	for _, table := range []string{"works", "editions", "journals", "journal_issues", "pages", "chapters"} {
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
	for _, v := range []string{"closed_works", "closed_chapters", "closed_pages"} {
		if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE VIEW %s.%s AS %s`, schema, v, views[v])); err != nil {
			t.Fatal(err)
		}
	}
	var where string
	if err := pool.QueryRow(ctx, `SELECT n.nspname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.oid = 'closed_works'::regclass`).Scan(&where); err != nil || where != schema {
		t.Fatalf("closed_works резолвится в %q, не в схему теста: %v", where, err)
	}
	return pool
}

// policyWorld — работа с шестью полосами и двумя главами; вторая закрыта.
func policyWorld(ctx context.Context, t *testing.T, dsn string) (int64, *pgxpool.Pool) {
	t.Helper()
	pool := isolatedPolicyPool(ctx, t, dsn)
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO works (title, file_path, language, status, role, owner_id)
		VALUES ('том', '', 'ru', 'draft', 'volume', 1) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 6; n++ {
		if _, err := pool.Exec(ctx, `INSERT INTO pages (work_id, page_number, content_markdown, status)
			VALUES ($1, $2, 'т', 'не_вычитана')`, id, n); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		title      string
		start, end int
		closed     bool
	}{{"открытая", 1, 3, false}, {"закрытая", 4, 5, true}} {
		if _, err := pool.Exec(ctx, `INSERT INTO chapters (work_id, title, type, order_number, start_page, end_page, closed)
			VALUES ($1, $2, 'chapter', 1, $3, $4, $5)`, id, c.title, c.start, c.end, c.closed); err != nil {
			t.Fatal(err)
		}
	}
	return id, pool
}

func TestPolicyExcludesLocallyClosedChapterPages(t *testing.T) {
	dsn := os.Getenv("PROOFREADER_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set PROOFREADER_TEST_DB_URL to a throwaway DB to run this test")
	}
	ctx := context.Background()
	id, pool := policyWorld(ctx, t, dsn)
	p, err := loadPolicy(ctx, pool, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]bool{4: true, 5: true}
	if !reflect.DeepEqual(p.closedPages[id], want) {
		t.Errorf("closedPages[%d] = %v, хотел %v", id, p.closedPages[id], want)
	}
	if p.exclude[id] {
		t.Error("работа с закрытой главой исключена целиком")
	}
}

func TestPolicyExcludesPagesMissingOnProd(t *testing.T) {
	dsn := os.Getenv("PROOFREADER_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set PROOFREADER_TEST_DB_URL to a throwaway DB to run this test")
	}
	ctx := context.Background()
	id, pool := policyWorld(ctx, t, dsn)
	dir := t.TempDir()
	only := filepath.Join(dir, "only.json")
	pages := filepath.Join(dir, "pages.json")
	if err := os.WriteFile(only, []byte(fmt.Sprintf(`[{"id": %d, "title": "том"}]`, id)), 0o644); err != nil {
		t.Fatal(err)
	}
	// На боевом нет полосы 2 (закрыта кнопкой там) и 6 (ещё не выложена);
	// 4 и 5 закрыты локально.
	if err := os.WriteFile(pages, []byte(fmt.Sprintf(`{"%d": [1, 3, 4, 5]}`, id)), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := loadPolicy(ctx, pool, "", only, pages)
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]bool{2: true, 4: true, 5: true, 6: true}
	if !reflect.DeepEqual(p.closedPages[id], want) {
		t.Errorf("closedPages[%d] = %v, хотел %v", id, p.closedPages[id], want)
	}

	// Работа из -only без записи в -only-pages — отказ, а не «все полосы есть».
	if err := os.WriteFile(pages, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPolicy(ctx, pool, "", only, pages); err == nil {
		t.Error("работа без полос боевого принята")
	}
}

func TestMissingOnProd(t *testing.T) {
	local := map[int64][]int{1: {1, 2, 3, 4}, 2: {1, 2}}
	prod := map[int64][]int{1: {1, 3}, 2: {1, 2}}
	got := missingOnProd(local, prod)
	if want := (map[int64]map[int]bool{1: {2: true, 4: true}}); !reflect.DeepEqual(got, want) {
		t.Errorf("missingOnProd = %v, хотел %v", got, want)
	}
}

func TestPageSpans(t *testing.T) {
	got := pageSpans(map[int]bool{7: true, 2: true, 3: true, 4: true, 9: true})
	want := []models.PageSpan{{Start: 2, End: 4}, {Start: 7, End: 7}, {Start: 9, End: 9}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pageSpans = %v, хотел %v", got, want)
	}
}

// Служебная работа тома (передние листы) — в карте боевого или нет. Нет карты
// (404: закрыта там или не выложена) — работа из архива уходит целиком; есть —
// фильтруется по полосам, как любая.
func TestPolicyFrontMatterMissingOnProdIsLeftOut(t *testing.T) {
	dsn := os.Getenv("PROOFREADER_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set PROOFREADER_TEST_DB_URL to a throwaway DB to run this test")
	}
	ctx := context.Background()
	id, pool := policyWorld(ctx, t, dsn)
	child := func(title string) int64 {
		var c int64
		if err := pool.QueryRow(ctx, `INSERT INTO works (title, file_path, language, status, role, owner_id, parent_work_id)
			VALUES ($1, '', 'ru', 'draft', 'front_matter', 1, $2) RETURNING id`, title, id).Scan(&c); err != nil {
			t.Fatal(err)
		}
		for n := 1; n <= 2; n++ {
			if _, err := pool.Exec(ctx, `INSERT INTO pages (work_id, page_number, content_markdown, status)
				VALUES ($1, $2, 'т', 'не_вычитана')`, c, n); err != nil {
				t.Fatal(err)
			}
		}
		return c
	}
	shown, hidden := child("листы открытые"), child("листы закрытые на боевом")

	dir := t.TempDir()
	only := filepath.Join(dir, "only.json")
	pages := filepath.Join(dir, "pages.json")
	if err := os.WriteFile(only, []byte(fmt.Sprintf(`[{"id": %d, "title": "том"}]`, id)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pages, []byte(fmt.Sprintf(`{"%d": [1,2,3,4,5,6], "%d": [1]}`, id, shown)), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := loadPolicy(ctx, pool, "", only, pages)
	if err != nil {
		t.Fatal(err)
	}
	if !p.only[shown] || p.exclude[shown] {
		t.Errorf("служебная работа с картой боевого выпала: only=%v exclude=%v", p.only[shown], p.exclude[shown])
	}
	if p.only[hidden] || !p.exclude[hidden] {
		t.Errorf("служебная работа без карты боевого осталась в составе: only=%v exclude=%v", p.only[hidden], p.exclude[hidden])
	}
	for _, x := range p.onlyIDs() {
		if x == hidden {
			t.Error("она же в onlyIDs (Allowed самопроверки)")
		}
	}
	if want := (map[int]bool{2: true}); !reflect.DeepEqual(p.closedPages[shown], want) {
		t.Errorf("полосы служебной работы: %v, хотел %v", p.closedPages[shown], want)
	}

	// Вклейка разбора и статья указателя из работы, которой нет в архиве
	// целиком, недоступны: это служебная без карты боевого и работа вне -only.
	var stranger int64
	if err := pool.QueryRow(ctx, `INSERT INTO works (title, file_path, language, status, role, owner_id)
		VALUES ('чужая', '', 'ru', 'draft', 'volume', 1) RETURNING id`).Scan(&stranger); err != nil {
		t.Fatal(err)
	}
	whole, err := p.wholeWorksOut(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if !whole[hidden] || !whole[stranger] || whole[id] || whole[shown] {
		t.Errorf("wholeWorksOut = %v: ждали служебную %d и вне -only %d, но не %d и %d", whole, hidden, stranger, id, shown)
	}
}
