package config

import (
	"errors"
	"fmt"
	"os"
	"proofreader/internal/site"
	"strconv"
	"strings"
	"time"
)

// Config holds all application configuration
type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	JWT      JWTConfig
	Storage  StorageConfig
	Admin    AdminConfig
	S3       S3Config
	Cache    CacheConfig
	Site     SiteConfig
}

// SiteConfig — всё, чем одна читальня отличается от другой: имя, описание,
// контакт для правообладателей и дополнительные занятые ники. Образы у всех
// экземпляров одни, различие живёт в окружении.
type SiteConfig struct {
	Name        string
	Description string
	// SupportURL, ChannelURL — ссылки «Поддержать» и на канал новостей в
	// подвале; AgeRating — знак возрастной маркировки («18+»). Пусто — не
	// показывать.
	SupportURL string
	ChannelURL string
	AgeRating  string
	// Tagline — короткая подпись под именем в шапке; пусто — подписи нет.
	Tagline string
	// ReservedNicknames дополняет базовый закрытый список ников
	// (models.SetExtraReservedNicknames) — обычно именами авторов корпуса.
	ReservedNicknames []string
}

// DefaultSiteName — имя читальни, когда SITE_NAME не задан.
const DefaultSiteName = site.DefaultName

// SiteNameOr возвращает имя экземпляра или умолчание для пустого — одно место
// на все пакеты, которым имя передаётся полем.
func SiteNameOr(name string) string {
	if name == "" {
		return DefaultSiteName
	}
	return name
}

// ServerConfig holds server-specific configuration
type ServerConfig struct {
	Host string
	Port int
	// PublicBaseURL — адрес читальни, под которым её видит браузер. Идёт на
	// титульный лист каждой выгрузки (см. internal/api/download_source.go),
	// поэтому не обязан совпадать с Host/Port внутри контейнера.
	PublicBaseURL string
	// TrustProxyHeaders — верить ли X-Real-IP при определении адреса
	// читателя. Включается там, где перед бэкендом стоит прокси, который
	// этот заголовок перезаписывает (compose и боевой); при прямом запуске
	// заголовок подделывается кем угодно, поэтому по умолчанию false.
	TrustProxyHeaders bool
	// MetricsAddr — адрес отдельного слушателя /metrics (Prometheus). Пусто —
	// слушателя нет (make run, тесты). В compose — ":9091" без публикации
	// порта: снаружи он недостижим, прокси до него не доходит.
	MetricsAddr string
	// StaticArchiveURL — публичный адрес бакета статической читальни
	// (https://s3.example.org/chitalnya; scripts/static-publish.sh). Из него
	// бэкенд читает manifest.json для /help#offline. Пусто — архива нет:
	// /api/static-archive отвечает 404 (make run, тесты).
	StaticArchiveURL string
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	SSLMode  string
}

// JWTConfig holds JWT configuration
type JWTConfig struct {
	Secret            string
	Expiration        time.Duration
	RefreshExpiration time.Duration
	// ReaderExpiration — отдельный срок читательской сессии. Довод не в
	// удобстве: пароль, введённый однажды и больше никогда, забывается
	// быстрее, чем истекает суточная сессия, а восстановления нет.
	ReaderExpiration time.Duration
}

// StorageConfig holds file storage configuration
type StorageConfig struct {
	Path          string
	MaxUploadSize int64
}

// AdminConfig holds initial admin user configuration
type AdminConfig struct {
	Email    string
	Password string
}

// S3Config holds S3-compatible object storage configuration (SeaweedFS).
type S3Config struct {
	Endpoint       string
	PublicEndpoint string
	Region         string
	Bucket         string
	// AudioBucket — отдельный бакет звука. Не префикс в Bucket: все три
	// скрипта бэкапа ходят в Bucket целиком или по works/{id}/, и звук
	// уезжал бы в каждый снапшот, а restore.sh стирал бы опубликованное после
	// снапшота (спека аудиокниг 29.09, «Хранилище»).
	AudioBucket  string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
	PresignTTL   time.Duration
	// CORSOrigins — origin, которым бэкенд открывает CORS бакета звука на
	// старте (S3_CORS_ORIGINS, через запятую). Пусто — не трогать: местный
	// SeaweedFS отражает любой origin сам.
	CORSOrigins []string

	// S3_FALLBACK_* — боевое хранилище, из которого локальный бэкенд читает
	// сканы выселенных томов (pkg/storage.Fallback, спека
	// 2026-10-03-local-scans-working-set). Пустой адрес — подстраховки нет:
	// так на боевом, в compose без настройки и в тестах. Регион свой: у
	// внешнего S3 (Ceph RGW) он default, у SeaweedFS us-east-1, а подпись его включает.
	FallbackEndpoint  string
	FallbackRegion    string
	FallbackBucket    string
	FallbackAccessKey string
	FallbackSecretKey string
}

// CacheConfig — файловый кэш готовых ответов по диапазону полос.
type CacheConfig struct {
	// Dir — каталог кэша. Пустой означает «кэш выключен»: локальный запуск и
	// тесты не должны заводить файлы на машине разработчика.
	Dir string
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			Host:              getEnv("SERVER_HOST", "0.0.0.0"),
			Port:              getEnvAsInt("SERVER_PORT", 8080),
			PublicBaseURL:     getEnv("PUBLIC_BASE_URL", "http://localhost:3100"),
			TrustProxyHeaders: getEnvAsBool("TRUST_PROXY_HEADERS", false),
			MetricsAddr:       getEnv("METRICS_ADDR", ""),
			StaticArchiveURL:  getEnv("STATIC_ARCHIVE_URL", ""),
		},
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnvAsInt("DB_PORT", 5432),
			User:     getEnv("DB_USER", "proofreader"),
			Password: getEnv("DB_PASSWORD", "proofreader_password"),
			Name:     getEnv("DB_NAME", "proofreader"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
		},
		JWT: JWTConfig{
			Secret:            getEnv("JWT_SECRET", "change-me-in-production"),
			Expiration:        getEnvAsDuration("JWT_EXPIRATION", 24*time.Hour),
			RefreshExpiration: getEnvAsDuration("JWT_REFRESH_EXPIRATION", 7*24*time.Hour),
			ReaderExpiration:  getEnvAsDuration("JWT_READER_EXPIRATION", 90*24*time.Hour),
		},
		Storage: StorageConfig{
			Path:          getEnv("STORAGE_PATH", "./storage"),
			MaxUploadSize: getEnvAsInt64("MAX_UPLOAD_SIZE", 100*1024*1024), // 100MB default
		},
		Admin: AdminConfig{
			Email:    getEnv("ADMIN_EMAIL", "admin@proofreader.local"),
			Password: getEnv("ADMIN_PASSWORD", "admin"),
		},
		S3: S3Config{
			Endpoint:       getEnv("S3_ENDPOINT", "http://localhost:8333"),
			PublicEndpoint: getEnv("S3_PUBLIC_ENDPOINT", ""),
			Region:         getEnv("S3_REGION", "us-east-1"),
			Bucket:         getEnv("S3_BUCKET", "proofreader"),
			AudioBucket:    getEnv("S3_AUDIO_BUCKET", "proofreader-audio"),
			AccessKey:      getEnv("S3_ACCESS_KEY", ""),
			SecretKey:      getEnv("S3_SECRET_KEY", ""),
			UsePathStyle:   getEnvAsBool("S3_USE_PATH_STYLE", true),
			PresignTTL:     getEnvAsDuration("S3_PRESIGN_TTL", 30*time.Minute),
			CORSOrigins:    getEnvAsList("S3_CORS_ORIGINS"),

			FallbackEndpoint:  getEnv("S3_FALLBACK_ENDPOINT", ""),
			FallbackRegion:    getEnv("S3_FALLBACK_REGION", ""),
			FallbackBucket:    getEnv("S3_FALLBACK_BUCKET", ""),
			FallbackAccessKey: getEnv("S3_FALLBACK_ACCESS_KEY", ""),
			FallbackSecretKey: getEnv("S3_FALLBACK_SECRET_KEY", ""),
		},
		Cache: CacheConfig{
			Dir: getEnv("PAGE_CACHE_DIR", ""),
		},
		Site: SiteConfig{
			Name:              getEnv("SITE_NAME", DefaultSiteName),
			Description:       getEnv("SITE_DESCRIPTION", "Библиотека книг, вычитанных по сканам постранично."),
			SupportURL:        getEnv("SITE_SUPPORT_URL", ""),
			ChannelURL:        getEnv("SITE_CHANNEL_URL", ""),
			AgeRating:         getEnv("SITE_AGE_RATING", ""),
			Tagline:           getEnv("SITE_TAGLINE", ""),
			ReservedNicknames: splitList(os.Getenv("RESERVED_NICKNAMES")),
		},
	}

	if cfg.S3.FallbackRegion == "" {
		cfg.S3.FallbackRegion = cfg.S3.Region
	}

	return cfg, nil
}

// FallbackEnabled — задана ли подстраховка чтения сканов.
func (c S3Config) FallbackEnabled() bool { return c.FallbackEndpoint != "" }

// ValidateFallback отвергает неполную подстраховку и подстраховку,
// указывающую на основное хранилище: второе значило бы, что боевому достались
// переменные локальной машины, и обёртка читала бы бакет из самого себя.
func (c S3Config) ValidateFallback() error {
	if !c.FallbackEnabled() {
		return nil
	}
	if c.FallbackBucket == "" || c.FallbackAccessKey == "" || c.FallbackSecretKey == "" {
		return errors.New("S3_FALLBACK_ENDPOINT задан, а S3_FALLBACK_BUCKET, S3_FALLBACK_ACCESS_KEY или S3_FALLBACK_SECRET_KEY пусты")
	}
	norm := func(s string) string { return strings.TrimRight(strings.ToLower(s), "/") }
	if norm(c.FallbackEndpoint) == norm(c.Endpoint) && c.FallbackBucket == c.Bucket {
		return fmt.Errorf("S3_FALLBACK_* указывает на основное хранилище (%s, бакет %s): подстраховка читала бы бакет из самого себя — уберите S3_FALLBACK_* из окружения", c.Endpoint, c.Bucket)
	}
	return nil
}

// DSN returns the PostgreSQL connection string
func (c *DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.Name, c.SSLMode,
	)
}

// Address returns the server address
func (c *ServerConfig) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// Helper functions to get environment variables with defaults
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// splitList режет список через запятую, обрезая пробелы и отбрасывая пустые
// элементы: " a , ,b" — это ["a", "b"].
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func getEnvAsInt(key string, defaultValue int) int {
	valueStr := os.Getenv(key)
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}
	return defaultValue
}

func getEnvAsInt64(key string, defaultValue int64) int64 {
	valueStr := os.Getenv(key)
	if value, err := strconv.ParseInt(valueStr, 10, 64); err == nil {
		return value
	}
	return defaultValue
}

func getEnvAsDuration(key string, defaultValue time.Duration) time.Duration {
	valueStr := os.Getenv(key)
	if value, err := time.ParseDuration(valueStr); err == nil {
		return value
	}
	return defaultValue
}

func getEnvAsBool(key string, defaultValue bool) bool {
	valueStr := os.Getenv(key)
	if value, err := strconv.ParseBool(valueStr); err == nil {
		return value
	}
	return defaultValue
}

// getEnvAsList — значения через запятую; пробелы по краям срезаны, пустые
// отброшены. Нет переменной или она пуста — nil.
func getEnvAsList(key string) []string {
	var out []string
	for _, v := range strings.Split(os.Getenv(key), ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
