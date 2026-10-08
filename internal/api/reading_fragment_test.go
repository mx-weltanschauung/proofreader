package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/pkg/markdown"
)

// fragmentPages — две полосы с подстрочной сноской и с ручной таблицей
// примечаний. Таблица здесь не для красоты: UseXHTML на вставленный руками
// HTML не действует, и сырой <br> из неё — единственное, что ловит подмену
// разбора дерева строковой обработкой.
func fragmentPages() []*models.Page {
	return []*models.Page{
		{ID: 10, WorkID: 1, PageNumber: 3,
			ContentMarkdown: "Альфа[^r2].\n\n[^r2]: первая подстрочная. *Ред.*"},
		{ID: 11, WorkID: 1, PageNumber: 4,
			ContentMarkdown: "<table><tr><td>центнер<br>второй строкой</td></tr></table>\n\n" +
				"Бета[^r3].\n\n[^r3]: вторая подстрочная. *Ред.*"},
	}
}

// assertFragment — полоса не должна быть целым документом.
func assertFragment(t *testing.T, where, html string) {
	t.Helper()
	if strings.TrimSpace(html) == "" {
		t.Errorf("%s: пусто — снятие обёртки не должно съедать текст", where)
		return
	}
	for _, bad := range []string{"<!DOCTYPE", "<html", "<head", "GENERATOR"} {
		if strings.Contains(html, bad) {
			t.Errorf("%s: полоса приехала целым документом, найдено %q:\n%s", where, bad, html)
		}
	}
}

// countFootnoteDefs — сколько определений сносок в блоке аппарата.
func countFootnoteDefs(s string) int { return strings.Count(s, `<li id="fn:`) }

// TestChapterPagesAreFragmentsWithFootnotesIntact — сторож на шве HTTP.
//
// Обе проверки в одном тесте намеренно. Обёртку снимает тот же код, по
// соседству с которым живёт извлечение сносок, и разъезжаются они молча:
// тест, проверяющий только обёртку, останется зелёным над уехавшим аппаратом,
// а ради аппарата весь этот код и написан. Тест, зовущий рендерер напрямую,
// не проверяет ни того ни другого — между рендерером и читателем лежит
// обработчик.
func TestChapterPagesAreFragmentsWithFootnotesIntact(t *testing.T) {
	h := &ChapterHandler{
		chapterRepo: &fakeChapterStore{
			getByIDFn: func(_ context.Context, id int64) (*models.Chapter, error) {
				return &models.Chapter{ID: id, WorkID: 1, StartPage: 3, EndPage: 4}, nil
			},
		},
		pageRepo: &fakePageStore{
			getPageRangeFn: func(_ context.Context, _ int64, _, _ int) ([]*models.Page, error) {
				return fragmentPages(), nil
			},
		},
		renderer: markdown.NewRenderer(),
	}

	req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/works/1/chapters/1/pages", nil),
		map[string]string{"workId": "1", "id": "1"})
	rec := httptest.NewRecorder()
	h.ListPages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200; тело: %s", rec.Code, rec.Body.String())
	}

	var got struct {
		Pages []struct {
			PageNumber int    `json:"page_number"`
			HTML       string `json:"html"`
		} `json:"pages"`
		FootnotesHTML string `json:"footnotes_html"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("разбор ответа: %v; тело: %s", err, rec.Body.String())
	}

	if len(got.Pages) != 2 {
		t.Fatalf("полос %d, ожидалось 2", len(got.Pages))
	}
	for _, p := range got.Pages {
		assertFragment(t, "полоса главы", p.HTML)
	}

	if n := countFootnoteDefs(got.FootnotesHTML); n != 2 {
		t.Errorf("определений сносок %d, ожидалось 2 — аппарат уехал вместе с обёрткой:\n%s",
			n, got.FootnotesHTML)
	}
	assertFragment(t, "блок сносок главы", got.FootnotesHTML)

	// Ручной HTML пережил разбор дерева и приехал закрытым по-XHTML.
	joined := got.Pages[0].HTML + got.Pages[1].HTML
	if !strings.Contains(joined, "центнер") {
		t.Errorf("таблица примечаний потерялась:\n%s", joined)
	}
	if strings.Contains(joined, "<br>") {
		t.Errorf("сырой <br> из ручного HTML не закрыт — разбор дерева не отработал:\n%s", joined)
	}
}

// TestCollectionItemPagesAreFragmentsWithFootnotesIntact — тот же шов
// (обёртка снята, аппарат сносок цел, ручной HTML пережил разбор дерева),
// но по маршруту элемента подборки: своя ветка разрешения границ
// (resolveItemPageRange), свой обработчик, форма ответа общая с главой.
func TestCollectionItemPagesAreFragmentsWithFootnotesIntact(t *testing.T) {
	store := collectionFixture()
	store.item = &models.CollectionItem{
		ID: 9, CollectionID: 1, Kind: models.CollectionItemKindChapter,
		ChapterID: ptrInt64(300), WorkID: ptrInt64(1),
	}
	store.rows = []repository.ItemRow{
		{
			Item: models.CollectionItem{
				ID: 9, Kind: models.CollectionItemKindChapter,
				ChapterID: ptrInt64(300), WorkID: ptrInt64(1),
			},
			ChapterStartPage: ptrInt(3),
			ChapterEndPage:   ptrInt(4),
		},
	}

	pageStore := &fakePageStore{
		getPageRangeFn: func(_ context.Context, _ int64, _, _ int) ([]*models.Page, error) {
			return fragmentPages(), nil
		},
	}

	// nil-кэш означает «кэша нет»: этот тест смотрит на форму ответа, а не на
	// отдачу из файла, и файлов на диске заводить ему незачем.
	h := NewCollectionHandler(store, pageStore, markdown.NewRenderer(), nil, "", false)

	req := httptest.NewRequest(http.MethodGet, "/api/collections/materializm/items/9/pages", nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "materializm", "itemId": "9"})
	rec := httptest.NewRecorder()
	h.ItemPages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200; тело: %s", rec.Code, rec.Body.String())
	}

	var got struct {
		Pages []struct {
			PageNumber int    `json:"page_number"`
			HTML       string `json:"html"`
		} `json:"pages"`
		FootnotesHTML string `json:"footnotes_html"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("разбор ответа: %v; тело: %s", err, rec.Body.String())
	}

	if len(got.Pages) != 2 {
		t.Fatalf("полос %d, ожидалось 2", len(got.Pages))
	}
	for _, p := range got.Pages {
		assertFragment(t, "полоса элемента подборки", p.HTML)
	}

	if n := countFootnoteDefs(got.FootnotesHTML); n != 2 {
		t.Errorf("определений сносок %d, ожидалось 2 — аппарат уехал вместе с обёрткой:\n%s",
			n, got.FootnotesHTML)
	}
	assertFragment(t, "блок сносок элемента подборки", got.FootnotesHTML)

	joined := got.Pages[0].HTML + got.Pages[1].HTML
	if !strings.Contains(joined, "центнер") {
		t.Errorf("таблица примечаний потерялась:\n%s", joined)
	}
	if strings.Contains(joined, "<br>") {
		t.Errorf("сырой <br> из ручного HTML не закрыт — разбор дерева не отработал:\n%s", joined)
	}
}

// TestReadingWindowPagesAreFragmentsWithFootnotesIntact — тот же шов на
// окне потокового чтения. Форма ответа другая: сноски едут не одним общим
// блоком, а по полю notes_html у каждой полосы отдельно — Window рендерит
// постранично, по одной полосе за вызов CollectPages (см. комментарий в
// ReadingHandler.Window про сквозной счёт подстрочных на потоке без границ).
// readingStore здесь не годится: она отдаёт страницы с предсказуемым
// текстом без сносок и ручного HTML — фейк собран отдельно, на fragmentPages.
func TestReadingWindowPagesAreFragmentsWithFootnotesIntact(t *testing.T) {
	store := &fakePageStore{
		maxPageNumberFn: func(_ context.Context, _ int64) (int, error) {
			return 4, nil
		},
		getPageRangeFn: func(_ context.Context, _ int64, _, _ int) ([]*models.Page, error) {
			return fragmentPages(), nil
		},
	}

	rec, body := doWindow(t, store, "?from=3&count=2")

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200; тело: %s", rec.Code, rec.Body.String())
	}
	if len(body.Pages) != 2 {
		t.Fatalf("полос %d, ожидалось 2", len(body.Pages))
	}

	totalDefs := 0
	var joined string
	for _, p := range body.Pages {
		assertFragment(t, "полоса окна чтения", p.HTML)
		assertFragment(t, "notes_html окна чтения", p.NotesHTML)
		totalDefs += countFootnoteDefs(p.NotesHTML)
		joined += p.HTML
	}

	if totalDefs != 2 {
		t.Errorf("определений сносок %d, ожидалось 2 — аппарат уехал вместе с обёрткой", totalDefs)
	}
	if !strings.Contains(joined, "центнер") {
		t.Errorf("таблица примечаний потерялась:\n%s", joined)
	}
	if strings.Contains(joined, "<br>") {
		t.Errorf("сырой <br> из ручного HTML не закрыт — разбор дерева не отработал:\n%s", joined)
	}
}
