package seo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"proofreader/internal/site"
	"sort"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"proofreader/internal/models"
)

// fakeDocuments — подставной источник разборов.
//
// Тело приходит УЖЕ СОБРАННЫМ (так устроен DocumentSource: собирает его
// api.assembleDocumentFor, единственный путь рендера авторского markdown), и
// подделка обязана отдавать ровно эту форму — готовый HTML, а не markdown.
// Ошибки отсутствия она отдаёт теми же двумя видами, какими их отдаёт
// настоящий переводчик: seo.ErrNeverPublished у черновика, seo.ErrNotFound у
// снятого.
type fakeDocuments struct {
	pages map[string]*DocumentPage
	list  []*models.Document
	err   error
	// cardCalls — сколько раз спросили карточку. Нужен там, где ошибочный
	// код ответа (404) можно получить и без единого похода в этот источник —
	// например, из общего «неизвестный вид карточки» в cardInput — и тест
	// обязан отличить «карточка честно отказала» от «карточка вовсе не
	// дошла до Documents.Card» (TestDocumentCardRefusesDraft).
	cardCalls int
}

func (f *fakeDocuments) key(nickname, slug string) string { return nickname + "/" + slug }

func (f *fakeDocuments) Page(ctx context.Context, nickname, slug string) (*DocumentPage, error) {
	if f.err != nil {
		return nil, f.err
	}
	p, ok := f.pages[f.key(nickname, slug)]
	if !ok {
		return nil, fmt.Errorf("%w: разбор %q", ErrNotFound, slug)
	}
	return p, nil
}

func (f *fakeDocuments) Card(ctx context.Context, nickname, slug string) (*DocumentCard, error) {
	f.cardCalls++
	p, err := f.Page(ctx, nickname, slug)
	if err != nil {
		return nil, err
	}
	return &DocumentCard{Document: p.Document, CutCount: p.CutCount}, nil
}

func (f *fakeDocuments) ListPublished(ctx context.Context, limit, offset int) ([]*models.Document, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.list, nil
}

func documentSource() *Source {
	published := time.Unix(1_700_000_000, 0)
	staff := &models.Document{
		ID: 1, Slug: "o-gosudarstve", Title: "О государстве",
		PublishedAt: &published, WasPublished: true,
		UpdatedAt: time.Unix(1_700_000_100, 0),
	}
	reader := &models.Document{
		ID: 2, Slug: "chto-delat", Title: "Что делать?", AuthorNickname: "чтец",
		PublishedAt: &published, WasPublished: true,
		UpdatedAt: time.Unix(1_700_000_200, 0),
	}
	docs := &fakeDocuments{
		pages: map[string]*DocumentPage{
			"/o-gosudarstve": {Document: staff, CutCount: 1,
				BodyHTML: "<p>Текст редакции о природе государства.</p>"},
			"чтец/chto-delat": {Document: reader, CutCount: 2,
				BodyHTML: "<p>Текст читателя о том, что делать.</p>"},
		},
		list: []*models.Document{staff, reader},
	}
	s := indexSource()
	s.Documents = docs
	return s
}

func TestDocumentPageCarriesTitleBodyAndCanonical(t *testing.T) {
	doc, err := documentSource().Document(context.Background(), "чтец", "chto-delat")
	if err != nil {
		t.Fatalf("Document: %v", err)
	}

	if want := "Что делать? — разбор читателя чтец — " + site.Name(); doc.Title != want {
		t.Errorf("заглавие %q, ожидалось %q", doc.Title, want)
	}
	if want := "https://lib.example.org/documents/чтец/chto-delat"; doc.Canonical != want {
		t.Errorf("canonical %q, ожидался %q", doc.Canonical, want)
	}
	if want := "https://lib.example.org/og/document/чтец/chto-delat.png"; doc.ImageURL != want {
		t.Errorf("og:image %q, ожидался %q", doc.ImageURL, want)
	}
	if !strings.Contains(doc.Body, "Текст читателя") {
		t.Errorf("собранного тела нет на странице:\n%s", doc.Body)
	}
	// Несъёмная пометка происхождения: её печатает читальня, а не автор.
	if !strings.Contains(doc.Body, "Собрал читатель чтец") {
		t.Errorf("нет пометки о том, кто собрал разбор:\n%s", doc.Body)
	}
	if doc.Description == "" || strings.Contains(doc.Description, "<") {
		t.Errorf("описание не снято с тела: %q", doc.Description)
	}
	if doc.OGType != "article" {
		t.Errorf("og:type %q, ожидался article", doc.OGType)
	}
	if !doc.CacheKey.Equal(time.Unix(1_700_000_200, 0)) {
		t.Errorf("CacheKey %v — не метка правки разбора", doc.CacheKey)
	}
}

// Сотруднический разбор адресуется коротко и подписи не несёт.
func TestStaffDocumentUsesShortAddressWithoutSignature(t *testing.T) {
	doc, err := documentSource().Document(context.Background(), "", "o-gosudarstve")
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if want := "https://lib.example.org/documents/o-gosudarstve"; doc.Canonical != want {
		t.Errorf("canonical %q, ожидался %q", doc.Canonical, want)
	}
	if strings.Contains(doc.Title, "читателя") {
		t.Errorf("сотруднический разбор подписан читателем: %q", doc.Title)
	}
	if strings.Contains(doc.Body, "Собрал читатель") {
		t.Errorf("сотруднический разбор подписан читателем:\n%s", doc.Body)
	}
}

// ОБЕ разновидности индексируются — и читательская тоже, в отличие от
// читательской подборки (TestReaderCollectionPageIsNoindex). Расхождение
// намеренное: подборка не модерируется, разбор проходит модерацию, и
// опубликованное уже просмотрено редактором (решение владельца 19.09.2026).
// Тест стоит ровно затем, чтобы «починка по образцу подборки» была заметна.
func TestBothKindsOfDocumentAreIndexed(t *testing.T) {
	s := documentSource()
	for _, tc := range []struct{ nickname, slug string }{
		{"", "o-gosudarstve"},
		{"чтец", "chto-delat"},
	} {
		doc, err := s.Document(context.Background(), tc.nickname, tc.slug)
		if err != nil {
			t.Fatalf("Document(%q, %q): %v", tc.nickname, tc.slug, err)
		}
		if doc.Robots != RobotsIndex {
			t.Errorf("разбор %q/%q отдан как %q, ожидался %q",
				tc.nickname, tc.slug, doc.Robots, RobotsIndex)
		}
	}
}

// Черновик — 404 (ErrNeverPublished), а не 410: 410 значит «было и снято» и
// выдал бы сам факт существования черновика. Проверяется КОД ОТВЕТА, а не
// только вид ошибки: между ними стоит writeRenderError, и перепутать ветки
// можно там же.
func TestDraftDocumentIsSoftNotFoundToCrawler(t *testing.T) {
	s := documentSource()
	// Текст ошибки нарочно КОНЧАЕТСЯ на "not found". Сегодняшний переводчик
	// такого сообщения не собирает — но isNotFound (source.go) судит в том
	// числе по этому суффиксу, и стоит кому-нибудь дописать в сообщение
	// ошибку репозитория без скобок, как черновик поедет краулеру кодом 410,
	// то есть подтвердит сам факт своего существования. Ветка
	// ErrNeverPublished в documentError стоит раньше проверки текста именно
	// поэтому, и этот вход — забор вокруг неё, а не воспроизведение живого
	// сообщения.
	s.Documents.(*fakeDocuments).err = fmt.Errorf("%w: разбор %q: %v",
		ErrNeverPublished, "chernovik", errors.New("document not found"))

	if _, err := s.Document(context.Background(), "чтец", "chernovik"); !errors.Is(err, ErrNeverPublished) {
		t.Fatalf("черновик отдан не как ErrNeverPublished: %v", err)
	} else if errors.Is(err, ErrNotFound) {
		t.Fatalf("черновик отдан как ErrNotFound — это 410: %v", err)
	}

	rr := httptest.NewRecorder()
	writeRenderError(rr, httptest.NewRequest(http.MethodGet, "/seo/documents/чтец/chernovik", nil),
		"/documents/чтец/chernovik", fmt.Errorf("%w: разбор", ErrNeverPublished))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("черновик отдан кодом %d, ожидался 404", rr.Code)
	}
}

// Снятый с публикации — 410: поисковик выбрасывает такой адрес сразу, а 404
// перепроверяет неделями.
func TestUnpublishedDocumentIsGoneToCrawler(t *testing.T) {
	s := documentSource()
	s.Documents.(*fakeDocuments).err = fmt.Errorf("%w: разбор %q", ErrNotFound, "snyato")

	_, err := s.Document(context.Background(), "чтец", "snyato")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("снятый разбор отдан не как ErrNotFound: %v", err)
	}

	rr := httptest.NewRecorder()
	writeRenderError(rr, httptest.NewRequest(http.MethodGet, "/seo/documents/чтец/snyato", nil),
		"/documents/чтец/snyato", err)
	if rr.Code != http.StatusGone {
		t.Fatalf("снятый разбор отдан кодом %d, ожидался 410", rr.Code)
	}
}

// Витрина перечисляет обе разновидности, каждую своим адресом.
func TestDocumentListPrintsBothAddressShapes(t *testing.T) {
	doc, err := documentSource().DocumentList(context.Background())
	if err != nil {
		t.Fatalf("DocumentList: %v", err)
	}
	for _, want := range []string{
		`href="/documents/o-gosudarstve"`,
		`href="/documents/чтец/chto-delat"`,
		"собрал читатель чтец",
	} {
		if !strings.Contains(doc.Body, want) {
			t.Errorf("в витрине нет %q:\n%s", want, doc.Body)
		}
	}
}

// ─── маршруты ──────────────────────────────────────────────────────────────

func TestMatchKnowsBothDocumentAddressShapes(t *testing.T) {
	for _, tc := range []struct {
		path  string
		heavy bool
	}{
		{"/documents", false},
		{"/documents/o-gosudarstve", true},
		{"/documents/чтец/chto-delat", true},
	} {
		r, ok := match(tc.path)
		if !ok {
			t.Fatalf("адрес %q не разобран", tc.path)
		}
		if r.heavy != tc.heavy {
			t.Errorf("%q: heavy=%v, ожидалось %v", tc.path, r.heavy, tc.heavy)
		}
		// Сверки канона у разбора нет: слаг заморожен при создании, и
		// неканонического вида адреса не существует.
		if r.canonical != nil {
			t.Errorf("%q: у разбора появилась сверка канона", tc.path)
		}
	}
}

// Форма создания разбора закрыта от обхода, а сама витрина и страницы
// разборов — нет. Проверяются АДРЕСА против правил, а не текст файла: прежняя
// построчная сверка у подборок пропускала правило, закрывавшее читальню
// целиком.
func TestRobotsClosesDocumentFormButNotDocuments(t *testing.T) {
	closed := func(path string) bool {
		for _, rule := range closedPaths {
			if matchesRobotsRule(rule, path) {
				return true
			}
		}
		return false
	}
	if !closed("/documents/new") {
		t.Error("форма создания разбора открыта обходу")
	}
	for _, open := range []string{"/documents", "/documents/o-gosudarstve", "/documents/чтец/chto-delat"} {
		if closed(open) {
			t.Errorf("адрес %q закрыт от обхода — краулер не прочтёт и разбора", open)
		}
	}
}

// matchesRobotsRule — правило robots.txt в том виде, в каком его понимает
// поисковик: префикс, с единственной звёздочкой-подстановкой посередине.
// Свой разбор, а не строковое сравнение: правило "/*/edit" закрывает
// "/documents/o-gosudarstve/edit", и сравнение по префиксу этого не видит.
func matchesRobotsRule(rule, path string) bool {
	star := strings.Index(rule, "*")
	if star < 0 {
		return strings.HasPrefix(path, rule)
	}
	head, tail := rule[:star], rule[star+1:]
	if !strings.HasPrefix(path, head) {
		return false
	}
	return strings.Contains(path[len(head):], tail)
}

// ─── чего страница не вправе добавить от себя ──────────────────────────────

// Заглавие и подпись пишет читатель, и печатает их ЭТА функция, а не
// подавленный рендерер: подавление стоит на теле разбора (api.assembleDocument),
// до заглавия оно не достаёт вовсе. Разбираем готовую вёрстку, а не ищем
// подстроки: со снятыми блочными атрибутами строка `{onclick="…"}` остаётся в
// выводе безобидным ТЕКСТОМ, и подстрочная проверка врёт в обе стороны
// (урок C1 ветки 2).
func TestDocumentPageEscapesAuthorWrittenTitleAndNickname(t *testing.T) {
	s := documentSource()
	published := time.Unix(1_700_000_000, 0)
	d := &models.Document{
		ID: 3, Slug: "zlo", AuthorNickname: `чтец"><script>alert(1)</script>`,
		Title:       `Разбор<img src=x onerror="alert(2)">`,
		PublishedAt: &published, WasPublished: true,
	}
	s.Documents.(*fakeDocuments).pages[`чтец"><script>alert(1)</script>/zlo`] = &DocumentPage{
		Document: d, BodyHTML: "<p>Безобидное тело.</p>",
	}

	doc, err := s.Document(context.Background(), d.AuthorNickname, "zlo")
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if bad := unsafeMarkup(Render(doc)); len(bad) > 0 {
		t.Errorf("страница разбора несёт исполняемое: %v\n%s", bad, Render(doc))
	}
}

// Контрольная группа: страница не вправе съедать разметку собранного тела —
// она приезжает уже отрисованной и уже подавленной, и трогать её здесь
// нечем. Без этой проверки «починка», выкинувшая тело целиком, прошла бы
// зелёной.
func TestDocumentPageKeepsAssembledBodyAsIs(t *testing.T) {
	s := documentSource()
	const bodyHTML = `<p><em>курсив</em> и <a href="https://example.org">ссылка</a></p>` +
		`<div class="document-cut"><table><tr><td>товар</td></tr></table></div>`
	s.Documents.(*fakeDocuments).pages["чтец/chto-delat"].BodyHTML = bodyHTML

	doc, err := s.Document(context.Background(), "чтец", "chto-delat")
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if !strings.Contains(doc.Body, bodyHTML) {
		t.Fatalf("собранное тело изменено страницей:\n%s", doc.Body)
	}
}

// unsafeMarkup — твин api.unsafeMarkupIn (internal/api/executable_markup_probe_test.go)
// и markdown-овского близнеца в pkg/markdown/untrusted_author_test.go: Go не
// умеет делить тестовые помощники между пакетами, а все стороны шва обязаны
// судить об исполняемом одинаково. Судит по РАЗМЕТКЕ, а не по подстрокам.
//
// Отличие от api-близнеца одно и оно осознанное: здесь не считается
// претензией внешний подресурс — страница краулера печатает СВОЮ вёрстку
// (её картинок в ней нет вовсе), а тело разбора приезжает сюда уже
// подавленным, и маячок в нём ловит сторож на той стороне шва
// (api.TestSEODocumentPageCarriesNoExecutableAuthorHTML).
func unsafeMarkup(fragment string) []string {
	nodes, err := html.ParseFragment(strings.NewReader(fragment),
		&html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body})
	if err != nil {
		return []string{fmt.Sprintf("фрагмент не разобрался: %v", err)}
	}

	found := map[string]bool{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			// <script type="application/ld+json"> — наша же микроразметка,
			// которую печатает Render; она не исполняется как код и в
			// претензии не идёт. Всякий ДРУГОЙ script — идёт.
			case "script":
				if !isJSONLD(n) {
					found["<script>"] = true
				}
			case "iframe", "object", "embed", "form", "base":
				found["<"+n.Data+">"] = true
			}
			for _, a := range n.Attr {
				name := strings.ToLower(a.Key)
				switch {
				case strings.HasPrefix(name, "on"):
					found[n.Data+"["+name+"]"] = true
				case name == "href" || name == "src" || name == "srcset" ||
					name == "action" || name == "formaction" || name == "data":
					if scheme := urlSchemeOf(a.Val); scheme != "" && !safeSchemes[scheme] {
						found[n.Data+"["+name+"="+scheme+":]"] = true
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, n := range nodes {
		walk(n)
	}

	out := make([]string, 0, len(found))
	for k := range found {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func isJSONLD(n *html.Node) bool {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, "type") && a.Val == "application/ld+json" {
			return true
		}
	}
	return false
}

var safeSchemes = map[string]bool{"http": true, "https": true, "mailto": true}

// urlSchemeOf — схема адреса в нижнем регистре или "" у относительного.
// Пробелы и управляющие знаки срезаются: `java\tscript:` браузер понимает, а
// наивное сравнение — нет.
func urlSchemeOf(raw string) string {
	trimmed := strings.Map(func(r rune) rune {
		if r <= ' ' {
			return -1
		}
		return r
	}, raw)
	i := strings.IndexByte(trimmed, ':')
	if i <= 0 {
		return ""
	}
	scheme := strings.ToLower(trimmed[:i])
	for _, r := range scheme {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') &&
			r != '+' && r != '-' && r != '.' {
			return ""
		}
	}
	return scheme
}
