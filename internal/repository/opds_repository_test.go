package repository

import (
	"context"
	"testing"
	"time"

	"proofreader/internal/models"
)

// setPageUpdated ставит метку полосы мимо триггера update_pages_updated_at
// (иначе он перепишет её на now()). session_replication_role гасит триггеры
// на одну транзакцию; нужен суперпользователь — им одноразовая база в
// docker-контейнере и заведена.
func setPageUpdated(t *testing.T, workID int64, page int, at time.Time) {
	t.Helper()
	ctx := context.Background()
	tx, err := testPool(t).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// После Commit откат — пустая операция; ошибка его ничего не значит.
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
		t.Fatalf("триггеры не гасятся (нужен суперпользователь тестовой базы): %v", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE pages SET updated_at = $3 WHERE work_id = $1 AND page_number = $2`, workID, page, at); err != nil {
		t.Fatalf("метка полосы: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestOPDSVolumeStamps(t *testing.T) {
	pool := testPool(t)
	repo := NewOPDSRepository(pool)
	ctx := context.Background()

	full := seedWorkWithPages(t, pool, "Том с полосами", "а", "б", "в")
	empty := seedWorkWithPages(t, pool, "Том без полос")
	early := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	at := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	for p := 1; p <= 3; p++ {
		setPageUpdated(t, full, p, early)
	}
	setPageUpdated(t, full, 2, at) // правка в середине тома — метка всего тома

	got, err := repo.VolumeStamps(ctx, []int64{full, empty})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got[empty]; ok {
		t.Errorf("том без полос не должен получать метку: %+v", got[empty])
	}
	s := got[full]
	if !s.Updated.Equal(at) || s.First != 1 || s.Last != 3 || s.Pages != 3 {
		t.Errorf("метка тома: %+v", s)
	}
}

func TestOPDSChapterStampsFollowPageRange(t *testing.T) {
	pool := testPool(t)
	repo := NewOPDSRepository(pool)
	ctx := context.Background()

	w := seedWorkWithPages(t, pool, "Том", "1", "2", "3", "4", "5")
	early := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	for p := 1; p <= 5; p++ {
		setPageUpdated(t, w, p, early)
	}
	setPageUpdated(t, w, 4, late) // правка полосы 4: видна только главе, которая её накрывает

	first := seedChapter(t, pool, w, "Первая", 1, 3)
	second := seedChapter(t, pool, w, "Вторая", 3, 4) // граница включительно
	beyond := seedChapter(t, pool, w, "За краем", 7, 9)

	got, err := repo.ChapterStamps(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if s := got[first]; !s.Updated.Equal(early) || s.Pages != 3 {
		t.Errorf("первая: %+v", s)
	}
	if s := got[second]; !s.Updated.Equal(late) || s.Pages != 2 {
		t.Errorf("вторая (3—4, правка на 4): %+v", s)
	}
	if s, ok := got[beyond]; !ok || s.Pages != 0 || !s.Updated.IsZero() {
		t.Errorf("глава без полос обязана быть в ответе с нулём: %+v, %v", s, ok)
	}
}

// Том без полос (публикатор завёл работу и ещё льёт полосы) — всегда самый
// свежий по created_at. Отсеивать его обязан сам запрос, до LIMIT: отсев
// после LIMIT давал 30 записей вместо 31, лента «Новые поступления» теряла
// next на всё время заливки, а 31-й том повторялся на второй странице.
func TestOPDSRecentVolumesSkipsVolumeWithoutPagesBeforeLimit(t *testing.T) {
	pool := testPool(t)
	repo := NewOPDSRepository(pool)
	ctx := context.Background()

	oldest := seedWorkWithPages(t, pool, "Первый", "а")
	middle := seedWorkWithPages(t, pool, "Второй", "б")
	newest := seedWorkWithPages(t, pool, "Третий", "в")
	seedChildWorkWithPages(t, pool, newest, "Передние листы третьего", "титул")
	seedWorkWithPages(t, pool, "Публикуется — полос ещё нет")

	first, err := repo.RecentVolumes(ctx, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0].ID != newest || first[1].ID != middle {
		t.Fatalf("первое окно: %v, ждали [%d %d]", workIDs(first), newest, middle)
	}
	if first[0].Slug == "" {
		t.Error("строка тома обязана нести слаг: из него строится адрес в читальне")
	}
	second, err := repo.RecentVolumes(ctx, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].ID != oldest {
		t.Fatalf("второе окно: %v, ждали [%d]", workIDs(second), oldest)
	}
}

// Строки томов для выдачи поиска: передние листы, тома без полос и
// несуществующие id не возвращаются.
func TestOPDSVolumesByIDKeepsOnlyVolumesWithPages(t *testing.T) {
	pool := testPool(t)
	repo := NewOPDSRepository(pool)

	vol := seedWorkWithPages(t, pool, "Том", "а")
	front := seedChildWorkWithPages(t, pool, vol, "Передние листы", "титул")
	empty := seedWorkWithPages(t, pool, "Пустой")

	got, err := repo.VolumesByID(context.Background(), []int64{vol, front, empty, 999999})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != vol {
		t.Fatalf("тома: %v, ждали [%d]", workIDs(got), vol)
	}
}

func workIDs(ws []*models.Work) []int64 {
	var out []int64
	for _, w := range ws {
		out = append(out, w.ID)
	}
	return out
}
