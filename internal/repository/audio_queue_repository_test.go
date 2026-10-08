package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"proofreader/internal/models"
)

func newAudioFixture(t *testing.T) (*AudioRepository, int64, int64) {
	t.Helper()
	pool := testPool(t)
	workID := seedWorkWithPages(t, pool, "том озвучки", "раз", "два", "три")
	chapterID := seedChapter(t, pool, workID, "Глава первая", 1, 2)
	return NewAudioRepository(pool), workID, chapterID
}

// claimOne забирает одну заявку и отдаёт её отметку забора — токен итога.
func claimOne(t *testing.T, r *AudioRepository, id int64, reclaim bool) time.Time {
	t.Helper()
	got, err := r.Claim(context.Background(), []int64{id}, reclaim)
	if err != nil || len(got) != 1 || got[0].ClaimedAt == nil {
		t.Fatalf("забор %d: %+v %v", id, got, err)
	}
	return *got[0].ClaimedAt
}

func TestAudioEnqueueReturnsExistingOpenItem(t *testing.T) {
	ctx := context.Background()
	r, w, c := newAudioFixture(t)
	by := int64(1)

	first, created, err := r.Enqueue(ctx, w, &c, &by)
	if err != nil || !created || first.Status != models.AudioQueued {
		t.Fatalf("первая постановка: %+v %v %v", first, created, err)
	}
	again, created, err := r.Enqueue(ctx, w, &c, &by)
	if err != nil || created || again.ID != first.ID {
		t.Fatalf("дубль не схлопнулся: %+v %v %v", again, created, err)
	}
	// Весь том — отдельная заявка; и она не дублируется (NULL через coalesce).
	vol1, _, _ := r.Enqueue(ctx, w, nil, &by)
	vol2, created, err := r.Enqueue(ctx, w, nil, &by)
	if err != nil || created || vol1.ID != vol2.ID || vol1.ID == first.ID {
		t.Fatalf("том: %d %d %v %v", vol1.ID, vol2.ID, created, err)
	}
	if first.WorkTitle != "том озвучки" || first.ChapterTitle != "Глава первая" || first.RequestedBy == "" {
		t.Errorf("поля заявки: %+v", first)
	}
}

func TestAudioEnqueueMissingWorkIsNotFound(t *testing.T) {
	r, _, _ := newAudioFixture(t)
	if _, _, err := r.Enqueue(context.Background(), 999999, nil, nil); !errors.Is(err, ErrAudioNotFound) {
		t.Errorf("нет тома: %v", err)
	}
}

func TestAudioClaimNeverHandsOutTwice(t *testing.T) {
	ctx := context.Background()
	r, w, _ := newAudioFixture(t)
	pool := testPool(t)
	for i := 0; i < 20; i++ {
		ch := seedChapter(t, pool, w, "г", 1, 1)
		if _, _, err := r.Enqueue(ctx, w, &ch, nil); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	seen := map[int64]int{}
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			items, err := r.Claim(ctx, nil, false)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			for _, it := range items {
				seen[it.ID]++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(seen) != 20 {
		t.Errorf("забрано %d заявок из 20", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("заявка %d выдана %d раз", id, n)
		}
	}
}

func TestAudioClaimByIDsAndReclaim(t *testing.T) {
	ctx := context.Background()
	r, w, c := newAudioFixture(t)
	a, _, _ := r.Enqueue(ctx, w, &c, nil)
	b, _, _ := r.Enqueue(ctx, w, nil, nil)

	got, err := r.Claim(ctx, []int64{b.ID}, false)
	if err != nil || len(got) != 1 || got[0].ID != b.ID || got[0].Status != models.AudioRunning {
		t.Fatalf("по номеру: %+v %v", got, err)
	}
	// Свежая «синтезируется» без reclaim не выдаётся; с reclaim — выдаётся.
	if again, _ := r.Claim(ctx, []int64{b.ID}, false); len(again) != 0 {
		t.Errorf("свежая забранная выдана повторно без reclaim")
	}
	if again, _ := r.Claim(ctx, []int64{b.ID}, true); len(again) != 1 {
		t.Errorf("reclaim не взял забранную")
	}
	// Зависшая дольше 6 ч выдаётся и без reclaim.
	if _, err := testPool(t).Exec(ctx,
		`UPDATE audio_queue SET claimed_at = now() - interval '7 hours' WHERE id = $1`, b.ID); err != nil {
		t.Fatal(err)
	}
	if again, _ := r.Claim(ctx, []int64{b.ID}, false); len(again) != 1 {
		t.Errorf("зависшая 7 ч не выдана")
	}
	// a не называли — не тронута.
	if it, _ := r.QueueItem(ctx, a.ID); it.Status != models.AudioQueued {
		t.Errorf("неназванная заявка сменила статус: %s", it.Status)
	}
}

func TestAudioFinishTransitions(t *testing.T) {
	ctx := context.Background()
	r, w, c := newAudioFixture(t)
	a, _, _ := r.Enqueue(ctx, w, &c, nil)

	if err := r.FinishQueueItem(ctx, a.ID, time.Now(), models.AudioDone, "", nil); !errors.Is(err, ErrAudioConflict) {
		t.Errorf("итог незабранной: %v, ждали ErrAudioConflict", err)
	}
	at := claimOne(t, r, a.ID, false)
	counts := map[string]int{"не_вычитана": 3}
	if err := r.FinishQueueItem(ctx, a.ID, at, models.AudioFailed, "упало предложение", counts); err != nil {
		t.Fatal(err)
	}
	it, _ := r.QueueItem(ctx, a.ID)
	if it.Status != models.AudioFailed || it.Error != "упало предложение" ||
		it.StatusCounts["не_вычитана"] != 3 || it.FinishedAt == nil {
		t.Errorf("после ошибки: %+v", it)
	}
	// Повтор возвращает в очередь; пока она открыта, вторая постановка — та же.
	back, err := r.RetryQueueItem(ctx, a.ID)
	if err != nil || back.Status != models.AudioQueued || back.Error != "" || back.FinishedAt != nil {
		t.Fatalf("повтор: %+v %v", back, err)
	}
	// Возврат в очередь из «синтезируется» (прерванный worker) снимает claimed_at.
	at = claimOne(t, r, a.ID, false)
	if err := r.FinishQueueItem(ctx, a.ID, at, models.AudioQueued, "", nil); err != nil {
		t.Fatal(err)
	}
	if it, _ := r.QueueItem(ctx, a.ID); it.Status != models.AudioQueued || it.ClaimedAt != nil {
		t.Errorf("возврат: %+v", it)
	}
}

func TestAudioRetryRefusesWhenSameChapterAlreadyOpen(t *testing.T) {
	ctx := context.Background()
	r, w, c := newAudioFixture(t)
	a, _, _ := r.Enqueue(ctx, w, &c, nil)
	if err := r.FinishQueueItem(ctx, a.ID, claimOne(t, r, a.ID, false), models.AudioFailed, "x", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Enqueue(ctx, w, &c, nil); err != nil { // новая открытая на ту же главу
		t.Fatal(err)
	}
	if _, err := r.RetryQueueItem(ctx, a.ID); !errors.Is(err, ErrAudioConflict) {
		t.Errorf("повтор при открытой: %v", err)
	}
}

func TestAudioCancelOnlyWaitingOrFailed(t *testing.T) {
	ctx := context.Background()
	r, w, c := newAudioFixture(t)
	a, _, _ := r.Enqueue(ctx, w, &c, nil)
	at := claimOne(t, r, a.ID, false)
	if err := r.CancelQueueItem(ctx, a.ID); !errors.Is(err, ErrAudioConflict) {
		t.Errorf("снята синтезируемая: %v", err)
	}
	if err := r.FinishQueueItem(ctx, a.ID, at, models.AudioFailed, "x", nil); err != nil {
		t.Fatal(err)
	}
	if err := r.CancelQueueItem(ctx, a.ID); err != nil {
		t.Errorf("упавшая не снялась: %v", err)
	}
	if err := r.CancelQueueItem(ctx, a.ID); !errors.Is(err, ErrAudioNotFound) {
		t.Errorf("повторное снятие: %v", err)
	}
}

// Тикет 01: после --reclaim прежний worker, если ещё жив, не закрывает итогом
// заявку, которую забрал другой. Токен — отметка забора.
func TestAudioFinishRefusesStaleClaim(t *testing.T) {
	ctx := context.Background()
	r, w, c := newAudioFixture(t)
	a, _, _ := r.Enqueue(ctx, w, &c, nil)
	first := claimOne(t, r, a.ID, false)
	second := claimOne(t, r, a.ID, true)
	if first.Equal(second) {
		t.Fatalf("повторный забор не сменил отметку: %v", first)
	}
	if err := r.FinishQueueItem(ctx, a.ID, first, models.AudioDone, "", nil); !errors.Is(err, ErrAudioConflict) {
		t.Errorf("итог прежнего забора: %v, ждали ErrAudioConflict", err)
	}
	if it, _ := r.QueueItem(ctx, a.ID); it.Status != models.AudioRunning {
		t.Errorf("итог прежнего забора сменил статус: %s", it.Status)
	}
	if err := r.FinishQueueItem(ctx, a.ID, second, models.AudioDone, "", nil); err != nil {
		t.Errorf("итог нынешнего забора: %v", err)
	}
}

// Тикет 01: покрытие, которого не было, — «готово» ставит finished_at.
func TestAudioFinishDoneSetsFinishedAt(t *testing.T) {
	ctx := context.Background()
	r, w, c := newAudioFixture(t)
	a, _, _ := r.Enqueue(ctx, w, &c, nil)
	if err := r.FinishQueueItem(ctx, a.ID, claimOne(t, r, a.ID, false), models.AudioDone, "",
		map[string]int{"не_вычитана": 1}); err != nil {
		t.Fatal(err)
	}
	it, _ := r.QueueItem(ctx, a.ID)
	if it.Status != models.AudioDone || it.FinishedAt == nil || it.ClaimedAt == nil || it.StatusCounts["не_вычитана"] != 1 {
		t.Errorf("после «готово»: %+v", it)
	}
}

func TestAudioQueueItemMissingIsNotFound(t *testing.T) {
	r, _, _ := newAudioFixture(t)
	if _, err := r.QueueItem(context.Background(), 999999999); !errors.Is(err, ErrAudioNotFound) {
		t.Errorf("нет заявки: %v", err)
	}
}

// ListOpenQueue — ждущие, синтезируемые и упавшие; готовые — нет.
func TestAudioListOpenQueueSkipsDone(t *testing.T) {
	ctx := context.Background()
	r, w, c := newAudioFixture(t)
	pool := testPool(t)
	c2 := seedChapter(t, pool, w, "Глава вторая", 3, 3)
	c3 := seedChapter(t, pool, w, "Глава третья", 3, 3)
	queued, _, _ := r.Enqueue(ctx, w, &c, nil)
	running, _, _ := r.Enqueue(ctx, w, &c2, nil)
	failed, _, _ := r.Enqueue(ctx, w, &c3, nil)
	done, _, _ := r.Enqueue(ctx, w, nil, nil)
	claimOne(t, r, running.ID, false)
	if err := r.FinishQueueItem(ctx, failed.ID, claimOne(t, r, failed.ID, false), models.AudioFailed, "x", nil); err != nil {
		t.Fatal(err)
	}
	if err := r.FinishQueueItem(ctx, done.ID, claimOne(t, r, done.ID, false), models.AudioDone, "", nil); err != nil {
		t.Fatal(err)
	}

	open, err := r.ListOpenQueue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int64]string{}
	for _, it := range open {
		if it.WorkID == w {
			seen[it.ID] = it.Status
		}
	}
	want := map[int64]string{queued.ID: models.AudioQueued, running.ID: models.AudioRunning, failed.ID: models.AudioFailed}
	if len(seen) != len(want) {
		t.Errorf("открытые тома: %v, ждали %v", seen, want)
	}
	for id, st := range want {
		if seen[id] != st {
			t.Errorf("заявка %d: %q, ждали %q", id, seen[id], st)
		}
	}
}

// StaleSummary считает устаревшие дорожки по тому; свежие не в счёт.
func TestAudioStaleSummaryCountsStaleTracksPerWork(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	r := NewAudioRepository(pool)
	w := seedWorkWithPages(t, pool, "том со старым звуком", "раз", "два", "три")
	fresh := seedWorkWithPages(t, pool, "том со свежим звуком", "раз")
	// Отпечаток не тот, что у текста полос, — регистрация сама пометит stale.
	a := trackFor(w, []string{"не тот текст"}, 1, 1)
	b := trackFor(w, []string{"и этот"}, 2, 2)
	ok := trackFor(w, []string{"три"}, 3, 3)
	if _, _, err := r.RegisterTracks(ctx, w, []models.AudioTrack{a, b, ok}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.RegisterTracks(ctx, fresh, []models.AudioTrack{trackFor(fresh, []string{"раз"}, 1, 1)}); err != nil {
		t.Fatal(err)
	}
	sum, err := r.StaleSummary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got []models.AudioStaleWork
	for _, s := range sum {
		if s.WorkID == w || s.WorkID == fresh {
			got = append(got, s)
		}
	}
	if len(got) != 1 || got[0].WorkID != w || got[0].Stale != 2 || got[0].WorkTitle != "том со старым звуком" {
		t.Errorf("сводка: %+v", got)
	}
}
