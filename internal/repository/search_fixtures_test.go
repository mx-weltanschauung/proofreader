package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

// seedWorkWithPages заводит том с заданным заголовком и по одной полосе на
// каждый переданный текст (нумерация с 1). Общая фикстура тестов области
// поиска — задачи 2, 4 и 5 репозитория используют её, чтобы не заводить
// каждая свою копию (см. образец seedWorkWithPage в
// page_suggestion_repository_test.go, откуда взят приём).
func seedWorkWithPages(t *testing.T, pool *pgxpool.Pool, title string, markdowns ...string) int64 {
	t.Helper()
	ctx := context.Background()

	workRepo := NewWorkRepository(pool)
	w := &models.Work{
		Title: title, Author: "проба", Language: "ru", Country: "ru",
		Status: models.WorkStatusDraft, Role: models.WorkRoleVolume,
		NumberingStyle: models.NumberingArabic, OwnerID: 1,
	}
	if err := workRepo.Create(ctx, w); err != nil {
		t.Fatalf("seedWorkWithPages: create work: %v", err)
	}
	t.Cleanup(func() { _ = workRepo.Delete(context.Background(), w.ID) })

	seedPages(t, pool, w.ID, markdowns...)
	return w.ID
}

// seedChildWorkWithPages заводит служебную работу (передние листы) с
// parent_work_id = parent. Отдельного Cleanup не заводит: parent_work_id
// стоит с ON DELETE CASCADE (миграция 000007), и родитель уносит её сам, как
// только его собственный Cleanup (из seedWorkWithPages) удалит родителя.
func seedChildWorkWithPages(t *testing.T, pool *pgxpool.Pool, parent int64, title string, markdowns ...string) int64 {
	t.Helper()
	ctx := context.Background()

	workRepo := NewWorkRepository(pool)
	w := &models.Work{
		Title: title, Author: "проба", Language: "ru", Country: "ru",
		Status: models.WorkStatusDraft, Role: models.WorkRoleFrontMatter,
		ParentWorkID: &parent, NumberingStyle: models.NumberingArabic, OwnerID: 1,
	}
	if err := workRepo.Create(ctx, w); err != nil {
		t.Fatalf("seedChildWorkWithPages: create work: %v", err)
	}

	seedPages(t, pool, w.ID, markdowns...)
	return w.ID
}

// seedPages вставляет полосы 1..N с переданными текстами.
func seedPages(t *testing.T, pool *pgxpool.Pool, workID int64, markdowns ...string) {
	t.Helper()
	ctx := context.Background()
	pageRepo := NewPageRepository(pool)
	for i, md := range markdowns {
		p := &models.Page{
			WorkID: workID, PageNumber: i + 1, ContentMarkdown: md,
			Status: models.PageStatusNotProofread,
		}
		if err := pageRepo.Create(ctx, p); err != nil {
			t.Fatalf("seedPages: create page %d for work %d: %v", i+1, workID, err)
		}
	}
}

// seedChapter заводит главу тома над диапазоном [startPage, endPage].
// order_number считается числом уже заведённых глав тома плюс один — этого
// достаточно для фикстур: тесты нумерацию глав не проверяют, а
// UNIQUE(work_id, order_number) требует лишь несовпадения внутри тома.
// Используется задачами 4 (сужение выдачи главами) и 5 (фасет глав).
func seedChapter(t *testing.T, pool *pgxpool.Pool, workID int64, title string, startPage, endPage int) int64 {
	t.Helper()
	ctx := context.Background()

	var order int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM chapters WHERE work_id = $1`, workID).Scan(&order); err != nil {
		t.Fatalf("seedChapter: count existing chapters: %v", err)
	}

	chapterRepo := NewChapterRepository(pool)
	c := &models.Chapter{
		WorkID: workID, Title: title, Type: "chapter",
		OrderNumber: order + 1, StartPage: startPage, EndPage: endPage,
	}
	if err := chapterRepo.Create(ctx, c); err != nil {
		t.Fatalf("seedChapter: create chapter %q for work %d: %v", title, workID, err)
	}
	return c.ID
}
