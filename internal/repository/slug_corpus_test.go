package repository

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/pkg/slug"
)

// corpusDBEnv — база с ЖИВЫМ корпусом, только на чтение. Это не
// PROOFREADER_TEST_DB_URL: та одноразовая и опустошается перед каждым тестом
// пакета, а здешним двум проверкам нужны настоящие данные — на пустой базе
// они бессмысленны. Разводить их было обязательно: пока корпусные тесты
// ходили в ту же базу, они «проходили» на трёх понятиях, насеянных соседней
// фикстурой, то есть проверяли выдумку вместо корпуса.
//
// Оба теста только SELECT-ят, поэтому сюда законно подставить и рабочую базу.
const corpusDBEnv = "PROOFREADER_CORPUS_DB_URL"

// corpusPool открывает пул к корпусу или пропускает тест. Пропуск — не успех:
// в CI переменная обязана быть задана.
func corpusPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv(corpusDBEnv)
	if url == "" {
		t.Skip(corpusDBEnv + " не задан: некуда идти за живым корпусом")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("подключение: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// Слаги понятий сделаны питоновым index_parser.slugify. Если правило в
// pkg/slug разошлось с ним, это видно на настоящих данных, а не на пяти
// придуманных строках.
func TestSlugMatchesExistingConceptSlugs(t *testing.T) {
	pool := corpusPool(t)

	rows, err := pool.Query(context.Background(),
		`SELECT title, slug FROM index_concepts WHERE kind = 'article'`)
	if err != nil {
		t.Fatalf("запрос понятий: %v", err)
	}
	defer rows.Close()

	checked, mismatched := 0, 0
	for rows.Next() {
		var title, stored string
		if err := rows.Scan(&title, &stored); err != nil {
			t.Fatalf("разбор строки: %v", err)
		}
		checked++
		// Потолок 60 применяется и здесь: расхождение по длине законно, по
		// содержанию — нет.
		want := stored
		if len(want) > slug.MaxLen {
			continue
		}
		if got := slug.Text(title); got != want {
			mismatched++
			if mismatched <= 10 {
				t.Errorf("%q: pkg/slug даёт %q, в базе %q", title, got, want)
			}
		}
	}
	if checked == 0 {
		t.Fatal("ни одного понятия не проверено — база пуста, тест бесполезен")
	}
	t.Logf("проверено понятий: %d, расхождений: %d", checked, mismatched)
}

// Понятия проверяют согласие с прежним правилом; главы — что правило не
// даёт мусора ни на одном из живых заголовков, включая тот, что на 375
// знаков (глава тома 25).
func TestChapterSlugsStayWithinLimitOnCorpus(t *testing.T) {
	pool := corpusPool(t)

	rows, err := pool.Query(context.Background(), `SELECT title FROM chapters`)
	if err != nil {
		t.Fatalf("запрос глав: %v", err)
	}
	defer rows.Close()

	checked, withSlug := 0, 0
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			t.Fatalf("разбор строки: %v", err)
		}
		checked++
		got := slug.Chapter(title)
		if got == "" {
			continue
		}
		withSlug++
		if len(got) > slug.MaxLen {
			t.Fatalf("%q: слаг длиной %d превысил потолок: %q", title, len(got), got)
		}
		if strings.HasPrefix(got, "-") || strings.HasSuffix(got, "-") {
			t.Fatalf("%q: слаг с дефисом на краю: %q", title, got)
		}
		if strings.Contains(got, "--") {
			t.Fatalf("%q: сдвоенный дефис: %q", title, got)
		}
	}
	if checked == 0 {
		t.Fatal("ни одной главы не проверено — база пуста, тест бесполезен")
	}
	t.Logf("проверено глав: %d, со слагом: %d, без слага (нумераторы): %d",
		checked, withSlug, checked-withSlug)
}
