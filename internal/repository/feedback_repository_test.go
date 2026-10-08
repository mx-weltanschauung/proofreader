package repository

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"proofreader/internal/models"
)

// Пустую базу готовит testPool — см. main_test.go.
func newFeedbackTestRepo(t *testing.T) (*FeedbackRepository, context.Context) {
	t.Helper()
	return NewFeedbackRepository(testPool(t)), context.Background()
}

func TestFeedbackListNewestFirst(t *testing.T) {
	repo, ctx := newFeedbackTestRepo(t)

	for _, msg := range []string{"первое", "второе", "третье"} {
		if err := repo.Create(ctx, &models.Feedback{Message: msg}); err != nil {
			t.Fatalf("Create(%q): %v", msg, err)
		}
	}

	all, err := repo.List(ctx, nil, 500)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("ожидалось 3 письма, получено %d", len(all))
	}
	if all[0].Message != "третье" {
		t.Errorf("новые должны быть сверху, первым пришло %q", all[0].Message)
	}
}

func TestFeedbackHandledFilter(t *testing.T) {
	repo, ctx := newFeedbackTestRepo(t)

	discarded := &models.Feedback{Message: "разобранное"}
	if err := repo.Create(ctx, discarded); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Create(ctx, &models.Feedback{Message: "новое"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.SetHandled(ctx, discarded.ID, true); err != nil {
		t.Fatalf("SetHandled: %v", err)
	}

	no := false
	unread, err := repo.List(ctx, &no, 500)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(unread) != 1 || unread[0].Message != "новое" {
		t.Fatalf("фильтр handled=false вернул %+v", unread)
	}

	count, err := repo.CountUnread(ctx)
	if err != nil {
		t.Fatalf("CountUnread: %v", err)
	}
	if count != 1 {
		t.Errorf("CountUnread = %d, ожидалась 1", count)
	}

	// Пометка снимается обратно, и письмо снова считается новым.
	if err := repo.SetHandled(ctx, discarded.ID, false); err != nil {
		t.Fatalf("SetHandled(false): %v", err)
	}
	count, err = repo.CountUnread(ctx)
	if err != nil {
		t.Fatalf("CountUnread: %v", err)
	}
	if count != 2 {
		t.Errorf("после снятия пометки CountUnread = %d, ожидалось 2", count)
	}
}

// TestFeedbackCreateWithinLimitWindow проверяет то же, что раньше проверял
// TestFeedbackCountRecentByIP (чужая отметка не считается, окно — по времени,
// а не по всей истории отметки), но через новый атомарный метод: счёт и
// запись теперь одно действие, отдельного метода счёта больше нет.
func TestFeedbackCreateWithinLimitWindow(t *testing.T) {
	repo, ctx := newFeedbackTestRepo(t)

	for i := 0; i < 3; i++ {
		ok, err := repo.CreateWithinLimit(ctx, &models.Feedback{Message: "письмо", IPHash: "aaaa"},
			10, time.Now().Add(-time.Hour))
		if err != nil {
			t.Fatalf("CreateWithinLimit: %v", err)
		}
		if !ok {
			t.Fatalf("письмо %d отбито пределом, хотя предел 10", i+1)
		}
	}
	if ok, err := repo.CreateWithinLimit(ctx, &models.Feedback{Message: "чужое", IPHash: "bbbb"},
		10, time.Now().Add(-time.Hour)); err != nil || !ok {
		t.Fatalf("CreateWithinLimit(чужое): ok=%v err=%v", ok, err)
	}

	// Предел 3 — свои три письма уже его исчерпали, чужая отметка (bbbb) в
	// счёт не идёт.
	ok, err := repo.CreateWithinLimit(ctx, &models.Feedback{Message: "четвёртое", IPHash: "aaaa"},
		3, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("CreateWithinLimit: %v", err)
	}
	if ok {
		t.Error("предел (3) исчерпан своими же письмами, письмо не должно приниматься")
	}

	// Окно в будущем не захватывает ничего: граница именно по времени, а не
	// «все письма этой отметки» — при том же пределе 3 письмо снова проходит.
	ok, err = repo.CreateWithinLimit(ctx, &models.Feedback{Message: "пятое", IPHash: "aaaa"},
		3, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateWithinLimit: %v", err)
	}
	if !ok {
		t.Error("окно в будущем должно давать 0 совпадений и пропускать письмо")
	}
}

// TestFeedbackCreateWithinLimitRace — находка проверки: раньше счёт и запись
// шли двумя раздельными действиями (CountRecentByIP, затем Create), и бот,
// шлющий параллельно, проскакивал мимо предела целиком — двадцать
// одновременных писем при пределе пять проходили все двадцать. Консультативная
// блокировка в CreateWithinLimit обязана впустить РОВНО пять.
func TestFeedbackCreateWithinLimitRace(t *testing.T) {
	repo, ctx := newFeedbackTestRepo(t)

	const (
		goroutines = 20
		limit      = 5
	)

	var wg sync.WaitGroup
	var accepted int32
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := repo.CreateWithinLimit(ctx, &models.Feedback{Message: "письмо", IPHash: "race"},
				limit, time.Now().Add(-time.Hour))
			if err != nil {
				t.Errorf("CreateWithinLimit: %v", err)
				return
			}
			if ok {
				atomic.AddInt32(&accepted, 1)
			}
		}()
	}
	wg.Wait()

	if accepted != limit {
		t.Fatalf("из %d одновременных писем принято %d, ожидалось ровно %d", goroutines, accepted, limit)
	}

	var count int
	if err := repo.pool.QueryRow(ctx, `SELECT count(*) FROM feedback WHERE ip_hash = $1`, "race").Scan(&count); err != nil {
		t.Fatalf("count(*): %v", err)
	}
	if count != limit {
		t.Fatalf("в таблице %d писем с отметкой race, ожидалось %d", count, limit)
	}
}

func TestFeedbackDeleteMissing(t *testing.T) {
	repo, ctx := newFeedbackTestRepo(t)

	if err := repo.Delete(ctx, 4242); err == nil {
		t.Error("удаление несуществующего письма обязано вернуть ошибку")
	}
	if err := repo.SetHandled(ctx, 4242, true); err == nil {
		t.Error("пометка несуществующего письма обязана вернуть ошибку")
	}
}
