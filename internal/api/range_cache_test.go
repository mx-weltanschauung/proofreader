package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/auth"
	"proofreader/internal/config"
	"proofreader/internal/models"
	"proofreader/internal/pagecache"
	"proofreader/internal/repository"
	"proofreader/pkg/markdown"
)

func chapterPagesHandler(t *testing.T, cache *RangeCache, pages PageStore) *ChapterHandler {
	t.Helper()
	return NewChapterHandler(
		&fakeChapterStore{
			getByIDFn: func(ctx context.Context, id int64) (*models.Chapter, error) {
				return &models.Chapter{ID: id, WorkID: 47, StartPage: 1, EndPage: 2}, nil
			},
		},
		pages,
		markdown.NewRenderer(),
		cache,
	)
}

func rangePages() []*models.Page {
	return []*models.Page{
		{ID: 1, WorkID: 47, PageNumber: 1, ContentMarkdown: "Первая полоса."},
		{ID: 2, WorkID: 47, PageNumber: 2, ContentMarkdown: "Вторая полоса."},
	}
}

func listPagesRequest(gzipOK bool) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/works/47/chapters/5/pages", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "47", "id": "5"})
	if gzipOK {
		req.Header.Set("Accept-Encoding", "gzip")
	}
	return req
}

// Первый заход собирает ответ и кладёт файл.
func TestListPagesMissFillsCache(t *testing.T) {
	store := pagecache.New(t.TempDir())
	h := chapterPagesHandler(t, NewRangeCache(store), &fakePageStore{
		getPageRangeFn: func(ctx context.Context, workID int64, s, e int) ([]*models.Page, error) {
			return rangePages(), nil
		},
	})

	rec := httptest.NewRecorder()
	h.ListPages(rec, listPagesRequest(true))

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", rec.Code)
	}
	if _, _, ok := store.Open(pagecache.Key{WorkID: 47, Start: 1, End: 2}); !ok {
		t.Error("после промаха файл в кэше не появился")
	}
}

// Главное свойство: попадание не ходит в базу вовсе.
func TestListPagesHitNeverTouchesPageStore(t *testing.T) {
	store := pagecache.New(t.TempDir())
	cache := NewRangeCache(store)

	warm := chapterPagesHandler(t, cache, &fakePageStore{
		getPageRangeFn: func(ctx context.Context, workID int64, s, e int) ([]*models.Page, error) {
			return rangePages(), nil
		},
	})
	warm.ListPages(httptest.NewRecorder(), listPagesRequest(true))

	cold := chapterPagesHandler(t, cache, &fakePageStore{
		getPageRangeFn: func(ctx context.Context, workID int64, s, e int) ([]*models.Page, error) {
			t.Error("попадание в кэш пошло в базу за полосами")
			return nil, nil
		},
	})

	rec := httptest.NewRecorder()
	cold.ListPages(rec, listPagesRequest(true))

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Errorf("Content-Encoding = %q, ожидался gzip", got)
	}

	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("тело не читается как gzip: %v", err)
	}
	var got ChapterPagesResponse
	if err := json.NewDecoder(zr).Decode(&got); err != nil {
		t.Fatalf("тело не разбирается как ответ главы: %v", err)
	}
	if len(got.Pages) != 2 || got.Pages[0].PageNumber != 1 {
		t.Errorf("из кэша пришло %+v, ожидались полосы 1 и 2", got.Pages)
	}
}

// Клиент без gzip (curl, часть ботов) обязан получить читаемый JSON.
func TestListPagesHitServesPlainJSONWithoutGzip(t *testing.T) {
	store := pagecache.New(t.TempDir())
	cache := NewRangeCache(store)
	h := chapterPagesHandler(t, cache, &fakePageStore{
		getPageRangeFn: func(ctx context.Context, workID int64, s, e int) ([]*models.Page, error) {
			return rangePages(), nil
		},
	})
	h.ListPages(httptest.NewRecorder(), listPagesRequest(true))

	rec := httptest.NewRecorder()
	h.ListPages(rec, listPagesRequest(false))

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q, клиент gzip не просил", got)
	}
	var got ChapterPagesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("тело не разбирается как JSON: %v", err)
	}
	if len(got.Pages) != 2 {
		t.Errorf("полос %d, ожидалось 2", len(got.Pages))
	}
}

// Выключенный кэш обслуживает как раньше — это же путь тестов и локального
// make run без PAGE_CACHE_DIR.
func TestListPagesWorksWithDisabledCache(t *testing.T) {
	h := chapterPagesHandler(t, NewRangeCache(pagecache.New("")), &fakePageStore{
		getPageRangeFn: func(ctx context.Context, workID int64, s, e int) ([]*models.Page, error) {
			return rangePages(), nil
		},
	})

	rec := httptest.NewRecorder()
	h.ListPages(rec, listPagesRequest(false))

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", rec.Code)
	}
	var got ChapterPagesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("тело не разбирается: %v", err)
	}
	if len(got.Pages) != 2 {
		t.Errorf("полос %d, ожидалось 2", len(got.Pages))
	}
}

// Два одновременных читателя холодной главы дают один рендер, а не два:
// на боевом два ядра, и второй рендер той же главы отбирает процессор у
// живых читателей.
func TestServeCollapsesConcurrentBuildsIntoOne(t *testing.T) {
	cache := NewRangeCache(pagecache.New(t.TempDir()))
	key := pagecache.Key{WorkID: 47, Start: 1, End: 2}

	var mu sync.Mutex
	builds := 0
	release := make(chan struct{})
	entered := make(chan struct{}, 2)

	build := func() (any, error) {
		mu.Lock()
		builds++
		mu.Unlock()
		entered <- struct{}{}
		<-release
		return ChapterPagesResponse{Pages: []ChapterPage{{PageNumber: 1, HTML: "<p>а</p>"}}}, nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.Serve(httptest.NewRecorder(), listPagesRequest(false), key, build)
		}()
	}

	// Ведущий вошёл в сборку.
	<-entered

	// Отпускать его можно только убедившись, что второй уже встал в
	// очередь: отпустив раньше, мы дали бы второму собрать самому — тест
	// падал бы через раз не по делу.
	deadline := time.After(2 * time.Second)
	for cache.waiterCount(key) == 0 {
		select {
		case <-deadline:
			t.Fatal("второй запрос так и не встал в очередь за чужой сборкой")
		default:
			runtime.Gosched()
		}
	}

	close(release)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if builds != 1 {
		t.Errorf("сборок %d, ожидалась одна на двоих", builds)
	}
}

// Беда кэша не должна превращаться в ошибку читателю.
func TestServeAnswersWhenCacheDirIsUnusable(t *testing.T) {
	// Файл вместо каталога: любая запись внутрь него провалится.
	broken := filepath.Join(t.TempDir(), "не-каталог")
	if err := os.WriteFile(broken, []byte("x"), 0o644); err != nil {
		t.Fatalf("подготовка: %v", err)
	}

	h := chapterPagesHandler(t, NewRangeCache(pagecache.New(broken)), &fakePageStore{
		getPageRangeFn: func(ctx context.Context, workID int64, s, e int) ([]*models.Page, error) {
			return rangePages(), nil
		},
	})

	rec := httptest.NewRecorder()
	h.ListPages(rec, listPagesRequest(false))

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d — отказ кэша уехал читателю как ошибка", rec.Code)
	}
	var got ChapterPagesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("тело не разбирается: %v", err)
	}
	if len(got.Pages) != 2 {
		t.Errorf("полос %d, ожидалось 2", len(got.Pages))
	}
}

// itemPagesRequest — по образцу listPagesRequest, но для маршрута элемента
// подборки: собственные URL-параметры (slug, itemId), диапазон полос тот же,
// что у главы в chapterPagesHandler (WorkID 47, 1—2) — иначе проверить общий
// файл было бы нечем.
func itemPagesRequest() *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/collections/sbornik/items/11/pages", nil)
	return mux.SetURLVars(req, map[string]string{"slug": "sbornik", "itemId": "11"})
}

// Глава и элемент подборки с одним диапазоном обязаны делить один файл:
// иначе один и тот же текст лежит на диске дважды. Доказательство —
// отрицательное: хранилище полос элемента роняет тест при обращении, а
// значит 200 с правильными полосами взялся только из файла, который прогрел
// путь главы, — не из повторного похода в базу.
func TestChapterAndCollectionItemShareOneCacheFile(t *testing.T) {
	dir := t.TempDir()
	cache := NewRangeCache(pagecache.New(dir))

	// Прогрев: тот же диапазон (WorkID 47, 1—2) кладёт в кэш путь главы.
	warm := chapterPagesHandler(t, cache, &fakePageStore{
		getPageRangeFn: func(ctx context.Context, workID int64, s, e int) ([]*models.Page, error) {
			return rangePages(), nil
		},
	})
	warm.ListPages(httptest.NewRecorder(), listPagesRequest(false))

	// Обвязка — по образцу проходящих тестов ItemPages в
	// collection_handler_test.go (collectionFixture/chapterRow), но с
	// границами элемента, подогнанными под диапазон главы выше: элемент 11
	// подборки "sbornik" — это глава 100 работы 47, полосы 1—2.
	collectionStore := &fakeCollectionStore{
		// Опубликована: ItemPages теперь гейтит черновик и снятую с публикации
		// (collectionVisibleTo) так же, как Get — этот тест проверяет
		// разделение кэш-файлов, а не видимость.
		collections: []*models.Collection{{ID: 1, Title: "Подборка", Slug: "sbornik", PublishedAt: ptrTime(time.Now().Add(-time.Hour))}},
		rows: []repository.ItemRow{{
			Item: models.CollectionItem{
				ID: 11, CollectionID: 1, Kind: models.CollectionItemKindChapter,
				ChapterID: ptrInt64(100), WorkID: ptrInt64(47),
				SnapshotTitle: "снимок", OrderNumber: 1,
			},
			ChapterStartPage: ptrInt(1),
			ChapterEndPage:   ptrInt(2),
		}},
		item: &models.CollectionItem{
			ID: 11, CollectionID: 1, Kind: models.CollectionItemKindChapter,
			ChapterID: ptrInt64(100), WorkID: ptrInt64(47), SnapshotTitle: "снимок",
		},
	}

	// Второй путь заходит с тем же *RangeCache, но с хранилищем полос,
	// которое роняет тест при обращении: попадание в кэш не имеет права
	// ходить в базу.
	coldPageStore := &fakePageStore{
		getPageRangeFn: func(ctx context.Context, workID int64, s, e int) ([]*models.Page, error) {
			t.Error("попадание в кэш пошло в базу за полосами")
			return nil, nil
		},
	}
	h := NewCollectionHandler(collectionStore, coldPageStore, markdown.NewRenderer(), cache, "", false)

	rec := httptest.NewRecorder()
	h.ItemPages(rec, itemPagesRequest())

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200; тело: %s", rec.Code, rec.Body.String())
	}
	var got ChapterPagesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("тело не разбирается как ответ страниц: %v", err)
	}
	if len(got.Pages) != 2 || got.Pages[0].PageNumber != 1 || got.Pages[1].PageNumber != 2 {
		t.Errorf("из кэша пришло %+v, ожидались полосы 1 и 2", got.Pages)
	}

	// Вторая половина свойства: элемент не завёл себе отдельный файл —
	// в каталоге работы ровно один, тот же, что положила глава.
	entries, err := os.ReadDir(filepath.Join(dir, "w47"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("файлов в каталоге работы %d, ожидался один на диапазон", len(entries))
	}
}

// TestChapterPagesFullRouterServesCachedBytesUnchanged проверяет стык,
// которого TestGzipLeavesPreCompressedResponseAlone (синтетический
// обработчик, без настоящего роутера) и TestListPagesHitNeverTouchesPageStore
// (обработчик напрямую, мимо CORS/Logging/Gzip) порознь не видят: настоящий
// Router.Setup() поверх прогретого файлового кэша обязан довезти кэшированные
// байты, их размер и код ответа через всю цепочку без искажений. Двойное
// сжатие и потерянный Content-Length ловятся именно здесь.
func TestChapterPagesFullRouterServesCachedBytesUnchanged(t *testing.T) {
	dir := t.TempDir()
	cache := NewRangeCache(pagecache.New(dir))
	h := chapterPagesHandler(t, cache, &fakePageStore{
		getPageRangeFn: func(ctx context.Context, workID int64, s, e int) ([]*models.Page, error) {
			return rangePages(), nil
		},
	})

	// Тот же секрет и те же сроки, что у createTestRouter (router_test.go) —
	// маршрут публичный, токен тут не нужен, но authService обязателен
	// для NewRouter.
	authService := auth.NewService(&config.JWTConfig{
		Secret:            "test-secret",
		Expiration:        time.Hour,
		RefreshExpiration: 24 * time.Hour,
	})
	rt := NewRouter(
		nil, nil, nil, h, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, authService,
	)
	mr := rt.Setup()

	// Прогрев через настоящий роутер: кладёт файл в кэш тем же путём, каким
	// его положил бы боевой сервер.
	warmReq := httptest.NewRequest(http.MethodGet, "/api/works/47/chapters/5/pages", nil)
	mr.ServeHTTP(httptest.NewRecorder(), warmReq)

	cachedPath := filepath.Join(dir, "w47", "1-2.json.gz")
	want, err := os.ReadFile(cachedPath)
	if err != nil {
		t.Fatalf("файл кэша %s не появился: %v", cachedPath, err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/works/47/chapters/5/pages", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	mr.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Errorf("Content-Encoding = %q, ожидался gzip — двойное сжатие или его потеря", got)
	}
	if got := rec.Header().Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Errorf("Vary = %q, ожидался Accept-Encoding", got)
	}
	if got := rec.Header().Get("Content-Length"); got != strconv.Itoa(len(want)) {
		t.Errorf("Content-Length = %q, ожидался размер файла кэша %d", got, len(want))
	}
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Errorf("тело цепочки CORS→Logging→Gzip не совпало с файлом кэша байт в байт: получено %d байт, ожидалось %d",
			rec.Body.Len(), len(want))
	}
}
