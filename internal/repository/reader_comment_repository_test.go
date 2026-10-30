package repository

import (
	"errors"
	"strings"
	"testing"

	"proofreader/internal/models"
)

func TestReaderCommentModeration(t *testing.T) {
	ctx, f, reader := marginSetup(t)
	repo := NewReaderCommentRepository(testPool(t))
	vol := newVolume(t, f.works, "Комментарии")
	who := models.CommentViewer{ID: reader.ID}
	staff := models.CommentViewer{ID: 1, Staff: true, Admin: true}
	in := models.MarginNoteInput{OwnerID: reader.ID, WorkID: vol.ID, Target: "work", Body: "первое"}
	c, err := repo.Save(ctx, who, 0, in, "ip", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if list, err := repo.List(ctx, models.CommentViewer{}, vol.ID, true, false, false, 200, 0); err != nil || len(list) != 0 {
		t.Fatalf("неодобренное видно: %+v %v", list, err)
	}
	if err := repo.Review(ctx, staff, c.ID, 1, true, ""); err != nil {
		t.Fatal(err)
	}
	in.Body = "новая правка"
	c, err = repo.Save(ctx, who, c.ID, in, "ip", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	public, err := repo.List(ctx, models.CommentViewer{}, vol.ID, true, false, false, 200, 0)
	if err != nil || len(public) != 1 || public[0].Body != "первое" || public[0].Status != "" || public[0].RejectReason != "" || public[0].PublishedBody != "" {
		t.Fatalf("утечка черновика: %+v %v", public, err)
	}
	if err := repo.Review(ctx, staff, c.ID, 1, true, ""); !errors.Is(err, ErrCommentConflict) {
		t.Fatalf("устаревшее решение: %v", err)
	}
	if err := repo.Review(ctx, staff, c.ID, 2, false, "причина"); err != nil {
		t.Fatal(err)
	}
	own, err := repo.Get(ctx, who, c.ID)
	if err != nil || own.Body != "новая правка" || own.RejectReason != "причина" {
		t.Fatalf("свой черновик: %+v %v", own, err)
	}
	history, err := repo.History(ctx, who, c.ID)
	if err != nil || len(history) != 2 || history[0].Body != "первое" || history[1].Status != "отклонено" {
		t.Fatalf("история %+v %v", history, err)
	}
	if _, err := repo.History(ctx, models.CommentViewer{ID: 999}, c.ID); !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("чужая история: %v", err)
	}
	if _, err := repo.Save(ctx, models.CommentViewer{ID: 999}, c.ID, in, "ip", 0, 2); err == nil {
		t.Fatal("чужая правка прошла")
	}
}

func TestReaderCommentBanAndLimits(t *testing.T) {
	ctx, f, reader := marginSetup(t)
	repo := NewReaderCommentRepository(testPool(t))
	vol := newVolume(t, f.works, "Частота")
	who := models.CommentViewer{ID: reader.ID}
	staff := models.CommentViewer{ID: 1, Staff: true, Admin: true}
	in := models.MarginNoteInput{OwnerID: reader.ID, WorkID: vol.ID, Target: "work", Body: "текст"}
	var first int64
	for i := 0; i < 10; i++ {
		c, err := repo.Save(ctx, who, 0, in, "ip", 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		first = c.ID
	}
	if _, err := repo.Save(ctx, who, 0, in, "another-ip", 0, 0); !errors.Is(err, ErrCommentRate) {
		t.Fatalf("предел: %v", err)
	}
	if err := repo.Ban(ctx, staff, first); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Save(ctx, who, 0, in, "new-ip", 0, 0); !errors.Is(err, ErrCommentBanned) {
		t.Fatalf("старый токен: %v", err)
	}
	if _, err := repo.Save(ctx, who, first, in, "new-ip", 0, 1); !errors.Is(err, ErrCommentBanned) {
		t.Fatalf("правка после бана: %v", err)
	}
	queue, err := repo.List(ctx, staff, 0, false, false, true, 200, 0)
	if err != nil || len(queue) != 0 {
		t.Fatalf("очередь не очищена: %v %+v", err, queue)
	}
	if _, err := f.repo.Create(ctx, in); err != nil {
		t.Fatalf("запрет сломал личные заметки: %v", err)
	}
	if err := repo.Unban(ctx, staff, reader.ID); err != nil {
		t.Fatal(err)
	}
}

func TestReaderCommentClosedRangeAndDeletedOwner(t *testing.T) {
	ctx, f, reader := marginSetup(t)
	repo := NewReaderCommentRepository(testPool(t))
	pool := testPool(t)
	vol := newVolume(t, f.works, "Закрытое")
	pages := f.addPages(t, vol.ID, 3)
	ch := f.addChapter(t, vol.ID, nil, "Середина", 1, 2, 2)
	in := quoteInput(reader.ID, vol.ID, pages[0], "секрет")
	end := 3
	in.EndPageNumber = &end
	in.EndPageID = &pages[2].ID
	who := models.CommentViewer{ID: reader.ID}
	staff := models.CommentViewer{ID: 1, Staff: true, Admin: true}
	c, err := repo.Save(ctx, who, 0, in, "ip", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Review(ctx, staff, c.ID, 1, true, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.closed.SetChapterClosed(ctx, ch.ID, true, models.ClosureActor{Name: "тест"}, "скрыть"); err != nil {
		t.Fatal(err)
	}
	for _, viewer := range []models.CommentViewer{{}, who} {
		if list, err := repo.List(ctx, viewer, vol.ID, true, false, false, 200, 0); err != nil || len(list) != 0 {
			t.Fatalf("закрытый диапазон виден: %+v %v", list, err)
		}
		if _, err := repo.Get(ctx, viewer, c.ID); !errors.Is(err, ErrCommentNotFound) {
			t.Fatalf("закрытое Get: %v", err)
		}
		if _, err := repo.History(ctx, viewer, c.ID); !errors.Is(err, ErrCommentNotFound) {
			t.Fatalf("история закрытой середины цитаты: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, reader.ID); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, staff, c.ID)
	if err != nil || !strings.HasPrefix(got.AuthorNickname, "Читатель-") {
		t.Fatalf("снимок подписи: %+v %v", got, err)
	}
}

func TestReaderCommentConcurrentAndIPLimit(t *testing.T) {
	ctx, f, reader := marginSetup(t)
	repo := NewReaderCommentRepository(testPool(t))
	vol := newVolume(t, f.works, "Параллельные комментарии")
	in := models.MarginNoteInput{WorkID: vol.ID, Target: "work", Body: "мысль"}
	who := models.CommentViewer{ID: reader.ID}
	start := make(chan struct{})
	results := make(chan error, 12)
	for i := 0; i < 12; i++ {
		go func() { <-start; _, err := repo.Save(ctx, who, 0, in, "shared-ip", 0, 0); results <- err }()
	}
	close(start)
	accepted, limited := 0, 0
	for i := 0; i < 12; i++ {
		err := <-results
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrCommentRate) {
			limited++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 10 || limited != 2 {
		t.Fatalf("залп обошёл предел: принято %d, ограничено %d", accepted, limited)
	}
	for _, nick := range []string{"Второй", "Третий"} {
		u := createReader(t, f.users, nick, "register-"+nick)
		for i := 0; i < 10; i++ {
			if _, err := repo.Save(ctx, models.CommentViewer{ID: u.ID}, 0, in, "shared-ip", 0, 0); err != nil {
				t.Fatal(err)
			}
		}
	}
	fourth := createReader(t, f.users, "Четвёртый", "register-fourth")
	if _, err := repo.Save(ctx, models.CommentViewer{ID: fourth.ID}, 0, in, "shared-ip", 0, 0); !errors.Is(err, ErrCommentRate) {
		t.Fatalf("31-я с адреса прошла: %v", err)
	}
}

func TestReaderCommentStaffPublication(t *testing.T) {
	ctx, f, _ := marginSetup(t)
	repo := NewReaderCommentRepository(testPool(t))
	vol := newVolume(t, f.works, "Редакция")
	staff := models.CommentViewer{ID: 1, Staff: true, Admin: true}
	in := models.MarginNoteInput{WorkID: vol.ID, Target: "work", Body: "от редакции"}
	c, err := repo.Save(ctx, staff, 0, in, "ip", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	editor := &models.User{Email: "comments-editor@example.invalid", PasswordHash: "x", Role: models.RoleEditor}
	if err := f.users.Create(ctx, editor); err != nil {
		t.Fatal(err)
	}
	in.Body = "исправление редакции"
	if _, err := repo.Save(ctx, models.CommentViewer{ID: editor.ID, Staff: true}, c.ID, in, "ip", 0, 1); err != nil {
		t.Fatal(err)
	}
	public, err := repo.Get(ctx, models.CommentViewer{}, c.ID)
	if err != nil || public.Body != in.Body || public.AuthorNickname != "" || public.EditedAt == nil {
		t.Fatalf("редакционный комментарий: %+v %v", public, err)
	}
}

func TestReaderCommentNoteConversion(t *testing.T) {
	ctx, f, reader := marginSetup(t)
	repo := NewReaderCommentRepository(testPool(t))
	vol := newVolume(t, f.works, "Преобразование")
	who := models.CommentViewer{ID: reader.ID}
	staff := models.CommentViewer{ID: 1, Staff: true, Admin: true}
	in := models.MarginNoteInput{OwnerID: reader.ID, WorkID: vol.ID, Target: "work", Body: "из заметки"}
	note, err := f.repo.Create(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Save(ctx, staff, 0, in, "ip", note.ID, 0); !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("сотрудник забрал чужую заметку: %v", err)
	}
	c, err := repo.Save(ctx, who, 0, in, "ip", note.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if notes, err := f.repo.ListByWork(ctx, reader.ID, vol.ID, false); err != nil || len(notes) != 0 {
		t.Fatalf("заметка не преобразована: %v %+v", err, notes)
	}
	if err := repo.Review(ctx, staff, c.ID, 1, false, "не по теме"); err != nil {
		t.Fatal(err)
	}
	restored, err := repo.ToNote(ctx, who, c.ID)
	if err != nil || restored.Body != "из заметки" {
		t.Fatalf("возврат в заметки: %+v %v", restored, err)
	}
	if _, err := repo.Get(ctx, who, c.ID); !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("после возврата остался комментарий: %v", err)
	}
	if _, err := repo.Save(ctx, who, 0, in, "ip", restored.ID, 0); err != nil {
		t.Fatal(err)
	}
	queue, err := repo.List(ctx, staff, vol.ID, false, false, true, 50, 0)
	if err != nil || len(queue) != 1 || queue[0].Rejections != 1 {
		t.Fatalf("возврат в заметки обнулил отказы: %+v %v", queue, err)
	}
	if err := repo.Review(ctx, staff, queue[0].ID, 1, false, ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.Remove(ctx, who, queue[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Save(ctx, who, 0, in, "ip", 0, 0); err != nil {
		t.Fatal(err)
	}
	queue, err = repo.List(ctx, staff, vol.ID, false, false, true, 50, 0)
	if err != nil || len(queue) != 1 || queue[0].Rejections != 2 {
		t.Fatalf("удаление обнулило отказы: %+v %v", queue, err)
	}
}
