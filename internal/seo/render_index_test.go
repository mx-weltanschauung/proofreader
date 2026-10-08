package seo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"proofreader/internal/site"
	"strings"
	"testing"
	"time"

	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/pkg/markdown"
)

type fakeConcepts struct {
	bySlug map[string]*models.IndexConcept
	all    []*models.IndexConcept
	// err, если задан, возвращается из GetConceptBySlug вместо чтения bySlug —
	// тем же приёмом, что и fakeWorks.err в render_volume_test.go: тесты
	// воспроизводят ровно то, что отдают настоящие репозитории — "... not
	// found" на pgx.ErrNoRows и "failed to ..." на прочих бедах.
	err error
}

func (f *fakeConcepts) GetConceptBySlug(
	ctx context.Context, slug string,
) (*models.IndexConcept, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.bySlug[slug], nil
}

func (f *fakeConcepts) ListConcepts(
	ctx context.Context, query, letter string, limit, offset int,
) ([]*models.IndexConcept, error) {
	if offset >= len(f.all) {
		return nil, nil
	}
	end := offset + limit
	if end > len(f.all) {
		end = len(f.all)
	}
	return f.all[offset:end], nil
}

type fakeCollections struct {
	bySlug map[string]*models.Collection
	// byAuthorSlug — читательские подборки, ключ "ник/слаг": слаг у них
	// уникален только в паре с ником, отдельным полем от bySlug (сотруднические,
	// пустой ник), как и у боевого репозитория (composite key).
	byAuthorSlug map[string]*models.Collection
	all          []*models.Collection
	// err — см. комментарий у fakeConcepts.err.
	err error
}

func (f *fakeCollections) GetBySlug(ctx context.Context, slug string) (*models.Collection, error) {
	return f.GetByAuthorSlug(ctx, "", slug)
}

func (f *fakeCollections) GetByAuthorSlug(ctx context.Context, nickname, slug string) (*models.Collection, error) {
	if f.err != nil {
		return nil, f.err
	}
	if nickname == "" {
		return f.bySlug[slug], nil
	}
	return f.byAuthorSlug[nickname+"/"+slug], nil
}
func (f *fakeCollections) List(ctx context.Context) ([]*models.Collection, error) {
	return f.all, nil
}

func indexSource() *Source {
	concept := &models.IndexConcept{
		ID: 1, Title: "Абстрактный труд", Slug: "abstraktnyj-trud",
		// Kind здесь — не легаси-поле статьи (то сняла миграция 000026,
		// задача 11), а сам вычисляемый агрегат ListConcepts (CASE … EXISTS
		// в listConceptsQuery): эта же фикстура отдаётся и как результат
		// ListConcepts (ConceptList читает c.Kind для отсева перенаправлений),
		// и как результат GetConceptBySlug (Concept читает только Articles и
		// это поле игнорирует) — так что заполнить его надо тем же значением,
		// какое дал бы настоящий агрегат для понятия со статьёй.
		Kind: models.IndexConceptKindArticle,
		Articles: []*models.IndexArticle{{
			Kind:            models.IndexConceptKindArticle,
			ArticleMarkdown: "Труд, лишённый конкретной формы.",
			References: []*models.IndexReference{
				{VolumeNumber: 23, PageStart: 45, PageEnd: 48, Rubric: "как субстанция стоимости"},
				{VolumeNumber: 23, PageStart: 51, PageEnd: 51},
			},
		}},
	}
	redirect := &models.IndexConcept{
		ID: 2, Title: "Труд абстрактный", Slug: "trud-abstraktnyj",
		Kind: models.IndexConceptKindRedirect,
		// У перенаправления нет ни текста статьи, ни адресов — этим оно и
		// отличается от обычного понятия.
	}
	published := time.Unix(1_700_000_000, 0)
	collection := &models.Collection{
		ID: 1, Title: "О государстве", Slug: "o-gosudarstve",
		Description: "Работы о природе государства.",
		// Сотрудническая (AuthorNickname пуст) и опубликованная — иначе
		// новый отбор черновиков (Source.Collection) её же и отсеет.
		PublishedAt: &published,
		Items: []*models.CollectionEntry{
			{ID: 1, Kind: models.CollectionItemKindChapter, Title: "Государство и революция",
				WorkAuthor: "В. И. Ленин"},
			{ID: 2, Kind: models.CollectionItemKindChapter, Title: "Утраченный источник",
				Broken: true},
		},
	}
	return &Source{
		BaseURL:  "https://lib.example.org",
		Renderer: markdown.NewRenderer(),
		Concepts: &fakeConcepts{
			bySlug: map[string]*models.IndexConcept{
				"abstraktnyj-trud": concept,
				"trud-abstraktnyj": redirect,
			},
			all: []*models.IndexConcept{concept, redirect},
		},
		Collections: &fakeCollections{
			bySlug: map[string]*models.Collection{"o-gosudarstve": collection},
			all:    []*models.Collection{collection},
		},
		Editions: &fakeEditions{
			// URLSlug заполнен — в базе он есть у всех пяти собраний
			// корпуса, фикстура без него проверяла бы то, чего не бывает.
			all:   []*models.Edition{{ID: 7, Title: "К. Маркс и Ф. Энгельс. Сочинения", URLSlug: "mae"}},
			byID:  map[int64]*models.Edition{},
			works: map[int64][]*models.Work{},
		},
		Catalog: &fakeCatalog{
			editions: []repository.CatalogRow{{ID: 7}},
			concepts: []repository.CatalogRow{{ID: 1, Slug: "abstraktnyj-trud"}},
			// AuthorNickname пуст и PublishedAt непуст — иначе publicCollectionRows
			// (sitemap.go) отсеет её, и /og/site/collections.png (siteCard)
			// станет считать ноль подборок вместо одной.
			collections: []repository.CatalogRow{{ID: 1, Slug: "o-gosudarstve", PublishedAt: &published}},
		},
	}
}

func TestConceptDocHasArticleAndAddresses(t *testing.T) {
	doc, err := indexSource().Concept(context.Background(), "abstraktnyj-trud")
	if err != nil {
		t.Fatalf("Concept: %v", err)
	}

	if !strings.Contains(doc.Body, "лишённый конкретной формы") {
		t.Errorf("в теле нет статьи:\n%s", doc.Body)
	}
	if !strings.Contains(doc.Body, "т. 23") || !strings.Contains(doc.Body, "45—48") {
		t.Errorf("в теле нет адресов:\n%s", doc.Body)
	}
	if doc.Canonical != "https://lib.example.org/concepts/abstraktnyj-trud" {
		t.Errorf("canonical: %q", doc.Canonical)
	}
	if doc.Robots != RobotsIndex {
		t.Errorf("понятие обязано индексироваться, получено %q", doc.Robots)
	}
}

// F1 итогового ревью: раньше Renderer.Render был собран с html.CompletePage
// и отдавал целый документ — DOCTYPE, <head> со своим <title> и GENERATOR,
// <body>. Вставленный прямо в тело страницы, он давал на /concepts/{slug} два
// элемента <title> (второй — пустой), а простой сборщик превью, берущий
// последнее совпадение, получал бы пустой заголовок. Обёртку теперь снимает
// сам pkg/markdown (контракт пакета, см. его докблок) на каждом публичном
// выходе; эта проверка стоит сторожем на случай, если контракт однажды
// нарушится.
func TestConceptBodyHasNoDocumentWrapper(t *testing.T) {
	doc, err := indexSource().Concept(context.Background(), "abstraktnyj-trud")
	if err != nil {
		t.Fatalf("Concept: %v", err)
	}
	assertBodyIsFragment(t, doc.Body)
}

// assertBodyIsFragment — тело страницы обязано быть содержимым <body>, а не
// целым HTML-документом: ни DOCTYPE, ни <html, ни второго <title внутри.
func assertBodyIsFragment(t *testing.T, body string) {
	t.Helper()
	lower := strings.ToLower(body)
	if strings.Contains(lower, "<!doctype") {
		t.Errorf("в теле страницы DOCTYPE — это целый документ, а не фрагмент:\n%s", body)
	}
	if strings.Contains(lower, "<html") {
		t.Errorf("в теле страницы <html> — это целый документ, а не фрагмент:\n%s", body)
	}
	if strings.Contains(lower, "<title") {
		t.Errorf("в теле страницы <title> — второй заголовок документа:\n%s", body)
	}
}

func TestConceptMissingIsNotFound(t *testing.T) {
	if _, err := indexSource().Concept(context.Background(), "net-takogo"); err == nil {
		t.Fatal("ожидалась ошибка для отсутствующего понятия")
	}
}

// Битый пункт подборки (источник удалён) в оглавлении остаётся, но ссылкой не
// становится: чтение такого пункта отвечает 410, и вести туда краулера значит
// кормить его битыми адресами.
func TestCollectionDocSkipsLinkForBrokenItem(t *testing.T) {
	doc, err := indexSource().Collection(context.Background(), "", "o-gosudarstve")
	if err != nil {
		t.Fatalf("Collection: %v", err)
	}

	if !strings.Contains(doc.Body, `href="/collections/o-gosudarstve/read/1"`) {
		t.Errorf("нет ссылки на живой пункт:\n%s", doc.Body)
	}
	if strings.Contains(doc.Body, `read/2"`) {
		t.Errorf("битый пункт стал ссылкой:\n%s", doc.Body)
	}
	if !strings.Contains(doc.Body, "Утраченный источник") {
		t.Errorf("битый пункт пропал из оглавления совсем:\n%s", doc.Body)
	}
}

// Читательская подборка открыта по прямому адресу (в отличие от черновика —
// см. TestDraftCollectionPageIsNotFoundToCrawler) и превью в мессенджере у неё
// есть (TestReaderCollectionStillHasCard, card_test.go), но в поиск она не
// должна попасть: подборки читателей не модерируются, и без noindex спам
// уехал бы прямиком в выдачу. RobotsIndex/RobotsNoIndex — это отдельный
// механизм от закрытия обхода в robots.txt (см. handler.go): здесь страница
// доступна и обходима, просто помечена не индексировать.
func TestReaderCollectionPageIsNoindex(t *testing.T) {
	s := indexSource()
	published := time.Unix(1_700_000_000, 0)
	s.Collections.(*fakeCollections).byAuthorSlug = map[string]*models.Collection{
		"chitatel/moi-glavy": {
			ID: 2, Title: "Мои любимые главы", Slug: "moi-glavy",
			AuthorNickname: "chitatel",
			PublishedAt:    &published,
			Items: []*models.CollectionEntry{
				{ID: 3, Kind: models.CollectionItemKindChapter, Title: "Тезисы о Фейербахе",
					WorkAuthor: "К. Маркс"},
			},
		},
	}

	doc, err := s.Collection(context.Background(), "chitatel", "moi-glavy")
	if err != nil {
		t.Fatalf("Collection: %v", err)
	}
	if doc.Robots != RobotsNoIndex {
		t.Errorf("читательская подборка должна отдавать noindex, получено %q", doc.Robots)
	}
	if doc.Canonical != "https://lib.example.org/collections/chitatel/moi-glavy" {
		t.Errorf("canonical не тот: %q", doc.Canonical)
	}
	if !strings.Contains(doc.Title, "чит") {
		t.Errorf("заголовок обязан постоянно нести отметку «читатель»: %q", doc.Title)
	}

	// Тот же вывод должен приходить и по полному HTTP-пути, а не только из
	// прямого вызова рендера — иначе следующая правка match() могла бы
	// разойтись с Source.Collection незамеченной.
	h := handlerFor(s)
	rec := get(t, h, "/collections/chitatel/moi-glavy", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `name="robots" content="noindex,follow"`) {
		t.Errorf("страница отдана без meta robots noindex:\n%s", rec.Body.String())
	}
}

// Зеркало TestReaderCollectionPageIsNoindex: сотрудническая подборка
// (AuthorNickname пуст) не должна ни спрятаться из индекса, ни обзавестись
// пометкой «подборка читателя» — обе ветки в Source.Collection читают одно и
// то же условие (AuthorNickname != ""), и без этого теста его инверсия
// прошла бы весь набор незамеченной (проверено мутацией: инверсия условия
// валит и этот тест, и TestReaderCollectionPageIsNoindex одновременно).
func TestStaffCollectionPageStaysIndexed(t *testing.T) {
	doc, err := indexSource().Collection(context.Background(), "", "o-gosudarstve")
	if err != nil {
		t.Fatalf("Collection: %v", err)
	}
	if doc.Robots != RobotsIndex {
		t.Errorf("сотрудническая подборка должна индексироваться, получено %q", doc.Robots)
	}
	if strings.Contains(doc.Title, "читател") {
		t.Errorf("у сотруднической подборки не должно быть пометки читателя: %q", doc.Title)
	}
	if strings.Contains(doc.Body, "Собрал читатель") {
		t.Errorf("у сотруднической подборки не должно быть строки «собрал читатель»:\n%s", doc.Body)
	}
}

// Черновик (PublishedAt == nil, PublishIPHash == "" — публикации не было ни
// разу) должен отвечать 404, а не 410: сущность не отсутствует, она
// существует и скрыта от постороннего, и 410 («было и снято») выдал бы
// краулеру сам факт существования черновика — ровно то, чего нельзя. См.
// ErrNeverPublished (doc.go) и doc-комментарий Collection.PublishIPHash в
// internal/models — зеркалит ветку internal/api/collection_handler.go (Get).
func TestDraftCollectionPageIsNotFoundToCrawler(t *testing.T) {
	s := indexSource()
	s.Collections.(*fakeCollections).bySlug["chernovik"] = &models.Collection{
		ID: 3, Title: "Неопубликованное", Slug: "chernovik",
	}

	h := handlerFor(s)
	rec := get(t, h, "/collections/chernovik", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("черновик подборки: код %d, ожидался 404", rec.Code)
	}
}

// Подборка, снятая с публикации ПОСЛЕ того, как побывала опубликованной
// (PublishedAt == nil, но PublishIPHash не пуст — отметка адреса публикации
// его не стирает), — другой случай: она была видна и её сняли, поэтому
// краулер обязан получить честный 410, ускоряющий выброс из индекса, а не
// 404 (который перепроверяется неделями). Различить эти два случая может
// только PublishIPHash — PublishedAt в обоих nil.
func TestUnpublishedCollectionPageIsGoneToCrawler(t *testing.T) {
	s := indexSource()
	s.Collections.(*fakeCollections).bySlug["snyato"] = &models.Collection{
		ID: 4, Title: "Снятое с публикации", Slug: "snyato",
		PublishIPHash: "somehash",
	}

	h := handlerFor(s)
	rec := get(t, h, "/collections/snyato", nil)
	if rec.Code != http.StatusGone {
		t.Errorf("снятая подборка: код %d, ожидался 410", rec.Code)
	}
}

// Задача 7: понятие расщеплено на понятие каталога и статьи указателей —
// сервер обязан печатать адреса ВСЕХ статей, а не только первой (прежние
// поля Kind/ArticleMarkdown/References понятия — временная совместимость до
// задачи 11, заполненная первой статьёй, и опираться на неё в новом коде
// нельзя).
func TestConceptDocListsAddressesOfEveryArticle(t *testing.T) {
	s := indexSource()
	s.Concepts.(*fakeConcepts).bySlug["trud"] = &models.IndexConcept{
		ID: 3, Title: "Труд", Slug: "trud",
		Articles: []*models.IndexArticle{
			{
				Kind:            models.IndexConceptKindArticle,
				ArticleMarkdown: "первая",
				References: []*models.IndexReference{
					{VolumeNumber: 12, PageStart: 730, PageEnd: 731, Rubric: "определение"},
				},
			},
			{
				Kind:            models.IndexConceptKindArticle,
				ArticleMarkdown: "вторая",
				References: []*models.IndexReference{
					{VolumeNumber: 33, PageStart: 12, PageEnd: 12, Rubric: "сущность"},
				},
			},
		},
	}

	doc, err := s.Concept(context.Background(), "trud")
	if err != nil {
		t.Fatalf("Concept: %v", err)
	}
	for _, want := range []string{"т. 12, с. 730—731", "т. 33, с. 12", "первая", "вторая"} {
		if !strings.Contains(doc.Body, want) {
			t.Errorf("в теле нет %q:\n%s", want, doc.Body)
		}
	}

	// Счётчик в описании — сумма адресов по ВСЕМ статьям (1 + 1 = 2), а не
	// количество адресов одной статьи (1): число нарочно отличается от
	// одиночного слагаемого, иначе регресс вида «взять только первую
	// статью» дал бы то же число и остался бы незамеченным.
	if !strings.Contains(doc.Description, "Адресов в корпусе: 2") {
		t.Errorf("в описании нет суммы адресов по всем статьям: %q", doc.Description)
	}
}

// Раунд правок 1: старое поле c.Kind (временная совместимость, заполняемая
// первой статьёй) и агрегат по c.Articles должны разойтись хотя бы в одной
// фикстуре — иначе запрещённая брифом логика («if c.Kind != article →
// noindex») давала бы тот же ответ во всём наборе и прошла бы незамеченной.
func TestConceptRobotsFollowsArticlesNotLegacyKind(t *testing.T) {
	s := indexSource()
	// Устаревшее поле понятия — redirect (старая логика сказала бы noindex),
	// но единственная статья — article: обязано индексироваться.
	s.Concepts.(*fakeConcepts).bySlug["stale-redirect"] = &models.IndexConcept{
		ID: 4, Title: "Просроченный редирект", Slug: "stale-redirect",
		Kind: models.IndexConceptKindRedirect,
		Articles: []*models.IndexArticle{
			{Kind: models.IndexConceptKindArticle, ArticleMarkdown: "текст статьи"},
		},
	}
	// Устаревшее поле понятия — article (старая логика сказала бы index), но
	// единственная статья — redirect: индексировать нечего.
	s.Concepts.(*fakeConcepts).bySlug["stale-article"] = &models.IndexConcept{
		ID: 5, Title: "Просроченная статья", Slug: "stale-article",
		Kind: models.IndexConceptKindArticle,
		Articles: []*models.IndexArticle{
			{Kind: models.IndexConceptKindRedirect},
		},
	}

	doc, err := s.Concept(context.Background(), "stale-redirect")
	if err != nil {
		t.Fatalf("Concept: %v", err)
	}
	if doc.Robots != RobotsIndex {
		t.Errorf("статья article обязана индексироваться независимо от устаревшего c.Kind, получено %q", doc.Robots)
	}

	doc, err = s.Concept(context.Background(), "stale-article")
	if err != nil {
		t.Fatalf("Concept: %v", err)
	}
	if doc.Robots != RobotsNoIndex {
		t.Errorf("понятие без единой статьи article не должно индексироваться независимо от устаревшего c.Kind, получено %q", doc.Robots)
	}
}

func TestConceptListLinksEveryConcept(t *testing.T) {
	doc, err := indexSource().ConceptList(context.Background())
	if err != nil {
		t.Fatalf("ConceptList: %v", err)
	}
	if !strings.Contains(doc.Body, `href="/concepts/abstraktnyj-trud"`) {
		t.Errorf("список не ссылается на понятие:\n%s", doc.Body)
	}
}

// Понятие-перенаправление ListConcepts отдаёт как есть (в отличие от карты
// сайта, у которой свой запрос с фильтром kind = 'article'): без отсева в
// самом рендерере список линковал бы заголовок без тела.
func TestConceptListSkipsRedirect(t *testing.T) {
	doc, err := indexSource().ConceptList(context.Background())
	if err != nil {
		t.Fatalf("ConceptList: %v", err)
	}
	if strings.Contains(doc.Body, `href="/concepts/trud-abstraktnyj"`) {
		t.Errorf("перенаправление попало в список:\n%s", doc.Body)
	}
	if strings.Contains(doc.Body, "Труд абстрактный") {
		t.Errorf("заголовок перенаправления попал в список:\n%s", doc.Body)
	}
}

// Перенаправление по прямому адресу всё равно достижимо (кто-то мог
// сохранить ссылку), поэтому Concept не 404-ит его, а отдаёт с noindex:
// страница пустая (нет ни статьи, ни адресов), индексировать её нечем.
func TestConceptRedirectIsNoIndex(t *testing.T) {
	doc, err := indexSource().Concept(context.Background(), "trud-abstraktnyj")
	if err != nil {
		t.Fatalf("Concept: %v", err)
	}
	if doc.Robots != RobotsNoIndex {
		t.Errorf("перенаправление не должно индексироваться, получено %q", doc.Robots)
	}
}

func TestConceptRepositoryNotFoundTextBecomesErrNotFound(t *testing.T) {
	s := indexSource()
	s.Concepts.(*fakeConcepts).err = fmt.Errorf("concept not found")

	_, err := s.Concept(context.Background(), "abstraktnyj-trud")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("ожидался ErrNotFound, получено: %v", err)
	}
}

// А поломка базы ("failed to ...") — это не «не найдено»: под ErrNotFound она
// уехала бы краулеру голой 404, спрятав живую аварию за неотличимым «нет
// страницы». Исходная ошибка обязана дойти наружу.
func TestConceptRepositoryFailureIsNotErrNotFound(t *testing.T) {
	s := indexSource()
	boom := errors.New("connection refused")
	s.Concepts.(*fakeConcepts).err = fmt.Errorf("failed to get concept: %w", boom)

	_, err := s.Concept(context.Background(), "abstraktnyj-trud")
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("поломка базы не должна маскироваться под ErrNotFound: %v", err)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("исходная ошибка потеряна: %v", err)
	}
}

func TestCollectionMissingIsNotFound(t *testing.T) {
	if _, err := indexSource().Collection(context.Background(), "", "net-takoj"); err == nil {
		t.Fatal("ожидалась ошибка для отсутствующей подборки")
	}
}

func TestCollectionRepositoryNotFoundTextBecomesErrNotFound(t *testing.T) {
	s := indexSource()
	s.Collections.(*fakeCollections).err = fmt.Errorf("collection not found")

	_, err := s.Collection(context.Background(), "", "o-gosudarstve")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("ожидался ErrNotFound, получено: %v", err)
	}
}

// SEOCollectionSource (internal/api) не отдаёт сырой текст репозитория —
// он заворачивает его в seo.ErrNotFound: "не найдено: подборка "..." (collection
// not found)". Такое сообщение больше не кончается на "not found" (кончается
// на закрывающую скобку), поэтому проверка isNotFound по одному хвосту текста
// на нём даёт ложь — её и чинит эта задача: isNotFound должен сперва пройти
// по цепочке errors.Is, и только потом падать на текст.
func TestCollectionWrappedErrNotFoundStillDetected(t *testing.T) {
	s := indexSource()
	s.Collections.(*fakeCollections).err = fmt.Errorf(
		`%w: подборка %q (collection not found)`, ErrNotFound, "нет-такой",
	)

	_, err := s.Collection(context.Background(), "", "нет-такой")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("завёрнутая ошибка адаптера не распознана как ErrNotFound: %v", err)
	}
}

// Прямая проверка самой isNotFound на завёрнутой ошибке — в дополнение к
// сквозным тестам выше (Source.Collection, cardInput в card_test.go): те
// проходят и на старой, текстовой-only версии isNotFound по случайности
// (fmt.Errorf("подборка %q: %w", slug, err) в ветке "не распознано как
// not found" всё равно протаскивает ErrNotFound дальше по цепочке через
// %w), так что сами по себе регресс не ловят. Эта проверка — про саму
// функцию: isNotFound обязана сказать true независимо от такой случайности
// в вызывающем коде.
func TestIsNotFoundChecksChainBeforeText(t *testing.T) {
	wrapped := fmt.Errorf(`%w: подборка %q (collection not found)`, ErrNotFound, "нет-такой")
	if !isNotFound(wrapped) {
		t.Fatalf("isNotFound не распознала завёрнутый ErrNotFound: %v", wrapped)
	}

	raw := fmt.Errorf("collection not found")
	if !isNotFound(raw) {
		t.Fatalf("isNotFound не распознала сырой текст репозитория: %v", raw)
	}

	boom := fmt.Errorf("failed to get collection: %w", errors.New("connection refused"))
	if isNotFound(boom) {
		t.Fatalf("isNotFound приняла поломку базы за отсутствие сущности: %v", boom)
	}

	if isNotFound(nil) {
		t.Fatal("isNotFound(nil) должна быть false")
	}
}

func TestCollectionRepositoryFailureIsNotErrNotFound(t *testing.T) {
	s := indexSource()
	boom := errors.New("connection refused")
	s.Collections.(*fakeCollections).err = fmt.Errorf("failed to get collection: %w", boom)

	_, err := s.Collection(context.Background(), "", "o-gosudarstve")
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("поломка базы не должна маскироваться под ErrNotFound: %v", err)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("исходная ошибка потеряна: %v", err)
	}
}

// Имя и описание — свойство экземпляра (SITE_NAME/SITE_DESCRIPTION), а не
// платформы: краулер чужой читальни не должен видеть нашего имени.
func TestHomeUsesSiteNameAndDescription(t *testing.T) {
	site.Set("Тестовая", "Тестовое собрание.")
	t.Cleanup(func() { site.Set("", "") })
	doc, err := indexSource().Home(context.Background())
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	page := Render(doc)
	for _, want := range []string{"<title>Тестовая", `content="Тестовая"`, "Тестовое собрание."} {
		if !strings.Contains(page, want) {
			t.Errorf("нет %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "Читальня") {
		t.Errorf("имя по умолчанию просочилось:\n%s", page)
	}
}

func TestHomeLinksEditions(t *testing.T) {
	doc, err := indexSource().Home(context.Background())
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if !strings.Contains(doc.Body, `href="/editions/7-mae"`) {
		t.Errorf("главная не ссылается на издание:\n%s", doc.Body)
	}
	// Без этой ссылки краулер, который не читает JS, никогда не находит
	// витрину разборов — а адреса самих разборов у него взяться неоткуда
	// иначе, чем через карту сайта, до которой он ещё не дошёл.
	if !strings.Contains(doc.Body, `href="/documents"`) {
		t.Errorf("главная не ссылается на разборы:\n%s", doc.Body)
	}
	if doc.Canonical != "https://lib.example.org/" {
		t.Errorf("canonical главной: %q", doc.Canonical)
	}
}

// Страница объявляет карточку — и карточка отдаётся. Проверка сквозная
// намеренно: превью ссылки ломается двумя независимыми способами, и порознь
// каждый выглядит исправным. Либо страница не объявляет og:image вовсе (так
// и было у корня читальни, /concepts и /collections — теги были, картинки
// нет), либо объявляет адрес, которого обработчик карточек не знает. Проверка
// «есть тег og:image» ловит первое и пропускает второе; проверка «/og/…
// отдаёт PNG» — наоборот. Ловит оба только замыкание круга: взять адрес из
// самого документа и сходить по нему.
func TestLandingPagesDeclareServableCard(t *testing.T) {
	s := indexSource()
	// Подборка в обеих формах адреса — сотрудническая (уже есть в
	// indexSource, "o-gosudarstve") и читательская (фикстура та же, что у
	// TestReaderCollectionPageIsNoindex). Обе строятся своим путём в
	// Source.Collection (imgPath: /og/collection/{слаг}.png против
	// /og/collection/{ник}/{слаг}.png) — ровно то расхождение, которое
	// closed-loop проверка и ловит: путь может разойтись с тем, что разбирает
	// card.go (parseCardPath), и порознь оба конца выглядели бы исправными.
	published := time.Unix(1_700_000_000, 0)
	s.Collections.(*fakeCollections).byAuthorSlug = map[string]*models.Collection{
		"chitatel/moi-glavy": {
			ID: 2, Title: "Мои любимые главы", Slug: "moi-glavy",
			AuthorNickname: "chitatel",
			PublishedAt:    &published,
			Items: []*models.CollectionEntry{
				{ID: 3, Kind: models.CollectionItemKindChapter, Title: "Тезисы о Фейербахе",
					WorkAuthor: "К. Маркс"},
			},
		},
	}
	// Разбор — тем же приёмом и по той же причине: сотруднический
	// (/documents/{слаг}) и читательский (/documents/{ник}/{слаг}) строят
	// og:image по-разному (documentCardPath), и closed-loop проверка обязана
	// пройти обе ветки, а не только одну.
	staffDoc := &models.Document{
		ID: 10, Slug: "o-gosudarstve-razbor", Title: "О государстве: разбор",
		PublishedAt: &published, WasPublished: true,
	}
	readerDoc := &models.Document{
		ID: 11, Slug: "chto-delat", Title: "Что делать?", AuthorNickname: "chitatel",
		PublishedAt: &published, WasPublished: true,
	}
	s.Documents = &fakeDocuments{
		pages: map[string]*DocumentPage{
			"/o-gosudarstve-razbor": {Document: staffDoc, BodyHTML: "<p>Текст.</p>"},
			"chitatel/chto-delat":   {Document: readerDoc, BodyHTML: "<p>Текст.</p>"},
		},
		list: []*models.Document{staffDoc, readerDoc},
	}
	h := handlerFor(s)

	pages := map[string]func() (*Doc, error){
		"/":            func() (*Doc, error) { return s.Home(context.Background()) },
		"/concepts":    func() (*Doc, error) { return s.ConceptList(context.Background()) },
		"/collections": func() (*Doc, error) { return s.CollectionList(context.Background()) },
		"/collections/o-gosudarstve": func() (*Doc, error) {
			return s.Collection(context.Background(), "", "o-gosudarstve")
		},
		"/collections/chitatel/moi-glavy": func() (*Doc, error) {
			return s.Collection(context.Background(), "chitatel", "moi-glavy")
		},
		"/documents": func() (*Doc, error) { return s.DocumentList(context.Background()) },
		"/documents/o-gosudarstve-razbor": func() (*Doc, error) {
			return s.Document(context.Background(), "", "o-gosudarstve-razbor")
		},
		"/documents/chitatel/chto-delat": func() (*Doc, error) {
			return s.Document(context.Background(), "chitatel", "chto-delat")
		},
	}

	for page, render := range pages {
		doc, err := render()
		if err != nil {
			t.Errorf("%s: %v", page, err)
			continue
		}
		if doc.ImageURL == "" {
			t.Errorf("%s: нет og:image — ссылка уйдёт в мессенджер без картинки", page)
			continue
		}
		path, ok := strings.CutPrefix(doc.ImageURL, s.BaseURL)
		if !ok {
			t.Errorf("%s: og:image %q не абсолютный адрес читальни", page, doc.ImageURL)
			continue
		}
		if rec := ogGet(t, h, path, nil); rec.Code != http.StatusOK {
			t.Errorf("%s: объявлена карточка %s, а обработчик отдаёт %d",
				page, doc.ImageURL, rec.Code)
		}
	}
}

func TestConceptPageLinksTextForLLM(t *testing.T) {
	src := indexSource()
	doc, err := src.Concept(context.Background(), "abstraktnyj-trud")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://lib.example.org/concepts/abstraktnyj-trud.md"
	if doc.Alternate != want || !strings.Contains(doc.Body, want) {
		t.Errorf("alternate %q, тело без ссылки на .md", doc.Alternate)
	}
	list, err := src.ConceptList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if list.Alternate != "https://lib.example.org/concepts.md" || !strings.Contains(list.Body, "https://lib.example.org/concepts.md") {
		t.Errorf("alternate списка %q, или в теле нет ссылки на .md", list.Alternate)
	}
}
