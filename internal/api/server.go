package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/auth"
	"proofreader/internal/config"
	"proofreader/internal/mcp"
	"proofreader/internal/metrics"
	"proofreader/internal/opds"
	"proofreader/internal/pagecache"
	"proofreader/internal/repository"
	"proofreader/internal/seo"
	"proofreader/internal/stats"
	"proofreader/pkg/markdown"
	"proofreader/pkg/storage"
)

// ServerDeps — то, что сборка читальни берёт снаружи: конфигурация, пул,
// хранилища и службы со своим циклом жизни в cmd/server (уборка кэша глав,
// слив посещаемости, слушатель метрик).
type ServerDeps struct {
	Config *config.Config
	Pool   *pgxpool.Pool
	// Commit — метка линковщика для /api/version; Build — отметка сборки,
	// общая у файлового кэша глав и ETag краулера (pagecache.ResolveCommit).
	Commit, Build string
	PageCache     *pagecache.Store
	// Scans — сканы (с подстраховкой выселенных томов), Audio — бакет звука.
	Scans, Audio storage.Storage
	Stats        *stats.Recorder
	Metrics      *metrics.Metrics
}

// NewServer собирает все обработчики читальни и роутер над ними. Один код на
// cmd/server и на сквозной сторож закрытых работ
// (internal/repository/closed_sweep_test.go): сторож обязан ходить ровно по
// той проводке, что работает на боевом, — копия в тесте пропустила бы,
// например, сырые работы вместо открытого вида в источнике краулера.
func NewServer(d ServerDeps) *mux.Router {
	cfg, pool := d.Config, d.Pool

	// Initialize repositories
	userRepo := repository.NewUserRepository(pool)
	workRepo := repository.NewWorkRepository(pool)
	categoryRepo := repository.NewCategoryRepository(pool)
	pageRepo := repository.NewPageRepository(pool)
	pageVersionRepo := repository.NewPageVersionRepository(pool)
	chapterRepo := repository.NewChapterRepository(pool)
	documentRepo := repository.NewDocumentRepository(pool)
	editionRepo := repository.NewEditionRepository(pool)
	journalRepo := repository.NewJournalRepository(pool)
	personRepo := repository.NewPersonRepository(pool)
	collectionRepo := repository.NewCollectionRepository(pool)
	indexRepo := repository.NewIndexRepository(pool)
	fragmentRepo := repository.NewIndexFragmentRepository(pool)
	documentCutRepo := repository.NewDocumentCutRepository(pool)
	feedbackRepo := repository.NewFeedbackRepository(pool)
	statsRepo := repository.NewStatsRepository(pool)

	// Initialize services
	authService := auth.NewService(&cfg.JWT)
	markdownRenderer := markdown.NewRenderer()
	build := d.Build
	pageCache := d.PageCache
	rangeCache := NewRangeCache(pageCache)
	scans := d.Scans
	audioStore := d.Audio
	statsRecorder := d.Stats
	appMetrics := d.Metrics

	audioRepo := repository.NewAudioRepository(pool)
	audioRecordingRepo := repository.NewAudioRecordingRepository(pool)

	// Initialize handlers
	authAttemptRepo := repository.NewAuthAttemptRepository(pool)
	authHandler := NewAuthHandler(userRepo, authService, authAttemptRepo,
		cfg.JWT.Secret, cfg.Server.TrustProxyHeaders)
	categoryHandler := NewCategoryHandler(categoryRepo)
	pageHandler := NewPageHandler(pageRepo, pageVersionRepo, fragmentRepo, documentCutRepo, markdownRenderer, scans, cfg.S3.PresignTTL)
	closedRepo := repository.NewClosedRepository(pool)
	chapterHandler := NewChapterHandler(chapterRepo, pageRepo, markdownRenderer, rangeCache).
		WithRecordings(audioRecordingRepo).
		WithCredits(personRepo).
		WithClosedChapters(closedRepo)
	eventRepo := repository.NewEventRepository(pool)
	readingHandler := NewReadingHandler(pageRepo, markdownRenderer)
	// Догрузка вклейки целиком по кнопке «Развернуть здесь» (задача 8) — тот же
	// набор источников, что у чтения разбора, плюс свой рендер без подрезки.
	documentCutHandler := NewDocumentCutHandler(documentRepo, documentCutRepo, workRepo, pageRepo, markdownRenderer).WithClosed(closedRepo)
	userHandler := NewUserHandler(userRepo, authService)
	exportHandler := NewExportHandler(workRepo, chapterRepo, pageRepo)
	editionHandler := NewEditionHandler(editionRepo)
	shelfHandler := NewShelfHandler(editionRepo, workRepo).WithJournals(journalRepo).WithClosed(closedRepo).WithEvents(eventRepo)
	highlightHandler := NewHighlightHandler(editionRepo).WithClosed(closedRepo)
	searchRepo := repository.NewSearchRepository(pool)
	searchHandler := NewSearchHandler(searchRepo)

	// Посещаемость (internal/stats): поиск пишет текст запроса и число
	// найденного сам.
	searchHandler.WithRecorder(statsRecorder, cfg.Server.TrustProxyHeaders)

	// Здоровье (internal/metrics): отдаётся на METRICS_ADDR и выжимкой на /admin/stats.
	appMetrics.RegisterSlots("search", searchHandler.Slots())
	// Каналы без токена (.md и MCP, краулер, карточки) видят указатель без
	// закрытого всегда — сотрудника на них нет.
	tokenlessIndex := NewTokenlessIndex(indexRepo, closedRepo)
	indexHandler := NewIndexHandler(indexRepo, workRepo, editionRepo, pageRepo, chapterRepo, fragmentRepo, markdownRenderer).WithClosed(closedRepo)
	collectionHandler := NewCollectionHandler(collectionRepo, pageRepo, markdownRenderer, rangeCache, cfg.JWT.Secret, cfg.Server.TrustProxyHeaders).WithClosed(closedRepo)
	downloadSource := NewDownloadSource(
		workRepo, chapterRepo, pageRepo, editionRepo, collectionRepo,
		markdownRenderer, cfg.Server.PublicBaseURL,
	)
	// Открытый вид работ и изданий и выгрузка над ним — для каналов без
	// токена (краулер, тексты для нейросетей, MCP, OPDS) и для выгрузки
	// не-сотруднику: закрытое там выглядит несуществующим для всех.
	openWorks := NewOpenWorks(workRepo, closedRepo)
	openEditions := NewOpenEditions(editionRepo, closedRepo)
	openDownload := NewDownloadSource(
		openWorks, chapterRepo, pageRepo, openEditions, collectionRepo,
		markdownRenderer, cfg.Server.PublicBaseURL,
	)
	downloadHandler := NewDownloadHandler(downloadSource).WithOpen(openDownload).WithClosed(closedRepo).
		WithAudio(NewAudioPlaylist(audioRepo, audioRecordingRepo, workRepo, chapterRepo, cfg.Server.PublicBaseURL))
	audioHandler := NewAudioHandler(audioRepo, audioRepo, audioRecordingRepo, chapterRepo, audioStore, closedRepo)
	seoRepo := repository.NewSEORepository(pool)
	seoHandler := seo.NewHandler(&seo.Source{
		Works:       openWorks,
		Chapters:    chapterRepo,
		Pages:       pageRepo,
		Editions:    openEditions,
		Concepts:    tokenlessIndex,
		Collections: NewSEOCollectionSource(collectionRepo).WithClosed(closedRepo),
		Documents: NewSEODocumentSource(
			documentRepo, documentCutRepo, openWorks, markdownRenderer).WithClosed(closedRepo),
		Books:        NewSEOBookSource(openDownload),
		ConceptBooks: NewConceptBookSource(tokenlessIndex, pageRepo, chapterRepo, cfg.Server.PublicBaseURL),
		Catalog:      seoRepo,
		Renderer:     markdownRenderer,
		// Тот же адрес, что печатает титульный лист выгрузок: два источника
		// адреса в одной программе разошлись бы, и canonical начал бы
		// противоречить скачанному файлу.
		BaseURL: cfg.Server.PublicBaseURL,
		// Та же отметка, что у файлового кэша глав: новая сборка меняет ETag
		// страниц краулера, иначе 304 отдавал бы вёрстку прежнего рендерера.
		Build: build,
	})
	// Кэши отдачи одним узлом. Собирается после seoHandler, потому что держит
	// и его половину тоже: снос тома обязан уносить и готовые главы с диска, и
	// страницы с карточками, которые краулерская половина держит в памяти.
	servingCache := NewServingCache(pageCache, seoHandler)
	apparatusRepo := repository.NewApparatusRepository(pool)
	workHandler := NewWorkHandler(
		workRepo, pageRepo, markdownRenderer, scans, cfg.S3.PresignTTL,
		servingCache, apparatusRepo,
	).WithAudioStore(audioStore).WithJournalIssues(journalRepo)
	// documentHandler/documentReviewHandler собираются здесь же, а не рядом с
	// остальными обработчиками разбора выше: снятие с публикации и удаление
	// обязаны сбросить краулерскую половину servingCache (см. DropCrawler), а
	// она собирается только после seoHandler.
	documentHandler := NewDocumentHandler(documentRepo, markdownRenderer, documentCutRepo, workRepo, servingCache).WithClosed(closedRepo)
	// Модерация разбора: отправка автором, очередь редактору, приём и отказ.
	// jwtSecret и trustProxy — рельс предела частоты, тот же, что у подборок.
	documentReviewHandler := NewDocumentReviewHandler(
		documentRepo, cfg.JWT.Secret, cfg.Server.TrustProxyHeaders, servingCache)

	versionHandler := NewVersionHandler(func(ctx context.Context) (int, bool, error) {
		var version int
		var dirty bool
		// golang-migrate держит в этой таблице ровно одну строку.
		err := pool.QueryRow(ctx,
			`SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty)
		return version, dirty, err
	}, d.Commit)
	feedbackHandler := NewFeedbackHandler(
		feedbackRepo, cfg.JWT.Secret, cfg.Server.TrustProxyHeaders,
	)
	pageSuggestionRepo := repository.NewPageSuggestionRepository(pool)
	pageSuggestionHandler := NewPageSuggestionHandler(
		pageSuggestionRepo, pageRepo, pageVersionRepo, fragmentRepo, documentCutRepo,
		cfg.JWT.Secret, cfg.Server.TrustProxyHeaders,
	).WithClosed(closedRepo)
	// Настоящее хранилище (pageCache), а не общий rangeCache: сбросу нужно
	// уметь удалять и мести файлы, а не только читать и наполнять.
	cacheHandler := NewCacheHandler(pageCache, chapterRepo)
	// Читатели глазами администратора: разбор жалобы идёт от ника к подборкам.
	readerAdminHandler := NewReaderAdminHandler(userRepo, collectionRepo)

	// MCP-сервер читальни: поиск тот же (и те же два слота, что у сайта —
	// через свои ворота), текст глав — через seoHandler с его кэшем, который
	// сбрасывает снятие тома.
	mcpHandler, mcpGates := mcp.NewHandlerWithGates(mcp.Deps{
		BaseURL:     cfg.Server.PublicBaseURL,
		Search:      searchRepo,
		SearchSlots: searchHandler.Slots(),
		Text:        seoHandler,
		Works:       openWorks,
		Chapters:    chapterRepo,
		Library:     NewMCPSource(openDownload),
		Stats:       statsRecorder,
	})
	// Каталог OPDS: поиск тот же (те же слоты и таймаут), файлы — тот же
	// /api/.../download.
	opdsHandler := opds.NewHandler(NewOPDSSource(
		openEditions, openWorks, chapterRepo, repository.NewOPDSRepository(pool), searchHandler,
	), cfg.Server.PublicBaseURL)
	appMetrics.RegisterSlots("heavy", seoHandler.HeavySlots())
	for name, s := range mcpGates {
		appMetrics.RegisterSlots(name, s)
	}

	// Create router
	router := NewRouter(
		authHandler,
		workHandler,
		pageHandler,
		chapterHandler,
		categoryHandler,
		documentHandler,
		documentCutHandler,
		documentReviewHandler,
		userHandler,
		exportHandler,
		editionHandler,
		shelfHandler,
		indexHandler,
		collectionHandler,
		downloadHandler,
		readingHandler,
		versionHandler,
		feedbackHandler,
		pageSuggestionHandler,
		searchHandler,
		seoHandler,
		cacheHandler,
		readerAdminHandler,
		authService,
	).WithMCP(mcpHandler).
		WithOPDS(opdsHandler).
		WithClosed(closedRepo).
		WithClosedWriter(NewClosedHandler(closedRepo, servingCache).WithChapters(chapterRepo).WithLog(closedRepo)).
		WithAudio(audioHandler).
		WithHighlights(highlightHandler).
		WithEvents(NewEventHandler(eventRepo, closedRepo)).
		WithSite(NewSiteHandler(cfg.Site)).
		WithJournals(NewJournalHandler(journalRepo), NewPersonHandler(personRepo, personRepo, chapterRepo)).
		WithStaticArchive(NewStaticArchiveHandler(cfg.Server.StaticArchiveURL, &http.Client{Timeout: 5 * time.Second})).
		WithStats(NewStatsHandler(statsRecorder, statsRepo, appMetrics, cfg.Server.TrustProxyHeaders)).
		WithMetrics(appMetrics)

	// Setup routes
	return router.Setup()
}
