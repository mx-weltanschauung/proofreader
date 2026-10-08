package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"proofreader/internal/audio"
	"proofreader/internal/models"
)

const recipeX = "1111111111111111111111111111111111111111111111111111111111111111"

func trackFor(workID int64, texts []string, start, end int) models.AudioTrack {
	pages := audio.PagesSHA256(texts)
	title := fmt.Sprintf("дорожка %d-%d", start, end)
	return models.AudioTrack{
		Title: title, StartPage: start, EndPage: end,
		S3Key: audio.TrackKey(workID, recipeX, pages, start, end, title), DurationMS: 1000, Bytes: 10,
		MD5: "00", RecipeSHA256: recipeX, PagesSHA256: pages,
	}
}

// Track отдаёт дорожку с заголовком — из него ссылка «скачать» составляет
// имя файла.
func TestTrackReturnsKeyAndTitle(t *testing.T) {
	ctx := context.Background()
	r, w, _ := newAudioFixture(t)
	ins, _, err := r.RegisterTracks(ctx, w, []models.AudioTrack{trackFor(w, []string{"раз", "два"}, 1, 2)})
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Track(ctx, ins[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.S3Key != ins[0].S3Key || got.Title != "дорожка 1-2" {
		t.Errorf("Track = %+v", got)
	}
	if _, err := r.Track(ctx, ins[0].ID+1000); !errors.Is(err, ErrAudioNotFound) {
		t.Errorf("отсутствующая: %v, ждали ErrAudioNotFound", err)
	}
}

func TestRegisterTracksComputesStaleFromCurrentText(t *testing.T) {
	ctx := context.Background()
	r, w, _ := newAudioFixture(t) // полосы 1..3: «раз», «два», «три»
	fresh := trackFor(w, []string{"раз", "два"}, 1, 2)
	old := trackFor(w, []string{"другой текст"}, 3, 3)
	old.PagesSHA256 = audio.PagesSHA256([]string{"другой текст"})

	ins, freed, err := r.RegisterTracks(ctx, w, []models.AudioTrack{fresh, old})
	if err != nil {
		t.Fatal(err)
	}
	if len(freed) != 0 || len(ins) != 2 || ins[0].ID == 0 {
		t.Fatalf("вставка: %+v освобождено %+v", ins, freed)
	}
	if ins[0].Stale || !ins[1].Stale {
		t.Errorf("stale: свежая %v, с чужим текстом %v", ins[0].Stale, ins[1].Stale)
	}
}

func TestRegisterTracksReplacesOverlappingAndKeepsNewKey(t *testing.T) {
	ctx := context.Background()
	r, w, _ := newAudioFixture(t)
	wide := trackFor(w, []string{"раз", "два", "три"}, 1, 3)
	if _, _, err := r.RegisterTracks(ctx, w, []models.AudioTrack{wide}); err != nil {
		t.Fatal(err)
	}
	// Та же раскладка, тот же рецепт, тот же текст: ключ совпадает и
	// в освободившиеся попасть не должен (иначе worker удалил бы залитое).
	_, freed, err := r.RegisterTracks(ctx, w, []models.AudioTrack{wide})
	if err != nil || len(freed) != 0 {
		t.Fatalf("повтор того же ключа: freed=%+v err=%v", freed, err)
	}
	narrow := trackFor(w, []string{"два"}, 2, 2)
	_, freed, err = r.RegisterTracks(ctx, w, []models.AudioTrack{narrow})
	if err != nil || len(freed) != 1 || freed[0].S3Key != wide.S3Key || freed[0].StartPage != 1 {
		t.Fatalf("пересекающаяся не снята: %+v %v", freed, err)
	}
	list, _ := r.ListTracks(ctx, w)
	if len(list) != 1 || list[0].StartPage != 2 {
		t.Errorf("после замены: %+v", list)
	}
}

func TestKeyReferenced(t *testing.T) {
	ctx := context.Background()
	r, w, _ := newAudioFixture(t)
	tr := trackFor(w, []string{"раз"}, 1, 1)
	r.RegisterTracks(ctx, w, []models.AudioTrack{tr})
	if ok, err := r.KeyReferenced(ctx, tr.S3Key); err != nil || !ok {
		t.Errorf("ключ дорожки не найден: %v %v", ok, err)
	}
	if ok, _ := r.KeyReferenced(ctx, "works/1/нет.opus"); ok {
		t.Error("чужой ключ найден")
	}
}

// backendPID — pid соединения транзакции: по нему проба ждёт именно тех, кого
// держит эта транзакция, а не любого ожидающего в общей тестовой базе
// (go test ./... гоняет пакеты параллельно, и чужой ожидающий прошёл бы
// пробу раньше времени).
func backendPID(t *testing.T, ctx context.Context, tx pgx.Tx) int {
	t.Helper()
	var pid int
	if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	return pid
}

// waitBlockedBy ждёт соединение, которое стоит на замке соединения blocker, и
// отдаёт его pid; 0 — если раньше закрылся done либо вышел срок.
func waitBlockedBy(t *testing.T, ctx context.Context, blocker int, done <-chan struct{}) int {
	t.Helper()
	pool := testPool(t)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return 0
		default:
		}
		var waiter int
		err := pool.QueryRow(ctx, `SELECT pid FROM pg_stat_activity
			WHERE $1 = ANY(pg_blocking_pids(pid)) LIMIT 1`, blocker).Scan(&waiter)
		if err == nil {
			return waiter
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return 0
}

// Тикет 01: две регистрации пересекающихся диапазонов разом. DELETE каждой не
// видит незакоммиченной вставки другой — без сериализации по тому остаются
// обе дорожки. Детерминированно: сторонняя транзакция держит незакоммиченной
// строку с ключом дорожки A, и регистрация A встаёт на своей вставке —
// ПОСЛЕ своего DELETE; регистрация B приходит в этот момент.
func TestRegisterTracksConcurrentOverlapLeavesNoOverlap(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	r := NewAudioRepository(pool)
	w := seedWorkWithPages(t, pool, "том", "раз", "два", "три")
	a := trackFor(w, []string{"раз", "два"}, 1, 2)
	b := trackFor(w, []string{"два", "три"}, 2, 3)

	hold, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Rollback(ctx) //nolint:errcheck // откат — часть сценария
	if _, err := hold.Exec(ctx, `
		INSERT INTO audio_tracks (work_id, title, start_page, end_page, s3_key, duration_ms, bytes, md5,
		                          recipe_sha256, pages_sha256)
		VALUES ($1, 'заслонка', 9, 9, $2, 1, 1, '00', $3, $3)`, w, a.S3Key, recipeX); err != nil {
		t.Fatal(err)
	}

	register := func(tr models.AudioTrack) (<-chan struct{}, *error) {
		done, res := make(chan struct{}), new(error)
		go func() { _, _, *res = r.RegisterTracks(ctx, w, []models.AudioTrack{tr}); close(done) }()
		return done, res
	}
	doneA, errA := register(a)
	pidA := waitBlockedBy(t, ctx, backendPID(t, ctx, hold), doneA)
	if pidA == 0 {
		t.Fatal("регистрация A не встала на заслонке")
	}
	doneB, errB := register(b)
	// С сериализацией B ждёт A; без неё — проходит насквозь. Оба исхода
	// дожидаемся, прежде чем отпустить A.
	waitBlockedBy(t, ctx, pidA, doneB)

	if err := hold.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct {
		name string
		done <-chan struct{}
		err  *error
	}{{"A", doneA, errA}, {"B", doneB, errB}} {
		select {
		case <-x.done:
			if *x.err != nil {
				t.Fatalf("регистрация %s: %v", x.name, *x.err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("регистрация %s не завершилась", x.name)
		}
	}
	list, err := r.ListTracks(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(list); i++ {
		if list[i].StartPage <= list[i-1].EndPage {
			t.Errorf("дорожки пересекаются: %d—%d и %d—%d", list[i-1].StartPage, list[i-1].EndPage,
				list[i].StartPage, list[i].EndPage)
		}
	}
	if len(list) != 1 || list[0].S3Key != b.S3Key {
		t.Errorf("после двух регистраций: %+v, ждали одну дорожку B", list)
	}
}
