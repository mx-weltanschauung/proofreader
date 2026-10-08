package api

import (
	"context"
	"fmt"
	"strings"

	"proofreader/internal/models"
	"proofreader/internal/seo"
	"proofreader/pkg/markdown"
)

// SEODocumentSource — разбор для страницы краулера.
//
// Нужен по той же причине, что SEOCollectionSource: internal/seo не может
// импортировать internal/api (router.go держит *seo.Handler, и импорт замкнул
// бы цикл), а всё, без чего разбор не напечатать, живёт здесь — и сборка тела
// (assembleDocumentFor), и правило «что именно вправе видеть пришедший»
// (documentForViewer).
type SEODocumentSource struct {
	documents DocumentStore
	cuts      DocumentCutStore
	works     WorkStore
	renderer  *markdown.Renderer
}

func NewSEODocumentSource(
	documents DocumentStore,
	cuts DocumentCutStore,
	works WorkStore,
	renderer *markdown.Renderer,
) *SEODocumentSource {
	return &SEODocumentSource{documents: documents, cuts: cuts, works: works, renderer: renderer}
}

// shown читает разбор по адресу и сводит его к той редакции, которую вправе
// видеть краулер.
func (a *SEODocumentSource) shown(
	ctx context.Context, nickname, slug string,
) (*models.Document, error) {
	document, err := a.documents.GetByAuthorSlug(ctx, nickname, slug)
	if err != nil {
		if documentNotFound(err) {
			return nil, fmt.Errorf("%w: разбор %q (%v)", seo.ErrNotFound, slug, err)
		}
		return nil, err
	}
	if document == nil {
		return nil, fmt.Errorf("%w: разбор %q пришёл пустым без ошибки", seo.ErrNotFound, slug)
	}

	// Тот же разбор «404 против 410», что у подборки (seo.Source.Collection)
	// и у самого обработчика (documentVisibleTo): «никогда не публиковался»
	// — 404, потому что 410 значит «было и снято» и подтвердил бы краулеру
	// сам факт существования черновика; «было и снято» — честный 410,
	// поисковик выбрасывает такой адрес сразу, а 404 перепроверяет неделями.
	// Краулер — всегда посторонний, поэтому ветки mayReadDraft тут нет
	// вовсе: черновик ему не достаётся ни в каком состоянии.
	if document.PublishedAt == nil {
		if !document.WasPublished {
			return nil, fmt.Errorf("%w: разбор %q", seo.ErrNeverPublished, slug)
		}
		return nil, fmt.Errorf("%w: разбор %q", seo.ErrNotFound, slug)
	}

	// Сведение к ОДОБРЕННОЙ редакции — тем же documentForViewer, что решает
	// это для всех прочих посторонних. nil-claims не «заглушка вместо
	// аутентификации», а точное описание пришедшего: у краулера нет и не
	// может быть токена. Без этой строки опубликованный разбор с правкой,
	// ждущей решения модератора, показывал бы краулеру ЧЕРНОВИК — и
	// модерация становилась бы театром (пропустить безобидное, потом
	// переписать), причём переписанное уезжало бы прямиком в поисковую
	// выдачу.
	return documentForViewer(nil, document), nil
}

// Page — разбор с собранным телом.
func (a *SEODocumentSource) Page(
	ctx context.Context, nickname, slug string,
) (*seo.DocumentPage, error) {
	document, err := a.shown(ctx, nickname, slug)
	if err != nil {
		return nil, err
	}

	body, cutCount, err := assembleDocumentFor(ctx, a.renderer, a.cuts, a.works, document)
	if err != nil {
		return nil, fmt.Errorf("сборка разбора %q: %w", slug, err)
	}

	return &seo.DocumentPage{Document: document, BodyHTML: body, CutCount: cutCount}, nil
}

// Card — то же для карточки превью, но БЕЗ сборки тела.
//
// CutCount считается по плейсхолдерам document.MarkdownContent (тело уже
// сведено к видимой краулеру редакции внутри shown), а не походом в
// a.cuts.ByDocument: последний отдал бы ВСЕ вклейки разбора, включая
// названные только черновиком, ждущим решения модератора, — карточка
// печатала бы больше вклеек, чем одобренный текст вообще называет. Тот же
// приём и та же причина, что у assembleDocumentFor (document_render.go), и
// он же сохраняет контракт «карточка не читает базу вклеек вовсе» —
// TestSEODocumentSourceCardShowsApprovedTitleWithoutAssembling.
func (a *SEODocumentSource) Card(
	ctx context.Context, nickname, slug string,
) (*seo.DocumentCard, error) {
	document, err := a.shown(ctx, nickname, slug)
	if err != nil {
		return nil, err
	}

	cutCount := len(uniqueIDs(cutPlaceholderIDs(document.MarkdownContent)))
	return &seo.DocumentCard{Document: document, CutCount: cutCount}, nil
}

// ListPublished — витрина разборов краулеру.
func (a *SEODocumentSource) ListPublished(
	ctx context.Context, limit, offset int,
) ([]*models.Document, error) {
	documents, err := a.documents.ListPublished(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	shown := make([]*models.Document, len(documents))
	for i, d := range documents {
		shown[i] = documentForViewer(nil, d)
	}
	return shown, nil
}

// documentNotFound отличает «разбора нет» от поломки базы. Тот же приём и та
// же причина, что у collectionNotFound (см. его комментарий): репозитории
// проекта sentinel-ошибок не заводят, на pgx.ErrNoRows DocumentRepository
// возвращает текст с суффиксом "not found", на прочих бедах — "failed to…".
// Своя функция, а не вызов чужой: соседка названа по подборкам, и «разбора
// нет» через неё читалось бы как описка.
func documentNotFound(err error) bool {
	return err != nil && strings.HasSuffix(err.Error(), "not found")
}
