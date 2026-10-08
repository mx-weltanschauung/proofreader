package seo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/pkg/book"
	"proofreader/pkg/markdown"
)

// Интерфейсы объявлены здесь, у потребителя, а не в репозиториях: пакет seo
// обязан собираться без базы, иначе его рендереры не проверить без docker.
// *repository.WorkRepository и остальные удовлетворяют им как есть.

type WorkSource interface {
	GetByID(ctx context.Context, id int64) (*models.Work, error)
	// ListChildren — служебные передние листы тома: оглавлению тома для
	// нейросетей они нужны разделом «Предваряющие материалы».
	ListChildren(ctx context.Context, parentID int64) ([]*models.Work, error)
	// ListWithoutEdition — тома вне изданий, для /llms.txt.
	ListWithoutEdition(ctx context.Context) ([]models.ShelfWork, error)
}

type ChapterSource interface {
	GetByID(ctx context.Context, id int64) (*models.Chapter, error)
	ListByWorkHierarchical(ctx context.Context, workID int64) ([]*models.Chapter, error)
	// FindByPage — накрывающая полосу глава или nil, если полосу не накрывает
	// ни одна. Нужна canonical полосы: ссылки с полосы на её главу в базе нет.
	FindByPage(ctx context.Context, workID int64, pageNumber int) (*models.Chapter, error)
}

type PageSource interface {
	GetByWorkAndPageNumber(ctx context.Context, workID int64, pageNumber int) (*models.Page, error)
}

type EditionSource interface {
	GetByID(ctx context.Context, id int64) (*models.Edition, error)
	List(ctx context.Context) ([]*models.Edition, error)
	ListWorks(ctx context.Context, editionID int64) ([]*models.Work, error)
}

type ConceptSource interface {
	GetConceptBySlug(ctx context.Context, slug string) (*models.IndexConcept, error)
	ListConcepts(ctx context.Context, query, letter string, limit, offset int) ([]*models.IndexConcept, error)
}

type CollectionSource interface {
	GetBySlug(ctx context.Context, slug string) (*models.Collection, error)
	// GetByAuthorSlug — второй, более длинный адрес подборки
	// (/collections/{ник}/{слаг}): у читательской подборки слаг уникален
	// только в паре с ником автора. Пустой ник — то же, что GetBySlug.
	GetByAuthorSlug(ctx context.Context, nickname, slug string) (*models.Collection, error)
	List(ctx context.Context) ([]*models.Collection, error)
}

// DocumentPage — разбор, готовый к печати краулеру: строка, УЖЕ сведённая к
// одобренной редакции, и собранное тело.
//
// Тело приходит СОБРАННЫМ, а не markdown'ом: сборка разбора живёт в
// internal/api (assembleDocumentFor), и повторять её здесь нельзя — текст
// автора пишет вошедший читатель и обязан идти через
// markdown.Renderer.ForUntrustedAuthor(), который стоит внутри той сборки.
// Второй путь рендера авторского markdown открыл бы четыре закрытых стока
// (сырой HTML, блочные атрибуты, схемы ссылок, внешние подресурсы) заново.
type DocumentPage struct {
	Document *models.Document
	BodyHTML string
	CutCount int
}

// DocumentCard — то же для карточки, но без тела: надписям нужно заглавие,
// подпись и число вклеек, а сборка тела стоит как рендер главы, и карточка
// платила бы за неё на каждом первом превью.
type DocumentCard struct {
	Document *models.Document
	CutCount int
}

// DocumentSource — разбор для краулера. Его удовлетворяет
// *api.SEODocumentSource: сборка тела и сведение строки к одобренной
// редакции живут в internal/api, а импортировать его сюда нельзя —
// router.go держит *seo.Handler, и импорт замкнул бы цикл.
type DocumentSource interface {
	Card(ctx context.Context, nickname, slug string) (*DocumentCard, error)
	Page(ctx context.Context, nickname, slug string) (*DocumentPage, error)
	ListPublished(ctx context.Context, limit, offset int) ([]*models.Document, error)
}

// BookSource — сборка книги. Его удовлетворяет *api.SEOBookSource (задача 10):
// сам *api.DownloadSource возвращает api.ErrNotFound, а пакет seo про
// internal/api знать не может — router.go держит *seo.Handler, и импорт
// замкнул бы цикл.
type BookSource interface {
	Chapter(ctx context.Context, workID, chapterID int64) (*book.Book, error)
}

// ConceptRubric — строка оглавления понятия: подрубрика и что под ней.
type ConceptRubric struct {
	// Edition — издание статьи. Печатается, только когда у понятия статей
	// несколько: подрубрики разных указателей иначе слились бы.
	Edition string
	// Path — путь подрубрики от корня статьи.
	Path []string
	// Places — адресов под подрубрикой вместе с вложенными, считая тома,
	// которых в читальне нет.
	Places int
	// Volumes — номера томов этих адресов, по первому появлению.
	Volumes []int
	// FirstPage — порядковый номер (с нуля) первой полосы подрубрики среди
	// полос книги в порядке book.MarkdownWithPageStarts; -1 — ни одной полосы.
	// По нему оглавление узнаёт, в какой части начинается подрубрика.
	FirstPage int
}

// ConceptBook — понятие указателя, собранное в книгу для текста нейросетям:
// секция на подрубрику, секция на место, полосы целиком, каждая один раз.
// Его отдаёт *api.ConceptBookSource: адреса разрешаются по карте томов
// своего издания (internal/api), а импортировать api сюда нельзя.
type ConceptBook struct {
	Concept *models.IndexConcept
	Book    *book.Book
	// Rubrics — оглавление в порядке книги.
	Rubrics []ConceptRubric
	// Places — адресов в указателе; Present — из них хотя бы с одной полосой
	// в читальне; Pages — разных полос в тексте.
	Places, Present, Pages int
	// Modified — max(updated_at) понятия и его полос: идёт в ETag, иначе
	// правка полосы не меняла бы его.
	Modified time.Time
}

// ConceptBookSource — сборка понятия. Отсутствие — ошибка с seo.ErrNotFound.
// rubric — путь подрубрики (?rubric_path= страницы понятия): непустой сужает
// книгу до адресов, чей путь начинается с него, тем же правилом, что поток
// понятия; ни одного такого адреса — ошибка с ErrNoSuchRubric.
type ConceptBookSource interface {
	Concept(ctx context.Context, slug string, rubric []string) (*ConceptBook, error)
}

// ErrNoSuchRubric — у понятия нет такой подрубрики. Понятие есть, адрес
// верной формы, а сужать нечем: 404, а не 410 и не понятие целиком — модель,
// получившая всё понятие вместо подрубрики, сочла бы, что получила просимое.
var ErrNoSuchRubric = errors.New("такой подрубрики у понятия нет")

// CatalogSource — каталог для карты сайта (*repository.SEORepository).
type CatalogSource interface {
	Works(ctx context.Context) ([]repository.CatalogRow, error)
	Chapters(ctx context.Context) ([]repository.CatalogRow, error)
	Editions(ctx context.Context) ([]repository.CatalogRow, error)
	Concepts(ctx context.Context) ([]repository.CatalogRow, error)
	Collections(ctx context.Context) ([]repository.CatalogRow, error)
	// ConceptShelf — понятия-статьи по алфавиту с числом адресов, для
	// витрины /concepts.md.
	ConceptShelf(ctx context.Context) ([]repository.ConceptShelfRow, error)
	// Documents — те же строки без фильтра, что и Collections (черновики и
	// снятое тоже); отбор публичных — Go-стороной, см. isPublicDocumentRow
	// в sitemap.go.
	Documents(ctx context.Context) ([]repository.CatalogRow, error)
}

// Source собирает Doc для каждого вида страницы. Рендерер — метод на нём.
type Source struct {
	Works       WorkSource
	Chapters    ChapterSource
	Pages       PageSource
	Editions    EditionSource
	Concepts    ConceptSource
	Collections CollectionSource
	Documents   DocumentSource
	Books       BookSource
	// ConceptBooks — понятие указателя книгой, для .md понятия.
	ConceptBooks ConceptBookSource
	Catalog      CatalogSource
	Renderer     *markdown.Renderer
	// BaseURL — адрес читальни снаружи, из cfg.Server.PublicBaseURL. Тот же,
	// что печатает титульный лист выгрузок: два источника адреса в одной
	// программе разошлись бы, и canonical начал бы противоречить файлу.
	BaseURL string
	// Build — отметка сборки (pagecache.ResolveCommit, та же, что у файлового
	// кэша глав). Входит в ETag: вёрстка меняется с кодом, а не только с
	// текстом, и без отметки краулер с If-None-Match получал бы 304 на
	// страницу, собранную прежним рендерером, пока не поправят сам текст.
	Build string
}

// abs делает из пути читальни абсолютный адрес: canonical, og:url и карта
// сайта относительных адресов не принимают.
func (s *Source) abs(path string) string {
	return strings.TrimSuffix(s.BaseURL, "/") + path
}

// notFound — единственное место, где рождается ErrNotFound рендереров:
// сообщение с адресом попадёт в журнал, наружу через writeRenderError уйдёт
// 410 («было и нет»), а не голый 404 — саму форму адреса краулеру знать
// незачем, но код различает «отсутствует» от «известной формы адреса нет
// вовсе» (та отвечает 404 из match() до всякого рендера).
func notFound(what string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrNotFound, fmt.Sprintf(what, args...))
}

// neverPublished — черновик подборки или разбора: сущность существует, но её
// не видел никто, кроме владельца. В отличие от notFound (410, «было и снято»),
// writeRenderError отвечает на неё 404 — иначе сам факт существования
// черновика утёк бы постороннему через код ответа.
func neverPublished(what string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrNeverPublished, fmt.Sprintf(what, args...))
}

// isNotFound отличает «сущности нет» от поломки.
//
// Источник бывает двух видов, и вызывающему знать какой — незачем: сырой
// репозиторий отдаёт fmt.Errorf("... not found") без sentinel-ошибки (на
// pgx.ErrNoRows), а переводчик из internal/api (SEOBookSource,
// SEOCollectionSource) уже заворачивает ErrNotFound — и текст такой ошибки
// кончается не на "not found", а на "(collection not found)" или похожее.
// Проверяем сперва цепочку, потом текст: иначе каждый рендерер обязан
// помнить происхождение своего поля Source, и первая же подмена источника
// разойдётся молча — ровно это и случилось с подборкой, когда её
// репозиторий сменился переводчиком.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNotFound) {
		return true
	}
	return strings.HasSuffix(err.Error(), "not found")
}

// IsNotFound — isNotFound для соседних пакетов: MCP-сервер ходит в те же
// репозитории и обязан отличать «нет» от поломки тем же правилом.
func IsNotFound(err error) bool { return isNotFound(err) }

// isNeverPublished — черновик, которого снаружи не существует. Отличается от
// isNotFound тем, что смотрит ТОЛЬКО цепочку ошибок и никогда не судит по
// тексту: текст здесь ничего не решает, а перепутать эти две ветки — значит
// ответить 410 вместо 404 и тем подтвердить краулеру, что черновик есть.
func isNeverPublished(err error) bool {
	return err != nil && errors.Is(err, ErrNeverPublished)
}

// sentences склеивает непустые куски описания через точку с пробелом. Пустые
// поля у работ корпуса — норма (у части томов нет автора, у части — издания),
// и без отсева описание начиналось бы с «. , ».
func sentences(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			// Excerpt обрезает длинный кусок многоточием «…» (doc.go), а не
			// точкой — это отдельная руна, не последовательность из трёх
			// точек. TrimRight(p, ".") её не трогал, и склейка через ". "
			// давала «…. Адресов в корпусе: 141». Обе отбивки — в один
			// набор символов.
			kept = append(kept, strings.TrimRight(p, ".…"))
		}
	}
	return strings.Join(kept, ". ")
}
