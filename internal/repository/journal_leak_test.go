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

// closedFixtureWithText — newClosedFixture, у каждой работы полоса и глава:
// каталог краулера перечисляет главы, а OPDS берёт только тома с полосами.
func closedFixtureWithText(t *testing.T) (*closedFixture, []*models.Work) {
	t.Helper()
	ctx := context.Background()
	f, _ := newClosedFixture(t)
	pool := testPool(t)
	all := []*models.Work{f.open, f.closedSelf, f.childOfClosed, f.inClosedEdition,
		f.childInClosedEdition, f.inClosedJournal, f.closedSelfAndEdition}
	for _, w := range all {
		if _, err := pool.Exec(ctx, `INSERT INTO pages (work_id, page_number, content_markdown, status)
			VALUES ($1, 1, 'текст', 'не_вычитана')`, w.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO chapters (work_id, title, type, order_number, start_page, end_page)
			VALUES ($1, 'Глава', 'chapter', 1, 1, 1)`, w.ID); err != nil {
			t.Fatal(err)
		}
	}
	return f, all
}

// TestSEOCatalogSkipsClosed — карта сайта (и всё, что строится из каталога
// краулера) не называет закрытого: ни работы любым путём закрытости, ни её
// глав, ни закрытого издания. Открытое при этом на месте — иначе пустой
// каталог прошёл бы проверку.
func TestSEOCatalogSkipsClosed(t *testing.T) {
	ctx := context.Background()
	f, all := closedFixtureWithText(t)
	seo := NewSEORepository(testPool(t))

	rows, err := seo.Works(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int64]bool{}
	for _, r := range rows {
		seen[r.ID] = true
	}
	chRows, err := seo.Chapters(ctx)
	if err != nil {
		t.Fatal(err)
	}
	chSeen := map[int64]bool{}
	for _, r := range chRows {
		chSeen[r.WorkID] = true
	}
	if !seen[f.open.ID] || !chSeen[f.open.ID] {
		t.Fatal("открытой работы нет в каталоге — проверка ничего не значит")
	}
	for _, w := range all[1:] {
		if seen[w.ID] {
			t.Errorf("закрытая работа %q в каталоге работ", w.Title)
		}
		if chSeen[w.ID] {
			t.Errorf("глава закрытой работы %q в каталоге глав", w.Title)
		}
	}

	edRows, err := seo.Editions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	edSeen := map[int64]bool{}
	for _, r := range edRows {
		edSeen[r.ID] = true
	}
	if !edSeen[f.openEdit.ID] {
		t.Error("открытого издания нет в каталоге")
	}
	if edSeen[f.closedEdition.ID] {
		t.Error("закрытое издание в каталоге")
	}
}

// TestOPDSVolumesSkipClosed — «Новые поступления» и тома выдачи поиска OPDS
// не берут закрытого тома.
func TestOPDSVolumesSkipClosed(t *testing.T) {
	ctx := context.Background()
	f, all := closedFixtureWithText(t)
	opds := NewOPDSRepository(testPool(t))

	recent, err := opds.RecentVolumes(ctx, 1000, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int64]bool{}
	for _, w := range recent {
		seen[w.ID] = true
	}
	if !seen[f.open.ID] {
		t.Fatal("открытого тома нет в «новых поступлениях» — проверка ничего не значит")
	}
	ids := []int64{}
	for _, w := range all {
		ids = append(ids, w.ID)
	}
	byID, err := opds.VolumesByID(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(byID) != 1 || byID[0].ID != f.open.ID {
		t.Errorf("VolumesByID: %d томов, ждали только открытый", len(byID))
	}
	for _, w := range all[1:] {
		if seen[w.ID] {
			t.Errorf("закрытый том %q в «новых поступлениях»", w.Title)
		}
	}
}
