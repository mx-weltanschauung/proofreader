package api

import (
	"context"
	"time"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

// PageStore is the slice of the page repository the HTTP handlers actually use.
// Declared here, on the consumer side, so handlers can be tested against a fake
// without a database. *repository.PageRepository satisfies it as written.
type PageStore interface {
	Create(ctx context.Context, page *models.Page) error
	GetByID(ctx context.Context, id int64) (*models.Page, error)
	GetByWorkAndPageNumber(ctx context.Context, workID int64, pageNumber int) (*models.Page, error)
	GetPageRange(ctx context.Context, workID int64, startPage, endPage int) ([]*models.Page, error)
	GetPagesByNumbers(ctx context.Context, workID int64, numbers []int) ([]*models.Page, error)
	ListByWork(ctx context.Context, workID int64) ([]*models.Page, error)
	ListPageMap(ctx context.Context, workID int64) ([]models.PageMapEntry, error)
	MaxPageNumber(ctx context.Context, workID int64) (int, error)
	// SaveEdit — единственная запись текста полосы, доступная обработчикам:
	// снимок прежнего текста и новый текст одной транзакцией. Голого Update
	// здесь нет намеренно. Он был, звал его ровно один applyPageEdit, и пара
	// «версия + запись» расходилась при сбое второго шага. Пока метод
	// доступен, четвёртый путь правки текста заводится одной строкой — а
	// CLAUDE.md запрещает это ровно потому, что забытое переякоривание
	// вырезок не падает, а молча уводит читателя не туда.
	SaveEdit(ctx context.Context, page *models.Page, version *models.PageVersion) error
}

// DocumentStore — срез репозитория разборов, которым пользуется обработчик.
// Объявлен ради проверяемости: разбор теперь собирается рендером в момент
// чтения, и эту сборку надо уметь прогнать без базы.
// *repository.DocumentRepository удовлетворяет ему как есть.
type DocumentStore interface {
	Create(ctx context.Context, document *models.Document) error
	GetByID(ctx context.Context, id int64) (*models.Document, error)
	// GetByAuthorSlug — чтение по АДРЕСУ разбора (пара «подпись + слаг»).
	// GetByID остаётся: обработчики адресуют разбор парой, а действуют по
	// document.ID — тот же приём, что у подборок (CollectionHandler.Delete
	// читает GetByAuthorSlug и удаляет по collection.ID).
	GetByAuthorSlug(ctx context.Context, nickname, slug string) (*models.Document, error)
	List(ctx context.Context, limit, offset int, ownerID *int64) ([]*models.Document, error)
	// ListPublished — витрина: только то, что на людях. Отдельный метод, а не
	// фильтр над List: черновик в публичном списке — та самая утечка, ради
	// которой модерация и заводится.
	ListPublished(ctx context.Context, limit, offset int) ([]*models.Document, error)
	ListByOwner(ctx context.Context, ownerID int64) ([]*models.Document, error)
	ListForReview(ctx context.Context) ([]*models.Document, error)
	// SubmitWithinLimit держит предел частоты и запись под одной блокировкой —
	// тот же приём, что у CollectionStore.PublishWithinLimit.
	SubmitWithinLimit(ctx context.Context, id int64, ipHash string, limit int, since time.Time) (bool, error)
	// Approve — реальная модерация: гвардировано атомарно, строка обязана
	// быть «на_рассмотрении» (repository.ErrDocumentNotPending иначе).
	Approve(ctx context.Context, id, moderatorID int64) error
	// PublishOwn — сотрудник публикует СВОЙ ЖЕ сотруднический разбор из
	// черновика, минуя очередь (DocumentReviewHandler.Submit); в отличие от
	// Approve, состояние не проверяет.
	PublishOwn(ctx context.Context, id, moderatorID int64) error
	Reject(ctx context.Context, id, moderatorID int64, reason models.DocumentRejectReason) error
	Unpublish(ctx context.Context, id int64) error
	Update(ctx context.Context, document *models.Document) error
	Delete(ctx context.Context, id int64) error
}

// PageVersionStore is the slice of the page-version repository the page
// handler uses. *repository.PageVersionRepository satisfies it as written.
//
// Объявлен ради проверяемости: с конкретным типом обработчик записи страницы
// нельзя прогнать без базы — nil-указатель паникует внутри репозитория.
// Записи здесь нет: снимок пишется вместе с текстом полосы, PageStore.SaveEdit.
type PageVersionStore interface {
	GetByID(ctx context.Context, id int64) (*models.PageVersion, error)
	ListByPage(ctx context.Context, pageID int64) ([]*models.PageVersion, error)
	GetLatestVersionNumber(ctx context.Context, pageID int64) (int, error)
}

// ChapterStore is the slice of the chapter repository the HTTP handlers use.
// *repository.ChapterRepository satisfies it as written.
type ChapterStore interface {
	Create(ctx context.Context, chapter *models.Chapter) error
	Delete(ctx context.Context, id int64) error
	GetByID(ctx context.Context, id int64) (*models.Chapter, error)
	ListByWorkHierarchical(ctx context.Context, workID int64) ([]*models.Chapter, error)
	Move(ctx context.Context, chapterID int64, newParentID *int64, newOrder int) error
	Update(ctx context.Context, chapter *models.Chapter) error
}

// ChapterGetter — срез репозитория глав для сброса кэша: одна глава по id.
// Узкий, а не ChapterStore: сбросу кэша незачем уметь править главы.
type ChapterGetter interface {
	GetByID(ctx context.Context, id int64) (*models.Chapter, error)
}

// ChapterTreeStore is the slice of the chapter repository the index handler uses.
//
// Нужен обработчику указателя лишь затем, чтобы назвать главу страницы
// в шапке фрагмента. Узкий срез, а не ChapterStore: править главы указателю незачем.
type ChapterTreeStore interface {
	ListByWorkHierarchical(ctx context.Context, workID int64) ([]*models.Chapter, error)
}

// WorkStore is the slice of the work repository the export and index
// handlers use. *repository.WorkRepository satisfies it as written.
type WorkStore interface {
	List(ctx context.Context, limit, offset int, status *models.WorkStatus, ownerID *int64) ([]*models.Work, error)
	GetByID(ctx context.Context, id int64) (*models.Work, error)
}

// WorkRepo — срез репозитория работ, которым пользуется WorkHandler.
// Объявлен на стороне потребителя, чтобы удаление тома вместе с хранилищем
// служебных работ проверялось подставным репозиторием, без базы.
// *repository.WorkRepository удовлетворяет ему как есть.
type WorkRepo interface {
	WorkStore
	Create(ctx context.Context, work *models.Work) error
	Update(ctx context.Context, work *models.Work) error
	Delete(ctx context.Context, id int64) error
	ListChildren(ctx context.Context, parentID int64) ([]*models.Work, error)
	AddCategory(ctx context.Context, workID, categoryID int64) error
	RemoveCategory(ctx context.Context, workID, categoryID int64) error
}

// EditionStore is the slice of the edition repository the handlers use.
// *repository.EditionRepository satisfies it as written.
type EditionStore interface {
	Create(ctx context.Context, edition *models.Edition) error
	Delete(ctx context.Context, id int64) error
	GetByID(ctx context.Context, id int64) (*models.Edition, error)
	List(ctx context.Context) ([]*models.Edition, error)
	ListWorks(ctx context.Context, editionID int64) ([]*models.Work, error)
	ListWorkSummaries(ctx context.Context, editionID int64) ([]*models.VolumeSummary, error)
	Update(ctx context.Context, edition *models.Edition) error
}

// ShelfEditions и ShelfWorks — то немногое, что главной нужно от двух
// репозиториев. Отдельными интерфейсами, а не расширением EditionStore и
// WorkRepo: полке нужно по одному-два метода от каждого, и подделкам остальных
// хендлеров незачем отращивать методы, которых они не зовут.
// *repository.EditionRepository и *repository.WorkRepository удовлетворяют им
// как есть.
type ShelfEditions interface {
	List(ctx context.Context) ([]*models.Edition, error)
	ListAllWorkSummaries(ctx context.Context) ([]*models.VolumeSummary, error)
}

// ShelfJournals — журналы для полки главной.
type ShelfJournals interface {
	List(ctx context.Context) ([]models.JournalSummary, error)
}

// JournalIssueLookup — журнальные координаты работы-номера.
type JournalIssueLookup interface {
	IssueForWork(ctx context.Context, workID int64) (*models.WorkJournalIssue, error)
}

// ShelfWorks — работы, не приписанные ни к одному собранию.
type ShelfWorks interface {
	ListWithoutEdition(ctx context.Context) ([]models.ShelfWork, error)
}

// HighlightStore — избранное собрания для экрана правки и PUT.
// *repository.EditionRepository удовлетворяет ему как есть.
type HighlightStore interface {
	GetByID(ctx context.Context, id int64) (*models.Edition, error)
	ListHighlights(ctx context.Context, editionID int64) ([]models.EditionHighlight, error)
	ReplaceHighlights(ctx context.Context, editionID int64, items []models.HighlightInput) error
}

// IndexStore is the slice of the index repository the handlers use.
// *repository.IndexRepository satisfies it as written.
type IndexStore interface {
	ReplaceForEdition(ctx context.Context, editionID int64, concepts []*models.IndexConcept) ([]string, error)
	GetConceptBySlug(ctx context.Context, slug string) (*models.IndexConcept, error)
	ArticleLinks(ctx context.Context, articleID int64) ([]repository.LinkWithTarget, error)
	IncomingLinks(ctx context.Context, conceptID int64) ([]repository.IncomingLink, error)
	ListConcepts(ctx context.Context, query, letter string, limit, offset int) ([]*models.IndexConcept, error)
	VolumeMap(ctx context.Context, editionID int64) ([]models.VolumeLocation, error)
	Backlinks(ctx context.Context, editionID int64, volumeNumber int, volumePart *string, printedPage int) ([]models.ConceptBacklink, error)
	TakenSlugs(ctx context.Context) (map[string]bool, error)
}

// FragmentStore is the slice of the fragment repository the handlers use.
// *repository.IndexFragmentRepository satisfies it as written.
type FragmentStore interface {
	ByReferences(ctx context.Context, referenceIDs []int64) (map[int64][]*models.IndexFragment, error)
	ByPage(ctx context.Context, pageID int64) ([]*models.IndexFragment, error)
	ReplaceForReference(ctx context.Context, referenceID int64, fragments []*models.IndexFragment) error
	UpdateAnchor(ctx context.Context, id int64, startOffset, endOffset int, startHash, endHash string) error
	SetStatus(ctx context.Context, id int64, status string) error
}

// DocumentCutStore — то, что обработчики и переякоривание знают о вклейках.
// *repository.DocumentCutRepository удовлетворяет ему как есть.
type DocumentCutStore interface {
	ByDocument(ctx context.Context, documentID int64) ([]*models.DocumentCut, error)
	ByIDs(ctx context.Context, documentID int64, ids []int64) ([]*models.DocumentCut, error)
	ByPage(ctx context.Context, pageID int64) ([]*models.DocumentCut, error)
	Create(ctx context.Context, cut *models.DocumentCut) error
	DeleteUnreferenced(ctx context.Context, documentID int64, keep []int64) error
	UpdateAnchor(ctx context.Context, id int64, startOffset, endOffset int, startHash, endHash string) error
	SetStatus(ctx context.Context, id int64, status string) error
	PagesOfCut(ctx context.Context, cut *models.DocumentCut) ([]*models.Page, error)
}

// CollectionStore is the slice of the collection repository the handlers use.
// *repository.CollectionRepository satisfies it as written.
type CollectionStore interface {
	Create(ctx context.Context, c *models.Collection) error
	GetBySlug(ctx context.Context, slug string) (*models.Collection, error)
	// GetByAuthorSlug ищет по паре «ник автора + слаг» — второй, более длинный
	// вид адреса подборки (/collections/{ник}/{слаг}). Пустой ник — то же, что
	// GetBySlug.
	GetByAuthorSlug(ctx context.Context, nickname, slug string) (*models.Collection, error)
	List(ctx context.Context) ([]*models.Collection, error)
	ListByOwner(ctx context.Context, ownerID int64) ([]*models.Collection, error)
	Update(ctx context.Context, c *models.Collection) error
	Delete(ctx context.Context, id int64) error
	// PublishWithinLimit держит предел частоты (3 в сутки) и запись под одной
	// блокировкой — тот же приём, что у PageSuggestionStore.CreateWithinLimit.
	PublishWithinLimit(ctx context.Context, id int64, ipHash string, limit int, since time.Time) (bool, time.Time, error)
	Unpublish(ctx context.Context, id int64) error
	AddItem(ctx context.Context, collectionID int64, kind string, chapterID, workID *int64, authorOverride string) (*models.CollectionItem, error)
	UpdateItemAuthor(ctx context.Context, collectionID, itemID int64, authorOverride string) error
	DeleteItem(ctx context.Context, collectionID, itemID int64) error
	MoveItem(ctx context.Context, collectionID, itemID int64, newOrder int) error
	ItemByID(ctx context.Context, collectionID, itemID int64) (*models.CollectionItem, error)
	ItemRows(ctx context.Context, collectionID int64) ([]repository.ItemRow, error)
	ChaptersForWorks(ctx context.Context, workIDs []int64) ([]*models.Chapter, error)
}

// PageSuggestionStore is the slice of the suggestion repository the handler
// uses. *repository.PageSuggestionRepository satisfies it as written.
type PageSuggestionStore interface {
	// CreateWithinLimit держит предел частоты и запись под одной блокировкой.
	// Отдельного счётчика в интерфейсе нет намеренно: счёт порознь со
	// вставкой — та самая гонка, из-за которой предел обходился залпом.
	CreateWithinLimit(ctx context.Context, s *models.PageSuggestion, limit int, since time.Time) (bool, error)
	List(ctx context.Context, status *models.PageSuggestionStatus, limit, offset int) ([]repository.SuggestionRow, int, error)
	ListByUserID(ctx context.Context, userID int64) ([]repository.SuggestionRow, error)
	GetDetail(ctx context.Context, id int64) (*repository.SuggestionDetail, error)
	Resolve(ctx context.Context, id int64, status models.PageSuggestionStatus, reason *models.RejectReason, moderatorID int64) error
	Delete(ctx context.Context, id int64) error
	DeleteRejected(ctx context.Context) (int64, error)
}

// SearchStore is the slice of the search repository the handler uses.
// *repository.SearchRepository satisfies it as written. Единственная
// реализация — Postgres FTS; замена движка меняет её и ничего больше.
type SearchStore interface {
	// Terms — леммы разобранного запроса без исключённых; пустой срез —
	// запрос пуст после разбора (обработчик отвечает 400).
	Terms(ctx context.Context, q models.SearchQuery) ([]string, error)
	Search(ctx context.Context, q models.SearchQuery) (*models.SearchResult, error)
	SearchPages(ctx context.Context, q models.SearchQuery, workID int64, chapterIDs []int64, limit, offset int) (*models.SearchPagesResult, error)
}

// JournalStore — журналы и номера (repository.JournalRepository).
type JournalStore interface {
	List(ctx context.Context) ([]models.JournalSummary, error)
	GetByID(ctx context.Context, id int64) (*models.Journal, error)
	Detail(ctx context.Context, slug string) (*models.JournalDetail, error)
	Create(ctx context.Context, j *models.Journal) error
	Update(ctx context.Context, j *models.Journal) error
	CreateIssue(ctx context.Context, issue *models.JournalIssue, work *models.Work) error
	GetIssue(ctx context.Context, id int64) (*models.JournalIssue, error)
	UpdateIssue(ctx context.Context, issue *models.JournalIssue, title string) error
}

// PersonStore — люди (repository.PersonRepository).
type PersonStore interface {
	Create(ctx context.Context, p *models.Person) error
	Update(ctx context.Context, p *models.Person) error
	GetByID(ctx context.Context, id int64) (*models.Person, error)
	Detail(ctx context.Context, slug string) (*models.PersonDetail, error)
	Search(ctx context.Context, q string, limit int) ([]models.Person, error)
	Merge(ctx context.Context, intoID, fromID int64) error
}

// CreditLister — подписи статей работы.
type CreditLister interface {
	ListCreditsByWork(ctx context.Context, workID int64) (map[int64][]models.ArticleCredit, error)
}

// CreditStore — подписи статей: чтение и замена.
type CreditStore interface {
	CreditLister
	ReplaceCredits(ctx context.Context, chapterID int64, credits []models.CreditInput) error
}
