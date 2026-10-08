package repository

import (
	"context"
	"errors"
	"testing"

	"proofreader/internal/models"
)

// Удаление разрешено только отклонённому: новое ждёт разбора, принятое —
// след того, откуда на полосе взялась правка.
func TestPageSuggestionDeleteOnlyRejected(t *testing.T) {
	pool := testPool(t) // пропустит тест без PROOFREADER_TEST_DB_URL
	ctx := context.Background()
	repo := NewPageSuggestionRepository(pool)

	_, pageID := seedWorkWithPage(t, pool, "Текст полосы")
	// Автор здесь не важен: тест проверяет удаление, а не авторство. user_id
	// допускает NULL (ON DELETE SET NULL), поэтому его можно не заполнять.
	s := &models.PageSuggestion{
		PageID: pageID, BaseMarkdown: "Текст полосы",
		ProposedMarkdown: "Текст полосы с правкой",
	}
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.Delete(ctx, s.ID); !errors.Is(err, ErrSuggestionNotDeletable) {
		t.Fatalf("новое предложение удалилось (%v), а должно было отбиться", err)
	}
	if _, err := repo.GetDetail(ctx, s.ID); err != nil {
		t.Fatalf("отбитое удаление всё-таки снесло строку: %v", err)
	}

	reason := models.RejectAsInOriginal
	if err := repo.Resolve(ctx, s.ID, models.SuggestionRejected, &reason, 1); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := repo.Delete(ctx, s.ID); err != nil {
		t.Fatalf("отклонённое не удалилось: %v", err)
	}
	if _, err := repo.GetDetail(ctx, s.ID); err == nil {
		t.Fatal("после удаления предложение всё ещё читается")
	}
	// Повторное удаление того же — тот же отказ, что и у неразобранного:
	// удалять нечего.
	if err := repo.Delete(ctx, s.ID); !errors.Is(err, ErrSuggestionNotDeletable) {
		t.Fatalf("повторное удаление дало %v, ожидался ErrSuggestionNotDeletable", err)
	}
}

// Массовая чистка сносит отклонённые и только их.
func TestPageSuggestionDeleteRejectedSparesTheRest(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPageSuggestionRepository(pool)

	_, pageID := seedWorkWithPage(t, pool, "Текст полосы")
	mk := func(proposed string) *models.PageSuggestion {
		s := &models.PageSuggestion{
			PageID: pageID, BaseMarkdown: "Текст полосы",
			ProposedMarkdown: proposed,
		}
		if err := repo.Create(ctx, s); err != nil {
			t.Fatalf("Create: %v", err)
		}
		return s
	}

	fresh := mk("Правка первая")
	accepted := mk("Правка вторая")
	rejected := mk("Правка третья")

	if err := repo.Resolve(ctx, accepted.ID, models.SuggestionAccepted, nil, 1); err != nil {
		t.Fatalf("Resolve accepted: %v", err)
	}
	reason := models.RejectOffTopic
	if err := repo.Resolve(ctx, rejected.ID, models.SuggestionRejected, &reason, 1); err != nil {
		t.Fatalf("Resolve rejected: %v", err)
	}

	// База общая на весь прогон, поэтому счёт проверяется снизу: в ней могли
	// остаться отклонённые от других тестов. Существенно, что наше отклонённое
	// снесено, а новое и принятое целы.
	n, err := repo.DeleteRejected(ctx)
	if err != nil {
		t.Fatalf("DeleteRejected: %v", err)
	}
	if n < 1 {
		t.Fatalf("чистка отчиталась о %d удалённых, а отклонённое было", n)
	}
	if _, err := repo.GetDetail(ctx, rejected.ID); err == nil {
		t.Fatal("отклонённое пережило чистку")
	}
	if _, err := repo.GetDetail(ctx, fresh.ID); err != nil {
		t.Fatalf("чистка снесла неразобранное: %v", err)
	}
	if _, err := repo.GetDetail(ctx, accepted.ID); err != nil {
		t.Fatalf("чистка снесла принятое: %v", err)
	}
}
