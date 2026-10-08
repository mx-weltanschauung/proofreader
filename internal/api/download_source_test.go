package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/pkg/book"
	"proofreader/pkg/markdown"
)

// fakeEditionStore уже объявлен в edition_handler_test.go — переиспользуем
// его, а не заводим второй подставной тип на тот же интерфейс.

func testPage(number int, text string, status models.PageStatus) *models.Page {
	return &models.Page{
		ID: int64(number), WorkID: 1, PageNumber: number,
		ContentMarkdown: text, Status: status,
		UpdatedAt: time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC),
	}
}

// downloadFixture — том из пяти страниц с одной главой на страницах 2—4,
// смещением печатной нумерации 100 и изданием.
func downloadFixture() *DownloadSource {
	editionID := int64(7)
	works := &fakeWorkStore{}
	works.getFn = func(_ context.Context, id int64) (*models.Work, error) {
		return &models.Work{
			ID: 1, Title: "Том 23", Author: "К. Маркс", Language: "ru",
			EditionID: &editionID, VolumeNumber: intPtr(23), PageOffset: 100,
			// Slug несёт настоящий репозиторий (задача 4) — фикстура без
			// него проверяла бы то, чего в жизни не бывает.
			Slug: "marks-t23",
		}, nil
	}
	chapters := &fakeChapterStore{
		listHierarchicalFn: func(context.Context, int64) ([]*models.Chapter, error) {
			return []*models.Chapter{{
				// Slug — pkg/slug.Chapter("Глава"), посчитан настоящим
				// пакетом, а не на глаз.
				ID: 10, WorkID: 1, Title: "Глава", StartPage: 2, EndPage: 4, Slug: "glava",
			}}, nil
		},
		getByIDFn: func(_ context.Context, id int64) (*models.Chapter, error) {
			return &models.Chapter{ID: 10, WorkID: 1, Title: "Глава", StartPage: 2, EndPage: 4, Slug: "glava"}, nil
		},
	}
	pages := &fakePageStore{
		listByWorkFn: func(context.Context, int64) ([]*models.Page, error) {
			return []*models.Page{
				testPage(1, "Титул.", models.PageStatusProofread),
				testPage(2, "Начало главы.", models.PageStatusProofread),
				testPage(3, "Середина.", models.PageStatusMachineProofread),
				testPage(4, "Конец главы.", models.PageStatusNotProofread),
				testPage(5, "Хвост тома.", models.PageStatusEmpty),
			}, nil
		},
	}
	pages.getPageRangeFn = func(_ context.Context, _ int64, from, to int) ([]*models.Page, error) {
		all, _ := pages.listByWorkFn(context.Background(), 1)
		var out []*models.Page
		for _, p := range all {
			if p.PageNumber >= from && p.PageNumber <= to {
				out = append(out, p)
			}
		}
		return out, nil
	}

	return NewDownloadSource(works, chapters, pages,
		&fakeEditionStore{editions: []*models.Edition{{ID: 7, Title: "Сочинения"}}},
		&fakeCollectionStoreForDownload{},
		markdown.NewRenderer(), "https://lib.example.org")
}

func intPtr(v int) *int { return &v }

func TestDownloadWorkCoversEveryPage(t *testing.T) {
	b, err := downloadFixture().Work(context.Background(), 1)
	if err != nil {
		t.Fatalf("Work() error = %v", err)
	}

	if got := b.PageCount(); got != 5 {
		t.Errorf("страниц в книге = %d, хотел 5 (том целиком, включая непокрытые главой)", got)
	}
}

func TestDownloadWorkUsesPrintedPageNumbers(t *testing.T) {
	b, err := downloadFixture().Work(context.Background(), 1)
	if err != nil {
		t.Fatalf("Work() error = %v", err)
	}

	if b.Meta.PageFrom != 101 || b.Meta.PageTo != 105 {
		t.Errorf("границы = %d—%d, хотел 101—105 (page_offset = 100)", b.Meta.PageFrom, b.Meta.PageTo)
	}
}

func TestDownloadWorkFillsMeta(t *testing.T) {
	b, err := downloadFixture().Work(context.Background(), 1)
	if err != nil {
		t.Fatalf("Work() error = %v", err)
	}

	if b.Meta.Edition != "Сочинения" {
		t.Errorf("издание = %q", b.Meta.Edition)
	}
	if b.Meta.Volume != "Том 23" {
		t.Errorf("том = %q", b.Meta.Volume)
	}
	// Адрес источника на титульном листе выгрузки — канон со слагом тома
	// (задача 8), а не голый номер, отвечающий 301 после задачи 10.
	if b.Meta.URL != "https://lib.example.org/works/1-marks-t23" {
		t.Errorf("ссылка = %q", b.Meta.URL)
	}
	if !b.Meta.Modified.Equal(time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("дата = %v, хотел max(updated_at)", b.Meta.Modified)
	}
}

// Пустая страница в знаменатель вычитки не входит: их в томе одна из пяти.
func TestDownloadWorkCountsProofreadStatuses(t *testing.T) {
	b, err := downloadFixture().Work(context.Background(), 1)
	if err != nil {
		t.Fatalf("Work() error = %v", err)
	}

	got := b.Meta.Proofread
	if got.Total != 4 || got.Human != 2 || got.Machine != 1 || got.Raw != 1 {
		t.Errorf("состояние вычитки = %+v, хотел Total 4, Human 2, Machine 1, Raw 1", got)
	}
}

// Обёртка постраничного документа обязана быть снята ещё здесь — иначе она
// уедет во все три HTML-формата.
func TestDownloadStripsPageDocumentWrapper(t *testing.T) {
	b, err := downloadFixture().Work(context.Background(), 1)
	if err != nil {
		t.Fatalf("Work() error = %v", err)
	}

	var seen int
	var walk func(sections []book.Section)
	walk = func(sections []book.Section) {
		for _, s := range sections {
			for _, blk := range s.Blocks {
				if blk.Child != nil {
					walk([]book.Section{*blk.Child})
					continue
				}
				for _, p := range blk.Pages {
					seen++
					for _, bad := range []string{"<!DOCTYPE", "<html", "<head", "GENERATOR"} {
						if strings.Contains(p.HTML, bad) {
							t.Errorf("страница %d содержит %q", p.Printed, bad)
						}
					}
				}
			}
		}
	}
	walk(b.Sections)
	if seen == 0 {
		t.Fatal("ни одной страницы не проверено")
	}
}

func TestDownloadChapterCoversOnlyItsRange(t *testing.T) {
	b, err := downloadFixture().Chapter(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("Chapter() error = %v", err)
	}

	if got := b.PageCount(); got != 3 {
		t.Errorf("страниц = %d, хотел 3 (страницы 2—4)", got)
	}
	if b.Meta.PageFrom != 102 || b.Meta.PageTo != 104 {
		t.Errorf("границы = %d—%d, хотел 102—104", b.Meta.PageFrom, b.Meta.PageTo)
	}
	if b.Meta.Title != "Глава" {
		t.Errorf("название = %q, хотел название главы", b.Meta.Title)
	}
	// Адрес источника на титульном листе выгрузки главы — канон со слагом
	// обеих частей (тома и главы): по нему читатель возвращается в
	// читальню, и после задачи 10 голый номер ответил бы 301.
	if b.Meta.URL != "https://lib.example.org/works/1-marks-t23/chapters/10-glava" {
		t.Errorf("ссылка = %q", b.Meta.URL)
	}
}

func TestDownloadChapterNotFound(t *testing.T) {
	src := downloadFixture()
	src.chapters.(*fakeChapterStore).getByIDFn = func(context.Context, int64) (*models.Chapter, error) {
		return nil, errors.New("нет такой главы")
	}

	if _, err := src.Chapter(context.Background(), 1, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, хотел ErrNotFound", err)
	}
}

// Глава принадлежит другому тому — путь не должен открывать чужой текст.
func TestDownloadChapterRejectsForeignWork(t *testing.T) {
	src := downloadFixture()
	src.chapters.(*fakeChapterStore).getByIDFn = func(context.Context, int64) (*models.Chapter, error) {
		return &models.Chapter{ID: 10, WorkID: 42, Title: "Чужая", StartPage: 1, EndPage: 2}, nil
	}

	if _, err := src.Chapter(context.Background(), 1, 10); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, хотел ErrNotFound для главы чужого тома", err)
	}
}

// Примечания собираются по секции верхнего уровня, а не на всю книгу.
func TestDownloadCollectsNotesPerTopSection(t *testing.T) {
	src := downloadFixture()
	src.pages.(*fakePageStore).listByWorkFn = func(context.Context, int64) ([]*models.Page, error) {
		return []*models.Page{
			testPage(1, "Текст[^1]\n\n[^1]: примечание", models.PageStatusProofread),
		}, nil
	}
	src.pages.(*fakePageStore).getPageRangeFn = func(context.Context, int64, int, int) ([]*models.Page, error) {
		return []*models.Page{testPage(1, "Текст[^1]\n\n[^1]: примечание", models.PageStatusProofread)}, nil
	}
	src.chapters.(*fakeChapterStore).listHierarchicalFn = func(context.Context, int64) ([]*models.Chapter, error) {
		return nil, nil
	}

	b, err := src.Work(context.Background(), 1)
	if err != nil {
		t.Fatalf("Work() error = %v", err)
	}

	if len(b.Sections) == 0 {
		t.Fatal("книга без секций")
	}
	if !strings.Contains(b.Sections[0].NotesHTML, "примечание") {
		t.Errorf("примечания не легли в секцию:\n%s", b.Sections[0].NotesHTML)
	}
}

// notesFractionFixture — том с единственной сноской, чей текст содержит
// дробь ("2/3") и длинное тире ("---"). html.CommonFlags у рендерера
// включает smartypants: дробь превращается в буквальный именованный
// XML-объект "&frasl;" (vendor/.../smartypants.go:308), тире — в "&mdash;".
// Текст страницы такие объекты переживает благодаря разбору внутри
// CollectPages (fragment() в её хвосте), а NotesHTML — благодаря такому же
// разбору внутри markdown.RenderNotes — оба контракт pkg/markdown. До
// починки разбор здесь, в renderRange, отсутствовал вовсе: NotesHTML шёл в
// обход него сырым (markdown.RenderNotes(notes) напрямую, без какой-либо
// нормализации), унося необъявленные для XML сущности прямиком в файл.
// Строка ниже — тот же самый прямой вызов, но теперь безопасный: разбор из
// этого дефекта переехал внутрь RenderNotes и применяется к его результату
// сам, до возврата.
func notesFractionFixture() *DownloadSource {
	src := downloadFixture()
	src.pages.(*fakePageStore).listByWorkFn = func(context.Context, int64) ([]*models.Page, error) {
		return []*models.Page{
			testPage(1, "Текст.[^1]\n\n[^1]: Здесь дробь 2/3 фунта, и тире --- в придачу.",
				models.PageStatusProofread),
		}, nil
	}
	src.chapters.(*fakeChapterStore).listHierarchicalFn = func(context.Context, int64) ([]*models.Chapter, error) {
		return nil, nil
	}
	return src
}

// Критический дефект, найденный на реальном скачивании тома 23: EPUB
// трёх из двенадцати файлов не проходил строгий разбор XML из-за
// необъявленной сущности "&frasl;" в NotesHTML. Тест ловит её на уровне
// DownloadSource — до всякого writer'а — разбором строгим encoding/xml.
func TestDownloadNotesHTMLHasNoUndefinedXMLEntities(t *testing.T) {
	b, err := notesFractionFixture().Work(context.Background(), 1)
	if err != nil {
		t.Fatalf("Work() error = %v", err)
	}
	if len(b.Sections) == 0 || b.Sections[0].NotesHTML == "" {
		t.Fatalf("книга без примечаний: %+v", b.Sections)
	}
	notesHTML := b.Sections[0].NotesHTML

	// Именованные объекты smartypants обязаны быть уже разрешены в символы.
	for _, entity := range []string{"&frasl;", "&mdash;"} {
		if strings.Contains(notesHTML, entity) {
			t.Errorf("NotesHTML сохранил необъявленную для XML сущность %q:\n%s", entity, notesHTML)
		}
	}
	if err := xml.Unmarshal([]byte(notesHTML), new(interface{})); err != nil {
		t.Errorf("NotesHTML не разбирается как строгий XML: %v\n%s", err, notesHTML)
	}
}

// Тот же дефект уровнем выше — конкретно там, где он и был обнаружен на
// реальных данных: строгий XML-парсер каждого сгенерированного файла EPUB.
// До починки секция с этой сноской не проходила разбор.
func TestDownloadEPUBSectionFilesParseStrictlyWithFractionInNotes(t *testing.T) {
	b, err := notesFractionFixture().Work(context.Background(), 1)
	if err != nil {
		t.Fatalf("Work() error = %v", err)
	}

	var buf bytes.Buffer
	if err := (book.EPUBWriter{}).Write(&buf, b); err != nil {
		t.Fatalf("EPUBWriter.Write() error = %v", err)
	}

	r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("EPUB не открывается как zip: %v", err)
	}

	checked := 0
	for _, f := range r.File {
		if !strings.HasSuffix(f.Name, ".xhtml") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("открыть %s: %v", f.Name, err)
		}
		var content bytes.Buffer
		_, err = content.ReadFrom(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("прочитать %s: %v", f.Name, err)
		}
		if err := xml.Unmarshal(content.Bytes(), new(interface{})); err != nil {
			t.Errorf("%s не разбирается как строгий XML: %v", f.Name, err)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("в архиве не нашлось ни одного .xhtml файла для проверки")
	}
}

// flattenPages обходит дерево секций в порядке чтения и собирает страницы —
// включая вложенные секции. Тот же приём, что и walk() выше, но как
// переиспользуемый хелпер для проверок Internal/Printed.
func flattenPages(sections []book.Section) []book.Page {
	var out []book.Page
	var walk func(sections []book.Section)
	walk = func(secs []book.Section) {
		for _, s := range secs {
			for _, blk := range s.Blocks {
				if blk.Child != nil {
					walk([]book.Section{*blk.Child})
					continue
				}
				out = append(out, blk.Pages...)
			}
		}
	}
	walk(sections)
	return out
}

// Печатный номер (Internal + page_offset) — то, что реально видит читатель
// как номер страницы в скачанном файле. Регрессия, роняющая +offset в
// renderRange, оставила бы все девять исходных тестов зелёными: они проверяют
// только Meta.PageFrom/PageTo, которые workMeta считает отдельным
// выражением от сырого среза страниц, а не Printed на самих страницах.
func TestDownloadWorkSetsPrintedAndInternalPageNumbers(t *testing.T) {
	b, err := downloadFixture().Work(context.Background(), 1)
	if err != nil {
		t.Fatalf("Work() error = %v", err)
	}

	pages := flattenPages(b.Sections)
	if len(pages) != 5 {
		t.Fatalf("страниц в потоке = %d, хотел 5", len(pages))
	}

	// page_offset = 100 в фикстуре — достаточно большой сдвиг, чтобы спутать
	// Internal с Printed было видно сразу, без арифметики в уме.
	checks := []struct {
		label        string
		idx          int
		wantInternal int
		wantPrinted  int
	}{
		{"первая", 0, 1, 101},
		{"средняя", 2, 3, 103},
		{"последняя", 4, 5, 105},
	}
	for _, c := range checks {
		p := pages[c.idx]
		if p.Internal != c.wantInternal {
			t.Errorf("%s страница: Internal = %d, хотел %d", c.label, p.Internal, c.wantInternal)
		}
		if p.Printed != c.wantPrinted {
			t.Errorf("%s страница: Printed = %d, хотел %d", c.label, p.Printed, c.wantPrinted)
		}
	}
}

// ---- topLevelNodes: раскладка глав верхнего уровня и зазоров ----
//
// buildSections зовёт CollectPages по каждому узлу отдельно (сноски по
// секции верхнего уровня, не по всей книге), поэтому неверная раскладка
// узлов — это либо потерянные страницы, либо повторно провалидированный
// диапазон. На живом корпусе раскладка нетривиальна в каждом из 25 томов
// (у тома 1 — 57 непокрытых хвостовых страниц, у тома 8 — 116), а тесты
// DownloadSource упражняют только вырожденный случай одной главы с зазором
// до и после. Ниже — прямые проверки функции на составных случаях.

// coverage разворачивает верхнеуровневые узлы в список покрытых страниц —
// без учёта Children: для соседей верхнего уровня диапазон [Start,End]
// самого узла уже описывает всё его покрытие целиком.
func coverage(nodes []book.Node) []int {
	var out []int
	for _, n := range nodes {
		for p := n.Start; p <= n.End; p++ {
			out = append(out, p)
		}
	}
	return out
}

func sequence(from, to int) []int {
	var out []int
	for p := from; p <= to; p++ {
		out = append(out, p)
	}
	return out
}

// Дыра между двумя главами верхнего уровня становится отдельным узлом с
// честным именем «Без заглавия», а не пропадает и не остаётся пустой строкой
// (пустая строка в оглавлении EPUB/FB2/HTML/Markdown выглядит как баг, а не
// как «здесь непричисленные к главам страницы»).
func TestTopLevelNodesFillsGapBetweenSiblings(t *testing.T) {
	chapters := []book.Node{
		{Title: "Первая", Start: 1, End: 3},
		{Title: "Вторая", Start: 7, End: 9},
	}

	nodes := topLevelNodes(chapters, 1, 9)

	if got, want := coverage(nodes), sequence(1, 9); !reflect.DeepEqual(got, want) {
		t.Fatalf("покрытие = %v, хотел %v", got, want)
	}
	if len(nodes) != 3 {
		t.Fatalf("узлов = %d, хотел 3 (глава, дыра, глава)", len(nodes))
	}
	if nodes[1].Title != untitledSectionTitle || nodes[1].Start != 4 || nodes[1].End != 6 {
		t.Errorf("дыра = %+v, хотел узел %q 4—6", nodes[1], untitledSectionTitle)
	}
}

// Пересекающиеся главы верхнего уровня: общая страница достаётся первой по
// порядку главе — то же соглашение, что и в book.BuildSection.
func TestTopLevelNodesFirstSiblingWinsOverlap(t *testing.T) {
	chapters := []book.Node{
		{Title: "Первая", Start: 1, End: 5},
		{Title: "Вторая", Start: 3, End: 8},
	}

	nodes := topLevelNodes(chapters, 1, 8)

	if got, want := coverage(nodes), sequence(1, 8); !reflect.DeepEqual(got, want) {
		t.Fatalf("покрытие = %v, хотел %v (без потерь и без повторов)", got, want)
	}
	if len(nodes) != 2 {
		t.Fatalf("узлов = %d, хотел 2", len(nodes))
	}
	if nodes[0].Start != 1 || nodes[0].End != 5 {
		t.Errorf("первая глава = %+v, хотел 1—5 (не тронута)", nodes[0])
	}
	if nodes[1].Start != 6 || nodes[1].End != 8 {
		t.Errorf("вторая глава = %+v, хотел 6—8 (отсечена от общих страниц 3—5)", nodes[1])
	}
}

// Глава, целиком поглощённая предыдущей, узла не получает вовсе — как и в
// book.BuildSection.
func TestTopLevelNodesDropsSiblingAbsorbedByPredecessor(t *testing.T) {
	chapters := []book.Node{
		{Title: "Первая", Start: 1, End: 8},
		{Title: "Вторая", Start: 3, End: 5},
	}

	nodes := topLevelNodes(chapters, 1, 8)

	if got, want := coverage(nodes), sequence(1, 8); !reflect.DeepEqual(got, want) {
		t.Fatalf("покрытие = %v, хотел %v", got, want)
	}
	if len(nodes) != 1 {
		t.Fatalf("узлов = %d, хотел 1 (поглощённая глава не порождает узел)", len(nodes))
	}
	if nodes[0].Title != "Первая" {
		t.Errorf("остался %q, хотел «Первая»", nodes[0].Title)
	}
}

// Глава с детьми тоже подчиняется клиппингу верхнего уровня: если её отсекает
// предыдущий сосед, собственный диапазон сжимается, но поддерево (Children)
// не теряется — иначе BuildSection ниже не найдёт вложенную главу вовсе.
func TestTopLevelNodesClipsParentRangeKeepsChildren(t *testing.T) {
	chapters := []book.Node{
		{Title: "Первая", Start: 1, End: 5},
		{
			Title: "Часть", Start: 3, End: 12,
			Children: []book.Node{{Title: "Глава", Start: 6, End: 12}},
		},
	}

	nodes := topLevelNodes(chapters, 1, 12)

	if got, want := coverage(nodes), sequence(1, 12); !reflect.DeepEqual(got, want) {
		t.Fatalf("покрытие = %v, хотел %v", got, want)
	}
	if len(nodes) != 2 {
		t.Fatalf("узлов = %d, хотел 2", len(nodes))
	}
	part := nodes[1]
	if part.Title != "Часть" || part.Start != 6 || part.End != 12 {
		t.Errorf("«Часть» = %+v, хотел диапазон 6—12 (отсечён общими страницами 3—5)", part)
	}
	if len(part.Children) != 1 || part.Children[0].Title != "Глава" {
		t.Fatalf("дети «Части» потеряны при отсечении: %+v", part.Children)
	}
}

// Все четыре теста topLevelNodes выше подают главы уже в порядке Start —
// sort.SliceStable в реализации от этого никак не проверяется: мутант без
// сортировки прошёл бы их все. На живом корпусе order_number совпадает со
// start_page везде, но это не гарантия: /works/{id}/chapters/{id}/move
// двигает order_number, не трогая границы страниц, — редактор, переставивший
// главы местами, и создаёт ровно то расхождение, которое тут проверяется.
func TestTopLevelNodesSortsChaptersNotGivenInPageOrder(t *testing.T) {
	chapters := []book.Node{
		{Title: "Вторая", Start: 7, End: 9},
		{Title: "Первая", Start: 1, End: 3},
	}

	nodes := topLevelNodes(chapters, 1, 9)

	if got, want := coverage(nodes), sequence(1, 9); !reflect.DeepEqual(got, want) {
		t.Fatalf("покрытие = %v, хотел %v (без потерь и без повторов)", got, want)
	}
	if len(nodes) != 3 {
		t.Fatalf("узлов = %d, хотел 3 (глава, дыра, глава)", len(nodes))
	}
	if nodes[0].Title != "Первая" || nodes[0].Start != 1 || nodes[0].End != 3 {
		t.Errorf("первый узел = %+v, хотел «Первая» 1—3 (раскладка должна идти по Start, не по порядку входного среза)", nodes[0])
	}
	if nodes[1].Title != untitledSectionTitle || nodes[1].Start != 4 || nodes[1].End != 6 {
		t.Errorf("дыра = %+v, хотел узел %q 4—6", nodes[1], untitledSectionTitle)
	}
	if nodes[2].Title != "Вторая" || nodes[2].Start != 7 || nodes[2].End != 9 {
		t.Errorf("третий узел = %+v, хотел «Вторая» 7—9", nodes[2])
	}
}

// ---- Collection: сборка из элементов разных источников ----

// fakeCollectionStoreForDownload — подставной сторадж подборок. Заполняются
// только методы, которыми пользуется выгрузка.
type fakeCollectionStoreForDownload struct {
	CollectionStore
	collection *models.Collection
	rows       []repository.ItemRow
	chapters   []*models.Chapter
}

func (f *fakeCollectionStoreForDownload) GetBySlug(context.Context, string) (*models.Collection, error) {
	return f.collection, nil
}

func (f *fakeCollectionStoreForDownload) GetByAuthorSlug(context.Context, string, string) (*models.Collection, error) {
	return f.collection, nil
}

func (f *fakeCollectionStoreForDownload) ItemRows(context.Context, int64) ([]repository.ItemRow, error) {
	return f.rows, nil
}

func (f *fakeCollectionStoreForDownload) ChaptersForWorks(context.Context, []int64) ([]*models.Chapter, error) {
	return f.chapters, nil
}

func strPtr(s string) *string { return &s }
func int64Ptr(v int64) *int64 { return &v }

// downloadCollectionFixture — подборка из двух элементов: живой главы и битого
// элемента, чей источник удалён.
func downloadCollectionFixture() *DownloadSource {
	src := downloadFixture()
	src.collections = &fakeCollectionStoreForDownload{
		// Опубликована: DownloadHandler.Collection теперь гейтит черновик и
		// снятую с публикации отдельно (см. TestDownloadCollection* в
		// download_handler_test.go) — эта фикстура проверяет сборку файла,
		// а не видимость, поэтому стоит опубликованной.
		collection: &models.Collection{ID: 5, Title: "Моя подборка", Slug: "moi", PublishedAt: ptrTime(time.Now().Add(-time.Hour))},
		rows: []repository.ItemRow{
			{
				Item: models.CollectionItem{
					ID: 1, CollectionID: 5, Kind: models.CollectionItemKindChapter,
					ChapterID: int64Ptr(10), WorkID: int64Ptr(1),
					SnapshotTitle: "Глава", SnapshotAuthor: "К. Маркс", OrderNumber: 1,
				},
				ChapterTitle: strPtr("Глава"), ChapterStartPage: intPtr(2), ChapterEndPage: intPtr(4),
				// WorkAuthor нарочно отличается от SnapshotAuthor: если бы автора секции
				// брали из живой работы, а не из снимка, тесты ниже это бы не заметили.
				WorkTitle: strPtr("Том 23"), WorkAuthor: strPtr("К. Маркс (совр. атрибуция)"),
				PageOffset: intPtr(100), VolumeNumber: intPtr(23), EditionTitle: strPtr("Сочинения"),
			},
			{
				Item: models.CollectionItem{
					ID: 2, CollectionID: 5, Kind: models.CollectionItemKindChapter,
					SnapshotTitle: "Тезисы о Фейербахе", OrderNumber: 2,
				},
			},
		},
	}
	return src
}

func TestDownloadCollectionBuildsSectionPerItem(t *testing.T) {
	b, err := downloadCollectionFixture().Collection(context.Background(), "", "moi")
	if err != nil {
		t.Fatalf("Collection() error = %v", err)
	}

	if b.Meta.Title != "Моя подборка" {
		t.Errorf("название = %q", b.Meta.Title)
	}
	if len(b.Sections) != 1 {
		t.Fatalf("секций = %d, хотел 1 (битый элемент пропущен)", len(b.Sections))
	}
	if b.Sections[0].Title != "Глава" {
		t.Errorf("название секции = %q", b.Sections[0].Title)
	}
	if got := b.PageCount(); got != 3 {
		t.Errorf("страниц = %d, хотел 3", got)
	}
}

// Битый элемент нельзя терять молча: он перечисляется на титуле.
func TestDownloadCollectionReportsMissingItems(t *testing.T) {
	b, err := downloadCollectionFixture().Collection(context.Background(), "", "moi")
	if err != nil {
		t.Fatalf("Collection() error = %v", err)
	}

	if len(b.Meta.Missing) != 1 || b.Meta.Missing[0] != "Тезисы о Фейербахе" {
		t.Errorf("пропущенные элементы = %v, хотел [Тезисы о Фейербахе]", b.Meta.Missing)
	}
}

// У подборки нет ни издания, ни единого диапазона: строка «Источник:»
// выродилась бы в огрызок.
func TestDownloadCollectionHasNoEditionOrRange(t *testing.T) {
	b, err := downloadCollectionFixture().Collection(context.Background(), "", "moi")
	if err != nil {
		t.Fatalf("Collection() error = %v", err)
	}

	if b.Meta.Edition != "" || b.Meta.Volume != "" {
		t.Errorf("у подборки проставлено издание %q / том %q", b.Meta.Edition, b.Meta.Volume)
	}
	if b.Meta.PageFrom != 0 || b.Meta.PageTo != 0 {
		t.Errorf("у подборки проставлен диапазон %d—%d", b.Meta.PageFrom, b.Meta.PageTo)
	}
}

// Смещение печатной нумерации у каждого элемента своё — оно берётся из
// работы-источника, а не из первого попавшегося тома.
func TestDownloadCollectionUsesPerItemPageOffset(t *testing.T) {
	b, err := downloadCollectionFixture().Collection(context.Background(), "", "moi")
	if err != nil {
		t.Fatalf("Collection() error = %v", err)
	}

	first := b.Sections[0].Blocks[0].Pages[0]
	if first.Printed != 102 {
		t.Errorf("печатный номер = %d, хотел 102 (внутренняя 2 + смещение 100)", first.Printed)
	}
}

// downloadCollectionTwoOffsetsFixture — подборка из двух ЖИВЫХ элементов
// разных работ с разным page_offset (5 и 1000). В downloadCollectionFixture
// живой элемент всего один, а битый смещения не несёт вовсе — тест на такой
// фикстуре не отличит «смещение берётся по элементу» от «смещение взято у
// первого элемента» или даже от константы. Здесь отличить можно: если бы
// смещение бралось не из своего элемента, кто-то из двух Printed не сошёлся
// бы с ожидаемым.
func downloadCollectionTwoOffsetsFixture() *DownloadSource {
	pages := &fakePageStore{}
	pages.getPageRangeFn = func(_ context.Context, workID int64, from, to int) ([]*models.Page, error) {
		switch workID {
		case 1:
			return []*models.Page{testPage(2, "Первая работа.", models.PageStatusProofread)}, nil
		case 2:
			return []*models.Page{testPage(3, "Вторая работа.", models.PageStatusProofread)}, nil
		}
		return nil, nil
	}

	collections := &fakeCollectionStoreForDownload{
		collection: &models.Collection{ID: 11, Title: "Разные смещения", Slug: "offsets"},
		rows: []repository.ItemRow{
			{
				Item: models.CollectionItem{
					ID: 1, CollectionID: 11, Kind: models.CollectionItemKindChapter,
					ChapterID: int64Ptr(10), WorkID: int64Ptr(1),
					SnapshotTitle: "Малое смещение", OrderNumber: 1,
				},
				ChapterStartPage: intPtr(2), ChapterEndPage: intPtr(2), PageOffset: intPtr(5),
			},
			{
				Item: models.CollectionItem{
					ID: 2, CollectionID: 11, Kind: models.CollectionItemKindChapter,
					ChapterID: int64Ptr(20), WorkID: int64Ptr(2),
					SnapshotTitle: "Большое смещение", OrderNumber: 2,
				},
				ChapterStartPage: intPtr(3), ChapterEndPage: intPtr(3), PageOffset: intPtr(1000),
			},
		},
		chapters: []*models.Chapter{
			{ID: 10, WorkID: 1, Title: "Малое смещение", StartPage: 2, EndPage: 2},
			{ID: 20, WorkID: 2, Title: "Большое смещение", StartPage: 3, EndPage: 3},
		},
	}

	return NewDownloadSource(&fakeWorkStore{}, &fakeChapterStore{}, pages,
		&fakeEditionStore{}, collections, markdown.NewRenderer(), "https://lib.example.org")
}

func TestDownloadCollectionUsesPerItemPageOffsetAcrossWorks(t *testing.T) {
	b, err := downloadCollectionTwoOffsetsFixture().Collection(context.Background(), "", "offsets")
	if err != nil {
		t.Fatalf("Collection() error = %v", err)
	}
	if len(b.Sections) != 2 {
		t.Fatalf("секций = %d, хотел 2", len(b.Sections))
	}

	first := b.Sections[0].Blocks[0].Pages[0]
	if first.Internal != 2 || first.Printed != 7 {
		t.Errorf("первая секция: Internal=%d Printed=%d, хотел Internal=2 Printed=7 (2+5)", first.Internal, first.Printed)
	}
	second := b.Sections[1].Blocks[0].Pages[0]
	if second.Internal != 3 || second.Printed != 1003 {
		t.Errorf("вторая секция: Internal=%d Printed=%d, хотел Internal=3 Printed=1003 (3+1000)", second.Internal, second.Printed)
	}
}

// downloadCollectionOutOfPageOrderFixture — элемент с бОльшим номером
// страницы стоит в списке элементов ПЕРВЫМ. Во всех остальных фикстурах
// порядок элементов случайно совпадает с порядком страниц, поэтому сортировка
// секций по странице прошла бы незамеченной.
func downloadCollectionOutOfPageOrderFixture() *DownloadSource {
	pages := &fakePageStore{}
	pages.getPageRangeFn = func(_ context.Context, _ int64, from, to int) ([]*models.Page, error) {
		all := []*models.Page{
			testPage(5, "Малая страница.", models.PageStatusProofread),
			testPage(50, "Большая страница.", models.PageStatusProofread),
		}
		var out []*models.Page
		for _, p := range all {
			if p.PageNumber >= from && p.PageNumber <= to {
				out = append(out, p)
			}
		}
		return out, nil
	}

	collections := &fakeCollectionStoreForDownload{
		collection: &models.Collection{ID: 12, Title: "Порядок элементов", Slug: "order"},
		rows: []repository.ItemRow{
			{
				Item: models.CollectionItem{
					ID: 1, CollectionID: 12, Kind: models.CollectionItemKindChapter,
					ChapterID: int64Ptr(10), WorkID: int64Ptr(1),
					SnapshotTitle: "Первый по списку, страница больше", OrderNumber: 1,
				},
				ChapterStartPage: intPtr(50), ChapterEndPage: intPtr(50),
			},
			{
				Item: models.CollectionItem{
					ID: 2, CollectionID: 12, Kind: models.CollectionItemKindChapter,
					ChapterID: int64Ptr(20), WorkID: int64Ptr(1),
					SnapshotTitle: "Второй по списку, страница меньше", OrderNumber: 2,
				},
				ChapterStartPage: intPtr(5), ChapterEndPage: intPtr(5),
			},
		},
		chapters: []*models.Chapter{
			{ID: 10, WorkID: 1, Title: "Первый по списку, страница больше", StartPage: 50, EndPage: 50},
			{ID: 20, WorkID: 1, Title: "Второй по списку, страница меньше", StartPage: 5, EndPage: 5},
		},
	}

	return NewDownloadSource(&fakeWorkStore{}, &fakeChapterStore{}, pages,
		&fakeEditionStore{}, collections, markdown.NewRenderer(), "https://lib.example.org")
}

// Порядок секций обязан следовать порядку элементов подборки, а не номерам
// страниц источников: сравнивать страницы разных работ друг с другом
// бессмысленно, поэтому сортировка по странице — не альтернативная, а просто
// неверная реализация.
func TestDownloadCollectionPreservesItemOrderIndependentOfPageOrder(t *testing.T) {
	b, err := downloadCollectionOutOfPageOrderFixture().Collection(context.Background(), "", "order")
	if err != nil {
		t.Fatalf("Collection() error = %v", err)
	}
	if len(b.Sections) != 2 {
		t.Fatalf("секций = %d, хотел 2", len(b.Sections))
	}

	if b.Sections[0].Title != "Первый по списку, страница больше" {
		t.Errorf("первая секция = %q, хотел элемент с OrderNumber=1", b.Sections[0].Title)
	}
	if b.Sections[1].Title != "Второй по списку, страница меньше" {
		t.Errorf("вторая секция = %q, хотел элемент с OrderNumber=2", b.Sections[1].Title)
	}
	// Страницы подтверждают, что порядок секций не совпал с порядком заголовков
	// случайно: первая секция действительно про страницу 50, вторая — про 5.
	if got := b.Sections[0].Blocks[0].Pages[0].Internal; got != 50 {
		t.Errorf("первая секция про страницу %d, хотел 50", got)
	}
	if got := b.Sections[1].Blocks[0].Pages[0].Internal; got != 5 {
		t.Errorf("вторая секция про страницу %d, хотел 5", got)
	}
}

// Автор секции показывает, откуда взят текст: у подборки авторы разные.
func TestDownloadCollectionCarriesItemAuthor(t *testing.T) {
	b, err := downloadCollectionFixture().Collection(context.Background(), "", "moi")
	if err != nil {
		t.Fatalf("Collection() error = %v", err)
	}

	if b.Sections[0].Author != "К. Маркс" {
		t.Errorf("автор секции = %q", b.Sections[0].Author)
	}
}

func TestDownloadCollectionOverrideWinsOverSnapshot(t *testing.T) {
	src := downloadCollectionFixture()
	store := src.collections.(*fakeCollectionStoreForDownload)
	store.rows[0].Item.AuthorOverride = "Ф. Энгельс"

	b, err := src.Collection(context.Background(), "", "moi")
	if err != nil {
		t.Fatalf("Collection() error = %v", err)
	}

	if b.Sections[0].Author != "Ф. Энгельс" {
		t.Errorf("автор = %q, хотел значение из AuthorOverride", b.Sections[0].Author)
	}
}

// itemAuthor реализует приоритет источников автора; фикстура выше уже не
// даёт SnapshotAuthor и WorkAuthor совпасть, но только прямая проверка самой
// функции на всех трёх полях разом доказывает, что читается именно то поле,
// что нужно, а не любое из непустых.
func TestItemAuthorPriority(t *testing.T) {
	cases := []struct {
		name       string
		override   string
		snapshot   string
		workAuthor *string
		want       string
	}{
		{
			name:     "override побеждает и снимок, и живого автора",
			override: "Ф. Энгельс", snapshot: "К. Маркс", workAuthor: strPtr("В. Ленин"),
			want: "Ф. Энгельс",
		},
		{
			name:     "override пуст — побеждает снимок, отличный от живого автора",
			override: "", snapshot: "К. Маркс", workAuthor: strPtr("Иной, отличный от снимка автор"),
			want: "К. Маркс",
		},
		{
			name:     "override и снимок пусты — остаётся живой автор работы",
			override: "", snapshot: "", workAuthor: strPtr("В. Ленин"),
			want: "В. Ленин",
		},
		{
			name:     "все три поля пусты — автора нет",
			override: "", snapshot: "", workAuthor: nil,
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			row := repository.ItemRow{
				Item:       models.CollectionItem{AuthorOverride: c.override, SnapshotAuthor: c.snapshot},
				WorkAuthor: c.workAuthor,
			}
			if got := itemAuthor(row); got != c.want {
				t.Errorf("itemAuthor() = %q, хотел %q", got, c.want)
			}
		})
	}
}

func TestDownloadCollectionNotFound(t *testing.T) {
	src := downloadCollectionFixture()
	src.collections.(*fakeCollectionStoreForDownload).collection = nil

	if _, err := src.Collection(context.Background(), "", "нет-такого"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, хотел ErrNotFound", err)
	}
}

// ---- Перенос дефекта: якоря сносок не должны сталкиваться между секциями ----
//
// CollectPages ручается за уникальность якорей "fn:<номер>-<имя>" только
// внутри одного своего вызова. buildSections и collectionItemSection зовут
// его по разу на каждую секцию верхнего уровня, и нумерация в каждом вызове
// начинается заново от номеров страниц этого куска — поэтому два разных
// источника подборки, у каждого из которых своя страница 5, получают один и
// тот же якорь "fn:5-1": FB2 сохранит только первое определение, HTML
// задвоит id, Markdown отдаст пандоку два одинаковых "[^5-1]:", и тот молча
// схлопнет их в одно. На корпусе постраничный вариант этого же дефекта
// (сноски одной страницы, не одной секции) задевает 2068 глав из 2305.

// footnoteCollisionSource — подборка из двух элементов разных работ, у каждой
// из которых своя (внутренняя) страница 5 с подстрочной сноской "[^1]": без
// починки оба элемента отрендерят один и тот же якорь "fn:5-1".
func footnoteCollisionSource() *DownloadSource {
	pages := &fakePageStore{}
	pages.getPageRangeFn = func(_ context.Context, workID int64, from, to int) ([]*models.Page, error) {
		switch workID {
		case 1:
			return []*models.Page{testPage(5, "Первый текст[^1]\n\n[^1]: Первое примечание.", models.PageStatusProofread)}, nil
		case 2:
			return []*models.Page{testPage(5, "Второй текст[^1]\n\n[^1]: Второе примечание.", models.PageStatusProofread)}, nil
		}
		return nil, nil
	}

	collections := &fakeCollectionStoreForDownload{
		collection: &models.Collection{ID: 9, Title: "Столкновение якорей", Slug: "collide"},
		rows: []repository.ItemRow{
			{Item: models.CollectionItem{
				ID: 1, CollectionID: 9, Kind: models.CollectionItemKindChapter,
				ChapterID: int64Ptr(10), WorkID: int64Ptr(1),
				SnapshotTitle: "Первая", OrderNumber: 1,
			}, ChapterStartPage: intPtr(5), ChapterEndPage: intPtr(5)},
			{Item: models.CollectionItem{
				ID: 2, CollectionID: 9, Kind: models.CollectionItemKindChapter,
				ChapterID: int64Ptr(20), WorkID: int64Ptr(2),
				SnapshotTitle: "Вторая", OrderNumber: 2,
			}, ChapterStartPage: intPtr(5), ChapterEndPage: intPtr(5)},
		},
		chapters: []*models.Chapter{
			{ID: 10, WorkID: 1, Title: "Первая", StartPage: 5, EndPage: 5},
			{ID: 20, WorkID: 2, Title: "Вторая", StartPage: 5, EndPage: 5},
		},
	}

	return NewDownloadSource(&fakeWorkStore{}, &fakeChapterStore{}, pages,
		&fakeEditionStore{}, collections, markdown.NewRenderer(), "https://lib.example.org")
}

// Два элемента подборки, у каждого своя (внутренняя) страница 5 — CollectPages
// вызывается по разу на элемент, и оба получили бы якорь "fn:5-1", не будь
// починки. После неё якоря различаются, и ссылка каждой секции ведёт в
// примечания именно своей секции, а не соседней.
func TestDownloadCollectionDisambiguatesFootnoteAnchorsAcrossSections(t *testing.T) {
	b, err := footnoteCollisionSource().Collection(context.Background(), "", "collide")
	if err != nil {
		t.Fatalf("Collection() error = %v", err)
	}
	if len(b.Sections) != 2 {
		t.Fatalf("секций = %d, хотел 2", len(b.Sections))
	}

	firstHTML := b.Sections[0].Blocks[0].Pages[0].HTML
	secondHTML := b.Sections[1].Blocks[0].Pages[0].HTML

	firstRef := fnrefAnchorRe.FindString(firstHTML)
	secondRef := fnrefAnchorRe.FindString(secondHTML)
	if firstRef == "" || secondRef == "" {
		t.Fatalf("не нашёл якорь fnref в тексте секций: %q / %q", firstHTML, secondHTML)
	}
	if firstRef == secondRef {
		t.Errorf("якоря секций совпали: %q", firstRef)
	}
	if strings.Contains(firstHTML, `id="fnref:5-1"`) || strings.Contains(secondHTML, `id="fnref:5-1"`) {
		t.Errorf("исходный (сталкивающийся) якорь fnref:5-1 пережил починку")
	}

	// Ссылка первой секции обязана вести на примечание в НЕЙ, а не в соседней.
	hrefRe := regexp.MustCompile(`href="#(fn:[\w-]+)"`)
	m := hrefRe.FindStringSubmatch(firstHTML)
	if m == nil {
		t.Fatalf("не нашёл href на fn: в первой секции: %q", firstHTML)
	}
	if !strings.Contains(b.Sections[0].NotesHTML, `id="`+m[1]+`"`) {
		t.Errorf("ссылка первой секции %q не находит примечание в своей секции:\n%s", m[1], b.Sections[0].NotesHTML)
	}
	if strings.Contains(b.Sections[1].NotesHTML, `id="`+m[1]+`"`) {
		t.Errorf("якорь первой секции %q обнаружился в примечаниях второй", m[1])
	}
}

// Тот же дефект уровнем ниже, в сыром Markdown: пандок сворачивает два
// одинаковых определения "[^…]:" в одно, теряя второе примечание молча
// (предупреждение "Duplicate note reference" в лог не попадает в файл).
func TestDownloadCollectionMarkdownWriterHasNoDuplicateFootnoteDefinitions(t *testing.T) {
	b, err := footnoteCollisionSource().Collection(context.Background(), "", "collide")
	if err != nil {
		t.Fatalf("Collection() error = %v", err)
	}

	var buf bytes.Buffer
	if err := (book.MarkdownWriter{}).Write(&buf, b); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	out := buf.String()

	defRe := regexp.MustCompile(`(?m)^\[\^[^\]]+\]:`)
	defs := defRe.FindAllString(out, -1)
	if len(defs) != 2 {
		t.Fatalf("определений сносок = %d, хотел 2:\n%s", len(defs), out)
	}
	seen := map[string]bool{}
	for _, d := range defs {
		if seen[d] {
			t.Fatalf("повторное определение сноски %q — второе примечание пандок схлопнёт:\n%s", d, out)
		}
		seen[d] = true
	}
	if !strings.Contains(out, "Первое примечание.") || !strings.Contains(out, "Второе примечание.") {
		t.Errorf("оба примечания должны остаться в файле:\n%s", out)
	}
}

// Тот же дефект в FB2 — там цена выше, чем в Markdown: writeFB2Notes
// (pkg/book/fb2.go) собирает тела примечаний в map, ключ которой — якорь;
// столкновение якорей без починки схлопнуло бы <section id="fn_5-1"> одной
// секции поверх другой безо всякой ошибки сборки или линтера — просто молча
// пропавшая сноска в читалке. Проверяем каждое звено: обе секции
// примечаний выжили под разными id, у каждой — своё (и только своё) тело, и
// каждая ссылка type="note" в тексте ведёт именно на свою.
func TestDownloadCollectionFB2WriterHasNoDuplicateFootnoteDefinitions(t *testing.T) {
	b, err := footnoteCollisionSource().Collection(context.Background(), "", "collide")
	if err != nil {
		t.Fatalf("Collection() error = %v", err)
	}

	var buf bytes.Buffer
	if err := (book.FB2Writer{}).Write(&buf, b); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	out := buf.String()

	notesBodyRe := regexp.MustCompile(`(?s)<body name="notes">(.*)</body>`)
	notesBody := notesBodyRe.FindStringSubmatch(out)
	if notesBody == nil {
		t.Fatalf("не нашёл <body name=\"notes\"> в выводе:\n%s", out)
	}

	sectionRe := regexp.MustCompile(`(?s)<section id="([^"]+)">(.*?)</section>`)
	sections := sectionRe.FindAllStringSubmatch(notesBody[1], -1)
	if len(sections) != 2 {
		t.Fatalf("секций примечаний = %d, хотел 2 (без починки якорь совпал бы, и вторая секция схлопнулась бы с первой):\n%s",
			len(sections), notesBody[1])
	}
	if sections[0][1] == sections[1][1] {
		t.Fatalf("id секций примечаний совпали: %q", sections[0][1])
	}

	bodies := map[string]string{sections[0][1]: sections[0][2], sections[1][1]: sections[1][2]}
	var firstID, secondID string
	for id, body := range bodies {
		if strings.Contains(body, "Первое примечание.") {
			firstID = id
		}
		if strings.Contains(body, "Второе примечание.") {
			secondID = id
		}
	}
	if firstID == "" || secondID == "" {
		t.Fatalf("оба тела примечаний должны остаться в выводе, каждое под своим id:\n%s", notesBody[1])
	}
	if firstID == secondID {
		t.Fatalf("оба примечания оказались под одним якорем %q — второе потеряно в map", firstID)
	}

	refRe := regexp.MustCompile(`<a l:href="#([^"]+)" type="note">`)
	refs := refRe.FindAllStringSubmatch(out, -1)
	if len(refs) != 2 {
		t.Fatalf("ссылок type=\"note\" в тексте = %d, хотел 2:\n%s", len(refs), out)
	}
	if refs[0][1] == refs[1][1] {
		t.Fatalf("обе ссылки в тексте ведут на один и тот же якорь %q", refs[0][1])
	}
	for i, ref := range refs {
		if ref[1] != firstID && ref[1] != secondID {
			t.Errorf("ссылка №%d %q не совпадает ни с одним известным якорем примечания (%q / %q)",
				i+1, ref[1], firstID, secondID)
		}
	}
}

// ---- Meta.CacheKey (internal/api/download_handler.go, ETag) ----
//
// max(pages.updated_at) — Meta.Modified — не видит переименование главы,
// перенос главы через /chapters/{id}/move, смещение печатной нумерации
// (works.page_offset) или правку состава подборки: ни одна из этих правок
// не трогает ни одной страницы, а байты файла меняются. Ниже — по одному
// повтору правки на entity.UpdatedAt для каждого из трёх сборщиков; страницы
// фикстуры не трогаются нигде, поэтому Modified обязан остаться прежним —
// только CacheKey обязан измениться.

var cacheKeyRenamedAt = time.Date(2026, time.August, 18, 0, 0, 0, 0, time.UTC)

func TestWorkCacheKeyReflectsWorkUpdatedAt(t *testing.T) {
	base, err := downloadFixture().Work(context.Background(), 1)
	if err != nil {
		t.Fatalf("Work() error = %v", err)
	}

	editionID := int64(7)
	src := downloadFixture()
	src.works.(*fakeWorkStore).getFn = func(context.Context, int64) (*models.Work, error) {
		return &models.Work{
			ID: 1, Title: "Том 23", Author: "К. Маркс", Language: "ru",
			EditionID: &editionID, VolumeNumber: intPtr(23), PageOffset: 100,
			UpdatedAt: cacheKeyRenamedAt,
		}, nil
	}
	got, err := src.Work(context.Background(), 1)
	if err != nil {
		t.Fatalf("Work() error = %v", err)
	}

	if got.Meta.CacheKey == base.Meta.CacheKey {
		t.Error("CacheKey не изменился при правке work.UpdatedAt — переименование или смещение " +
			"печатной нумерации не даст читателю новый ETag")
	}
	if got.Meta.Modified != base.Meta.Modified {
		t.Error("Modified не должен зависеть от work.UpdatedAt — иначе титул начнёт врать про правки текста")
	}
}

func TestChapterCacheKeyReflectsChapterUpdatedAt(t *testing.T) {
	base, err := downloadFixture().Chapter(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("Chapter() error = %v", err)
	}

	src := downloadFixture()
	src.chapters.(*fakeChapterStore).getByIDFn = func(context.Context, int64) (*models.Chapter, error) {
		return &models.Chapter{ID: 10, WorkID: 1, Title: "Глава", StartPage: 2, EndPage: 4, UpdatedAt: cacheKeyRenamedAt}, nil
	}
	got, err := src.Chapter(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("Chapter() error = %v", err)
	}

	if got.Meta.CacheKey == base.Meta.CacheKey {
		t.Error("CacheKey не изменился при правке chapter.UpdatedAt — переименование или " +
			"/chapters/{id}/move не даст читателю новый ETag")
	}
	if got.Meta.Modified != base.Meta.Modified {
		t.Error("Modified не должен зависеть от chapter.UpdatedAt — иначе титул начнёт врать про правки текста")
	}
}

func TestCollectionCacheKeyReflectsItemUpdatedAt(t *testing.T) {
	base, err := downloadCollectionFixture().Collection(context.Background(), "", "moi")
	if err != nil {
		t.Fatalf("Collection() error = %v", err)
	}

	src := downloadCollectionFixture()
	store := src.collections.(*fakeCollectionStoreForDownload)
	store.rows[0].Item.UpdatedAt = cacheKeyRenamedAt // как после переупорядочивания пунктов
	got, err := src.Collection(context.Background(), "", "moi")
	if err != nil {
		t.Fatalf("Collection() error = %v", err)
	}

	if got.Meta.CacheKey == base.Meta.CacheKey {
		t.Error("CacheKey не изменился при правке UpdatedAt пункта подборки — изменение состава " +
			"или порядка не даст читателю новый ETag")
	}
	if got.Meta.Modified != base.Meta.Modified {
		t.Error("Modified не должен зависеть от UpdatedAt пункта подборки — иначе титул начнёт врать про правки текста")
	}
}

// Правка самой подборки (не пункта) тоже обязана менять CacheKey — например
// переименование подборки.
func TestCollectionCacheKeyReflectsCollectionUpdatedAt(t *testing.T) {
	base, err := downloadCollectionFixture().Collection(context.Background(), "", "moi")
	if err != nil {
		t.Fatalf("Collection() error = %v", err)
	}

	src := downloadCollectionFixture()
	store := src.collections.(*fakeCollectionStoreForDownload)
	store.collection.UpdatedAt = cacheKeyRenamedAt
	got, err := src.Collection(context.Background(), "", "moi")
	if err != nil {
		t.Fatalf("Collection() error = %v", err)
	}

	if got.Meta.CacheKey == base.Meta.CacheKey {
		t.Error("CacheKey не изменился при правке collection.UpdatedAt — переименование подборки " +
			"не даст читателю новый ETag")
	}
}

// chapterIDs собирает ChapterID всех секций в порядке чтения, с глубиной.
func chapterIDs(sections []book.Section) []int64 {
	var out []int64
	var walk func(s book.Section)
	walk = func(s book.Section) {
		out = append(out, s.ChapterID)
		for _, b := range s.Blocks {
			if b.Child != nil {
				walk(*b.Child)
			}
		}
	}
	for _, s := range sections {
		walk(s)
	}
	return out
}

func nestedChaptersFixture(anchors bool) *DownloadSource {
	s := downloadFixture()
	s.chapters = &fakeChapterStore{
		listHierarchicalFn: func(context.Context, int64) ([]*models.Chapter, error) {
			return []*models.Chapter{{
				ID: 10, WorkID: 1, Title: "Глава", StartPage: 2, EndPage: 4,
				Children: []*models.Chapter{{ID: 11, WorkID: 1, Title: "Подглава", StartPage: 3, EndPage: 4}},
			}}, nil
		},
	}
	if anchors {
		return s.WithChapterAnchors()
	}
	return s
}

func TestDownloadWorkWithChapterAnchorsSetsChapterIDs(t *testing.T) {
	b, err := nestedChaptersFixture(true).Work(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	// Хвост до главы (с. 1), глава 10 с подглавой 11, хвост после (с. 5).
	got := chapterIDs(b.Sections)
	want := []int64{0, 10, 11, 0}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("ChapterID секций = %v, хотел %v", got, want)
	}
}

func TestDownloadWorkWithoutAnchorsLeavesChapterIDZero(t *testing.T) {
	b, err := nestedChaptersFixture(false).Work(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range chapterIDs(b.Sections) {
		if id != 0 {
			t.Fatalf("выгрузка получила ChapterID %d — её файлы перестанут быть прежними", id)
		}
	}
}

func TestWithChapterAnchorsDoesNotChangeOriginal(t *testing.T) {
	s := nestedChaptersFixture(false)
	_ = s.WithChapterAnchors()
	if s.chapterAnchors {
		t.Fatal("WithChapterAnchors переключил исходный источник, а не копию")
	}
}
