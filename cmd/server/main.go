package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"proofreader/internal/site"
	"syscall"
	"time"

	"proofreader/internal/api"
	"proofreader/internal/auth"
	"proofreader/internal/config"
	"proofreader/internal/database"
	"proofreader/internal/mcp"
	"proofreader/internal/metrics"
	"proofreader/internal/models"
	"proofreader/internal/opds"
	"proofreader/internal/pagecache"
	"proofreader/internal/repository"
	"proofreader/internal/seo"
	"proofreader/internal/stats"
	"proofreader/pkg/markdown"
	"proofreader/pkg/storage"

	"github.com/joho/godotenv"
)

// Commit подставляется линковщиком: -ldflags "-X main.Commit=$(git rev-parse --short HEAD)".
// Значение по умолчанию честно говорит, что сборка не помечена.
var Commit = "unknown"

func main() {
	if err := godotenv.Load(); err != nil {
		log.Printf("No .env file loaded (%v); using environment variables", err)
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	// Имя самой читальни занято всегда: подпись «Читальня» под подборкой
	// читателя выдавала бы её за редакционную.
	site.Set(cfg.Site.Name, cfg.Site.Description)
	models.SetExtraReservedNicknames(append([]string{cfg.Site.Name}, cfg.Site.ReservedNicknames...))

	// Connect to database
	db, err := database.New(&cfg.Database)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	log.Println("Connected to database successfully")

	// Initialize repositories
	userRepo := repository.NewUserRepository(db.Pool)
	workRepo := repository.NewWorkRepository(db.Pool)
	categoryRepo := repository.NewCategoryRepository(db.Pool)
	pageRepo := repository.NewPageRepository(db.Pool)
	pageVersionRepo := repository.NewPageVersionRepository(db.Pool)
	chapterRepo := repository.NewChapterRepository(db.Pool)
	documentRepo := repository.NewDocumentRepository(db.Pool)
	editionRepo := repository.NewEditionRepository(db.Pool)
	collectionRepo := repository.NewCollectionRepository(db.Pool)
	indexRepo := repository.NewIndexRepository(db.Pool)
	fragmentRepo := repository.NewIndexFragmentRepository(db.Pool)
	documentCutRepo := repository.NewDocumentCutRepository(db.Pool)
	feedbackRepo := repository.NewFeedbackRepository(db.Pool)
	statsRepo := repository.NewStatsRepository(db.Pool)

	// Initialize services
	authService := auth.NewService(&cfg.JWT)
	markdownRenderer := markdown.NewRenderer()

	// Файловый кэш готовых ответов по диапазону полос. Отметка коммита
	// стирает содержимое чужой сборки: изменившийся рендерер не должен
	// доехать до читателя. ResolveCommit подстраховывает: compose-сборки
	// (включая bootstrap.sh на боевом) не передают --build-arg COMMIT и дают
	// одинаковый "unknown" на любой пересборке — без подмены это обещание
	// держалось бы только на пути release.sh.
	//
	// Отметка вычисляется один раз и отдаётся ещё и краулерской половине (ETag
	// в internal/seo): без коммита ResolveCommit может вернуть метку момента
	// запуска, и два вызова дали бы две разные сборки.
	build := pagecache.ResolveCommit(Commit)
	pageCache := pagecache.New(cfg.Cache.Dir)
	if err := pageCache.Init(build); err != nil {
		log.Printf("кэш глав выключен: %v", err)
		pageCache = pagecache.New("")
	}
	rangeCache := api.NewRangeCache(pageCache)

	if pageCache.Enabled() {
		go func() {
			ticker := time.NewTicker(10 * time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				expired, evicted, err := pageCache.Sweep()
				if err != nil {
					log.Printf("уборка кэша глав: %v", err)
					continue
				}
				if evicted > 0 {
					// Вытеснение по объёму означает, что потолок подобран
					// неверно: часовой срок годности должен справляться сам.
					log.Printf("кэш глав: снято по сроку %d, вытеснено по объёму %d", expired, evicted)
				}
			}
		}()
	}

	store, err := storage.NewS3Storage(storage.S3Config{
		Endpoint:       cfg.S3.Endpoint,
		PublicEndpoint: cfg.S3.PublicEndpoint,
		Region:         cfg.S3.Region,
		Bucket:         cfg.S3.Bucket,
		AccessKey:      cfg.S3.AccessKey,
		SecretKey:      cfg.S3.SecretKey,
		UsePathStyle:   cfg.S3.UsePathStyle,
	})
	if err != nil {
		log.Fatalf("Failed to init storage: %v", err)
	}
	if err := store.EnsureBucket(context.Background()); err != nil {
		log.Printf("Warning: ensure bucket: %v", err)
	}

	// Сканы выселенных томов: локально только рабочий набор, остальное
	// читается из боевого бакета по тому же ключу (спека
	// 2026-10-03-local-scans-working-set). Отказ старта на подстраховке,
	// указывающей на основное хранилище, — защита боевого от локальных
	// переменных.
	if err := cfg.S3.ValidateFallback(); err != nil {
		log.Fatalf("Failed to init storage: %v", err)
	}
	var scans storage.Storage = store
	if cfg.S3.FallbackEnabled() {
		secondary, err := storage.NewS3Storage(storage.S3Config{
			Endpoint:     cfg.S3.FallbackEndpoint,
			Region:       cfg.S3.FallbackRegion,
			Bucket:       cfg.S3.FallbackBucket,
			AccessKey:    cfg.S3.FallbackAccessKey,
			SecretKey:    cfg.S3.FallbackSecretKey,
			UsePathStyle: cfg.S3.UsePathStyle,
		})
		if err != nil {
			log.Fatalf("Failed to init fallback storage: %v", err)
		}
		scans = storage.NewFallback(store, secondary)
		log.Printf("сканы выселенных томов читаются из %s, бакет %s", cfg.S3.FallbackEndpoint, cfg.S3.FallbackBucket)
	}

	// Аудиобакет: тот же SeaweedFS и то же удостоверение, отдельный бакет —
	// бэкапы ходят в основной целиком или по works/{id}/ (спека аудиокниг,
	// «Хранилище»).
	audioStore, err := storage.NewS3Storage(storage.S3Config{
		Endpoint:       cfg.S3.Endpoint,
		PublicEndpoint: cfg.S3.PublicEndpoint,
		Region:         cfg.S3.Region,
		Bucket:         cfg.S3.AudioBucket,
		AccessKey:      cfg.S3.AccessKey,
		SecretKey:      cfg.S3.SecretKey,
		UsePathStyle:   cfg.S3.UsePathStyle,
	})
	if err != nil {
		log.Fatalf("Failed to init audio storage: %v", err)
	}
	if err := audioStore.EnsureBucket(context.Background()); err != nil {
		log.Printf("Warning: ensure audio bucket: %v", err)
	}
	// Запись человека заливается presigned PUT прямо из браузера; внешний S3
	// без правил CORS отвечает на preflight 403. Предупреждение, а не отказ
	// стартовать — как у EnsureBucket выше: читальня важнее записи, а смок
	// переключения хранилища проверяет preflight отдельно.
	if err := audioStore.SetCORS(context.Background(), cfg.S3.CORSOrigins); err != nil {
		log.Printf("Warning: audio bucket CORS: %v", err)
	}
	audioRepo := repository.NewAudioRepository(db.Pool)
	audioRecordingRepo := repository.NewAudioRecordingRepository(db.Pool)

	// Initialize handlers
	authAttemptRepo := repository.NewAuthAttemptRepository(db.Pool)
	authHandler := api.NewAuthHandler(userRepo, authService, authAttemptRepo,
		cfg.JWT.Secret, cfg.Server.TrustProxyHeaders)
	categoryHandler := api.NewCategoryHandler(categoryRepo)
	pageHandler := api.NewPageHandler(pageRepo, pageVersionRepo, fragmentRepo, documentCutRepo, markdownRenderer, scans, cfg.S3.PresignTTL)
	chapterHandler := api.NewChapterHandler(chapterRepo, pageRepo, markdownRenderer, rangeCache).
		WithRecordings(audioRecordingRepo)
	readingHandler := api.NewReadingHandler(pageRepo, markdownRenderer)
	// Догрузка вклейки целиком по кнопке «Развернуть здесь» (задача 8) — тот же
	// набор источников, что у чтения разбора, плюс свой рендер без подрезки.
	documentCutHandler := api.NewDocumentCutHandler(documentRepo, documentCutRepo, workRepo, pageRepo, markdownRenderer)
	userHandler := api.NewUserHandler(userRepo, authService)
	exportHandler := api.NewExportHandler(workRepo, chapterRepo, pageRepo)
	editionHandler := api.NewEditionHandler(editionRepo)
	shelfHandler := api.NewShelfHandler(editionRepo, workRepo)
	highlightHandler := api.NewHighlightHandler(editionRepo)
	searchRepo := repository.NewSearchRepository(db.Pool)
	searchHandler := api.NewSearchHandler(searchRepo)

	// Посещаемость (internal/stats): буфер пишет пачками в фоне, раз в час
	// сворачивает сутки. Поиск пишет текст запроса и число найденного сам.
	statsRecorder := stats.NewRecorder(statsRepo)
	searchHandler.WithRecorder(statsRecorder, cfg.Server.TrustProxyHeaders)

	// Здоровье (internal/metrics): отдаётся на METRICS_ADDR и выжимкой на /admin/stats.
	appMetrics := metrics.New()
	appMetrics.RegisterPgx(db.Pool)
	appMetrics.RegisterSlots("search", searchHandler.Slots())
	appMetrics.RegisterCounterFunc("stats_events_accepted_total", "Принятые события посещаемости.",
		func() float64 { return float64(statsRecorder.Accepted()) })
	appMetrics.RegisterCounterFunc("stats_events_dropped_total", "Отброшенные при полном буфере.",
		func() float64 { return float64(statsRecorder.Dropped()) })
	appMetrics.RegisterCounterFunc("stats_write_errors_total", "Сбои записи посещаемости.",
		func() float64 { return float64(statsRecorder.WriteErrors()) })
	indexHandler := api.NewIndexHandler(indexRepo, workRepo, editionRepo, pageRepo, chapterRepo, fragmentRepo, markdownRenderer)
	collectionHandler := api.NewCollectionHandler(collectionRepo, pageRepo, markdownRenderer, rangeCache, cfg.JWT.Secret, cfg.Server.TrustProxyHeaders)
	downloadSource := api.NewDownloadSource(
		workRepo, chapterRepo, pageRepo, editionRepo, collectionRepo,
		markdownRenderer, cfg.Server.PublicBaseURL,
	)
	downloadHandler := api.NewDownloadHandler(downloadSource).
		WithAudio(api.NewAudioPlaylist(audioRepo, audioRecordingRepo, workRepo, chapterRepo, cfg.Server.PublicBaseURL))
	audioHandler := api.NewAudioHandler(audioRepo, audioRepo, audioRecordingRepo, chapterRepo, audioStore)
	seoRepo := repository.NewSEORepository(db.Pool)
	seoHandler := seo.NewHandler(&seo.Source{
		Works:       workRepo,
		Chapters:    chapterRepo,
		Pages:       pageRepo,
		Editions:    editionRepo,
		Concepts:    indexRepo,
		Collections: api.NewSEOCollectionSource(collectionRepo),
		Documents: api.NewSEODocumentSource(
			documentRepo, documentCutRepo, workRepo, markdownRenderer),
		Books:        api.NewSEOBookSource(downloadSource),
		ConceptBooks: api.NewConceptBookSource(indexRepo, pageRepo, chapterRepo, cfg.Server.PublicBaseURL),
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
	servingCache := api.NewServingCache(pageCache, seoHandler)
	apparatusRepo := repository.NewApparatusRepository(db.Pool)
	workHandler := api.NewWorkHandler(
		workRepo, pageRepo, markdownRenderer, scans, cfg.S3.PresignTTL,
		servingCache, apparatusRepo,
	).WithAudioStore(audioStore)
	// documentHandler/documentReviewHandler собираются здесь же, а не рядом с
	// остальными обработчиками разбора выше: снятие с публикации и удаление
	// обязаны сбросить краулерскую половину servingCache (см. DropCrawler), а
	// она собирается только после seoHandler.
	documentHandler := api.NewDocumentHandler(documentRepo, markdownRenderer, documentCutRepo, workRepo, servingCache)
	// Модерация разбора: отправка автором, очередь редактору, приём и отказ.
	// jwtSecret и trustProxy — рельс предела частоты, тот же, что у подборок.
	documentReviewHandler := api.NewDocumentReviewHandler(
		documentRepo, cfg.JWT.Secret, cfg.Server.TrustProxyHeaders, servingCache)

	versionHandler := api.NewVersionHandler(func(ctx context.Context) (int, bool, error) {
		var version int
		var dirty bool
		// golang-migrate держит в этой таблице ровно одну строку.
		err := db.Pool.QueryRow(ctx,
			`SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty)
		return version, dirty, err
	}, Commit)
	feedbackHandler := api.NewFeedbackHandler(
		feedbackRepo, cfg.JWT.Secret, cfg.Server.TrustProxyHeaders,
	)
	pageSuggestionRepo := repository.NewPageSuggestionRepository(db.Pool)
	pageSuggestionHandler := api.NewPageSuggestionHandler(
		pageSuggestionRepo, pageRepo, pageVersionRepo, fragmentRepo, documentCutRepo,
		cfg.JWT.Secret, cfg.Server.TrustProxyHeaders,
	)
	// Настоящее хранилище (pageCache), а не общий rangeCache: сбросу нужно
	// уметь удалять и мести файлы, а не только читать и наполнять.
	cacheHandler := api.NewCacheHandler(pageCache, chapterRepo)
	// Читатели глазами администратора: разбор жалобы идёт от ника к подборкам.
	readerAdminHandler := api.NewReaderAdminHandler(userRepo, collectionRepo)

	// MCP-сервер читальни: поиск тот же (и те же два слота, что у сайта —
	// через свои ворота), текст глав — через seoHandler с его кэшем, который
	// сбрасывает снятие тома.
	mcpHandler, mcpGates := mcp.NewHandlerWithGates(mcp.Deps{
		BaseURL:     cfg.Server.PublicBaseURL,
		Search:      searchRepo,
		SearchSlots: searchHandler.Slots(),
		Text:        seoHandler,
		Works:       workRepo,
		Chapters:    chapterRepo,
		Library:     api.NewMCPSource(downloadSource),
		Stats:       statsRecorder,
	})
	// Каталог OPDS: поиск тот же (те же слоты и таймаут), файлы — тот же
	// /api/.../download.
	opdsHandler := opds.NewHandler(api.NewOPDSSource(
		editionRepo, workRepo, chapterRepo, repository.NewOPDSRepository(db.Pool), searchHandler,
	), cfg.Server.PublicBaseURL)
	appMetrics.RegisterSlots("heavy", seoHandler.HeavySlots())
	for name, s := range mcpGates {
		appMetrics.RegisterSlots(name, s)
	}

	// Create router
	router := api.NewRouter(
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
		WithAudio(audioHandler).
		WithHighlights(highlightHandler).
		WithSite(api.NewSiteHandler(cfg.Site)).
		WithStaticArchive(api.NewStaticArchiveHandler(cfg.Server.StaticArchiveURL, &http.Client{Timeout: 5 * time.Second})).
		WithStats(api.NewStatsHandler(statsRecorder, statsRepo, appMetrics, cfg.Server.TrustProxyHeaders)).
		WithMetrics(appMetrics)

	// Setup routes
	r := router.Setup()

	// Seed admin user if not exists
	if err := seedAdminUser(userRepo, authService, &cfg.Admin); err != nil {
		log.Printf("Warning: Failed to seed admin user: %v", err)
	}

	// Create HTTP server
	srv := &http.Server{
		Addr:         cfg.Server.Address(),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	statsCtx, stopStats := context.WithCancel(context.Background())
	statsDone := make(chan struct{})
	go func() { statsRecorder.Run(statsCtx); close(statsDone) }()

	var metricsSrv *http.Server
	if addr := cfg.Server.MetricsAddr; addr != "" {
		metricsSrv = &http.Server{Addr: addr, Handler: appMetrics.Handler(), ReadTimeout: 5 * time.Second}
		go func() {
			log.Printf("Metrics on %s", addr)
			if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("metrics listener: %v", err) // не фатально: читальня работает и без него
			}
		}()
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Server starting on %s", cfg.Server.Address())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Server is shutting down...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	// Сначала перестали принимать запросы, теперь досливаем буфер посещаемости.
	stopStats()
	<-statsDone
	if metricsSrv != nil {
		_ = metricsSrv.Shutdown(ctx)
	}

	log.Println("Server stopped")
}

// seedAdminUser creates an admin user if one doesn't exist
func seedAdminUser(userRepo *repository.UserRepository, authService *auth.Service, adminCfg *config.AdminConfig) error {
	ctx := context.Background()

	// Check if admin user already exists
	_, err := userRepo.GetByEmail(ctx, adminCfg.Email)
	if err == nil {
		// Admin user already exists
		return nil
	}

	// Create admin user
	hashedPassword, err := authService.HashPassword(adminCfg.Password)
	if err != nil {
		return err
	}

	adminUser := &models.User{
		Email:        adminCfg.Email,
		PasswordHash: hashedPassword,
		Role:         models.RoleAdministrator,
	}

	if err := userRepo.Create(ctx, adminUser); err != nil {
		return err
	}

	log.Printf("Admin user created: %s", adminCfg.Email)
	return nil
}
