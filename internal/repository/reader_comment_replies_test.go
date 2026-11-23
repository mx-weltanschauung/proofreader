package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"proofreader/internal/models"
)

func TestReaderCommentReplies(t *testing.T) {
	ctx, f, reader := marginSetup(t)
	repo := NewReaderCommentRepository(testPool(t))
	work := newVolume(t, f.works, "Ветка")
	page := f.addPages(t, work.ID, 1)[0]
	chapter := f.addChapter(t, work.ID, nil, "Цитата", 1, 1, 1)
	alice := models.CommentViewer{ID: reader.ID}
	bobUser := createReader(t, f.users, "Собеседник", "register-bob")
	bob := models.CommentViewer{ID: bobUser.ID}
	staff := models.CommentViewer{ID: 1, Staff: true, Admin: true}

	save := func(body string) *models.ReaderComment {
		t.Helper()
		c, err := repo.Save(ctx, alice, 0, quoteInput(alice.ID, work.ID, page, body), "alice-ip", 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	reply := func(viewer models.CommentViewer, target int64, body string) *models.ReaderComment {
		t.Helper()
		c, err := repo.Reply(ctx, viewer, target, body, "reply-ip")
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	approve := func(c *models.ReaderComment) {
		t.Helper()
		if err := repo.Review(ctx, staff, c.ID, c.Revision, true, ""); err != nil {
			t.Fatal(err)
		}
	}
	count := func(viewer models.CommentViewer, want int) {
		t.Helper()
		got, err := repo.UnreadReplies(ctx, viewer)
		if err != nil || got != want {
			t.Fatalf("уведомления: %d, ожидалось %d: %v", got, want, err)
		}
	}
	thread := func(viewer models.CommentViewer, id int64, want int) *models.CommentThread {
		t.Helper()
		got, err := repo.Thread(ctx, viewer, id)
		if err != nil || len(got.Replies) != want {
			t.Fatalf("ветка: %+v, ожидалось %d ответов: %v", got, want, err)
		}
		return got
	}
	remove := func(viewer models.CommentViewer, id int64) {
		t.Helper()
		if err := repo.Remove(ctx, viewer, id); err != nil {
			t.Fatal(err)
		}
	}

	root := save("исходный комментарий")
	if _, err := repo.Reply(ctx, bob, root.ID, "рано", "bob-ip"); !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("ответ на неодобренный комментарий: %v", err)
	}
	approve(root)
	first := reply(bob, root.ID, "первый ответ")
	if first.ThreadID != root.ID || first.QuoteText != root.QuoteText || first.QuoteKey != root.QuoteKey || first.WorkID != work.ID {
		t.Fatalf("ответ потерял привязку: %+v", first)
	}
	if _, err := repo.Reply(ctx, alice, first.ID, "рано", "alice-ip"); !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("ответ на неодобренный ответ: %v", err)
	}
	thread(alice, root.ID, 0)
	thread(bob, root.ID, 1)
	count(alice, 0)

	approve(first)
	firstPage := thread(alice, root.ID, 1)
	if !firstPage.Replies[0].Unread || firstPage.Replies[0].Body != first.Body {
		t.Fatalf("первый ответ: %+v", firstPage.Replies[0])
	}
	list, err := repo.List(ctx, models.CommentViewer{}, work.ID, true, false, false, 10, 0)
	if err != nil || len(list) != 1 || list[0].ReplyCount != 1 {
		t.Fatalf("на полях должен быть один корень: %+v (%v)", list, err)
	}
	second := reply(bob, root.ID, "поздний ответ")
	approve(second)
	for i := 0; i < 2; i++ {
		if err := repo.MarkRepliesRead(ctx, bob, root.ID, []int64{first.ID, second.ID}); err != nil {
			t.Fatal(err)
		}
		count(alice, 2-i)
		if err := repo.MarkRepliesRead(ctx, alice, root.ID, []int64{first.ID}); err != nil {
			t.Fatal(err)
		}
	}
	count(alice, 1)
	count(bob, 0)

	nested := reply(alice, first.ID, "ответ участнику")
	approve(nested)
	if nested.ThreadID != root.ID || nested.ReplyToNickname == nil || *nested.ReplyToNickname != *bobUser.Nickname {
		t.Fatalf("адресат ответа: %+v", nested)
	}
	count(bob, 1)
	count(alice, 1)
	editorReply := reply(staff, nested.ID, "ответ редакции")
	if !editorReply.Published {
		t.Fatalf("ответ сотрудника задержан: %+v", editorReply)
	}
	count(alice, 2)
	selfReply := reply(alice, root.ID, "добавление автора")
	approve(selfReply)
	count(alice, 2)

	edited, err := repo.Save(ctx, bob, first.ID, models.MarginNoteInput{Body: "правка"}, "bob-ip", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := thread(alice, root.ID, 5).Replies[0]; got.Body != first.Body || got.Unread {
		t.Fatalf("черновик правки или повторное уведомление: %+v", got)
	}
	approve(edited)
	if got := thread(alice, root.ID, 5).Replies[0]; got.Body != "правка" || got.Unread {
		t.Fatalf("одобренная правка: %+v", got)
	}

	actor := models.ClosureActor{Name: "тест"}
	if _, err := f.closed.SetChapterClosed(ctx, chapter.ID, true, actor, "скрыть"); err != nil {
		t.Fatal(err)
	}
	count(alice, 0)
	count(bob, 0)
	if list, err := repo.IncomingReplies(ctx, alice, 50, 0); err != nil || len(list) != 0 {
		t.Fatalf("закрытые уведомления: %+v, %v", list, err)
	}
	if _, err := repo.Thread(ctx, alice, root.ID); !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("прямой адрес закрытого: %v", err)
	}
	if err := repo.MarkRepliesRead(ctx, alice, root.ID, []int64{second.ID}); !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("чтение закрытого: %v", err)
	}
	if _, err := repo.Reply(ctx, bob, root.ID, "на закрытое", "bob-ip"); !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("ответ в закрытую главу: %v", err)
	}
	thread(staff, root.ID, 5)
	if _, err := f.closed.SetChapterClosed(ctx, chapter.ID, false, actor, "открыть"); err != nil {
		t.Fatal(err)
	}
	count(alice, 2)
	if list, err := repo.IncomingReplies(ctx, alice, 50, 0); err != nil || len(list) != 3 || !list[0].Unread {
		t.Fatalf("уведомления после открытия: %+v, %v", list, err)
	}

	remove(alice, root.ID)
	got := thread(bob, root.ID, 5).Root
	if got.Removed != "author" || got.Body != "" || got.PublishedBody != "" || got.CanEdit {
		t.Fatalf("корень после удаления: %+v", got)
	}
	if _, err := repo.Reply(ctx, bob, root.ID, "на удалённое", "bob-ip"); !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("ответ на удалённый корень: %v", err)
	}
	if history, err := repo.History(ctx, alice, root.ID); err != nil || len(history) != 1 || history[0].Body != root.Body {
		t.Fatalf("история удалённого: %+v, %v", history, err)
	}
	for _, item := range []struct {
		viewer models.CommentViewer
		id     int64
	}{
		{bob, first.ID}, {bob, second.ID}, {alice, nested.ID}, {staff, editorReply.ID}, {alice, selfReply.ID},
	} {
		remove(item.viewer, item.id)
	}
	if _, err := repo.Thread(ctx, alice, root.ID); !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("ветка после удаления последнего ответа: %v", err)
	}
	count(alice, 0)

	moderated := save("снятый комментарий")
	approve(moderated)
	approve(reply(bob, moderated.ID, "сохранённый ответ"))
	if _, err := repo.Save(ctx, alice, moderated.ID, models.MarginNoteInput{Body: "ожидающая правка корня"}, "alice-ip", 0, 1); err != nil {
		t.Fatal(err)
	}
	remove(staff, moderated.ID)
	if got := thread(alice, moderated.ID, 1).Root; got.Removed != "moderator" || got.Body != "" {
		t.Fatalf("корень после модерации: %+v", got)
	}

	queue, err := repo.List(ctx, staff, work.ID, false, false, true, 50, 0)
	if err != nil || len(queue) != 0 {
		t.Fatalf("снятый комментарий остался в очереди: %+v %v", queue, err)
	}

	for _, pending := range []bool{false, true} {
		root := save("короткая ветка")
		approve(root)
		var child *models.ReaderComment
		if pending {
			child = reply(bob, root.ID, "ждёт решения")
		}
		remove(alice, root.ID)
		if _, err := repo.Get(ctx, staff, root.ID); !errors.Is(err, ErrCommentNotFound) {
			t.Fatalf("корень без одобренных ответов не удалён: %v", err)
		}
		if child != nil {
			if _, err := repo.Get(ctx, staff, child.ID); !errors.Is(err, ErrCommentNotFound) {
				t.Fatalf("неодобренный ответ остался: %v", err)
			}
			if err := repo.Review(ctx, staff, child.ID, 1, true, ""); !errors.Is(err, ErrCommentConflict) {
				t.Fatalf("отозванную заявку можно одобрить: %v", err)
			}
		}
	}
}

func TestReaderCommentReplySharesLimits(t *testing.T) {
	ctx, f, reader := marginSetup(t)
	repo := NewReaderCommentRepository(testPool(t))
	work := newVolume(t, f.works, "Частота ответов")
	in := models.MarginNoteInput{WorkID: work.ID, Target: "work", Body: "текст"}
	viewer := models.CommentViewer{ID: reader.ID}
	staff := models.CommentViewer{ID: 1, Staff: true, Admin: true}
	root, err := repo.Save(ctx, staff, 0, in, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 9; i++ {
		if _, err := repo.Save(ctx, viewer, 0, in, "shared-ip", 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	reply, err := repo.Reply(ctx, viewer, root.ID, "десятая отправка", "shared-ip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Reply(ctx, viewer, root.ID, "сверх лимита", "different-ip"); !errors.Is(err, ErrCommentRate) {
		t.Fatalf("ответ вне общего ограничения: %v", err)
	}
	if err := repo.Ban(ctx, staff, reply.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Reply(ctx, viewer, root.ID, "после запрета", "shared-ip"); !errors.Is(err, ErrCommentBanned) {
		t.Fatalf("ответ обошёл запрет: %v", err)
	}
	if got, err := repo.Get(ctx, viewer, reply.ID); err != nil || got.Status != "отклонено" {
		t.Fatalf("ожидающий ответ не отклонён: %+v, %v", got, err)
	}
}

func TestReaderCommentRootRemovalRacesReplyApproval(t *testing.T) {
	ctx, f, reader := marginSetup(t)
	repo := NewReaderCommentRepository(testPool(t))
	work := newVolume(t, f.works, "Удаление ветки")
	author := models.CommentViewer{ID: 1, Staff: true, Admin: true}
	respondent := models.CommentViewer{ID: reader.ID}
	in := models.MarginNoteInput{WorkID: work.ID, Target: "work", Body: "корень"}

	for i := 0; i < 8; i++ {
		root, err := repo.Save(ctx, author, 0, in, "", 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		reply, err := repo.Reply(ctx, respondent, root.ID, "ответ", "reply-ip")
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		approval := make(chan error, 1)
		go func() {
			<-start
			approval <- repo.Review(ctx, author, reply.ID, 1, true, "")
		}()
		close(start)
		removeErr := repo.Remove(ctx, author, root.ID)
		approveErr := <-approval
		if removeErr != nil {
			t.Fatal(removeErr)
		}
		thread, threadErr := repo.Thread(ctx, respondent, root.ID)
		switch {
		case approveErr == nil:
			if threadErr != nil || thread.Root.Removed != "author" || len(thread.Replies) != 1 {
				t.Fatalf("одобренный ответ утрачен: %+v %v", thread, threadErr)
			}
		case errors.Is(approveErr, ErrCommentConflict):
			if !errors.Is(threadErr, ErrCommentNotFound) {
				t.Fatalf("ветка без одобренных ответов осталась: %+v %v", thread, threadErr)
			}
		default:
			t.Fatal(approveErr)
		}
	}
}

func TestReaderCommentReplyDuringRecipientDeletion(t *testing.T) {
	ctx, f, reader := marginSetup(t)
	pool := testPool(t)
	repo := NewReaderCommentRepository(pool)
	work := newVolume(t, f.works, "Удаление учётной записи")
	owner := models.CommentViewer{ID: reader.ID}
	staff := models.CommentViewer{ID: 1, Staff: true, Admin: true}
	in := models.MarginNoteInput{WorkID: work.ID, Target: "work", Body: "комментарий"}
	root, err := repo.Save(ctx, owner, 0, in, "owner-ip", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Review(ctx, staff, root.ID, 1, true, ""); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var pid int32
	if err := tx.QueryRow(ctx, `SELECT pg_backend_pid() FROM users WHERE id=$1 FOR UPDATE`, owner.ID).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := repo.Reply(ctx, staff, root.ID, "ответ", "")
		result <- err
	}()
	// Дожидаемся, пока ответ попробует обратиться к занятой строке пользователя.
	for {
		var waiting bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(
			SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid))
		)`, pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id=$1`, owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	got, err := repo.Thread(ctx, models.CommentViewer{}, root.ID)
	if err != nil || len(got.Replies) != 1 || got.Replies[0].RecipientID != nil || got.Root.AuthorNickname == "" {
		t.Fatalf("ветка после удаления адресата: %+v %v", got, err)
	}
}
