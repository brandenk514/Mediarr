// Package e2e runs the full movie pipeline end to end against a real Postgres
// and Redis (testcontainers) and a *real* download client — qBittorrent (#36)
// or SABnzbd (#37) — driven over HTTP against an in-process stub of each
// client's Web API. This is the M5 close-out test (#38): it proves the
// config-driven client swap works end to end (add → search → match → client
// add → state poll → import → history), not just per-adapter.
//
// The download client under test is the *real* production adapter
// (internal/downloads QBittorrentClient / SABnzbdClient), constructed through
// the same config-driven factory (downloads.NewClient) that cmd/mediarr uses,
// with config.LoadFromOS() driving the swap. Only the remote qBittorrent /
// SABnzbd server is stubbed (httptest); Postgres and Redis are real
// testcontainers, so the full search → parse → match → download → import path
// runs against real Postgres persistence and a real Redis result cache +
// pub/sub queue bus.
package e2e_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/config"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/indexers"
	"github.com/brandenk514/mediarr/internal/postgres"
	"github.com/brandenk514/mediarr/internal/redis"
	moviesvc "github.com/brandenk514/mediarr/internal/services/movies"
	"github.com/brandenk514/mediarr/internal/store"
	_ "github.com/jackc/pgx/v5/stdlib"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

// testKey is a 64-hex-char (32-byte) AES-256-GCM key for the indexer vault.
// Test-only, not a real credential.
const testKey = "0000000000000000000000000000000000000000000000000000000000000000"

// harness bundles the live Postgres + Redis + repository used by a pipeline run.
type harness struct {
	ctx       context.Context
	dsn       string
	redisURL  string
	db        *sql.DB
	movieRepo *postgres.MovieRepo
	cache     store.Cache
	events    store.Events
}

// startHarness spins up Postgres + Redis containers, opens the pools, runs the
// migrations, and builds the redis Cache + Events the service will use.
func startHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()

	pgC, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("mediarr"),
		tcpostgres.WithUsername("mediarr"),
		tcpostgres.WithPassword("mediarr-test"),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = pgC.Terminate(context.Background()) })
	dsn, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres conn string: %v", err)
	}

	rdC, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("start redis: %v", err)
	}
	t.Cleanup(func() { _ = rdC.Terminate(context.Background()) })
	redisURL, err := rdC.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("redis conn string: %v", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	deadline := time.Now().Add(30 * time.Second)
	for {
		if err := db.PingContext(ctx); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ping postgres: %v", err)
		}
		time.Sleep(250 * time.Millisecond)
	}

	if err := postgres.NewWithDB(db).Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rdb, err := redis.New(redisURL)
	if err != nil {
		t.Fatalf("redis client: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })

	return &harness{
		ctx:       ctx,
		dsn:       dsn,
		redisURL:  redisURL,
		db:        db,
		movieRepo: postgres.NewMovieRepo(db),
		cache:     redis.NewCache(rdb),
		events:    redis.NewEvents(rdb),
	}
}

// setMediaEnv points the production config loader at this run's temp
// media/downloads dirs, the real container DSN/URL, and the selected client
// base + key (the config-driven swap under test).
func (h *harness) setMediaEnv(t *testing.T, clientType, base, apiKey, dlDir, mediaRoot string) {
	t.Helper()
	t.Setenv("MEDIARR_POSTGRES_DSN", h.dsn)
	t.Setenv("MEDIARR_REDIS_URL", h.redisURL)
	t.Setenv("MEDIARR_ENCRYPTION_KEY", testKey)
	t.Setenv("MEDIARR_DOWNLOADS_DIR", dlDir)
	t.Setenv("MEDIARR_MEDIA_ROOT", mediaRoot)
	t.Setenv("MEDIARR_DOWNLOAD_CLIENT", clientType)
	if clientType == "qbittorrent" {
		t.Setenv("MEDIARR_QB_BASE", base)
		t.Setenv("MEDIARR_QB_API_KEY", apiKey)
	} else if clientType == "sabnzbd" {
		t.Setenv("MEDIARR_SAB_BASE", base)
		t.Setenv("MEDIARR_SAB_API_KEY", apiKey)
	}
}

// loadConfig runs the real production config loader (the swap entry point).
func loadConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.LoadFromOS()
	if err != nil {
		t.Fatalf("config.LoadFromOS: %v", err)
	}
	return cfg
}

// clientNameFor runs the same config-driven factory (downloads.NewClient) that
// cmd/mediarr uses and returns the name of the client it built — the
// config-driven swap under test. It builds the client from cfg exactly as the
// production wiring does.
func clientNameFor(t *testing.T, cfg *config.Config) (string, error) {
	t.Helper()
	client, err := downloads.NewClient(downloads.ClientOptions{
		Kind:    downloads.ClientKind(cfg.ClientType),
		MockDir: cfg.DownloadsDir,
		QB: downloads.QBittorrentConfig{
			Base:     cfg.QBittorrent.Base,
			APIKey:   cfg.QBittorrent.APIKey,
			Username: cfg.QBittorrent.Username,
			Password: cfg.QBittorrent.Password,
		},
		SAB: downloads.SABnzbdConfig{
			Base:        cfg.SABnzbd.Base,
			APIKey:      cfg.SABnzbd.APIKey,
			CompleteDir: cfg.DownloadsDir,
		},
	})
	if err != nil {
		return "", err
	}
	return client.Name(), nil
}

// buildMovieService wires the movie service exactly as cmd/mediarr does: the
// config-driven download client from downloads.NewClient, a fake indexer
// wrapped in the shared Redis result cache, and the real redis Events bus.
func (h *harness) buildMovieService(t *testing.T, cfg *config.Config, fake *indexers.FakeIndexer) *moviesvc.Service {
	t.Helper()
	client, err := downloads.NewClient(downloads.ClientOptions{
		Kind:    downloads.ClientKind(cfg.ClientType),
		MockDir: cfg.DownloadsDir,
		QB: downloads.QBittorrentConfig{
			Base:     cfg.QBittorrent.Base,
			APIKey:   cfg.QBittorrent.APIKey,
			Username: cfg.QBittorrent.Username,
			Password: cfg.QBittorrent.Password,
		},
		SAB: downloads.SABnzbdConfig{
			Base:        cfg.SABnzbd.Base,
			APIKey:      cfg.SABnzbd.APIKey,
			CompleteDir: cfg.DownloadsDir,
		},
	})
	if err != nil {
		t.Fatalf("build download client (config-driven swap): %v", err)
	}
	searchers := []indexers.Searcher{indexers.NewCacheSearcher(fake, h.cache, 10*time.Minute)}
	return moviesvc.New(moviesvc.Deps{
		Repo:           h.movieRepo,
		Indexers:       searchers,
		Client:         client,
		MediaRoot:      cfg.MediaRoot,
		DefaultProfile: "HD-1080p",
		DownloadsDir:   cfg.DownloadsDir,
		Events:         h.events,
	})
}

// queueCollector buffers queue events as they arrive on the pub/sub channel.
type queueCollector struct {
	mu     sync.Mutex
	ch     <-chan store.Event
	events []store.Event
}

// startQueueCollector opens a pub/sub subscription for queue.updated and
// returns a collector that buffers the events. Call before the pipeline so no
// event is missed.
func (h *harness) startQueueCollector(t *testing.T) *queueCollector {
	t.Helper()
	subCtx, cancel := context.WithCancel(h.ctx)
	t.Cleanup(cancel)
	ch, err := h.events.Subscribe(subCtx, "queue.updated")
	if err != nil {
		t.Fatalf("subscribe queue events: %v", err)
	}
	return &queueCollector{ch: ch}
}

// collect appends any currently-buffered events to the collector.
func (c *queueCollector) collect() {
	for {
		select {
		case ev, ok := <-c.ch:
			if !ok {
				return
			}
			c.mu.Lock()
			c.events = append(c.events, ev)
			c.mu.Unlock()
		default:
			return
		}
	}
}

// stateSequence decodes each collected queue event and returns its state field,
// in the order observed. The bus publishes store.Event, whose Payload is a
// base64-encoded JSON body (json.Marshal(Event) in the redis client), so we
// decode that envelope before reading the inner "state" field.
func (c *queueCollector) stateSequence() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var seq []string
	for _, ev := range c.events {
		if ev.Type != "queue.updated" {
			continue
		}
		body := ev.Payload
		if dec, err := base64.StdEncoding.DecodeString(string(body)); err == nil {
			body = dec
		}
		var p struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			continue
		}
		seq = append(seq, p.State)
	}
	return seq
}

// runPipeline drives add → pipeline and returns the movie id and import path.
func (h *harness) runPipeline(t *testing.T, svc *moviesvc.Service, title, year string) (int64, string) {
	t.Helper()
	id, err := svc.AddMovie(h.ctx, title, year, "HD-1080p")
	if err != nil {
		t.Fatalf("add movie: %v", err)
	}
	imported, err := svc.RunPipeline(h.ctx, id)
	if err != nil {
		t.Fatalf("run pipeline: %v", err)
	}
	if imported == "" {
		t.Fatal("pipeline returned empty import path")
	}
	return id, imported
}

// assertImported verifies the imported file exists on disk at the expected
// <mediaRoot>/<Title (Year)>/<Title (Year)>.mkv location.
func assertImported(t *testing.T, imported, mediaRoot, title string, year int) {
	t.Helper()
	if _, err := os.Stat(imported); err != nil {
		t.Fatalf("imported file not on disk: %v", err)
	}
	name := fmt.Sprintf("%s (%d)", title, year)
	want := filepath.Join(mediaRoot, name, name+".mkv")
	if imported != want {
		t.Errorf("imported = %q, want %q", imported, want)
	}
}

// containsInOrder reports whether seq contains want as an in-order subsequence
// (other values may appear between them). Used to assert the queue's pub/sub
// state transitions occurred in order.
func containsInOrder(seq, want []string) bool {
	wi := 0
	for _, s := range seq {
		if wi < len(want) && s == want[wi] {
			wi++
		}
	}
	return wi == len(want)
}

// assertHistory verifies the pipeline recorded lifecycle history on Postgres.
func assertHistory(t *testing.T, h *harness, movieID int64) {
	t.Helper()
	hist, err := h.movieRepo.ListHistory(h.ctx, 100)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(hist) == 0 {
		t.Fatal("expected history entries, got none")
	}
	var sawAdded, sawImported bool
	for _, e := range hist {
		if e.MovieID != movieID {
			continue
		}
		if e.Event == "added" {
			sawAdded = true
		}
		if e.Event == "imported" {
			sawImported = true
		}
	}
	if !sawAdded {
		t.Error("history missing 'added' event")
	}
	if !sawImported {
		t.Error("history missing 'imported' event")
	}
}
