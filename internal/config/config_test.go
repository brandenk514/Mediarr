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

func TestLoad_DownloadClientDefaultMock(t *testing.T) {
	// Unset: defaults to the mock (no external dependency; M1 behaviour).
	cfg, err := Load(baseEnv())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClientType != "mock" {
		t.Errorf("default ClientType = %q, want mock", cfg.ClientType)
	}
}

func TestLoad_DownloadClientQBittorrent(t *testing.T) {
	env := baseEnv()
	env["MEDIARR_DOWNLOAD_CLIENT"] = "qbittorrent"
	env["MEDIARR_QB_BASE"] = "http://q:8080"
	env["MEDIARR_QB_API_KEY"] = "k3y"
	env["MEDIARR_QB_USERNAME"] = "user"
	env["MEDIARR_QB_PASSWORD"] = "pw"
	cfg, err := Load(env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientType != "qbittorrent" {
		t.Errorf("ClientType = %q, want qbittorrent", cfg.ClientType)
	}
	if cfg.QBittorrent.Base != "http://q:8080" || cfg.QBittorrent.APIKey != "k3y" {
		t.Errorf("QBittorrent = %+v, want base http://q:8080 key k3y", cfg.QBittorrent)
	}
	if cfg.QBittorrent.Username != "user" || cfg.QBittorrent.Password != "pw" {
		t.Errorf("QBittorrent user/pass not forwarded: %+v", cfg.QBittorrent)
	}
}

func TestLoad_DownloadClientSABnzbd(t *testing.T) {
	env := baseEnv()
	env["MEDIARR_DOWNLOAD_CLIENT"] = "sabnzbd"
	env["MEDIARR_SAB_BASE"] = "http://s:8080"
	env["MEDIARR_SAB_API_KEY"] = "sabk"
	cfg, err := Load(env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientType != "sabnzbd" {
		t.Errorf("ClientType = %q, want sabnzbd", cfg.ClientType)
	}
	if cfg.SABnzbd.Base != "http://s:8080" || cfg.SABnzbd.APIKey != "sabk" {
		t.Errorf("SABnzbd = %+v, want base http://s:8080 key sabk", cfg.SABnzbd)
	}
}

func TestLoad_DownloadClientUnknownFailsFast(t *testing.T) {
	// A misconfigured MEDIARR_DOWNLOAD_CLIENT must fail boot, never silently
	// fall back to a mock (which would "download" into the wrong place).
	env := baseEnv()
	env["MEDIARR_DOWNLOAD_CLIENT"] = "deluge"
	if _, err := Load(env); err == nil {
		t.Fatal("expected error for unknown download client, got nil")
	}
}
