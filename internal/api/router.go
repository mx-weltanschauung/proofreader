package api

import (
	"net/http"

	"github.com/gorilla/mux"

	"proofreader/internal/auth"
	"proofreader/internal/metrics"
	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/internal/seo"
)

// Router holds all API handlers and creates routes
type Router struct {
	// mcpHandler — MCP-сервер читальни (internal/mcp). nil — маршрут отвечает
	// 404: так собран роутер в тестах, которым сервер не нужен.
	mcpHandler http.Handler
	// opdsHandler — каталог OPDS (internal/opds). nil — /opds отвечает 404:
	// так собран роутер в тестах, которым каталог не нужен.
	opdsHandler http.Handler
	// statsHandler — посещаемость (internal/stats). nil — маячок отвечает 204
	// и ничего не пишет, серверные каналы молчат: так собран роутер в тестах.
	statsHandler *StatsHandler
	// metrics — здоровье; nil — без метрик (тесты).
	metrics               *metrics.Metrics
	authHandler           *AuthHandler
	workHandler           *WorkHandler
	pageHandler           *PageHandler
	chapterHandler        *ChapterHandler
	categoryHandler       *CategoryHandler
	documentHandler       *DocumentHandler
	documentCutHandler    *DocumentCutHandler
	documentReviewHandler *DocumentReviewHandler
	userHandler           *UserHandler
	exportHandler         *ExportHandler
	editionHandler        *EditionHandler
	shelfHandler          *ShelfHandler
	// nil — маршруты зарегистрированы, вызов паникует (тесты роутера ловят это
	// serveWithRecover), как у озвучки.
	highlightHandler      *HighlightHandler
	journalHandler        *JournalHandler
	personHandler         *PersonHandler
	indexHandler          *IndexHandler
	collectionHandler     *CollectionHandler
	downloadHandler       *DownloadHandler
	readingHandler        *ReadingHandler
	audioHandler          *AudioHandler
	versionHandler        *VersionHandler
	feedbackHandler       *FeedbackHandler
	pageSuggestionHandler *PageSuggestionHandler
	searchHandler         *SearchHandler
	seoHandler            *seo.Handler
	cacheHandler          *CacheHandler
	readerAdminHandler    *ReaderAdminHandler
	authService           *auth.Service
	// siteHandler — имя и контакты экземпляра (/api/site); nil — умолчание.
	siteHandler *SiteHandler
	// staticArchiveHandler — вся читальня одним архивом (/help#offline).
	staticArchiveHandler *StaticArchiveHandler
}

// WithMCP подключает MCP-сервер читальни на /mcp. Отдельным методом, а не
// параметром NewRouter: у конструктора два десятка позиционных параметров и
// полдюжины вызовов в тестах.
func (rt *Router) WithMCP(h http.Handler) *Router {
	rt.mcpHandler = h
	return rt
}

// WithOPDS подключает каталог OPDS на /opds. Отдельным методом по той же
// причине, что WithMCP.
func (rt *Router) WithOPDS(h http.Handler) *Router {
	rt.opdsHandler = h
	return rt
}

// WithStats подключает посещаемость: маячок /api/hit, серверные каналы и
// экран /admin/stats. Отдельным методом по той же причине, что WithMCP.
func (rt *Router) WithStats(h *StatsHandler) *Router {
	rt.statsHandler = h
	return rt
}

// WithMetrics подключает счёт запросов и задержек по маршрутам.
func (rt *Router) WithMetrics(m *metrics.Metrics) *Router {
	rt.metrics = m
	return rt
}

// NewRouter creates a new API router
func NewRouter(
	authHandler *AuthHandler,
	workHandler *WorkHandler,
	pageHandler *PageHandler,
	chapterHandler *ChapterHandler,
	categoryHandler *CategoryHandler,
	documentHandler *DocumentHandler,
	documentCutHandler *DocumentCutHandler,
	documentReviewHandler *DocumentReviewHandler,
	userHandler *UserHandler,
	exportHandler *ExportHandler,
	editionHandler *EditionHandler,
	shelfHandler *ShelfHandler,
	indexHandler *IndexHandler,
	collectionHandler *CollectionHandler,
	downloadHandler *DownloadHandler,
	readingHandler *ReadingHandler,
	versionHandler *VersionHandler,
	feedbackHandler *FeedbackHandler,
	pageSuggestionHandler *PageSuggestionHandler,
	searchHandler *SearchHandler,
	seoHandler *seo.Handler,
	cacheHandler *CacheHandler,
	readerAdminHandler *ReaderAdminHandler,
	authService *auth.Service,
) *Router {
	return &Router{
		authHandler:           authHandler,
		workHandler:           workHandler,
		pageHandler:           pageHandler,
		chapterHandler:        chapterHandler,
		categoryHandler:       categoryHandler,
		documentHandler:       documentHandler,
		documentCutHandler:    documentCutHandler,
		documentReviewHandler: documentReviewHandler,
		userHandler:           userHandler,
		exportHandler:         exportHandler,
		editionHandler:        editionHandler,
		shelfHandler:          shelfHandler,
		indexHandler:          indexHandler,
		collectionHandler:     collectionHandler,
		downloadHandler:       downloadHandler,
		readingHandler:        readingHandler,
		versionHandler:        versionHandler,
		feedbackHandler:       feedbackHandler,
		pageSuggestionHandler: pageSuggestionHandler,
		searchHandler:         searchHandler,
		seoHandler:            seoHandler,
		cacheHandler:          cacheHandler,
		readerAdminHandler:    readerAdminHandler,
		authService:           authService,
	}
}

// WithAudio подключает озвучку. nil допустим: маршруты регистрируются всегда
// (сторож ролей обязан их видеть), обработчик на nil паникует, и тесты
// роутера ловят панику через serveWithRecover.
func (rt *Router) WithAudio(h *AudioHandler) *Router {
	rt.audioHandler = h
	return rt
}

// WithSite подключает сведения об экземпляре (/api/site). nil допустим:
// маршрут отдаёт умолчание.
func (rt *Router) WithSite(h *SiteHandler) *Router {
	rt.siteHandler = h
	return rt
}

// WithStaticArchive подключает сведения об архиве статической читальни
// (/api/static-archive). nil допустим, как у WithAudio: маршруты регистрируются
// всегда, чтобы их видел сторож ролей.
func (rt *Router) WithStaticArchive(h *StaticArchiveHandler) *Router {
	rt.staticArchiveHandler = h
	return rt
}

// WithJournals подключает журналы, номера и людей. nil допустим, как у
// WithAudio: маршруты регистрируются всегда, чтобы их видел сторож ролей.
func (rt *Router) WithJournals(j *JournalHandler, p *PersonHandler) *Router {
	rt.journalHandler, rt.personHandler = j, p
	return rt
}

// WithHighlights подключает избранное собраний. Отдельным методом по той же
// причине, что WithMCP: у NewRouter два десятка позиционных параметров.
func (rt *Router) WithHighlights(h *HighlightHandler) *Router {
	rt.highlightHandler = h
	return rt
}

// Setup creates and configures the HTTP router
func (rt *Router) Setup() *mux.Router {
	r := mux.NewRouter()

	// Apply global middleware
	r.Use(middleware.CORS)
	r.Use(middleware.Logging)
	if rt.metrics != nil {
		r.Use(rt.metrics.Middleware)
	}
	// Серверные каналы посещаемости: смотрят на код ответа после обработчика.
	r.Use(rt.statsHandler.Channels)
	// Gzip идёт последним, то есть ближе всех к обработчику: журнал должен
	// видеть настоящий код ответа, а не отложенный сжатием.
	r.Use(middleware.Gzip)

	// Страницы для краулеров и всё, что к ним прилагается. Вне /api
	// намеренно: пути повторяют адреса читальни, а не API, и nginx приводит
	// сюда запрос краулера как /seo<путь> (см. frontend/nginx.conf).
	//
	// Живой читатель здесь не появляется — его запрос nginx отдаёт SPA.
	//
	// HEAD наравне с GET: net/http сам отбрасывает тело ответа, но метод
	// обязан быть объявлен явно, иначе mux отвечает 405 — а часть загрузчиков
	// превью и мониторингов пробует HEAD прежде GET.
	r.PathPrefix("/seo").HandlerFunc(rt.seoHandler.Page).Methods("GET", "HEAD")
	r.HandleFunc("/robots.txt", rt.seoHandler.Robots).Methods("GET", "HEAD")
	r.HandleFunc("/sitemap.xml", rt.seoHandler.SitemapIndex).Methods("GET", "HEAD")
	r.PathPrefix("/sitemap-").HandlerFunc(rt.seoHandler.Sitemap).Methods("GET", "HEAD")
	r.PathPrefix("/og/").HandlerFunc(rt.seoHandler.OGCard).Methods("GET", "HEAD")

	// MCP-сервер читальни: вопросы нейросети по всей читальне
	// (docs/superpowers/specs/2026-09-29-corpus-mcp-design.md). Публичный, без
	// токена: коннектор Claude ключа в адресе не передаёт, а OAuth не заведён.
	// GET отвечает 405 с адресом справки — решает сам обработчик.
	mcpHandler := rt.mcpHandler
	if mcpHandler == nil {
		mcpHandler = http.NotFoundHandler()
	}
	r.Handle("/mcp", mcpHandler).Methods("GET", "HEAD", "POST", "DELETE")

	// Каталог OPDS для читалок и агрегаторов
	// (docs/superpowers/specs/2026-10-03-opds-catalog-design.md). Публичный,
	// вне /api, как /llms.txt: адрес вводится в читалку руками и должен быть
	// коротким. Разбор пути под /opds — в самом обработчике (opds.ParsePath).
	opdsHandler := rt.opdsHandler
	if opdsHandler == nil {
		opdsHandler = http.NotFoundHandler()
	}
	r.PathPrefix("/opds").Handler(opdsHandler).Methods("GET", "HEAD")

	// API prefix
	api := r.PathPrefix("/api").Subrouter()

	// Public routes
	api.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}).Methods("GET")
	api.HandleFunc("/version", rt.versionHandler.Version).Methods("GET")

	// Auth routes (public)
	authRoutes := api.PathPrefix("/auth").Subrouter()
	authRoutes.HandleFunc("/login", rt.authHandler.Login).Methods("POST")
	// Дверь читателя — вход, равный регистрации. Отдельная от /login
	// намеренно, см. докблок ReaderLogin.
	authRoutes.HandleFunc("/reader", rt.authHandler.ReaderLogin).Methods("POST")

	// reader — всё, что читатель оставляет под своим именем. Сотрудник умеет
	// всё, что читатель, иначе владелец не сможет проверить собственную
	// читальню.
	//
	// Роль проверяется, а не факт входа: тем же JWT_SECRET, которым подписан
	// токен администратора, подписан теперь и токен постороннего.
	//
	// Объявлен ДО public и staff намеренно: mux перебирает подроутеры в
	// порядке их создания, и общий шаблон подхватил бы литеральный путь
	// раньше reader, окажись он раньше в этом списке. Так уже устроено ниже
	// для "/suggestions/{id}" (staff) против "/suggestions/mine" (reader);
	// по той же причине reader стоит и перед public — иначе публичный общий
	// "/collections/{slug}" разобрал бы "/collections/mine" как слаг
	// подборки, и запрос до reader.HandleFunc("/collections/mine", ...) не
	// дошёл бы вовсе (см. TestCollectionsMineRouteNotSwallowedByGenericSlug).
	reader := api.PathPrefix("").Subrouter()
	reader.Use(middleware.AuthMiddleware(rt.authService))
	reader.Use(middleware.RequireRole(
		models.RoleReader, models.RoleEditor, models.RoleAdministrator))

	// staffEarly — сотруднические маршруты с ЛИТЕРАЛЬНЫМ путём, который иначе
	// перехватил бы публичный шаблон с переменной. mux перебирает подроутеры
	// в порядке их создания: "/documents/review", объявленный на staff (он
	// создаётся ПОСЛЕ public), достался бы публичному "/documents/{slug}" и
	// искал бы разбор со слагом "review" — то есть в 404 вместо очереди. Тот
	// же приём, которым reader объявлен раньше public ради "/collections/mine".
	staffEarly := api.PathPrefix("").Subrouter()
	staffEarly.Use(middleware.AuthMiddleware(rt.authService))
	staffEarly.Use(middleware.RequireRole(models.RoleEditor, models.RoleAdministrator))

	// Public read routes: no auth required, but attach claims if a token is
	// present (so handlers can vary behavior for logged-in users later).
	public := api.PathPrefix("").Subrouter()
	public.Use(middleware.OptionalAuth(rt.authService))

	// Authenticated routes: any valid token, any role.
	authRequired := api.PathPrefix("").Subrouter()
	authRequired.Use(middleware.AuthMiddleware(rt.authService))

	// Staff-only mutations: valid token AND editor/admin role.
	staff := api.PathPrefix("").Subrouter()
	staff.Use(middleware.AuthMiddleware(rt.authService))
	staff.Use(middleware.RequireRole(models.RoleEditor, models.RoleAdministrator))

	// /auth/me — продлить и посмотреть собственную сессию вправе всякий
	// вошедший, а не только сотрудник: это действие над своей же личностью, а
	// не доступ к чужим данным. Единственный маршрут, которому годится
	// authRequired без RequireRole вообще — до читательской двери "любой
	// вошедший" совпадало с "сотрудник" случайно, потому что учётку заводил
	// только администратор; здесь это совпадение не имело значения, и с его
	// исчезновением здесь ничего не меняется. Другим непубличным маршрутам
	// это обоснование не переносится: см. историю правок полосы ниже — там то
	// же самое рассуждение ошибочно, потому что доступ идёт к чужим данным.
	authRequired.HandleFunc("/auth/me", rt.authHandler.Me).Methods("GET")

	// Уход читателя. На reader, а не authRequired: это действие над своей
	// личностью, но доступное ТОЛЬКО читателю (см. AuthHandler.DeleteMe) —
	// сотрудник, дошедший до обработчика подроутером reader (он пускает и
	// staff — см. её комментарий выше), получает честный 403 внутри самого
	// DeleteMe, а не молчаливый допуск.
	reader.HandleFunc("/auth/me", rt.authHandler.DeleteMe).Methods("DELETE")

	// Category routes (public reads)
	public.HandleFunc("/categories", rt.categoryHandler.List).Methods("GET")
	public.HandleFunc("/categories/{id}", rt.categoryHandler.Get).Methods("GET")

	// Category management (Editor and Admin only)
	staff.HandleFunc("/categories", rt.categoryHandler.Create).Methods("POST")
	staff.HandleFunc("/categories/{id}", rt.categoryHandler.Update).Methods("PUT")
	staff.HandleFunc("/categories/{id}", rt.categoryHandler.Delete).Methods("DELETE")

	// Edition routes (public reads)
	public.HandleFunc("/editions", rt.editionHandler.List).Methods("GET")
	public.HandleFunc("/editions/{id}", rt.editionHandler.Get).Methods("GET")
	public.HandleFunc("/editions/{id}/works", rt.editionHandler.ListWorks).Methods("GET")
	public.HandleFunc("/editions/{id}/highlights", rt.highlightHandler.Get).Methods("GET")

	// Всё, что рисует главная, одним ответом: собрания со своими томами и
	// работы вне собраний. Полки эти же данные собирали шестью запросами в
	// две волны — см. ShelfHandler.
	public.HandleFunc("/shelf", rt.shelfHandler.Get).Methods("GET")
	public.HandleFunc("/journals", rt.journalHandler.List).Methods("GET")
	public.HandleFunc("/journals/{slug}", rt.journalHandler.Get).Methods("GET")
	staff.HandleFunc("/journals", rt.journalHandler.Create).Methods("POST")
	staff.HandleFunc("/journals/{id}", rt.journalHandler.Update).Methods("PUT")
	staff.HandleFunc("/journals/{id}/issues", rt.journalHandler.CreateIssue).Methods("POST")
	staff.HandleFunc("/journal-issues/{id}", rt.journalHandler.UpdateIssue).Methods("PUT")
	public.HandleFunc("/persons", rt.personHandler.Search).Methods("GET")
	public.HandleFunc("/persons/{slug}", rt.personHandler.Get).Methods("GET")
	staff.HandleFunc("/persons", rt.personHandler.Create).Methods("POST")
	staff.HandleFunc("/persons/{id}", rt.personHandler.Update).Methods("PUT")
	staff.HandleFunc("/persons/{id}/merge", rt.personHandler.Merge).Methods("POST")
	staff.HandleFunc("/works/{workId}/chapters/{id}/credits", rt.personHandler.ReplaceCredits).Methods("PUT")

	// Вся читальня одним архивом (scripts/static-publish.sh): сведения для
	// справки и переход на файл в бакете. Публичны, как всё чтение.
	public.HandleFunc("/site", rt.siteHandler.Get).Methods("GET")
	public.HandleFunc("/static-archive", rt.staticArchiveHandler.Get).Methods("GET")
	public.HandleFunc("/static-archive/download", rt.staticArchiveHandler.Download).Methods("GET")

	// Маячок посещаемости SPA — public, а не голый api: OptionalAuth нужен,
	// чтобы не считать сотрудников.
	public.HandleFunc("/hit", rt.statsHandler.Hit).Methods("POST")

	// Полнотекстовый поиск: каталог и тома одним ответом, полосы тома —
	// вторым. Публичен, как все чтения; таймаут и потолки — в обработчике.
	public.HandleFunc("/search", rt.searchHandler.Search).Methods("GET")
	public.HandleFunc("/search/pages", rt.searchHandler.Pages).Methods("GET")

	// Edition management (Editor and Admin only)
	staff.HandleFunc("/editions", rt.editionHandler.Create).Methods("POST")
	staff.HandleFunc("/editions/{id}", rt.editionHandler.Update).Methods("PUT")
	staff.HandleFunc("/editions/{id}", rt.editionHandler.Delete).Methods("DELETE")
	staff.HandleFunc("/editions/{id}/highlights", rt.highlightHandler.Replace).Methods("PUT")

	// Work routes (public reads)
	public.HandleFunc("/works", rt.workHandler.List).Methods("GET")
	public.HandleFunc("/works/{id}", rt.workHandler.Get).Methods("GET")
	public.HandleFunc("/works/{id}/notes", rt.workHandler.ListNotes).Methods("GET")

	// Скачивание тома файлом. Публично: тот же текст уже отдаётся без токена
	// постранично, закрывать файл при открытом тексте не от чего.
	public.HandleFunc("/works/{id}/download", rt.downloadHandler.Work).Methods("GET")

	// Work management (Editor and Admin only)
	staff.HandleFunc("/works", rt.workHandler.Create).Methods("POST")
	staff.HandleFunc("/works/{id}", rt.workHandler.Update).Methods("PUT")
	staff.HandleFunc("/works/{id}", rt.workHandler.Delete).Methods("DELETE")
	staff.HandleFunc("/works/{id}/upload", rt.workHandler.UploadFile).Methods("POST")
	// Снятие аппарата тома: главы is_apparatus с их полосами, предметный
	// указатель и служебные передние листы. Необратимо; GET того же адреса
	// отдаёт план, ничего не трогая. План не публичен — он перечисляет
	// поимённо то, что собираются снять, и читателю не адресован.
	staff.HandleFunc("/works/{id}/apparatus", rt.workHandler.ApparatusPlan).Methods("GET")
	staff.HandleFunc("/works/{id}/apparatus", rt.workHandler.DeleteApparatus).Methods("DELETE")

	staff.HandleFunc("/works/{id}/create-pages", rt.workHandler.CreatePages).Methods("POST")
	staff.HandleFunc("/works/{id}/categories", rt.workHandler.AddCategory).Methods("POST")
	staff.HandleFunc("/works/{id}/categories/{categoryId}", rt.workHandler.RemoveCategory).Methods("DELETE")

	// Page routes (public reads)
	public.HandleFunc("/works/{workId}/pages", rt.pageHandler.List).Methods("GET")
	public.HandleFunc("/works/{workId}/pages/{pageId}", rt.pageHandler.Get).Methods("GET")
	public.HandleFunc("/works/{workId}/pages/{pageId}/render", rt.pageHandler.Render).Methods("GET")

	// Полоса, разрезанная на блоки: поверхность выбора для подборщика
	// вклейки. Публично — тот же текст уже отдаётся целиком по /pages/{pageId}.
	public.HandleFunc("/works/{workId}/pages/{pageId}/blocks", rt.pageHandler.Blocks).Methods("GET")

	// Resolve a page by its number within the work. No collision with the two
	// routes above: /pages/{pageId} is one segment shorter, and
	// /pages/{pageId}/render requires the literal "render" last.
	public.HandleFunc("/works/{workId}/pages/by-number/{pageNumber}", rt.pageHandler.GetByNumber).Methods("GET")

	// Карта страниц тома: номер и статус, без текста и без подписанных URL.
	// Путь `page-map`, а не `pages/summary`: `/pages/{pageId}` объявлен выше и
	// поймал бы `summary` как идентификатор страницы.
	public.HandleFunc("/works/{workId}/page-map", rt.pageHandler.PageMap).Methods("GET")

	// История правок полосы (Editor и Admin only) — не authRequired.
	// page_versions хранит полный прежний текст, включая то, что убрано
	// точечной правкой (в том числе по жалобе): пока учётку заводил только
	// администратор, "любой вошедший" и "сотрудник" были одним множеством, и
	// маршрут можно было держать под authRequired без разницы. С появлением
	// читательской двери (POST /auth/reader, заводит учётку за секунду кому
	// угодно) это перестало быть верно — редакционная история не должна
	// расширяться в общий доступ как побочный эффект читательских аккаунтов.
	staff.HandleFunc("/works/{workId}/pages/{pageId}/versions", rt.pageHandler.ListVersions).Methods("GET")
	staff.HandleFunc("/works/{workId}/pages/{pageId}/versions/{versionId}", rt.pageHandler.GetVersion).Methods("GET")

	// Page editing (Editor and Admin only - closes the "any logged-in user can edit" hole)
	staff.HandleFunc("/works/{workId}/pages/{pageId}", rt.pageHandler.Update).Methods("PUT")

	// Page version restore (Editor and Admin only)
	staff.HandleFunc("/works/{workId}/pages/{pageId}/versions/{versionId}/restore", rt.pageHandler.RestoreVersion).Methods("POST")

	// Предложение исправления от читателя. Под входом: маршрут стоит на
	// reader, а не на public — правку подаёт учётная запись, а не
	// предъявительский билет.
	reader.HandleFunc("/works/{workId}/pages/{pageId}/suggestions",
		rt.pageSuggestionHandler.Create).Methods("POST")

	// Свои правки — по учётной записи, а не по ключу читателя. Отдельный
	// путь, а не тот же /suggestions с другим смыслом: reader и staff —
	// разные подроутеры на одном префиксе, и два GET /api/suggestions
	// разошлись бы по порядку строк в файле, а не по правам.
	reader.HandleFunc("/suggestions/mine", rt.pageSuggestionHandler.Mine).Methods("GET")

	// Очередь предложений разбирает редактор — это работа с текстом полосы,
	// та же, что он делает через PUT /pages/{id}. Поэтому staff, а не admin.
	//
	// От захвата "/suggestions/mine" ниже стоящим "/suggestions/{id}" защищает
	// не порядок этих двух строк (переставь их — ничего не изменится), а то,
	// что подроутер reader создан раньше staff (см. объявления выше): mux
	// перебирает маршруты в порядке создания подроутеров и уже находит "mine"
	// в reader раньше, чем добирается до staff.
	staff.HandleFunc("/suggestions", rt.pageSuggestionHandler.List).Methods("GET")
	staff.HandleFunc("/suggestions/{id}", rt.pageSuggestionHandler.Get).Methods("GET")
	staff.HandleFunc("/suggestions/{id}/accept", rt.pageSuggestionHandler.Accept).Methods("POST")
	staff.HandleFunc("/suggestions/{id}/reject", rt.pageSuggestionHandler.Reject).Methods("POST")

	// Массовая чистка стоит ВЫШЕ удаления одного: оба маршрута в одном
	// подроутере, и здесь порядок строк — единственное, что их разводит
	// (в отличие от пары mine/{id} выше, где разводит порядок подроутеров).
	// Переставь их — и "rejected" уедет в ParseInt, то есть в 400.
	// Закрыто тестом TestPurgeRouteBeatsSuggestionIDRoute.
	staff.HandleFunc("/suggestions/rejected", rt.pageSuggestionHandler.DeleteRejected).Methods("DELETE")
	staff.HandleFunc("/suggestions/{id}", rt.pageSuggestionHandler.Delete).Methods("DELETE")

	// Index concept routes (public reads)
	public.HandleFunc("/concepts", rt.indexHandler.ListConcepts).Methods("GET")
	public.HandleFunc("/concepts/{slug}", rt.indexHandler.GetConcept).Methods("GET")
	// Тексты страниц, на которые указывает понятие. Двухсегментный путь не
	// перехватывает односегментный /concepts/{slug} выше.
	public.HandleFunc("/concepts/{slug}/fragments", rt.indexHandler.Fragments).Methods("GET")
	// Разворот одной страницы адреса, разрезанной по границам его вырезок.
	public.HandleFunc("/concepts/{slug}/references/{refId}/pages/{pageId}", rt.indexHandler.ExpandPage).Methods("GET")
	public.HandleFunc("/works/{workId}/pages/{pageId}/concepts", rt.indexHandler.PageConcepts).Methods("GET")

	// Subject-index import (Editor and Admin only)
	staff.HandleFunc("/editions/{id}/index/import", rt.indexHandler.Import).Methods("POST")

	// Границы вырезок правит редактор — из UI или скиллом-нарезчиком.
	staff.HandleFunc("/concepts/{slug}/references/{refId}/cuts", rt.indexHandler.PutCuts).Methods("PUT")

	// Chapter routes (public reads)
	public.HandleFunc("/works/{workId}/chapters", rt.chapterHandler.List).Methods("GET")
	public.HandleFunc("/works/{workId}/chapters/{id}", rt.chapterHandler.Get).Methods("GET")
	public.HandleFunc("/works/{workId}/chapters/{id}/pages", rt.chapterHandler.ListPages).Methods("GET")

	// Скачивание главы файлом. Публично по той же причине, что и у тома выше.
	public.HandleFunc("/works/{workId}/chapters/{id}/download", rt.downloadHandler.Chapter).Methods("GET")

	// Chapter management (Editor and Admin only)
	staff.HandleFunc("/works/{workId}/chapters", rt.chapterHandler.Create).Methods("POST")
	staff.HandleFunc("/works/{workId}/chapters/{id}", rt.chapterHandler.Update).Methods("PUT")
	staff.HandleFunc("/works/{workId}/chapters/{id}", rt.chapterHandler.Delete).Methods("DELETE")
	staff.HandleFunc("/works/{workId}/chapters/{id}/move", rt.chapterHandler.Move).Methods("PATCH")

	// Сброс кэша одной главы — редактору: устаревший текст после правки
	// бьёт именно по нему, а стоит сброс один файл и один рендер.
	staff.HandleFunc("/works/{workId}/chapters/{id}/cache", rt.cacheHandler.PurgeChapter).Methods("DELETE")

	// Окно страниц для потокового чтения: адресует работу, а не главу —
	// поток идёт сквозь том и границу главы не замечает.
	public.HandleFunc("/works/{workId}/reading", rt.readingHandler.Window).Methods("GET")

	// Озвучка (спека аудиокниг 29.09, шаг 2). Переменная под /audio/ —
	// только числом: буквальные queue/claim/rec того же уровня не должны
	// читаться как {id}, и .opus не должен съедать слово queue.
	public.HandleFunc("/works/{id}/audio", rt.audioHandler.WorkAudio).Methods("GET")
	// Редиректы звука отвечают и HEAD: curl -I и часть проигрывателей
	// спрашивают адрес им раньше GET и на 405 считают его мёртвым.
	public.HandleFunc("/audio/{id:[0-9]+}.opus", rt.audioHandler.TrackRedirect).Methods("GET", "HEAD")
	public.HandleFunc("/audio/rec/{id:[0-9]+}", rt.audioHandler.RecordingRedirect).Methods("GET", "HEAD")

	staff.HandleFunc("/works/{id}/audio/queue", rt.audioHandler.Enqueue).Methods("POST")
	staff.HandleFunc("/works/{id}/audio/uploads", rt.audioHandler.UploadURL).Methods("POST")
	staff.HandleFunc("/works/{id}/audio/tracks", rt.audioHandler.RegisterTracks).Methods("POST")
	staff.HandleFunc("/works/{id}/audio/objects", rt.audioHandler.DeleteObjects).Methods("DELETE")
	staff.HandleFunc("/works/{id}/audio/requeue-stale", rt.audioHandler.RequeueStale).Methods("POST")
	staff.HandleFunc("/works/{workId}/chapters/{id}/recordings/uploads", rt.audioHandler.RecordingUploadURL).Methods("POST")
	staff.HandleFunc("/works/{workId}/chapters/{id}/recordings", rt.audioHandler.RegisterRecording).Methods("POST")
	staff.HandleFunc("/audio/rec/{id:[0-9]+}", rt.audioHandler.UpdateRecording).Methods("PATCH")
	staff.HandleFunc("/audio/rec/{id:[0-9]+}", rt.audioHandler.DeleteRecording).Methods("DELETE")
	staff.HandleFunc("/audio/queue", rt.audioHandler.Queue).Methods("GET")
	staff.HandleFunc("/audio/queue/claim", rt.audioHandler.Claim).Methods("POST")
	staff.HandleFunc("/audio/queue/{id:[0-9]+}", rt.audioHandler.CancelQueue).Methods("DELETE")
	staff.HandleFunc("/audio/queue/{id:[0-9]+}/retry", rt.audioHandler.RetryQueue).Methods("POST")
	staff.HandleFunc("/audio/queue/{id:[0-9]+}/result", rt.audioHandler.Result).Methods("POST")

	// Обращения: приём письма от читателя (публично)
	public.HandleFunc("/feedback", rt.feedbackHandler.Create).Methods("POST")

	// Document routes (public reads — витрина и одобренная редакция).
	//
	// Два вида адреса разбора, как у подборок: короткий /documents/{слаг} —
	// сотруднический (пустая подпись), длинный /documents/{ник}/{слаг} —
	// читательский. Различает их не число сегментов само по себе, а пара
	// UNIQUE (author_nickname, slug): короткий адрес ищет строку с пустым
	// ником.
	//
	// Порядок обязателен. "/documents/{slug}/view" и "/documents/{nickname}/{slug}"
	// — оба трёхсегментные, и объявленный первым забирает совпадение. Первым
	// стоит короткий с литеральным хвостом: иначе /documents/chitatel/view
	// уехало бы в длинный адрес и искало разбор со слагом «view». Обратная
	// цена названа и закрыта списком reservedDocumentSlugs (document_slug.go):
	// читательский разбор со слагом «view» недостижим, поэтому такой слаг не
	// выдаётся вовсе. Тот же приём и тот же порядок у подборок ниже.
	public.HandleFunc("/documents", rt.documentHandler.List).Methods("GET")
	public.HandleFunc("/documents/{slug}/view", rt.documentHandler.View).Methods("GET")
	public.HandleFunc("/documents/{slug}/cuts/{cutId}", rt.documentCutHandler.Full).Methods("GET")
	public.HandleFunc("/documents/{nickname}/{slug}/view", rt.documentHandler.View).Methods("GET")
	public.HandleFunc("/documents/{nickname}/{slug}/cuts/{cutId}", rt.documentCutHandler.Full).Methods("GET")
	public.HandleFunc("/documents/{slug}", rt.documentHandler.Get).Methods("GET")
	public.HandleFunc("/documents/{nickname}/{slug}", rt.documentHandler.Get).Methods("GET")

	// Своё у читателя: разбор пишет и правит автор — читатель наравне с
	// сотрудником. Кто именно вправе править сам разбор, решает
	// mayEditDocument внутри DocumentHandler (различие идёт по нику-снимку, а
	// не по роли), поэтому подроутер — reader, а не staff.
	//
	// Тот же строй адреса-пары, что и у публичных маршрутов выше: короткий
	// /documents/{слаг} — сотруднический, длинный /documents/{ник}/{слаг} —
	// читательский. Настоящей коллизии сегментов здесь нет — у каждой пары
	// PUT/DELETE ниже короткий и длинный вид разной длины ("{slug}" — два
	// сегмента, "{nickname}/{slug}" — три), но короткий объявлен первым тем
	// же строем, что и везде в этом файле.
	reader.HandleFunc("/documents", rt.documentHandler.Create).Methods("POST")
	reader.HandleFunc("/documents/{slug}", rt.documentHandler.Update).Methods("PUT")
	reader.HandleFunc("/documents/{nickname}/{slug}", rt.documentHandler.Update).Methods("PUT")
	reader.HandleFunc("/documents/{slug}", rt.documentHandler.Delete).Methods("DELETE")
	reader.HandleFunc("/documents/{nickname}/{slug}", rt.documentHandler.Delete).Methods("DELETE")

	// Заведение и снятие вклейки — на том же подроутере reader, что и правка
	// самого разбора, тем же доводом «своё у читателя». Авторство при этом
	// проверяется: DocumentCutHandler.Create/Delete читают разбор
	// (documentKey + h.documents.GetByAuthorSlug) и пропускают дальше только
	// через mayEditDocument — вклейку в ЧУЖОЙ разбор не заводит и не снимает
	// никто, включая редактора («мы размещаем, автор собирает»). Подроутер
	// reader тут отвечает за «нужен вход», а не за «можно всякому
	// вошедшему»: решение о праве принимает обработчик, потому что различие
	// идёт по нику-снимку, а не по роли.
	reader.HandleFunc("/documents/{slug}/cuts", rt.documentCutHandler.Create).Methods("POST")
	reader.HandleFunc("/documents/{nickname}/{slug}/cuts", rt.documentCutHandler.Create).Methods("POST")
	reader.HandleFunc("/documents/{slug}/cuts/{cutId}", rt.documentCutHandler.Delete).Methods("DELETE")
	reader.HandleFunc("/documents/{nickname}/{slug}/cuts/{cutId}", rt.documentCutHandler.Delete).Methods("DELETE")

	// «Моё» автора: черновики и судьба поданного. Личные данные, поэтому
	// reader. От захвата публичным "/documents/{slug}" защищает не порядок
	// этих строк, а то, что подроутер reader объявлен раньше public — тем же
	// механизмом, что и "/collections/mine".
	reader.HandleFunc("/documents/mine", rt.documentReviewHandler.Mine).Methods("GET")
	reader.HandleFunc("/documents/{slug}/submit", rt.documentReviewHandler.Submit).Methods("POST")
	reader.HandleFunc("/documents/{nickname}/{slug}/submit", rt.documentReviewHandler.Submit).Methods("POST")
	reader.HandleFunc("/documents/{slug}/unpublish", rt.documentReviewHandler.Unpublish).Methods("POST")
	reader.HandleFunc("/documents/{nickname}/{slug}/unpublish", rt.documentReviewHandler.Unpublish).Methods("POST")

	// Очередь модерации — сотруднику, на staffEarly: литеральный "review"
	// иначе уехал бы в публичный "/documents/{slug}" (см. объявление
	// staffEarly выше).
	staffEarly.HandleFunc("/documents/review", rt.documentReviewHandler.Queue).Methods("GET")
	// approve/reject на обычном staff, а не staffEarly: их путь
	// "/documents/{slug}/approve" на сегмент длиннее "/documents/{slug}" и ни
	// при каком порядке подроутеров с ним не совпадёт — задача захвата стоит
	// только перед "review" и "mine", у которых сегментов ровно столько же.
	staff.HandleFunc("/documents/{slug}/approve", rt.documentReviewHandler.Approve).Methods("POST")
	staff.HandleFunc("/documents/{nickname}/{slug}/approve", rt.documentReviewHandler.Approve).Methods("POST")
	staff.HandleFunc("/documents/{slug}/reject", rt.documentReviewHandler.Reject).Methods("POST")
	staff.HandleFunc("/documents/{nickname}/{slug}/reject", rt.documentReviewHandler.Reject).Methods("POST")

	// /collections/mine — экран «моё» читателя. Личные данные, включая
	// черновики, поэтому висит на reader (AuthMiddleware + RequireRole), а не
	// на public — как /suggestions/mine у предложений правок. Ручная проверка
	// claims внутри CollectionHandler.Mine оставлена страховкой на случай,
	// если маршрут когда-нибудь переедет. От захвата общим
	// "/collections/{slug}" (ниже, на public) защищает не порядок этих двух
	// строк, а то, что подроутер reader объявлен раньше public (см. комментарий
	// у объявления reader выше).
	reader.HandleFunc("/collections/mine", rt.collectionHandler.Mine).Methods("GET")

	// Collection routes (public reads)
	public.HandleFunc("/collections", rt.collectionHandler.List).Methods("GET")
	public.HandleFunc("/collections/{slug}", rt.collectionHandler.Get).Methods("GET")
	// Страницы элемента: та же форма ответа, что у страниц главы.
	public.HandleFunc("/collections/{slug}/items/{itemId}/pages", rt.collectionHandler.ItemPages).Methods("GET")
	// Скачивание подборки файлом. Публично по той же причине, что и у тома выше.
	public.HandleFunc("/collections/{slug}/download", rt.downloadHandler.Collection).Methods("GET")

	// Второй, более длинный вид адреса подборки — /collections/{ник}/{слаг},
	// для читательских подборок (у сотруднических ник пуст, и они остаются на
	// коротком адресе выше). Конкретные маршруты (items/{itemId}/pages,
	// download) объявлены ПЕРЕД общим /{nickname}/{slug}: тот же метод GET и
	// то же число сегментов, что у .../download, поэтому общий вид, будь он
	// первым, перехватил бы /collections/чтец/download как подборку «чтец» со
	// слагом «download» и до обработчика скачивания запрос не дошёл бы вовсе.
	public.HandleFunc("/collections/{nickname}/{slug}/items/{itemId}/pages", rt.collectionHandler.ItemPages).Methods("GET")
	public.HandleFunc("/collections/{nickname}/{slug}/download", rt.downloadHandler.Collection).Methods("GET")
	public.HandleFunc("/collections/{nickname}/{slug}", rt.collectionHandler.Get).Methods("GET")

	// Collection management (владелец — читатель или сотрудник, см. mayEdit
	// в collection_handler.go; администратору сверх этого доступно удаление
	// чужой подборки — рычаг снятия, а не правки).
	reader.HandleFunc("/collections", rt.collectionHandler.Create).Methods("POST")
	reader.HandleFunc("/collections/{slug}", rt.collectionHandler.Update).Methods("PUT")
	reader.HandleFunc("/collections/{slug}", rt.collectionHandler.Delete).Methods("DELETE")
	reader.HandleFunc("/collections/{slug}/items", rt.collectionHandler.AddItem).Methods("POST")
	reader.HandleFunc("/collections/{slug}/items/{itemId}", rt.collectionHandler.UpdateItem).Methods("PUT")
	reader.HandleFunc("/collections/{slug}/items/{itemId}", rt.collectionHandler.DeleteItem).Methods("DELETE")
	reader.HandleFunc("/collections/{slug}/items/{itemId}/move", rt.collectionHandler.MoveItem).Methods("PATCH")
	reader.HandleFunc("/collections/{slug}/publish", rt.collectionHandler.Publish).Methods("POST")
	reader.HandleFunc("/collections/{slug}/unpublish", rt.collectionHandler.Unpublish).Methods("POST")

	// Те же двойники для читательского вида адреса. Настоящих коллизий по
	// сегментам здесь нет (метод у каждого маршрута свой), но специфичные
	// объявлены раньше общего PUT/DELETE — тем же порядком, что и выше.
	reader.HandleFunc("/collections/{nickname}/{slug}/items", rt.collectionHandler.AddItem).Methods("POST")
	reader.HandleFunc("/collections/{nickname}/{slug}/items/{itemId}", rt.collectionHandler.UpdateItem).Methods("PUT")
	reader.HandleFunc("/collections/{nickname}/{slug}/items/{itemId}", rt.collectionHandler.DeleteItem).Methods("DELETE")
	reader.HandleFunc("/collections/{nickname}/{slug}/items/{itemId}/move", rt.collectionHandler.MoveItem).Methods("PATCH")
	reader.HandleFunc("/collections/{nickname}/{slug}/publish", rt.collectionHandler.Publish).Methods("POST")
	reader.HandleFunc("/collections/{nickname}/{slug}/unpublish", rt.collectionHandler.Unpublish).Methods("POST")
	reader.HandleFunc("/collections/{nickname}/{slug}", rt.collectionHandler.Update).Methods("PUT")
	reader.HandleFunc("/collections/{nickname}/{slug}", rt.collectionHandler.Delete).Methods("DELETE")

	// User management (Administrator only)
	admin := api.PathPrefix("").Subrouter()
	admin.Use(middleware.AuthMiddleware(rt.authService))
	admin.Use(middleware.RequireRole(models.RoleAdministrator))
	// Читатели глазами администратора — отдельный вид, а не снятие фильтра с
	// /users: смешать сотрудников с читателями значит зарастить управление
	// сотрудниками. Роль читателя здесь только видна; ни назначить её, ни
	// снять через /users нельзя (validRole знает лишь сотруднические роли).
	//
	// Личные данные, поэтому admin, а не staff, и проверяется РОЛЬ, а не факт
	// входа: тем же JWT_SECRET подписан токен любого постороннего.
	admin.HandleFunc("/readers", rt.readerAdminHandler.List).Methods("GET")
	admin.HandleFunc("/readers/{nickname}/collections", rt.readerAdminHandler.Collections).Methods("GET")

	admin.HandleFunc("/users", rt.userHandler.List).Methods("GET")
	admin.HandleFunc("/users", rt.userHandler.Create).Methods("POST")
	admin.HandleFunc("/users/{id}", rt.userHandler.Update).Methods("PUT")
	admin.HandleFunc("/users/{id}", rt.userHandler.Delete).Methods("DELETE")

	// Обращения: разбор писем (Administrator only)
	//
	// unread-count регистрируется ДО /feedback/{id}: mux разбирает маршруты в
	// порядке добавления, и {id} проглотил бы "unread-count" как значение.
	admin.HandleFunc("/feedback", rt.feedbackHandler.List).Methods("GET")
	admin.HandleFunc("/feedback/unread-count", rt.feedbackHandler.UnreadCount).Methods("GET")
	admin.HandleFunc("/feedback/{id}", rt.feedbackHandler.SetHandled).Methods("PATCH")
	admin.HandleFunc("/feedback/{id}", rt.feedbackHandler.Delete).Methods("DELETE")

	// Сводка и полный сброс — администратору. /cache/stats регистрируется до
	// /cache: mux разбирает маршруты в порядке объявления, и общий путь
	// перехватил бы частный.
	admin.HandleFunc("/cache/stats", rt.cacheHandler.Stats).Methods("GET")
	admin.HandleFunc("/cache", rt.cacheHandler.PurgeAll).Methods("DELETE")

	// Посещаемость и здоровье — администратору.
	admin.HandleFunc("/admin/stats/traffic", rt.statsHandler.Traffic).Methods("GET")
	admin.HandleFunc("/admin/stats/top", rt.statsHandler.Top).Methods("GET")
	admin.HandleFunc("/admin/stats/referrers", rt.statsHandler.Referrers).Methods("GET")
	admin.HandleFunc("/admin/stats/searches", rt.statsHandler.Searches).Methods("GET")
	admin.HandleFunc("/admin/stats/crawlers", rt.statsHandler.Crawlers).Methods("GET")
	admin.HandleFunc("/admin/stats/devices", rt.statsHandler.Devices).Methods("GET")
	admin.HandleFunc("/admin/stats/health", rt.statsHandler.Health).Methods("GET")

	// Full-corpus markdown export (Administrator only)
	admin.HandleFunc("/export", rt.exportHandler.Export).Methods("GET")

	return r
}
