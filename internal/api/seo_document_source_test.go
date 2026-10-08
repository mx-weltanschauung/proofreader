package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"proofreader/internal/models"
	"proofreader/internal/seo"
	"proofreader/pkg/markdown"
)

// ─── фикстура ──────────────────────────────────────────────────────────────

// seoDocCorpus — кусок корпуса во вклейке. Ручная таблица тома и сноска: по
// ним видно, что вклейка едет ПРЕЖНИМ рендерером, а не подавленным.
const seoDocCorpus = "Ленин писал[^r1] так:\n\n" +
	"<table><tr><td>товар</td><td>1901</td></tr></table>\n\n" +
	"*Разрядка* в тексте.\n\n[^r1]: примечание внизу полосы"

// seoDocApproved и seoDocDraft — две редакции одного разбора. Различаются не
// косметикой, а СЛОВАМИ: тест ищет в выводе именно их, и подмена одной на
// другую видна без разбора вёрстки.
const (
	seoDocApproved = "Одобренный текст разбора.\n\n<cut id=\"7\">\n\nКонец одобренного.\n"
	seoDocDraft    = "ЧЕРНОВИК ждёт модерации.\n\n<cut id=\"7\">\n\nКонец черновика.\n"
)

// seoDocFixture — разбор читателя, опубликованный и с правкой, ждущей
// решения модератора: ровно тот случай, в котором страница краулера обязана
// показать ОДОБРЕННУЮ редакцию.
func seoDocFixture() (*models.Document, *fakeDocumentStore, *fakeDocumentCutStore, WorkStore) {
	cut := &models.DocumentCut{
		ID: 7, DocumentID: 1, WorkID: ptrInt64(1),
		Anchor: models.Anchor{StartPageID: 10, StartOffset: 0, EndPageID: 10,
			EndOffset: len(seoDocCorpus)},
		Status: models.CutStatusOK, SourceTitle: "Ленин. Что делать?",
	}
	cuts := newFakeDocumentCutStore(cut)
	cuts.pagesByCutID = map[int64][]*models.Page{7: {cutPage(10, 4, seoDocCorpus)}}

	published := time.Unix(1_700_000_000, 0)
	doc := &models.Document{
		ID: 1, Slug: "chto-delat", AuthorNickname: "чтец", OwnerID: ptrInt64(5),
		Title: "Черновое заглавие", MarkdownContent: seoDocDraft,
		PublishedTitle: "Что делать?", PublishedMarkdown: seoDocApproved,
		PublishedAt: &published, WasPublished: true,
		ReviewStatus: models.DocumentPending,
		UpdatedAt:    time.Unix(1_700_000_500, 0),
	}
	store := &fakeDocumentStore{doc: doc}
	return doc, store, cuts, seoDocWorks()
}

// seoDocWorks повторяет контракт настоящего WorkRepository: отсутствие тома
// приходит ОШИБКОЙ с суффиксом "not found", а не парой (nil, nil) —
// подделка, отдающая nil без ошибки, отдаёт форму, которой репозиторий не
// возвращает, и зелёный тест на ней ничего не значит.
func seoDocWorks() WorkStore {
	return &fakeWorkStore{getFn: func(ctx context.Context, id int64) (*models.Work, error) {
		if id == 1 {
			return &models.Work{ID: 1, PageOffset: 232}, nil
		}
		return nil, fmt.Errorf("work not found")
	}}
}

// ─── что видит краулер ─────────────────────────────────────────────────────

// Страница краулера обязана показывать ОДОБРЕННУЮ редакцию, а не черновик:
// правка, ждущая модерации, не должна уезжать в поисковую выдачу — иначе
// модерация театр (пропустить безобидное, потом переписать). Краулер —
// посторонний, и сводит строку к одобренному тот же documentForViewer(nil, …),
// которым это решено для всех прочих читателей.
func TestSEODocumentSourceShowsApprovedEdition(t *testing.T) {
	_, store, cuts, works := seoDocFixture()
	src := NewSEODocumentSource(store, cuts, works, markdown.NewRenderer())

	page, err := src.Page(context.Background(), "чтец", "chto-delat")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if !strings.Contains(page.BodyHTML, "Одобренный текст") {
		t.Errorf("одобренная редакция не доехала до краулера:\n%s", page.BodyHTML)
	}
	if strings.Contains(page.BodyHTML, "ЧЕРНОВИК") {
		t.Errorf("черновик уехал краулеру:\n%s", page.BodyHTML)
	}
	if page.Document.Title != "Что делать?" {
		t.Errorf("заглавие черновика уехало краулеру: %q", page.Document.Title)
	}
	// Поля модерации гасятся там же: состояние проверки — не публичное
	// сведение, а краулер печатает то, что ему дали, целиком.
	if page.Document.ReviewStatus != "" {
		t.Errorf("состояние модерации уехало краулеру: %q", page.Document.ReviewStatus)
	}
	// Вклейка на месте и несёт корпус: без неё тест выше прошёл бы и на
	// пустом теле.
	if !strings.Contains(page.BodyHTML, "Ленин писал") {
		t.Errorf("вклейка не собралась:\n%s", page.BodyHTML)
	}
	if page.CutCount != 1 {
		t.Errorf("вклеек насчитано %d, ожидалась 1", page.CutCount)
	}
}

// Карточка ходит тем же путём и обязана давать то же заглавие — но БЕЗ
// сборки тела: она рисуется на каждое первое превью, а сборка стоит как
// рендер главы.
func TestSEODocumentSourceCardShowsApprovedTitleWithoutAssembling(t *testing.T) {
	_, store, cuts, works := seoDocFixture()
	src := NewSEODocumentSource(store, cuts, works, markdown.NewRenderer())

	card, err := src.Card(context.Background(), "чтец", "chto-delat")
	if err != nil {
		t.Fatalf("Card: %v", err)
	}
	if card.Document.Title != "Что делать?" {
		t.Errorf("на карточке заглавие черновика: %q", card.Document.Title)
	}
	if card.CutCount != 1 {
		t.Errorf("вклеек на карточке %d, ожидалась 1", card.CutCount)
	}
	// Признак того, что тело НЕ собиралось: полосы вклеек не читались.
	// PagesOfCut — самая дорогая часть сборки (запрос на вклейку), и её
	// отсутствие и есть разница между карточкой и страницей.
	if cuts.pagesOfCutCalls != 0 {
		t.Errorf("карточка собрала тело: PagesOfCut звали %d раз", cuts.pagesOfCutCalls)
	}
}

// Сверка с обработчиком: тело страницы краулера собирается тем же путём, что
// и /view, иначе они разойдутся молча (тот же сторож, что
// TestSEOCollectionSourceMatchesHandler).
func TestSEODocumentSourceMatchesHandler(t *testing.T) {
	_, store, cuts, works := seoDocFixture()

	// Путь API: DocumentHandler.View анонимом — тот же посторонний, что и
	// краулер.
	h := &DocumentHandler{documentRepo: store, renderer: markdown.NewRenderer(),
		cuts: cuts, works: works}
	rr := doGetVars(t, h.View, "/api/documents/чтец/chto-delat/view",
		map[string]string{"nickname": "чтец", "slug": "chto-delat"}, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("View: код %d, тело: %s", rr.Code, rr.Body.String())
	}
	var viaAPI map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &viaAPI); err != nil {
		t.Fatalf("ответ View не разобрался: %v", err)
	}
	apiHTML, _ := viaAPI["html_content"].(string)

	// Путь краулера: тот же стор, тот же адрес.
	src := NewSEODocumentSource(store, cuts, works, markdown.NewRenderer())
	page, err := src.Page(context.Background(), "чтец", "chto-delat")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}

	if apiHTML == "" {
		t.Fatal("html_content пуст — тест ничего не проверил бы")
	}
	if apiHTML != page.BodyHTML {
		t.Fatalf("сборка разошлась.\nAPI:\n%s\n\nкраулер:\n%s", apiHTML, page.BodyHTML)
	}
}

// Черновик снаружи не существует вовсе — 404 (ErrNeverPublished), а не 410:
// 410 значит «было и снято» и выдал бы сам факт существования черновика.
func TestSEODocumentSourceDraftIsNeverPublished(t *testing.T) {
	_, store, cuts, works := seoDocFixture()
	store.doc.PublishedAt = nil
	store.doc.WasPublished = false
	store.doc.PublishedTitle = ""
	store.doc.PublishedMarkdown = ""
	src := NewSEODocumentSource(store, cuts, works, markdown.NewRenderer())

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"страница", func() error {
			_, err := src.Page(context.Background(), "чтец", "chto-delat")
			return err
		}},
		{"карточка", func() error {
			_, err := src.Card(context.Background(), "чтец", "chto-delat")
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if !errors.Is(err, seo.ErrNeverPublished) {
				t.Fatalf("черновик отдан не как ErrNeverPublished: %v", err)
			}
			if errors.Is(err, seo.ErrNotFound) {
				t.Fatalf("черновик отдан как ErrNotFound (410) — выдаёт факт существования: %v", err)
			}
		})
	}
}

// Снятый с публикации — 410 (ErrNotFound): поисковик выбрасывает его сразу, а
// не перепроверяет неделями.
func TestSEODocumentSourceUnpublishedIsNotFound(t *testing.T) {
	_, store, cuts, works := seoDocFixture()
	store.doc.PublishedAt = nil
	store.doc.WasPublished = true
	src := NewSEODocumentSource(store, cuts, works, markdown.NewRenderer())

	if _, err := src.Page(context.Background(), "чтец", "chto-delat"); !errors.Is(err, seo.ErrNotFound) {
		t.Fatalf("снятый разбор отдан не как ErrNotFound: %v", err)
	}
	if _, err := src.Card(context.Background(), "чтец", "chto-delat"); !errors.Is(err, seo.ErrNotFound) {
		t.Fatalf("снятая карточка отдана не как ErrNotFound: %v", err)
	}
}

// Отсутствующий разбор доезжает до краулера 404-й веткой (ErrNotFound
// переводится writeRenderError в 410, а «формы адреса нет вовсе» — в 404 из
// match): важно, что это НЕ 500 — иначе поисковик решит, что читальня
// сломана, и оставит адрес в очереди.
func TestSEODocumentSourceTranslatesNotFound(t *testing.T) {
	_, _, cuts, works := seoDocFixture()
	// Пустой fakeDocumentStore отдаёт ровно тот текст, каким отсутствие
	// строки возвращает настоящий DocumentRepository на pgx.ErrNoRows.
	src := NewSEODocumentSource(&fakeDocumentStore{}, cuts, works, markdown.NewRenderer())

	if _, err := src.Page(context.Background(), "чтец", "net-takogo"); !errors.Is(err, seo.ErrNotFound) {
		t.Fatalf("отсутствие разбора не переведено в seo.ErrNotFound: %v", err)
	}
}

// (nil, nil) от хранилища не должен доехать до seo: Source.Document
// разыменовывает результат сразу после проверки ошибки. Тот же контракт, что
// у SEOBookSource.Chapter и SEOCollectionSource.GetByAuthorSlug.
func TestSEODocumentSourceRefusesNilNil(t *testing.T) {
	_, _, cuts, works := seoDocFixture()
	src := NewSEODocumentSource(documentNilStore{&fakeDocumentStore{}}, cuts, works, markdown.NewRenderer())

	page, err := src.Page(context.Background(), "", "pusto")
	if page != nil {
		t.Fatalf("страница не nil при (nil, nil) от хранилища: %+v", page)
	}
	if !errors.Is(err, seo.ErrNotFound) {
		t.Fatalf("(nil, nil) не превращён в seo.ErrNotFound: %v", err)
	}
}

// documentNilStore отдаёт пару (nil, nil) — форму, которой настоящий
// репозиторий не возвращает. Проверяется, что граница пакетов её не
// пропускает дальше: в internal/seo такая пара стала бы разыменованием nil.
type documentNilStore struct{ *fakeDocumentStore }

func (documentNilStore) GetByAuthorSlug(ctx context.Context, nickname, slug string) (*models.Document, error) {
	return nil, nil
}

// Черновик может ссылаться на вклейку, которой одобренная редакция не
// называет вовсе — автор завёл кусок корпуса, а модератор его ещё не видел.
// ByDocument честно отдаёт ОБЕ вклейки разбора (фикстура ниже это
// проверяет), но CutCount страницы и карточки обязан считать по телу,
// которое реально показано пришедшему (плейсхолдеры <cut id="N"> ОДОБРЕННОЙ
// редакции), а не по сырому списку хранилища — иначе счёт на карточке и
// внизу страницы был бы больше числа вклеек, которые пришедший вообще может
// увидеть.
func TestSEODocumentSourceCutCountIgnoresDraftOnlyCuts(t *testing.T) {
	cut7 := &models.DocumentCut{
		ID: 7, DocumentID: 1, WorkID: ptrInt64(1),
		Anchor: models.Anchor{StartPageID: 10, StartOffset: 0, EndPageID: 10,
			EndOffset: len(seoDocCorpus)},
		Status: models.CutStatusOK, SourceTitle: "Ленин. Что делать?",
	}
	cut8 := &models.DocumentCut{
		ID: 8, DocumentID: 1, WorkID: ptrInt64(1),
		Anchor: models.Anchor{StartPageID: 11, StartOffset: 0, EndPageID: 11, EndOffset: 10},
		Status: models.CutStatusOK, SourceTitle: "Ленин. Ещё не одобрено.",
	}
	cuts := newFakeDocumentCutStore(cut7, cut8)
	cuts.pagesByCutID = map[int64][]*models.Page{
		7: {cutPage(10, 4, seoDocCorpus)},
		8: {cutPage(11, 5, "запасной кусок, который ещё не одобрили")},
	}

	published := time.Unix(1_700_000_000, 0)
	// MarkdownContent (черновик) называет и 7, и 8; PublishedMarkdown
	// (одобрено) — только 7. Это ровно случай, где ByDocument (2 вклейки) и
	// плейсхолдеры показанного тела (1 вклейка) расходятся.
	draftWithExtraCut := seoDocDraft + "\n<cut id=\"8\">\n"
	doc := &models.Document{
		ID: 1, Slug: "chto-delat", AuthorNickname: "чтец", OwnerID: ptrInt64(5),
		Title: "Черновое заглавие", MarkdownContent: draftWithExtraCut,
		PublishedTitle: "Что делать?", PublishedMarkdown: seoDocApproved,
		PublishedAt: &published, WasPublished: true,
		ReviewStatus: models.DocumentPending,
		UpdatedAt:    time.Unix(1_700_000_500, 0),
	}
	store := &fakeDocumentStore{doc: doc}
	works := seoDocWorks()
	src := NewSEODocumentSource(store, cuts, works, markdown.NewRenderer())

	// Фикстура настоящая, не куцая: сырое хранилище видит ОБЕ вклейки.
	all, err := cuts.ByDocument(context.Background(), 1)
	if err != nil || len(all) != 2 {
		t.Fatalf("фикстура сломана: ByDocument вернул %d вклеек, %v", len(all), err)
	}

	page, err := src.Page(context.Background(), "чтец", "chto-delat")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.CutCount != 1 {
		t.Errorf("страница насчитала %d вклеек, ожидалась 1 (вклейка черновика не входит в одобренную редакцию)", page.CutCount)
	}

	card, err := src.Card(context.Background(), "чтец", "chto-delat")
	if err != nil {
		t.Fatalf("Card: %v", err)
	}
	if card.CutCount != 1 {
		t.Errorf("карточка насчитала %d вклеек, ожидалась 1", card.CutCount)
	}
}
