package seo

import (
	"context"
	"proofreader/internal/site"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"proofreader/internal/models"
	"proofreader/pkg/book"
	"proofreader/pkg/markdown"
)

type fakeBooks struct {
	byChapter map[int64]*book.Book
	err       error

	// calls и delay нужны только TestHandlerDedupesConcurrentIdenticalRenders
	// в handler_test.go: посчитать реальные обращения к источнику и растянуть
	// рендер, чтобы второй запрос гарантированно застал первый в полёте.
	// Нулевые по умолчанию — на остальные тесты не влияют.
	calls int32
	delay time.Duration
}

func (f *fakeBooks) Chapter(ctx context.Context, workID, chapterID int64) (*book.Book, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.delay > 0 {
		// select, а не голый time.Sleep: настоящий репозиторий (pgx) обрывает
		// запрос по отмене контекста, а не ждёт таймер — это и воспроизводит
		// F5 итогового ревью (TestHandlerLeaderCancellationDoesNotFailFollowers
		// в handler_test.go). Пятимиллисекундная пауза после Done — не
		// мгновенный обрыв, а имитация задержки настоящего драйвера, чтобы
		// тест не зависел от гонки между этим select и удалением полёта из
		// карты в handler.go.
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			time.Sleep(5 * time.Millisecond)
			return nil, ctx.Err()
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	b, ok := f.byChapter[chapterID]
	if !ok {
		return nil, notFound("глава %d", chapterID)
	}
	return b, nil
}

type fakePages struct {
	byNumber map[int]*models.Page
}

func (f *fakePages) GetByWorkAndPageNumber(
	ctx context.Context, workID int64, pageNumber int,
) (*models.Page, error) {
	return f.byNumber[pageNumber], nil
}

func textSource() *Source {
	return &Source{
		BaseURL:  "https://lib.example.org",
		Renderer: markdown.NewRenderer(),
		Works: &fakeWorks{byID: map[int64]*models.Work{
			// Slug несёт настоящий репозиторий (задача 4) — фикстура без
			// него проверяла бы то, чего в жизни не бывает.
			1: {ID: 1, Title: "Том 42", Author: "В. И. Ленин", Role: models.WorkRoleVolume, Slug: "lenin-t42"},
			// PageOffset ненулевой: печатный номер (то, что видит читатель на
			// скане) расходится с внутренним pageNumber. Самый повторяющийся
			// класс дефектов проекта — держать фикстуру, где это расхождение
			// реально проверяется, а не всегда совпадает по случайности нуля.
			2: {ID: 2, Title: "Том 7", Author: "Г. В. Плеханов", Role: models.WorkRoleVolume,
				PageOffset: 100, Slug: "plehanov-t07"},
		}},
		Chapters: &fakeChapters{
			byID: map[int64]*models.Chapter{
				// Slug — pkg/slug.Chapter("Государство и революция"), не на
				// глаз: посчитан настоящим пакетом (задача 8, ревизия).
				10: {ID: 10, WorkID: 1, Title: "Государство и революция", StartPage: 5, EndPage: 6,
					Slug: "gosudarstvo-i-revolyuciya"},
				// Без этой строки Chapter() (render_text.go) паникует на ch.Slug:
				// её читает и глава без автора (TestChapterWithoutAuthorOmitsJSONLDAuthorKey).
				11: {ID: 11, WorkID: 1, Title: "Передовая статья", StartPage: 1, EndPage: 1,
					Slug: "peredovaya-statya"},
				// Глава-нумератор: слага не бывает вовсе (isEnumerator в
				// pkg/slug), таких в корпусе около семисот. Канон обязан
				// остаться голым номером — без висячего дефиса.
				12: {ID: 12, WorkID: 1, Title: "12", StartPage: 7, EndPage: 7},
			},
			trees: map[int64][]*models.Chapter{},
			// Полосу 5 накрывает глава 10 (стр. 5—6) — так это и считает
			// FindByPage по диапазону. Полосу 9 не накрывает ничто: дыра
			// между работами тома, штатный случай, canonical откатывается на
			// карточку тома.
			covering: map[coverKey]*models.Chapter{
				{1, 5}: {ID: 10, WorkID: 1, Title: "Государство и революция", StartPage: 5, EndPage: 6,
					Slug: "gosudarstvo-i-revolyuciya"},
			},
		},
		Pages: &fakePages{byNumber: map[int]*models.Page{
			// ChapterID не заполнен — и это не упрощение фикстуры, а корпус:
			// конвейер поле не пишет ни одной полосе, главы живут диапазонами
			// start_page/end_page. Прежняя фикстура ставила его руками, и
			// проверка canonical была зелёной на боевом, отдающем том.
			5: {ID: 50, WorkID: 1, PageNumber: 5, ContentMarkdown: "Классовое общество и государство.",
				Status: models.PageStatusProofread},
			9: {ID: 54, WorkID: 1, PageNumber: 9, ContentMarkdown: "Полоса без главы.",
				Status: models.PageStatusProofread},
			3: {ID: 30, WorkID: 2, PageNumber: 3, ContentMarkdown: "Полоса тома со смещением.",
				Status: models.PageStatusProofread},
		}},
		Books: &fakeBooks{byChapter: map[int64]*book.Book{
			10: {
				Meta: book.Meta{
					Title: "Государство и революция", Authors: []string{"В. И. Ленин"},
					Edition: "Полное собрание сочинений", Volume: "Том 42",
					CacheKey: time.Unix(1_700_000_000, 0),
				},
				Sections: []book.Section{{
					Title: "Государство и революция",
					Blocks: []book.Block{{Pages: []book.Page{
						{Internal: 5, Printed: 5, HTML: "<p>Классовое общество и государство.</p>",
							Markdown: "Классовое общество и государство."},
					}}},
				}},
			},
			// Без автора — для TestChapterWithoutAuthorOmitsJSONLDAuthorKey:
			// коллективный труд или статья без указанного автора.
			11: {
				Meta: book.Meta{
					Title: "Передовая статья", CacheKey: time.Unix(1_700_000_001, 0),
				},
				Sections: []book.Section{{
					Title: "Передовая статья",
					Blocks: []book.Block{{Pages: []book.Page{
						{Internal: 1, Printed: 1, HTML: "<p>Текст без автора.</p>"},
					}}},
				}},
			},
			// Для TestChapterCanonicalKeepsBareNumberForEnumerator — глава-
			// нумератор без слага.
			12: {
				Meta: book.Meta{
					Title: "12", CacheKey: time.Unix(1_700_000_002, 0),
				},
				Sections: []book.Section{{
					Title: "12",
					Blocks: []book.Block{{Pages: []book.Page{
						{Internal: 7, Printed: 7, HTML: "<p>Текст главы-нумератора.</p>"},
					}}},
				}},
			},
		}},
	}
}

// Глава — единственная страница с настоящим текстом. Тело берётся из
// book.BodyHTML, той же функции, что печатает скачиваемый файл.
func TestChapterDocCarriesFullText(t *testing.T) {
	doc, err := textSource().Chapter(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("Chapter: %v", err)
	}

	if !strings.Contains(doc.Body, "Классовое общество и государство") {
		t.Errorf("в теле нет текста главы:\n%s", doc.Body)
	}
	// Различающее — название главы — вперёд, автор следом; та же причина и та
	// же продублированная строка во фронтовом тесте useDocumentTitle.test.ts,
	// что и у карточки тома (render_volume_test.go).
	wantTitle := "Государство и революция — В. И. Ленин — " + site.Name()
	if doc.Title != wantTitle {
		t.Errorf("заголовок: %q, ожидалось %q", doc.Title, wantTitle)
	}
	if doc.Robots != RobotsIndex {
		t.Errorf("глава обязана индексироваться, получено %q", doc.Robots)
	}
	if doc.OGType != "article" {
		t.Errorf("тип главы: %q", doc.OGType)
	}
	// Канон со слагом и тома, и главы — задача 8.
	if doc.Canonical != "https://lib.example.org/works/1-lenin-t42/chapters/10-gosudarstvo-i-revolyuciya" {
		t.Errorf("canonical: %q", doc.Canonical)
	}
	// Описание — из текста, не из титульного блока: в титуле выходные данные,
	// а поисковик показывает описание под ссылкой.
	if !strings.Contains(doc.Description, "Классовое общество") {
		t.Errorf("описание собрано не из текста: %q", doc.Description)
	}
	if doc.CacheKey.Unix() != 1_700_000_000 {
		t.Errorf("метка кэша не взята из книги: %v", doc.CacheKey)
	}
}

// Три дешёвые мелочи итогового ревью: пустой ключ "author" в JSON-LD — ошибка
// проверки в Яндекс.Вебмастере. У главы без автора ключа быть не должно
// вовсе, а не пустой строки.
func TestChapterWithoutAuthorOmitsJSONLDAuthorKey(t *testing.T) {
	doc, err := textSource().Chapter(context.Background(), 1, 11)
	if err != nil {
		t.Fatalf("Chapter: %v", err)
	}
	jsonLD, ok := doc.JSONLD.(map[string]any)
	if !ok {
		t.Fatalf("JSONLD не map[string]any: %T", doc.JSONLD)
	}
	if _, present := jsonLD["author"]; present {
		t.Errorf("ключ author не должен печататься для главы без автора: %#v", jsonLD)
	}
}

// Глава-нумератор («2», «II», в корпусе таких около семисот) слага не несёт
// вовсе (pkg/slug.Chapter отбраковывает такие заголовки) — канон обязан
// остаться голым номером, без висячего дефиса на месте пустого слага.
func TestChapterCanonicalKeepsBareNumberForEnumerator(t *testing.T) {
	doc, err := textSource().Chapter(context.Background(), 1, 12)
	if err != nil {
		t.Fatalf("Chapter: %v", err)
	}
	if doc.Canonical != "https://lib.example.org/works/1-lenin-t42/chapters/12" {
		t.Errorf("canonical: %q", doc.Canonical)
	}
}

// Полоса получает теги превью — человек делится тем, что читает сейчас, — но
// в индекс не идёт: это тысячи почти одинаковых адресов, дробящих один текст.
func TestReadPageIsNoIndexAndPointsAtChapter(t *testing.T) {
	doc, err := textSource().ReadPage(context.Background(), 1, 5)
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}

	if doc.Robots != RobotsNoIndex {
		t.Errorf("полоса обязана быть noindex, получено %q", doc.Robots)
	}
	if doc.Canonical != "https://lib.example.org/works/1-lenin-t42/chapters/10-gosudarstvo-i-revolyuciya" {
		t.Errorf("canonical полосы должен вести на главу, получено %q", doc.Canonical)
	}
	if !strings.Contains(doc.Body, "Классовое общество") {
		t.Errorf("в теле нет текста полосы:\n%s", doc.Body)
	}
}

// F1 итогового ревью: та же беда, что и у Concept (render_index_test.go,
// TestConceptBodyHasNoDocumentWrapper) — раньше Renderer.Render отдавал целый
// документ, и тело полосы несло бы второй <title>. Обёртку теперь снимает сам
// pkg/markdown (контракт пакета, см. его докблок) на каждом публичном выходе;
// эта проверка стоит сторожем на случай, если контракт однажды нарушится.
func TestReadPageBodyHasNoDocumentWrapper(t *testing.T) {
	doc, err := textSource().ReadPage(context.Background(), 1, 5)
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	assertBodyIsFragment(t, doc.Body)
}

// Полоса для краулера печатает под текстом настоящие номера: голый Render
// оставлял <ol> gomarkdown, где примечание тома 27 шло первым пунктом.
func TestReadPagePrintsRealNoteMarkers(t *testing.T) {
	s := textSource()
	s.Pages.(*fakePages).byNumber[5].ContentMarkdown =
		"Кретинизма[^27] и оглашение[^s1].\n\n[^s1]: В «Искре»?\n\n[^27]: Выражение Маркса.\n"
	doc, err := s.ReadPage(context.Background(), 1, 5)
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if strings.Contains(doc.Body, "<ol") {
		t.Fatalf("блок сносок нумерует браузер: %s", doc.Body)
	}
	for _, want := range []string{`href="#fnref:5-s1">(1)</a>`, `href="#fnref:5-27">27</a>`} {
		if !strings.Contains(doc.Body, want) {
			t.Errorf("нет %s в %s", want, doc.Body)
		}
	}
	assertBodyIsFragment(t, doc.Body)
}

// У полосы вне главы канонический адрес — карточка тома: иначе он повёл бы в
// никуда.
func TestReadPageWithoutChapterPointsAtWork(t *testing.T) {
	doc, err := textSource().ReadPage(context.Background(), 1, 9)
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if doc.Canonical != "https://lib.example.org/works/1-lenin-t42" {
		t.Errorf("canonical: %q", doc.Canonical)
	}
}

// Печатный номер полосы — pageNumber + PageOffset. Ни один тест до этого не
// брал том с ненулевым смещением, а расхождение внутренней и печатной
// нумерации — самый повторяющийся класс дефектов проекта.
func TestReadPagePrintsOffsetPageNumber(t *testing.T) {
	doc, err := textSource().ReadPage(context.Background(), 2, 3)
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	// 3 (внутренний номер) + 100 (смещение) = 103 печатный.
	if !strings.Contains(doc.Title, "с. 103") {
		t.Errorf("заголовок должен печатать печатный номер 103: %q", doc.Title)
	}
	if strings.Contains(doc.Title, "с. 3 ") || strings.Contains(doc.Title, "с. 3—") {
		t.Errorf("заголовок печатает внутренний номер вместо печатного: %q", doc.Title)
	}
	if !strings.Contains(doc.Body, "с. 103") {
		t.Errorf("тело должно печатать печатный номер 103:\n%s", doc.Body)
	}
}

func TestReadPageMissingIsNotFound(t *testing.T) {
	if _, err := textSource().ReadPage(context.Background(), 1, 777); err == nil {
		t.Fatal("ожидалась ошибка для отсутствующей полосы")
	}
}

// Длинную главу, пришедшую в чат обычной ссылкой, чат обрежет. Строка в
// самом начале тела ведёт модель на текст по частям.
func TestChapterDocOffersTextForLLMs(t *testing.T) {
	doc, err := textSource().Chapter(context.Background(), 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://lib.example.org/works/1-lenin-t42/chapters/10-gosudarstvo-i-revolyuciya.md"
	if doc.Alternate != want {
		t.Errorf("Alternate: %q", doc.Alternate)
	}
	if !strings.HasPrefix(doc.Body, `<p class="llm-note">Текст для нейросетей, по частям: <a href="`+want+`">`) {
		t.Errorf("тело начинается не со ссылки на текст:\n%s", doc.Body[:min(len(doc.Body), 300)])
	}
}
