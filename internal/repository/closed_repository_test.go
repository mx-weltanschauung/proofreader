package repository

import (
	"context"
	"errors"
	"sort"
	"testing"

	"proofreader/internal/models"
)

// closedFixture — работы всех путей закрытости разом: имена говорят, что
// ждёт от них work_closed_by.
type closedFixture struct {
	open, closedSelf        *models.Work
	childOfClosed           *models.Work // служебная закрытого тома
	inClosedEdition         *models.Work
	childInClosedEdition    *models.Work // служебная тома из закрытого издания
	inClosedJournal         *models.Work
	closedSelfAndEdition    *models.Work
	closedEdition, openEdit *models.Edition
	closedJournal           *models.Journal
}

func newClosedFixture(t *testing.T) (*closedFixture, *ClosedRepository) {
	t.Helper()
	ctx := context.Background()
	pool := testPool(t)
	works := NewWorkRepository(pool)
	editions := NewEditionRepository(pool)
	journals := NewJournalRepository(pool)
	closed := NewClosedRepository(pool)
	f := &closedFixture{}

	f.closedEdition = &models.Edition{Title: "закрытое", Slug: "closed-ed"}
	f.openEdit = &models.Edition{Title: "открытое", Slug: "open-ed"}
	for _, e := range []*models.Edition{f.closedEdition, f.openEdit} {
		if err := editions.Create(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := closed.SetEditionClosed(ctx, f.closedEdition.ID, true); err != nil {
		t.Fatal(err)
	}

	f.open = newVolume(t, works, "открытая")
	f.closedSelf = newVolume(t, works, "закрытая сама")
	f.inClosedEdition = newVolume(t, works, "в закрытом издании")
	f.closedSelfAndEdition = newVolume(t, works, "сама и издание")
	f.childOfClosed = newChild(t, works, f.closedSelf, "служебная закрытого")
	f.childInClosedEdition = newChild(t, works, f.inClosedEdition, "служебная издания")
	for _, w := range []*models.Work{f.inClosedEdition, f.closedSelfAndEdition} {
		w.EditionID = &f.closedEdition.ID
		if _, err := pool.Exec(ctx, `UPDATE works SET edition_id = $2 WHERE id = $1`, w.ID, f.closedEdition.ID); err != nil {
			t.Fatal(err)
		}
	}
	for _, w := range []*models.Work{f.closedSelf, f.closedSelfAndEdition} {
		if _, err := closed.SetWorkClosed(ctx, w.ID, true); err != nil {
			t.Fatal(err)
		}
	}

	f.closedJournal = newJournal(t, journals, "closed-jr")
	_, f.inClosedJournal = newIssue(t, journals, f.closedJournal, 1925, 1, 1)
	if _, err := closed.SetJournalClosed(ctx, f.closedJournal.ID, true); err != nil {
		t.Fatal(err)
	}
	return f, closed
}

func newChild(t *testing.T, repo *WorkRepository, parent *models.Work, title string) *models.Work {
	t.Helper()
	w := &models.Work{
		Title: title, Language: "ru", Status: models.WorkStatusDraft,
		Role: models.WorkRoleFrontMatter, ParentWorkID: &parent.ID, OwnerID: 1,
	}
	if err := repo.Create(context.Background(), w); err != nil {
		t.Fatalf("create child: %v", err)
	}
	return w
}

func TestClosedByEachPath(t *testing.T) {
	ctx := context.Background()
	f, closed := newClosedFixture(t)
	for _, c := range []struct {
		name string
		w    *models.Work
		want string
	}{
		{"открытая", f.open, ""},
		{"закрыта сама", f.closedSelf, models.ClosedByWork},
		{"служебная закрытого тома", f.childOfClosed, models.ClosedByParent},
		{"том закрытого издания", f.inClosedEdition, models.ClosedByEdition},
		{"служебная тома закрытого издания", f.childInClosedEdition, models.ClosedByParent},
		{"номер закрытого журнала", f.inClosedJournal, models.ClosedByJournal},
		{"сама и в закрытом издании", f.closedSelfAndEdition, models.ClosedByWork},
	} {
		got, err := closed.ClosedBy(ctx, c.w.ID)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s: ClosedBy = %q, ждали %q", c.name, got, c.want)
		}
	}
	if got, err := closed.ClosedBy(ctx, 999999); err != nil || got != "" {
		t.Errorf("неизвестная работа: %q, %v; ждали пусто без ошибки", got, err)
	}
}

func TestClosedIDsMatchesClosedBy(t *testing.T) {
	ctx := context.Background()
	f, closed := newClosedFixture(t)
	all := []*models.Work{f.open, f.closedSelf, f.childOfClosed, f.inClosedEdition,
		f.childInClosedEdition, f.inClosedJournal, f.closedSelfAndEdition}
	want := map[int64]bool{}
	for _, w := range all {
		by, err := closed.ClosedBy(ctx, w.ID)
		if err != nil {
			t.Fatal(err)
		}
		if by != "" {
			want[w.ID] = true
		}
	}
	got, err := closed.ClosedIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) || len(want) != 6 {
		t.Fatalf("ClosedIDs = %v, ждали ровно %v (6 штук)", got, want)
	}
	for id := range want {
		if !got[id] {
			t.Errorf("работа %d закрыта, а в ClosedIDs её нет", id)
		}
	}
	eds, err := closed.ClosedEditionIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(eds) != 1 || !eds[f.closedEdition.ID] {
		t.Errorf("ClosedEditionIDs = %v, ждали только %d", eds, f.closedEdition.ID)
	}
}

func sortedIDs(ids []int64) []int64 {
	out := append([]int64{}, ids...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func TestSetEditionClosedReturnsAllWorks(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	works := NewWorkRepository(pool)
	editions := NewEditionRepository(pool)
	closed := NewClosedRepository(pool)

	e := &models.Edition{Title: "проба-закрытие", Slug: "proba-closing"}
	if err := editions.Create(ctx, e); err != nil {
		t.Fatal(err)
	}
	a := newVolume(t, works, "том 1")
	b := newVolume(t, works, "том 2")
	for _, w := range []*models.Work{a, b} {
		if _, err := pool.Exec(ctx, `UPDATE works SET edition_id = $2 WHERE id = $1`, w.ID, e.ID); err != nil {
			t.Fatal(err)
		}
	}
	child := newChild(t, works, a, "служебная")
	// посторонняя работа не должна попасть в ответ
	newVolume(t, works, "чужая")

	want := sortedIDs([]int64{a.ID, b.ID, child.ID})
	for i := 0; i < 2; i++ {
		got, err := closed.SetEditionClosed(ctx, e.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		got = sortedIDs(got)
		if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
			t.Fatalf("вызов %d: вернулись %v, ждали %v", i+1, got, want)
		}
	}
	if _, err := closed.SetEditionClosed(ctx, 999999, true); !errors.Is(err, ErrClosedTargetNotFound) {
		t.Errorf("нет издания: %v, ждали ErrClosedTargetNotFound", err)
	}
	if _, err := closed.SetWorkClosed(ctx, 999999, true); !errors.Is(err, ErrClosedTargetNotFound) {
		t.Errorf("нет работы: %v", err)
	}
	if _, err := closed.SetJournalClosed(ctx, 999999, true); !errors.Is(err, ErrClosedTargetNotFound) {
		t.Errorf("нет журнала: %v", err)
	}
}

func TestSetWorkAndJournalClosedReturnAffected(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	works := NewWorkRepository(pool)
	journals := NewJournalRepository(pool)
	closed := NewClosedRepository(pool)

	vol := newVolume(t, works, "том")
	child := newChild(t, works, vol, "служебная")
	got, err := closed.SetWorkClosed(ctx, vol.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	got = sortedIDs(got)
	if len(got) != 2 || got[0] != vol.ID || got[1] != child.ID {
		t.Fatalf("SetWorkClosed вернул %v, ждали том и служебную", got)
	}

	j := newJournal(t, journals, "aff-jr")
	_, w1 := newIssue(t, journals, j, 1925, 1, 1)
	_, w2 := newIssue(t, journals, j, 1925, 2, 2)
	got, err = closed.SetJournalClosed(ctx, j.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	got = sortedIDs(got)
	want := sortedIDs([]int64{w1.ID, w2.ID})
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("SetJournalClosed вернул %v, ждали %v", got, want)
	}
}

func TestCreateCarriesClosed(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	works := NewWorkRepository(pool)
	editions := NewEditionRepository(pool)
	journals := NewJournalRepository(pool)

	w := &models.Work{Title: "закрытая с рождения", Language: "ru", Status: models.WorkStatusDraft,
		Role: models.WorkRoleVolume, NumberingStyle: models.NumberingArabic, OwnerID: 1, Closed: true}
	if err := works.Create(ctx, w); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = works.Delete(ctx, w.ID) })
	got, err := works.GetByID(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Closed || got.ClosedBy != models.ClosedByWork {
		t.Errorf("работа: Closed=%v ClosedBy=%q", got.Closed, got.ClosedBy)
	}

	e := &models.Edition{Title: "закрытое с рождения", Slug: "closed-born", Closed: true}
	if err := editions.Create(ctx, e); err != nil {
		t.Fatal(err)
	}
	ge, err := editions.GetByID(ctx, e.ID)
	if err != nil || !ge.Closed {
		t.Errorf("издание: %+v, %v", ge, err)
	}

	j := &models.Journal{Slug: "closed-born-jr", Title: "Проба", Closed: true}
	if err := journals.Create(ctx, j); err != nil {
		t.Fatal(err)
	}
	gj, err := journals.GetByID(ctx, j.ID)
	if err != nil || !gj.Closed {
		t.Errorf("журнал: %+v, %v", gj, err)
	}

	open := newJournal(t, journals, "open-born-jr")
	issue := &models.JournalIssue{JournalID: open.ID, Year: 1926, NumberFrom: 1, NumberTo: 1, Label: "1"}
	iw := &models.Work{Title: "номер", Language: "ru", Status: models.WorkStatusDraft, OwnerID: 1, Closed: true}
	if err := journals.CreateIssue(ctx, issue, iw); err != nil {
		t.Fatal(err)
	}
	gi, err := works.GetByID(ctx, iw.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !gi.Closed || gi.ClosedBy != models.ClosedByWork {
		t.Errorf("работа номера: Closed=%v ClosedBy=%q", gi.Closed, gi.ClosedBy)
	}
}

func TestSummariesCarryClosedBy(t *testing.T) {
	ctx := context.Background()
	f, _ := newClosedFixture(t)
	editions := NewEditionRepository(testPool(t))

	all, err := editions.ListAllWorkSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	by := map[int64]*models.VolumeSummary{}
	for _, s := range all {
		by[s.ID] = s
	}
	s := by[f.inClosedEdition.ID]
	if s == nil {
		t.Fatal("том закрытого издания не попал в сводки")
	}
	if s.ClosedBy != models.ClosedByEdition || s.Closed {
		t.Errorf("сводка: Closed=%v ClosedBy=%q, ждали false и %q", s.Closed, s.ClosedBy, models.ClosedByEdition)
	}
	if c := by[f.closedSelfAndEdition.ID]; c == nil || !c.Closed || c.ClosedBy != models.ClosedByWork {
		t.Errorf("сводка закрытой самой: %+v", c)
	}
	one, err := editions.ListWorkSummaries(ctx, f.closedEdition.ID)
	if err != nil || len(one) == 0 || one[0].ClosedBy == "" {
		t.Errorf("ListWorkSummaries по одному изданию: %v, %v", one, err)
	}
}

func TestReadersCarryClosedBy(t *testing.T) {
	ctx := context.Background()
	f, _ := newClosedFixture(t)
	works := NewWorkRepository(testPool(t))
	editions := NewEditionRepository(testPool(t))

	kids, err := works.ListChildren(ctx, f.closedSelf.ID)
	if err != nil || len(kids) != 1 || kids[0].ClosedBy != models.ClosedByParent {
		t.Errorf("ListChildren: %v, %v", kids, err)
	}
	ew, err := editions.ListWorks(ctx, f.closedEdition.ID)
	if err != nil || len(ew) == 0 || ew[0].ClosedBy == "" {
		t.Errorf("ListWorks издания: %v, %v", ew, err)
	}
}

// Общий Update признак не пишет: правка заглавия не должна ни открыть
// закрытое, ни закрыть открытое.
func TestUpdateLeavesClosedAlone(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	editions := NewEditionRepository(pool)
	journals := NewJournalRepository(pool)
	closed := NewClosedRepository(pool)

	e := &models.Edition{Title: "правка", Slug: "upd-closed"}
	if err := editions.Create(ctx, e); err != nil {
		t.Fatal(err)
	}
	if _, err := closed.SetEditionClosed(ctx, e.ID, true); err != nil {
		t.Fatal(err)
	}
	e.Title = "правка 2" // модель из тела запроса: Closed в ней false
	if err := editions.Update(ctx, e); err != nil {
		t.Fatal(err)
	}
	if got, _ := editions.GetByID(ctx, e.ID); got == nil || !got.Closed {
		t.Errorf("Update издания открыл его: %+v", got)
	}

	j := newJournal(t, journals, "upd-closed-jr")
	if _, err := closed.SetJournalClosed(ctx, j.ID, true); err != nil {
		t.Fatal(err)
	}
	j.Title = "правка 2"
	j.Closed = false
	if err := journals.Update(ctx, j); err != nil {
		t.Fatal(err)
	}
	if got, _ := journals.GetByID(ctx, j.ID); got == nil || !got.Closed {
		t.Errorf("Update журнала открыл его: %+v", got)
	}
}

func issuesOf(d *models.JournalDetail) int {
	n := 0
	for _, y := range d.Years {
		n += len(y.Issues)
	}
	return n
}

func TestJournalClosedHiddenUnlessIncluded(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	journals := NewJournalRepository(pool)
	closed := NewClosedRepository(pool)
	j := newJournal(t, journals, "cl-list")
	newIssue(t, journals, j, 1925, 1, 1)
	if _, err := closed.SetJournalClosed(ctx, j.ID, true); err != nil {
		t.Fatal(err)
	}

	if _, err := journals.Detail(ctx, j.Slug, false); !errors.Is(err, ErrJournalNotFound) {
		t.Fatalf("Detail(false) закрытого журнала: %v, ждали ErrJournalNotFound", err)
	}
	d, err := journals.Detail(ctx, j.Slug, true)
	if err != nil || issuesOf(d) != 1 || !d.Journal.Closed {
		t.Fatalf("Detail(true): %+v, %v", d, err)
	}
	has := func(include bool) bool {
		list, err := journals.List(ctx, include)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range list {
			if s.ID == j.ID {
				return true
			}
		}
		return false
	}
	if has(false) {
		t.Error("List(false) отдал закрытый журнал")
	}
	if !has(true) {
		t.Error("List(true) потерял закрытый журнал")
	}
}

func TestJournalClosedIssueLeavesCountsAndYears(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	journals := NewJournalRepository(pool)
	closed := NewClosedRepository(pool)
	j := newJournal(t, journals, "cl-issue")
	newIssue(t, journals, j, 1925, 1, 1)
	_, w2 := newIssue(t, journals, j, 1926, 1, 1)
	if _, err := closed.SetWorkClosed(ctx, w2.ID, true); err != nil {
		t.Fatal(err)
	}

	d, err := journals.Detail(ctx, j.Slug, false)
	if err != nil || issuesOf(d) != 1 || len(d.Years) != 1 || d.Years[0].Year != 1925 {
		t.Fatalf("Detail(false): %+v, %v", d, err)
	}
	if d, _ = journals.Detail(ctx, j.Slug, true); issuesOf(d) != 2 {
		t.Fatalf("Detail(true): номеров %d, ждали 2", issuesOf(d))
	}
	summary := func(include bool) models.JournalSummary {
		list, err := journals.List(ctx, include)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range list {
			if s.ID == j.ID {
				return s
			}
		}
		t.Fatalf("журнал %d не в списке (include=%v)", j.ID, include)
		return models.JournalSummary{}
	}
	if s := summary(false); s.IssuesTotal != 1 || *s.YearFrom != 1925 || *s.YearTo != 1925 {
		t.Errorf("List(false): %+v", s)
	}
	if s := summary(true); s.IssuesTotal != 2 || *s.YearTo != 1926 {
		t.Errorf("List(true): %+v", s)
	}
}

func TestPersonDetailDropsClosedArticles(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	journals := NewJournalRepository(pool)
	persons := NewPersonRepository(pool)
	chapters := NewChapterRepository(pool)
	closed := NewClosedRepository(pool)

	j := newJournal(t, journals, "cl-person")
	_, w1 := newIssue(t, journals, j, 1925, 1, 1)
	_, w2 := newIssue(t, journals, j, 1926, 1, 1)
	p := &models.Person{Name: "Закрытый Автор"}
	if err := persons.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	kind := "статья"
	for _, w := range []*models.Work{w1, w2} {
		ch := &models.Chapter{WorkID: w.ID, Title: "Статья", Type: "chapter", OrderNumber: 1,
			StartPage: 1, EndPage: 2, ArticleKind: &kind}
		if err := chapters.Create(ctx, ch); err != nil {
			t.Fatal(err)
		}
		if err := persons.ReplaceCredits(ctx, ch.ID, []models.CreditInput{{Role: "author", Printed: "З. А.", PersonID: &p.ID}}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := closed.SetWorkClosed(ctx, w2.ID, true); err != nil {
		t.Fatal(err)
	}
	d, err := persons.Detail(ctx, p.Slug, false)
	if err != nil || len(d.Articles) != 1 || d.Articles[0].WorkID != w1.ID {
		t.Fatalf("наполовину закрыт, Detail(false): %+v, %v", d, err)
	}
	if d, err = persons.Detail(ctx, p.Slug, true); err != nil || len(d.Articles) != 2 {
		t.Fatalf("Detail(true): %+v, %v", d, err)
	}

	if _, err := closed.SetWorkClosed(ctx, w1.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := persons.Detail(ctx, p.Slug, false); !errors.Is(err, ErrPersonNotFound) {
		t.Fatalf("всё закрыто, Detail(false): %v, ждали ErrPersonNotFound", err)
	}
	if d, err = persons.Detail(ctx, p.Slug, true); err != nil || len(d.Articles) != 2 {
		t.Fatalf("всё закрыто, Detail(true): %+v, %v", d, err)
	}
}

func TestPersonWithoutCreditsIsNotHidden(t *testing.T) {
	ctx := context.Background()
	persons := NewPersonRepository(testPool(t))
	p := &models.Person{Name: "Без Статей"}
	if err := persons.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if d, err := persons.Detail(ctx, p.Slug, false); err != nil || len(d.Articles) != 0 {
		t.Fatalf("человек без статей: %+v, %v", d, err)
	}
}
