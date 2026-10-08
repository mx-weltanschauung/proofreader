package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"proofreader/internal/models"
)

// I2. Снятие с публикации ОТЗЫВАЕТ и заявку.
//
// До этой правки Unpublish трогал только published_at: состояние оставалось
// «на_рассмотрении», строка оставалась в очереди модератора, и обычное
// «Принять» публиковало разбор обратно. Редактор снял по письму — назавтра
// кто-то разобрал очередь и вернул текст на люди, не спросив автора.
//
// Проверяется на НАСТОЯЩЕМ Postgres, а не на фейке: правка — SQL, и
// семантика SET-выражений, читающих старое значение review_status, ровно та,
// из-за которой соседний тест (Update на «на_рассмотрении») тоже живёт здесь.
func TestDocumentUnpublishWithdrawsPendingSubmission(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewDocumentRepository(pool)

	// Разбор на людях, поверх которого автор подал правку: одобрен и
	// опубликован, затем изменён и отправлен заново. Это и есть тот случай,
	// в котором «снять по жалобе» и «принять из очереди» сталкивались.
	doc := &models.Document{Title: "Разбор", MarkdownContent: "первая редакция", AuthorNickname: "чтец", Slug: "razbor-withdraws"}
	if err := repo.Create(ctx, doc); err != nil {
		t.Fatalf("создать: %v", err)
	}
	if ok, err := repo.SubmitWithinLimit(ctx, doc.ID, "адрес", 3, time.Now().Add(-24*time.Hour)); err != nil || !ok {
		t.Fatalf("первая отправка: ok=%v err=%v", ok, err)
	}
	if err := repo.Approve(ctx, doc.ID, 1); err != nil {
		t.Fatalf("одобрение: %v", err)
	}
	doc.Title = "Разбор"
	doc.MarkdownContent = "вторая редакция, ещё не одобренная"
	if err := repo.Update(ctx, doc); err != nil {
		t.Fatalf("правка поверх публикации: %v", err)
	}
	if ok, err := repo.SubmitWithinLimit(ctx, doc.ID, "адрес", 3, time.Now().Add(-24*time.Hour)); err != nil || !ok {
		t.Fatalf("вторая отправка: ok=%v err=%v", ok, err)
	}

	if err := repo.Unpublish(ctx, doc.ID); err != nil {
		t.Fatalf("снятие с публикации: %v", err)
	}

	after, err := repo.GetByID(ctx, doc.ID)
	if err != nil {
		t.Fatalf("перечитать: %v", err)
	}
	if after.PublishedAt != nil {
		t.Error("разбор остался на публикации")
	}
	if after.ReviewStatus != models.DocumentDraft {
		t.Errorf("заявка не отозвана: состояние %q, ожидалось %q",
			after.ReviewStatus, models.DocumentDraft)
	}
	if after.SubmittedAt != nil {
		t.Error("отметка подачи пережила снятие")
	}
	// Текст черновика снятие не трогает — автору остаётся его правка.
	if after.MarkdownContent != "вторая редакция, ещё не одобренная" {
		t.Errorf("снятие тронуло черновик: %q", after.MarkdownContent)
	}

	// Строки в очереди больше нет.
	pending, err := repo.ListForReview(ctx)
	if err != nil {
		t.Fatalf("очередь: %v", err)
	}
	if containsDocument(pending, doc.ID) {
		t.Error("снятый разбор остался в очереди модератора")
	}

	// Принять снятое нельзя: сторож состояния Approve требует
	// «на_рассмотрении», и после отзыва заявки он срабатывает.
	if err := repo.Approve(ctx, doc.ID, 1); !errors.Is(err, ErrDocumentNotPending) {
		t.Fatalf("приём снятого прошёл: получено %v, ожидался ErrDocumentNotPending", err)
	}
	republished, err := repo.GetByID(ctx, doc.ID)
	if err != nil {
		t.Fatalf("перечитать после отказа в приёме: %v", err)
	}
	if republished.PublishedAt != nil {
		t.Fatal("снятый по жалобе разбор вернулся на люди приёмом из очереди")
	}
}

// Снятие разбора, у которого заявки НЕТ (одобрен и с тех пор не правился),
// состояния не меняет: «одобрено» — правда о черновике, и терять её незачем.
// Отзывается заявка, а не история модерации.
func TestDocumentUnpublishLeavesResolvedStatusAlone(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewDocumentRepository(pool)

	doc := &models.Document{Title: "Разбор", MarkdownContent: "тело", AuthorNickname: "чтец", Slug: "razbor-resolved-alone"}
	if err := repo.Create(ctx, doc); err != nil {
		t.Fatalf("создать: %v", err)
	}
	if ok, err := repo.SubmitWithinLimit(ctx, doc.ID, "адрес", 3, time.Now().Add(-24*time.Hour)); err != nil || !ok {
		t.Fatalf("отправка: ok=%v err=%v", ok, err)
	}
	if err := repo.Approve(ctx, doc.ID, 1); err != nil {
		t.Fatalf("одобрение: %v", err)
	}
	if err := repo.Unpublish(ctx, doc.ID); err != nil {
		t.Fatalf("снятие: %v", err)
	}
	after, err := repo.GetByID(ctx, doc.ID)
	if err != nil {
		t.Fatalf("перечитать: %v", err)
	}
	if after.ReviewStatus != models.DocumentApproved {
		t.Errorf("снятие переписало решение модератора: %q", after.ReviewStatus)
	}
}
