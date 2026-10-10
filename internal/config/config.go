// Package config loads and validates runtime configuration from the
// environment. No package may read os.Getenv directly for runtime settings;
// everything funnels through here so defaults, validation, and redaction are
// enforced in one place.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the validated runtime configuration for the mediarr service.
type Config struct {
	// Server
	HTTPAddr        string // listen address, e.g. ":8080"
	HTTPBaseURL     string // public base URL for CORS/links, e.g. "http://localhost:8080"
	ReadTimeout     int    // seconds
	WriteTimeout    int    // seconds
	ShutdownTimeout int    // seconds to drain in-flight requests

	// Storage
	Postgres DSN // PostgreSQL connection string
	Redis    URL // Redis connection URL
	RedisDB  int // Redis logical database index

	// Media
	// MediaRoot is the directory where imported media files are placed
	// (e.g. /media/movies). Required for the import step to know its target.
	MediaRoot string
	// DownloadsDir is where the download client writes completed files before
	// import moves them under MediaRoot.
	DownloadsDir string

	// Security
	EncryptionKey string // 32-byte hex key for AES-256-GCM at-rest encryption
	// AuthPinning: when true, the first user created is the only one that can
	// change credentials (single-user v1 mode).
	AuthPinning bool

	// Indexer
	// IndexerHealthInterval is how often the health worker probes every enabled
	// indexer (#34). Zero disables the periodic loop (on-demand tests still
	// work). Negative values are normalised to zero in the worker.
	IndexerHealthInterval time.Duration
}

// DSN is a validated PostgreSQL connection string.
type DSN string

// URL is a validated connection URL (redis://, postgres://).
type URL string

// Load reads configuration from the environment, applying defaults and
// failing fast on any invalid or missing required value.
func Load(env map[string]string) (*Config, error) {
	get := func(k, def string) string {
		if v, ok := env[k]; ok && v != "" {
			return v
		}
		return def
	}

	cfg := &Config{
		HTTPAddr:        get("MEDIARR_HTTP_ADDR", ":8080"),
		HTTPBaseURL:     get("MEDIARR_HTTP_BASE_URL", "http://localhost:8080"),
		ReadTimeout:     getInt(env, "MEDIARR_READ_TIMEOUT", 15),
		WriteTimeout:    getInt(env, "MEDIARR_WRITE_TIMEOUT", 60),
		ShutdownTimeout: getInt(env, "MEDIARR_SHUTDOWN_TIMEOUT", 10),
		RedisDB:         getInt(env, "MEDIARR_REDIS_DB", 0),
		MediaRoot:       get("MEDIARR_MEDIA_ROOT", "/media/movies"),
		DownloadsDir:    get("MEDIARR_DOWNLOADS_DIR", "/downloads"),
	}

	// Postgres DSN (required)
	pg := env["MEDIARR_POSTGRES_DSN"]
	if pg == "" {
		pg = env["DATABASE_URL"]
	}
	if pg == "" {
		return nil, fmt.Errorf("MEDIARR_POSTGRES_DSN (or DATABASE_URL) is required")
	}
	if err := validatePostgresDSN(pg); err != nil {
		return nil, err
	}
	cfg.Postgres = DSN(pg)

	// Redis URL (required)
	redis := env["MEDIARR_REDIS_URL"]
	if redis == "" {
		return nil, fmt.Errorf("MEDIARR_REDIS_URL is required")
	}
	if err := validateURL(redis); err != nil {
		return nil, err
	}
	cfg.Redis = URL(redis)

	// Encryption key (required): 64 hex chars = 32 bytes.
	key := env["MEDIARR_ENCRYPTION_KEY"]
	if key == "" {
		return nil, fmt.Errorf("MEDIARR_ENCRYPTION_KEY is required (64 hex chars, 32 bytes)")
	}
	if err := validateEncryptionKey(key); err != nil {
		return nil, err
	}
	cfg.EncryptionKey = key

	// Auth pinning (single-user v1 mode) — default on.
	cfg.AuthPinning = getBool(env, "MEDIARR_AUTH_PINNING", true)

	// Indexer health-check cadence (#34) — default 5m; 0 disables the periodic
	// loop. A non-duration string degrades to 0 (disabled) rather than
	// failing boot: health monitoring is an enhancement, not a precondition.
	if raw := env["MEDIARR_INDEXER_HEALTH_INTERVAL"]; raw != "" {
		if d, err := time.ParseDuration(raw); err == nil {
			if d < 0 {
				d = 0
			}
			cfg.IndexerHealthInterval = d
		} else {
			cfg.IndexerHealthInterval = 0
		}
	}

	return cfg, nil
}

// LoadFromOS is a convenience wrapper over Load for the real process.
func LoadFromOS() (*Config, error) {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 {
			env[kv[:i]] = kv[i+1:]
		}
	}
	return Load(env)
}

func getInt(env map[string]string, k string, def int) int {
	if v, ok := env[k]; ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			// Fall back to default rather than failing; callers that need a
			// hard requirement can re-validate.
			return def
		}
		return n
	}
	return def
}

func getBool(env map[string]string, k string, def bool) bool {
	if v, ok := env[k]; ok && v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return def
		}
		return b
	}
	return def
}

func validatePostgresDSN(dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("invalid postgres DSN: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return fmt.Errorf("postgres DSN must use postgres:// or postgresql:// scheme, got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("postgres DSN missing host")
	}
	return nil
}

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "redis" && u.Scheme != "rediss" {
		return fmt.Errorf("redis URL must use redis:// or rediss:// scheme, got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("redis URL missing host")
	}
	return nil
}

func validateEncryptionKey(key string) error {
	if len(key) != 64 {
		return fmt.Errorf("encryption key must be 64 hex chars (32 bytes), got %d", len(key))
	}
	for _, c := range key {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return fmt.Errorf("encryption key must be hexadecimal")
		}
	}
	return nil
}
