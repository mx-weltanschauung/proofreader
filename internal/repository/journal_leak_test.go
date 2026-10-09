package repository

import (
	"context"
	"testing"

	"proofreader/internal/models"
)

// TestJournalIssueDoesNotLeakIntoChannels — номер журнала не попадает ни в
// один перечень, из которого каналы выдачи собирают свои списки. Открывать
// каналы — спека Б, вместе с рычагом «снять статью».
func TestJournalIssueDoesNotLeakIntoChannels(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	journals := NewJournalRepository(pool)
	j := newJournal(t, journals, "leak")
	_, work := newIssue(t, journals, j, 1928, 12, 12)
	if _, err := pool.Exec(ctx, `INSERT INTO pages (work_id, page_number, content_markdown, status)
		VALUES ($1, 1, 'текст', 'не_вычитана')`, work.ID); err != nil {
		t.Fatal(err)
	}
	kind := "статья"
	if err := NewChapterRepository(pool).Create(ctx, &models.Chapter{WorkID: work.ID, Title: "Статья",
		Type: "chapter", OrderNumber: 1, StartPage: 1, EndPage: 1, ArticleKind: &kind}); err != nil {
		t.Fatal(err)
	}

	works := NewWorkRepository(pool)
	list, err := works.List(ctx, 1000, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range list {
		if w.ID == work.ID {
			t.Error("номер в /api/works")
		}
	}
	loose, err := works.ListWithoutEdition(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range loose {
		if w.ID == work.ID {
			t.Error("номер в «томах вне собраний» (полка, llms.txt, статическая читальня)")
		}
	}
	sums, err := NewEditionRepository(pool).ListAllWorkSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sums {
		if s.ID == work.ID {
			t.Error("номер в полках собраний")
		}
	}
	seo := NewSEORepository(pool)
	rows, err := seo.Works(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == work.ID {
			t.Error("номер в карте сайта")
		}
	}
	chRows, err := seo.Chapters(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range chRows {
		if r.WorkID == work.ID {
			t.Error("статья номера в карте сайта")
		}
	}
	opds := NewOPDSRepository(pool)
	recent, err := opds.RecentVolumes(ctx, 1000, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range recent {
		if w.ID == work.ID {
			t.Error("номер в «новых поступлениях» OPDS")
		}
	}
	byID, err := opds.VolumesByID(ctx, []int64{work.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(byID) != 0 {
		t.Error("номер в выдаче поиска OPDS (VolumesByID)")
	}
}
