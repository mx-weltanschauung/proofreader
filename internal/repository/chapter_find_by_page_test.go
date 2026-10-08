package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Пустую базу и владельца (id = 1) готовит testPool — см. main_test.go.
func newChapterPageTestRepo(t *testing.T) (*ChapterRepository, *pgxpool.Pool, context.Context) {
	t.Helper()
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO works (id, title, author, file_path, owner_id, role)
		VALUES (10, 'Том 42', 'Автор', 'f', 1, 'volume'),
		       (11, 'Том 43', 'Автор', 'f', 1, 'volume')`); err != nil {
		t.Fatalf("подготовка работ: %v", err)
	}
	return NewChapterRepository(pool), pool, ctx
}

// Главы вложены: произведение занимает полтома, внутри него подглавы. Полосу
// накрывают обе сразу, и canonical полосы обязан вести в самую глубокую —
// иначе читатель, пришедший по цитате, попадает не в ту главу, где эта цитата
// стоит. Правило — ORDER BY end_page - start_page: собственного признака
// глубины у главы нет (parent_id есть, но выбирать по нему значило бы
// подниматься по дереву на каждую полосу), и ширина диапазона её заменяет.
func TestFindByPagePrefersNarrowestChapter(t *testing.T) {
	repo, pool, ctx := newChapterPageTestRepo(t)

	if _, err := pool.Exec(ctx, `
		INSERT INTO chapters (id, work_id, parent_id, title, type, order_number, start_page, end_page)
		VALUES (100, 10, NULL, 'Государство и революция', 'chapter', 1,  5, 120),
		       (101, 10,  100, 'Глава I',                 'chapter', 1,  5,  27),
		       (102, 10,  101, 'Параграф 3',              'chapter', 3, 20,  22)`); err != nil {
		t.Fatalf("подготовка глав: %v", err)
	}

	for _, tc := range []struct {
		page int
		want int64
		why  string
	}{
		{page: 21, want: 102, why: "накрыта всеми тремя — нужен параграф"},
		{page: 25, want: 101, why: "накрыта двумя — нужна глава I"},
		{page: 100, want: 100, why: "накрыта только произведением"},
		// Границы включительно, с обоих концов. Отдельными случаями потому,
		// что мутация «> start_page AND < end_page» убивала лишь один тест из
		// четырёх: полосы в таблице лежали строго внутри диапазонов, и
		// последняя полоса главы — самое частое место цитаты, конец
		// произведения — уезжала бы в главу выше.
		{page: 5, want: 101, why: "первая полоса главы I, она же первая полоса произведения"},
		{page: 22, want: 102, why: "последняя полоса параграфа"},
		{page: 27, want: 101, why: "последняя полоса главы I"},
		{page: 120, want: 100, why: "последняя полоса произведения"},
	} {
		ch, err := repo.FindByPage(ctx, 10, tc.page)
		if err != nil {
			t.Fatalf("FindByPage(10, %d): %v", tc.page, err)
		}
		if ch == nil {
			t.Errorf("FindByPage(10, %d): nil, ожидалась глава %d (%s)", tc.page, tc.want, tc.why)
			continue
		}
		if ch.ID != tc.want {
			t.Errorf("FindByPage(10, %d) = глава %d, ожидалась %d (%s)", tc.page, ch.ID, tc.want, tc.why)
		}
	}
}

// Ничем не накрытая полоса — не ошибка, а треть тома: передние листы, дыры
// между работами и хвост за концом последней главы. Ответ nil, nil, и
// вызывающий откатывает canonical на карточку тома.
func TestFindByPageOutsideEveryChapterIsNil(t *testing.T) {
	repo, pool, ctx := newChapterPageTestRepo(t)

	if _, err := pool.Exec(ctx, `
		INSERT INTO chapters (id, work_id, title, type, order_number, start_page, end_page)
		VALUES (100, 10, 'Государство и революция', 'chapter', 1, 5, 120)`); err != nil {
		t.Fatalf("подготовка глав: %v", err)
	}

	for _, page := range []int{1, 4, 121, 900} {
		ch, err := repo.FindByPage(ctx, 10, page)
		if err != nil {
			t.Fatalf("FindByPage(10, %d): %v", page, err)
		}
		if ch != nil {
			t.Errorf("FindByPage(10, %d) = глава %d, ожидался nil", page, ch.ID)
		}
	}
}

// Нумерация полос у каждой работы своя и начинается с единицы, поэтому полоса
// 10 существует в каждом томе корпуса. Без фильтра по work_id запрос отдал бы
// главу чужого тома — и canonical повёл бы читателя в другую книгу.
func TestFindByPageIsScopedToItsWork(t *testing.T) {
	repo, pool, ctx := newChapterPageTestRepo(t)

	if _, err := pool.Exec(ctx, `
		INSERT INTO chapters (id, work_id, title, type, order_number, start_page, end_page)
		VALUES (100, 10, 'Глава тома 42', 'chapter', 1, 1, 50),
		       (200, 11, 'Глава тома 43', 'chapter', 1, 1, 50)`); err != nil {
		t.Fatalf("подготовка глав: %v", err)
	}

	for workID, want := range map[int64]int64{10: 100, 11: 200} {
		ch, err := repo.FindByPage(ctx, workID, 10)
		if err != nil {
			t.Fatalf("FindByPage(%d, 10): %v", workID, err)
		}
		if ch == nil || ch.ID != want {
			t.Errorf("FindByPage(%d, 10) = %v, ожидалась глава %d", workID, ch, want)
		}
	}
}

// Слаг главы — хвост канонического адреса, и считает его scanChapter, а не
// вызывающий. Забытый слаг не падает: canonical уедет голым номером, то есть
// на неканонический адрес, который /seo сам же перенаправит 301.
func TestFindByPageFillsSlug(t *testing.T) {
	repo, pool, ctx := newChapterPageTestRepo(t)

	if _, err := pool.Exec(ctx, `
		INSERT INTO chapters (id, work_id, title, type, order_number, start_page, end_page)
		VALUES (100, 10, 'Государство и революция', 'chapter', 1, 5, 120)`); err != nil {
		t.Fatalf("подготовка глав: %v", err)
	}

	ch, err := repo.FindByPage(ctx, 10, 5)
	if err != nil {
		t.Fatalf("FindByPage: %v", err)
	}
	if ch.Slug != "gosudarstvo-i-revolyuciya" {
		t.Errorf("слаг главы: %q", ch.Slug)
	}
}
