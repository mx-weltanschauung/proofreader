package seo

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

type fakeCatalog struct {
	works, chapters, editions, concepts, collections, documents []repository.CatalogRow
	conceptShelf                                                []repository.ConceptShelfRow
}

func (f *fakeCatalog) ConceptShelf(context.Context) ([]repository.ConceptShelfRow, error) {
	return f.conceptShelf, nil
}

func (f *fakeCatalog) Works(context.Context) ([]repository.CatalogRow, error) {
	return f.works, nil
}
func (f *fakeCatalog) Chapters(context.Context) ([]repository.CatalogRow, error) {
	return f.chapters, nil
}
func (f *fakeCatalog) Editions(context.Context) ([]repository.CatalogRow, error) {
	return f.editions, nil
}
func (f *fakeCatalog) Concepts(context.Context) ([]repository.CatalogRow, error) {
	return f.concepts, nil
}
func (f *fakeCatalog) Collections(context.Context) ([]repository.CatalogRow, error) {
	return f.collections, nil
}
func (f *fakeCatalog) Documents(context.Context) ([]repository.CatalogRow, error) {
	return f.documents, nil
}

func catalogHandler(chapterCount int) *Handler {
	stamp := time.Unix(1_700_000_000, 0)
	cat := &fakeCatalog{
		works:    []repository.CatalogRow{{ID: 1, UpdatedAt: stamp}},
		editions: []repository.CatalogRow{{ID: 7, UpdatedAt: stamp}},
		concepts: []repository.CatalogRow{{Slug: "abstraktnyj-trud", UpdatedAt: stamp}},
		// AuthorNickname пуст, PublishedAt непуст — сотрудническая и
		// опубликованная, иначе publicCollectionRows (задача 13) отсеет её из
		// карты сайта молча.
		collections: []repository.CatalogRow{{Slug: "o-gosudarstve", UpdatedAt: stamp, PublishedAt: &stamp}},
	}
	for i := 1; i <= chapterCount; i++ {
		cat.chapters = append(cat.chapters,
			repository.CatalogRow{ID: int64(i), WorkID: 1, UpdatedAt: stamp})
	}
	return NewHandler(&Source{BaseURL: "https://lib.example.org", Catalog: cat})
}

// catalogSource — собран из textSource() (том 1 со слагом lenin-t42, глава
// 10 у него с настоящим book.Book — карта глав рендерит главу целиком, и без
// него сквозной прогон падает паникой в Source.Chapter) добавлением издания
// 5 и каталога с непустыми Works/Chapters/Editions: TestSitemapPrintsOnly...
// прогоняет каждый адрес карты через настоящий обработчик и должен получить
// канон, а не 301, поэтому слаги в каталоге обязаны совпасть со слагами,
// которые вычислят Works/Chapters/Editions при разборе адреса.
func catalogSource() *Source {
	s := textSource()
	s.Editions = &fakeEditions{byID: map[int64]*models.Edition{
		5: {ID: 5, Title: "Н. Г. Чернышевский. Полное собрание сочинений", URLSlug: "chernyshevsky"},
	}}

	stamp := time.Unix(1_700_000_000, 0)
	s.Catalog = &fakeCatalog{
		works: []repository.CatalogRow{{ID: 1, Slug: "lenin-t42", UpdatedAt: stamp}},
		chapters: []repository.CatalogRow{{
			ID: 10, WorkID: 1, Slug: "gosudarstvo-i-revolyuciya", WorkSlug: "lenin-t42",
			UpdatedAt: stamp,
		}},
		editions: []repository.CatalogRow{{ID: 5, Slug: "chernyshevsky", UpdatedAt: stamp}},
		// Оба вида опубликованного разбора — сотруднический (короткий адрес)
		// и читательский (адрес с подписью): TestSitemapPrintsOnlyServable...
		// обязан прогнать обе ветки documentPath через настоящий обработчик.
		documents: []repository.CatalogRow{
			{Slug: "o-gosudarstve", UpdatedAt: stamp, PublishedAt: &stamp},
			{Slug: "chto-delat", AuthorNickname: "chitatel", UpdatedAt: stamp, PublishedAt: &stamp},
		},
	}
	staffDoc := &models.Document{Slug: "o-gosudarstve", Title: "О государстве", PublishedAt: &stamp, WasPublished: true}
	readerDoc := &models.Document{Slug: "chto-delat", Title: "Что делать?", AuthorNickname: "chitatel",
		PublishedAt: &stamp, WasPublished: true}
	s.Documents = &fakeDocuments{
		pages: map[string]*DocumentPage{
			"/o-gosudarstve":      {Document: staffDoc, BodyHTML: "<p>Текст.</p>"},
			"chitatel/chto-delat": {Document: readerDoc, BodyHTML: "<p>Текст.</p>"},
		},
	}
	return s
}

// Карта, печатающая неканонический адрес, отправляет краулера в лишний 301 на
// каждом адресе корпуса. Проверяется не текстом, а прогоном: каждый адрес из
// карты скармливается обратно обработчику и обязан ответить 200.
func TestSitemapPrintsOnlyServableCanonicalURLs(t *testing.T) {
	src := catalogSource()
	h := handlerFor(src)

	for _, kind := range []string{"works", "chapters", "editions", "documents"} {
		req := httptest.NewRequest(http.MethodGet, "/sitemap-"+kind+".xml", nil)
		rec := httptest.NewRecorder()
		h.Sitemap(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: код %d", kind, rec.Code)
		}

		var set urlSet
		if err := xml.Unmarshal(rec.Body.Bytes(), &set); err != nil {
			t.Fatalf("%s: разбор карты: %v", kind, err)
		}
		if len(set.URLs) == 0 {
			t.Fatalf("%s: карта пуста — тест не проверил бы ничего", kind)
		}

		for _, u := range set.URLs {
			path := strings.TrimPrefix(u.Loc, "https://lib.example.org")
			page := get(t, h, path, nil)
			if page.Code != http.StatusOK {
				t.Errorf("%s: %s отвечает %d, а карта обязана печатать только канон",
					kind, u.Loc, page.Code)
			}
		}
	}
}

// Читательские подборки не модерируются: без отбора спам от читателя попал
// бы прямо в карту сайта. Отбор (publicCollectionRows, sitemap.go) держит
// три случая — сотрудническую опубликованную (остаётся), читательскую
// опубликованную (снаружи видна по ссылке, но не в карте) и сотруднический
// черновик (снаружи не существует вовсе) — и только первая должна выжить.
func TestSitemapSkipsReaderCollections(t *testing.T) {
	stamp := time.Unix(1_700_000_000, 0)
	cat := &fakeCatalog{
		collections: []repository.CatalogRow{
			{Slug: "o-gosudarstve", UpdatedAt: stamp, PublishedAt: &stamp},
			{Slug: "moi-lyubimye-glavy", AuthorNickname: "chitatel", UpdatedAt: stamp, PublishedAt: &stamp},
			{Slug: "chernovik", UpdatedAt: stamp},
		},
	}
	h := NewHandler(&Source{BaseURL: "https://lib.example.org", Catalog: cat})

	rec := fetch(t, h, "/sitemap-collections.xml", h.Sitemap)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	var set urlSet
	if err := xml.Unmarshal(rec.Body.Bytes(), &set); err != nil {
		t.Fatalf("карта не разбирается как XML: %v\n%s", err, rec.Body.String())
	}

	if len(set.URLs) != 1 {
		t.Fatalf("ожидался один адрес (сотрудническая опубликованная), получено %d: %+v",
			len(set.URLs), set.URLs)
	}
	if got := set.URLs[0].Loc; got != "https://lib.example.org/collections/o-gosudarstve" {
		t.Errorf("в карте не та подборка: %q", got)
	}
	if strings.Contains(rec.Body.String(), "moi-lyubimye-glavy") {
		t.Errorf("читательская подборка попала в карту сайта:\n%s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "chernovik") {
		t.Errorf("черновик попал в карту сайта:\n%s", rec.Body.String())
	}
}

// В карту сайта идут ОБЕ разновидности опубликованного разбора — читательский
// наравне с сотрудническим, — в отличие от подборок: разбор проходит
// модерацию редактора, и опубликованный уже просмотрен независимо от того,
// кто его написал (решение владельца 19.09.2026, isPublicDocumentRow в
// sitemap.go). Черновик (PublishedAt == nil) в карту не идёт ни в каком виде.
func TestSitemapListsBothKindsOfPublishedDocuments(t *testing.T) {
	stamp := time.Unix(1_700_000_000, 0)
	cat := &fakeCatalog{
		documents: []repository.CatalogRow{
			{Slug: "o-gosudarstve", UpdatedAt: stamp, PublishedAt: &stamp},
			{Slug: "chto-delat", AuthorNickname: "chitatel", UpdatedAt: stamp, PublishedAt: &stamp},
			{Slug: "chernovik", UpdatedAt: stamp},
		},
	}
	h := NewHandler(&Source{BaseURL: "https://lib.example.org", Catalog: cat})

	rec := fetch(t, h, "/sitemap-documents.xml", h.Sitemap)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	var set urlSet
	if err := xml.Unmarshal(rec.Body.Bytes(), &set); err != nil {
		t.Fatalf("карта не разбирается как XML: %v\n%s", err, rec.Body.String())
	}

	if len(set.URLs) != 2 {
		t.Fatalf("ожидалось два адреса (сотруднический и читательский, черновик не идёт), получено %d: %+v",
			len(set.URLs), set.URLs)
	}
	if strings.Contains(rec.Body.String(), "chernovik") {
		t.Errorf("черновик попал в карту сайта:\n%s", rec.Body.String())
	}
	var gotStaff, gotReader bool
	for _, u := range set.URLs {
		switch u.Loc {
		case "https://lib.example.org/documents/o-gosudarstve":
			gotStaff = true
		case "https://lib.example.org/documents/chitatel/chto-delat":
			gotReader = true
		}
	}
	if !gotStaff {
		t.Errorf("нет сотруднического разбора в карте: %+v", set.URLs)
	}
	if !gotReader {
		t.Errorf("нет читательского разбора в карте: %+v", set.URLs)
	}
}

// Адрес читательского разбора в карте — длинный, с подписью
// (/documents/{ник}/{слаг}), а не короткий сотруднический
// (/documents/{слаг}): слаг у разбора уникален только в паре с подписью
// автора (documentPath, canonical.go).
func TestSitemapPrintsReaderDocumentWithNickname(t *testing.T) {
	stamp := time.Unix(1_700_000_000, 0)
	cat := &fakeCatalog{
		documents: []repository.CatalogRow{
			{Slug: "chto-delat", AuthorNickname: "chitatel", UpdatedAt: stamp, PublishedAt: &stamp},
		},
	}
	h := NewHandler(&Source{BaseURL: "https://lib.example.org", Catalog: cat})

	rec := fetch(t, h, "/sitemap-documents.xml", h.Sitemap)
	var set urlSet
	if err := xml.Unmarshal(rec.Body.Bytes(), &set); err != nil {
		t.Fatalf("карта не разбирается как XML: %v\n%s", err, rec.Body.String())
	}
	if len(set.URLs) != 1 {
		t.Fatalf("ожидался один адрес, получено %d: %+v", len(set.URLs), set.URLs)
	}
	if got := set.URLs[0].Loc; got != "https://lib.example.org/documents/chitatel/chto-delat" {
		t.Errorf("адрес читательского разбора: %q", got)
	}
}

func fetch(t *testing.T, h *Handler, path string, fn http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	fn(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// Карта обязана разбираться как XML: молча битую карту Яндекс отвергает
// целиком, без указания строки.
func TestSitemapIsValidXML(t *testing.T) {
	h := catalogHandler(3)
	rec := fetch(t, h, "/sitemap-works.xml", h.Sitemap)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	var parsed struct {
		URLs []struct {
			Loc     string `xml:"loc"`
			LastMod string `xml:"lastmod"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("карта не разбирается как XML: %v\n%s", err, rec.Body.String())
	}
	if len(parsed.URLs) != 1 {
		t.Fatalf("ожидался один адрес, получено %d", len(parsed.URLs))
	}
	if parsed.URLs[0].Loc != "https://lib.example.org/works/1" {
		t.Errorf("адрес: %q", parsed.URLs[0].Loc)
	}
	if parsed.URLs[0].LastMod == "" {
		t.Error("нет lastmod — Яндекс не узнает, что страница изменилась")
	}
}

// В одном файле карты не может быть больше 50 000 адресов. Глав по корпусу
// уже десятки тысяч, поэтому карта глав режется на куски.
func TestSitemapChaptersAreChunked(t *testing.T) {
	h := catalogHandler(sitemapChunk + 5)

	first := fetch(t, h, "/sitemap-chapters-1.xml", h.Sitemap)
	if first.Code != http.StatusOK {
		t.Fatalf("первый кусок: код %d", first.Code)
	}
	if got := strings.Count(first.Body.String(), "<loc>"); got != sitemapChunk {
		t.Errorf("в первом куске %d адресов, ожидалось %d", got, sitemapChunk)
	}

	second := fetch(t, h, "/sitemap-chapters-2.xml", h.Sitemap)
	if got := strings.Count(second.Body.String(), "<loc>"); got != 5 {
		t.Errorf("во втором куске %d адресов, ожидалось 5", got)
	}

	if third := fetch(t, h, "/sitemap-chapters-3.xml", h.Sitemap); third.Code != http.StatusNotFound {
		t.Errorf("третий кусок должен отвечать 404, получено %d", third.Code)
	}
}

// Индекс обязан перечислять все куски: кусок, на который никто не ссылается,
// поисковик не найдёт.
func TestSitemapIndexListsEveryChunk(t *testing.T) {
	h := catalogHandler(sitemapChunk + 1)
	rec := fetch(t, h, "/sitemap.xml", h.SitemapIndex)

	body := rec.Body.String()
	for _, want := range []string{
		"/sitemap-works.xml", "/sitemap-editions.xml",
		"/sitemap-concepts.xml", "/sitemap-collections.xml",
		"/sitemap-documents.xml",
		"/sitemap-chapters-1.xml", "/sitemap-chapters-2.xml",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("индекс не ссылается на %s:\n%s", want, body)
		}
	}
}

// Адрес и заголовок понятия приходят из базы и могут содержать амперсанд;
// не заэкранированный, он делает карту неразбираемой.
func TestSitemapEscapesAmpersandInSlug(t *testing.T) {
	h := NewHandler(&Source{
		BaseURL: "https://lib.example.org",
		Catalog: &fakeCatalog{concepts: []repository.CatalogRow{
			{Slug: "trud&kapital", UpdatedAt: time.Unix(1, 0)},
		}},
	})
	rec := fetch(t, h, "/sitemap-concepts.xml", h.Sitemap)

	var parsed struct {
		URLs []struct {
			Loc string `xml:"loc"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("амперсанд в адресе сделал карту неразбираемой: %v\n%s", err, rec.Body.String())
	}
	if len(parsed.URLs) != 1 || !strings.HasSuffix(parsed.URLs[0].Loc, "trud&kapital") {
		t.Errorf("адрес искажён: %+v", parsed.URLs)
	}
}

func TestSitemapInvalidKindIs404(t *testing.T) {
	h := catalogHandler(1)
	rec := fetch(t, h, "/sitemap-foo.xml", h.Sitemap)

	if rec.Code != http.StatusNotFound {
		t.Errorf("неизвестный вид карты должен быть 404, получено %d", rec.Code)
	}
}

func TestSitemapInvalidChapterChunkIs404(t *testing.T) {
	h := catalogHandler(1)
	rec := fetch(t, h, "/sitemap-chapters-abc.xml", h.Sitemap)

	if rec.Code != http.StatusNotFound {
		t.Errorf("/sitemap-chapters-abc.xml должна быть 404, получено %d", rec.Code)
	}
}

func TestSitemapWorksWithChunkIs404(t *testing.T) {
	h := catalogHandler(1)
	rec := fetch(t, h, "/sitemap-works-1.xml", h.Sitemap)

	if rec.Code != http.StatusNotFound {
		t.Errorf("/sitemap-works-1.xml (нумерованная работа) должна быть 404, получено %d", rec.Code)
	}
}

// robotsAllowsAgent — пускает ли robots.txt агента agent на адрес path, так
// же, как файл читает сам робот: берётся группа, где агент назван по имени
// (без учёта регистра), а если такой нет — группа «*»; правило Disallow этой
// группы срабатывает, если адрес начинается с образца, где «*» покрывает
// любую последовательность знаков. Группы нет вовсе — обход открыт.
//
// Матчер живёт в тесте намеренно. Прежняя версия проверки перечисляла строки
// файла дословно и была слепа к добавлениям: мутация, дописавшая
// "Disallow: /works/" — то есть закрывшая читальню целиком, — проходила
// зелёной. Сторожить надо обходимость адреса, а не текст файла.
func robotsAllowsAgent(body, agent, path string) bool {
	type group struct {
		agents []string
		rules  []string
	}
	var groups []*group
	var cur *group
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if ua, ok := strings.CutPrefix(line, "User-agent:"); ok {
			if cur == nil || len(cur.rules) > 0 {
				cur = &group{}
				groups = append(groups, cur)
			}
			cur.agents = append(cur.agents, strings.ToLower(strings.TrimSpace(ua)))
			continue
		}
		if rule, ok := strings.CutPrefix(line, "Disallow:"); ok && cur != nil {
			cur.rules = append(cur.rules, strings.TrimSpace(rule))
		}
	}
	pick := func(name string) *group {
		for _, g := range groups {
			for _, a := range g.agents {
				if a == name {
					return g
				}
			}
		}
		return nil
	}
	g := pick(strings.ToLower(agent))
	if g == nil {
		g = pick("*")
	}
	if g == nil {
		return true
	}
	for _, rule := range g.rules {
		if rule == "" {
			continue
		}
		var re strings.Builder
		re.WriteString("^")
		for i, part := range strings.Split(rule, "*") {
			if i > 0 {
				re.WriteString(".*")
			}
			re.WriteString(regexp.QuoteMeta(part))
		}
		if regexp.MustCompile(re.String()).MatchString(path) {
			return false
		}
	}
	return true
}

// robotsAllows — прежний помощник: обычный поисковик.
func robotsAllows(body, path string) bool { return robotsAllowsAgent(body, "Googlebot", path) }

// Обучающие роботы закрыты целиком (спека, решение 1: текст, ушедший в
// обучающий набор, не снять). Сборщики по запросу читателя и поисковики —
// открыты, в том числе на .md и llms.txt. Список — литералом, а не
// trainingBots: тест, читающий тот же список, сторожил бы сам себя.
func TestRobotsClosesTrainingBotsOnly(t *testing.T) {
	h := catalogHandler(1)
	body := fetch(t, h, "/robots.txt", h.Robots).Body.String()
	paths := []string{"/", "/works/49-lenin-t06/chapters/10125-chto-delat",
		"/works/49-lenin-t06/chapters/10125-chto-delat.md", "/llms.txt"}
	for _, bot := range []string{"GPTBot", "ClaudeBot", "CCBot", "Google-Extended",
		"Applebot-Extended", "meta-externalagent", "Bytespider", "Amazonbot"} {
		for _, p := range paths {
			if robotsAllowsAgent(body, bot, p) {
				t.Errorf("%s пущен на %s:\n%s", bot, p, body)
			}
		}
	}
	for _, agent := range []string{"ChatGPT-User", "Claude-User", "Perplexity-User", "Googlebot", "YandexBot"} {
		for _, p := range paths {
			if !robotsAllowsAgent(body, agent, p) {
				t.Errorf("%s не пущен на %s:\n%s", agent, p, body)
			}
		}
	}
}

// Полоса — то, чем цитирует читатель: именно на неё придёт внешняя ссылка, и
// именно её теги (noindex, follow с canonical на главу) склеивают цитату с
// главой. Закрытая в robots полоса этих тегов не показывает никому, а
// краулеры Телеграма и ВК robots соблюдают — цитата уходит в чат ещё и без
// карточки. Обходу открыто, из индекса убрано тегом; это два разных
// механизма, и закрывать обход первым из них — значит отменить второй.
func TestRobotsLeavesReadingSurfaceOpen(t *testing.T) {
	h := catalogHandler(1)
	body := fetch(t, h, "/robots.txt", h.Robots).Body.String()

	for _, path := range []string{
		"/",
		"/works/49-lenin-t06",
		"/works/49-lenin-t06/chapters/10125-chto-delat",
		"/works/49-lenin-t06/pages/233",
		"/works/49-lenin-t06/reading",
		"/editions/5-chernyshevsky",
		"/concepts/abstraktnyj-trud",
		"/collections/o-gosudarstve",
		"/documents",
		"/documents/12",
		"/documents/o-gosudarstve",
		"/documents/chitatel/chto-delat",
	} {
		if !robotsAllows(body, path) {
			t.Errorf("robots.txt закрывает читательский адрес %q:\n%s", path, body)
		}
	}

	// Форма создания разбора — редакторский адрес того же семейства: в
	// выдаче ей делать нечего, ровно как /works/new. Без этой проверки снятие
	// запрета на /documents (сделанное чужим коммитом до этой ветки) некому
	// удержать от расползания на /documents/new тоже.
	if robotsAllows(body, "/documents/new") {
		t.Errorf("robots.txt не закрывает форму создания разбора /documents/new:\n%s", body)
	}
}

func TestRobotsClosesEditorRoutesAndNamesSitemap(t *testing.T) {
	h := catalogHandler(1)
	rec := fetch(t, h, "/robots.txt", h.Robots)

	body := rec.Body.String()
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type: %q", ct)
	}
	for _, path := range []string{
		"/login",
		"/admin/cache",
		"/works/new",
		"/works/49-lenin-t06/edit",
		// Выдача поиска — неограниченное пространство адресов, каждый из
		// которых стоит запроса к базе по всему корпусу; обходить его
		// поисковику незачем.
		"/search",
	} {
		if robotsAllows(body, path) {
			t.Errorf("robots.txt не закрывает редакторский адрес %q:\n%s", path, body)
		}
	}
	if !strings.Contains(body, "Sitemap: https://lib.example.org/sitemap.xml") {
		t.Errorf("в robots.txt нет карты сайта:\n%s", body)
	}
	if !strings.Contains(body, "User-agent: *") {
		t.Errorf("нет User-agent:\n%s", body)
	}
}
