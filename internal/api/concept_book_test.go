package api

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"proofreader/internal/models"
	"proofreader/internal/seo"
	"proofreader/pkg/book"
)

// conceptPages — полосы нескольких томов: pageStub не различает работы.
type conceptPages map[int64][]*models.Page

func (p conceptPages) GetPagesByNumbers(ctx context.Context, workID int64, numbers []int) ([]*models.Page, error) {
	var out []*models.Page
	for _, n := range numbers {
		for _, page := range p[workID] {
			if page.PageNumber == n {
				out = append(out, page)
			}
		}
	}
	return out, nil
}

type conceptChapters map[int64][]*models.Chapter

func (c conceptChapters) ListByWorkHierarchical(ctx context.Context, workID int64) ([]*models.Chapter, error) {
	return c[workID], nil
}

func rubricRef(id int64, order int, path []string, vol, from, to int) *models.IndexReference {
	r := &models.IndexReference{ID: id, ArticleID: 1, OrderNumber: order, VolumeNumber: vol, PageStart: from, PageEnd: to, RubricPath: path}
	if len(path) > 0 {
		r.Rubric = path[len(path)-1]
	}
	return r
}

var pageTime = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

// Фикстура: т. 13 (работа 15, смещение 0) и т. 23 (работа 47, смещение 10 —
// печатная 46 лежит на внутренней 36). Т. 31 в читальне нет. У т. 13 с. 46 и
// т. 23 с. 46 обе сноски называются [^1] — печатный номер один.
func conceptBookFixture() *ConceptBookSource {
	idx := &fakeIndexStore{
		concept: &models.IndexConcept{ID: 8, Slug: "abstraktnyj-trud", Title: "Абстрактный труд", UpdatedAt: pageTime,
			Articles: []*models.IndexArticle{{ID: 1, EditionID: 1, EditionTitle: "К. Маркс и Ф. Энгельс. Сочинения, 2-е изд.",
				Kind: models.IndexConceptKindArticle,
				References: []*models.IndexReference{
					rubricRef(1, 1, []string{"определение"}, 23, 46, 47),
					rubricRef(2, 2, []string{"как субстанция стоимости"}, 23, 47, 47),
					rubricRef(3, 3, []string{"определение"}, 13, 46, 46),
					rubricRef(4, 4, []string{"определение"}, 31, 5, 5),
					rubricRef(5, 5, []string{"определение"}, 23, 900, 901),
				}}}},
		volumes: []models.VolumeLocation{
			{VolumeNumber: 13, WorkID: 15, WorkSlug: "mae-t13", PageOffset: 0, MaxPage: 100},
			{VolumeNumber: 23, WorkID: 47, WorkSlug: "mae-t23", PageOffset: 10, MaxPage: 100},
		},
	}
	pages := conceptPages{
		15: {{ID: 1, WorkID: 15, PageNumber: 46, ContentMarkdown: "Текст т13 с46[^1]\n\n[^1]: сноска т13", UpdatedAt: pageTime}},
		47: {
			{ID: 2, WorkID: 47, PageNumber: 36, ContentMarkdown: "Текст т23 с46[^1]\n\n[^1]: сноска т23", UpdatedAt: pageTime},
			{ID: 3, WorkID: 47, PageNumber: 37, ContentMarkdown: "Текст т23 с47", UpdatedAt: pageTime.Add(time.Hour)},
		},
	}
	chapters := conceptChapters{
		15: {{ID: 1524, Title: "Глава первая. Товар", StartPage: 1, EndPage: 100}},
		47: {{ID: 2066, Title: "Книга первая", StartPage: 1, EndPage: 100}},
	}
	return NewConceptBookSource(idx, pages, chapters, "https://lib.example.org")
}

func conceptMarkdown(t *testing.T, cb *seo.ConceptBook) string {
	t.Helper()
	text, _ := book.MarkdownWithPageStarts(cb.Book)
	return text
}

func TestConceptBookOrdersPlacesByRubricThenVolume(t *testing.T) {
	cb, err := conceptBookFixture().Concept(context.Background(), "abstraktnyj-trud", nil)
	if err != nil {
		t.Fatal(err)
	}
	text := conceptMarkdown(t, cb)
	order := []string{
		"## определение",
		"### [т. 13, с. 46](https://lib.example.org/works/15-mae-t13/pages/46) — Глава первая. Товар",
		"### [т. 23, с. 46—47](https://lib.example.org/works/47-mae-t23/pages/36) — Книга первая",
		"### т. 23, с. 900—901 — этих страниц нет в читальне",
		"### т. 31, с. 5 — тома нет в читальне",
		"## как субстанция стоимости",
		"### [т. 23, с. 47](https://lib.example.org/works/47-mae-t23/pages/37) — Книга первая",
	}
	at := -1
	for _, line := range order {
		i := strings.Index(text, "\n"+line+"\n")
		if i < 0 {
			t.Fatalf("нет строки %q:\n%s", line, text)
		}
		if i < at {
			t.Errorf("строка %q стоит раньше предыдущей:\n%s", line, text)
		}
		at = i
	}
}

func TestConceptBookPrintsRepeatedPageOnce(t *testing.T) {
	cb, err := conceptBookFixture().Concept(context.Background(), "abstraktnyj-trud", nil)
	if err != nil {
		t.Fatal(err)
	}
	text := conceptMarkdown(t, cb)
	if n := strings.Count(text, "Текст т23 с47"); n != 1 {
		t.Errorf("текст полосы 47 напечатан %d раз, ожидался один:\n%s", n, text)
	}
	want := "[47]\n\nТекст страницы приведён выше: «определение», т. 23, с. 46—47.\n"
	if !strings.Contains(text, want) {
		t.Errorf("нет строки-ссылки с меткой страницы %q:\n%s", want, text)
	}
	// Разных полос три: т. 13 с. 46, т. 23 с. 46 и с. 47.
	if cb.Pages != 3 {
		t.Errorf("Pages = %d, ожидалось 3", cb.Pages)
	}
}

func TestConceptBookKeepsFootnotesOfSamePrintedPageApart(t *testing.T) {
	cb, err := conceptBookFixture().Concept(context.Background(), "abstraktnyj-trud", nil)
	if err != nil {
		t.Fatal(err)
	}
	text := conceptMarkdown(t, cb)
	defs := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "[^46-") {
			name, _, _ := strings.Cut(line, "]:")
			if defs[name] {
				t.Errorf("определение сноски %s повторяется:\n%s", name, text)
			}
			defs[name] = true
		}
	}
	if len(defs) != 2 {
		t.Errorf("определений сносок печатной 46: %d, ожидалось 2:\n%s", len(defs), text)
	}
}

func TestConceptBookCountsAndOutline(t *testing.T) {
	cb, err := conceptBookFixture().Concept(context.Background(), "abstraktnyj-trud", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cb.Places != 5 || cb.Present != 3 {
		t.Errorf("Places %d Present %d, ожидалось 5 и 3", cb.Places, cb.Present)
	}
	if len(cb.Rubrics) != 2 {
		t.Fatalf("подрубрик %d, ожидалось 2: %+v", len(cb.Rubrics), cb.Rubrics)
	}
	def, sub := cb.Rubrics[0], cb.Rubrics[1]
	if strings.Join(def.Path, "/") != "определение" || def.Places != 4 || def.FirstPage != 0 {
		t.Errorf("определение: %+v", def)
	}
	if got := def.Volumes; len(got) != 3 || got[0] != 13 || got[1] != 23 || got[2] != 31 {
		t.Errorf("тома определения: %v", got)
	}
	// Полосы книги по порядку: т13 с46 (0), т23 с46 (1), т23 с47 (2), повтор с47 (3).
	if strings.Join(sub.Path, "/") != "как субстанция стоимости" || sub.Places != 1 || sub.FirstPage != 3 {
		t.Errorf("как субстанция стоимости: %+v", sub)
	}
	if !cb.Modified.Equal(pageTime.Add(time.Hour)) {
		t.Errorf("Modified %v, ожидалось время самой свежей полосы", cb.Modified)
	}
}

func TestConceptBookNestedRubricsAndOwnPlacesFirst(t *testing.T) {
	src := conceptBookFixture()
	idx := src.index.(*fakeIndexStore)
	idx.concept.Articles[0].References = []*models.IndexReference{
		rubricRef(1, 1, []string{"КПСС — съезды", "II съезд"}, 13, 46, 46),
		rubricRef(2, 2, []string{"КПСС — съезды"}, 23, 46, 46),
	}
	cb, err := src.Concept(context.Background(), "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	text := conceptMarkdown(t, cb)
	own := strings.Index(text, "### [т. 23, с. 46]")
	nested := strings.Index(text, "### II съезд")
	if own < 0 || nested < 0 || own > nested {
		t.Errorf("свои адреса раздела обязаны идти раньше вложенной подрубрики:\n%s", text)
	}
	if !strings.Contains(text, "#### [т. 13, с. 46]") {
		t.Errorf("адрес вложенной подрубрики не на четвёртом уровне:\n%s", text)
	}
	if len(cb.Rubrics) != 2 || len(cb.Rubrics[1].Path) != 2 || cb.Rubrics[0].Places != 2 {
		t.Errorf("оглавление: %+v", cb.Rubrics)
	}
}

// Две статьи разных изданий: том 23 Маркса и том 23 Ленина — разные работы,
// и адрес каждой статьи разрешается по СВОЕМУ изданию. Третья статья —
// отсылка без адресов — раздела не получает. Полутом печатается как в
// указателе: «т. 45, 2».
func TestConceptBookArticlesResolveAgainstOwnEdition(t *testing.T) {
	src := conceptBookFixture()
	idx := src.index.(*fakeIndexStore)
	part := "2"
	idx.concept.Articles = []*models.IndexArticle{
		{ID: 1, EditionID: 1, EditionTitle: "Маркс и Энгельс", Kind: models.IndexConceptKindArticle,
			References: []*models.IndexReference{rubricRef(1, 1, nil, 23, 46, 46)}},
		{ID: 2, EditionID: 4, EditionTitle: "Ленин", Kind: models.IndexConceptKindArticle,
			References: []*models.IndexReference{
				rubricRef(2, 1, nil, 23, 46, 46),
				{ID: 3, ArticleID: 2, OrderNumber: 2, VolumeNumber: 45, VolumePart: &part, PageStart: 46, PageEnd: 46},
			}},
		{ID: 3, EditionID: 4, EditionTitle: "Отсылка", Kind: models.IndexConceptKindRedirect},
	}
	idx.volumesByEdition = map[int64][]models.VolumeLocation{
		1: {{VolumeNumber: 23, WorkID: 47, WorkSlug: "mae-t23", PageOffset: 10, MaxPage: 100}},
		4: {
			{VolumeNumber: 23, WorkID: 15, WorkSlug: "lenin-t23", PageOffset: 0, MaxPage: 100},
			{VolumeNumber: 45, VolumePart: &part, WorkID: 15, WorkSlug: "lenin-t45-2", PageOffset: 0, MaxPage: 100},
		},
	}
	cb, err := src.Concept(context.Background(), "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	text := conceptMarkdown(t, cb)
	for _, want := range []string{
		"## Маркс и Энгельс\n\n### [т. 23, с. 46](https://lib.example.org/works/47-mae-t23/pages/36)",
		"## Ленин\n\n### [т. 23, с. 46](https://lib.example.org/works/15-lenin-t23/pages/46)",
		"### [т. 45, 2, с. 46](https://lib.example.org/works/15-lenin-t45-2/pages/46)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("нет %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "## Отсылка") {
		t.Errorf("статья без адресов получила раздел:\n%s", text)
	}
}

func TestConceptBookMissingIsNotFound(t *testing.T) {
	src := conceptBookFixture()
	src.index.(*fakeIndexStore).concept = nil
	_, err := src.Concept(context.Background(), "net-takogo", nil)
	if !errors.Is(err, seo.ErrNotFound) {
		t.Fatalf("ожидался seo.ErrNotFound, получено %v", err)
	}
}

// Адреса без подрубрики — одна строка оглавления с пустым путём, на месте их
// первого появления в книге: иначе сводка «мест: N» считала бы их, а
// оглавление не называло ни одного.
func TestConceptBookPlacesWithoutRubricGetOutlineRow(t *testing.T) {
	src := conceptBookFixture()
	idx := src.index.(*fakeIndexStore)
	idx.concept.Articles[0].References = []*models.IndexReference{
		rubricRef(1, 1, nil, 13, 46, 46),
		rubricRef(2, 2, []string{"определение"}, 23, 46, 46),
		rubricRef(3, 3, nil, 31, 5, 5),
	}
	cb, err := src.Concept(context.Background(), "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cb.Rubrics) != 2 {
		t.Fatalf("строк оглавления %d, ожидалось 2: %+v", len(cb.Rubrics), cb.Rubrics)
	}
	none, def := cb.Rubrics[0], cb.Rubrics[1]
	if len(none.Path) != 0 || none.Places != 2 || none.FirstPage != 0 ||
		len(none.Volumes) != 2 || none.Volumes[0] != 13 || none.Volumes[1] != 31 {
		t.Errorf("без подрубрики: %+v", none)
	}
	if strings.Join(def.Path, "/") != "определение" || def.Places != 1 || def.FirstPage != 1 {
		t.Errorf("определение: %+v", def)
	}
}

func TestConceptBookRedirectHasNoPlaces(t *testing.T) {
	src := conceptBookFixture()
	a := src.index.(*fakeIndexStore).concept.Articles[0]
	a.Kind, a.References, a.ArticleMarkdown = models.IndexConceptKindRedirect, nil, "*См.* **Труд.**"
	cb, err := src.Concept(context.Background(), "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cb.Places != 0 || len(cb.Book.Sections) != 0 || len(cb.Rubrics) != 0 {
		t.Errorf("у отсылки мест быть не должно: %+v", cb)
	}
}

// Подрубрика сужает книгу до своих адресов (и вложенных): сводка, оглавление
// и полосы — только её. Порядок групп — по полному указателю, как в потоке.
func TestConceptBookNarrowsToRubric(t *testing.T) {
	cb, err := conceptBookFixture().Concept(context.Background(), "abstraktnyj-trud", []string{"как субстанция стоимости"})
	if err != nil {
		t.Fatal(err)
	}
	text := conceptMarkdown(t, cb)
	if strings.Contains(text, "## определение") || strings.Contains(text, "Текст т13 с46") {
		t.Errorf("в книгу попали адреса другой подрубрики:\n%s", text)
	}
	// Полоса 47 под «определением» здесь не печаталась — значит, и ссылки
	// «приведён выше» нет: текст стоит сам.
	if !strings.Contains(text, "## как субстанция стоимости") || !strings.Contains(text, "Текст т23 с47") {
		t.Errorf("нет адреса подрубрики с текстом:\n%s", text)
	}
	if cb.Places != 1 || cb.Present != 1 || cb.Pages != 1 || len(cb.Rubrics) != 1 {
		t.Errorf("Places %d Present %d Pages %d Rubrics %+v", cb.Places, cb.Present, cb.Pages, cb.Rubrics)
	}
}

// Путь-префикс накрывает вложенные подрубрики, как фильтр потока понятия.
func TestConceptBookRubricPrefixKeepsNested(t *testing.T) {
	src := conceptBookFixture()
	idx := src.index.(*fakeIndexStore)
	idx.concept.Articles[0].References = []*models.IndexReference{
		rubricRef(1, 1, []string{"КПСС — съезды", "II съезд"}, 13, 46, 46),
		rubricRef(2, 2, []string{"КПСС — съезды"}, 23, 46, 46),
		rubricRef(3, 3, []string{"другое"}, 23, 47, 47),
	}
	cb, err := src.Concept(context.Background(), "x", []string{"КПСС — съезды"})
	if err != nil {
		t.Fatal(err)
	}
	if text := conceptMarkdown(t, cb); cb.Places != 2 || strings.Contains(text, "другое") {
		t.Errorf("Places %d:\n%s", cb.Places, text)
	}
}

func TestConceptBookUnknownRubric(t *testing.T) {
	_, err := conceptBookFixture().Concept(context.Background(), "abstraktnyj-trud", []string{"нет такой"})
	if !errors.Is(err, seo.ErrNoSuchRubric) {
		t.Fatalf("ожидался seo.ErrNoSuchRubric, получено %v", err)
	}
}
