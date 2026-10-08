package seo

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/pkg/book"
)

type fakeConceptBooks struct {
	bySlug map[string]*ConceptBook
	calls  int32
	// rubrics — подрубрики, с которыми звали сборку, по порядку вызовов.
	rubrics [][]string
}

// fakeNoRubric — подрубрика, которой у понятия нет.
const fakeNoRubric = "нет такой"

func (f *fakeConceptBooks) Concept(ctx context.Context, slug string, rubric []string) (*ConceptBook, error) {
	atomic.AddInt32(&f.calls, 1)
	f.rubrics = append(f.rubrics, rubric)
	cb, ok := f.bySlug[slug]
	if !ok {
		return nil, notFound("понятие %q", slug)
	}
	if len(rubric) > 0 && rubric[0] == fakeNoRubric {
		return nil, ErrNoSuchRubric
	}
	return cb, nil
}

// conceptBookOf — понятие из двух подрубрик; size — знаков на полосу.
// Подрубрика «как субстанция» начинается с полосы 2.
func conceptBookOf(size int) *ConceptBook {
	body := strings.Repeat("т", size)
	page := func(n int) book.Page { return book.Page{Internal: n, Printed: n, Markdown: body} }
	place := func(title string, pages ...book.Page) *book.Section {
		return &book.Section{Title: title, Blocks: []book.Block{{Pages: pages}}}
	}
	return &ConceptBook{
		Concept: &models.IndexConcept{Slug: "abstraktnyj-trud", Title: "Абстрактный труд",
			Articles: []*models.IndexArticle{{EditionTitle: "К. Маркс и Ф. Энгельс. Сочинения, 2-е изд.",
				ArticleMarkdown: "**Абстрактный труд**\n\n— определение — **23**, 46—47"}}},
		Book: &book.Book{Meta: book.Meta{Title: "Абстрактный труд"}, Sections: []book.Section{
			{Title: "определение", Blocks: []book.Block{
				{Child: place("[т. 23, с. 1—2](https://lib.example.org/works/47/pages/1)", page(1), page(2))},
			}},
			{Title: "как субстанция стоимости", Blocks: []book.Block{
				{Child: place("[т. 23, с. 3](https://lib.example.org/works/47/pages/3)", page(3))},
				{Child: &book.Section{Title: "т. 31, с. 5 — тома нет в читальне"}},
			}},
			{Title: "без полос", Blocks: []book.Block{
				{Child: &book.Section{Title: "т. 40, с. 1 — тома нет в читальне"}},
			}},
		}},
		Rubrics: []ConceptRubric{
			{Path: []string{"определение"}, Places: 1, Volumes: []int{23}, FirstPage: 0},
			{Path: []string{"как субстанция стоимости"}, Places: 2, Volumes: []int{23, 31}, FirstPage: 2},
			{Path: []string{"без полос"}, Places: 1, Volumes: []int{40}, FirstPage: -1},
		},
		Places: 4, Present: 2, Pages: 3,
		Modified: time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC),
	}
}

func conceptTextSource(cb *ConceptBook) *Source {
	return &Source{
		BaseURL:      "https://lib.example.org",
		ConceptBooks: &fakeConceptBooks{bySlug: map[string]*ConceptBook{"abstraktnyj-trud": cb}},
	}
}

const conceptURL = "https://lib.example.org/concepts/abstraktnyj-trud"

func TestConceptTextSinglePart(t *testing.T) {
	base, docs, err := conceptTextSource(conceptBookOf(100)).ConceptTextParts(context.Background(), "abstraktnyj-trud", nil)
	if err != nil {
		t.Fatal(err)
	}
	if base != "/concepts/abstraktnyj-trud" || len(docs) != 1 {
		t.Fatalf("base %q, частей %d", base, len(docs))
	}
	d := docs[0]
	for _, want := range []string{
		"Понятие «Абстрактный труд» — предметный указатель\n",
		"Источник: К. Маркс и Ф. Энгельс. Сочинения, 2-е изд.\n",
		"Адрес понятия: " + conceptURL + "\n",
		conceptReadingNote,
		"## Статья указателя\n\n**Абстрактный труд**",
		"Мест в указателе: 4; в читальне: 2, на 3 страницах.",
		"- определение (мест: 1; т. 23)\n",
		"- как субстанция стоимости (мест: 2; тт. 23, 31)\n",
		"- без полос (мест: 1; т. 40; в читальне нет)\n",
		"## определение",
	} {
		if !strings.Contains(d.Body, want) {
			t.Errorf("нет %q:\n%s", want, d.Body)
		}
	}
	if strings.Contains(d.Body, "Часть 1 из") || strings.Contains(d.Body, "Продолжение:") {
		t.Errorf("у понятия в одну часть нет навигации:\n%s", d.Body)
	}
	if d.Canonical != conceptURL || !d.NoIndex || !d.CacheKey.Equal(time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("canonical %q noindex %v cachekey %v", d.Canonical, d.NoIndex, d.CacheKey)
	}
}

// Полоса в 50 тыс. знаков: в часть 80 тыс. влезает одна. Полосы 1, 2, 3 —
// части 1, 2, 3; «как субстанция» начинается с полосы 3, то есть в части 3.
func TestConceptTextPartsChainAndOutlineParts(t *testing.T) {
	_, docs, err := conceptTextSource(conceptBookOf(50000)).ConceptTextParts(context.Background(), "abstraktnyj-trud", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 3 {
		t.Fatalf("частей %d, ожидалось 3", len(docs))
	}
	if !strings.Contains(docs[0].Body, "- определение (мест: 1; т. 23; часть 1)\n") ||
		!strings.Contains(docs[0].Body, "- как субстанция стоимости (мест: 2; тт. 23, 31; часть 3)\n") {
		t.Errorf("номера частей в оглавлении:\n%s", docs[0].Body[:2000])
	}
	if !strings.Contains(docs[2].Body, "## как субстанция стоимости") {
		t.Errorf("заголовок подрубрики не в части 3")
	}
	for k, d := range docs {
		if strings.Contains(d.Body, "## Оглавление понятия") != (k == 0) {
			t.Errorf("часть %d: оглавление только в части 1", k+1)
		}
		if !strings.Contains(d.Body, "Адрес понятия: "+conceptURL+"\n") {
			t.Errorf("часть %d без адреса понятия", k+1)
		}
		line := "Часть " + string(rune('1'+k)) + " из 3."
		if !strings.Contains(d.Body, line) {
			t.Errorf("часть %d: нет %q", k+1, line)
		}
		if k < 2 && !strings.HasSuffix(d.Body, "Продолжение: "+conceptURL+"/part-"+string(rune('2'+k))+".md\n") {
			t.Errorf("часть %d не кончается продолжением", k+1)
		}
		if k > 0 && !strings.Contains(d.Body, "Предыдущая: "+conceptURL+chapterTextPath("", k)) {
			t.Errorf("часть %d без предыдущей", k+1)
		}
	}
}

// Адреса без подрубрики (пустой Path) печатаются строкой «без подрубрики» на
// нулевом уровне вложенности и не роняют оглавление.
func TestConceptTextOutlineRowWithoutRubric(t *testing.T) {
	cb := conceptBookOf(100)
	cb.Rubrics = append([]ConceptRubric{{Places: 2, Volumes: []int{13, 31}, FirstPage: -1}}, cb.Rubrics...)
	cb.Rubrics = append(cb.Rubrics, ConceptRubric{Places: 1, Volumes: []int{40}, FirstPage: 1})
	_, docs, err := conceptTextSource(cb).ConceptTextParts(context.Background(), "abstraktnyj-trud", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(docs[0].Body, "\n- без подрубрики (мест: 2; тт. 13, 31; в читальне нет)\n") {
		t.Errorf("строка без подрубрики:\n%s", docs[0].Body)
	}
	if !strings.Contains(docs[0].Body, "\n- без подрубрики (мест: 1; т. 40)\n") {
		t.Errorf("строка без подрубрики с полосой (одна часть — без номера):\n%s", docs[0].Body)
	}
}

func TestConceptTextWithoutArticleTextSkipsArticleSection(t *testing.T) {
	cb := conceptBookOf(100)
	cb.Concept.Articles[0].ArticleMarkdown = ""
	_, docs, err := conceptTextSource(cb).ConceptTextParts(context.Background(), "abstraktnyj-trud", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(docs[0].Body, "## Статья указателя") {
		t.Errorf("пустая статья напечатана:\n%s", docs[0].Body)
	}
}

// Подрубрики двух изданий — заголовок издания в оглавлении; отсылка без
// адресов и текста в «Источник» не попадает.
func TestConceptTextOutlineGroupsByEdition(t *testing.T) {
	cb := conceptBookOf(100)
	cb.Concept.Articles = append(cb.Concept.Articles, &models.IndexArticle{EditionTitle: "Ленин, ПСС"},
		&models.IndexArticle{EditionTitle: "Ленин, указатель", References: []*models.IndexReference{{ID: 9}}})
	cb.Rubrics[0].Edition = "К. Маркс и Ф. Энгельс. Сочинения, 2-е изд."
	cb.Rubrics[1].Edition = "К. Маркс и Ф. Энгельс. Сочинения, 2-е изд."
	cb.Rubrics[2].Edition = "Ленин, указатель"
	_, docs, err := conceptTextSource(cb).ConceptTextParts(context.Background(), "abstraktnyj-trud", nil)
	if err != nil {
		t.Fatal(err)
	}
	body := docs[0].Body
	for _, want := range []string{
		"Источник: К. Маркс и Ф. Энгельс. Сочинения, 2-е изд.; Ленин, указатель\n",
		"страницах.\n\n### К. Маркс и Ф. Энгельс. Сочинения, 2-е изд.\n\n- определение",
		"(мест: 2; тт. 23, 31)\n\n### Ленин, указатель\n\n- без полос",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("нет %q:\n%s", want, body)
		}
	}
}

func TestConceptTextMissingIsNotFound(t *testing.T) {
	_, _, err := conceptTextSource(conceptBookOf(100)).ConceptTextParts(context.Background(), "net", nil)
	if !isNotFound(err) {
		t.Fatalf("ожидалось «нет», получено %v", err)
	}
}

// Оглавление большого понятия входит в предел части 1: MCP fetch режет
// результат на 100 тыс. знаков, а оглавление — до 20 тыс. сверх текста.
func TestConceptTextFirstPartFitsWithOutline(t *testing.T) {
	cb := conceptBookOf(30000)
	for i := 0; i < 200; i++ {
		cb.Rubrics = append(cb.Rubrics, ConceptRubric{
			Path: []string{fmt.Sprintf("длинная подрубрика номер %03d для оглавления", i)}, Places: 1,
			Volumes: []int{1, 3, 5, 7, 9, 11, 13, 15, 17, 19}, FirstPage: -1,
		})
	}
	_, docs, err := conceptTextSource(cb).ConceptTextParts(context.Background(), "abstraktnyj-trud", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, after, _ := strings.Cut(docs[0].Body, "\n---\n\n")
	before := strings.TrimSuffix(docs[0].Body, after)
	if n := utf8.RuneCountInString(docs[0].Body); n > llmPartRunes+2000 {
		t.Errorf("часть 1 — %d знаков, оглавление в предел не вошло (тело %d, шапка с оглавлением %d)",
			n, utf8.RuneCountInString(after), utf8.RuneCountInString(before))
	}
}
func TestVolumeListCollapsesRuns(t *testing.T) {
	for _, c := range []struct {
		in   []int
		want string
	}{
		{[]int{23}, "т. 23"},
		{[]int{16, 12, 13, 23}, "тт. 12—13, 16, 23"},
		{[]int{5, 6, 7}, "тт. 5—7"},
	} {
		if got := volumeList(c.in); got != c.want {
			t.Errorf("volumeList(%v) = %q, ожидалось %q", c.in, got, c.want)
		}
	}
}

func shelfSource(rows []repository.ConceptShelfRow) *Source {
	return &Source{BaseURL: "https://lib.example.org", Catalog: &fakeCatalog{conceptShelf: rows}}
}

func TestConceptShelfTextGroupsByLetter(t *testing.T) {
	// Порядок строк — как отдаёт SQL (ORDER BY sort_key): кавычки впереди.
	base, docs, err := shelfSource([]repository.ConceptShelfRow{
		{Slug: "borba", Title: "«Борьба»", SortKey: "«борьба»", Places: 3},
		{Slug: "abstraktnyj-trud", Title: "Абстрактный труд", SortKey: "абстрактный труд", Places: 95},
		{Slug: "avans", Title: "Аванс", SortKey: "аванс", Places: 1},
		{Slug: "bank", Title: "Банк", SortKey: "банк", Places: 2},
		{Slug: "yashchik", Title: "Ящик [лат.]", SortKey: "ящик", Places: 0},
	}).ConceptShelfTextParts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if base != "/concepts" || len(docs) != 1 {
		t.Fatalf("base %q частей %d", base, len(docs))
	}
	body := docs[0].Body
	for _, want := range []string{
		"Адрес указателя: https://lib.example.org/concepts\n",
		"## А\n\n- [Абстрактный труд](https://lib.example.org/concepts/abstraktnyj-trud.md) — мест: 95\n" +
			"- [Аванс](https://lib.example.org/concepts/avans.md) — мест: 1\n",
		"мест: 1\n\n## Б\n\n- [Банк](https://lib.example.org/concepts/bank.md) — мест: 2\n" +
			"- [«Борьба»](https://lib.example.org/concepts/borba.md) — мест: 3\n\n## Я\n\n",
		"- [Ящик \\[лат.\\]](https://lib.example.org/concepts/yashchik.md) — мест: 0\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("нет %q:\n%s", want, body)
		}
	}
	if strings.Count(body, "## Б\n") != 1 || strings.Contains(body, "## «") {
		t.Errorf("буква «Б» разбита или кавычки завели свою группу:\n%s", body)
	}
	if docs[0].Canonical != "https://lib.example.org/concepts" || !docs[0].NoIndex {
		t.Errorf("canonical %q noindex %v", docs[0].Canonical, docs[0].NoIndex)
	}
}

func TestConceptShelfTextSplitsIntoParts(t *testing.T) {
	var rows []repository.ConceptShelfRow
	for i := 0; i < 3000; i++ {
		letter := string([]rune("АБВГДЕЖЗИК")[i/300])
		rows = append(rows, repository.ConceptShelfRow{
			Slug: fmt.Sprintf("ponyatie-%04d", i), Title: fmt.Sprintf("%s понятие номер %04d", letter, i),
			SortKey: strings.ToLower(letter) + fmt.Sprintf(" %04d", i), Places: i,
		})
	}
	_, docs, err := shelfSource(rows).ConceptShelfTextParts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) < 2 {
		t.Fatalf("частей %d, ожидалось больше одной", len(docs))
	}
	seen := 0
	for k, d := range docs {
		seen += strings.Count(d.Body, "\n- [")
		if k < len(docs)-1 && !strings.HasSuffix(d.Body, "Продолжение: https://lib.example.org/concepts/part-"+fmt.Sprint(k+2)+".md\n") {
			t.Errorf("часть %d не кончается продолжением", k+1)
		}
	}
	if seen != 3000 {
		t.Errorf("строк понятий во всех частях %d, ожидалось 3000", seen)
	}
}

func TestParseRubricPathAndQueryRoundTrip(t *testing.T) {
	for _, path := range [][]string{
		{"определение"},
		{"КПСС — съезды", "II съезд РСДРП"},
		{"а:б", "в+г д"},
	} {
		q := rubricQuery(path)
		v, err := url.ParseQuery(strings.TrimPrefix(q, "?"))
		if err != nil {
			t.Fatalf("%v: %v", path, err)
		}
		if got := ParseRubricPath(v.Get("rubric_path")); !reflect.DeepEqual(got, path) {
			t.Errorf("%v → %q → %v", path, q, got)
		}
	}
	// Так страница понятия кладёт путь в адрес (encodeRubricPath +
	// URLSearchParams); для кириллицы канон Go совпадает с ним буква в букву.
	if got := rubricQuery([]string{"определение"}); got != "?rubric_path=%25D0%25BE%25D0%25BF%25D1%2580%25D0%25B5%25D0%25B4%25D0%25B5%25D0%25BB%25D0%25B5%25D0%25BD%25D0%25B8%25D0%25B5" {
		t.Errorf("канон: %q", got)
	}
	// Двухзвенный путь с пробелами и тире — тот же пример, что в
	// frontend/src/utils/askAi.test.ts: для таких звеньев кнопка и сервер дают одну строку.
	if got := rubricQuery([]string{"КПСС — съезды", "II съезд"}); got != "?rubric_path="+
		"%25D0%259A%25D0%259F%25D0%25A1%25D0%25A1%2520%25E2%2580%2594%2520%25D1%2581%25D1%258A%25D0%25B5%25D0%25B7%25D0%25B4%25D1%258B"+
		"%3AII%2520%25D1%2581%25D1%258A%25D0%25B5%25D0%25B7%25D0%25B4" {
		t.Errorf("канон двух звеньев: %q", got)
	}
	for _, bad := range []string{"", "%ZZ", "а::б"} {
		if got := ParseRubricPath(bad); got != nil {
			t.Errorf("ParseRubricPath(%q) = %v, ожидался nil", bad, got)
		}
	}
	if rubricQuery(nil) != "" {
		t.Error("без подрубрики хвоста быть не должно")
	}
}

func TestConceptTextRubricHeaderAndPartLinks(t *testing.T) {
	rubric := []string{"как субстанция стоимости"}
	_, docs, err := conceptTextSource(conceptBookOf(50000)).ConceptTextParts(context.Background(), "abstraktnyj-trud", rubric)
	if err != nil {
		t.Fatal(err)
	}
	q := rubricQuery(rubric)
	if !strings.Contains(docs[0].Body, "Только подрубрика «как субстанция стоимости». Понятие целиком: "+conceptURL+".md\n") {
		t.Errorf("нет строки о подрубрике:\n%s", docs[0].Body[:1500])
	}
	if !strings.Contains(docs[0].Body, "Следующая: "+conceptURL+"/part-2.md"+q+".") ||
		!strings.HasSuffix(docs[0].Body, "Продолжение: "+conceptURL+"/part-2.md"+q+"\n") ||
		!strings.Contains(docs[1].Body, "Предыдущая: "+conceptURL+".md"+q+".") {
		t.Error("ссылки на части потеряли подрубрику")
	}
	if docs[0].Canonical != conceptURL+q {
		t.Errorf("canonical %q", docs[0].Canonical)
	}
	_, whole, _ := conceptTextSource(conceptBookOf(100)).ConceptTextParts(context.Background(), "abstraktnyj-trud", nil)
	if strings.Contains(whole[0].Body, "Только подрубрика") {
		t.Error("у понятия целиком строки о подрубрике быть не должно")
	}
}
