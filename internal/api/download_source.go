package api

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/pkg/book"
	"proofreader/pkg/markdown"
)

// ErrNotFound — источник выгрузки не найден. Обработчик по нему отличает 404
// от внутренней ошибки.
var ErrNotFound = errors.New("не найдено")

// DownloadSource собирает книгу для выгрузки из тех же стораджей, через
// которые ходит остальное API. Единственное место, знающее об этом: пакет
// pkg/book о базе не подозревает вовсе.
type DownloadSource struct {
	works       WorkStore
	chapters    ChapterStore
	pages       PageStore
	editions    EditionStore
	collections CollectionStore
	renderer    *markdown.Renderer
	baseURL     string
	// chapterAnchors — проставлять секциям id глав (book.Section.ChapterID).
	// Включает только WithChapterAnchors: выгрузки собираются без него и
	// остаются прежними байт в байт.
	chapterAnchors bool
}

func NewDownloadSource(
	works WorkStore, chapters ChapterStore, pages PageStore, editions EditionStore,
	collections CollectionStore, renderer *markdown.Renderer, baseURL string,
) *DownloadSource {
	return &DownloadSource{
		works: works, chapters: chapters, pages: pages, editions: editions,
		collections: collections, renderer: renderer, baseURL: strings.TrimSuffix(baseURL, "/"),
	}
}

// WithChapterAnchors — копия источника, которая проставляет секциям глав их
// id. Нужна статической читальне (internal/staticsite): она ссылается на
// подглаву якорем ch-<id> из оглавления тома, указателя и подборок.
func (s *DownloadSource) WithChapterAnchors() *DownloadSource {
	c := *s
	c.chapterAnchors = true
	return &c
}

// Work собирает том целиком: главы верхнего уровня секциями, непокрытые ими
// страницы — блоками корня.
//
// Служебные работы (role = front_matter) сюда не входят: у них своя карточка и
// своя кнопка, а куда вставлять их в томе — вопрос без единственного ответа.
func (s *DownloadSource) Work(ctx context.Context, workID int64) (*book.Book, error) {
	work, err := s.works.GetByID(ctx, workID)
	if err != nil || work == nil {
		return nil, fmt.Errorf("%w: работа %d", ErrNotFound, workID)
	}

	pages, err := s.pages.ListByWork(ctx, workID)
	if err != nil {
		return nil, fmt.Errorf("страницы работы %d: %w", workID, err)
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("%w: в работе %d нет страниц", ErrNotFound, workID)
	}

	chapters, err := s.chapters.ListByWorkHierarchical(ctx, workID)
	if err != nil {
		return nil, fmt.Errorf("главы работы %d: %w", workID, err)
	}

	first, last := pages[0].PageNumber, pages[len(pages)-1].PageNumber
	var nodes []book.Node
	if len(chapters) == 0 {
		// Тома без глав тоже бывают. Одна секция на весь текст.
		nodes = []book.Node{{Title: work.Title, Start: first, End: last}}
	} else {
		// Главы верхнего уровня редко покрывают весь том без зазоров: перед
		// первой главой обычно живёт титул, после последней — иногда хвост
		// без главы вовсе (как в фикстуре). Такие страницы не теряются, а
		// становятся отдельными безымянными секциями верхнего уровня — тем
		// самым «блоками корня», о которых говорит комментарий выше.
		nodes = topLevelNodes(chaptersToNodes(chapters, s.chapterAnchors), first, last)
	}

	meta := s.workMeta(ctx, work, pages, first, last)
	// work.UpdatedAt закрывает то, что max(pages.updated_at) не видит:
	// смещение печатной нумерации (works.page_offset) меняет каждый номер
	// страницы в файле, ни одной страницы при этом не трогая.
	meta.CacheKey = latestOf(meta.Modified, work.UpdatedAt)

	b := &book.Book{Meta: meta}
	b.Sections = disambiguateFootnotes(s.buildSections(nodes, pages, work.PageOffset))
	return b, nil
}

// Chapter собирает одну главу вместе с её поддеревом.
func (s *DownloadSource) Chapter(ctx context.Context, workID, chapterID int64) (*book.Book, error) {
	chapter, err := s.chapters.GetByID(ctx, chapterID)
	if err != nil || chapter == nil {
		return nil, fmt.Errorf("%w: глава %d", ErrNotFound, chapterID)
	}
	// Глава чужого тома по этому пути открываться не должна.
	if chapter.WorkID != workID {
		return nil, fmt.Errorf("%w: глава %d не принадлежит работе %d", ErrNotFound, chapterID, workID)
	}

	work, err := s.works.GetByID(ctx, workID)
	if err != nil || work == nil {
		return nil, fmt.Errorf("%w: работа %d", ErrNotFound, workID)
	}

	tree, err := s.chapters.ListByWorkHierarchical(ctx, workID)
	if err != nil {
		return nil, fmt.Errorf("главы работы %d: %w", workID, err)
	}
	node := book.Node{Title: chapter.Title, Start: chapter.StartPage, End: chapter.EndPage}
	if found := findChapter(tree, chapterID); found != nil {
		// Выгрузка главы захватывает её поддерево целиком.
		node = chaptersToNodes([]*models.Chapter{found}, s.chapterAnchors)[0]
	}

	pages, err := s.pages.GetPageRange(ctx, workID, chapter.StartPage, chapter.EndPage)
	if err != nil {
		return nil, fmt.Errorf("страницы главы %d: %w", chapterID, err)
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("%w: в главе %d нет страниц", ErrNotFound, chapterID)
	}

	meta := s.workMeta(ctx, work, pages,
		pages[0].PageNumber, pages[len(pages)-1].PageNumber)
	meta.Title = chapter.Title
	// Тот же приём слага, что и в workMeta, но для обеих частей адреса —
	// тома и главы: адрес источника на титульном листе — то, по чему
	// читатель возвращается в читальню из скачанного EPUB/FB2, и после
	// задачи 10 голый номер ответил бы ему 301. Построитель из internal/seo
	// (canonical.go) отсюда не позвать — он неэкспортирован в чужом пакете;
	// копия оправдана тем же, чем и в workMeta — общий помощник на два
	// применения в разных пакетах пока не заводим.
	chapterURL := fmt.Sprintf("%s/works/%d", s.baseURL, workID)
	if work.Slug != "" {
		chapterURL += "-" + work.Slug
	}
	chapterURL += fmt.Sprintf("/chapters/%d", chapterID)
	if chapter.Slug != "" {
		chapterURL += "-" + chapter.Slug
	}
	meta.URL = chapterURL
	// Своя UpdatedAt главы закрывает переименование и перенос
	// (/chapters/{id}/move переставляет границы, не трогая ни одной
	// страницы), UpdatedAt работы — то же, что и у выгрузки тома целиком.
	meta.CacheKey = latestOf(meta.Modified, chapter.UpdatedAt, work.UpdatedAt)

	b := &book.Book{Meta: meta}
	b.Sections = disambiguateFootnotes(s.buildSections([]book.Node{node}, pages, work.PageOffset))
	return b, nil
}

// PageRange собирает полосы from…to тома — для MCP-сервера читальни:
// найденное место читается узким куском, а не главой целиком. Главы тома
// обрезаются по диапазону тем же topLevelNodes, что у выгрузки тома, поэтому
// заголовок главы, в которую попал кусок, стоит перед его первой полосой.
// Полос за концом тома нет — кусок кончается там, где кончается том.
func (s *DownloadSource) PageRange(ctx context.Context, workID int64, from, to int) (*book.Book, error) {
	work, err := s.works.GetByID(ctx, workID)
	if err != nil || work == nil {
		return nil, fmt.Errorf("%w: работа %d", ErrNotFound, workID)
	}
	pages, err := s.pages.GetPageRange(ctx, workID, from, to)
	if err != nil {
		return nil, fmt.Errorf("страницы %d—%d работы %d: %w", from, to, workID, err)
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("%w: в работе %d нет страниц %d—%d", ErrNotFound, workID, from, to)
	}
	chapters, err := s.chapters.ListByWorkHierarchical(ctx, workID)
	if err != nil {
		return nil, fmt.Errorf("главы работы %d: %w", workID, err)
	}

	first, last := pages[0].PageNumber, pages[len(pages)-1].PageNumber
	nodes := []book.Node{{Title: work.Title, Start: first, End: last}}
	if len(chapters) > 0 {
		nodes = topLevelNodes(chaptersToNodes(chapters, s.chapterAnchors), first, last)
	}
	b := &book.Book{Meta: s.workMeta(ctx, work, pages, first, last)}
	b.Sections = disambiguateFootnotes(s.buildSections(nodes, pages, work.PageOffset))
	return b, nil
}

// Collection собирает подборка: каждый элемент — своя секция со своим автором
// и своим смещением печатной нумерации.
//
// Единого издания, тома и диапазона страниц у подборки нет — эти поля Meta
// остаются пустыми, иначе титул получил бы огрызок «Источник: , с. 0—0».
func (s *DownloadSource) Collection(ctx context.Context, nickname, slug string) (*book.Book, error) {
	collection, err := s.collections.GetByAuthorSlug(ctx, nickname, slug)
	if err != nil || collection == nil {
		return nil, fmt.Errorf("%w: подборка %q", ErrNotFound, slug)
	}

	rows, err := s.collections.ItemRows(ctx, collection.ID)
	if err != nil {
		return nil, fmt.Errorf("состав подборки %q: %w", slug, err)
	}

	// Читательский адрес двухсегментный (ник + слаг); у сотруднической
	// подборки (пустой ник) остаётся прежний однослаговый вид — это тот же
	// адрес, что печатает /collections в списке, и его нельзя менять молча.
	url := fmt.Sprintf("%s/collections/%s", s.baseURL, slug)
	if nickname != "" {
		url = fmt.Sprintf("%s/collections/%s/%s", s.baseURL, nickname, slug)
	}
	b := &book.Book{Meta: book.Meta{
		Title: collection.Title,
		URL:   url,
		Lang:  "ru",
	}}

	// collection.UpdatedAt и UpdatedAt каждого пункта закрывают то, что
	// max(pages.updated_at) не видит: переименование подборки, состав и
	// порядок пунктов (row.Item.UpdatedAt) — правки, которые меняют файл,
	// не трогая ни одной страницы источника. Битые пункты в сумму входят
	// тоже: они меняют строку "недоступно" на титуле и свою позицию в ней.
	cacheKey := collection.UpdatedAt

	var statuses []models.PageStatus
	for _, row := range rows {
		cacheKey = latestOf(cacheKey, row.Item.UpdatedAt)

		section, pages, ok := s.collectionItemSection(ctx, collection.ID, row)
		if !ok {
			// Источник удалён или пересоздан. Молча терять пункт оглавления
			// нельзя — он перечисляется на титуле.
			b.Meta.Missing = append(b.Meta.Missing, itemLabel(row))
			continue
		}
		b.Sections = append(b.Sections, section)
		for _, p := range pages {
			statuses = append(statuses, p.Status)
			if p.UpdatedAt.After(b.Meta.Modified) {
				b.Meta.Modified = p.UpdatedAt
			}
		}
	}

	b.Sections = disambiguateFootnotes(b.Sections)
	b.Meta.Proofread = book.CountProofread(statuses)
	b.Meta.Authors = collectionAuthors(b.Sections)
	b.Meta.CacheKey = latestOf(cacheKey, b.Meta.Modified)
	return b, nil
}

// collectionItemSection собирает секцию одного элемента подборки.
func (s *DownloadSource) collectionItemSection(
	ctx context.Context, collectionID int64, row repository.ItemRow,
) (book.Section, []*models.Page, bool) {
	item := row.Item
	if item.WorkID == nil {
		return book.Section{}, nil, false
	}
	if item.Kind == models.CollectionItemKindChapter && item.ChapterID == nil {
		return book.Section{}, nil, false
	}

	start, end, ok := collectionItemRange(row)
	if !ok {
		// Границы элемента-работы своих строк не имеют — берутся из глав
		// верхнего уровня, как это делает ItemPages.
		chapters, err := s.collections.ChaptersForWorks(ctx, []int64{*item.WorkID})
		if err != nil {
			return book.Section{}, nil, false
		}
		if start, end, ok = topLevelPageRange(chapters, *item.WorkID); !ok {
			pageMap, err := s.pages.ListPageMap(ctx, *item.WorkID)
			if err != nil || len(pageMap) == 0 {
				return book.Section{}, nil, false
			}
			start, end = pageMap[0].PageNumber, pageMap[len(pageMap)-1].PageNumber
		}
	}

	pages, err := s.pages.GetPageRange(ctx, *item.WorkID, start, end)
	if err != nil || len(pages) == 0 {
		return book.Section{}, nil, false
	}

	offset := 0
	if row.PageOffset != nil {
		offset = *row.PageOffset
	}

	node := book.Node{Title: itemLabel(row), Start: start, End: end}
	if item.Kind == models.CollectionItemKindChapter && item.ChapterID != nil {
		if chapters, err := s.collections.ChaptersForWorks(ctx, []int64{*item.WorkID}); err == nil {
			if found := findChapter(buildTree(chapters), *item.ChapterID); found != nil {
				node = chaptersToNodes([]*models.Chapter{found}, s.chapterAnchors)[0]
				node.Title = itemLabel(row)
			}
		}
	}

	rendered := s.renderRange(pages, start, end, offset)
	section := book.BuildSection(node, rendered.pages)
	section.NotesHTML = rendered.notesHTML
	section.Author = itemAuthor(row)
	return section, pages, true
}

// collectionItemRange — собственные границы элемента-главы, уже подтянутые
// джойном в ItemRows.
func collectionItemRange(row repository.ItemRow) (start, end int, ok bool) {
	if row.ChapterStartPage != nil && row.ChapterEndPage != nil {
		return *row.ChapterStartPage, *row.ChapterEndPage, true
	}
	return 0, 0, false
}

// itemLabel — как элемент называется в оглавлении. Снимок названия переживает
// переименование источника, поэтому он в приоритете.
func itemLabel(row repository.ItemRow) string {
	if title := strings.TrimSpace(row.Item.SnapshotTitle); title != "" {
		return title
	}
	if row.ChapterTitle != nil && *row.ChapterTitle != "" {
		return *row.ChapterTitle
	}
	if row.WorkTitle != nil {
		return *row.WorkTitle
	}
	return "Без названия"
}

// itemAuthor — автор элемента. Правка составителя перекрывает снимок.
func itemAuthor(row repository.ItemRow) string {
	if a := strings.TrimSpace(row.Item.AuthorOverride); a != "" {
		return a
	}
	if a := strings.TrimSpace(row.Item.SnapshotAuthor); a != "" {
		return a
	}
	if row.WorkAuthor != nil {
		return strings.TrimSpace(*row.WorkAuthor)
	}
	return ""
}

// collectionAuthors собирает авторов подборки без повторов, сохраняя порядок
// появления: перебор map недетерминирован, а файл обязан быть воспроизводим.
func collectionAuthors(sections []book.Section) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range sections {
		if s.Author == "" || seen[s.Author] {
			continue
		}
		seen[s.Author] = true
		out = append(out, s.Author)
	}
	return out
}

// buildTree складывает плоский список глав в дерево. ChaptersForWorks отдаёт
// их плоскими, в отличие от ListByWorkHierarchical.
func buildTree(chapters []*models.Chapter) []*models.Chapter {
	byID := make(map[int64]*models.Chapter, len(chapters))
	for _, c := range chapters {
		c.Children = nil
		byID[c.ID] = c
	}
	var roots []*models.Chapter
	for _, c := range chapters {
		if c.ParentID == nil {
			roots = append(roots, c)
			continue
		}
		if parent, ok := byID[*c.ParentID]; ok {
			parent.Children = append(parent.Children, c)
			continue
		}
		roots = append(roots, c)
	}
	return roots
}

// buildSections превращает узлы в секции и собирает примечания — по одной
// секции верхнего уровня за раз.
//
// CollectPages перенумеровывает подстрочные сноски сквозь весь переданный
// кусок. Вызови его на весь том — и получишь (1)…(1500) одной простынёй в
// конце. По верхнеуровневой секции нумерация начинается заново в каждой
// работе тома, а книжные номера эндноутов не трогаются в любом случае.
func (s *DownloadSource) buildSections(nodes []book.Node, pages []*models.Page, offset int) []book.Section {
	sections := make([]book.Section, 0, len(nodes))
	for _, node := range nodes {
		rendered := s.renderRange(pages, node.Start, node.End, offset)
		section := book.BuildSection(node, rendered.pages)
		section.NotesHTML = rendered.notesHTML
		sections = append(sections, section)
	}
	return sections
}

type renderedRange struct {
	pages     book.Pages
	notesHTML string
}

func (s *DownloadSource) renderRange(pages []*models.Page, from, to, offset int) renderedRange {
	var window []*models.Page
	for _, p := range pages {
		if p.PageNumber >= from && p.PageNumber <= to {
			window = append(window, p)
		}
	}
	if len(window) == 0 {
		return renderedRange{}
	}

	contents := make([]markdown.PageContent, len(window))
	for i, p := range window {
		contents[i] = markdown.PageContent{PageNumber: p.PageNumber, Content: p.ContentMarkdown}
	}
	pageHTML, notes := s.renderer.CollectPages(contents)

	out := make(book.Pages, len(window))
	for i, p := range window {
		out[i] = book.Page{
			Internal: p.PageNumber,
			Printed:  p.PageNumber + offset,
			// CollectPages отдаёт готовый XHTML-фрагмент (контракт
			// pkg/markdown). Раньше обёртку снимали здесь, а ошибку разбора
			// глушили до пустого тела — рядом в файл едет markdown, и текст
			// не терялся. Развилка уехала внутрь пакета, где ветка ошибки
			// недостижима: источник разбора — строка.
			HTML:     pageHTML[i],
			Markdown: p.ContentMarkdown,
		}
	}
	return renderedRange{pages: out, notesHTML: markdown.RenderNotes(notes)}
}

func (s *DownloadSource) workMeta(
	ctx context.Context, work *models.Work, pages []*models.Page, first, last int,
) book.Meta {
	statuses := make([]models.PageStatus, len(pages))
	var modified time.Time
	for i, p := range pages {
		statuses[i] = p.Status
		if p.UpdatedAt.After(modified) {
			modified = p.UpdatedAt
		}
	}

	// Адрес источника печатается в скачиваемый файл (титульный лист EPUB/FB2)
	// и должен вести на канон, а не на устаревающий голый номер — та же
	// логика, что и у internal/seo (canonical.go), но builder оттуда
	// недоступен: пакет чужой, а функция там неэкспортирована нарочно.
	url := fmt.Sprintf("%s/works/%d", s.baseURL, work.ID)
	if work.Slug != "" {
		url += "-" + work.Slug
	}

	meta := book.Meta{
		Title:     work.Title,
		Edition:   s.editionTitle(ctx, work),
		Volume:    volumeLabel(work),
		PageFrom:  first + work.PageOffset,
		PageTo:    last + work.PageOffset,
		URL:       url,
		Modified:  modified,
		Lang:      langOrRussian(work.Language),
		Proofread: book.CountProofread(statuses),
	}
	if author := strings.TrimSpace(work.Author); author != "" {
		meta.Authors = []string{author}
	}
	return meta
}

func (s *DownloadSource) editionTitle(ctx context.Context, work *models.Work) string {
	if work.EditionID == nil {
		return ""
	}
	edition, err := s.editions.GetByID(ctx, *work.EditionID)
	if err != nil || edition == nil {
		// Издание — украшение титула, а не причина отказать в выгрузке.
		return ""
	}
	return edition.Title
}

func volumeLabel(work *models.Work) string {
	if work.VolumeNumber == nil {
		return ""
	}
	label := fmt.Sprintf("Том %d", *work.VolumeNumber)
	if work.VolumePart != nil && *work.VolumePart != "" {
		label += ", " + *work.VolumePart
	}
	return label
}

// latestOf — максимум из времён, нулевые значения не побеждают ненулевые.
// Собирает book.Meta.CacheKey из UpdatedAt нескольких сущностей разом.
func latestOf(times ...time.Time) time.Time {
	var out time.Time
	for _, t := range times {
		if t.After(out) {
			out = t
		}
	}
	return out
}

func langOrRussian(lang string) string {
	if strings.TrimSpace(lang) == "" {
		return "ru"
	}
	return lang
}

// untitledSectionTitle — название секции оглавления для страниц тома, не
// попавших ни в одну главу верхнего уровня: шмуцтитул перед первой главой,
// «КНИГА ПЕРВАЯ» без своей записи в дереве, дыра между соседями. Раздел не
// пуст — там настоящий текст, просто без заглавия в дереве глав, — поэтому
// пустая строка в оглавлении недопустима: писатели всех четырёх форматов
// ждут, что у секции есть имя (иначе EPUB/FB2/Markdown/HTML получают пустой
// пункт содержания). Кавычки — как в остальных плейсхолдерах корпуса.
const untitledSectionTitle = "«Без заглавия»"

// topLevelNodes раскладывает главы верхнего уровня в диапазоне [first, last]
// вперемешку с узлами-заглушками для страниц, которых не коснулась ни одна
// глава: перед первой, между соседями и после последней. Каждый такой узел —
// самостоятельная секция верхнего уровня (см. buildSections про сноски), а
// не часть чужой главы.
//
// Раскладка (сорт по границе, курсор, клиппинг, пропуск поглощённого
// ребёнка) — book.ClipChildren, общая с BuildSection: это та же самая
// задача, «дети вперемешку с непокрытыми пробелами», просто на верхнем
// уровне тома вместо секции. Пробел здесь не может быть пуст по построению
// (диапазон [cursor, n.Start-1] или хвостовой всегда содержит хотя бы одну
// страницу), поэтому в отличие от BuildSection ему не нужна проверка
// len(own) > 0 — он просто получает имя-заглушку.
func topLevelNodes(chapters []book.Node, first, last int) []book.Node {
	steps := book.ClipChildren(chapters, first, last)
	out := make([]book.Node, 0, len(steps))
	for _, step := range steps {
		if step.Gap {
			out = append(out, book.Node{Title: untitledSectionTitle, Start: step.Start, End: step.End})
			continue
		}
		out = append(out, step.Node)
	}
	return out
}

// chaptersToNodes переводит дерево глав в узлы сборки. withIDs — переносить
// ли id глав в узлы (только для WithChapterAnchors).
func chaptersToNodes(chapters []*models.Chapter, withIDs bool) []book.Node {
	nodes := make([]book.Node, 0, len(chapters))
	for _, c := range chapters {
		n := book.Node{
			Title:    c.Title,
			Start:    c.StartPage,
			End:      c.EndPage,
			Children: chaptersToNodes(c.Children, withIDs),
		}
		if withIDs {
			n.ChapterID = c.ID
		}
		nodes = append(nodes, n)
	}
	return nodes
}

// findChapter ищет главу в дереве тома — вместе с её детьми, которые
// ListByWorkHierarchical уже проставил.
func findChapter(chapters []*models.Chapter, id int64) *models.Chapter {
	for _, c := range chapters {
		if c.ID == id {
			return c
		}
		if found := findChapter(c.Children, id); found != nil {
			return found
		}
	}
	return nil
}

// ---- Уникальность якорей сносок между секциями верхнего уровня ----
//
// CollectPages ручается за уникальность якорей "fn:<номер>-<имя>" только
// внутри одного своего вызова. buildSections и collectionItemSection зовут
// его по разу на каждую секцию верхнего уровня — у тома это глава, у подборки
// элемент, — и нумерация в каждом вызове начинается заново от номеров страниц
// этого куска. Два разных источника легко показывают свою печатную страницу 5
// — и оба несут якорь "fn:5-1": FB2 сохраняет только первое определение,
// HTML задваивает id (браузер прыгает на первый), Markdown отдаёт
// pandoc'у два одинаковых "[^5-1]:" и тот молча схлопывает их в одно.
// EPUB спасает случайно — каждая секция верхнего уровня там и так уезжает в
// свой файл.
//
// disambiguateFootnotes закрывает это, пока Book не покинул DownloadSource:
// каждой секции верхнего уровня достаётся её порядковый номер в книге, и все
// три места, где живёт имя якоря — ссылка в тексте (id="fnref:…" и
// href="#fn:…"), тело примечания (id="fn:…" и обратная ссылка
// href="#fnref:…"), и сырые "[^имя]" в Page.Markdown — переписываются этим
// номером согласованно.

// disambiguateFootnotes переписывает якоря каждой секции верхнего уровня её
// порядковым номером в книге — единственным, что у секции точно есть и что
// точно не совпадёт с номером соседней.
func disambiguateFootnotes(sections []book.Section) []book.Section {
	for i := range sections {
		sections[i] = rewriteSectionFootnoteAnchors(sections[i], i)
	}
	return sections
}

// rewriteSectionFootnoteAnchors переименовывает якоря одной секции верхнего
// уровня целиком, вместе с вложенными секциями (Block.Child): у них сноски —
// от того же вызова CollectPages, что и у родителя (см. комментарий у
// buildSections про сквозной проход по верхнеуровневому узлу), поэтому им
// достаётся тот же индекс, а не собственный.
func rewriteSectionFootnoteAnchors(sec book.Section, idx int) book.Section {
	prefix := fmt.Sprintf("s%d-", idx)
	sec.NotesHTML = rewriteFootnoteAnchorsHTML(sec.NotesHTML, prefix)
	sec.Blocks = rewriteFootnoteAnchorsBlocks(sec.Blocks, prefix)
	return sec
}

func rewriteFootnoteAnchorsBlocks(blocks []book.Block, prefix string) []book.Block {
	out := make([]book.Block, len(blocks))
	for i, blk := range blocks {
		if blk.Child != nil {
			child := *blk.Child
			child.NotesHTML = rewriteFootnoteAnchorsHTML(child.NotesHTML, prefix)
			child.Blocks = rewriteFootnoteAnchorsBlocks(child.Blocks, prefix)
			out[i] = book.Block{Child: &child}
			continue
		}
		pages := make([]book.Page, len(blk.Pages))
		for j, p := range blk.Pages {
			p.HTML = rewriteFootnoteAnchorsHTML(p.HTML, prefix)
			p.Markdown = rewriteFootnoteAnchorsMarkdown(p.Markdown, prefix)
			pages[j] = p
		}
		out[i] = book.Block{Pages: pages}
	}
	return out
}

var (
	// fnrefAnchorRe и fnAnchorRe ловят обе формы якоря: "fnref:" — ссылка в
	// тексте, "fn:" — сама сноска. "fnref:" не содержит "fn:" как непрерывную
	// подстроку (после "fn" там идёт "ref", а не двоеточие), поэтому порядок
	// замен друг на друга не влияет и не задваивает префикс.
	fnrefAnchorRe = regexp.MustCompile(`fnref:([\w-]+)`)
	fnAnchorRe    = regexp.MustCompile(`fn:([\w-]+)`)
)

// rewriteFootnoteAnchorsHTML добавляет различающий префикс к обеим формам
// якоря во фрагменте отрендеренного HTML — как в тексте страницы, так и в
// блоке примечаний секции.
func rewriteFootnoteAnchorsHTML(html, prefix string) string {
	if html == "" {
		return html
	}
	html = fnrefAnchorRe.ReplaceAllString(html, "fnref:"+prefix+"$1")
	html = fnAnchorRe.ReplaceAllString(html, "fn:"+prefix+"$1")
	return html
}

var (
	// footnoteNameRe — тот же паттерн, что footnoteNameRe в pkg/book/markdown.go:
	// экспортировать его оттуда сюда нельзя, не протащив в pkg/book знание об
	// уровне сборки (см. запрет пакета на знание о базе) — поэтому короткое
	// регулярное выражение продублировано, а не переиспользовано через границу
	// пакетов.
	footnoteNameRe = regexp.MustCompile(`\[\^([^\]]+)\]`)
	// markdownFenceRe — начало отгороженного блока кода (``` или ~~~), внутри
	// которого "[^имя]" — буквальный пример разметки, а не настоящая сноска.
	markdownFenceRe = regexp.MustCompile("^\\s*(`{3,}|~{3,})")
)

// rewriteFootnoteAnchorsMarkdown добавляет различающий префикс к сырым
// "[^имя]" — и ссылкам, и определениям — в исходном тексте страницы,
// пропуская строки внутри отгороженных блоков кода.
func rewriteFootnoteAnchorsMarkdown(markdownText, prefix string) string {
	lines := strings.Split(markdownText, "\n")

	inFence := false
	var fenceChar byte
	var fenceLen int

	for i, line := range lines {
		if m := markdownFenceRe.FindStringSubmatch(line); m != nil {
			marker := m[1]
			switch {
			case !inFence:
				inFence, fenceChar, fenceLen = true, marker[0], len(marker)
			case marker[0] == fenceChar && len(marker) >= fenceLen:
				inFence = false
			}
			continue
		}
		if inFence {
			continue
		}
		lines[i] = footnoteNameRe.ReplaceAllString(line, "[^"+prefix+"$1]")
	}
	return strings.Join(lines, "\n")
}
