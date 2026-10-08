package seo

import (
	"context"
	"errors"
	"proofreader/internal/site"
	"strings"
	"testing"
	"time"

	"proofreader/internal/models"
	"proofreader/pkg/book"
)

// pagesBook — глава из n полос по size знаков каждая: печатные 101, 102, …
func pagesBook(n, size int) *book.Book {
	var pages []book.Page
	for i := 0; i < n; i++ {
		pages = append(pages, book.Page{Internal: i + 1, Printed: 101 + i,
			Markdown: strings.Repeat("ж", size)})
	}
	return &book.Book{
		Meta: book.Meta{Title: "Длинная", Authors: []string{"К. Маркс"},
			Edition: "Сочинения", Volume: "Том 23", PageFrom: 101, PageTo: 100 + n,
			CacheKey: time.Unix(1_700_000_100, 0)},
		Sections: []book.Section{{Title: "Длинная", Blocks: []book.Block{{Pages: pages}}}},
	}
}

// synth — текст с известными размерами: титул «TT» (2 знака) и n полос по
// size знаков «ж» (кириллица: 2 байта на знак — смещения байтовые, счёт
// знаковый, и тест держит оба).
func synth(n, size int) (string, []book.PageStart) {
	var b strings.Builder
	b.WriteString("TT")
	var starts []book.PageStart
	for i := 0; i < n; i++ {
		starts = append(starts, book.PageStart{Offset: b.Len(), Printed: 101 + i})
		b.WriteString(strings.Repeat("ж", size))
	}
	return b.String(), starts
}

func TestSplitPartsKeepsWholeTextAndCutsOnlyAtPageStarts(t *testing.T) {
	text, starts := synth(5, 10)
	parts := splitParts(text, starts, 25)

	var glued strings.Builder
	for _, p := range parts {
		glued.WriteString(p.Body)
	}
	if glued.String() != text {
		t.Fatal("склейка частей не равна тексту")
	}
	if len(parts) < 2 {
		t.Fatalf("частей %d, ожидалось больше одной", len(parts))
	}
	offset := len(parts[0].Body)
	for i, p := range parts[1:] {
		found := false
		for _, st := range starts {
			if st.Offset == offset {
				found = true
			}
		}
		if !found {
			t.Errorf("часть %d начинается не на начале полосы (смещение %d)", i+2, offset)
		}
		offset += len(p.Body)
	}
}

func TestSplitPartsPageRanges(t *testing.T) {
	// Титул 2 + полосы по 10 при пределе 25: 2+10+10=22 — две полосы,
	// третья дала бы 32; дальше 10+10=20 — снова две; остаток — одна.
	text, starts := synth(5, 10)
	parts := splitParts(text, starts, 25)
	want := [][2]int{{101, 102}, {103, 104}, {105, 105}}
	if len(parts) != len(want) {
		t.Fatalf("частей %d, ожидалось %d", len(parts), len(want))
	}
	for i, p := range parts {
		if p.FromPrinted != want[i][0] || p.ToPrinted != want[i][1] {
			t.Errorf("часть %d: с. %d—%d, ожидалось %d—%d", i+1, p.FromPrinted, p.ToPrinted, want[i][0], want[i][1])
		}
	}
}

// Полоса больше предела — одна часть целиком: полосу не режем, пустых
// частей не бывает.
func TestSplitPartsOversizedPageIsOwnPart(t *testing.T) {
	text, starts := synth(3, 100)
	parts := splitParts(text, starts, 50)
	if len(parts) != 3 {
		t.Fatalf("частей %d, ожидалось 3", len(parts))
	}
	for i, p := range parts {
		if p.FromPrinted != 101+i || p.ToPrinted != 101+i {
			t.Errorf("часть %d: с. %d—%d", i+1, p.FromPrinted, p.ToPrinted)
		}
		if strings.TrimSpace(p.Body) == "" {
			t.Errorf("часть %d пустая", i+1)
		}
	}
}

func TestSplitPartsWithoutPagesIsSinglePart(t *testing.T) {
	parts := splitParts("# Заглавие\n\n", nil, 5)
	if len(parts) != 1 || parts[0].Body != "# Заглавие\n\n" {
		t.Fatalf("ожидалась одна часть со всем текстом: %+v", parts)
	}
}

// Заголовок вложенной главы едет в ту же часть, что её первая полоса.
func TestSplitPartsKeepsHeadingWithItsPage(t *testing.T) {
	inner := book.Section{Title: "Глава вторая", Blocks: []book.Block{{Pages: []book.Page{
		{Internal: 2, Printed: 2, Markdown: strings.Repeat("б", 30)},
	}}}}
	b := &book.Book{Meta: book.Meta{Title: "Т"}, Sections: []book.Section{{
		Title: "Т",
		Blocks: []book.Block{
			{Pages: []book.Page{{Internal: 1, Printed: 1, Markdown: strings.Repeat("а", 30)}}},
			{Child: &inner},
		},
	}}}
	text, starts := book.MarkdownWithPageStarts(b)
	parts := splitParts(text, starts, 40)
	if len(parts) != 2 {
		t.Fatalf("частей %d, ожидалось 2", len(parts))
	}
	if !strings.HasPrefix(parts[1].Body, "### Глава вторая") {
		t.Errorf("вторая часть начинается не с заголовка своей главы:\n%q", parts[1].Body)
	}
	if strings.Contains(parts[0].Body, "Глава вторая") {
		t.Error("заголовок второй главы остался в первой части")
	}
}

func TestChapterTextPathShapes(t *testing.T) {
	ch := "/works/1-lenin-t42/chapters/10-gosudarstvo-i-revolyuciya"
	if got := chapterTextPath(ch, 1); got != ch+".md" {
		t.Errorf("часть 1: %q", got)
	}
	if got := chapterTextPath(ch, 3); got != ch+"/part-3.md" {
		t.Errorf("часть 3: %q", got)
	}
}

// Глава в одну часть: шапка, затем вывод MarkdownWriter байт в байт.
func TestChapterTextSinglePartIsWriterOutput(t *testing.T) {
	src := textSource()
	doc, err := src.ChapterText(context.Background(), 1, 10, 1)
	if err != nil {
		t.Fatalf("ChapterText: %v", err)
	}
	b, _ := src.Books.Chapter(context.Background(), 1, 10)
	var want strings.Builder
	if err := (book.MarkdownWriter{}).Write(&want, b); err != nil {
		t.Fatal(err)
	}
	_, body, ok := strings.Cut(doc.Body, "\n---\n\n")
	if !ok {
		t.Fatalf("нет разделителя шапки:\n%s", doc.Body)
	}
	if body != want.String() {
		t.Errorf("тело разошлось с MarkdownWriter:\n--- got\n%s\n--- want\n%s", body, want.String())
	}
	for _, line := range []string{
		"Глава «Государство и революция» — В. И. Ленин\n",
		"Источник: Полное собрание сочинений. Том 42\n",
		"Адрес главы: https://lib.example.org/works/1-lenin-t42/chapters/10-gosudarstvo-i-revolyuciya\n",
		"[123] перед абзацами",
	} {
		if !strings.Contains(doc.Body, line) {
			t.Errorf("в шапке нет %q:\n%s", line, doc.Body)
		}
	}
	if strings.Contains(doc.Body, "Часть 1 из") || strings.Contains(doc.Body, "Продолжение:") {
		t.Errorf("у главы в одну часть не должно быть навигации по частям:\n%s", doc.Body)
	}
	if doc.Canonical != "https://lib.example.org/works/1-lenin-t42/chapters/10-gosudarstvo-i-revolyuciya" {
		t.Errorf("Canonical: %q", doc.Canonical)
	}
	if !doc.NoIndex {
		t.Error("текст главы обязан быть noindex — иначе 11 тыс. дублей глав в поиске")
	}
	if doc.CacheKey.Unix() != 1_700_000_000 {
		t.Errorf("CacheKey: %v", doc.CacheKey)
	}
}

// longSource — textSource, у которого глава 10 — пять полос по 30 тыс. знаков:
// при пределе 80 тыс. это три части (101—102, 103—104, 105).
func longSource() *Source {
	src := textSource()
	src.Books.(*fakeBooks).byChapter[10] = pagesBook(5, 30000)
	return src
}

func TestChapterTextPartsChain(t *testing.T) {
	src := longSource()
	base := "https://lib.example.org/works/1-lenin-t42/chapters/10-gosudarstvo-i-revolyuciya"
	want := []struct {
		line, prev, next string
	}{
		{"Часть 1 из 3, с. 101—102.", "", base + "/part-2.md"},
		{"Часть 2 из 3, с. 103—104.", base + ".md", base + "/part-3.md"},
		{"Часть 3 из 3, с. 105—105.", base + "/part-2.md", ""},
	}
	for i, w := range want {
		doc, err := src.ChapterText(context.Background(), 1, 10, i+1)
		if err != nil {
			t.Fatalf("часть %d: %v", i+1, err)
		}
		if !strings.Contains(doc.Body, w.line) {
			t.Errorf("часть %d: нет строки %q", i+1, w.line)
		}
		if w.prev != "" && !strings.Contains(doc.Body, "Предыдущая: "+w.prev+".") {
			t.Errorf("часть %d: нет ссылки на предыдущую %s", i+1, w.prev)
		}
		if w.prev == "" && strings.Contains(doc.Body, "Предыдущая:") {
			t.Errorf("часть %d: у первой части нет предыдущей", i+1)
		}
		if w.next != "" {
			if !strings.Contains(doc.Body, "Следующая: "+w.next+".") {
				t.Errorf("часть %d: нет ссылки на следующую в шапке", i+1)
			}
			if !strings.HasSuffix(doc.Body, "Продолжение: "+w.next+"\n") {
				t.Errorf("часть %d: последняя строка не «Продолжение»:\n%q", i+1, doc.Body[len(doc.Body)-120:])
			}
		} else if strings.Contains(doc.Body, "Продолжение:") || strings.Contains(doc.Body, "Следующая:") {
			t.Errorf("у последней части нет следующей")
		}
	}
}

func TestChapterTextPartOutOfRange(t *testing.T) {
	src := longSource()
	for _, part := range []int{0, 4} {
		if _, err := src.ChapterText(context.Background(), 1, 10, part); !errors.Is(err, errNoSuchPart) {
			t.Errorf("часть %d: ошибка %v, ожидалась errNoSuchPart", part, err)
		}
	}
}

func TestChapterTextEmptyChapterSaysSo(t *testing.T) {
	src := textSource()
	src.Books.(*fakeBooks).byChapter[10] = &book.Book{
		Meta: book.Meta{Title: "Пустая"},
		Sections: []book.Section{{Title: "Пустая", Blocks: []book.Block{{Pages: []book.Page{
			{Internal: 5, Printed: 5, Markdown: "  \n"},
		}}}}},
	}
	doc, err := src.ChapterText(context.Background(), 1, 10, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.Body, "Текста на этих страницах нет.") {
		t.Errorf("пустая глава не сказала об этом:\n%s", doc.Body)
	}
}

// Сборка книги сломалась — ошибка уходит наверх как есть (writeRenderError
// различает 410 и 500 сам).
func TestChapterTextPropagatesBookError(t *testing.T) {
	src := textSource()
	if _, err := src.ChapterText(context.Background(), 1, 999, 1); !isNotFound(err) {
		t.Errorf("ошибка %v, ожидалось «не найдено»", err)
	}
}

// llmVolumeSource — том 1 с вложенными главами, главой аппарата, заглавием со
// скобками и передними листами; издание 7 с томами 1 и 3 (3 — передние листы,
// в llms.txt не идут); том вне изданий 50.
func llmVolumeSource() *Source {
	src := volumeSource()
	works := src.Works.(*fakeWorks)
	works.byID[1].PageOffset = 2
	works.byID[3] = &models.Work{ID: 3, Title: "Титул и содержание", Role: models.WorkRoleFrontMatter,
		Slug: "lenin-t42-titul"}
	works.children = map[int64][]*models.Work{1: {works.byID[3]}}
	works.loose = []models.ShelfWork{{ID: 50, Title: "Отдельная [книга]", Slug: "otdelnaya-kniga"}}

	chapters := src.Chapters.(*fakeChapters)
	chapters.trees[1] = []*models.Chapter{
		{ID: 10, WorkID: 1, Title: "Государство и революция", StartPage: 5, EndPage: 120,
			Slug: "gosudarstvo-i-revolyuciya", Children: []*models.Chapter{
				{ID: 13, WorkID: 1, Title: "Глава I [черновик]", StartPage: 7, EndPage: 30, Slug: "glava-i"},
			}},
		{ID: 14, WorkID: 1, Title: "Примечания", StartPage: 121, EndPage: 130, Slug: "primechaniya",
			IsApparatus: true},
	}
	chapters.trees[3] = []*models.Chapter{
		{ID: 20, WorkID: 3, Title: "Содержание", StartPage: 1, EndPage: 4, Slug: "soderzhanie"},
	}

	eds := src.Editions.(*fakeEditions)
	eds.all = []*models.Edition{eds.byID[7]}
	eds.works[7] = []*models.Work{works.byID[1], works.byID[3]}
	return src
}

func TestWorkTextListsChaptersWithTextLinks(t *testing.T) {
	doc, err := llmVolumeSource().WorkText(context.Background(), 1)
	if err != nil {
		t.Fatalf("WorkText: %v", err)
	}
	base := "https://lib.example.org/works/1-lenin-t42/chapters/"
	for _, line := range []string{
		"# Полное собрание сочинений. Том 42\n",
		"В. И. Ленин\n",
		"В. И. Ленин. Полное собрание сочинений\n",
		"Адрес тома: https://lib.example.org/works/1-lenin-t42\n",
		// Печатные номера: внутренние + page_offset (2).
		"- [Государство и революция](" + base + "10-gosudarstvo-i-revolyuciya.md) — с. 7—122\n",
		"  - [Глава I \\[черновик\\]](" + base + "13-glava-i.md) — с. 9—32\n",
		"- [Примечания](" + base + "14-primechaniya.md) — с. 123—132 (аппарат издания)\n",
	} {
		if !strings.Contains(doc.Body, line) {
			t.Errorf("нет строки %q:\n%s", line, doc.Body)
		}
	}
	if doc.Canonical != "https://lib.example.org/works/1-lenin-t42" || !doc.NoIndex {
		t.Errorf("Canonical %q, NoIndex %v", doc.Canonical, doc.NoIndex)
	}
}

// Передние листы — своим разделом, ссылками на тексты их глав. Номеров
// страниц нет: у передних листов своя, римская нумерация.
func TestWorkTextListsFrontMatter(t *testing.T) {
	doc, err := llmVolumeSource().WorkText(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"## Предваряющие материалы\n",
		"### Титул и содержание\n",
		"- [Содержание](https://lib.example.org/works/3-lenin-t42-titul/chapters/20-soderzhanie.md)\n",
	} {
		if !strings.Contains(doc.Body, line) {
			t.Errorf("нет строки %q:\n%s", line, doc.Body)
		}
	}
}

// Передние листы под своим адресом — 410, как у HTML-карточки (Work()).
func TestWorkTextFrontMatterIsNotFound(t *testing.T) {
	if _, err := llmVolumeSource().WorkText(context.Background(), 3); !errors.Is(err, ErrNotFound) {
		t.Errorf("ошибка %v, ожидалась ErrNotFound", err)
	}
}

func TestWorkTextMissingIsNotFound(t *testing.T) {
	if _, err := llmVolumeSource().WorkText(context.Background(), 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("ошибка %v, ожидалась ErrNotFound", err)
	}
}

// Цитата под заголовком llms.txt — описание экземпляра: чужая читальня не
// представляется нейросетям «библиотекой собраний сочинений».
func TestLLMsTextUsesSiteDescription(t *testing.T) {
	site.Set("Тестовая", "Журналы двадцатых годов.")
	t.Cleanup(func() { site.Set("", "") })
	doc, err := llmVolumeSource().LLMsText(context.Background())
	if err != nil {
		t.Fatalf("LLMsText: %v", err)
	}
	if !strings.HasPrefix(doc.Body, "# Тестовая\n\n> Журналы двадцатых годов. ") {
		t.Errorf("заголовок или цитата не из экземпляра:\n%.200s", doc.Body)
	}
	if strings.Contains(doc.Body, "собраний сочинений") {
		t.Errorf("описание по умолчанию прошито:\n%.200s", doc.Body)
	}
}

func TestLLMsTextListsEveryVolume(t *testing.T) {
	doc, err := llmVolumeSource().LLMsText(context.Background())
	if err != nil {
		t.Fatalf("LLMsText: %v", err)
	}
	body := doc.Body
	if !strings.HasPrefix(body, "# Читальня\n\n> ") {
		t.Errorf("не по llmstxt.org — нет заголовка и цитаты:\n%s", body)
	}
	for _, line := range []string{
		"## Как читать тексты читальни\n",
		"https://lib.example.org/works/{том}/chapters/{глава}.md",
		"## Издания\n",
		"### В. И. Ленин. Полное собрание сочинений\n",
		"- [Полное собрание сочинений. Том 42](https://lib.example.org/works/1-lenin-t42.md)\n",
		"## Отдельные книги\n",
		"- [Отдельная \\[книга\\]](https://lib.example.org/works/50-otdelnaya-kniga.md)\n",
	} {
		if !strings.Contains(body, line) {
			t.Errorf("нет строки %q:\n%s", line, body)
		}
	}
	if strings.Contains(body, "lenin-t42-titul") {
		t.Errorf("передние листы попали в llms.txt:\n%s", body)
	}
	for _, want := range []string{
		"- Понятие предметного указателя: https://lib.example.org/concepts/{слаг}.md",
		"- Все понятия указателя: https://lib.example.org/concepts.md",
	} {
		if !strings.Contains(doc.Body, want) {
			t.Errorf("llms.txt: нет %q", want)
		}
	}
	// Обычная страница главы открывается только сборщикам из карты
	// $is_crawler nginx; .md — всем. Обещать обычную ссылку «любому чату»
	// нельзя: DeepSeek или GigaChat получат пустую оболочку SPA.
	if !strings.Contains(body, "открывается сборщикам ChatGPT, Claude и Perplexity") ||
		!strings.Contains(body, ".md открывается любому чату") {
		t.Errorf("строка про обычную ссылку не называет, кому она открывается:\n%s", body)
	}
	if got := strings.Count(body, "\n- ["); got != 2 {
		t.Errorf("ссылок на тома %d, ожидалось 2", got)
	}
	if doc.NoIndex || doc.Canonical != "" {
		t.Errorf("llms.txt не дубль страницы: NoIndex %v, Canonical %q", doc.NoIndex, doc.Canonical)
	}
}

func TestLLMsTextMentionsMCP(t *testing.T) {
	doc, err := llmVolumeSource().LLMsText(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.Body, "https://lib.example.org/mcp") {
		t.Errorf("llms.txt не называет MCP-сервер:\n%s", doc.Body)
	}
}

func TestMdLinkTextEscapesBrackets(t *testing.T) {
	if got := mdLinkText(`a [b] \c`); got != `a \[b\] \\c` {
		t.Errorf("%q", got)
	}
}
