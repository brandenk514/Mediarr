package config

import (
	"strings"
	"testing"
	"time"
)

// validKey returns a deterministic 32-byte hex key for tests.
func validKey() string {
	return strings.Repeat("ab", 32)
}

func baseEnv() map[string]string {
	return map[string]string{
		"MEDIARR_POSTGRES_DSN":   "postgres://user:pass@localhost:5432/mediarr?sslmode=disable",
		"MEDIARR_REDIS_URL":      "redis://localhost:6379",
		"MEDIARR_ENCRYPTION_KEY": validKey(),
	}
}

func TestLoad_HappyPath(t *testing.T) {
	cfg, err := Load(baseEnv())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.Postgres == "" || cfg.Redis == "" || cfg.EncryptionKey == "" {
		t.Error("required fields empty")
	}
	if !cfg.AuthPinning {
		t.Error("AuthPinning should default to true")
	}
}

func TestLoad_MissingPostgres(t *testing.T) {
	env := baseEnv()
	delete(env, "MEDIARR_POSTGRES_DSN")
	if _, err := Load(env); err == nil {
		t.Fatal("expected error for missing Postgres DSN")
	}
}

func TestLoad_MissingRedis(t *testing.T) {
	env := baseEnv()
	delete(env, "MEDIARR_REDIS_URL")
	if _, err := Load(env); err == nil {
		t.Fatal("expected error for missing Redis URL")
	}
}

func TestLoad_MissingKey(t *testing.T) {
	env := baseEnv()
	delete(env, "MEDIARR_ENCRYPTION_KEY")
	if _, err := Load(env); err == nil {
		t.Fatal("expected error for missing encryption key")
	}
}

func TestLoad_BadKeyLength(t *testing.T) {
	env := baseEnv()
	env["MEDIARR_ENCRYPTION_KEY"] = "deadbeef" // too short
	if _, err := Load(env); err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestLoad_BadKeyNonHex(t *testing.T) {
	env := baseEnv()
	env["MEDIARR_ENCRYPTION_KEY"] = strings.Repeat("g", 64) // not hex
	if _, err := Load(env); err == nil {
		t.Fatal("expected error for non-hex key")
	}
}

func TestLoad_BadPostgresScheme(t *testing.T) {
	env := baseEnv()
	env["MEDIARR_POSTGRES_DSN"] = "mysql://user@localhost/db"
	if _, err := Load(env); err == nil {
		t.Fatal("expected error for non-postgres scheme")
	}
}

func TestLoad_BadRedisScheme(t *testing.T) {
	env := baseEnv()
	env["MEDIARR_REDIS_URL"] = "http://localhost:6379"
	if _, err := Load(env); err == nil {
		t.Fatal("expected error for non-redis scheme")
	}
}

func TestLoad_DatabaseURLFallback(t *testing.T) {
	env := baseEnv()
	delete(env, "MEDIARR_POSTGRES_DSN")
	env["DATABASE_URL"] = "postgresql://user@localhost:5432/mediarr"
	if _, err := Load(env); err != nil {
		t.Fatalf("DATABASE_URL should be accepted: %v", err)
	}
}

func TestLoad_ExplicitOverrides(t *testing.T) {
	env := baseEnv()
	env["MEDIARR_HTTP_ADDR"] = ":9090"
	env["MEDIARR_READ_TIMEOUT"] = "30"
	env["MEDIARR_AUTH_PINNING"] = "false"
	cfg, err := Load(env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HTTPAddr != ":9090" {
		t.Errorf("HTTPAddr = %q, want :9090", cfg.HTTPAddr)
	}
	if cfg.ReadTimeout != 30 {
		t.Errorf("ReadTimeout = %d, want 30", cfg.ReadTimeout)
	}
	if cfg.AuthPinning {
		t.Error("AuthPinning should be false")
	}
}

func TestLoad_IndexerHealthInterval(t *testing.T) {
	// Unset: defaults to 0 (worker runs a single initial sweep, on-demand
	// tests still work).
	cfg, err := Load(baseEnv())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IndexerHealthInterval != 0 {
		t.Errorf("default IndexerHealthInterval = %v, want 0", cfg.IndexerHealthInterval)
	}

	// A valid duration is honoured.
	env := baseEnv()
	env["MEDIARR_INDEXER_HEALTH_INTERVAL"] = "2m30s"
	cfg, err = Load(env)
	if err != nil {
		t.Fatal(err)
	}
	if want := 150 * time.Second; cfg.IndexerHealthInterval != want {
		t.Errorf("IndexerHealthInterval = %v, want %v", cfg.IndexerHealthInterval, want)
	}

	// A malformed value degrades to 0 rather than failing boot.
	env = baseEnv()
	env["MEDIARR_INDEXER_HEALTH_INTERVAL"] = "not-a-duration"
	cfg, err = Load(env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IndexerHealthInterval != 0 {
		t.Errorf("malformed duration should degrade to 0, got %v", cfg.IndexerHealthInterval)
	}
}
