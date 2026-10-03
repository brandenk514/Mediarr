// Command mediarr is the entrypoint for the mediarr service.
//
// Usage:
//
//	mediarr              # start the HTTP service
//	mediarr healthcheck  # ping Postgres + Redis, exit 0/1 (container healthcheck)
//
// Startup order:
//  1. Load and validate configuration (fail fast on any error).
//  2. Open Postgres and Redis pools.
//  3. Apply migrations.
//  4. Build the API server and health checker.
//  5. Serve until SIGINT/SIGTERM, then drain in-flight requests.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/brandenk514/mediarr/internal/api"
	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/config"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/postgres"
	"github.com/brandenk514/mediarr/internal/redis"
)

// Build-time stamped values (see -ldflags in Dockerfile / Makefile).
var (
	version   = "dev"
	commit    = "none"
	buildTime = "unknown"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)
	health.SetVersion(version, commit, buildTime)

	if err := run(); err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}
}

// healthcheck verifies Postgres and Redis are reachable. Used as the
// container HEALTHCHECK (works in distroless — no shell required).
// Exit 0 = healthy, 1 = unhealthy.
func healthcheck() int {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))

	cfg, err := config.LoadFromOS()
	if err != nil {
		log.Warn("config load failed", "error", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pg, err := postgres.New(string(cfg.Postgres))
	if err != nil {
		log.Warn("postgres open failed", "error", err)
		return 1
	}
	defer pg.Close()
	if err := pg.Ping(ctx); err != nil {
		log.Warn("postgres ping failed", "error", err)
		return 1
	}

	rdb, err := redis.New(string(cfg.Redis))
	if err != nil {
		log.Warn("redis open failed", "error", err)
		return 1
	}
	defer rdb.Close()
	if err := rdb.RDB().Ping(ctx).Err(); err != nil {
		log.Warn("redis ping failed", "error", err)
		return 1
	}
	return 0
}

func run() error {
	cfg, err := config.LoadFromOS()
	if err != nil {
		return err
	}
	logger := slog.Default()
	logger.Info("config loaded",
		"addr", cfg.HTTPAddr,
		"postgres_host", hostOf(string(cfg.Postgres)),
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// --- Postgres -------------------------------------------------------
	pg, err := postgres.New(string(cfg.Postgres))
	if err != nil {
		return err
	}
	defer pg.Close()

	if err := pg.Migrate(ctx); err != nil {
		return err
	}
	v, _ := pg.Version(ctx)
	logger.Info("postgres ready", "schema_version", v)

	// --- Redis ----------------------------------------------------------
	rdb, err := redis.New(string(cfg.Redis))
	if err != nil {
		return err
	}
	defer rdb.Close()

	cache := redis.NewCache(rdb)
	queue := redis.NewQueue(rdb)
	events := redis.NewEvents(rdb)
	_ = queue  // consumed by workers starting M1
	_ = events // consumed by workers starting M1

	if err := cache.Ping(ctx); err != nil {
		return err
	}
	logger.Info("redis ready")

	// --- Health + API ---------------------------------------------------
	checker := health.NewChecker(pg, cache)
	srv := api.NewServer(checker)
	srv.SetAuth(&api.AuthDeps{Repo: auth.NewRepo(pg.DB())})

	logger.Info("server starting", "addr", cfg.HTTPAddr)
	return srv.Serve(ctx,
		cfg.HTTPAddr,
		time.Duration(cfg.ReadTimeout)*time.Second,
		time.Duration(cfg.WriteTimeout)*time.Second,
		time.Duration(cfg.ShutdownTimeout)*time.Second,
	)
}

// hostOf extracts the host portion of a DSN for logging (never logs creds).
func hostOf(dsn string) string {
	for i := 0; i < len(dsn); i++ {
		if dsn[i] == '/' && i > 0 {
			for j := i + 1; j < len(dsn); j++ {
				if dsn[j] == '/' || dsn[j] == '?' {
					return dsn[i+1 : j]
				}
			}
			return dsn[i+1:]
		}
	}
	return dsn
}
