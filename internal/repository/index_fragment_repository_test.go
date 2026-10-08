package repository

import (
	"context"
	"strings"
	"testing"

	"proofreader/internal/models"
)

// start_hash/end_hash — CHAR(64): под настоящий sha256-хэш ровно 64 символа,
// иначе Postgres дополняет значение пробелами до ширины столбца. Короткие
// заглушки вроде "a" тут не годятся — берём хэшеподобные строки нужной длины.
var (
	hash64A = strings.Repeat("a", 64)
	hash64B = strings.Repeat("b", 64)
)

func TestIndexFragmentReplaceAndRead(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	works := NewWorkRepository(pool)
	pages := NewPageRepository(pool)
	frags := NewIndexFragmentRepository(pool)

	vol := newVolume(t, works, "проба вырезок — том")
	page := &models.Page{
		WorkID: vol.ID, PageNumber: 1,
		ContentMarkdown: "первый абзац\n\nвторой абзац",
		Status:          models.PageStatusNotProofread,
	}
	if err := pages.Create(ctx, page); err != nil {
		t.Fatalf("создать страницу: %v", err)
	}

	// Понятие со статьёй заводится напрямую seed-помощниками пакета (см.
	// index_repository_test.go): ReplaceForWork ушёл вместе со старой
	// однотабличной формой понятия, и вырезки этого файла проверяют
	// IndexFragmentRepository, а не ввоз указателя.
	editionID, idxWorkID := seedEditionAndWork(t, pool)
	conceptID := seedConcept(t, pool, "Проба", "proba-vyrezok")
	articleID := seedArticle(t, pool, conceptID, editionID, &idxWorkID, "Проба")
	rubricID := seedRubric(t, pool, articleID, "определение", 1)
	refID := seedReference(t, pool, articleID, &rubricID, 1, 1, 1, 1)

	err := frags.ReplaceForReference(ctx, refID, []*models.IndexFragment{
		{
			ReferenceID: refID, OrderNumber: 1,
			StartPageID: page.ID, StartOffset: 0,
			EndPageID: page.ID, EndOffset: 12,
			HeadQuote: "первый абзац", TailQuote: "первый абзац",
			StartHash: hash64A, EndHash: hash64A, Status: models.FragmentStatusMachine,
		},
	})
	if err != nil {
		t.Fatalf("записать вырезку: %v", err)
	}

	byRef, err := frags.ByReferences(ctx, []int64{refID})
	if err != nil {
		t.Fatalf("ByReferences: %v", err)
	}
	if len(byRef[refID]) != 1 || byRef[refID][0].EndOffset != 12 {
		t.Fatalf("вырезки адреса: %+v", byRef[refID])
	}

	byPage, err := frags.ByPage(ctx, page.ID)
	if err != nil {
		t.Fatalf("ByPage: %v", err)
	}
	if len(byPage) != 1 {
		t.Fatalf("вырезок на странице %d, ожидалась 1", len(byPage))
	}

	// Замена — именно замена, а не добавление: скилл гоняется повторно.
	if err := frags.ReplaceForReference(ctx, refID, nil); err != nil {
		t.Fatalf("стереть вырезки: %v", err)
	}
	byRef, err = frags.ByReferences(ctx, []int64{refID})
	if err != nil {
		t.Fatalf("ByReferences после стирания: %v", err)
	}
	if len(byRef[refID]) != 0 {
		t.Fatalf("после стирания осталось %d вырезок", len(byRef[refID]))
	}
}

func TestIndexFragmentUpdateAnchorAndStatus(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	works := NewWorkRepository(pool)
	pages := NewPageRepository(pool)
	frags := NewIndexFragmentRepository(pool)

	vol := newVolume(t, works, "проба якоря — том")
	page := &models.Page{
		WorkID: vol.ID, PageNumber: 1, ContentMarkdown: "текст",
		Status: models.PageStatusNotProofread,
	}
	if err := pages.Create(ctx, page); err != nil {
		t.Fatalf("создать страницу: %v", err)
	}

	// См. комментарий в TestIndexFragmentReplaceAndRead: понятие заводится
	// seed-помощниками напрямую, без ушедшего ReplaceForWork.
	editionID, idxWorkID := seedEditionAndWork(t, pool)
	conceptID := seedConcept(t, pool, "Якорь", "proba-yakorya")
	articleID := seedArticle(t, pool, conceptID, editionID, &idxWorkID, "Якорь")
	refID := seedReference(t, pool, articleID, nil, 1, 1, 1, 1)

	if err := frags.ReplaceForReference(ctx, refID, []*models.IndexFragment{{
		ReferenceID: refID, OrderNumber: 1,
		StartPageID: page.ID, StartOffset: 0, EndPageID: page.ID, EndOffset: 5,
		HeadQuote: "текст", TailQuote: "текст", StartHash: hash64A, EndHash: hash64A,
		Status: models.FragmentStatusConfirmed,
	}}); err != nil {
		t.Fatalf("записать вырезку: %v", err)
	}
	byRef, _ := frags.ByReferences(ctx, []int64{refID})
	id := byRef[refID][0].ID

	if err := frags.UpdateAnchor(ctx, id, 7, 12, hash64B, hash64B); err != nil {
		t.Fatalf("UpdateAnchor: %v", err)
	}
	if err := frags.SetStatus(ctx, id, models.FragmentStatusStale); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	byRef, _ = frags.ByReferences(ctx, []int64{refID})
	got := byRef[refID][0]
	if got.StartOffset != 7 || got.EndOffset != 12 || got.StartHash != hash64B {
		t.Fatalf("после UpdateAnchor: %+v", got)
	}
	if got.Status != models.FragmentStatusStale {
		t.Fatalf("статус %q", got.Status)
	}
}

func TestIndexFragmentByReferencesEmptyInput(t *testing.T) {
	pool := testPool(t)
	frags := NewIndexFragmentRepository(pool)

	// Пустой срез адресов — обычное дело: порция потока может целиком
	// состоять из ненарезанных адресов. Запрос при этом делать незачем.
	got, err := frags.ByReferences(context.Background(), nil)
	if err != nil {
		t.Fatalf("ByReferences(nil): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("вернулось %d групп", len(got))
	}
}
