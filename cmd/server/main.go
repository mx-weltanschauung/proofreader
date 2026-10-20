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
	"proofreader/internal/metrics"
	"proofreader/internal/models"
	"proofreader/internal/pagecache"
	"proofreader/internal/repository"
	"proofreader/internal/stats"
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
	// Посещаемость (internal/stats): буфер пишет пачками в фоне, раз в час
	// сворачивает сутки.
	statsRecorder := stats.NewRecorder(repository.NewStatsRepository(db.Pool))

	// Здоровье (internal/metrics): отдаётся на METRICS_ADDR и выжимкой на /admin/stats.
	appMetrics := metrics.New()
	appMetrics.RegisterPgx(db.Pool)
	appMetrics.RegisterCounterFunc("stats_events_accepted_total", "Принятые события посещаемости.",
		func() float64 { return float64(statsRecorder.Accepted()) })
	appMetrics.RegisterCounterFunc("stats_events_dropped_total", "Отброшенные при полном буфере.",
		func() float64 { return float64(statsRecorder.Dropped()) })
	appMetrics.RegisterCounterFunc("stats_write_errors_total", "Сбои записи посещаемости.",
		func() float64 { return float64(statsRecorder.WriteErrors()) })

	// Обработчики и маршруты — api.NewServer: та же сборка идёт в сквозной
	// сторож закрытых работ (internal/repository/closed_sweep_test.go).
	r := api.NewServer(api.ServerDeps{
		Config:    cfg,
		Pool:      db.Pool,
		Commit:    Commit,
		Build:     build,
		PageCache: pageCache,
		Scans:     scans,
		Audio:     audioStore,
		Stats:     statsRecorder,
		Metrics:   appMetrics,
	})

	// Seed admin user if not exists
	if err := seedAdminUser(repository.NewUserRepository(db.Pool), auth.NewService(&cfg.JWT), &cfg.Admin); err != nil {
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
