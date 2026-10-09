package models

import "time"

// UserRole represents user roles in the system
type UserRole string

const (
	RoleAdministrator UserRole = "administrator"
	RoleEditor        UserRole = "editor"
	// RoleReader — читатель: заводится сам, владеет своими правками и
	// подборками. Дремлющее "proofreader" не переиспользуем: в этом проекте
	// «вычитчик» — занятое слово, и омоним разойдётся в первый же спор.
	RoleReader UserRole = "reader"
)

// PageStatus represents the status of a page
type PageStatus string

const (
	PageStatusNotProofread     PageStatus = "не_вычитана"
	PageStatusInProgress       PageStatus = "вычитывается"
	PageStatusProofread        PageStatus = "вычитана"
	PageStatusHasIssues        PageStatus = "есть_проблемы"
	PageStatusEmpty            PageStatus = "пустая_страница"
	PageStatusMachineProofread PageStatus = "вычитано_машиной"
	PageStatusNeedsAttention   PageStatus = "требует_внимания"
)

// WorkStatus represents the status of a work
type WorkStatus string

const (
	WorkStatusDraft      WorkStatus = "draft"
	WorkStatusInProgress WorkStatus = "in_progress"
	WorkStatusCompleted  WorkStatus = "completed"
	WorkStatusArchived   WorkStatus = "archived"
)

// Роли работы. volume — том издания, front_matter — его передние листы
// (обложка, титул, содержание, предисловие) с собственной нумерацией.
const (
	WorkRoleVolume             = "volume"
	WorkRoleFrontMatter        = "front_matter"
	WorkRoleEditionFrontMatter = "edition_front_matter"
	// WorkRoleJournalIssue — работа номера журнала: без родителя и без
	// издания; координаты номера — в journal_issues.
	WorkRoleJournalIssue = "journal_issue"
)

// Стиль печатной колонцифры. Печатный номер считается как
// page_number + page_offset и рендерится в этом стиле.
const (
	NumberingArabic = "arabic"
	NumberingRoman  = "roman"
)

// User represents a user in the system
type User struct {
	ID           int64    `json:"id" db:"id"`
	Email        string   `json:"email" db:"email"`
	PasswordHash string   `json:"-" db:"password_hash"`
	Role         UserRole `json:"role" db:"role"`
	// Nickname есть только у читателя; сотрудник входит почтой. Пустой ник в
	// адресе подборки означает «сотрудническая».
	Nickname *string `json:"nickname,omitempty" db:"nickname"`
	// SignupIPHash — отметка адреса, с которого завелась учётная запись.
	// Сам адрес не хранится нигде, как и у писем.
	SignupIPHash string    `json:"-" db:"signup_ip_hash"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// Work represents a literary work
type Work struct {
	ID    int64  `json:"id" db:"id"`
	Title string `json:"title" db:"title"`
	// Slug — человекочитаемый хвост адреса (`lenin-t06`). Ключом не является:
	// том ищется по номеру, и пустой слаг даёт голый номер в адресе — вид
	// рабочий, краулеру он отдаётся с 301 на канон. Считается на чтении, в
	// базе не хранится: заголовки корпуса правит машинная вычитка, и колонка
	// разошлась бы с ними молча.
	Slug string `json:"slug" db:"-"`
	// EditionTitle — название собрания, к которому принадлежит работа
	// («К. Маркс и Ф. Энгельс. Сочинения, 2-е изд.»). Как и Slug, считается
	// на чтении и в базе не хранится: хранимая копия разошлась бы с
	// editions.title молча. Нужна подписи цитаты, которая собирается на
	// клиенте в пределах жеста — четвёртому запросу в этот момент места нет.
	// Пустая строка — работа вне собрания.
	EditionTitle    string     `json:"edition_title" db:"-"`
	Author          string     `json:"author" db:"author"`
	PublicationDate *time.Time `json:"publication_date,omitempty" db:"publication_date"`
	Language        string     `json:"language" db:"language"`
	Country         string     `json:"country" db:"country"`
	FilePath        string     `json:"file_path" db:"file_path"`
	Status          WorkStatus `json:"status" db:"status"`
	EditionID       *int64     `json:"edition_id,omitempty" db:"edition_id"`
	VolumeNumber    *int       `json:"volume_number,omitempty" db:"volume_number"`
	VolumePart      *string    `json:"volume_part,omitempty" db:"volume_part"`
	PageOffset      int        `json:"page_offset" db:"page_offset"`
	// ParentWorkID связывает служебную работу (передние листы) с томом.
	// У тома всегда nil — это гарантирует works_parent_role_check.
	ParentWorkID   *int64 `json:"parent_work_id,omitempty" db:"parent_work_id"`
	Role           string `json:"role" db:"role"`
	NumberingStyle string `json:"numbering_style" db:"numbering_style"`
	// PrecedesVolume — том, перед которым работа стоит на полке издания.
	// Только у edition_front_matter; у тома и передних листов nil.
	PrecedesVolume *int `json:"precedes_volume,omitempty" db:"precedes_volume"`
	// ShelfLabel — подпись корешка, заданная руками. Пустая строка означает
	// «подписи нет»: тогда её выводит фронт из top_chapters.
	ShelfLabel string `json:"shelf_label" db:"shelf_label"`
	// Description — свободный текст о томе, заданный руками: почему в скане не
	// хватает полос, откуда взят источник. Пустая строка — описания нет.
	Description string    `json:"description" db:"description"`
	OwnerID     int64     `json:"owner_id" db:"owner_id"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// TopChapter is one of a volume's largest top-level chapters - the works the
// volume is known for. In a collected-works edition the volume titles are
// identical, so this is what tells one volume from another.
type TopChapter struct {
	Title string `json:"title"`
	Pages int    `json:"pages"`
	// Share — доля главы в объёме тома, 0..1. Знаменатель — сумма
	// протяжённостей глав верхнего уровня БЕЗ аппарата: примечания,
	// указатели, списки и приложения не работы, и в объёме, по которому
	// меряется главная работа, им не место.
	Share float64 `json:"share"`
}

// VolumeSummary is a Work plus the aggregates the shelf needs. Work is
// embedded, so the JSON keeps the shape /editions/{id}/works had before -
// the aggregates are added alongside, nothing moves.
type VolumeSummary struct {
	Work
	PagesTotal    int            `json:"pages_total"`
	PagesByStatus map[string]int `json:"pages_by_status"`
	ChaptersTotal int            `json:"chapters_total"`
	// TopChapters — до четырёх крупнейших неаппаратных глав верхнего уровня,
	// по убыванию объёма. Пустой список у тома без глав; omitempty его
	// прячет, и ключа в JSON тогда нет вовсе.
	TopChapters []TopChapter `json:"top_chapters,omitempty"`
}

// ShelfWork — работа вне собраний, как её видит главная: имя да ссылка.
// Агрегатов у неё нет и корешок из неё не нарисовать, поэтому полной строки
// works тут не нужно.
type ShelfWork struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	// Slug — хвост адреса; у работы вне собрания собирается из заголовка.
	Slug string `json:"slug"`
}

// ShelfEdition — одна полка: собрание со своими томами в порядке издания.
type ShelfEdition struct {
	Edition *Edition         `json:"edition"`
	Volumes []*VolumeSummary `json:"volumes"`
}

// Shelf — весь ответ /api/shelf, то есть всё, что рисует главная. Собран одним
// ответом намеренно: прежде страница брала список собраний, потом по запросу
// на собрание за томами, и вторая волна не могла начаться, пока не ответила
// первая.
type Shelf struct {
	Editions   []ShelfEdition `json:"editions"`
	LooseWorks []ShelfWork    `json:"loose_works"`
	// Journals — журналы, у которых есть хоть один номер; всегда массив.
	Journals []JournalSummary `json:"journals"`
}

// Category represents a work category
type Category struct {
	ID          int64     `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Slug        string    `json:"slug" db:"slug"`
	Description string    `json:"description" db:"description"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// Page represents a page of a work
type Page struct {
	ID              int64      `json:"id" db:"id"`
	WorkID          int64      `json:"work_id" db:"work_id"`
	PageNumber      int        `json:"page_number" db:"page_number"`
	PreviewPath     string     `json:"preview_path" db:"preview_path"`
	ContentMarkdown string     `json:"content_markdown" db:"content_markdown"`
	Status          PageStatus `json:"status" db:"status"`
	ChapterID       *int64     `json:"chapter_id,omitempty" db:"chapter_id"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`

	// TextEditedAt — когда текст полосы правили в последний раз:
	// MAX(page_versions.created_at) СРЕДИ ВЕРСИЙ, чей текст отличается от
	// текущего content_markdown полосы. Не updated_at — его триггер двигает и
	// смена статуса без правки текста, то есть он соврал бы молча; и не голый
	// MAX по всем версиям — applyPageEdit пишет версию безусловно, в том
	// числе когда новый текст равен прежнему (так шлёт машинная вычитка,
	// меняя только статус), и такая холостая запись двигала бы дату вслед за
	// собой, снова молча. Замер на живом корпусе (37% полос, 21 день среднего
	// опоздания) — в докблоке repository.textEditedAtSubquery.
	//
	// Заполняется ТОЛЬКО одиночными чтениями (GetByID,
	// GetByWorkAndPageNumber): показывается на экране полосы, а в карте тома
	// стоил бы подзапроса на каждую из 840 строк. В списочных ответах поле
	// отсутствует, и это не «правок не было» — там его просто не спрашивали;
	// читать его где-либо, кроме экрана полосы, нельзя.
	TextEditedAt *time.Time `json:"text_edited_at,omitempty" db:"-"`
}

// PageMapEntry — строка карты страниц тома: всё, что нужно, чтобы нарисовать
// обрез, и ничего сверх того. Текст страницы и подписанные URL превью в карту
// не входят: на томе в 840 страниц они дают 4.2 МБ ответа.
type PageMapEntry struct {
	PageNumber int        `json:"page_number"`
	Status     PageStatus `json:"status"`
}

// PageVersion represents a historical version of a page
type PageVersion struct {
	ID              int64     `json:"id" db:"id"`
	PageID          int64     `json:"page_id" db:"page_id"`
	ContentMarkdown string    `json:"content_markdown" db:"content_markdown"`
	VersionNumber   int       `json:"version_number" db:"version_number"`
	UserID          int64     `json:"user_id" db:"user_id"`
	Comment         string    `json:"comment" db:"comment"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
}

// Chapter represents a chapter or part of a work
type Chapter struct {
	ID       int64  `json:"id" db:"id"`
	WorkID   int64  `json:"work_id" db:"work_id"`
	ParentID *int64 `json:"parent_id,omitempty" db:"parent_id"`
	Title    string `json:"title" db:"title"`
	// Slug — хвост адреса главы; пуст у главы-нумератора («2», «II»).
	// Ключом не является, считается на чтении из заголовка.
	Slug        string `json:"slug" db:"-"`
	Type        string `json:"type" db:"type"` // "chapter" or "part"
	OrderNumber int    `json:"order_number" db:"order_number"`
	StartPage   int    `json:"start_page" db:"start_page"`
	EndPage     int    `json:"end_page" db:"end_page"`
	// IsApparatus — глава принадлежит аппарату тома (примечания, указатели,
	// списки, приложения), а не является произведением. При создании ставится
	// классификатором по заголовку, дальше правится руками: границы тонкие.
	IsApparatus bool `json:"is_apparatus" db:"is_apparatus"`
	// ArticleKind — вид статьи журнала (models.ArticleKinds); nil — не статья.
	ArticleKind *string `json:"article_kind,omitempty" db:"article_kind"`
	// Credits — подпись статьи; заполняется обработчиком, в базе — article_credits.
	Credits   []ArticleCredit `json:"credits,omitempty" db:"-"`
	CreatedAt time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt time.Time       `json:"updated_at" db:"updated_at"`
	Children  []*Chapter      `json:"children,omitempty" db:"-"`
}

// Footnote represents a footnote in a page
type Footnote struct {
	ID              int64     `json:"id" db:"id"`
	PageID          int64     `json:"page_id" db:"page_id"`
	ContentMarkdown string    `json:"content_markdown" db:"content_markdown"`
	OrderNumber     int       `json:"order_number" db:"order_number"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
}

// StatusHistory represents a history of status changes
type StatusHistory struct {
	ID         int64     `json:"id" db:"id"`
	EntityType string    `json:"entity_type" db:"entity_type"` // "work" or "page"
	EntityID   int64     `json:"entity_id" db:"entity_id"`
	Status     string    `json:"status" db:"status"`
	UserID     int64     `json:"user_id" db:"user_id"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

// Document represents a markdown document with embedded page content
type Document struct {
	ID              int64  `json:"id" db:"id"`
	Title           string `json:"title" db:"title"`
	MarkdownContent string `json:"markdown_content" db:"markdown_content"`
	// OwnerID — nullable с миграции 000024 (ON DELETE SET NULL у автора).
	// Указатель, а не голое int64: без него pgx роняет скан на NULL, и
	// document_handler.Get сворачивает эту ошибку в «не найдено».
	OwnerID *int64 `json:"owner_id" db:"owner_id"`
	// AuthorNickname — снимок подписи автора. Ник есть только у читателя, так
	// что у сотруднического разбора он пуст (claims.Nickname сотрудника —
	// пустая строка) — тот же признак, что отличает витринную подборку от
	// читательской. Снимок, а не ссылка на владельца: удалил автор учётку —
	// подпись остаётся, а разбор перестаёт быть редактируемым вовсе
	// (mayEditDocument).
	AuthorNickname string `json:"author_nickname" db:"author_nickname"`
	// Slug — адрес разбора. Ключ вместе с AuthorNickname: пара уникальна,
	// и она же выбирает вид адреса — /documents/{слаг} у сотруднического
	// разбора (пустой ник), /documents/{ник}/{слаг} у читательского. Лепит
	// его сервер из заглавия при создании и больше не меняет: переименование
	// заглавия не двигает адрес, иначе внешняя ссылка протухает молча.
	Slug string `json:"slug" db:"slug"`
	// PublishedTitle/PublishedMarkdown — ОДОБРЕННАЯ редакция, то, что на
	// людях. Title/MarkdownContent при этом черновик автора: правка
	// опубликованного не меняет показываемого, пока её не одобрили, иначе
	// модерация — театр (пропустить безобидное, потом переписать).
	PublishedTitle    string     `json:"published_title" db:"published_title"`
	PublishedMarkdown string     `json:"published_markdown" db:"published_markdown"`
	PublishedAt       *time.Time `json:"published_at" db:"published_at"`
	// WasPublished отличает «никогда не публиковался» (404) от «было и
	// снято» (410) — ровно та роль, которую у подборки играет
	// PublishIPHash. Отдельной колонкой, а не выводом из отметки адреса: у
	// разбора отметка привязана к ОТПРАВКЕ на проверку, а не к публикации.
	WasPublished bool                  `json:"was_published" db:"was_published"`
	ReviewStatus DocumentReviewStatus  `json:"review_status" db:"review_status"`
	RejectReason *DocumentRejectReason `json:"reject_reason,omitempty" db:"reject_reason"`
	ModeratorID  *int64                `json:"moderator_id,omitempty" db:"moderator_id"`
	ReviewedAt   *time.Time            `json:"reviewed_at,omitempty" db:"reviewed_at"`
	// SubmittedAt/SubmitIPHash — рельс предела частоты отправки на
	// рассмотрение. Отметка адреса наружу не отдаётся, как и везде.
	SubmittedAt  *time.Time `json:"submitted_at,omitempty" db:"submitted_at"`
	SubmitIPHash string     `json:"-" db:"submit_ip_hash"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at" db:"updated_at"`
}

// Edition is a collected-works series: the structural parent of volume works.
type Edition struct {
	ID    int64  `json:"id" db:"id"`
	Title string `json:"title" db:"title"`
	Slug  string `json:"slug" db:"slug"`
	// URLSlug — короткий слаг для адресов читальни (lenin, mae). Пусто —
	// адрес откатывается на Slug. Slug при этом трогать нельзя: он ключ
	// журнала публикации.
	URLSlug     string `json:"url_slug" db:"url_slug"`
	Description string `json:"description" db:"description"`
	// Сколько томов в собрании по плану издания; пусто — план неизвестен.
	// Полка на главной пишет по нему «45 из 55»: сколько томов ещё не снято
	// со сканов, из самих данных о работах не выводится никак.
	VolumesPlanned *int      `json:"volumes_planned,omitempty" db:"volumes_planned"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" db:"updated_at"`
}

// Index concept kinds and link kinds.
const (
	IndexConceptKindArticle  = "article"
	IndexConceptKindRedirect = "redirect"
	IndexLinkKindSee         = "see"
	IndexLinkKindSeeAlso     = "see_also"
)

// IndexConcept is the catalog-level entry of a subject index ("Абстрактный
// труд"): a title plus one or more IndexArticles (one per index volume that
// carries a piece about it). The article-shaped fields (work, markdown, source
// pages, kind, references) lived here until migration 000026 — they now
// belong to IndexArticle, since one concept can have several.
type IndexConcept struct {
	ID       int64  `json:"id" db:"id"`
	Title    string `json:"title" db:"title"`
	Slug     string `json:"slug" db:"slug"`
	SortKey  string `json:"sort_key" db:"sort_key"`
	TitleKey string `json:"title_key" db:"title_key"`
	// Kind — "article" если хоть одна статья понятия несёт разбор, иначе
	// "redirect". Вычисляется listConceptsQuery (CASE … EXISTS), не хранится:
	// понятие само по себе редиректом или статьёй не бывает, это свойство
	// его статей.
	Kind     string              `json:"kind" db:"-"`
	Links    []*IndexConceptLink `json:"links,omitempty" db:"-"`
	Articles []*IndexArticle     `json:"articles,omitempty" db:"-"`
	// EditionIDs — издания, у которых есть статья об этом понятии
	// (index_concept_articles.edition_id, агрегат listConceptsQuery в
	// index_repository.go). Питоновский публикатор (apply_index.count_existing,
	// publish_volume.index_payload) считает и отбирает понятия по этому полю,
	// а не по WorkID — держи агрегат живым, если снова правишь
	// listConceptsQuery.
	EditionIDs []int64   `json:"edition_ids" db:"-"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time `json:"updated_at" db:"updated_at"`
}

// IndexArticle — статья одного указателя о понятии.
//
// EditionID обязателен, WorkID — нет: резолв адресов идёт через издание
// (VolumeMap), а работа-указатель есть в корпусе не у всякого собрания —
// у Ленина указатель взят с внешнего источника, и его адрес лежит в SourceURL.
type IndexArticle struct {
	ID              int64               `json:"id" db:"id"`
	ConceptID       int64               `json:"concept_id" db:"concept_id"`
	EditionID       int64               `json:"edition_id" db:"edition_id"`
	EditionTitle    string              `json:"edition_title" db:"-"`
	WorkID          *int64              `json:"work_id,omitempty" db:"work_id"`
	SourceURL       string              `json:"source_url" db:"source_url"`
	Title           string              `json:"title" db:"title"`
	TitleKey        string              `json:"title_key" db:"title_key"`
	ArticleMarkdown string              `json:"article_markdown" db:"article_markdown"`
	Kind            string              `json:"kind" db:"kind"`
	SourcePageStart int                 `json:"source_page_start" db:"source_page_start"`
	SourcePageEnd   int                 `json:"source_page_end" db:"source_page_end"`
	References      []*IndexReference   `json:"references,omitempty" db:"-"`
	Links           []*IndexConceptLink `json:"links,omitempty" db:"-"`
	CreatedAt       time.Time           `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at" db:"updated_at"`
}

// IndexRubric — подрубрика внутри статьи. Хранится строкой таблицы, а не
// текстом на каждом адресе: у ленинского указателя 5136 подрубрик на 199 тысяч
// адресов, и текстом это было бы 199 тысяч копий строки.
type IndexRubric struct {
	ID          int64  `json:"id" db:"id"`
	ArticleID   int64  `json:"article_id" db:"article_id"`
	Title       string `json:"title" db:"title"`
	TitleKey    string `json:"title_key" db:"title_key"`
	OrderNumber int    `json:"order_number" db:"order_number"`
}

// IndexReference is one address in a volume. PageStart/PageEnd are PRINTED
// page numbers; resolving them into a work's page_number needs the volume's
// page_offset.
type IndexReference struct {
	ID           int64   `json:"id" db:"id"`
	ArticleID    int64   `json:"article_id" db:"article_id"`
	RubricID     *int64  `json:"rubric_id,omitempty" db:"rubric_id"`
	VolumeNumber int     `json:"volume_number" db:"volume_number"`
	VolumePart   *string `json:"volume_part,omitempty" db:"volume_part"`
	PageStart    int     `json:"page_start" db:"page_start"`
	PageEnd      int     `json:"page_end" db:"page_end"`
	// Rubric — название подрубрики, заполняется джойном при чтении
	// (COALESCE(ru.title, '') по RubricID); в index_references не хранится.
	Rubric string `json:"rubric" db:"rubric"`
	// RubricPath — путь подрубрик от корня к листу. Пуст или отсутствует —
	// адрес висит прямо на статье. Длина 1 — прежний плоский случай. Длина
	// 2 — печатная вложенность «КПСС — съезды» → «I съезд РСДРП…» →
	// «значение съезда». Поле необязательное: тело ввоза шлют два
	// разборщика, и марксов (parse_index.py) о вложенности не знает — при
	// отсутствии RubricPath путь строится из Rubric.
	RubricPath  []string `json:"rubric_path,omitempty" db:"-"`
	OrderNumber int      `json:"order_number" db:"order_number"`
	IsUncertain bool     `json:"is_uncertain" db:"is_uncertain"`
	Note        string   `json:"note,omitempty" db:"note"`
}

// IndexConceptLink is a "см." / "см. также" pointer between concepts.
//
// Отсылка принадлежит СТАТЬЕ (FromArticleID), а не понятию каталога — у
// понятия с несколькими статьями каждая несёт собственный набор отсылок.
type IndexConceptLink struct {
	ID            int64  `json:"id" db:"id"`
	FromArticleID int64  `json:"from_article_id" db:"from_article_id"`
	ToConceptID   *int64 `json:"to_concept_id,omitempty" db:"to_concept_id"`
	TargetTitle   string `json:"target_title" db:"target_title"`
	Kind          string `json:"kind" db:"kind"`
	OrderNumber   int    `json:"order_number" db:"order_number"`
}

// Fragment statuses. machine — нарезано машиной, человек не смотрел;
// confirmed — человек подтвердил или поправил; stale — после правки страницы
// переякорить не удалось.
const (
	FragmentStatusMachine   = "machine"
	FragmentStatusConfirmed = "confirmed"
	FragmentStatusStale     = "stale"
)

// Anchor — то, чем кусок полосы держится за живой текст: пара границ и пара
// цитат по краям. Общая часть вырезки предметного указателя и вклейки
// разбора; переякоривание у них одно на двоих, а словари состояний разные,
// поэтому состояние сюда не входит.
//
// Смещения БАЙТОВЫЕ, по content_markdown.
//
// Все поля — json:"-": наружу ходит номер полосы, а не pages.id (решение о
// грамматике адреса, совет Internet Archive не светить внутренний счёт), и
// смещения/хэши/цитаты — механика переякоривания, читателю её показывать не
// за чем. Тег стоит на самом типе, а не на вызывающей стороне: у DocumentCut
// Anchor встроен безымянным полем, и без json:"-" здесь export полей
// сериализовался бы под их именами в любом месте, где DocumentCut (или
// будущий второй потребитель Anchor) уйдёт в JSON — один пропущенный DTO не
// прикроет остальные.
type Anchor struct {
	StartPageID int64  `json:"-"`
	StartOffset int    `json:"-"`
	EndPageID   int64  `json:"-"`
	EndOffset   int    `json:"-"`
	HeadQuote   string `json:"-"`
	TailQuote   string `json:"-"`
	StartHash   string `json:"-"`
	EndHash     string `json:"-"`
}

// IndexFragment is one cut: the passage of a page where the concept is
// actually developed, as opposed to the whole page the printed index
// addresses.
//
// Смещения БАЙТОВЫЕ, по content_markdown. Цитаты — то, чем вырезка держится
// за текст: страницы правят и после нарезки, смещения от этого уезжают.
type IndexFragment struct {
	ID          int64  `json:"id" db:"id"`
	ReferenceID int64  `json:"reference_id" db:"reference_id"`
	OrderNumber int    `json:"order_number" db:"order_number"`
	StartPageID int64  `json:"start_page_id" db:"start_page_id"`
	StartOffset int    `json:"start_offset" db:"start_offset"`
	EndPageID   int64  `json:"end_page_id" db:"end_page_id"`
	EndOffset   int    `json:"end_offset" db:"end_offset"`
	HeadQuote   string `json:"head_quote" db:"head_quote"`
	TailQuote   string `json:"tail_quote" db:"tail_quote"`
	StartHash   string `json:"start_hash" db:"start_hash"`
	EndHash     string `json:"end_hash" db:"end_hash"`
	Status      string `json:"status" db:"status"`
}

// Anchor отдаёт якорь вырезки общему слою.
func (f *IndexFragment) Anchor() Anchor {
	return Anchor{
		StartPageID: f.StartPageID, StartOffset: f.StartOffset,
		EndPageID: f.EndPageID, EndOffset: f.EndOffset,
		HeadQuote: f.HeadQuote, TailQuote: f.TailQuote,
		StartHash: f.StartHash, EndHash: f.EndHash,
	}
}

// Cut statuses. Свой, а не общий с вырезкой словарь: machine|confirmed у
// вырезки значат «нарезала машина»/«подтвердил человек», а границы вклейки
// человек ставит с самого начала — подтверждать нечего.
const (
	// CutStatusOK — вклейка стоит на своём месте.
	CutStatusOK = "ok"
	// CutStatusStale — якорь не нашёлся после правки полосы: показывать
	// прежний срез нельзя, смещения уехали и вырежут чужой кусок.
	CutStatusStale = "stale"
)

// DocumentCut — вклейка: кусок корпуса внутри разбора.
//
// От вырезки указателя отличается тремя вещами, и каждая — решение, а не
// совпадение: полосы уходят в NULL, а не каскадом (битую вклейку читатель
// обязан увидеть); подпись источника лежит снимком на случай их исчезновения;
// словарь состояний свой.
type DocumentCut struct {
	ID         int64  `json:"id" db:"id"`
	DocumentID int64  `json:"document_id" db:"document_id"`
	WorkID     *int64 `json:"work_id" db:"work_id"`
	Anchor
	Status      string    `json:"status" db:"status"`
	SourceTitle string    `json:"source_title" db:"source_title"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

// Broken — источника больше нет: том или полосы сняли, ссылки обнулены.
// Отличается от stale тем, что идти некуда, и показывается иначе.
func (c *DocumentCut) Broken() bool {
	return c.StartPageID == 0 || c.EndPageID == 0 || c.WorkID == nil
}

// VolumeLocation is what the resolver needs about one volume of an edition.
type VolumeLocation struct {
	VolumeNumber int     `json:"volume_number"`
	VolumePart   *string `json:"volume_part,omitempty"`
	WorkID       int64   `json:"work_id"`
	// WorkSlug — хвост адреса тома; адресная панель понятия строит по нему
	// /works/{id}-{slug}/pages/{page}, а не голый номер.
	WorkSlug   string `json:"work_slug"`
	PageOffset int    `json:"page_offset"`
	MaxPage    int    `json:"max_page"`
}

// ConceptBacklink is one concept described on a given page of a volume.
type ConceptBacklink struct {
	ConceptID int64  `json:"concept_id"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Rubric    string `json:"rubric"`
	PageStart int    `json:"page_start"`
	PageEnd   int    `json:"page_end"`
	// RubricPath — путь подрубрик от корня к листу, как у IndexReference
	// (см. её комментарий). Пуст, если адрес висит прямо на статье.
	RubricPath []string `json:"rubric_path,omitempty"`
}

// Вид элемента подборки. Хранится явной колонкой, а не выводится из того,
// какая ссылка заполнена: у элемента с удалённым источником chapter_id пуст.
const (
	CollectionItemKindChapter = "chapter"
	CollectionItemKindWork    = "work"
)

// Collection — подборка глав и работ разных авторов, собранная составителем.
type Collection struct {
	ID          int64  `json:"id" db:"id"`
	Title       string `json:"title" db:"title"`
	Slug        string `json:"slug" db:"slug"`
	Description string `json:"description" db:"description"`
	// Владелец обнуляется вместе с удалением аккаунта: подборка публична и
	// переживает своего составителя.
	OwnerID *int64 `json:"owner_id,omitempty" db:"owner_id"`
	// AuthorNickname — снимок подписи. Заполняется при создании: у читателя это
	// сегмент его адреса, у сотрудника пусто. Снимок, а не ссылка на
	// владельца, — чтобы адрес пережил удаление учётной записи.
	AuthorNickname string `json:"author_nickname" db:"author_nickname"`
	// PublishedAt nil — черновик, виден только владельцу. Обнуляется при
	// снятии с публикации, но отметка адреса (PublishIPHash) при этом не
	// стирается — по ней Get отличает черновик (никогда не публиковался,
	// 404) от снятого (был опубликован, 410).
	PublishedAt *time.Time `json:"published_at,omitempty" db:"published_at"`
	// PublishIPHash — отметка адреса последней публикации: рельс предела
	// частоты (3 в сутки) и, после снятия, единственный признак "было и
	// снято" в базе. json:"-" — служебное поле, наружу не отдаётся.
	PublishIPHash string    `json:"-" db:"publish_ip_hash"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
	// Items заполняется только на чтении одной подборки, в списке — nil.
	// omitempty снят быть не может: в списке он даёт "items": null для каждой
	// строки — тот самый сквозной дефект (список отдаёт null вместо []), которого
	// проект избегает. Из-за него же у пустой подборки (Items == []*CollectionEntry{})
	// ключ "items" в ответе отсутствует вовсе — это ожидаемый контракт, а не баг:
	// потребитель обязан читать отсутствие ключа как пустой состав.
	Items []*CollectionEntry `json:"items,omitempty" db:"-"`
}

// CollectionItem — строка состава как она лежит в базе. Для чтения наружу
// служит CollectionEntry: там заголовок уже разрешён, а номера печатные.
type CollectionItem struct {
	ID             int64     `json:"id" db:"id"`
	CollectionID   int64     `json:"collection_id" db:"collection_id"`
	Kind           string    `json:"kind" db:"kind"`
	ChapterID      *int64    `json:"chapter_id,omitempty" db:"chapter_id"`
	WorkID         *int64    `json:"work_id,omitempty" db:"work_id"`
	SnapshotTitle  string    `json:"snapshot_title" db:"snapshot_title"`
	SnapshotAuthor string    `json:"snapshot_author" db:"snapshot_author"`
	AuthorOverride string    `json:"author_override" db:"author_override"`
	OrderNumber    int       `json:"order_number" db:"order_number"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" db:"updated_at"`
}

// CollectionSource — откуда взят элемент. Номера страниц печатные.
type CollectionSource struct {
	WorkID int64 `json:"work_id"`
	// WorkSlug — хвост адреса тома-источника; см. SearchChapter.WorkSlug.
	// Пусто, если источник (работа) пропал.
	WorkSlug     string  `json:"work_slug,omitempty"`
	WorkTitle    string  `json:"work_title"`
	EditionTitle string  `json:"edition_title,omitempty"`
	VolumeNumber *int    `json:"volume_number,omitempty"`
	VolumePart   *string `json:"volume_part,omitempty"`
	PageStart    int     `json:"page_start"`
	PageEnd      int     `json:"page_end"`
}

// CollectionTocNode — узел поддерева глав в оглавлении: заголовок, печатный
// номер начала и адрес главы. Разворачивать подглаву читатель уходит в саму
// главу тома-источника, оттого id и отдаётся наружу.
type CollectionTocNode struct {
	// ChapterID — адрес главы в томе-источнике: по нему оглавление подборки
	// ведёт читателя в саму главу.
	ChapterID int64  `json:"chapter_id"`
	Title     string `json:"title"`
	// ChapterSlug — хвост адреса главы; см. Chapter.Slug. Пуст у главы-
	// нумератора, как и у самой главы.
	ChapterSlug string              `json:"chapter_slug,omitempty"`
	PageStart   int                 `json:"page_start"`
	Children    []CollectionTocNode `json:"children,omitempty"`
}

// CollectionEntry — строка оглавления, собранная на чтении.
type CollectionEntry struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`
	OrderNumber int    `json:"order_number"`
	// Title живой, из chapters/works; у битого элемента — снимок.
	Title string `json:"title"`
	// WorkAuthor и AuthorOverride отдаются наружу обе: форме состава нужна
	// подсказка из работы рядом с полем переопределения.
	WorkAuthor     string `json:"work_author"`
	AuthorOverride string `json:"author_override"`
	Author         string `json:"author"`
	// Broken — источник удалён: строка показывается снимком заголовка, а
	// чтение элемента отвечает 410.
	Broken   bool                `json:"broken"`
	Source   *CollectionSource   `json:"source,omitempty"`
	Children []CollectionTocNode `json:"children,omitempty"`
}

// ResolvedAuthor — автор строки оглавления: переопределение, если задано,
// иначе автор работы. Пустое переопределение означает «брать автора работы».
func (e *CollectionEntry) ResolvedAuthor() string {
	if e.AuthorOverride != "" {
		return e.AuthorOverride
	}

	return e.WorkAuthor
}

// PageSuggestionStatus — состояние предложения читателя. Строки русские, как
// у page_status: второй язык статусов в соседней таблице — тот шов, на котором
// потом пишут `if status == "accepted" || status == "принято"`.
type PageSuggestionStatus string

const (
	SuggestionNew      PageSuggestionStatus = "новое"
	SuggestionAccepted PageSuggestionStatus = "принято"
	SuggestionRejected PageSuggestionStatus = "отклонено"
)

// RejectReason — причина отказа. Список закрыт намеренно: ответить редактору
// читатель не может, и свободная причина превратила бы модерацию в переписку
// в один конец.
type RejectReason string

const (
	RejectAsInOriginal RejectReason = "так_в_оригинале"
	RejectAlreadyFixed RejectReason = "уже_исправлено"
	RejectOffTopic     RejectReason = "не_по_теме"
)

// ValidRejectReason сообщает, входит ли причина в закрытый список.
func ValidRejectReason(r RejectReason) bool {
	switch r {
	case RejectAsInOriginal, RejectAlreadyFixed, RejectOffTopic:
		return true
	}
	return false
}

// DocumentReviewStatus — судьба ЧЕРНОВИКА разбора относительно модерации.
// Публикация — отдельная ось (Document.PublishedAt): у опубликованного
// разбора черновик законно бывает «на_рассмотрении», и на людях при этом
// остаётся прежняя одобренная редакция.
type DocumentReviewStatus string

const (
	DocumentDraft    DocumentReviewStatus = "черновик"
	DocumentPending  DocumentReviewStatus = "на_рассмотрении"
	DocumentApproved DocumentReviewStatus = "одобрено"
	DocumentRejected DocumentReviewStatus = "отклонено"
)

// DocumentRejectReason — причина отказа в публикации разбора. Список закрыт
// по тому же доводу, что и RejectReason у предложений правок: ответить
// модератору автор не может, и свободная причина превратила бы модерацию в
// переписку в один конец. Список СВОЙ, а не общий с правками полос: «так в
// оригинале» разбору сказать нечего.
type DocumentRejectReason string

const (
	DocumentRejectOffTopic     DocumentRejectReason = "не_по_теме"
	DocumentRejectNoCommentary DocumentRejectReason = "текст_без_разбора"
	DocumentRejectAbuse        DocumentRejectReason = "брань_или_оскорбления"
	DocumentRejectUnlawful     DocumentRejectReason = "нарушает_закон"
)

// ValidDocumentRejectReason сообщает, входит ли причина в закрытый список.
func ValidDocumentRejectReason(r DocumentRejectReason) bool {
	switch r {
	case DocumentRejectOffTopic, DocumentRejectNoCommentary,
		DocumentRejectAbuse, DocumentRejectUnlawful:
		return true
	}
	return false
}

// PageSuggestion — предложение исправления полосы, поданное читателем.
//
// IPHash наружу не отдаётся никогда: отметка адреса нужна только пределу
// частоты, и в ответе API ей делать нечего.
type PageSuggestion struct {
	ID               int64  `json:"id" db:"id"`
	PageID           int64  `json:"page_id" db:"page_id"`
	BaseMarkdown     string `json:"base_markdown" db:"base_markdown"`
	ProposedMarkdown string `json:"proposed_markdown" db:"proposed_markdown"`
	Note             string `json:"note" db:"note"`
	// UserID — автор правки. ReaderKey остался исторической колонкой: выданные
	// билеты не переносим, новые строки пишут user_id.
	UserID       *int64               `json:"user_id,omitempty" db:"user_id"`
	IPHash       string               `json:"-" db:"ip_hash"`
	Status       PageSuggestionStatus `json:"status" db:"status"`
	RejectReason *RejectReason        `json:"reject_reason,omitempty" db:"reject_reason"`
	ModeratorID  *int64               `json:"moderator_id,omitempty" db:"moderator_id"`
	ResolvedAt   *time.Time           `json:"resolved_at,omitempty" db:"resolved_at"`
	CreatedAt    time.Time            `json:"created_at" db:"created_at"`
}

// Feedback — обращение: письмо читателя в читальню.
//
// IPHash наружу не отдаётся даже администратору: разбирать письма он может
// и без отметки адреса, а лишнее поле в ответе — лишний способ ею
// воспользоваться.
type Feedback struct {
	ID         int64      `json:"id" db:"id"`
	Message    string     `json:"message" db:"message"`
	SourcePath string     `json:"source_path" db:"source_path"`
	IPHash     string     `json:"-" db:"ip_hash"`
	HandledAt  *time.Time `json:"handled_at" db:"handled_at"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
}

// EditionHighlight — избранная работа собрания на главной: глава и её короткая
// подпись. Координата тома — от работы главы (у служебных передних листов —
// от родителя), слаги собраны тем же кодом, что в остальных ответах. Подпись
// пуста — клиент берёт первое предложение заглавия (chapterLabel), второй
// копии этого правила на Go нет.
type EditionHighlight struct {
	EditionID    int64   `json:"-"`
	ChapterID    int64   `json:"chapter_id"`
	ChapterSlug  string  `json:"chapter_slug"`
	ChapterTitle string  `json:"chapter_title"`
	WorkID       int64   `json:"work_id"`
	WorkSlug     string  `json:"work_slug"`
	VolumeNumber *int    `json:"volume_number"`
	VolumePart   *string `json:"volume_part"`
	Label        string  `json:"label"`
}

// HighlightInput — пункт тела PUT /editions/{id}/highlights. Порядок пунктов
// в массиве и есть порядок на главной.
type HighlightInput struct {
	ChapterID int64  `json:"chapter_id"`
	Label     string `json:"label"`
}
