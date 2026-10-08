package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"proofreader/internal/models"
)

func newRecording(workID, chapterID int64, n int) *models.AudioRecording {
	return &models.AudioRecording{
		WorkID: workID, ChapterID: chapterID, ContentType: "audio/mpeg",
		S3Key: fmt.Sprintf("works/%d/rec/%d/%d.mp3", workID, chapterID, n), Bytes: 10, DurationMS: 1000,
	}
}

// Recording отдаёт запись с заголовком главы и чтецом — из них ссылка
// «скачать» составляет имя файла.
func TestRecordingCarriesChapterTitleAndReader(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	w := seedWorkWithPages(t, pool, "том", "раз")
	c := seedChapter(t, pool, w, "Товар", 1, 1)
	r := NewAudioRecordingRepository(pool)
	rec := newRecording(w, c, 1)
	rec.Reader = "Иванов"
	if err := r.CreateRecording(ctx, rec); err != nil {
		t.Fatal(err)
	}
	got, err := r.Recording(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.S3Key != rec.S3Key || got.ChapterTitle != "Товар" || got.Reader != "Иванов" ||
		got.Position != 1 || got.ContentType != "audio/mpeg" {
		t.Errorf("Recording = %+v", got)
	}
	if _, err := r.Recording(ctx, rec.ID+1000); !errors.Is(err, ErrAudioNotFound) {
		t.Errorf("отсутствующая: %v, ждали ErrAudioNotFound", err)
	}
}

func TestRecordingsAppendMoveDelete(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	w := seedWorkWithPages(t, pool, "том", "раз", "два")
	c := seedChapter(t, pool, w, "Глава", 1, 2)
	r := NewAudioRecordingRepository(pool)

	var ids []int64
	for i := 1; i <= 3; i++ {
		rec := newRecording(w, c, i)
		if err := r.CreateRecording(ctx, rec); err != nil {
			t.Fatal(err)
		}
		if rec.Position != i {
			t.Fatalf("позиция %d-й записи = %d", i, rec.Position)
		}
		ids = append(ids, rec.ID)
	}
	order := func() []int64 {
		list, err := r.ListRecordings(ctx, w)
		if err != nil {
			t.Fatal(err)
		}
		var out []int64
		for _, x := range list {
			out = append(out, x.ID)
		}
		return out
	}
	// Третью — на первое место; поле «Читает» правится тем же вызовом.
	pos, reader := 1, "Иванов"
	moved, err := r.UpdateRecording(ctx, ids[2], &reader, &pos)
	if err != nil || moved.Position != 1 || moved.Reader != "Иванов" {
		t.Fatalf("перестановка: %+v %v", moved, err)
	}
	if got := order(); fmt.Sprint(got) != fmt.Sprint([]int64{ids[2], ids[0], ids[1]}) {
		t.Errorf("порядок после перестановки: %v", got)
	}
	// Позиция за пределами — зажимается в конец.
	far := 99
	clamped, err := r.UpdateRecording(ctx, ids[2], nil, &far)
	if err != nil || clamped.Position != 3 {
		t.Errorf("зажим: %d %v", clamped.Position, err)
	}
	key, err := r.DeleteRecording(ctx, ids[0])
	if err != nil || key != newRecording(w, c, 1).S3Key {
		t.Fatalf("удаление: %q %v", key, err)
	}
	list, err := r.ListRecordings(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Position != 1 || list[1].Position != 2 {
		t.Errorf("позиции не сомкнулись: %+v", list)
	}
}

func TestRecordingForeignChapterIsNotFound(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	w1 := seedWorkWithPages(t, pool, "том 1", "раз")
	w2 := seedWorkWithPages(t, pool, "том 2", "раз")
	c2 := seedChapter(t, pool, w2, "чужая", 1, 1)
	r := NewAudioRecordingRepository(pool)
	if err := r.CreateRecording(ctx, newRecording(w1, c2, 1)); !errors.Is(err, ErrAudioNotFound) {
		t.Errorf("глава чужого тома: %v", err)
	}
}

// Тикет 02: двойной клик «Прикрепить» регистрирует тот же ключ дважды —
// это отказ «уже есть», а не ошибка сервера, и соседей он не двигает.
func TestRecordingSameKeyTwiceIsDuplicate(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	w := seedWorkWithPages(t, pool, "том", "раз")
	c := seedChapter(t, pool, w, "Глава", 1, 1)
	r := NewAudioRecordingRepository(pool)
	first := newRecording(w, c, 1)
	if err := r.CreateRecording(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateRecording(ctx, newRecording(w, c, 1)); !errors.Is(err, ErrAudioDuplicate) {
		t.Fatalf("повтор ключа: %v, ждали ErrAudioDuplicate", err)
	}
	list, err := r.ListRecordings(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != first.ID || list[0].Position != 1 {
		t.Errorf("после повтора: %+v", list)
	}
}

// Рецензия ветки: список записей тома (JSON /works/{id}/audio) идёт тем же
// порядком, что плейлист (тикет 03): объемлющая глава раньше вложенной, даже
// если номер у неё больше.
func TestListRecordingsPutsEnclosingChapterFirst(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	w := seedWorkWithPages(t, pool, "том", "раз", "два", "три")
	sub := seedChapter(t, pool, w, "§ 1", 1, 2)     // номер меньше
	outer := seedChapter(t, pool, w, "Глава", 1, 3) // объемлющая
	r := NewAudioRecordingRepository(pool)
	for _, c := range []int64{sub, outer} {
		if err := r.CreateRecording(ctx, newRecording(w, c, 1)); err != nil {
			t.Fatal(err)
		}
	}
	list, err := r.ListRecordings(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ChapterID != outer || list[1].ChapterID != sub {
		t.Errorf("порядок записей: %+v, ждали сперва главу %d, потом %d", list, outer, sub)
	}
}

func TestCountInChapterSubtree(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	w := seedWorkWithPages(t, pool, "том", "раз", "два")
	parent := seedChapter(t, pool, w, "Отдел", 1, 2)
	child := seedChapter(t, pool, w, "Глава", 2, 2)
	if _, err := pool.Exec(ctx, `UPDATE chapters SET parent_id = $1 WHERE id = $2`, parent, child); err != nil {
		t.Fatal(err)
	}
	r := NewAudioRecordingRepository(pool)
	if err := r.CreateRecording(ctx, newRecording(w, child, 1)); err != nil {
		t.Fatal(err)
	}
	if n, err := r.CountInChapterSubtree(ctx, parent); err != nil || n != 1 {
		t.Errorf("поддерево: %d %v", n, err)
	}
}

// Создание, перестановка и удаление в одной главе идут параллельно; итог —
// позиции ровно 1..n. Без запирания главы во всех трёх путях под READ COMMITTED
// остаются дыры или дубли (отказ на коммите).
func TestRecordingsConcurrentKeepPositionsDense(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	w := seedWorkWithPages(t, pool, "том", "раз")
	c := seedChapter(t, pool, w, "Глава", 1, 1)
	r := NewAudioRecordingRepository(pool)
	var ids []int64
	for i := 1; i <= 4; i++ {
		rec := newRecording(w, c, i)
		if err := r.CreateRecording(ctx, rec); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, rec.ID)
	}
	for iter := 0; iter < 25; iter++ {
		var wg sync.WaitGroup
		n := 100 + iter*10
		wg.Add(3)
		go func() { defer wg.Done(); _, _ = r.DeleteRecording(ctx, ids[0]) }()
		go func() { defer wg.Done(); _ = r.CreateRecording(ctx, newRecording(w, c, n)) }()
		go func() { defer wg.Done(); p := 1; _, _ = r.UpdateRecording(ctx, ids[len(ids)-1], nil, &p) }()
		wg.Wait()
		list, err := r.ListRecordings(ctx, w)
		if err != nil {
			t.Fatal(err)
		}
		for i, x := range list {
			if x.Position != i+1 {
				t.Fatalf("итерация %d: позиции не 1..n: %+v", iter, list)
			}
		}
		// Восполнить убыль: следующая итерация снова удаляет «первую».
		rec := newRecording(w, c, 1000+iter)
		if err := r.CreateRecording(ctx, rec); err != nil {
			t.Fatal(err)
		}
		ids[0] = rec.ID
		if len(list) > 0 {
			ids[len(ids)-1] = list[len(list)-1].ID
		}
	}
}
