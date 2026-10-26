package repository

import (
	"context"
	"errors"
	"sort"
	"testing"

	"proofreader/internal/models"
)

type eventFixture struct {
	repo *EventRepository
	org  *models.EventOrganization
	kind *models.EventKind
}

func newEventFixture(t *testing.T, orgSlug string) eventFixture {
	t.Helper()
	ctx := context.Background()
	repo := NewEventRepository(testPool(t))
	org := &models.EventOrganization{Slug: orgSlug, Title: "Организация А"}
	if err := repo.CreateOrganization(ctx, org); err != nil {
		t.Fatalf("create org: %v", err)
	}
	kind := &models.EventKind{OrganizationID: org.ID, Title: "Съезды"}
	if err := repo.CreateKind(ctx, kind); err != nil {
		t.Fatalf("create kind: %v", err)
	}
	return eventFixture{repo, org, kind}
}

func (f eventFixture) event(t *testing.T, slug string) *models.Event {
	t.Helper()
	e := &models.Event{KindID: f.kind.ID, Slug: slug, Title: "I съезд"}
	if err := f.repo.CreateEvent(context.Background(), e); err != nil {
		t.Fatalf("create event: %v", err)
	}
	return e
}

// worksAndChapters: тома 10 («Том А») и 11 («Том Б»), главы 100—102 в А, 110 в Б.
func seedEventCorpus(t *testing.T) {
	t.Helper()
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO works (id, title, author, file_path, owner_id, role)
		VALUES (10, 'Том А', 'Автор', 'f', 1, 'volume'),
		       (11, 'Том Б', 'Автор', 'f', 1, 'volume');
		INSERT INTO chapters (id, work_id, title, type, order_number, start_page, end_page)
		VALUES (100, 10, 'Глава 1', 'chapter', 1, 1, 5),
		       (101, 10, 'Глава 2', 'chapter', 2, 6, 9),
		       (102, 10, 'Глава 3', 'chapter', 3, 10, 12),
		       (110, 11, 'Глава Б', 'chapter', 1, 1, 5)`); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func ch(id int64) *int64 { return &id }

func TestEventCreateWithExplicitID(t *testing.T) {
	ctx := context.Background()
	f := newEventFixture(t, "org-a")
	e := &models.Event{ID: 900, KindID: f.kind.ID, Slug: "s1", Title: "I съезд"}
	if err := f.repo.CreateEvent(ctx, e); err != nil {
		t.Fatal(err)
	}
	got, err := f.repo.EventByID(ctx, 900)
	if err != nil || got.Slug != "s1" {
		t.Fatalf("EventByID: %v %+v", err, got)
	}
	dup := &models.Event{ID: 900, KindID: f.kind.ID, Slug: "s2", Title: "II съезд"}
	if err := f.repo.CreateEvent(ctx, dup); !errors.Is(err, ErrEventIDTaken) {
		t.Fatalf("повтор id: %v", err)
	}
	next := f.event(t, "s3")
	if next.ID <= 900 {
		t.Fatalf("следующий id %d не больше 900", next.ID)
	}
	// организация и вид — тоже с явным id
	org := &models.EventOrganization{ID: 700, Slug: "org-x", Title: "Организация Б"}
	if err := f.repo.CreateOrganization(ctx, org); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.CreateOrganization(ctx, &models.EventOrganization{ID: 700, Slug: "org-y", Title: "В"}); !errors.Is(err, ErrEventIDTaken) {
		t.Fatalf("повтор id организации: %v", err)
	}
	k := &models.EventKind{ID: 800, OrganizationID: org.ID, Title: "Пленумы"}
	if err := f.repo.CreateKind(ctx, k); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.CreateKind(ctx, &models.EventKind{ID: 800, OrganizationID: org.ID, Title: "Ещё"}); !errors.Is(err, ErrEventIDTaken) {
		t.Fatalf("повтор id вида: %v", err)
	}
	k2 := &models.EventKind{OrganizationID: org.ID, Title: "Ещё"}
	if err := f.repo.CreateKind(ctx, k2); err != nil || k2.ID <= 800 {
		t.Fatalf("следующий вид: %v id=%d", err, k2.ID)
	}
	got2, err := f.repo.KindByID(ctx, 800)
	if err != nil || got2.OrganizationID != org.ID {
		t.Fatalf("KindByID: %v %+v", err, got2)
	}
	if _, err := f.repo.KindByID(ctx, 99999); !errors.Is(err, ErrEventKindNotFound) {
		t.Fatalf("KindByID missing: %v", err)
	}
}

func TestEventSlugTakenGlobally(t *testing.T) {
	ctx := context.Background()
	a := newEventFixture(t, "org-a")
	a.event(t, "same")
	b := &models.EventOrganization{Slug: "org-b", Title: "Организация Б"}
	if err := a.repo.CreateOrganization(ctx, b); err != nil {
		t.Fatal(err)
	}
	k := &models.EventKind{OrganizationID: b.ID, Title: "Съезды"}
	if err := a.repo.CreateKind(ctx, k); err != nil {
		t.Fatal(err)
	}
	err := a.repo.CreateEvent(ctx, &models.Event{KindID: k.ID, Slug: "same", Title: "I съезд"})
	if !errors.Is(err, ErrEventSlugTaken) {
		t.Fatalf("want ErrEventSlugTaken, got %v", err)
	}
	if err := a.repo.CreateOrganization(ctx, &models.EventOrganization{Slug: "org-a", Title: "Дубль"}); !errors.Is(err, ErrEventSlugTaken) {
		t.Fatalf("slug организации: %v", err)
	}
}

func TestReplaceAgendaRoundTrip(t *testing.T) {
	ctx := context.Background()
	seedEventCorpus(t)
	f := newEventFixture(t, "org-a")
	e := f.event(t, "s1")
	in := models.AgendaInput{Sections: []models.AgendaSectionInput{
		{Title: "Пункт 1", Entries: []models.AgendaEntryInput{
			{Label: "Доклад", Targets: []models.AgendaTargetInput{
				{Kind: "chapter", WorkID: 10, ChapterID: ch(102), Label: "речь"},
				{Kind: "chapter", WorkID: 10, ChapterID: ch(100)},
				{Kind: "chapter", WorkID: 10, ChapterID: ch(101)},
			}},
			{Label: "Резолюция", Targets: []models.AgendaTargetInput{{Kind: "work", WorkID: 11}}},
		}},
		{Title: "Пункт 2", Entries: []models.AgendaEntryInput{{Label: "Прения"}}},
	}}
	if err := f.repo.ReplaceAgenda(ctx, e.ID, in); err != nil {
		t.Fatal(err)
	}
	d, err := f.repo.EventDetail(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Organization.Slug != "org-a" || d.KindTitle != "Съезды" || len(d.Sections) != 2 {
		t.Fatalf("detail: %+v", d)
	}
	s0 := d.Sections[0]
	if s0.Title != "Пункт 1" || s0.Position != 0 || d.Sections[1].Position != 1 || len(s0.Entries) != 2 {
		t.Fatalf("section: %+v", s0)
	}
	ts := s0.Entries[0].Targets
	if len(ts) != 3 || ts[0].Title != "Глава 3" || ts[1].Title != "Глава 1" || ts[2].Title != "Глава 2" {
		t.Fatalf("порядок целей: %+v", ts)
	}
	if ts[0].WorkTitle != "Том А" || ts[0].Label != "речь" || ts[0].Broken || *ts[0].ChapterID != 102 {
		t.Fatalf("цель: %+v", ts[0])
	}
	w := s0.Entries[1].Targets[0]
	if w.Kind != "work" || w.Title != "Том Б" || w.ChapterID != nil || w.Broken {
		t.Fatalf("цель-работа: %+v", w)
	}
	if d.Sections[1].Entries[0].Targets == nil || len(d.Sections[1].Entries[0].Targets) != 0 {
		t.Fatalf("пустые цели должны быть []: %#v", d.Sections[1].Entries[0].Targets)
	}
	// перезапись заменяет целиком
	if err := f.repo.ReplaceAgenda(ctx, e.ID, models.AgendaInput{}); err != nil {
		t.Fatal(err)
	}
	d, _ = f.repo.EventDetail(ctx, "s1")
	if d.Sections == nil || len(d.Sections) != 0 {
		t.Fatalf("после пустой записи: %#v", d.Sections)
	}
}

func TestReplaceAgendaRejectsForeignChapter(t *testing.T) {
	ctx := context.Background()
	seedEventCorpus(t)
	f := newEventFixture(t, "org-a")
	e := f.event(t, "s1")
	good := models.AgendaInput{Sections: []models.AgendaSectionInput{{Title: "П", Entries: []models.AgendaEntryInput{
		{Label: "Доклад", Targets: []models.AgendaTargetInput{{Kind: "chapter", WorkID: 10, ChapterID: ch(100)}}}}}}}
	if err := f.repo.ReplaceAgenda(ctx, e.ID, good); err != nil {
		t.Fatal(err)
	}
	for name, tg := range map[string]models.AgendaTargetInput{
		"чужая глава":     {Kind: "chapter", WorkID: 10, ChapterID: ch(110)},
		"нет главы":       {Kind: "chapter", WorkID: 10, ChapterID: ch(9999)},
		"нет работы":      {Kind: "work", WorkID: 9999},
		"глава без id":    {Kind: "chapter", WorkID: 10},
		"неизвестный вид": {Kind: "page", WorkID: 10},
		"работа с главой": {Kind: "work", WorkID: 10, ChapterID: ch(100)},
	} {
		bad := models.AgendaInput{Sections: []models.AgendaSectionInput{{Title: "П", Entries: []models.AgendaEntryInput{
			{Label: "Доклад", Targets: []models.AgendaTargetInput{tg}}}}}}
		if err := f.repo.ReplaceAgenda(ctx, e.ID, bad); !errors.Is(err, ErrAgendaTarget) {
			t.Fatalf("%s: want ErrAgendaTarget, got %v", name, err)
		}
	}
	d, _ := f.repo.EventDetail(ctx, "s1")
	if len(d.Sections) != 1 || d.Sections[0].Entries[0].Targets[0].Title != "Глава 1" {
		t.Fatalf("прежняя повестка тронута: %+v", d.Sections)
	}
	if err := f.repo.ReplaceAgenda(ctx, 99999, good); !errors.Is(err, ErrEventNotFound) {
		t.Fatalf("нет события: %v", err)
	}
}

func TestDeletedChapterMakesTargetBroken(t *testing.T) {
	ctx := context.Background()
	seedEventCorpus(t)
	f := newEventFixture(t, "org-a")
	e := f.event(t, "s1")
	in := models.AgendaInput{Sections: []models.AgendaSectionInput{{Title: "П", Entries: []models.AgendaEntryInput{
		{Label: "Доклад", Targets: []models.AgendaTargetInput{{Kind: "chapter", WorkID: 10, ChapterID: ch(100)}}}}}}}
	if err := f.repo.ReplaceAgenda(ctx, e.ID, in); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool(t).Exec(ctx, `DELETE FROM chapters WHERE id = 100`); err != nil {
		t.Fatal(err)
	}
	d, _ := f.repo.EventDetail(ctx, "s1")
	tg := d.Sections[0].Entries[0].Targets[0]
	if tg.Kind != "chapter" || !tg.Broken || tg.Title != "Глава 1" || tg.ChapterID != nil {
		t.Fatalf("битая цель: %+v", tg)
	}
	if tg.Title == tg.WorkTitle {
		t.Fatalf("заглавие подменено томом: %+v", tg)
	}
}

func TestOrganizationsCarryTargetWorkIDs(t *testing.T) {
	ctx := context.Background()
	seedEventCorpus(t)
	f := newEventFixture(t, "org-a")
	e := f.event(t, "s1")
	in := models.AgendaInput{Sections: []models.AgendaSectionInput{{Title: "П", Entries: []models.AgendaEntryInput{
		{Label: "Доклад", Targets: []models.AgendaTargetInput{
			{Kind: "chapter", WorkID: 10, ChapterID: ch(100)},
			{Kind: "chapter", WorkID: 10, ChapterID: ch(101)},
			{Kind: "work", WorkID: 11},
			{Kind: "chapter", WorkID: 11, ChapterID: ch(110)},
		}}}}}}
	if err := f.repo.ReplaceAgenda(ctx, e.ID, in); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool(t).Exec(ctx, `DELETE FROM chapters WHERE id = 110`); err != nil {
		t.Fatal(err)
	}
	orgs, err := f.repo.Organizations(ctx)
	if err != nil || len(orgs) != 1 || len(orgs[0].Kinds) != 1 || len(orgs[0].Kinds[0].Events) != 1 {
		t.Fatalf("organizations: %v %+v", err, orgs)
	}
	s := orgs[0].Kinds[0].Events[0]
	ids := append([]int64(nil), s.TargetWorkIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) != 3 || ids[0] != 10 || ids[1] != 10 || ids[2] != 11 {
		t.Fatalf("TargetWorkIDs: %v", ids)
	}
	if s.MaterialsTotal != 3 {
		t.Fatalf("MaterialsTotal: %d", s.MaterialsTotal)
	}
	// TargetChapterIDs идёт параллельно TargetWorkIDs; 0 — цель целая работа.
	pairs := map[[2]int64]bool{}
	if len(s.TargetChapterIDs) != len(s.TargetWorkIDs) {
		t.Fatalf("TargetChapterIDs: %v при %v", s.TargetChapterIDs, s.TargetWorkIDs)
	}
	for i := range s.TargetWorkIDs {
		pairs[[2]int64{s.TargetWorkIDs[i], s.TargetChapterIDs[i]}] = true
	}
	if !pairs[[2]int64{10, 100}] || !pairs[[2]int64{10, 101}] || !pairs[[2]int64{11, 0}] || len(pairs) != 3 {
		t.Fatalf("пары работа/глава: %v", pairs)
	}
	od, err := f.repo.OrganizationDetail(ctx, "org-a")
	if err != nil || len(od.Kinds[0].Events[0].TargetWorkIDs) != 3 {
		t.Fatalf("OrganizationDetail: %v %+v", err, od)
	}
	if _, err := f.repo.OrganizationDetail(ctx, "нет"); !errors.Is(err, ErrEventOrgNotFound) {
		t.Fatalf("нет организации: %v", err)
	}
	if _, err := f.repo.EventDetail(ctx, "нет"); !errors.Is(err, ErrEventNotFound) {
		t.Fatalf("нет события: %v", err)
	}
}

func TestOrganizationsEmptyKindSerializesEvents(t *testing.T) {
	ctx := context.Background()
	f := newEventFixture(t, "org-a")
	orgs, err := f.repo.Organizations(ctx)
	if err != nil || orgs[0].Kinds[0].Events == nil {
		t.Fatalf("Events должен быть []: %v %#v", err, orgs)
	}
}

func TestChapterEventsLinks(t *testing.T) {
	ctx := context.Background()
	seedEventCorpus(t)
	f := newEventFixture(t, "org-a")
	e1 := f.event(t, "s1")
	e2 := f.event(t, "s2")
	mk := func(title string) models.AgendaInput {
		return models.AgendaInput{Sections: []models.AgendaSectionInput{
			{Title: "Первый", Entries: []models.AgendaEntryInput{{Label: "Х"}}},
			{Title: title, Entries: []models.AgendaEntryInput{{Label: "Доклад", Targets: []models.AgendaTargetInput{
				{Kind: "chapter", WorkID: 10, ChapterID: ch(100)}}}}}}}
	}
	if err := f.repo.ReplaceAgenda(ctx, e1.ID, mk("Доклады")); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.ReplaceAgenda(ctx, e2.ID, mk("Отчёт")); err != nil {
		t.Fatal(err)
	}
	links, err := f.repo.ChapterEvents(ctx, 10, 100)
	if err != nil || len(links) != 2 {
		t.Fatalf("links: %v %+v", err, links)
	}
	l := links[0]
	if l.OrganizationSlug != "org-a" || l.OrganizationTitle != "Организация А" || l.EventSlug != "s1" ||
		l.EventTitle != "I съезд" || l.SectionTitle != "Доклады" || l.SectionPosition != 1 {
		t.Fatalf("link: %+v", l)
	}
	if _, err := f.repo.ChapterEvents(ctx, 10, 110); !errors.Is(err, ErrChapterNotInWork) {
		t.Fatalf("чужая глава: %v", err)
	}
	if _, err := f.repo.ChapterEvents(ctx, 10, 9999); !errors.Is(err, ErrChapterNotInWork) {
		t.Fatalf("нет главы: %v", err)
	}
	none, err := f.repo.ChapterEvents(ctx, 10, 101)
	if err != nil || none == nil || len(none) != 0 {
		t.Fatalf("без событий должно быть []: %v %#v", err, none)
	}
}

func TestDeleteKindInUse(t *testing.T) {
	ctx := context.Background()
	f := newEventFixture(t, "org-a")
	e := f.event(t, "s1")
	if err := f.repo.DeleteKind(ctx, f.kind.ID); !errors.Is(err, ErrEventKindInUse) {
		t.Fatalf("want ErrEventKindInUse, got %v", err)
	}
	if err := f.repo.DeleteEvent(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.DeleteEvent(ctx, e.ID); !errors.Is(err, ErrEventNotFound) {
		t.Fatalf("повторное удаление: %v", err)
	}
	if err := f.repo.DeleteKind(ctx, f.kind.ID); err != nil {
		t.Fatal(err)
	}
}

func TestEventUpdatesKeepSlug(t *testing.T) {
	ctx := context.Background()
	f := newEventFixture(t, "org-a")
	e := f.event(t, "s1")
	e.Slug = "changed"
	e.Title = "II съезд"
	e.Dates = "1—2 марта"
	if err := f.repo.UpdateEvent(ctx, e); err != nil {
		t.Fatal(err)
	}
	got, _ := f.repo.EventByID(ctx, e.ID)
	if got.Slug != "s1" || got.Title != "II съезд" || got.Dates != "1—2 марта" {
		t.Fatalf("update: %+v", got)
	}
	f.org.Title = "Новое"
	f.org.Slug = "changed"
	if err := f.repo.UpdateOrganization(ctx, f.org, nil); err != nil {
		t.Fatal(err)
	}
	od, _ := f.repo.OrganizationDetail(ctx, "org-a")
	if od.Title != "Новое" {
		t.Fatalf("org: %+v", od)
	}
	f.kind.Title = "Пленумы"
	if err := f.repo.UpdateKind(ctx, f.kind); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.UpdateKind(ctx, &models.EventKind{ID: 99999, Title: "x"}); !errors.Is(err, ErrEventKindNotFound) {
		t.Fatalf("%v", err)
	}
	if err := f.repo.UpdateEvent(ctx, &models.Event{ID: 99999, Title: "x"}); !errors.Is(err, ErrEventNotFound) {
		t.Fatalf("%v", err)
	}
	if err := f.repo.UpdateOrganization(ctx, &models.EventOrganization{ID: 99999, Title: "x"}, nil); !errors.Is(err, ErrEventOrgNotFound) {
		t.Fatalf("%v", err)
	}
}

// Правка организации без раздела (nil) раздел не стирает; список
// организаций его отдаёт.
func TestUpdateOrganizationKeepsShelfGroup(t *testing.T) {
	ctx := context.Background()
	f := newEventFixture(t, "org-a")
	group := "Раздел Б"
	if err := f.repo.UpdateOrganization(ctx, f.org, &group); err != nil {
		t.Fatal(err)
	}
	f.org.Title = "Новое"
	if err := f.repo.UpdateOrganization(ctx, f.org, nil); err != nil {
		t.Fatal(err)
	}
	orgs, err := f.repo.Organizations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(orgs) != 1 || orgs[0].Title != "Новое" || orgs[0].ShelfGroup != "Раздел Б" {
		t.Fatalf("организации: %+v", orgs)
	}
	empty := ""
	if err := f.repo.UpdateOrganization(ctx, f.org, &empty); err != nil {
		t.Fatal(err)
	}
	od, err := f.repo.OrganizationDetail(ctx, "org-a")
	if err != nil || od.ShelfGroup != "" {
		t.Fatalf("после пустого: %+v %v", od, err)
	}
}
