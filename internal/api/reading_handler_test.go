package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/pkg/markdown"
)

// pagesFrom собирает подряд идущие страницы с предсказуемым текстом.
func pagesFrom(start, n int) []*models.Page {
	pages := make([]*models.Page, n)
	for i := 0; i < n; i++ {
		number := start + i
		pages[i] = &models.Page{
			WorkID:          1,
			PageNumber:      number,
			ContentMarkdown: fmt.Sprintf("Текст страницы %d.", number),
		}
	}
	return pages
}

// readingStore — фейк, отвечающий страницами из непрерывной работы длиной
// total: ровно так устроены все работы в базе.
func readingStore(total int) *fakePageStore {
	return &fakePageStore{
		maxPageNumberFn: func(_ context.Context, _ int64) (int, error) {
			return total, nil
		},
		getPageRangeFn: func(_ context.Context, _ int64, start, end int) ([]*models.Page, error) {
			if start > total {
				return nil, nil
			}
			if end > total {
				end = total
			}
			return pagesFrom(start, end-start+1), nil
		},
	}
}

func doWindow(t *testing.T, store *fakePageStore, query string) (*httptest.ResponseRecorder, ReadingWindowResponse) {
	t.Helper()
	handler := NewReadingHandler(store, markdown.NewRenderer())

	req := httptest.NewRequest(http.MethodGet, "/api/works/1/reading"+query, nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "1"})
	rec := httptest.NewRecorder()

	handler.Window(rec, req)

	var body ReadingWindowResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("разбор ответа: %v; тело: %s", err, rec.Body.String())
		}
	}
	return rec, body
}

func TestReadingWindowReturnsRequestedCount(t *testing.T) {
	rec, body := doWindow(t, readingStore(100), "?from=43&count=10")

	if rec.Code != http.StatusOK {
		t.Fatalf("код = %d, ожидался 200", rec.Code)
	}
	if len(body.Pages) != 10 {
		t.Fatalf("страниц = %d, ожидалось 10", len(body.Pages))
	}
	if body.Pages[0].PageNumber != 43 {
		t.Errorf("первая страница = %d, ожидалась 43", body.Pages[0].PageNumber)
	}
	if body.Pages[9].PageNumber != 52 {
		t.Errorf("последняя страница = %d, ожидалась 52", body.Pages[9].PageNumber)
	}
	if body.TotalPages != 100 {
		t.Errorf("total_pages = %d, ожидалось 100", body.TotalPages)
	}
}

// Страница-разведчик спрашивается, но в ответ не едет: её номер и есть
// next_from.
func TestReadingWindowProbePageDoesNotLeakIntoResponse(t *testing.T) {
	_, body := doWindow(t, readingStore(100), "?from=43&count=10")

	if body.NextFrom == nil {
		t.Fatal("next_from = null, ожидалось 53")
	}
	if *body.NextFrom != 53 {
		t.Errorf("next_from = %d, ожидалось 53", *body.NextFrom)
	}
	for _, p := range body.Pages {
		if p.PageNumber == 53 {
			t.Error("страница-разведчик 53 попала в ответ")
		}
	}
}

// Последнее окно работы: продолжения нет.
func TestReadingWindowLastWindowHasNoNext(t *testing.T) {
	_, body := doWindow(t, readingStore(50), "?from=45&count=10")

	if len(body.Pages) != 6 {
		t.Fatalf("страниц = %d, ожидалось 6 (45..50)", len(body.Pages))
	}
	if body.NextFrom != nil {
		t.Errorf("next_from = %d, ожидался null", *body.NextFrom)
	}
}

func TestReadingWindowClampsCount(t *testing.T) {
	_, tooBig := doWindow(t, readingStore(500), "?from=1&count=999")
	if len(tooBig.Pages) != 50 {
		t.Errorf("страниц при count=999 = %d, ожидалось 50", len(tooBig.Pages))
	}

	_, tooSmall := doWindow(t, readingStore(500), "?from=1&count=0")
	if len(tooSmall.Pages) != 1 {
		t.Errorf("страниц при count=0 = %d, ожидалась 1", len(tooSmall.Pages))
	}
}

// Кривой или отсутствующий параметр не ошибка: читалка начинает сначала.
func TestReadingWindowDefaults(t *testing.T) {
	_, body := doWindow(t, readingStore(100), "")

	if len(body.Pages) != 10 {
		t.Errorf("страниц по умолчанию = %d, ожидалось 10", len(body.Pages))
	}
	if body.Pages[0].PageNumber != 1 {
		t.Errorf("первая страница по умолчанию = %d, ожидалась 1", body.Pages[0].PageNumber)
	}

	_, garbage := doWindow(t, readingStore(100), "?from=abc&count=xyz")
	if len(garbage.Pages) != 10 || garbage.Pages[0].PageNumber != 1 {
		t.Errorf("нечисловые параметры должны дать умолчания, получено %d страниц с %d",
			len(garbage.Pages), garbage.Pages[0].PageNumber)
	}
}

// Ключевое отличие от чтения главы: счётчик подстрочных сносок
// перезапускается на каждой странице. У потока нет области, по которой можно
// было бы вести сквозной счёт, — он не кончается.
func TestReadingWindowNumbersSubscriptNotesPerPage(t *testing.T) {
	store := &fakePageStore{
		maxPageNumberFn: func(_ context.Context, _ int64) (int, error) { return 2, nil },
		getPageRangeFn: func(_ context.Context, _ int64, _, _ int) ([]*models.Page, error) {
			return []*models.Page{
				{PageNumber: 1, ContentMarkdown: "Альфа[^r2].\n\n[^r2]: первая."},
				{PageNumber: 2, ContentMarkdown: "Бета[^r7].\n\n[^r7]: вторая."},
			}, nil
		},
	}

	_, body := doWindow(t, store, "?from=1&count=10")

	if len(body.Pages) != 2 {
		t.Fatalf("страниц = %d, ожидалось 2", len(body.Pages))
	}
	for i, p := range body.Pages {
		if !strings.Contains(p.HTML, ">(1)</a>") {
			t.Errorf("страница %d: маркер (1) не найден в тексте:\n%s", i, p.HTML)
		}
		if p.NotesHTML == "" {
			t.Errorf("страница %d: сноски пусты, а должны быть свои", i)
		}
	}
	// Сквозной счёт дал бы (2) на второй странице — проверяем, что его нет.
	if strings.Contains(body.Pages[1].HTML, ">(2)</a>") {
		t.Error("на второй странице маркер (2): счётчик не перезапустился")
	}
}

// Номера редакционных примечаний книжные и постранично не перезапускаются.
func TestReadingWindowKeepsEndnoteNumbers(t *testing.T) {
	store := &fakePageStore{
		maxPageNumberFn: func(_ context.Context, _ int64) (int, error) { return 1, nil },
		getPageRangeFn: func(_ context.Context, _ int64, _, _ int) ([]*models.Page, error) {
			return []*models.Page{
				{PageNumber: 1, ContentMarkdown: "Текст[^317].\n\n[^317]: примечание."},
			}, nil
		},
	}

	_, body := doWindow(t, store, "?from=1&count=10")

	if !strings.Contains(body.Pages[0].HTML, ">317</a>") {
		t.Errorf("номер примечания 317 потерян:\n%s", body.Pages[0].HTML)
	}
}

func TestReadingWindowMarksBlankPages(t *testing.T) {
	store := &fakePageStore{
		maxPageNumberFn: func(_ context.Context, _ int64) (int, error) { return 2, nil },
		getPageRangeFn: func(_ context.Context, _ int64, _, _ int) ([]*models.Page, error) {
			return []*models.Page{
				{PageNumber: 1, ContentMarkdown: "   \n\n  "},
				{PageNumber: 2, ContentMarkdown: "Текст."},
			}, nil
		},
	}

	_, body := doWindow(t, store, "?from=1&count=10")

	if !body.Pages[0].Blank {
		t.Error("страница из одних пробелов не помечена пустой")
	}
	if body.Pages[1].Blank {
		t.Error("страница с текстом помечена пустой")
	}
}

func TestReadingWindowRejectsBadInput(t *testing.T) {
	handler := NewReadingHandler(readingStore(10), markdown.NewRenderer())

	req := httptest.NewRequest(http.MethodGet, "/api/works/abc/reading", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "abc"})
	rec := httptest.NewRecorder()
	handler.Window(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("нечисловой workId: код = %d, ожидался 400", rec.Code)
	}

	// Работа без страниц.
	rec404, _ := doWindow(t, readingStore(0), "?from=1")
	if rec404.Code != http.StatusNotFound {
		t.Errorf("работа без страниц: код = %d, ожидался 404", rec404.Code)
	}

	// Стартовая страница за концом работы.
	recPast, _ := doWindow(t, readingStore(10), "?from=999")
	if recPast.Code != http.StatusNotFound {
		t.Errorf("from за концом работы: код = %d, ожидался 404", recPast.Code)
	}
}

// gappedStore — работа с дырой в нумерации: страницы есть по обе стороны
// разрыва, самих номеров внутри разрыва нет. Так же ведёт себя и настоящий
// запрос: он отбирает существующие строки диапазона, отсутствующие просто не
// приходят.
func gappedStore(total int, missing ...int) *fakePageStore {
	gap := make(map[int]bool, len(missing))
	for _, n := range missing {
		gap[n] = true
	}
	return &fakePageStore{
		maxPageNumberFn: func(_ context.Context, _ int64) (int, error) {
			return total, nil
		},
		getPageRangeFn: func(_ context.Context, _ int64, start, end int) ([]*models.Page, error) {
			if end > total {
				end = total
			}
			var pages []*models.Page
			for n := start; n <= end; n++ {
				if gap[n] {
					continue
				}
				pages = append(pages, pagesFrom(n, 1)...)
			}
			return pages, nil
		},
	}
}

// Продолжение обработчик узнаёт приёмом «страница-разведчик»: просит на
// страницу больше, чем отдаёт, и наличие лишней и есть ответ. Приём опирается
// на сплошную нумерацию — в базе она сплошная, но договор стоит зафиксировать,
// а не подразумевать.
//
// На дыре разведчик не приходит, и поток встаёт перед разрывом. Отказ мягкий
// по устройству: читатель видит меньше, чем есть, но не видит неверного —
// отданные страницы целы и идут подряд.
func TestReadingWindowStopsAtNumberingGap(t *testing.T) {
	// Дыра ровно там, где идёт разведчик: окно 1..5 отдаётся целиком, а
	// страницы 7..30 остаются недостижимыми.
	_, body := doWindow(t, gappedStore(30, 6), "?from=1&count=5")

	if len(body.Pages) != 5 {
		t.Fatalf("страниц = %d, ожидалось 5", len(body.Pages))
	}
	if body.Pages[0].PageNumber != 1 || body.Pages[4].PageNumber != 5 {
		t.Errorf("отданы страницы %d..%d, ожидались 1..5",
			body.Pages[0].PageNumber, body.Pages[4].PageNumber)
	}
	if body.NextFrom != nil {
		t.Errorf("next_from = %d, ожидался null: за дырой поток не продолжается",
			*body.NextFrom)
	}
	// Длина работы приходит настоящая, из MaxPageNumber: поток встал, но доля
	// прочитанного считается по всему тому и не врёт в большую сторону.
	if body.TotalPages != 30 {
		t.Errorf("total_pages = %d, ожидалось 30", body.TotalPages)
	}
}

// Дыра внутри окна выдачу не рвёт — страницы по обе стороны едут подряд, —
// но останавливает поток так же, как дыра на границе.
//
// Разведчик выбирается по номеру (from+count), а не по счёту пришедших строк:
// при дыре на 4 диапазон 1..6 отдаёт пять страниц вместо шести, и страница 6,
// которая была разведчиком, занимает освободившееся место в самом окне.
// Отличить «окно кончилось» от «дальше есть» становится нечем.
func TestReadingWindowSkipsGapInsideWindow(t *testing.T) {
	_, body := doWindow(t, gappedStore(30, 4), "?from=1&count=5")

	got := make([]int, len(body.Pages))
	for i, p := range body.Pages {
		got[i] = p.PageNumber
	}
	want := []int{1, 2, 3, 5, 6}
	if len(got) != len(want) {
		t.Fatalf("страницы = %v, ожидались %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("страницы = %v, ожидались %v", got, want)
		}
	}
	// Страницы 7..30 существуют, но поток до них не дойдёт: разведчика,
	// который бы о них сообщил, дыра забрала в выдачу.
	if body.NextFrom != nil {
		t.Errorf("next_from = %d, ожидался null: дыра съела разведчика", *body.NextFrom)
	}
}
