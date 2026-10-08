package api

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

// TestIsCollectionSlugTakenErrorRecognizesRealConstraintViolation бьёт по
// живой одноразовой базе, а не по самодельному *pgconn.PgError: остальные
// тесты (TestCreateCollectionDuplicateSlugConflicts,
// TestUpdateCollectionDuplicateSlugConflicts) проверяют, что isCollectionSlugTakenError
// правильно превращает СВОЙ ЖЕ синтетический pgconn.PgError в 409 — то есть
// проверяют отображение кода в ответ, а не то, что настоящее нарушение
// индекса действительно называется collections_author_slug_key. Если бы имя
// разошлось (переименовали индекс в миграции, опечатались), эти тесты
// остались бы зелёными, а на боевом 409 молча стал бы 500 — тот же класс
// промаха, что нашёлся в TestDraftCollectionHiddenFromStranger: тест ходит
// мимо настоящего шва.
//
// Без PROOFREADER_TEST_DB_URL тест пропускается — рабочая база разработчика
// не должна пострадать (см. CLAUDE.md и internal/repository/main_test.go).
func TestIsCollectionSlugTakenErrorRecognizesRealConstraintViolation(t *testing.T) {
	dsn := os.Getenv("PROOFREADER_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set PROOFREADER_TEST_DB_URL to a throwaway DB to run this test")
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("не удалось подключиться к тестовой БД: %v", err)
	}
	defer pool.Close()

	repo := repository.NewCollectionRepository(pool)
	ctx := context.Background()

	// Отметка своя у каждого прогона: строки не убираются за собой (тест не
	// владеет очисткой всей базы, в отличие от internal/repository, который
	// перед каждым корневым тестом усекает все таблицы), поэтому имя обязано
	// не совпасть со следом прошлого запуска.
	nickname := "живая-проверка-409"
	slug := "constraint-check-" + time.Now().Format("20060102-150405.000000000")

	first := &models.Collection{Title: "Первая", Slug: slug, AuthorNickname: nickname}
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("Create(первая): %v", err)
	}

	second := &models.Collection{Title: "Вторая", Slug: slug, AuthorNickname: nickname}
	err = repo.Create(ctx, second)
	if err == nil {
		t.Fatal("повтор пары (ник, слаг) обязан упереться в collections_author_slug_key, а вставка прошла")
	}
	if !isCollectionSlugTakenError(err) {
		t.Fatalf("isCollectionSlugTakenError не распознала настоящее нарушение индекса: %v", err)
	}
}
