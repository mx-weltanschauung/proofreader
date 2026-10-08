package seo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/font/sfnt"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

func decodeCard(t *testing.T, in CardInput) image.Image {
	t.Helper()
	raw, err := Card(in)
	if err != nil {
		t.Fatalf("Card: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("карточка не разбирается как PNG: %v", err)
	}
	return img
}

func TestCardHasDeclaredSize(t *testing.T) {
	img := decodeCard(t, CardInput{Title: "Государство и революция"})

	b := img.Bounds()
	if b.Dx() != cardWidth || b.Dy() != cardHeight {
		t.Errorf("размер %dx%d, а в og:image:width/height заявлено %dx%d",
			b.Dx(), b.Dy(), cardWidth, cardHeight)
	}
}

// Главная проверка после подрезки шрифта: если кириллица из TTF выпала, текст
// нарисуется пустыми прямоугольниками или не нарисуется вовсе, и карточка
// молча станет однотонной заливкой.
func TestCardActuallyDrawsCyrillic(t *testing.T) {
	blank := decodeCard(t, CardInput{})
	filled := decodeCard(t, CardInput{Title: "Материализм и эмпириокритицизм"})

	if inkPixels(filled) <= inkPixels(blank) {
		t.Error("кириллический заголовок не нарисовался: чернил на карточке не больше, чем на пустой")
	}
}

// Длинное название не должно уезжать за край: кегль подбирается так, чтобы
// строки уместились.
func TestCardLongTitleStaysInside(t *testing.T) {
	long := "Материализм и эмпириокритицизм. Критические заметки об одной " +
		"реакционной философии, с приложением статей и примечаний редакции"
	img := decodeCard(t, CardInput{Title: long, Subtitle: "В. И. Ленин",
		Footer: "Полное собрание сочинений, том 18"})

	// Правая и нижняя кромки шириной в отступ обязаны остаться чистыми.
	if n := inkPixels(cropRight(img, cardPad/2)); n > 0 {
		t.Errorf("текст уехал за правый край: %d непустых точек", n)
	}
	if n := inkPixels(cropBottom(img, cardPad/4)); n > 0 {
		t.Errorf("текст уехал за нижний край: %d непустых точек", n)
	}
}

// inkPixels считает точки, отличные от фона карточки.
func inkPixels(img image.Image) int {
	n := 0
	b := img.Bounds()
	wantR, wantG, wantB, _ := cardPaper.RGBA()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r != wantR || g != wantG || bl != wantB {
				n++
			}
		}
	}
	return n
}

func cropRight(img image.Image, width int) image.Image {
	b := img.Bounds()
	return img.(interface {
		SubImage(image.Rectangle) image.Image
	}).SubImage(image.Rect(b.Max.X-width, b.Min.Y, b.Max.X, b.Max.Y))
}

// cropBottom берёт нижнюю кромку, начиная от левого отступа: полоса-корешок
// идёт во всю высоту карточки, и без этого сдвига «чистая кромка» никогда бы
// не была чистой.
func cropBottom(img image.Image, height int) image.Image {
	b := img.Bounds()
	return img.(interface {
		SubImage(image.Rectangle) image.Image
	}).SubImage(image.Rect(b.Min.X+cardPad, b.Max.Y-height, b.Max.X, b.Max.Y))
}

// TestCardDump рисует карточки в файлы, чтобы посмотреть на них глазами.
// Обычным прогоном пропускается: автоматически «красиво» не проверить, а
// Телеграм кэширует превью по адресу и сам его не обновляет — неудачная
// карточка останется на всех уже разосланных ссылках.
//
//	SEO_CARD_DUMP=/tmp/cards go test ./internal/seo/ -run TestCardDump
func TestCardDump(t *testing.T) {
	dir := os.Getenv("SEO_CARD_DUMP")
	if dir == "" {
		t.Skip("задайте SEO_CARD_DUMP=<каталог>, чтобы получить PNG для просмотра")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("не удалось создать каталог: %v", err)
	}

	samples := map[string]CardInput{
		"work": {Title: "Полное собрание сочинений. Том 42",
			Subtitle: "В. И. Ленин", Footer: "Полное собрание сочинений"},
		"chapter-short": {Title: "Апрельские тезисы",
			Subtitle: "В. И. Ленин", Footer: "Том 31, с. 103—112"},
		"chapter-long": {Title: "Материализм и эмпириокритицизм. Критические заметки об одной реакционной философии",
			Subtitle: "В. И. Ленин", Footer: "Том 18"},
		"concept": {Title: "Абстрактный труд",
			Subtitle: "Предметный указатель", Footer: "Адресов в корпусе: 24"},
		"collection": {Title: "О государстве",
			Subtitle: "Подборка", Footer: "Пунктов: 7"},
		// Диапазон подрезки шрифта расширен именно ради «» — без образца с
		// ними в глазах дефект (пустые квадраты) остался бы незамеченным.
		"quotes": {Title: `«Левые» о войне`, Subtitle: "В. И. Ленин", Footer: "Том 31"},
		"site-home": {Title: "Читальня",
			Subtitle: "Собрания сочинений постранично", Footer: "Собраний сочинений: 4"},
		"site-concepts": {Title: "Предметный указатель",
			Subtitle: "Понятия корпуса с адресами в томах", Footer: "Понятий: 128"},
	}
	for name, in := range samples {
		raw, err := Card(in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		path := filepath.Join(dir, name+".png")
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		t.Logf("написано %s (%d КБ)", path, len(raw)/1024)
	}
}

// cardHTTPSource — по одной живой записи каждого вида, которые понимает
// OGCard/parseCardPath/cardInput. Отдельный набор фикстур от
// volumeSource/textSource/indexSource: тем нужны свои сочетания полей, а тут
// нужно ровно по одной записи на все пять видов карточек сразу.
func cardHTTPSource() *Source {
	editionID := int64(7)
	stamp := time.Unix(1_700_000_000, 0)
	return &Source{
		BaseURL: "https://lib.example.org",
		Works: &fakeWorks{byID: map[int64]*models.Work{
			1: {ID: 1, Title: "Полное собрание сочинений. Том 42",
				Author: "В. И. Ленин", EditionID: &editionID,
				Role: models.WorkRoleVolume, UpdatedAt: stamp},
		}},
		Chapters: &fakeChapters{byID: map[int64]*models.Chapter{
			10: {ID: 10, WorkID: 1, Title: "Государство и революция", UpdatedAt: stamp},
		}},
		Editions: &fakeEditions{
			byID:  map[int64]*models.Edition{7: {ID: 7, Title: "В. И. Ленин. ПСС", UpdatedAt: stamp}},
			works: map[int64][]*models.Work{},
		},
		Concepts: &fakeConcepts{bySlug: map[string]*models.IndexConcept{
			"abstraktnyj-trud": {ID: 1, Title: "Абстрактный труд", Slug: "abstraktnyj-trud", UpdatedAt: stamp},
		}},
		Collections: &fakeCollections{bySlug: map[string]*models.Collection{
			// Опубликована — иначе cardInput("collection", ...) теперь считает
			// её черновиком и отвечает 404 вместо карточки.
			"o-gosudarstve": {ID: 1, Title: "О государстве", Slug: "o-gosudarstve", UpdatedAt: stamp, PublishedAt: &stamp},
		}},
		// Карточкам страниц-списков нужен каталог: счёт они берут оттуда, а не
		// из списков самих страниц, у которых стоит потолок.
		Catalog: &fakeCatalog{
			editions: []repository.CatalogRow{{ID: 7, UpdatedAt: stamp}},
			concepts: []repository.CatalogRow{{ID: 1, Slug: "abstraktnyj-trud", UpdatedAt: stamp}},
			// AuthorNickname пуст, PublishedAt непуст — публичная, иначе
			// publicCollectionRows отсеет её и /og/site/collections.png
			// станет считать ноль.
			collections: []repository.CatalogRow{{ID: 1, Slug: "o-gosudarstve", UpdatedAt: stamp, PublishedAt: &stamp}},
			// AuthorNickname непуст — читательский разбор считается наравне с
			// сотрудническим (isPublicDocumentRow), в отличие от подборки выше.
			documents: []repository.CatalogRow{{Slug: "chto-delat", AuthorNickname: "чтец", UpdatedAt: stamp, PublishedAt: &stamp}},
		},
	}
}

func ogGet(t *testing.T, h *Handler, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.OGCard(rec, req)
	return rec
}

// Мусорный вид — регрессия задачи 8 была именно на непокрытом разборе
// адреса, так что это не украшение покрытия, а прямая проверка того же
// класса ошибки.
func TestOGCardUnknownKindIs404(t *testing.T) {
	rec := ogGet(t, handlerFor(cardHTTPSource()), "/og/nonsense/1.png", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("код %d, ожидался 404", rec.Code)
	}
}

func TestOGCardNonNumericIDIs404(t *testing.T) {
	for _, path := range []string{"/og/work/abc.png", "/og/chapter/abc.png", "/og/edition/abc.png"} {
		if rec := ogGet(t, handlerFor(cardHTTPSource()), path, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: код %d, ожидался 404", path, rec.Code)
		}
	}
}

func TestOGCardMissingSlugIs404(t *testing.T) {
	for _, path := range []string{"/og/concept/net-takogo.png", "/og/collection/net-takoj.png"} {
		if rec := ogGet(t, handlerFor(cardHTTPSource()), path, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: код %d, ожидался 404", path, rec.Code)
		}
	}
}

// Тот же разрыв, что чинится в isNotFound (см. render_index_test.go,
// TestCollectionWrappedErrNotFoundStillDetected), но на пути карточки:
// cardInput("collection", ...) должен дать ErrNotFound и на завёрнутой
// ошибке SEOCollectionSource, а не только на сыром тексте репозитория.
func TestCardInputCollectionWrappedErrNotFoundStillDetected(t *testing.T) {
	s := cardHTTPSource()
	s.Collections.(*fakeCollections).err = fmt.Errorf(
		`%w: подборка %q (collection not found)`, ErrNotFound, "нет-такой",
	)

	_, _, err := s.cardInput(context.Background(), "collection", "нет-такой")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("завёрнутая ошибка адаптера не распознана как ErrNotFound: %v", err)
	}
}

// Задача 7, раунд правок 1: подпись карточки понятия — сумма адресов по ВСЕМ
// статьям, а не длина c.References (поле совместимости, заполненное только
// первой статьёй). Числа подобраны так, чтобы сумма (1 + 2 = 3) отличалась и
// от количества адресов одной статьи, и от числа статей — иначе регресс вида
// «взять len(c.References)» (осталось бы 0, фикстура его не заполняет) или
// «взять только первую статью» (осталось бы 1) прошёл бы тем же значением.
func TestCardInputConceptSumsAddressesAcrossArticles(t *testing.T) {
	s := cardHTTPSource()
	s.Concepts.(*fakeConcepts).bySlug["trud"] = &models.IndexConcept{
		ID: 3, Title: "Труд", Slug: "trud",
		Articles: []*models.IndexArticle{
			{ArticleMarkdown: "первая", References: []*models.IndexReference{
				{VolumeNumber: 12, PageStart: 1, PageEnd: 1},
			}},
			{ArticleMarkdown: "вторая", References: []*models.IndexReference{
				{VolumeNumber: 33, PageStart: 2, PageEnd: 2},
				{VolumeNumber: 34, PageStart: 3, PageEnd: 3},
			}},
		},
	}

	in, _, err := s.cardInput(context.Background(), "concept", "trud")
	if err != nil {
		t.Fatalf("cardInput: %v", err)
	}
	if in.Footer != "Адресов в корпусе: 3" {
		t.Errorf("подпись карточки: %q, ожидалось «Адресов в корпусе: 3»", in.Footer)
	}
}

// Задача 7, раунд правок 1 (важное замечание рецензии): siteCard("documents")
// считает через publicDocumentRows/isPublicDocumentRow, но до этого теста ни
// один тест не проверял РЕЗУЛЬТАТ фильтра — только форму ответа (HTTP 200,
// тип содержимого, нижняя граница размера в TestOGCardSiteKindsServePNG),
// которая осталась бы неизменной, даже если фильтр всегда возвращал бы true
// (черновики считались бы), всегда false (не считалось бы ничего) или
// скопировал бы проверку isPublicCollectionRow (author_nickname == "") —
// а последнее и есть та самая асимметрия с подборкой, которую решение
// владельца запрещает.
//
// Фикстура — СВОЯ, не cardHTTPSource(): та уже используется десятком других
// тестов карточек, и её собственная строка documents (одна, читательская)
// не может отличить «черновик не считается» от «читательский считается
// наравне» — для этого нужны все три случая разом, а расширять общую
// фикстуру третьей строкой значило бы менять то, что уже проверяют другие
// тесты.
func TestSiteCardCountsOnlyPublicDocuments(t *testing.T) {
	stamp := time.Unix(1_700_000_000, 0)
	newer := time.Unix(1_700_000_500, 0)
	s := &Source{
		BaseURL: "https://lib.example.org",
		Catalog: &fakeCatalog{documents: []repository.CatalogRow{
			// Черновик — published_at пуст, не считается никем.
			{Slug: "chernovik", UpdatedAt: stamp},
			// Сотруднический, опубликован — считается.
			{Slug: "o-gosudarstve", UpdatedAt: stamp, PublishedAt: &stamp},
			// Читательский, опубликован — считается НАРАВНЕ с сотрудническим
			// (в отличие от подборки, где author_nickname непустой отсеивал
			// бы строку). Самая свежая отметка — у этой строки, чтобы
			// проверить и её, не только счёт.
			{Slug: "chto-delat", AuthorNickname: "чтец", UpdatedAt: newer, PublishedAt: &newer},
		}},
	}

	in, stampGot, err := s.cardInput(context.Background(), "site", "documents")
	if err != nil {
		t.Fatalf("cardInput: %v", err)
	}
	if in.Footer != "Разборов: 2" {
		t.Fatalf("подпись карточки витрины: %q, ожидалось «Разборов: 2» (черновик не считается, читательский считается наравне с сотрудническим)", in.Footer)
	}
	if !stampGot.Equal(newer) {
		t.Errorf("метка времени карточки: %v, ожидалась метка самой свежей публичной строки %v", stampGot, newer)
	}
}

// Читательская подборка не индексируется (TestReaderCollectionPageIsNoindex)
// и не входит в карту сайта (TestSitemapSkipsReaderCollections), но ссылка,
// разосланная в мессенджер, обязана развернуться превью — иначе единственный
// канал, которым читатель вообще может поделиться подборкой, окажется
// голым. Подпись обязана постоянно нести «собрал читатель», а не только на
// странице: это то же требование задачи 13, что и в render_index.go.
func TestReaderCollectionStillHasCard(t *testing.T) {
	s := cardHTTPSource()
	published := time.Unix(1_700_000_000, 0)
	s.Collections.(*fakeCollections).byAuthorSlug = map[string]*models.Collection{
		"chitatel/moi-glavy": {
			ID: 2, Title: "Мои любимые главы", Slug: "moi-glavy",
			AuthorNickname: "chitatel",
			PublishedAt:    &published,
			Items: []*models.CollectionEntry{
				{ID: 3, Kind: models.CollectionItemKindChapter, Title: "Тезисы о Фейербахе"},
			},
		},
	}

	in, _, err := s.cardInput(context.Background(), "collection", "chitatel/moi-glavy")
	if err != nil {
		t.Fatalf("cardInput: %v", err)
	}
	if !strings.Contains(in.Subtitle, "собрал читатель chitatel") {
		t.Errorf("подпись карточки не несёт отметку «собрал читатель»: %q", in.Subtitle)
	}

	h := handlerFor(s)
	rec := ogGet(t, h, "/og/collection/chitatel/moi-glavy.png", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type: %q", ct)
	}
	if rec.Body.Len() == 0 {
		t.Error("пустое тело")
	}
}

func TestOGCardServesPNG(t *testing.T) {
	rec := ogGet(t, handlerFor(cardHTTPSource()), "/og/work/1.png", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type: %q", ct)
	}
	if rec.Body.Len() == 0 {
		t.Error("пустое тело")
	}
}

// Повторный обход не должен пересобирать карточку: ETag и 304 — тот же
// приём, что и у страниц (см. TestHandlerAnswers304OnMatchingETag).
func TestOGCardAnswers304OnMatchingETag(t *testing.T) {
	h := handlerFor(cardHTTPSource())

	first := ogGet(t, h, "/og/work/1.png", nil)
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("ETag не выставлен")
	}

	second := ogGet(t, h, "/og/work/1.png", map[string]string{"If-None-Match": etag})
	if second.Code != http.StatusNotModified {
		t.Errorf("код %d, ожидался 304", second.Code)
	}
	if second.Body.Len() != 0 {
		t.Errorf("при 304 тело должно быть пустым, получено %d байт", second.Body.Len())
	}
}

// Диапазон подрезки шрифта — рукописный список, и любая его правка способна
// молча выбросить знак, который в корпусе на каждой странице. Отсутствующий
// глиф рисуется пустым квадратом, поэтому «чернил стало больше» тут ничего не
// доказывает (см. TestCardActuallyDrawsCyrillic) — проверяем наличие глифа
// прямо через GlyphAdvance.
func TestCardFontsCoverCorpusRunes(t *testing.T) {
	if err := loadFonts(); err != nil {
		t.Fatalf("шрифты не загрузились: %v", err)
	}
	const required = "АБВГДЕЁЖЗИЙКЛМНОПРСТУФХЦЧШЩЪЫЬЭЮЯабвгдеёжзийклмнопрстуфхцчшщъыьэюя" +
		"«»„“”—–…№§IVXLC0123456789.,;:!?()[]-"
	for _, f := range []*sfnt.Font{fontRegular, fontBold} {
		face, err := newFace(f, 48)
		if err != nil {
			t.Fatalf("не удалось построить начертание: %v", err)
		}
		for _, r := range required {
			if _, ok := face.GlyphAdvance(r); !ok {
				t.Errorf("в шрифте нет глифа для %q (U+%04X)", r, r)
			}
		}
	}
}

// Карточки страниц-списков: три адреса, и ровно три. Вид «site» появился
// последним и легко выпадает из проверок — а именно на нём держится превью
// корня читальни, самой пересылаемой ссылки.
func TestOGCardSiteKindsServePNG(t *testing.T) {
	for _, id := range []string{"home", "concepts", "collections", "documents"} {
		path := "/og/site/" + id + ".png"
		rec := ogGet(t, handlerFor(cardHTTPSource()), path, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: код %d, ожидался 200", path, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
			t.Errorf("%s: Content-Type %q, ожидался image/png", path, ct)
		}
		if n := rec.Body.Len(); n < 1024 {
			t.Errorf("%s: тело %d байт — на карточку не похоже", path, n)
		}
	}
}

// Алиасов у «site» нет по той же причине, что и у видов карточек: чужой кэш
// держит картинку годами, и второй адрес одной карточки означает две её
// версии, которые никогда не сойдутся.
func TestOGCardUnknownSiteIDIs404(t *testing.T) {
	for _, path := range []string{
		"/og/site/nonsense.png",
		"/og/site/Home.png",    // регистр
		"/og/site/concept.png", // единственное число вместо множественного
		"/og/site/collection.png",
		"/og/site/document.png", // единственное число разбора вместо множественного
	} {
		if rec := ogGet(t, handlerFor(cardHTTPSource()), path, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: код %d, ожидался 404", path, rec.Code)
		}
	}
}

// ─── задача 7: карточка разбора ────────────────────────────────────────────

// Верна дословно из брифа задачи 7: parseCardPath обязан принимать
// читательский адрес разбора (ник/слаг) как id одной строкой, тем же
// приёмом, что и у читательской подборки.
func TestCardPathAcceptsReaderDocument(t *testing.T) {
	kind, id, ok := parseCardPath("/og/document/chitatel/chto-delat.png")
	if !ok || kind != "document" || id != "chitatel/chto-delat" {
		t.Fatalf("разбор адреса карточки: %q %q %v", kind, id, ok)
	}
}

// Черновик — «ни страницы, ни карточки»: страница разбора уже отвечает так
// черновику (seo.ErrNeverPublished из shown/documentForViewer), и карточка
// обязана соблюдать то же правило, а не выдать превью, которое подтвердило
// бы существование ещё не одобренного текста. Тот же довод и тот же тест по
// форме, что у TestReaderCollectionStillHasCard — только в обратную сторону:
// там карточка ЕСТЬ у опубликованного, здесь её обязано НЕ быть у черновика.
func TestDocumentCardRefusesDraft(t *testing.T) {
	s := cardHTTPSource()
	fd := &fakeDocuments{
		err: fmt.Errorf("%w: разбор %q", ErrNeverPublished, "chernovik"),
	}
	s.Documents = fd

	_, _, err := s.cardInput(context.Background(), "document", "чтец/chernovik")
	if !isNotFound(err) {
		t.Fatalf("cardInput не отдал ErrNotFound на черновик: %v", err)
	}
	// Не просто «код ответа 404»: cardInput отвечает тем же кодом и на
	// неизвестный вид карточки, минуя источник разбора вовсе (см. ветку
	// notFound в конце cardInput) — без этой проверки тест остался бы
	// зелёным, даже если case "document" исчезнет из cardInput целиком.
	if fd.cardCalls != 1 {
		t.Fatalf("cardInput не дошёл до Documents.Card: cardCalls=%d", fd.cardCalls)
	}

	rec := ogGet(t, handlerFor(s), "/og/document/чтец/chernovik.png", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("код %d, ожидался 404", rec.Code)
	}
	if rec.Body.Len() > 0 && rec.Header().Get("Content-Type") == "image/png" {
		t.Error("черновику отдана картинка")
	}
}

// Заглавие разбора пишет вошедший читатель и может доходить до потолка формы
// (documentTitleMaxRunes = 500, internal/api/document_handler.go) — карточка
// обязана обрезать его вглубь холста тем же fitTitle/wrapWords, что и у
// длинного заглавия тома (TestCardLongTitleStaysInside), а не напечатать
// не поместившийся хвост поверх подписи или за краем. Слово, а не одна
// огромная строка без пробелов: wrapWords переносит по словам, и не влезающее
// целиком слово остаётся как есть — реалистичное заглавие читателя пробелы
// несёт всегда.
func TestDocumentCardTruncatesLongAuthorTitle(t *testing.T) {
	long := strings.Repeat("длинное ", 63) // 504 руны — выше потолка формы в 500
	if n := len([]rune(long)); n < 500 {
		t.Fatalf("фикстура короче потолка формы: %d рун", n)
	}
	published := time.Unix(1_700_000_000, 0)
	doc := &models.Document{
		ID: 9, Slug: "dlinnoe", AuthorNickname: "чтец",
		Title: long, PublishedAt: &published, WasPublished: true,
		UpdatedAt: published,
	}
	s := cardHTTPSource()
	s.Documents = &fakeDocuments{
		pages: map[string]*DocumentPage{
			"чтец/dlinnoe": {Document: doc, CutCount: 0},
		},
	}

	in, _, err := s.cardInput(context.Background(), "document", "чтец/dlinnoe")
	if err != nil {
		t.Fatalf("cardInput: %v", err)
	}

	img := decodeCard(t, in)
	if n := inkPixels(cropRight(img, cardPad/2)); n > 0 {
		t.Errorf("заглавие уехало за правый край: %d непустых точек", n)
	}
	if n := inkPixels(cropBottom(img, cardPad/4)); n > 0 {
		t.Errorf("заглавие уехало за нижний край: %d непустых точек", n)
	}
}
