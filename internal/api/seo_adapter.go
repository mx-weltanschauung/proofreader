package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"proofreader/internal/models"
	"proofreader/internal/seo"
	"proofreader/pkg/book"
)

// chapterBuilder — то, что SEOBookSource оборачивает. Интерфейс, а не
// *DownloadSource: с конкретным типом переводчик не прогнать без базы.
type chapterBuilder interface {
	Chapter(ctx context.Context, workID, chapterID int64) (*book.Book, error)
}

// SEOBookSource переводит ErrNotFound выгрузки в ошибку пакета seo.
//
// Нужен потому, что internal/seo не может импортировать internal/api:
// router.go держит *seo.Handler, и импорт в обратную сторону замкнул бы цикл.
// Без перевода отсутствующая глава уехала бы краулеру как 500, и поисковик
// понял бы это как «сайт сломан», а не «страницы нет».
type SEOBookSource struct {
	chapters chapterBuilder
}

func NewSEOBookSource(src *DownloadSource) *SEOBookSource {
	return &SEOBookSource{chapters: src}
}

func (a *SEOBookSource) Chapter(
	ctx context.Context, workID, chapterID int64,
) (*book.Book, error) {
	b, err := a.chapters.Chapter(ctx, workID, chapterID)
	if err != nil && errors.Is(err, ErrNotFound) {
		// Оборачиваем обе: seo.ErrNotFound для сравнения, исходную — для
		// журнала, где важно, чего именно не нашлось.
		return nil, fmt.Errorf("%w (%v)", seo.ErrNotFound, err)
	}
	// Контракт seo.BookSource: (nil, nil) недопустим — Source.Chapter (в
	// internal/seo) разыменовывает результат сразу после проверки ошибки, без
	// проверки на nil, потому что подставной источник в его тестах такую пару
	// не отдаёт. DownloadSource.Chapter такую пару не возвращает ни разу
	// (см. internal/api/download_source.go), но полагаться на обещание
	// соседнего файла надёжнее не становится — держим инвариант здесь, на
	// границе пакетов, чтобы регрессия в DownloadSource не превратилась в
	// панику у краулера.
	if b == nil && err == nil {
		return nil, fmt.Errorf("%w: глава %d тома %d пришла пустой без ошибки", seo.ErrNotFound, chapterID, workID)
	}
	return b, err
}

// SEOCollectionSource — подборка вместе с собранным оглавлением.
//
// CollectionRepository.GetBySlug оглавления не собирает: его строит
// CollectionHandler.Get (internal/api/collection_handler.go) из ItemRows,
// ChaptersForWorks и buildCollectionTOC (internal/api/collection_toc.go) —
// обе функции сборки неэкспортированы. Пакету seo эта сборка недоступна:
// импорт internal/api замкнул бы цикл, — поэтому она здесь, рядом с
// CollectionHandler, тем же путём, что и он. Если этот путь разойдётся с
// CollectionHandler.Get, оглавление подборки в API и на странице краулера
// начнёт различаться молча — тест TestSEOCollectionSourceMatchesHandler
// держит их вместе.
type SEOCollectionSource struct {
	collections CollectionStore
}

func NewSEOCollectionSource(collections CollectionStore) *SEOCollectionSource {
	return &SEOCollectionSource{collections: collections}
}

func (a *SEOCollectionSource) GetBySlug(ctx context.Context, slug string) (*models.Collection, error) {
	return a.GetByAuthorSlug(ctx, "", slug)
}

// GetByAuthorSlug — то же самое, но по паре «ник + слаг», второму адресу
// читательской подборки. GetBySlug — частный случай с пустым ником, поэтому
// делегирует сюда, а не наоборот: до задачи 13 GetBySlug сам заворачивал
// CollectionStore.GetBySlug, который под капотом уже был
// GetByAuthorSlug(ctx, "", slug) — здесь дублировать эту развилку незачем.
func (a *SEOCollectionSource) GetByAuthorSlug(ctx context.Context, nickname, slug string) (*models.Collection, error) {
	collection, err := a.collections.GetByAuthorSlug(ctx, nickname, slug)
	if err != nil {
		if collectionNotFound(err) {
			return nil, fmt.Errorf("%w: подборка %q (%v)", seo.ErrNotFound, slug, err)
		}
		return nil, err
	}
	// Симметрично SEOBookSource.Chapter: настоящий CollectionRepository
	// (nil, nil) не отдаёт, но CollectionHandler.Get — путь, который этот
	// метод обязан повторять, — эту проверку имеет (не полагаться на
	// обещание соседнего файла надёжнее, чем полагаться на него).
	if collection == nil {
		return nil, fmt.Errorf("%w: подборка %q пришёл пустым без ошибки", seo.ErrNotFound, slug)
	}

	rows, err := a.collections.ItemRows(ctx, collection.ID)
	if err != nil {
		return nil, fmt.Errorf("состав подборки %q: %w", slug, err)
	}

	chapters, err := a.collections.ChaptersForWorks(ctx, collectionWorkIDs(rows))
	if err != nil {
		return nil, fmt.Errorf("главы подборки %q: %w", slug, err)
	}

	// Тот же вызов, что и в CollectionHandler.Get — состав не должен
	// разойтись между API и страницей краулера.
	collection.Items = buildCollectionTOC(rows, chapters)

	return collection, nil
}

func (a *SEOCollectionSource) List(ctx context.Context) ([]*models.Collection, error) {
	return a.collections.List(ctx)
}

// collectionNotFound отличает «подборки нет» от поломки базы. Репозитории
// проекта sentinel-ошибок не заводят: на отсутствие строки CollectionRepository
// возвращает текст с суффиксом "not found", на прочих бедах — "failed to..."
// (тот же приём документирован в internal/seo/source.go, isNotFound — но он
// неэкспортирован, а internal/api о internal/seo знать не обязан настолько
// глубоко, чтобы делить с ним приватную функцию).
func collectionNotFound(err error) bool {
	return err != nil && strings.HasSuffix(err.Error(), "not found")
}
