package api

import (
	"context"
	"errors"

	"proofreader/internal/models"
)

// Обёртки чтения полос и глав по зрителю. Закрытая полоса — та, что попадает
// в диапазон закрытой главы (своим признаком или через предка-главу); для
// всех, кроме сотрудника, её нет ни в одном методе чтения. Зритель берётся из
// контекста запроса: бестокенные каналы (/seo, .md, MCP, OPDS, статическая
// сборка) идут с контекстом без claims и фильтруются той же обёрткой без
// отдельной проводки, а вызов, забывший передать контекст запроса
// (context.Background()), закрытое прячет, а не выдаёт.
//
// Закрытость РАБОТЫ здесь не решается: её держат closedGate и открытый вид
// работ (OpenWorks). Обёртки только убирают сверх того, ослабить закрытие
// работы им нечем.
//
// Интерфейс намеренно не встроен: метод чтения, добавленный в PageStore или
// ChapterStore, без своей реализации здесь не скомпилируется в server.go, а
// встроенный прошёл бы мимо фильтра молча. Сторож разбора методов —
// TestViewWrappersClassifyEveryMethod.

// Тексты отказов — те же, что у репозиториев: обработчики переводят их в 404
// прежними ветками, и закрытое неотличимо от несуществующего.
var (
	errHiddenPage    = errors.New("page not found")
	errHiddenChapter = errors.New("chapter not found")
)

// inSpans — попадает ли номер полосы в один из диапазонов.
func inSpans(spans []models.PageSpan, n int) bool {
	for _, s := range spans {
		if n >= s.Start && n <= s.End {
			return true
		}
	}
	return false
}

// spansOverlap — пересекает ли диапазон [start, end] хоть один из spans.
func spansOverlap(spans []models.PageSpan, start, end int) bool {
	for _, s := range spans {
		if s.Start <= end && start <= s.End {
			return true
		}
	}
	return false
}

// ViewPages — PageStore, который не-сотруднику не показывает закрытых полос.
// Запись проходит как есть.
type ViewPages struct {
	pages  PageStore
	closed ClosedSpansStore
}

var _ PageStore = (*ViewPages)(nil)

// NewViewPages оборачивает хранилище полос. closed == nil — ничего не закрыто.
func NewViewPages(pages PageStore, closed ClosedSpansStore) *ViewPages {
	return &ViewPages{pages: pages, closed: closed}
}

// HiddenSpans — диапазоны полос работы, которых этот зритель не видит;
// сотруднику и без хранилища — nil.
func (v *ViewPages) HiddenSpans(ctx context.Context, workID int64) ([]models.PageSpan, error) {
	if v.closed == nil || staffContext(ctx) {
		return nil, nil
	}
	return v.closed.ClosedSpans(ctx, workID)
}

// HasClosedIn — есть ли в диапазоне полос закрытые, независимо от зрителя.
// По нему обработчики глав и подборок идут мимо кэша диапазонов: файл,
// собранный сотрудником, нёс бы закрытое, а собранный не-сотрудником — дыру,
// которую сотрудник видеть не должен.
func (v *ViewPages) HasClosedIn(ctx context.Context, workID int64, start, end int) (bool, error) {
	if v.closed == nil {
		return false, nil
	}
	spans, err := v.closed.ClosedSpans(ctx, workID)
	if err != nil {
		return false, err
	}
	return spansOverlap(spans, start, end), nil
}

func (v *ViewPages) filter(ctx context.Context, workID int64, pages []*models.Page) ([]*models.Page, error) {
	spans, err := v.HiddenSpans(ctx, workID)
	if err != nil {
		return nil, err
	}
	if len(spans) == 0 {
		return pages, nil
	}
	out := make([]*models.Page, 0, len(pages))
	for _, p := range pages {
		if !inSpans(spans, p.PageNumber) {
			out = append(out, p)
		}
	}
	return out, nil
}

// one — отказ «нет полосы», если полоса закрыта для зрителя.
func (v *ViewPages) one(ctx context.Context, page *models.Page) (*models.Page, error) {
	if page == nil {
		return nil, nil
	}
	spans, err := v.HiddenSpans(ctx, page.WorkID)
	if err != nil {
		return nil, err
	}
	if inSpans(spans, page.PageNumber) {
		return nil, errHiddenPage
	}
	return page, nil
}

func (v *ViewPages) GetByID(ctx context.Context, id int64) (*models.Page, error) {
	page, err := v.pages.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return v.one(ctx, page)
}

func (v *ViewPages) GetByWorkAndPageNumber(ctx context.Context, workID int64, pageNumber int) (*models.Page, error) {
	page, err := v.pages.GetByWorkAndPageNumber(ctx, workID, pageNumber)
	if err != nil {
		return nil, err
	}
	return v.one(ctx, page)
}

func (v *ViewPages) GetPageRange(ctx context.Context, workID int64, startPage, endPage int) ([]*models.Page, error) {
	pages, err := v.pages.GetPageRange(ctx, workID, startPage, endPage)
	if err != nil {
		return nil, err
	}
	return v.filter(ctx, workID, pages)
}

func (v *ViewPages) GetPagesByNumbers(ctx context.Context, workID int64, numbers []int) ([]*models.Page, error) {
	pages, err := v.pages.GetPagesByNumbers(ctx, workID, numbers)
	if err != nil {
		return nil, err
	}
	return v.filter(ctx, workID, pages)
}

func (v *ViewPages) ListByWork(ctx context.Context, workID int64) ([]*models.Page, error) {
	pages, err := v.pages.ListByWork(ctx, workID)
	if err != nil {
		return nil, err
	}
	return v.filter(ctx, workID, pages)
}

func (v *ViewPages) ListPageMap(ctx context.Context, workID int64) ([]models.PageMapEntry, error) {
	entries, err := v.pages.ListPageMap(ctx, workID)
	if err != nil {
		return nil, err
	}
	spans, err := v.HiddenSpans(ctx, workID)
	if err != nil {
		return nil, err
	}
	if len(spans) == 0 {
		return entries, nil
	}
	out := make([]models.PageMapEntry, 0, len(entries))
	for _, e := range entries {
		if !inSpans(spans, e.PageNumber) {
			out = append(out, e)
		}
	}
	return out, nil
}

// MaxPageNumber — последняя видимая полоса: закрытый хвост тома не выдаёт
// своей длины. Нумерация в базе сплошная, поэтому шаг на полосу перед
// закрытым диапазоном и есть последняя видимая.
func (v *ViewPages) MaxPageNumber(ctx context.Context, workID int64) (int, error) {
	n, err := v.pages.MaxPageNumber(ctx, workID)
	if err != nil || n == 0 {
		return n, err
	}
	spans, err := v.HiddenSpans(ctx, workID)
	if err != nil {
		return 0, err
	}
	for moved := true; moved && n > 0; {
		moved = false
		for _, s := range spans {
			if n >= s.Start && n <= s.End {
				n, moved = s.Start-1, true
			}
		}
	}
	if n < 0 {
		n = 0
	}
	return n, nil
}

func (v *ViewPages) Create(ctx context.Context, page *models.Page) error {
	return v.pages.Create(ctx, page)
}

func (v *ViewPages) SaveEdit(ctx context.Context, page *models.Page, version *models.PageVersion) error {
	return v.pages.SaveEdit(ctx, page, version)
}

// ChapterReadStore — хранилище глав, которое оборачивает ViewChapters: срез
// ChapterStore и чтение главы по полосе, нужное краулеру, MCP и озвучке.
// *repository.ChapterRepository удовлетворяет ему как есть.
type ChapterReadStore interface {
	ChapterStore
	FindByPage(ctx context.Context, workID int64, pageNumber int) (*models.Chapter, error)
}

// ViewChapters — хранилище глав, которое не-сотруднику не показывает
// закрытых глав и их поддеревьев. Закрытость главы берётся из самой строки
// (Closed, ClosedBy — их считает репозиторий по closed_chapters тем же
// запросом), закрытость полосы — из ClosedSpans. Запись проходит как есть.
//
// Пути записи ChapterHandler читают через голое хранилище, а не через эту
// обёртку: охрана закрытого (409) обязана видеть закрытые главы и на
// контексте без claims.
type ViewChapters struct {
	chapters ChapterStore
	// finder — глава по полосе; nil у обёртки обработчика глав, которому
	// она не нужна (FindByPage тогда отказывает).
	finder interface {
		FindByPage(ctx context.Context, workID int64, pageNumber int) (*models.Chapter, error)
	}
	closed ClosedSpansStore
}

var _ ChapterReadStore = (*ViewChapters)(nil)

// NewViewChapters оборачивает хранилище глав. closed == nil — полосы не
// закрыты; закрытые главы по своей строке скрываются всё равно.
func NewViewChapters(chapters ChapterReadStore, closed ClosedSpansStore) *ViewChapters {
	return &ViewChapters{chapters: chapters, finder: chapters, closed: closed}
}

// viewOfChapterStore — обёртка над срезом без главы по полосе: её строит
// себе ChapterHandler поверх голого хранилища, которым пишет.
func viewOfChapterStore(chapters ChapterStore, closed ClosedSpansStore) *ViewChapters {
	return &ViewChapters{chapters: chapters, closed: closed}
}

var errNoFindByPage = errors.New("хранилище глав без поиска главы по полосе")

func chapterIsClosed(c *models.Chapter) bool {
	return c != nil && (c.Closed || c.ClosedBy != "")
}

func (v *ViewChapters) GetByID(ctx context.Context, id int64) (*models.Chapter, error) {
	c, err := v.chapters.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if chapterIsClosed(c) && !staffContext(ctx) {
		return nil, errHiddenChapter
	}
	return c, nil
}

// pruneClosed — дерево без закрытых глав вместе с поддеревьями.
func pruneClosed(tree []*models.Chapter) []*models.Chapter {
	out := make([]*models.Chapter, 0, len(tree))
	for _, c := range tree {
		if chapterIsClosed(c) {
			continue
		}
		c.Children = pruneClosed(c.Children)
		out = append(out, c)
	}
	return out
}

func (v *ViewChapters) ListByWorkHierarchical(ctx context.Context, workID int64) ([]*models.Chapter, error) {
	tree, err := v.chapters.ListByWorkHierarchical(ctx, workID)
	if err != nil || staffContext(ctx) {
		return tree, err
	}
	return pruneClosed(tree), nil
}

// FindByPage — глава закрытой полосы для не-сотрудника не существует: самой
// полосы для него нет. Самая узкая глава открытой полосы открыта по
// определению (закрытая глава закрыла бы полосу), но строка проверяется всё
// равно.
func (v *ViewChapters) FindByPage(ctx context.Context, workID int64, pageNumber int) (*models.Chapter, error) {
	if v.finder == nil {
		return nil, errNoFindByPage
	}
	c, err := v.finder.FindByPage(ctx, workID, pageNumber)
	if err != nil || c == nil || staffContext(ctx) {
		return c, err
	}
	if chapterIsClosed(c) {
		return nil, nil
	}
	if v.closed != nil {
		spans, err := v.closed.ClosedSpans(ctx, workID)
		if err != nil {
			return nil, err
		}
		if inSpans(spans, pageNumber) {
			return nil, nil
		}
	}
	return c, nil
}

func (v *ViewChapters) Create(ctx context.Context, chapter *models.Chapter) error {
	return v.chapters.Create(ctx, chapter)
}

func (v *ViewChapters) Delete(ctx context.Context, id int64) error {
	return v.chapters.Delete(ctx, id)
}

func (v *ViewChapters) Move(ctx context.Context, chapterID int64, newParentID *int64, newOrder int) error {
	return v.chapters.Move(ctx, chapterID, newParentID, newOrder)
}

func (v *ViewChapters) Update(ctx context.Context, chapter *models.Chapter) error {
	return v.chapters.Update(ctx, chapter)
}

// closedRanger — то, что умеет ViewPages для кэша и потока чтения. Обработчики
// спрашивают его у своего PageStore: обёрнутое хранилище несёт и фильтр, и
// ответ «мимо кэша», поэтому одно без другого не проведёшь.
type closedRanger interface {
	HasClosedIn(ctx context.Context, workID int64, start, end int) (bool, error)
	HiddenSpans(ctx context.Context, workID int64) ([]models.PageSpan, error)
}

// rangeHasClosed — есть ли закрытые полосы в диапазоне; хранилище без
// обёртки закрытого не знает. Ответ true значит «мимо кэша диапазонов»
// ((*RangeCache)(nil).Serve — ни чтения, ни записи) для любого зрителя.
func rangeHasClosed(ctx context.Context, pages PageStore, workID int64, start, end int) (bool, error) {
	cr, ok := pages.(closedRanger)
	if !ok {
		return false, nil
	}
	return cr.HasClosedIn(ctx, workID, start, end)
}

// closedMarkers — для сотрудника: заглавие закрытой главы, накрывающей
// полосу, самой широкой из них: подглава закрыта вместе с родителем, и
// назвать стоит родителя — его и открывают кнопкой. Вне закрытых глав —
// пусто.
func closedMarkers(ctx context.Context, chapters ChapterTreeStore, workID int64) (func(page int) string, error) {
	tree, err := chapters.ListByWorkHierarchical(ctx, workID)
	if err != nil {
		return nil, err
	}
	var closed []*models.Chapter
	var walk func([]*models.Chapter)
	walk = func(cs []*models.Chapter) {
		for _, c := range cs {
			if chapterIsClosed(c) {
				closed = append(closed, c)
			}
			walk(c.Children)
		}
	}
	walk(tree)
	return func(page int) string {
		var best *models.Chapter
		for _, c := range closed {
			if page < c.StartPage || page > c.EndPage {
				continue
			}
			if best == nil || c.EndPage-c.StartPage > best.EndPage-best.StartPage {
				best = c
			}
		}
		if best == nil {
			return ""
		}
		return best.Title
	}, nil
}

// hiddenSpansOf — диапазоны, которых зритель не видит, у обёрнутого
// хранилища; у голого — nil.
func hiddenSpansOf(ctx context.Context, pages PageStore, workID int64) ([]models.PageSpan, error) {
	cr, ok := pages.(closedRanger)
	if !ok {
		return nil, nil
	}
	return cr.HiddenSpans(ctx, workID)
}

// visibleWindowEnd — номер, на котором диапазон от from набирает want
// номеров вне hidden (или total, если раньше кончается том).
func visibleWindowEnd(from, want, total int, hidden []models.PageSpan) int {
	n := from - 1
	for want > 0 && n < total {
		n++
		if !inSpans(hidden, n) {
			want--
		}
	}
	return n
}
