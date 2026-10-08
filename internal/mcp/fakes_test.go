package mcp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"proofreader/internal/limit"
	"proofreader/internal/models"
	"proofreader/internal/seo"
	"proofreader/pkg/book"
)

const base = "https://lib.example.org"

// testGates — ожидания в долях секунды: иначе проверка отказа стоила бы
// двадцати секунд.
func testGates() gates {
	return gates{
		total: limit.New(4), search: limit.New(1), heavy: limit.New(1),
		totalWait: 2 * time.Second, gateWait: 2 * time.Second, sharedWait: time.Second,
	}
}

type fakeSearch struct {
	result *models.SearchResult
	pages  *models.SearchPagesResult
	err    error
	// block — если не nil, запрос ждёт его закрытия; entered — сигнал
	// «вошёл в хранилище».
	block   chan struct{}
	entered chan struct{}
	panics  bool

	gotQuery    models.SearchQuery
	gotWork     int64
	gotChapters []int64
	gotLimit    int
	gotOffset   int
}

func (f *fakeSearch) wait() {
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.block != nil {
		<-f.block
	}
	if f.panics {
		panic("хранилище упало")
	}
}

func (f *fakeSearch) Search(_ context.Context, q models.SearchQuery) (*models.SearchResult, error) {
	f.gotQuery = q
	f.wait()
	return f.result, f.err
}

func (f *fakeSearch) SearchPages(_ context.Context, q models.SearchQuery, work int64, chapters []int64, limit, offset int) (*models.SearchPagesResult, error) {
	f.gotQuery, f.gotWork, f.gotChapters, f.gotLimit, f.gotOffset = q, work, chapters, limit, offset
	f.wait()
	return f.pages, f.err
}

type fakeWorks map[int64]*models.Work

func (f fakeWorks) GetByID(_ context.Context, id int64) (*models.Work, error) {
	if w, ok := f[id]; ok {
		return w, nil
	}
	return nil, fmt.Errorf("work not found")
}

// fakeChapters — глава по номеру полосы.
type fakeChapters map[int]*models.Chapter

func (f fakeChapters) FindByPage(_ context.Context, _ int64, page int) (*models.Chapter, error) {
	return f[page], nil
}

type fakeLibrary struct {
	books map[[3]int]*book.Book // {work, from, to}
}

func (f *fakeLibrary) PageRange(_ context.Context, work int64, from, to int) (*book.Book, error) {
	if b, ok := f.books[[3]int{int(work), from, to}]; ok {
		return b, nil
	}
	return nil, fmt.Errorf("%w: полосы %d—%d", ErrNotFound, from, to)
}

// fakeText — seo.Handler понарошку: пути — в каком виде пришли; heavy, если
// задан, занимается через acquire, как у настоящего рендера главы.
type fakeText struct {
	byPath  map[string]*seo.TextResult
	heavy   limit.Slots
	block   chan struct{}
	entered chan struct{}
	mu      sync.Mutex
	gotPath []string
}

func (f *fakeText) Text(ctx context.Context, path string, acquire seo.AcquireFunc) (*seo.TextResult, error) {
	f.mu.Lock()
	f.gotPath = append(f.gotPath, path)
	f.mu.Unlock()
	if f.heavy != nil {
		rel, err := acquire(ctx, f.heavy, true)
		if err != nil {
			return nil, err
		}
		defer rel()
		if f.entered != nil {
			f.entered <- struct{}{}
		}
		if f.block != nil {
			<-f.block
		}
	}
	if r, ok := f.byPath[path]; ok {
		return r, nil
	}
	return nil, fmt.Errorf("%w: %s", seo.ErrNotFound, path)
}
