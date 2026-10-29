package repository

import (
	"context"
	"errors"
	"strings"
	"testing"

	"proofreader/internal/models"
)

type marginFixture struct {
	works  *WorkRepository
	pages  *PageRepository
	chaps  *ChapterRepository
	closed *ClosedRepository
	users  *UserRepository
	repo   *MarginNoteRepository
}

func marginSetup(t *testing.T) (context.Context, *marginFixture, *models.User) {
	t.Helper()
	pool := testPool(t)
	f := &marginFixture{
		works:  NewWorkRepository(pool),
		pages:  NewPageRepository(pool),
		chaps:  NewChapterRepository(pool),
		closed: NewClosedRepository(pool),
		users:  NewUserRepository(pool),
		repo:   NewMarginNoteRepository(pool),
	}
	reader := createReader(t, f.users, "Читатель-"+strings.ReplaceAll(t.Name(), "/", "-"), "ip-"+t.Name())
	return context.Background(), f, reader
}

func (f *marginFixture) addPages(t *testing.T, workID int64, n int) []*models.Page {
	t.Helper()
	var out []*models.Page
	for i := 1; i <= n; i++ {
		p := &models.Page{WorkID: workID, PageNumber: i, ContentMarkdown: "текст", Status: models.PageStatusNotProofread}
		if err := f.pages.Create(context.Background(), p); err != nil {
			t.Fatalf("полоса %d: %v", i, err)
		}
		out = append(out, p)
	}
	return out
}

func (f *marginFixture) addChapter(t *testing.T, workID int64, parent *int64, title string, order, from, to int) *models.Chapter {
	t.Helper()
	c := &models.Chapter{WorkID: workID, ParentID: parent, Title: title, Type: "chapter", OrderNumber: order, StartPage: from, EndPage: to}
	if err := f.chaps.Create(context.Background(), c); err != nil {
		t.Fatalf("глава %q: %v", title, err)
	}
	return c
}

func quoteInput(owner, work int64, p *models.Page, body string) models.MarginNoteInput {
	n := p.PageNumber
	return models.MarginNoteInput{
		OwnerID: owner, WorkID: work, Target: models.MarginTargetQuote,
		StartPageID: &p.ID, EndPageID: &p.ID, StartPageNumber: &n, EndPageNumber: &n,
		QuoteKey: "ключ", QuoteTail: "хвост", QuoteText: "цитата", Body: body,
	}
}

func TestMarginNoteRoundTrip(t *testing.T) {
	ctx, f, reader := marginSetup(t)
	vol := newVolume(t, f.works, "заметки — круг")
	pg := f.addPages(t, vol.ID, 3)
	ch := f.addChapter(t, vol.ID, nil, "Глава один", 1, 1, 3)

	if _, err := f.repo.Create(ctx, models.MarginNoteInput{OwnerID: reader.ID, WorkID: vol.ID, Target: models.MarginTargetWork, Body: "о томе"}); err != nil {
		t.Fatalf("заметка на том: %v", err)
	}
	if _, err := f.repo.Create(ctx, models.MarginNoteInput{OwnerID: reader.ID, WorkID: vol.ID, Target: models.MarginTargetChapter,
		ChapterID: &ch.ID, ChapterTitle: ch.Title, Body: "о главе"}); err != nil {
		t.Fatalf("заметка на главу: %v", err)
	}
	q, err := f.repo.Create(ctx, quoteInput(reader.ID, vol.ID, pg[1], "о цитате"))
	if err != nil {
		t.Fatalf("заметка на цитату: %v", err)
	}
	if q.ID == 0 || q.CreatedAt.IsZero() {
		t.Fatalf("созданная заметка не заполнена: %+v", q)
	}

	list, err := f.repo.ListByWork(ctx, reader.ID, vol.ID, false)
	if err != nil || len(list) != 3 {
		t.Fatalf("ListByWork: %v, %d заметок", err, len(list))
	}
	// Без номера полосы (том, глава) идут первыми.
	if list[2].ID != q.ID {
		t.Fatalf("цитата должна идти последней, а идёт %+v", list[2])
	}
	got := list[2]
	if got.StartPage == nil || *got.StartPage != 2 || got.EndPage == nil || *got.EndPage != 2 || got.PageMissing {
		t.Fatalf("номера полосы цитаты: %+v", got)
	}
	if got.SnapshotStart == nil || *got.SnapshotStart != 2 || got.Body != "о цитате" || got.QuoteKey != "ключ" {
		t.Fatalf("поля цитаты: %+v", got)
	}
	if list[1].ChapterID == nil || *list[1].ChapterID != ch.ID || list[1].ChapterTitle != "Глава один" {
		t.Fatalf("поля главы: %+v", list[1])
	}

	upd, err := f.repo.UpdateBody(ctx, reader.ID, q.ID, "правка", false)
	if err != nil || upd.Body != "правка" || !upd.UpdatedAt.After(q.CreatedAt) && !upd.UpdatedAt.Equal(q.CreatedAt) {
		t.Fatalf("UpdateBody: %v %+v", err, upd)
	}
	if err := f.repo.Delete(ctx, reader.ID, q.ID, false); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if list, _ := f.repo.ListByWork(ctx, reader.ID, vol.ID, false); len(list) != 2 {
		t.Fatalf("после удаления %d заметок", len(list))
	}
}

func TestMarginNoteOwnerIsolation(t *testing.T) {
	ctx, f, a := marginSetup(t)
	b := createReader(t, f.users, "Чужой-читатель", "ip-other")
	vol := newVolume(t, f.works, "заметки — чужие")
	n, err := f.repo.Create(ctx, models.MarginNoteInput{OwnerID: a.ID, WorkID: vol.ID, Target: models.MarginTargetWork, Body: "моё"})
	if err != nil {
		t.Fatal(err)
	}
	if list, _ := f.repo.ListByWork(ctx, b.ID, vol.ID, true); len(list) != 0 {
		t.Fatalf("чужой видит %d заметок", len(list))
	}
	if _, err := f.repo.UpdateBody(ctx, b.ID, n.ID, "взлом", true); err == nil || !strings.HasSuffix(err.Error(), "not found") {
		t.Fatalf("чужая правка: %v", err)
	}
	if err := f.repo.Delete(ctx, b.ID, n.ID, true); err == nil || !strings.HasSuffix(err.Error(), "not found") {
		t.Fatalf("чужое удаление: %v", err)
	}
	if list, _ := f.repo.ListByWork(ctx, a.ID, vol.ID, false); len(list) != 1 || list[0].Body != "моё" {
		t.Fatalf("заметка владельца пострадала: %+v", list)
	}
}

func TestMarginNoteHidesClosed(t *testing.T) {
	ctx, f, reader := marginSetup(t)
	actor := models.ClosureActor{Name: "тест"}

	// 1. закрытая работа
	closedVol := newVolume(t, f.works, "заметки — закрытый том")
	// Заметка на сам том, без полосы и главы: скрыть её может только условие
	// по closed_works (полосы закрытой работы лежат и в closed_pages, так что
	// цитата прошла бы проверку и без него).
	nWork, _ := f.repo.Create(ctx, models.MarginNoteInput{OwnerID: reader.ID, WorkID: closedVol.ID,
		Target: models.MarginTargetWork, Body: "на закрытом томе"})

	// 2. открытый том: закрытая глава (с подглавой) и открытая глава
	vol := newVolume(t, f.works, "заметки — открытый том")
	pg := f.addPages(t, vol.ID, 6)
	open := f.addChapter(t, vol.ID, nil, "Открытая", 1, 1, 3)
	shut := f.addChapter(t, vol.ID, nil, "Закрытая", 2, 4, 6)
	sub := f.addChapter(t, vol.ID, &shut.ID, "Подглава", 1, 5, 6)
	_ = sub
	nOpen, _ := f.repo.Create(ctx, quoteInput(reader.ID, vol.ID, pg[0], "на открытой полосе"))
	nPage, _ := f.repo.Create(ctx, quoteInput(reader.ID, vol.ID, pg[4], "на полосе закрытой подглавы"))
	nChap, _ := f.repo.Create(ctx, models.MarginNoteInput{OwnerID: reader.ID, WorkID: vol.ID, Target: models.MarginTargetChapter,
		ChapterID: &shut.ID, ChapterTitle: shut.Title, Body: "на закрытой главе"})
	_ = open
	if _, err := f.closed.SetChapterClosed(ctx, shut.ID, true, actor, "проба"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.closed.SetWorkClosed(ctx, closedVol.ID, true, actor, "проба"); err != nil {
		t.Fatal(err)
	}

	ids := func(ns []*models.MarginNote) map[int64]bool {
		m := map[int64]bool{}
		for _, n := range ns {
			m[n.ID] = true
		}
		return m
	}

	byWork, err := f.repo.ListByWork(ctx, reader.ID, vol.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(byWork); len(got) != 1 || !got[nOpen.ID] {
		t.Fatalf("ListByWork без закрытого: %v", got)
	}
	if list, _ := f.repo.ListByWork(ctx, reader.ID, closedVol.ID, false); len(list) != 0 {
		t.Fatalf("закрытый том отдал заметки: %d", len(list))
	}
	all, total, err := f.repo.ListByOwner(ctx, reader.ID, nil, 50, 0, false)
	if err != nil || total != 1 || len(all) != 1 || all[0].ID != nOpen.ID {
		t.Fatalf("ListByOwner без закрытого: %v total=%d %d", err, total, len(all))
	}

	for _, id := range []int64{nWork.ID, nPage.ID, nChap.ID} {
		if _, err := f.repo.UpdateBody(ctx, reader.ID, id, "x", false); err == nil || !strings.HasSuffix(err.Error(), "not found") {
			t.Fatalf("UpdateBody закрытой %d: %v", id, err)
		}
		if err := f.repo.Delete(ctx, reader.ID, id, false); err == nil || !strings.HasSuffix(err.Error(), "not found") {
			t.Fatalf("Delete закрытой %d: %v", id, err)
		}
	}

	_, total, err = f.repo.ListByOwner(ctx, reader.ID, nil, 50, 0, true)
	if err != nil || total != 4 {
		t.Fatalf("includeClosed: total=%d err=%v", total, err)
	}
	if list, _ := f.repo.ListByWork(ctx, reader.ID, vol.ID, true); len(list) != 3 {
		t.Fatalf("includeClosed ListByWork: %d", len(list))
	}
	if _, err := f.repo.UpdateBody(ctx, reader.ID, nPage.ID, "сотрудник", true); err != nil {
		t.Fatalf("includeClosed UpdateBody: %v", err)
	}
	if err := f.repo.Delete(ctx, reader.ID, nChap.ID, true); err != nil {
		t.Fatalf("includeClosed Delete: %v", err)
	}
}

func TestMarginNoteSurvivesPageAndChapterDelete(t *testing.T) {
	ctx, f, reader := marginSetup(t)
	vol := newVolume(t, f.works, "заметки — снос")
	pg := f.addPages(t, vol.ID, 2)
	ch := f.addChapter(t, vol.ID, nil, "Исчезающая", 1, 1, 2)
	q, _ := f.repo.Create(ctx, quoteInput(reader.ID, vol.ID, pg[1], "цитата"))
	c, _ := f.repo.Create(ctx, models.MarginNoteInput{OwnerID: reader.ID, WorkID: vol.ID, Target: models.MarginTargetChapter,
		ChapterID: &ch.ID, ChapterTitle: ch.Title, Body: "глава"})

	if err := f.pages.Delete(ctx, pg[1].ID); err != nil {
		t.Fatal(err)
	}
	if err := f.chaps.Delete(ctx, ch.ID); err != nil {
		t.Fatal(err)
	}
	list, err := f.repo.ListByWork(ctx, reader.ID, vol.ID, false)
	if err != nil || len(list) != 2 {
		t.Fatalf("после сноса: %v, %d", err, len(list))
	}
	for _, n := range list {
		switch n.ID {
		case q.ID:
			if n.StartPage != nil || !n.PageMissing || n.SnapshotStart == nil || *n.SnapshotStart != 2 {
				t.Fatalf("цитата после сноса полосы: %+v", n)
			}
		case c.ID:
			if n.ChapterID != nil || n.ChapterTitle != "Исчезающая" {
				t.Fatalf("глава после сноса: %+v", n)
			}
		}
	}
}

func TestMarginNoteCascades(t *testing.T) {
	ctx, f, reader := marginSetup(t)
	other := createReader(t, f.users, "Уходящий-читатель", "ip-leaving")
	vol := newVolume(t, f.works, "заметки — каскад")
	if _, err := f.repo.Create(ctx, models.MarginNoteInput{OwnerID: reader.ID, WorkID: vol.ID, Target: models.MarginTargetWork, Body: "a"}); err != nil {
		t.Fatal(err)
	}
	vol2 := newVolume(t, f.works, "заметки — каскад 2")
	if _, err := f.repo.Create(ctx, models.MarginNoteInput{OwnerID: other.ID, WorkID: vol2.ID, Target: models.MarginTargetWork, Body: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := f.works.Delete(ctx, vol.ID); err != nil {
		t.Fatal(err)
	}
	if _, total, _ := f.repo.ListByOwner(ctx, reader.ID, nil, 10, 0, true); total != 0 {
		t.Fatalf("заметки пережили работу: %d", total)
	}
	if err := f.users.DeleteReader(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := f.repo.ListByWork(ctx, other.ID, vol2.ID, true); len(list) != 0 {
		t.Fatalf("заметки пережили читателя: %d", len(list))
	}
}

func TestMarginNoteLimit(t *testing.T) {
	ctx, f, reader := marginSetup(t)
	vol := newVolume(t, f.works, "заметки — потолок")
	old := marginNotesPerOwner
	marginNotesPerOwner = 2
	t.Cleanup(func() { marginNotesPerOwner = old })

	in := models.MarginNoteInput{OwnerID: reader.ID, WorkID: vol.ID, Target: models.MarginTargetWork, Body: "n"}
	for i := 0; i < 2; i++ {
		if _, err := f.repo.Create(ctx, in); err != nil {
			t.Fatalf("заметка %d: %v", i, err)
		}
	}
	if _, err := f.repo.Create(ctx, in); !errors.Is(err, ErrMarginNoteLimit) {
		t.Fatalf("третья: %v", err)
	}
}

func TestMarginNoteListByOwnerFields(t *testing.T) {
	ctx, f, reader := marginSetup(t)
	vol := newVolume(t, f.works, "Заметки поля")
	pg := f.addPages(t, vol.ID, 10)
	wide := f.addChapter(t, vol.ID, nil, "Широкая", 1, 1, 10)
	f.addChapter(t, vol.ID, &wide.ID, "Узкая", 1, 4, 6)
	f.addChapter(t, vol.ID, &wide.ID, "Средняя", 2, 3, 8)
	vol2 := newVolume(t, f.works, "Другой том")
	f.addPages(t, vol2.ID, 1)

	if _, err := f.repo.Create(ctx, quoteInput(reader.ID, vol.ID, pg[4], "в узкой")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.Create(ctx, models.MarginNoteInput{OwnerID: reader.ID, WorkID: vol2.ID, Target: models.MarginTargetWork, Body: "в другом"}); err != nil {
		t.Fatal(err)
	}

	all, total, err := f.repo.ListByOwner(ctx, reader.ID, nil, 10, 0, false)
	if err != nil || total != 2 || len(all) != 2 {
		t.Fatalf("список: %v total=%d", err, total)
	}
	// created_at DESC: свежая — на другом томе.
	if all[0].WorkTitle != "Другой том" || all[0].ContextChapter != "" {
		t.Fatalf("первая: %+v", all[0])
	}
	if all[1].WorkTitle != "Заметки поля" || all[1].WorkSlug != "zametki-polya" || all[1].ContextChapter != "Узкая" {
		t.Fatalf("вторая: %+v", all[1])
	}

	one, total, err := f.repo.ListByOwner(ctx, reader.ID, &vol2.ID, 10, 0, false)
	if err != nil || total != 1 || len(one) != 1 || one[0].WorkID != vol2.ID {
		t.Fatalf("фильтр по тому: %v total=%d", err, total)
	}
	page2, total, err := f.repo.ListByOwner(ctx, reader.ID, nil, 1, 1, false)
	if err != nil || total != 2 || len(page2) != 1 || page2[0].WorkID != vol.ID {
		t.Fatalf("смещение: %v total=%d", err, total)
	}
}
