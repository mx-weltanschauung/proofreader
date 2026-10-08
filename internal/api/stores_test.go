package api

import (
	"context"
	"fmt"
	"testing"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

// Настоящие репозитории обязаны удовлетворять интерфейсам — иначе проводка
// в cmd/server/main.go перестанет компилироваться.
var (
	_ PageStore        = (*repository.PageRepository)(nil)
	_ PageVersionStore = (*repository.PageVersionRepository)(nil)
	_ ChapterStore     = (*repository.ChapterRepository)(nil)
	_ FragmentStore    = (*repository.IndexFragmentRepository)(nil)
	_ HighlightStore   = (*repository.EditionRepository)(nil)
)

// fakePageStore — подставной PageStore для тестов хендлеров. Каждое поле-функция
// необязательно: невыставленное возвращает нулевые значения.
type fakePageStore struct {
	createFn                 func(ctx context.Context, page *models.Page) error
	getByIDFn                func(ctx context.Context, id int64) (*models.Page, error)
	getByWorkAndPageNumberFn func(ctx context.Context, workID int64, pageNumber int) (*models.Page, error)
	getPageRangeFn           func(ctx context.Context, workID int64, startPage, endPage int) ([]*models.Page, error)
	getPagesByNumbersFn      func(ctx context.Context, workID int64, numbers []int) ([]*models.Page, error)
	listByWorkFn             func(ctx context.Context, workID int64) ([]*models.Page, error)
	listPageMapFn            func(ctx context.Context, workID int64) ([]models.PageMapEntry, error)
	maxPageNumberFn          func(ctx context.Context, workID int64) (int, error)
	saveEditFn               func(ctx context.Context, page *models.Page, version *models.PageVersion) error

	// savedVersions — снимки, уехавшие в SaveEdit. Настоящий репозиторий
	// пишет снимок и текст полосы одной транзакцией, поэтому и здесь они
	// считаются вместе: тест, проверяющий «снимка не было», обязан опираться
	// на тот же счётчик, что и тест «снимок был».
	savedVersions []*models.PageVersion
}

func (f *fakePageStore) Create(ctx context.Context, page *models.Page) error {
	if f.createFn != nil {
		return f.createFn(ctx, page)
	}
	return nil
}

// Умолчание — отказ «не найдено», а не (nil, nil): настоящий
// PageRepository пары (nil, nil) не возвращает НИКОГДА, он отдаёт либо полосу,
// либо ошибку. Тест, дошедший до чтения полосы и не выставивший getByIDFn,
// иначе получил бы null в теле ответа вместо внятного отказа — и остался бы
// зелёным. В этой же ветке уже был случай, когда лгущий фейк делал дефект
// невыразимым: fakePageVersions.GetByID игнорировал аргумент id, и
// несоответствие версии полосе нельзя было проверить ни одним тестом.
func (f *fakePageStore) GetByID(ctx context.Context, id int64) (*models.Page, error) {
	if f.getByIDFn != nil {
		return f.getByIDFn(ctx, id)
	}
	return nil, fmt.Errorf("page %d not found", id)
}

func (f *fakePageStore) GetByWorkAndPageNumber(ctx context.Context, workID int64, pageNumber int) (*models.Page, error) {
	if f.getByWorkAndPageNumberFn != nil {
		return f.getByWorkAndPageNumberFn(ctx, workID, pageNumber)
	}
	return nil, fmt.Errorf("page %d of work %d not found", pageNumber, workID)
}

func (f *fakePageStore) GetPageRange(ctx context.Context, workID int64, startPage, endPage int) ([]*models.Page, error) {
	if f.getPageRangeFn != nil {
		return f.getPageRangeFn(ctx, workID, startPage, endPage)
	}
	return nil, nil
}

func (f *fakePageStore) GetPagesByNumbers(ctx context.Context, workID int64, numbers []int) ([]*models.Page, error) {
	if f.getPagesByNumbersFn != nil {
		return f.getPagesByNumbersFn(ctx, workID, numbers)
	}
	return nil, nil
}

func (f *fakePageStore) ListByWork(ctx context.Context, workID int64) ([]*models.Page, error) {
	if f.listByWorkFn != nil {
		return f.listByWorkFn(ctx, workID)
	}
	return nil, nil
}

func (f *fakePageStore) ListPageMap(ctx context.Context, workID int64) ([]models.PageMapEntry, error) {
	if f.listPageMapFn != nil {
		return f.listPageMapFn(ctx, workID)
	}
	return nil, nil
}

func (f *fakePageStore) MaxPageNumber(ctx context.Context, workID int64) (int, error) {
	if f.maxPageNumberFn != nil {
		return f.maxPageNumberFn(ctx, workID)
	}
	return 0, nil
}

// SaveEdit по умолчанию делает то же, что удачная транзакция настоящего
// репозитория: запоминает снимок и отвечает успехом. Сбой выражается
// saveEditFn — и тогда снимок НЕ запоминается, потому что настоящая
// транзакция его откатывает.
func (f *fakePageStore) SaveEdit(ctx context.Context, page *models.Page, version *models.PageVersion) error {
	if f.saveEditFn != nil {
		return f.saveEditFn(ctx, page, version)
	}
	f.savedVersions = append(f.savedVersions, version)
	return nil
}

// fakeChapterStore — подставной ChapterStore для тестов хендлеров.
type fakeChapterStore struct {
	createFn           func(ctx context.Context, chapter *models.Chapter) error
	deleteFn           func(ctx context.Context, id int64) error
	getByIDFn          func(ctx context.Context, id int64) (*models.Chapter, error)
	listHierarchicalFn func(ctx context.Context, workID int64) ([]*models.Chapter, error)
	moveFn             func(ctx context.Context, chapterID int64, newParentID *int64, newOrder int) error
	updateFn           func(ctx context.Context, chapter *models.Chapter) error
}

func (f *fakeChapterStore) Create(ctx context.Context, chapter *models.Chapter) error {
	if f.createFn != nil {
		return f.createFn(ctx, chapter)
	}
	return nil
}

func (f *fakeChapterStore) Delete(ctx context.Context, id int64) error {
	if f.deleteFn != nil {
		return f.deleteFn(ctx, id)
	}
	return nil
}

// Как и у fakePageStore: настоящий ChapterRepository отдаёт либо главу,
// либо ошибку, пары (nil, nil) он не возвращает никогда.
func (f *fakeChapterStore) GetByID(ctx context.Context, id int64) (*models.Chapter, error) {
	if f.getByIDFn != nil {
		return f.getByIDFn(ctx, id)
	}
	return nil, fmt.Errorf("chapter %d not found", id)
}

func (f *fakeChapterStore) ListByWorkHierarchical(ctx context.Context, workID int64) ([]*models.Chapter, error) {
	if f.listHierarchicalFn != nil {
		return f.listHierarchicalFn(ctx, workID)
	}
	return nil, nil
}

func (f *fakeChapterStore) Move(ctx context.Context, chapterID int64, newParentID *int64, newOrder int) error {
	if f.moveFn != nil {
		return f.moveFn(ctx, chapterID, newParentID, newOrder)
	}
	return nil
}

func (f *fakeChapterStore) Update(ctx context.Context, chapter *models.Chapter) error {
	if f.updateFn != nil {
		return f.updateFn(ctx, chapter)
	}
	return nil
}

// Подставные реализации обязаны удовлетворять интерфейсам.
var (
	_ PageStore    = (*fakePageStore)(nil)
	_ ChapterStore = (*fakeChapterStore)(nil)
)

func TestFakePageStoreDelegates(t *testing.T) {
	called := false
	f := &fakePageStore{
		getPageRangeFn: func(_ context.Context, workID int64, start, end int) ([]*models.Page, error) {
			called = true
			if workID != 7 || start != 3 || end != 9 {
				t.Errorf("got (%d, %d, %d), want (7, 3, 9)", workID, start, end)
			}
			return []*models.Page{{PageNumber: 3}}, nil
		},
	}
	pages, err := f.GetPageRange(context.Background(), 7, 3, 9)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("getPageRangeFn was not called")
	}
	if len(pages) != 1 || pages[0].PageNumber != 3 {
		t.Errorf("unexpected pages: %+v", pages)
	}
}

// Невыставленные поля-функции не паникуют — тесты хендлеров выставляют только
// те методы, которые реально вызываются. Но одиночное чтение по id отвечает
// ОТКАЗОМ, а не пустотой: (nil, nil) — форма, которой настоящие репозитории не
// возвращают никогда, и тест, забредший сюда без выставленной функции, получил
// бы null в теле ответа и остался бы зелёным.
func TestFakeStoresZeroValueIsUsable(t *testing.T) {
	page, err := (&fakePageStore{}).GetByID(context.Background(), 1)
	if err == nil {
		t.Errorf("умолчание вернуло (%+v, nil): фейк обязан отказывать, как репозиторий", page)
	}
	chapter, err := (&fakeChapterStore{}).GetByID(context.Background(), 1)
	if err == nil {
		t.Errorf("умолчание вернуло (%+v, nil): фейк обязан отказывать, как репозиторий", chapter)
	}
	// Списки — другой договор: репозиторий отдаёт пустой срез без ошибки.
	if _, err := (&fakePageStore{}).ListByWork(context.Background(), 1); err != nil {
		t.Errorf("список полос: неожиданная ошибка %v", err)
	}
}
