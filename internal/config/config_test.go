package config

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadSiteDefaultsAndOverrides(t *testing.T) {
	t.Setenv("SITE_NAME", "")
	t.Setenv("RESERVED_NICKNAMES", " Ленин , ,маркс")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Site.Name != "Читальня" {
		t.Fatalf("имя по умолчанию: %q", cfg.Site.Name)
	}
	want := []string{"Ленин", "маркс"}
	if !reflect.DeepEqual(cfg.Site.ReservedNicknames, want) {
		t.Fatalf("ники: %q", cfg.Site.ReservedNicknames)
	}
}

func TestLoadSiteFromEnv(t *testing.T) {
	t.Setenv("SITE_NAME", "Тестовая")
	t.Setenv("SITE_SUPPORT_URL", "https://example.org/give")
	t.Setenv("SITE_CHANNEL_URL", "https://example.org/news")
	t.Setenv("SITE_AGE_RATING", "18+")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Site.SupportURL != "https://example.org/give" || cfg.Site.ChannelURL != "https://example.org/news" || cfg.Site.AgeRating != "18+" {
		t.Fatalf("ссылки экземпляра: %+v", cfg.Site)
	}
	if cfg.Site.Name != "Тестовая" {
		t.Fatalf("%+v", cfg.Site)
	}
}

func TestLoad(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("Expected config to be loaded")
	}
}

func TestLoadWithDefaults(t *testing.T) {
	os.Clearenv()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host = %s, want 0.0.0.0", cfg.Server.Host)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port = %d, want 8080", cfg.Server.Port)
	}

	if cfg.Database.Host != "localhost" {
		t.Errorf("Database.Host = %s, want localhost", cfg.Database.Host)
	}
	if cfg.Database.Port != 5432 {
		t.Errorf("Database.Port = %d, want 5432", cfg.Database.Port)
	}

	if cfg.JWT.Secret != "change-me-in-production" {
		t.Errorf("JWT.Secret = %s, want change-me-in-production", cfg.JWT.Secret)
	}
	if cfg.JWT.Expiration != 24*time.Hour {
		t.Errorf("JWT.Expiration = %v, want 24h", cfg.JWT.Expiration)
	}
}

func TestLoadWithEnvVars(t *testing.T) {
	os.Clearenv()
	os.Setenv("SERVER_HOST", "127.0.0.1")
	os.Setenv("SERVER_PORT", "9000")
	os.Setenv("DB_HOST", "db.example.com")
	os.Setenv("DB_PORT", "5433")
	os.Setenv("JWT_SECRET", "my-secret-key")
	defer os.Clearenv()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("Server.Host = %s, want 127.0.0.1", cfg.Server.Host)
	}
	if cfg.Server.Port != 9000 {
		t.Errorf("Server.Port = %d, want 9000", cfg.Server.Port)
	}
	if cfg.Database.Host != "db.example.com" {
		t.Errorf("Database.Host = %s, want db.example.com", cfg.Database.Host)
	}
	if cfg.Database.Port != 5433 {
		t.Errorf("Database.Port = %d, want 5433", cfg.Database.Port)
	}
	if cfg.JWT.Secret != "my-secret-key" {
		t.Errorf("JWT.Secret = %s, want my-secret-key", cfg.JWT.Secret)
	}
}

func TestDatabaseConfigDSN(t *testing.T) {
	cfg := DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "testuser",
		Password: "testpass",
		Name:     "testdb",
		SSLMode:  "disable",
	}

	expected := "host=localhost port=5432 user=testuser password=testpass dbname=testdb sslmode=disable"
	dsn := cfg.DSN()
	if dsn != expected {
		t.Errorf("DSN() = %s, want %s", dsn, expected)
	}
}

func TestServerConfigAddress(t *testing.T) {
	tests := []struct {
		host     string
		port     int
		expected string
	}{
		{"0.0.0.0", 8080, "0.0.0.0:8080"},
		{"127.0.0.1", 3000, "127.0.0.1:3000"},
		{"localhost", 80, "localhost:80"},
	}

	for _, tt := range tests {
		cfg := ServerConfig{Host: tt.host, Port: tt.port}
		addr := cfg.Address()
		if addr != tt.expected {
			t.Errorf("Address() = %s, want %s", addr, tt.expected)
		}
	}
}

func TestGetEnvAsInt(t *testing.T) {
	os.Clearenv()

	os.Setenv("TEST_INT_VALID", "42")
	os.Setenv("TEST_INT_INVALID", "not-a-number")
	defer os.Clearenv()

	if v := getEnvAsInt("TEST_INT_VALID", 0); v != 42 {
		t.Errorf("getEnvAsInt valid = %d, want 42", v)
	}

	if v := getEnvAsInt("TEST_INT_INVALID", 99); v != 99 {
		t.Errorf("getEnvAsInt invalid = %d, want 99 (default)", v)
	}

	if v := getEnvAsInt("TEST_INT_MISSING", 77); v != 77 {
		t.Errorf("getEnvAsInt missing = %d, want 77 (default)", v)
	}
}

func TestGetEnvAsInt64(t *testing.T) {
	os.Clearenv()

	os.Setenv("TEST_INT64_VALID", "9999999999")
	os.Setenv("TEST_INT64_INVALID", "abc")
	defer os.Clearenv()

	if v := getEnvAsInt64("TEST_INT64_VALID", 0); v != 9999999999 {
		t.Errorf("getEnvAsInt64 valid = %d, want 9999999999", v)
	}

	if v := getEnvAsInt64("TEST_INT64_INVALID", 123); v != 123 {
		t.Errorf("getEnvAsInt64 invalid = %d, want 123 (default)", v)
	}

	if v := getEnvAsInt64("TEST_INT64_MISSING", 456); v != 456 {
		t.Errorf("getEnvAsInt64 missing = %d, want 456 (default)", v)
	}
}

func TestGetEnvAsDuration(t *testing.T) {
	os.Clearenv()

	os.Setenv("TEST_DUR_VALID", "2h30m")
	os.Setenv("TEST_DUR_INVALID", "not-a-duration")
	defer os.Clearenv()

	expected := 2*time.Hour + 30*time.Minute
	if v := getEnvAsDuration("TEST_DUR_VALID", 0); v != expected {
		t.Errorf("getEnvAsDuration valid = %v, want %v", v, expected)
	}

	defaultDur := time.Hour
	if v := getEnvAsDuration("TEST_DUR_INVALID", defaultDur); v != defaultDur {
		t.Errorf("getEnvAsDuration invalid = %v, want %v (default)", v, defaultDur)
	}

	if v := getEnvAsDuration("TEST_DUR_MISSING", defaultDur); v != defaultDur {
		t.Errorf("getEnvAsDuration missing = %v, want %v (default)", v, defaultDur)
	}
}

func TestGetEnv(t *testing.T) {
	os.Clearenv()

	os.Setenv("TEST_STRING", "hello")
	defer os.Clearenv()

	if v := getEnv("TEST_STRING", "default"); v != "hello" {
		t.Errorf("getEnv existing = %s, want hello", v)
	}

	if v := getEnv("TEST_STRING_MISSING", "default"); v != "default" {
		t.Errorf("getEnv missing = %s, want default", v)
	}
}

func TestLoadS3Defaults(t *testing.T) {
	for _, k := range []string{"S3_ENDPOINT", "S3_REGION", "S3_BUCKET", "S3_AUDIO_BUCKET", "S3_ACCESS_KEY", "S3_SECRET_KEY", "S3_USE_PATH_STYLE", "S3_PRESIGN_TTL"} {
		os.Unsetenv(k)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.S3.Bucket != "proofreader" {
		t.Errorf("S3.Bucket = %q, want %q", cfg.S3.Bucket, "proofreader")
	}
	if cfg.S3.AudioBucket != "proofreader-audio" {
		t.Errorf("S3.AudioBucket = %q, want %q", cfg.S3.AudioBucket, "proofreader-audio")
	}
	if cfg.S3.PresignTTL != 30*time.Minute {
		t.Errorf("S3.PresignTTL = %v, want 30m", cfg.S3.PresignTTL)
	}
	if !cfg.S3.UsePathStyle {
		t.Error("S3.UsePathStyle should default to true")
	}
}

func TestLoadS3Overrides(t *testing.T) {
	os.Setenv("S3_ENDPOINT", "http://seaweed:8333")
	os.Setenv("S3_BUCKET", "books")
	os.Setenv("S3_PRESIGN_TTL", "10m")
	defer func() {
		os.Unsetenv("S3_ENDPOINT")
		os.Unsetenv("S3_BUCKET")
		os.Unsetenv("S3_PRESIGN_TTL")
	}()
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.S3.Endpoint != "http://seaweed:8333" {
		t.Errorf("S3.Endpoint = %q", cfg.S3.Endpoint)
	}
	if cfg.S3.Bucket != "books" {
		t.Errorf("S3.Bucket = %q", cfg.S3.Bucket)
	}
	if cfg.S3.PresignTTL != 10*time.Minute {
		t.Errorf("S3.PresignTTL = %v", cfg.S3.PresignTTL)
	}
}

func TestLoadReadsPageCacheDir(t *testing.T) {
	t.Setenv("PAGE_CACHE_DIR", "/var/cache/proofreader")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Cache.Dir != "/var/cache/proofreader" {
		t.Errorf("Cache.Dir = %q, ожидался /var/cache/proofreader", cfg.Cache.Dir)
	}
}

// Пустой по умолчанию: локальный make run и go test ./... не должны
// заводить файлы на машине разработчика.
func TestPageCacheDirIsEmptyByDefault(t *testing.T) {
	t.Setenv("PAGE_CACHE_DIR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Cache.Dir != "" {
		t.Errorf("Cache.Dir = %q, ожидалась пустая строка", cfg.Cache.Dir)
	}
}

func TestLoadS3PublicEndpoint(t *testing.T) {
	os.Unsetenv("S3_PUBLIC_ENDPOINT")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.S3.PublicEndpoint != "" {
		t.Errorf("default S3.PublicEndpoint = %q, want empty", cfg.S3.PublicEndpoint)
	}
	os.Setenv("S3_PUBLIC_ENDPOINT", "http://localhost:8333")
	defer os.Unsetenv("S3_PUBLIC_ENDPOINT")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.S3.PublicEndpoint != "http://localhost:8333" {
		t.Errorf("S3.PublicEndpoint = %q", cfg.S3.PublicEndpoint)
	}
}

func TestLoadS3CORSOrigins(t *testing.T) {
	t.Setenv("S3_CORS_ORIGINS", " https://lib.example.org , ,https://x.example.org")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"https://lib.example.org", "https://x.example.org"}
	if !reflect.DeepEqual(cfg.S3.CORSOrigins, want) {
		t.Errorf("CORSOrigins = %q, ждали %q", cfg.S3.CORSOrigins, want)
	}
}

func TestLoadS3CORSOriginsEmptyByDefault(t *testing.T) {
	t.Setenv("S3_CORS_ORIGINS", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.S3.CORSOrigins) != 0 {
		t.Errorf("CORSOrigins = %q, ждали пусто", cfg.S3.CORSOrigins)
	}
}

// Без S3_FALLBACK_ENDPOINT подстраховки нет: так на боевом, в compose без
// настройки и в go test.
func TestS3FallbackDisabledByDefault(t *testing.T) {
	t.Setenv("S3_FALLBACK_ENDPOINT", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.S3.FallbackEnabled() {
		t.Fatal("подстраховка включилась без S3_FALLBACK_ENDPOINT")
	}
	if err := cfg.S3.ValidateFallback(); err != nil {
		t.Fatalf("выключенная подстраховка отвергнута: %v", err)
	}
}

// У внешнего S3 регион default, у локального SeaweedFS us-east-1: подпись SigV4
// включает регион, поэтому у подстраховки он свой; пусто — как у основного.
func TestS3FallbackRegionDefaultsToMain(t *testing.T) {
	t.Setenv("S3_REGION", "us-east-1")
	t.Setenv("S3_FALLBACK_REGION", "")
	cfg, _ := Load()
	if cfg.S3.FallbackRegion != "us-east-1" {
		t.Fatalf("FallbackRegion = %q, ждали регион основного", cfg.S3.FallbackRegion)
	}
	t.Setenv("S3_FALLBACK_REGION", "default")
	cfg, _ = Load()
	if cfg.S3.FallbackRegion != "default" {
		t.Fatalf("FallbackRegion = %q, ждали default", cfg.S3.FallbackRegion)
	}
}

// Подстраховка, указывающая на основное хранилище, — защита от случайно
// доставшихся боевому переменных: отказ старта.
func TestS3FallbackRefusesMainStorage(t *testing.T) {
	c := S3Config{
		Endpoint: "http://seaweedfs:8333", Bucket: "proofreader",
		FallbackEndpoint: "HTTP://seaweedfs:8333/", FallbackBucket: "proofreader",
		FallbackAccessKey: "k", FallbackSecretKey: "s",
	}
	err := c.ValidateFallback()
	if err == nil || !strings.Contains(err.Error(), "основное хранилище") {
		t.Fatalf("ValidateFallback = %v; ждали отказ «основное хранилище»", err)
	}
	c.FallbackEndpoint = "https://s3.example.org"
	if err := c.ValidateFallback(); err != nil {
		t.Fatalf("внешний S3 отвергнут: %v", err)
	}
	c.FallbackEndpoint, c.FallbackBucket = "http://seaweedfs:8333", "proofreader-prod-copy"
	if err := c.ValidateFallback(); err != nil {
		t.Fatalf("другой бакет на том же адресе отвергнут: %v", err)
	}
}

func TestS3FallbackNeedsBucketAndKeys(t *testing.T) {
	c := S3Config{Endpoint: "http://seaweedfs:8333", Bucket: "proofreader", FallbackEndpoint: "https://s3.example.org"}
	if err := c.ValidateFallback(); err == nil {
		t.Fatal("подстраховка без бакета и ключей принята")
	}
}
