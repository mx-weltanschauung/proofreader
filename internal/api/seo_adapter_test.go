package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/pagecache"
	"proofreader/internal/seo"
	"proofreader/pkg/book"
)

type fakeChapterBuilder struct {
	err error
	b   *book.Book
}

func (f *fakeChapterBuilder) Chapter(
	ctx context.Context, workID, chapterID int64,
) (*book.Book, error) {
	return f.b, f.err
}

// Без перевода отсутствующая глава уехала бы краулеру как 500: пакет seo
// сравнивает ошибку со своей ErrNotFound, а выгрузка возвращает свою.
func TestSEOBookSourceTranslatesNotFound(t *testing.T) {
	src := &SEOBookSource{chapters: &fakeChapterBuilder{
		err: fmt.Errorf("%w: глава 7", ErrNotFound),
	}}

	_, err := src.Chapter(context.Background(), 1, 7)
	if !errors.Is(err, seo.ErrNotFound) {
		t.Fatalf("ошибка не переведена в seo.ErrNotFound: %v", err)
	}
}

// Остальные ошибки должны доехать как есть: 500 на поломку базы — правильный
// ответ, и подменять его на 404 нельзя, иначе поисковик выкинет живую
// страницу из индекса.
func TestSEOBookSourceKeepsOtherErrors(t *testing.T) {
	boom := errors.New("база отвалилась")
	src := &SEOBookSource{chapters: &fakeChapterBuilder{err: boom}}

	_, err := src.Chapter(context.Background(), 1, 7)
	if errors.Is(err, seo.ErrNotFound) {
		t.Fatal("поломка базы выдана за отсутствие главы")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("исходная ошибка потеряна: %v", err)
	}
}

// seo.Source.Chapter разыменовывает книгу сразу после проверки ошибки, без
// проверки на nil (задача 5, отмечено ревью). Контракт держится на
// SEOBookSource: (nil, nil) не должен доехать до seo вовсе.
func TestSEOBookSourceRefusesNilNil(t *testing.T) {
	src := &SEOBookSource{chapters: &fakeChapterBuilder{b: nil, err: nil}}

	b, err := src.Chapter(context.Background(), 1, 7)
	if b != nil {
		t.Fatalf("книга не nil при (nil, nil) от источника: %+v", b)
	}
	if !errors.Is(err, seo.ErrNotFound) {
		t.Fatalf("(nil, nil) не превращён в seo.ErrNotFound: %v", err)
	}
}

// Состав, который отдаёт SEOCollectionSource.GetBySlug, обязан совпадать с
// тем, что кладёт в ответ CollectionHandler.Get на тех же данных — иначе
// сборка оглавления подборки живёт в двух местах и может разойтись:
// страница краулера показала бы не то же самое, что API читателю.
func TestSEOCollectionSourceMatchesHandler(t *testing.T) {
	store := collectionFixture()

	// Путь API: CollectionHandler.Get, как его вызывает роутер.
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)
	req := httptest.NewRequest(http.MethodGet, "/api/collections/materializm", nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "materializm"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("CollectionHandler.Get: код %d, тело: %s", rec.Code, rec.Body.String())
	}
	var viaAPI models.Collection
	if err := json.NewDecoder(rec.Body).Decode(&viaAPI); err != nil {
		t.Fatalf("ответ API не разобрался: %v", err)
	}

	// Путь краулера: SEOCollectionSource.GetBySlug — тот же стор, тот же слаг.
	src := NewSEOCollectionSource(store)
	viaSEO, err := src.GetBySlug(context.Background(), "materializm")
	if err != nil {
		t.Fatalf("SEOCollectionSource.GetBySlug: %v", err)
	}

	apiJSON, err := json.Marshal(viaAPI.Items)
	if err != nil {
		t.Fatalf("marshal API items: %v", err)
	}
	seoJSON, err := json.Marshal(viaSEO.Items)
	if err != nil {
		t.Fatalf("marshal SEO items: %v", err)
	}
	if string(apiJSON) != string(seoJSON) {
		t.Fatalf("оглавление разошлось:\nAPI: %s\nSEO: %s", apiJSON, seoJSON)
	}
	if len(viaSEO.Items) == 0 {
		t.Fatal("оглавление пустое — тест ничего не проверил бы")
	}
}

// collectionNotFoundStore подменяет GetBySlug текстом ошибки, каким его
// реально отдаёт CollectionRepository на pgx.ErrNoRows (см.
// internal/repository/collection_repository.go): fakeCollectionStore из
// collection_handler_test.go возвращает под тем же случаем context.Canceled,
// который под конвенцию "not found" не подходит.
type collectionNotFoundStore struct {
	*fakeCollectionStore
}

// GetByAuthorSlug, не GetBySlug: SEOCollectionSource.GetBySlug теперь сам
// делегирует в GetByAuthorSlug(ctx, "", slug) (задача 13, второй адрес
// подборки), и подмена на уровне GetBySlug перестала бы перехватываться.
func (collectionNotFoundStore) GetByAuthorSlug(ctx context.Context, nickname, slug string) (*models.Collection, error) {
	return nil, fmt.Errorf("collection not found")
}

// Отсутствующая подборка должна дойти до краулера как 404, а не как 500:
// иначе поисковик решит, что сайт сломан, а не что подборки нет.
func TestSEOCollectionSourceTranslatesNotFound(t *testing.T) {
	src := NewSEOCollectionSource(collectionNotFoundStore{&fakeCollectionStore{}})

	_, err := src.GetBySlug(context.Background(), "не-существует")
	if !errors.Is(err, seo.ErrNotFound) {
		t.Fatalf("ошибка не переведена в seo.ErrNotFound: %v", err)
	}
}

// Поломка базы не должна прикидываться отсутствием подборки: 500 на
// действительную ошибку — правильный ответ, 404 тут вычеркнул бы живую
// страницу из индекса.
func TestSEOCollectionSourceKeepsOtherErrors(t *testing.T) {
	src := NewSEOCollectionSource(&fakeCollectionStore{})

	_, err := src.GetBySlug(context.Background(), "не-существует")
	if errors.Is(err, seo.ErrNotFound) {
		t.Fatal("поломка базы выдана за отсутствие подборки")
	}
}
