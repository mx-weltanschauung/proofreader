package repository

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

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
		if err := editions.Create(ctx, e, models.ClosureActor{}, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := closed.SetEditionClosed(ctx, f.closedEdition.ID, true, models.ClosureActor{Name: "test"}, "test"); err != nil {
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
		if _, err := closed.SetWorkClosed(ctx, w.ID, true, models.ClosureActor{Name: "test"}, "test"); err != nil {
			t.Fatal(err)
		}
	}

	f.closedJournal = newJournal(t, journals, "closed-jr")
	_, f.inClosedJournal = newIssue(t, journals, f.closedJournal, 1925, 1, 1)
	if _, err := closed.SetJournalClosed(ctx, f.closedJournal.ID, true, models.ClosureActor{Name: "test"}, "test"); err != nil {
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
	if err := repo.Create(context.Background(), w, models.ClosureActor{}, ""); err != nil {
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
	if err := editions.Create(ctx, e, models.ClosureActor{}, ""); err != nil {
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
		got, err := closed.SetEditionClosed(ctx, e.ID, true, models.ClosureActor{Name: "test"}, "test")
		if err != nil {
			t.Fatal(err)
		}
		got = sortedIDs(got)
		if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
			t.Fatalf("вызов %d: вернулись %v, ждали %v", i+1, got, want)
		}
	}
	if _, err := closed.SetEditionClosed(ctx, 999999, true, models.ClosureActor{Name: "test"}, "test"); !errors.Is(err, ErrClosedTargetNotFound) {
		t.Errorf("нет издания: %v, ждали ErrClosedTargetNotFound", err)
	}
	if _, err := closed.SetWorkClosed(ctx, 999999, true, models.ClosureActor{Name: "test"}, "test"); !errors.Is(err, ErrClosedTargetNotFound) {
		t.Errorf("нет работы: %v", err)
	}
	if _, err := closed.SetJournalClosed(ctx, 999999, true, models.ClosureActor{Name: "test"}, "test"); !errors.Is(err, ErrClosedTargetNotFound) {
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
	got, err := closed.SetWorkClosed(ctx, vol.ID, true, models.ClosureActor{Name: "test"}, "test")
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
	got, err = closed.SetJournalClosed(ctx, j.ID, true, models.ClosureActor{Name: "test"}, "test")
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
	if err := works.Create(ctx, w, models.ClosureActor{Name: "test"}, "test"); err != nil {
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
	if err := editions.Create(ctx, e, models.ClosureActor{Name: "test"}, "test"); err != nil {
		t.Fatal(err)
	}
	ge, err := editions.GetByID(ctx, e.ID)
	if err != nil || !ge.Closed {
		t.Errorf("издание: %+v, %v", ge, err)
	}

	j := &models.Journal{Slug: "closed-born-jr", Title: "Проба", Closed: true}
	if err := journals.Create(ctx, j, models.ClosureActor{Name: "test"}, "test"); err != nil {
		t.Fatal(err)
	}
	gj, err := journals.GetByID(ctx, j.ID)
	if err != nil || !gj.Closed {
		t.Errorf("журнал: %+v, %v", gj, err)
	}

	open := newJournal(t, journals, "open-born-jr")
	issue := &models.JournalIssue{JournalID: open.ID, Year: 1926, NumberFrom: 1, NumberTo: 1, Label: "1"}
	iw := &models.Work{Title: "номер", Language: "ru", Status: models.WorkStatusDraft, OwnerID: 1, Closed: true}
	if err := journals.CreateIssue(ctx, issue, iw, models.ClosureActor{Name: "test"}, "test"); err != nil {
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
	if err := editions.Create(ctx, e, models.ClosureActor{}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := closed.SetEditionClosed(ctx, e.ID, true, models.ClosureActor{Name: "test"}, "test"); err != nil {
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
	if _, err := closed.SetJournalClosed(ctx, j.ID, true, models.ClosureActor{Name: "test"}, "test"); err != nil {
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
	if _, err := closed.SetJournalClosed(ctx, j.ID, true, models.ClosureActor{Name: "test"}, "test"); err != nil {
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
	if _, err := closed.SetWorkClosed(ctx, w2.ID, true, models.ClosureActor{Name: "test"}, "test"); err != nil {
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

	if _, err := closed.SetWorkClosed(ctx, w2.ID, true, models.ClosureActor{Name: "test"}, "test"); err != nil {
		t.Fatal(err)
	}
	d, err := persons.Detail(ctx, p.Slug, false)
	if err != nil || len(d.Articles) != 1 || d.Articles[0].WorkID != w1.ID {
		t.Fatalf("наполовину закрыт, Detail(false): %+v, %v", d, err)
	}
	if d, err = persons.Detail(ctx, p.Slug, true); err != nil || len(d.Articles) != 2 {
		t.Fatalf("Detail(true): %+v, %v", d, err)
	}

	if _, err := closed.SetWorkClosed(ctx, w1.ID, true, models.ClosureActor{Name: "test"}, "test"); err != nil {
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

// Снос закрытого издания открыл бы его тома (edition_id — ON DELETE SET
// NULL): отказ, издание и привязка томов на месте. Открытое удаляется.
func TestDeleteClosedEditionRefused(t *testing.T) {
	ctx := context.Background()
	f, closed := newClosedFixture(t)
	editions := NewEditionRepository(testPool(t))

	if err := editions.Delete(ctx, f.closedEdition.ID); !errors.Is(err, ErrEditionClosed) {
		t.Fatalf("Delete закрытого: %v, ждали ErrEditionClosed", err)
	}
	if by, err := closed.ClosedBy(ctx, f.inClosedEdition.ID); err != nil || by != models.ClosedByEdition {
		t.Errorf("том закрытого издания после отказа: %q, %v", by, err)
	}
	if err := editions.Delete(ctx, f.openEdit.ID); err != nil {
		t.Errorf("Delete открытого: %v", err)
	}
	if err := editions.Delete(ctx, 999999); err == nil || errors.Is(err, ErrEditionClosed) {
		t.Errorf("Delete несуществующего: %v, ждали «не найдено»", err)
	}
}

// Причина закрытости соединениями (closedByJoins/closedByCase — полка,
// сводки издания, worksWithSlug) обязана совпадать с work_closed_by на каждом
// пути: функция — образец, списки считают то же без вызова на строку.
func TestClosedBySetMatchesFunction(t *testing.T) {
	ctx := context.Background()
	f, closed := newClosedFixture(t)
	pool := testPool(t)
	works := NewWorkRepository(pool)
	editions := NewEditionRepository(pool)

	// Том открытого издания и его служебная: в сводки попадает и пустая причина.
	inOpenEdition := newVolume(t, works, "в открытом издании")
	if _, err := pool.Exec(ctx, `UPDATE works SET edition_id = $2 WHERE id = $1`, inOpenEdition.ID, f.openEdit.ID); err != nil {
		t.Fatal(err)
	}
	childOfOpen := newChild(t, works, inOpenEdition, "служебная открытого")

	all := []*models.Work{f.open, f.closedSelf, f.childOfClosed, f.inClosedEdition,
		f.childInClosedEdition, f.inClosedJournal, f.closedSelfAndEdition, inOpenEdition, childOfOpen}
	want := map[int64]string{}
	for _, w := range all {
		by, err := closed.ClosedBy(ctx, w.ID)
		if err != nil {
			t.Fatal(err)
		}
		want[w.ID] = by
	}

	// Сами фрагменты — по всей таблице.
	rows, err := pool.Query(ctx, `SELECT works.id, `+closedByCase+`, work_closed_by(works.id) FROM works`+closedByJoins)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for rows.Next() {
		var id int64
		var set, fn string
		if err := rows.Scan(&id, &set, &fn); err != nil {
			t.Fatal(err)
		}
		n++
		if set != fn {
			t.Errorf("работа %d: соединения %q, функция %q", id, set, fn)
		}
	}
	if rows.Err() != nil || n < len(all) {
		t.Fatalf("строк %d (ждали не меньше %d): %v", n, len(all), rows.Err())
	}

	// worksWithSlug — карточка работы и дети тома.
	for _, w := range all {
		got, err := works.GetByID(ctx, w.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.ClosedBy != want[w.ID] {
			t.Errorf("GetByID %d %q: ClosedBy %q, функция %q", w.ID, w.Title, got.ClosedBy, want[w.ID])
		}
	}
	kids, err := works.ListChildren(ctx, f.inClosedEdition.ID)
	if err != nil || len(kids) != 1 || kids[0].ClosedBy != models.ClosedByParent {
		t.Errorf("ListChildren тома закрытого издания: %+v, %v", kids, err)
	}

	// Сводки полки и издания: в них только тома с изданием.
	sums, err := editions.ListAllWorkSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, s := range sums {
		if w, ok := want[s.ID]; ok {
			seen++
			if s.ClosedBy != w {
				t.Errorf("сводка %d: ClosedBy %q, функция %q", s.ID, s.ClosedBy, w)
			}
		}
	}
	if seen != 3 {
		t.Errorf("в сводках %d томов фикстуры, ждали 3 (два в закрытом издании, один в открытом)", seen)
	}
	for _, ed := range []int64{f.closedEdition.ID, f.openEdit.ID} {
		one, err := editions.ListWorkSummaries(ctx, ed)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range one {
			if s.ClosedBy != want[s.ID] {
				t.Errorf("сводка издания %d, том %d: ClosedBy %q, функция %q", ed, s.ID, s.ClosedBy, want[s.ID])
			}
		}
	}
}

// --- закрытые главы -------------------------------------------------------

var testActor = models.ClosureActor{Name: "админ"}

func newPages(t *testing.T, pool *pgxpool.Pool, workID int64, from, to int) {
	t.Helper()
	pages := NewPageRepository(pool)
	for n := from; n <= to; n++ {
		p := &models.Page{WorkID: workID, PageNumber: n, PreviewPath: "p.png",
			ContentMarkdown: "x", Status: models.PageStatusNotProofread}
		if err := pages.Create(context.Background(), p); err != nil {
			t.Fatalf("page %d: %v", n, err)
		}
	}
}

func newChapter(t *testing.T, chapters *ChapterRepository, workID int64, parent *int64, title string, start, end, order int) *models.Chapter {
	t.Helper()
	c := &models.Chapter{WorkID: workID, ParentID: parent, Title: title, Type: "chapter",
		OrderNumber: order, StartPage: start, EndPage: end}
	if err := chapters.Create(context.Background(), c); err != nil {
		t.Fatalf("chapter %q: %v", title, err)
	}
	return c
}

func closedPageNumbers(t *testing.T, pool *pgxpool.Pool, workID int64) []int {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT page_number FROM closed_pages WHERE work_id = $1 ORDER BY page_number`, workID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	return out
}

func TestClosedPagesChapterRange(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	works, chapters, closed := NewWorkRepository(pool), NewChapterRepository(pool), NewClosedRepository(pool)
	w := newVolume(t, works, "главы")
	newPages(t, pool, w.ID, 8, 22)
	first := newChapter(t, chapters, w.ID, nil, "первая", 10, 14, 1)
	newChapter(t, chapters, w.ID, nil, "вторая", 14, 20, 2)

	got, err := closed.SetChapterClosed(ctx, first.ID, true, testActor, "письмо")
	if err != nil || len(got) != 1 || got[0] != w.ID {
		t.Fatalf("SetChapterClosed = %v, %v; ждали [%d]", got, err, w.ID)
	}
	nums := closedPageNumbers(t, pool, w.ID)
	want := []int{10, 11, 12, 13, 14}
	if len(nums) != len(want) {
		t.Fatalf("closed_pages = %v, ждали %v", nums, want)
	}
	for i := range want {
		if nums[i] != want[i] {
			t.Fatalf("closed_pages = %v, ждали %v", nums, want)
		}
	}
	spans, err := closed.ClosedSpans(ctx, w.ID)
	if err != nil || len(spans) != 1 || spans[0] != (models.PageSpan{Start: 10, End: 14}) {
		t.Fatalf("ClosedSpans = %v, %v", spans, err)
	}
	ids, err := closed.ClosedChapterIDs(ctx, w.ID)
	if err != nil || len(ids) != 1 || !ids[first.ID] {
		t.Fatalf("ClosedChapterIDs = %v, %v", ids, err)
	}
	if _, err := closed.SetChapterClosed(ctx, 999999, true, testActor, "r"); !errors.Is(err, ErrClosedTargetNotFound) {
		t.Fatalf("нет главы: %v, ждали ErrClosedTargetNotFound", err)
	}
}

func TestClosedChapterClosesDescendants(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	works, chapters, closed := NewWorkRepository(pool), NewChapterRepository(pool), NewClosedRepository(pool)
	w := newVolume(t, works, "дерево")
	newPages(t, pool, w.ID, 1, 10)
	parent := newChapter(t, chapters, w.ID, nil, "родитель", 1, 8, 1)
	child := newChapter(t, chapters, w.ID, &parent.ID, "потомок", 3, 5, 1)
	other := newChapter(t, chapters, w.ID, nil, "соседка", 9, 10, 2)

	if _, err := closed.SetChapterClosed(ctx, parent.ID, true, testActor, "письмо"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		id   int64
		want string
	}{{parent.ID, "chapter"}, {child.ID, "parent"}, {other.ID, ""}} {
		got, err := chapters.GetByID(ctx, c.id)
		if err != nil {
			t.Fatal(err)
		}
		if got.ClosedBy != c.want {
			t.Errorf("глава %d: ClosedBy = %q, ждали %q", c.id, got.ClosedBy, c.want)
		}
		if got.Closed != (c.id == parent.ID) {
			t.Errorf("глава %d: Closed = %v", c.id, got.Closed)
		}
	}
	ids, err := closed.ClosedChapterIDs(ctx, w.ID)
	if err != nil || len(ids) != 2 || !ids[parent.ID] || !ids[child.ID] {
		t.Fatalf("ClosedChapterIDs = %v, %v", ids, err)
	}
	list, err := chapters.ListByWork(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	by := map[int64]string{}
	for _, c := range list {
		by[c.ID] = c.ClosedBy
	}
	if by[parent.ID] != "chapter" || by[child.ID] != "parent" || by[other.ID] != "" {
		t.Errorf("ListByWork closed_by = %v", by)
	}
}

func TestOpeningEditionKeepsChapterClosed(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	works, editions := NewWorkRepository(pool), NewEditionRepository(pool)
	chapters, closed := NewChapterRepository(pool), NewClosedRepository(pool)
	e := &models.Edition{Title: "ed", Slug: "ed-keep"}
	if err := editions.Create(ctx, e, testActor, ""); err != nil {
		t.Fatal(err)
	}
	w := newVolume(t, works, "в издании")
	if _, err := pool.Exec(ctx, `UPDATE works SET edition_id = $2 WHERE id = $1`, w.ID, e.ID); err != nil {
		t.Fatal(err)
	}
	newPages(t, pool, w.ID, 1, 10)
	ch := newChapter(t, chapters, w.ID, nil, "глава", 2, 4, 1)
	if _, err := closed.SetEditionClosed(ctx, e.ID, true, testActor, "издание"); err != nil {
		t.Fatal(err)
	}
	if _, err := closed.SetChapterClosed(ctx, ch.ID, true, testActor, "глава"); err != nil {
		t.Fatal(err)
	}
	if _, err := closed.SetEditionClosed(ctx, e.ID, false, testActor, "открыли"); err != nil {
		t.Fatal(err)
	}
	if nums := closedPageNumbers(t, pool, w.ID); len(nums) != 3 || nums[0] != 2 || nums[2] != 4 {
		t.Fatalf("после открытия издания closed_pages = %v, ждали 2..4", nums)
	}

	// Обратно: глава открыта внутри закрытого тома — полосы всё равно закрыты.
	if _, err := closed.SetChapterClosed(ctx, ch.ID, false, testActor, "открыли главу"); err != nil {
		t.Fatal(err)
	}
	if nums := closedPageNumbers(t, pool, w.ID); len(nums) != 0 {
		t.Fatalf("всё открыто, а closed_pages = %v", nums)
	}
	if _, err := closed.SetWorkClosed(ctx, w.ID, true, testActor, "том"); err != nil {
		t.Fatal(err)
	}
	if nums := closedPageNumbers(t, pool, w.ID); len(nums) != 10 {
		t.Fatalf("закрытый том: closed_pages = %v, ждали все 10", nums)
	}
}

func TestSetChapterClosedWritesClosureInTx(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	works, chapters, closed := NewWorkRepository(pool), NewChapterRepository(pool), NewClosedRepository(pool)
	w := newVolume(t, works, "журнал")
	ch := newChapter(t, chapters, w.ID, nil, "Глава про X", 1, 3, 1)

	uid := int64(1)
	actor := models.ClosureActor{ID: &uid, Name: "admin@test.local"}
	if _, err := closed.SetChapterClosed(ctx, ch.ID, true, actor, "письмо от А"); err != nil {
		t.Fatal(err)
	}
	var kind, title, reason, name string
	var isClosed bool
	var actorID *int64
	if err := pool.QueryRow(ctx, `SELECT entity_kind, title_snapshot, closed, reason, actor_id, actor_name
		FROM closures WHERE entity_id = $1`, ch.ID).Scan(&kind, &title, &isClosed, &reason, &actorID, &name); err != nil {
		t.Fatal(err)
	}
	if kind != "chapter" || title != "Глава про X" || !isClosed || reason != "письмо от А" ||
		actorID == nil || *actorID != 1 || name != "admin@test.local" {
		t.Fatalf("строка журнала: %s %q %v %q %v %q", kind, title, isClosed, reason, actorID, name)
	}

	// Пустая причина: CHECK откатывает транзакцию, признак не меняется.
	if _, err := closed.SetChapterClosed(ctx, ch.ID, false, actor, "  "); err == nil {
		t.Fatal("пустая причина принята")
	}
	got, err := chapters.GetByID(ctx, ch.ID)
	if err != nil || !got.Closed {
		t.Fatalf("признак изменился после отказа: %+v, %v", got, err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM closures WHERE entity_id = $1`, ch.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("строк журнала %d, ждали 1 (%v)", n, err)
	}
	// Повтор того же значения тоже пишет строку.
	if _, err := closed.SetChapterClosed(ctx, ch.ID, true, actor, "ещё раз"); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM closures WHERE entity_id = $1`, ch.ID).Scan(&n); err != nil || n != 2 {
		t.Fatalf("после повтора строк журнала %d, ждали 2 (%v)", n, err)
	}
}

func TestCreateClosedWritesClosure(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	works := NewWorkRepository(pool)
	w := &models.Work{Title: "сразу закрыт", Language: "ru", Status: models.WorkStatusDraft,
		Role: models.WorkRoleVolume, OwnerID: 1, Closed: true}
	if err := works.Create(ctx, w, testActor, "закрыто при создании"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = works.Delete(ctx, w.ID) })
	var n int
	var closedFlag bool
	var reason string
	if err := pool.QueryRow(ctx, `SELECT count(*), bool_and(closed), min(reason) FROM closures
		WHERE entity_kind = 'work' AND entity_id = $1`, w.ID).Scan(&n, &closedFlag, &reason); err != nil {
		t.Fatal(err)
	}
	if n != 1 || !closedFlag || reason != "закрыто при создании" {
		t.Fatalf("журнал: n=%d closed=%v reason=%q", n, closedFlag, reason)
	}
	// Открытая работа журнала не пишет.
	o := newVolume(t, works, "открытая")
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM closures WHERE entity_id = $1`, o.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("открытая работа оставила %d строк журнала (%v)", n, err)
	}
}

func TestLatestClosure(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	works, closed := NewWorkRepository(pool), NewClosedRepository(pool)
	w := newVolume(t, works, "последняя")
	got, err := closed.LatestClosure(ctx, "work", w.ID)
	if err != nil || got != nil {
		t.Fatalf("без строк: %v, %v; ждали nil, nil", got, err)
	}
	if _, err := closed.SetWorkClosed(ctx, w.ID, true, testActor, "первая"); err != nil {
		t.Fatal(err)
	}
	if _, err := closed.SetWorkClosed(ctx, w.ID, false, testActor, "вторая"); err != nil {
		t.Fatal(err)
	}
	got, err = closed.LatestClosure(ctx, "work", w.ID)
	if err != nil || got == nil || got.Reason != "вторая" || got.Closed || got.ActorName != "админ" || got.Title != w.Title {
		t.Fatalf("LatestClosure = %+v, %v", got, err)
	}
	list, err := closed.ListClosures(ctx, 10, 0)
	if err != nil || len(list) != 2 || list[0].Reason != "вторая" {
		t.Fatalf("ListClosures = %+v, %v", list, err)
	}
}
