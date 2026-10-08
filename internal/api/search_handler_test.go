package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

type fakeSearchStore struct {
	terms        []string
	result       *models.SearchResult
	pages        *models.SearchPagesResult
	err          error
	searchCalls  int
	lastQuery    models.SearchQuery
	lastWork     int64
	lastLimit    int
	lastOffset   int
	lastChapters []int64
}

func (f *fakeSearchStore) Terms(ctx context.Context, q models.SearchQuery) ([]string, error) {
	f.lastQuery = q
	return f.terms, f.err
}

func (f *fakeSearchStore) Search(ctx context.Context, q models.SearchQuery) (*models.SearchResult, error) {
	f.searchCalls++
	f.lastQuery = q
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func (f *fakeSearchStore) SearchPages(
	ctx context.Context, q models.SearchQuery, workID int64, chapterIDs []int64, limit, offset int,
) (*models.SearchPagesResult, error) {
	f.lastQuery, f.lastWork, f.lastChapters, f.lastLimit, f.lastOffset = q, workID, chapterIDs, limit, offset
	if f.err != nil {
		return nil, f.err
	}
	return f.pages, nil
}

// Запрос-затычка в здешних адресах — `q=xx`, а не `q=x`: односимвольный
// запрос обработчик отклоняет ДО хранилища (minQueryRunes), и тесты, которым
// нужен дошедший до хранилища запрос, молча перестали бы его проверять. У
// тестов ограничителя это не «молча»: блокирующее хранилище так и не
// займётся, и ожидание `<-store.entered` виснет до таймаута всего набора.
func doSearch(t *testing.T, h *SearchHandler, url string, fn http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	fn(rec, req)
	return rec
}

func TestSearchHandler_EmptyQueryIs400WithMessage(t *testing.T) {
	h := NewSearchHandler(&fakeSearchStore{})
	for _, url := range []string{"/api/search", "/api/search?q=%20%20", "/api/search/pages?q=&work_id=1"} {
		fn := h.Search
		if strings.Contains(url, "/pages") {
			fn = h.Pages
		}
		rec := doSearch(t, h, url, fn)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: code %d", url, rec.Code)
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["message"] == "" {
			t.Errorf("%s: ошибка не в форме {message}: %s", url, rec.Body.String())
		}
	}
}

func TestSearchHandler_UnparseableQueryIs400(t *testing.T) {
	store := &fakeSearchStore{result: &models.SearchResult{Query: "...", Terms: nil}}
	rec := doSearch(t, NewSearchHandler(store), "/api/search?q=...", NewSearchHandler(store).Search)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code %d, body %s", rec.Code, rec.Body.String())
	}
}

func TestSearchHandler_TermsOnlySkipsCatalog(t *testing.T) {
	store := &fakeSearchStore{terms: []string{"гегел"}}
	h := NewSearchHandler(store)
	rec := doSearch(t, h, "/api/search?q=Гегеля&terms_only=1", h.Search)
	if rec.Code != http.StatusOK || store.searchCalls != 0 {
		t.Fatalf("code %d, Search calls %d", rec.Code, store.searchCalls)
	}
	var body models.SearchTerms
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Query != "Гегеля" || strings.Join(body.Terms, ",") != "гегел" {
		t.Errorf("body = %+v", body)
	}
}

func TestSearchHandler_NeverReturnsNullLists(t *testing.T) {
	store := &fakeSearchStore{result: &models.SearchResult{Query: "x", Terms: []string{"x"}}}
	h := NewSearchHandler(store)
	rec := doSearch(t, h, "/api/search?q=xx", h.Search)
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d", rec.Code)
	}
	body := rec.Body.String()
	for _, key := range []string{`"chapters":[]`, `"concepts":[]`, `"volumes":[]`} {
		if !strings.Contains(body, key) {
			t.Errorf("want %s in %s", key, body)
		}
	}
	// Полосы внутри тома — такой же список, по которому клиент зовёт .map:
	// null здесь роняет первый экран целиком, а не портит одну строку.
	store.result = &models.SearchResult{
		Query: "x", Terms: []string{"x"},
		Volumes: []models.SearchVolume{{WorkID: 1, Title: "Том"}},
	}
	rec = doSearch(t, h, "/api/search?q=xx", h.Search)
	if !strings.Contains(rec.Body.String(), `"pages":[]`) {
		t.Errorf("want \"pages\":[] в томе, got %s", rec.Body.String())
	}
	// Фасет глав — тот же приём: Chapters: nil обязан уехать в JSON как [].
	store.pages = &models.SearchPagesResult{Query: "x", Terms: []string{"x"}}
	rec = doSearch(t, h, "/api/search/pages?q=xx&work_id=5", h.Pages)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"pages":[]`) ||
		!strings.Contains(rec.Body.String(), `"chapters":[]`) {
		t.Errorf("pages: code %d body %s", rec.Code, rec.Body.String())
	}
}

func TestSearchHandler_EditionFilterAndBadParams(t *testing.T) {
	store := &fakeSearchStore{result: &models.SearchResult{Query: "x", Terms: []string{"x"}}}
	h := NewSearchHandler(store)
	rec := doSearch(t, h, "/api/search?q=xx&edition_id=7", h.Search)
	if rec.Code != http.StatusOK || len(store.lastQuery.EditionIDs) != 1 || store.lastQuery.EditionIDs[0] != 7 {
		t.Errorf("edition_id не дошёл: code %d, %+v", rec.Code, store.lastQuery)
	}
	if rec := doSearch(t, h, "/api/search?q=xx&edition_id=abc", h.Search); rec.Code != http.StatusBadRequest {
		t.Errorf("мусорный edition_id: code %d", rec.Code)
	}
}

func TestSearchHandler_PagesParams(t *testing.T) {
	store := &fakeSearchStore{pages: &models.SearchPagesResult{Query: "x", Terms: []string{"x"}}}
	h := NewSearchHandler(store)
	rec := doSearch(t, h, "/api/search/pages?q=xx&work_id=5", h.Pages)
	if rec.Code != http.StatusOK || store.lastWork != 5 || store.lastLimit != 50 || store.lastOffset != 0 {
		t.Errorf("умолчания: code %d work %d limit %d offset %d", rec.Code, store.lastWork, store.lastLimit, store.lastOffset)
	}
	rec = doSearch(t, h, "/api/search/pages?q=xx&work_id=5&limit=500&offset=20", h.Pages)
	if rec.Code != http.StatusOK || store.lastLimit != 100 || store.lastOffset != 20 {
		t.Errorf("потолок: code %d limit %d offset %d", rec.Code, store.lastLimit, store.lastOffset)
	}
	for _, bad := range []string{
		"/api/search/pages?q=xx", "/api/search/pages?q=xx&work_id=abc",
		"/api/search/pages?q=xx&work_id=5&limit=0", "/api/search/pages?q=xx&work_id=5&offset=-1",
	} {
		if rec := doSearch(t, h, bad, h.Pages); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: code %d", bad, rec.Code)
		}
	}
}

func TestSearchHandler_PagesChaptersReachStore(t *testing.T) {
	store := &fakeSearchStore{pages: &models.SearchPagesResult{Query: "x", Terms: []string{"x"}}}
	h := NewSearchHandler(store)
	rec := doSearch(t, h, "/api/search/pages?q=xx&work_id=5&chapters=101,102", h.Pages)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(store.lastChapters) != 2 {
		t.Fatalf("chapters = %v, want 2 chapters", store.lastChapters)
	}
	if store.lastChapters[0] != 101 || store.lastChapters[1] != 102 {
		t.Fatalf("chapters = %v, want [101 102]", store.lastChapters)
	}
}

func TestSearchHandler_PagesChaptersBrokenIs400(t *testing.T) {
	store := &fakeSearchStore{pages: &models.SearchPagesResult{Query: "x", Terms: []string{"x"}}}
	h := NewSearchHandler(store)
	rec := doSearch(t, h, "/api/search/pages?q=xx&work_id=5&chapters=101,abc", h.Pages)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
}

func TestSearchHandler_TimeoutIs503(t *testing.T) {
	store := &fakeSearchStore{err: repository.ErrSearchTimeout}
	h := NewSearchHandler(store)
	rec := doSearch(t, h, "/api/search?q=xx", h.Search)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("code %d, body %s", rec.Code, rec.Body.String())
	}
	store.err = errors.New("boom")
	if rec := doSearch(t, h, "/api/search?q=xx", h.Search); rec.Code != http.StatusInternalServerError {
		t.Errorf("другая ошибка: code %d", rec.Code)
	}
}

// blockingSearchStore держит каждый поиск, пока тест не отпустит release, и
// сообщает о входе через entered. Нужен, чтобы занять слоты ограничителя без
// настоящей базы.
type blockingSearchStore struct {
	entered chan struct{}
	release chan struct{}
}

func (b *blockingSearchStore) Terms(ctx context.Context, q models.SearchQuery) ([]string, error) {
	return []string{"x"}, nil
}

func (b *blockingSearchStore) Search(ctx context.Context, q models.SearchQuery) (*models.SearchResult, error) {
	b.entered <- struct{}{}
	<-b.release
	return &models.SearchResult{Query: q.Text, Terms: []string{"x"}}, nil
}

func (b *blockingSearchStore) SearchPages(
	ctx context.Context, q models.SearchQuery, workID int64, chapterIDs []int64, limit, offset int,
) (*models.SearchPagesResult, error) {
	b.entered <- struct{}{}
	<-b.release
	return &models.SearchPagesResult{Query: q.Text, Terms: []string{"x"}}, nil
}

// Два публичных маршрута без авторизации над корпусом в 50 810 полос: конфиг
// ru намеренно без стоп-слов, поэтому «и» — запрос почти по всему корпусу
// (1.5 с на одном, 2.3 с на шести разом). Ограничитель — тот же приём, что у
// тяжёлых рендеров краулеру (internal/seo/handler.go): searchSlots
// одновременно, ожидание ограничено, дальше 503 с {"message"} — той же формы,
// что у исчерпанного statement_timeout.
func TestSearchHandler_ConcurrencyCapRefusesExtraWithBusy503(t *testing.T) {
	store := &blockingSearchStore{
		entered: make(chan struct{}, searchSlots+2),
		release: make(chan struct{}),
	}
	h := NewSearchHandler(store)
	h.wait = 20 * time.Millisecond // иначе проверка отказа стоила бы searchQueueWait

	var wg sync.WaitGroup
	for i := 0; i < searchSlots; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			doSearch(t, h, "/api/search?q=xx", h.Search)
		}()
	}
	for i := 0; i < searchSlots; i++ {
		<-store.entered // оба слота заняты
	}

	// Лишний запрос ждёт h.wait и получает отказ. Проверяем оба маршрута:
	// ограничитель у них общий. Ответ ждём с потолком: без ограничителя
	// запрос уходит в занятое хранилище и висит там до конца теста —
	// внятного отказа не будет, будет тишина.
	for _, tc := range []struct {
		url string
		fn  http.HandlerFunc
	}{
		{"/api/search?q=xx", h.Search},
		{"/api/search/pages?q=xx&work_id=5", h.Pages},
	} {
		done := make(chan *httptest.ResponseRecorder, 1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			done <- doSearch(t, h, tc.url, tc.fn)
		}()
		var rec *httptest.ResponseRecorder
		select {
		case rec = <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: ответа нет — запрос прошёл мимо ограничителя в занятое хранилище", tc.url)
		}
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: code %d, want 503", tc.url, rec.Code)
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["message"] == "" {
			t.Errorf("%s: отказ не в форме {message}: %s", tc.url, rec.Body.String())
		}
		if rec.Header().Get("Retry-After") == "" {
			t.Errorf("%s: нет Retry-After", tc.url)
		}
	}

	close(store.release)
	wg.Wait()
}

// Слот возвращается после запроса, а не удерживается до конца процесса:
// забытое освобождение видно только так — первые searchSlots запросов
// проходят и без него. Заодно проверяется сама очередь: третий запрос уходит
// в ожидание при занятых слотах и дожидается освобождения, а не получает
// отказ немедленно.
func TestSearchHandler_SlotIsReleasedAndQueuedRequestSucceeds(t *testing.T) {
	store := &blockingSearchStore{
		entered: make(chan struct{}, searchSlots+2),
		release: make(chan struct{}),
	}
	h := NewSearchHandler(store)

	var wg sync.WaitGroup
	for i := 0; i < searchSlots; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			doSearch(t, h, "/api/search?q=xx", h.Search)
		}()
	}
	for i := 0; i < searchSlots; i++ {
		<-store.entered
	}

	queued := make(chan int, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		queued <- doSearch(t, h, "/api/search?q=xx", h.Search).Code
	}()
	// Дать третьему дойти до ожидания слота: слоты в этот момент заняты, так
	// что пройти он может только после освобождения.
	time.Sleep(20 * time.Millisecond)
	close(store.release)

	if code := <-queued; code != http.StatusOK {
		t.Errorf("ждавший запрос получил %d, want 200", code)
	}
	wg.Wait()

	// Ограничитель снова пуст: следующий запрос проходит сразу.
	if rec := doSearch(t, h, "/api/search?q=xx", h.Search); rec.Code != http.StatusOK {
		t.Errorf("после освобождения слотов code %d, want 200", rec.Code)
	}
}

func TestSearchHandler_ScopeReachesStore(t *testing.T) {
	store := &fakeSearchStore{terms: []string{"xx"}, result: &models.SearchResult{Terms: []string{"xx"}}}
	h := NewSearchHandler(store)
	rec := doSearch(t, h, "/api/search?q=xx&editions=2&works=13,14", h.Search)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(store.lastQuery.EditionIDs) != 1 || store.lastQuery.EditionIDs[0] != 2 {
		t.Fatalf("editions = %v", store.lastQuery.EditionIDs)
	}
	if len(store.lastQuery.WorkIDs) != 2 {
		t.Fatalf("works = %v", store.lastQuery.WorkIDs)
	}
}

func TestSearchHandler_LegacyEditionIDStillWorks(t *testing.T) {
	store := &fakeSearchStore{terms: []string{"xx"}, result: &models.SearchResult{Terms: []string{"xx"}}}
	h := NewSearchHandler(store)
	rec := doSearch(t, h, "/api/search?q=xx&edition_id=5", h.Search)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	if len(store.lastQuery.EditionIDs) != 1 || store.lastQuery.EditionIDs[0] != 5 {
		t.Fatalf("editions = %v", store.lastQuery.EditionIDs)
	}
}

func TestSearchHandler_BrokenScopeIs400(t *testing.T) {
	h := NewSearchHandler(&fakeSearchStore{terms: []string{"xx"}})
	rec := doSearch(t, h, "/api/search?q=xx&works=13,ы", h.Search)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body.String())
	}
}

// TestSearchResponseCarriesSlugs — глава выдачи обязана довезти свой слаг и
// слаг тома до JSON: клиент строит ссылку по ним, ничего не транслитерируя.
func TestSearchResponseCarriesSlugs(t *testing.T) {
	// Форма подставного хранилища — как в существующих тестах файла: оно
	// отдаёт готовый models.SearchResult полем result, отдельного списка
	// глав у него нет. Terms обязателен непустым: обработчик отвечает 400
	// на пустой разбор ДО того, как посмотрит на список глав (см. другие
	// тесты файла, где Terms задан всюду) — в брифе эта строка была
	// пропущена, и тест с ней падал бы не на слагах, а на "Пустой запрос".
	store := &fakeSearchStore{result: &models.SearchResult{
		Query: "xx",
		Terms: []string{"xx"},
		Chapters: []models.SearchChapter{{
			ID: 10125, Title: "ЧТО ДЕЛАТЬ?", WorkID: 49,
			Slug: "chto-delat", WorkSlug: "lenin-t06",
		}},
	}}
	h := NewSearchHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/search?q=xx", nil)
	rec := httptest.NewRecorder()
	h.Search(rec, req)

	var body struct {
		Chapters []struct {
			Slug     string `json:"slug"`
			WorkSlug string `json:"work_slug"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if len(body.Chapters) != 1 ||
		body.Chapters[0].Slug != "chto-delat" ||
		body.Chapters[0].WorkSlug != "lenin-t06" {
		t.Fatalf("слаги не доехали: %s", rec.Body.String())
	}
}
