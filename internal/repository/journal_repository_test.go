package repository

import (
	"context"
	"errors"
	"testing"

	"proofreader/internal/models"
)

func newJournal(t *testing.T, repo *JournalRepository, slug string) *models.Journal {
	t.Helper()
	j := &models.Journal{Slug: slug, Title: "Проба " + slug}
	if err := repo.Create(context.Background(), j); err != nil {
		t.Fatalf("create journal: %v", err)
	}
	return j
}

func newIssue(t *testing.T, repo *JournalRepository, j *models.Journal, year, from, to int) (*models.JournalIssue, *models.Work) {
	t.Helper()
	label := models.IssueLabel(from, to)
	issue := &models.JournalIssue{JournalID: j.ID, Year: year, NumberFrom: from, NumberTo: to, Label: label}
	work := &models.Work{Title: models.IssueTitle(j.Title, year, label), Language: "ru",
		Status: models.WorkStatusDraft, OwnerID: 1}
	if err := repo.CreateIssue(context.Background(), issue, work); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	return issue, work
}

func TestJournalCreateIssueMakesWorkAndRow(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := NewJournalRepository(pool)
	works := NewWorkRepository(pool)
	j := newJournal(t, repo, "jr-make")
	issue, work := newIssue(t, repo, j, 1925, 5, 6)

	got, err := works.GetByID(ctx, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Role != models.WorkRoleJournalIssue || got.EditionID != nil || got.ParentWorkID != nil {
		t.Fatalf("работа номера: роль %q, издание %v, родитель %v", got.Role, got.EditionID, got.ParentWorkID)
	}
	if issue.WorkID != work.ID || issue.ID == 0 {
		t.Fatalf("строка номера: %+v", issue)
	}
	ref, err := repo.IssueForWork(ctx, work.ID)
	if err != nil || ref == nil || ref.Label != "5—6" || ref.JournalSlug != "jr-make" || ref.Year != 1925 {
		t.Fatalf("IssueForWork = %+v, %v", ref, err)
	}
}

func TestJournalIssueTakenIsRejectedAndRolledBack(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := NewJournalRepository(pool)
	j := newJournal(t, repo, "jr-taken")
	newIssue(t, repo, j, 1925, 5, 6)
	var before int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM works`).Scan(&before)

	dup := &models.JournalIssue{JournalID: j.ID, Year: 1925, NumberFrom: 5, NumberTo: 5, Label: "5"}
	w := &models.Work{Title: "дубль", Status: models.WorkStatusDraft, OwnerID: 1}
	err := repo.CreateIssue(ctx, dup, w)
	if !errors.Is(err, ErrIssueTaken) {
		t.Fatalf("ждали ErrIssueTaken, получили %v", err)
	}
	var after int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM works`).Scan(&after)
	if after != before {
		t.Fatalf("работа дубля осталась: было %d, стало %d", before, after)
	}
}

func TestJournalIssueExplicitIDs(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := NewJournalRepository(pool)
	j := &models.Journal{ID: 4242, Slug: "jr-explicit", Title: "Явный"}
	if err := repo.Create(ctx, j); err != nil || j.ID != 4242 {
		t.Fatalf("журнал с явным id: %d, %v", j.ID, err)
	}
	issue := &models.JournalIssue{ID: 5151, JournalID: j.ID, Year: 1930, NumberFrom: 1, NumberTo: 1, Label: "1"}
	work := &models.Work{ID: 6161, Title: "Явный, 1930, № 1", Status: models.WorkStatusDraft, OwnerID: 1}
	if err := repo.CreateIssue(ctx, issue, work); err != nil {
		t.Fatal(err)
	}
	if issue.ID != 5151 || work.ID != 6161 || issue.WorkID != 6161 {
		t.Fatalf("id: номер %d, работа %d", issue.ID, work.ID)
	}
	again := &models.Journal{ID: 4242, Slug: "jr-explicit-2", Title: "Повтор"}
	if err := repo.Create(ctx, again); !errors.Is(err, ErrIDTaken) {
		t.Fatalf("занятый id журнала: %v", err)
	}
	if err := repo.Create(ctx, &models.Journal{Slug: "jr-explicit", Title: "Тот же слаг"}); !errors.Is(err, ErrJournalSlugTaken) {
		t.Fatalf("занятый слаг: %v", err)
	}
}

func TestJournalDetailGroupsByYearAndDeleteCascades(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := NewJournalRepository(pool)
	works := NewWorkRepository(pool)
	j := newJournal(t, repo, "jr-detail")
	_, w1 := newIssue(t, repo, j, 1926, 3, 3)
	newIssue(t, repo, j, 1925, 7, 8)
	newIssue(t, repo, j, 1925, 1, 2)

	d, err := repo.Detail(ctx, "jr-detail")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Years) != 2 || d.Years[0].Year != 1925 || len(d.Years[0].Issues) != 2 ||
		d.Years[0].Issues[0].Label != "1—2" || d.Years[1].Issues[0].Label != "3" {
		t.Fatalf("годы: %+v", d.Years)
	}
	if d.Years[1].Issues[0].WorkSlug == "" {
		t.Fatal("у номера пустой слаг работы")
	}
	list, err := repo.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, s := range list {
		if s.Slug == "jr-detail" {
			found = true
			if s.IssuesTotal != 3 || *s.YearFrom != 1925 || *s.YearTo != 1926 {
				t.Fatalf("сводка: %+v", s)
			}
		}
	}
	if !found {
		t.Fatal("журнала нет в списке")
	}
	if err := works.Delete(ctx, w1.ID); err != nil {
		t.Fatal(err)
	}
	d, _ = repo.Detail(ctx, "jr-detail")
	if len(d.Years) != 1 {
		t.Fatalf("после удаления работы номера годы: %+v", d.Years)
	}
	if _, err := repo.Detail(ctx, "net-takogo"); !errors.Is(err, ErrJournalNotFound) {
		t.Fatalf("неизвестный журнал: %v", err)
	}
}

func TestJournalUpdateIssueRenamesWork(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := NewJournalRepository(pool)
	works := NewWorkRepository(pool)
	j := newJournal(t, repo, "jr-upd")
	issue, work := newIssue(t, repo, j, 1925, 5, 5)
	issue.NumberTo = 6
	issue.Label = "5—6"
	if err := repo.UpdateIssue(ctx, issue, models.IssueTitle(j.Title, 1925, "5—6")); err != nil {
		t.Fatal(err)
	}
	got, _ := works.GetByID(ctx, work.ID)
	if got.Title != "Проба jr-upd, 1925, № 5—6" {
		t.Fatalf("заглавие работы: %q", got.Title)
	}
}
