package repository

import (
	"context"
	"testing"

	"proofreader/internal/models"
)

func TestDocumentCutRoundTripAndPagesOfCut(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	works := NewWorkRepository(pool)
	pages := NewPageRepository(pool)
	docs := NewDocumentRepository(pool)
	repo := NewDocumentCutRepository(pool)

	vol := newVolume(t, works, "проба вклеек — том")
	workID := vol.ID

	texts := []string{"первая полоса", "вторая полоса", "третья полоса"}
	pageIDs := make([]int64, len(texts))
	for i, text := range texts {
		p := &models.Page{
			WorkID: workID, PageNumber: i + 1,
			ContentMarkdown: text,
			Status:          models.PageStatusNotProofread,
		}
		if err := pages.Create(ctx, p); err != nil {
			t.Fatalf("создать страницу %d: %v", i+1, err)
		}
		pageIDs[i] = p.ID
	}

	doc := &models.Document{Title: "Разбор", MarkdownContent: "текст разбора", Slug: "razbor-cut-roundtrip"}
	if err := docs.Create(ctx, doc); err != nil {
		t.Fatalf("создать разбор: %v", err)
	}

	cut := &models.DocumentCut{
		DocumentID: doc.ID, WorkID: &workID,
		Anchor: models.Anchor{
			StartPageID: pageIDs[0], StartOffset: 7,
			EndPageID: pageIDs[2], EndOffset: 6,
			HeadQuote: "полоса", TailQuote: "третья",
			StartHash: hash64A, EndHash: hash64B,
		},
		Status:      models.CutStatusOK,
		SourceTitle: "Автор. Работа // Издание, т. 1, с. 1—3",
	}
	if err := repo.Create(ctx, cut); err != nil {
		t.Fatalf("создание вклейки: %v", err)
	}
	if cut.ID == 0 {
		t.Fatal("id вклейки не вернулся")
	}

	// Полосы вклейки — ВСЕ от начальной до конечной включительно, в порядке
	// чтения: середина диапазона входит в неё целиком.
	pagesOfCut, err := repo.PagesOfCut(ctx, cut)
	if err != nil {
		t.Fatalf("полосы вклейки: %v", err)
	}
	if len(pagesOfCut) != 3 {
		t.Fatalf("полос %d, ожидалось 3", len(pagesOfCut))
	}
	if pagesOfCut[0].ID != pageIDs[0] || pagesOfCut[2].ID != pageIDs[2] {
		t.Fatalf("порядок полос нарушен: %d…%d", pagesOfCut[0].ID, pagesOfCut[2].ID)
	}

	// Переякоривание ходит по обеим сторонам, значит и выборка по полосе.
	byPage, err := repo.ByPage(ctx, pageIDs[2])
	if err != nil || len(byPage) != 1 {
		t.Fatalf("вклейка не найдена по конечной полосе: %v, найдено %d", err, len(byPage))
	}
}

func TestDeleteUnreferencedKeepsNamedCuts(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	works := NewWorkRepository(pool)
	pages := NewPageRepository(pool)
	docs := NewDocumentRepository(pool)
	repo := NewDocumentCutRepository(pool)

	vol := newVolume(t, works, "проба вклеек — сборка мусора")
	workID := vol.ID
	page := &models.Page{
		WorkID: workID, PageNumber: 1, ContentMarkdown: "полоса",
		Status: models.PageStatusNotProofread,
	}
	if err := pages.Create(ctx, page); err != nil {
		t.Fatalf("создать страницу: %v", err)
	}

	doc := &models.Document{Title: "Разбор", Slug: "razbor-named-cuts"}
	if err := docs.Create(ctx, doc); err != nil {
		t.Fatalf("создать разбор: %v", err)
	}

	mk := func() *models.DocumentCut {
		c := &models.DocumentCut{
			DocumentID: doc.ID, WorkID: &workID,
			Anchor: models.Anchor{
				StartPageID: page.ID, StartOffset: 0,
				EndPageID: page.ID, EndOffset: 6,
				HeadQuote: "по", TailQuote: "са", StartHash: hash64A, EndHash: hash64B,
			},
			Status: models.CutStatusOK, SourceTitle: "снимок",
		}
		if err := repo.Create(ctx, c); err != nil {
			t.Fatalf("создание: %v", err)
		}
		return c
	}
	kept, dropped := mk(), mk()

	if err := repo.DeleteUnreferenced(ctx, doc.ID, []int64{kept.ID}); err != nil {
		t.Fatalf("сборка мусора: %v", err)
	}

	left, err := repo.ByDocument(ctx, doc.ID)
	if err != nil {
		t.Fatalf("список: %v", err)
	}
	if len(left) != 1 || left[0].ID != kept.ID {
		t.Fatalf("осталось %d вклеек, ожидалась только %d (ушла %d)", len(left), kept.ID, dropped.ID)
	}
}

// TestDeleteUnreferencedDropsAllWithNilAndEmptyKeep проверяет ровно то место,
// где nil-срез и пустой срез расходятся в pgx: nil кодируется в параметр
// массива как NULL, и NOT (id = ANY(NULL)) — тоже NULL, WHERE не берёт ни
// одной строки. Следующая задача (сборка мусора при сохранении разбора) зовёт
// DeleteUnreferenced именно с nil, когда тело без единого тега <cut> — без
// этого теста «пустой keep сносит все» остаётся утверждением в комментарии, а
// не проверенным поведением.
func TestDeleteUnreferencedDropsAllWithNilAndEmptyKeep(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	works := NewWorkRepository(pool)
	pages := NewPageRepository(pool)
	docs := NewDocumentRepository(pool)
	repo := NewDocumentCutRepository(pool)

	vol := newVolume(t, works, "проба вклеек — снос всех")
	workID := vol.ID
	page := &models.Page{
		WorkID: workID, PageNumber: 1, ContentMarkdown: "полоса",
		Status: models.PageStatusNotProofread,
	}
	if err := pages.Create(ctx, page); err != nil {
		t.Fatalf("создать страницу: %v", err)
	}

	mk := func(docID int64) *models.DocumentCut {
		c := &models.DocumentCut{
			DocumentID: docID, WorkID: &workID,
			Anchor: models.Anchor{
				StartPageID: page.ID, StartOffset: 0,
				EndPageID: page.ID, EndOffset: 6,
				HeadQuote: "по", TailQuote: "са", StartHash: hash64A, EndHash: hash64B,
			},
			Status: models.CutStatusOK, SourceTitle: "снимок",
		}
		if err := repo.Create(ctx, c); err != nil {
			t.Fatalf("создание: %v", err)
		}
		return c
	}

	cases := []struct {
		name string
		slug string
		keep []int64
	}{
		{"nil", "razbor-bez-vkleek-nil", nil},
		{"пустой срез", "razbor-bez-vkleek-empty", []int64{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := &models.Document{Title: "Разбор без вклеек — " + tc.name, Slug: tc.slug}
			if err := docs.Create(ctx, doc); err != nil {
				t.Fatalf("создать разбор: %v", err)
			}
			mk(doc.ID)
			mk(doc.ID)

			if err := repo.DeleteUnreferenced(ctx, doc.ID, tc.keep); err != nil {
				t.Fatalf("сборка мусора: %v", err)
			}

			left, err := repo.ByDocument(ctx, doc.ID)
			if err != nil {
				t.Fatalf("список: %v", err)
			}
			if len(left) != 0 {
				t.Fatalf("осталось %d вклеек, ожидалось 0 (keep=%v)", len(left), tc.keep)
			}
		})
	}
}
