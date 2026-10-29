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
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/brandenk514/mediarr/internal/api"
	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/config"
	moviedom "github.com/brandenk514/mediarr/internal/domains/movies"
	music "github.com/brandenk514/mediarr/internal/domains/music"
	rfdom "github.com/brandenk514/mediarr/internal/domains/rootfolders"
	tvd "github.com/brandenk514/mediarr/internal/domains/tv"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/indexers"
	indexerdefinitions "github.com/brandenk514/mediarr/internal/indexers/definitions"
	"github.com/brandenk514/mediarr/internal/metawrite"
	"github.com/brandenk514/mediarr/internal/metrics"
	"github.com/brandenk514/mediarr/internal/postgres"
	"github.com/brandenk514/mediarr/internal/providers"
	"github.com/brandenk514/mediarr/internal/redis"
	"github.com/brandenk514/mediarr/internal/secrets"
	blocklistsvc "github.com/brandenk514/mediarr/internal/services/blocklist"
	bookssvc "github.com/brandenk514/mediarr/internal/services/books"
	calendarsvc "github.com/brandenk514/mediarr/internal/services/calendar"
	historysvc "github.com/brandenk514/mediarr/internal/services/historyfeed"
	moviesvc "github.com/brandenk514/mediarr/internal/services/movies"
	musicsvc "github.com/brandenk514/mediarr/internal/services/music"
	rootfolders "github.com/brandenk514/mediarr/internal/services/rootfolders"
	tvs "github.com/brandenk514/mediarr/internal/services/tv"
	wantedsvc "github.com/brandenk514/mediarr/internal/services/wanted"
	"github.com/brandenk514/mediarr/internal/settings"
	"github.com/brandenk514/mediarr/internal/system"
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

	// P6: the Log Files page (System > Log Files) shows the real application
	// logs: the default JSON handler is wrapped in a slog bridge that mirrors
	// every record into the system log ring buffer.
	logSvc := system.NewLogService(1000)
	logger := slog.New(system.NewSlogBridge(
		slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}),
		logSvc,
	))
	slog.SetDefault(logger)
	health.SetVersion(version, commit, buildTime)

	if err := run(logSvc); err != nil {
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
	defer func() { _ = pg.Close() }() // teardown: shutdown path
	if err := pg.Ping(ctx); err != nil {
		log.Warn("postgres ping failed", "error", err)
		return 1
	}

	rdb, err := redis.New(string(cfg.Redis))
	if err != nil {
		log.Warn("redis open failed", "error", err)
		return 1
	}
	defer func() { _ = rdb.Close() }() // teardown: shutdown path
	if err := rdb.RDB().Ping(ctx).Err(); err != nil {
		log.Warn("redis ping failed", "error", err)
		return 1
	}
	return 0
}

func run(logSvc *system.LogService) error {
	cfg, err := config.LoadFromOS()
	if err != nil {
		return err
	}
	logger := slog.Default()
	logger.Info("config loaded",
		"addr", cfg.HTTPAddr,
		"postgres_host", hostOf(string(cfg.Postgres)),
	)

	// P6: the process start time for the About uptime readout.
	startedAt := time.Now()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// --- Postgres -------------------------------------------------------
	pg, err := postgres.New(string(cfg.Postgres))
	if err != nil {
		return err
	}
	defer func() { _ = pg.Close() }() // teardown: shutdown path

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
	defer func() { _ = rdb.Close() }() // teardown: shutdown path

	cache := redis.NewCache(rdb)
	queue := redis.NewQueue(rdb)
	events := redis.NewEvents(rdb)
	_ = queue // consumed by workers starting M1
	// events is the live pub/sub bus: fed by all four pipelines (queue.updated
	// / history.appended) and read by the /api/v1/events WebSocket bridge.

	if err := cache.Ping(ctx); err != nil {
		return err
	}
	logger.Info("redis ready")

	// --- Metrics (M7 #46) --------------------------------------------
	// The scrape-time collector reads queue depth / library sizes straight
	// from Postgres and re-pings dependencies during the scrape, so it needs
	// the pool and the liveness checker. /metrics itself is auth-gated by
	// the API layer; this layer only owns the registry and recording.
	checker := health.NewChecker(pg, cache)
	m := metrics.New(metrics.Options{
		DB:      pg.DB(),
		Checker: checker,
		Build:   metrics.BuildInfo{Version: version, Commit: commit, BuildTime: buildTime},
	})

	// --- Indexer layer (M5, #30) --------------------------------------
	// The Prowlarr-role framework composes here, once, and is shared by all four
	// media pipelines:
	//
	//   vault        AES-256-GCM at-rest encryption for indexer API keys (PLAN §7).
	//   indexerRepo  the Postgres definition store (migration 0007) — the only
	//                layer that encrypts/decrypts keys.
	//   registry     maps a protocol kind -> Provider constructor. The fake is the
	//                reference Provider; real adapters (#31-33) register here.
	//   result cache every materialized indexer is wrapped in a CacheSearcher so
	//                its search results live in Redis (PLAN §5, TTL 10min).
	//
	// When the store is empty (a fresh install) there are no *configured*
	// indexers; the per-domain demo fakes below remain the dev path (the same
	// mock download client / fake indexer model as M1-M4).
	vault, err := secrets.NewVault(cfg.EncryptionKey)
	if err != nil {
		return fmt.Errorf("build indexer vault: %w", err)
	}
	indexerRepo := postgres.NewIndexersRepo(pg.DB(), vault)

	// Prowlarr PR2: the built-in indexer definition library. Seed the table
	// from the embedded Prowlarr community set on first boot (idempotent);
	// every row records the seed revision so the definition-update task
	// (PR6) can detect stale rows and re-upsert them.
	definitionsCatalog, err := indexerdefinitions.Load()
	if err != nil {
		return fmt.Errorf("load indexer definition seed: %w", err)
	}
	definitionsRepo := postgres.NewIndexerDefinitionsRepo(pg.DB())
	if n, err := definitionsRepo.SeedIfEmpty(ctx, definitionsCatalog.Definitions, definitionsCatalog.Revision); err != nil {
		return fmt.Errorf("seed indexer definitions: %w", err)
	} else if n > 0 {
		logger.Info("indexer definition library seeded", "count", n, "revision", definitionsCatalog.Revision)
	}

	// The shared health tracker is the single source of truth for per-indexer
	// stats (#34/#35): the live search adapters, the periodic worker, and the
	// on-demand Test probe all record into it, so the stats endpoint reflects
	// real traffic. It is handed to the registry (for every built adapter) and
	// to the health worker (for its sweep + on-demand probes).
	healthTracker := indexers.NewHealthTracker()
	// Mirror every indexer call into Prometheus (M7 #46) while the inner
	// tracker keeps the stats rollup the status API reads.
	healthTracker = metrics.NewIndexerRecorder(healthTracker, m)
	registry := indexers.NewDefaultRegistry()
	registry.Deps = indexers.AdapterDeps{Health: healthTracker}

	// The health worker (#34) periodically probes every enabled indexer and
	// supports on-demand probes. A non-positive interval runs a single
	// initial sweep and stops (on-demand tests still work); otherwise it runs
	// until the process context is cancelled.
	//
	// P6: the sweep is driven by the System task registry instead of its own
	// goroutine, so the Tasks page can observe it (one loop, live running /
	// last-run state) without reinventing the worker.
	healthWorker := indexers.NewHealthWorker(indexerRepo, registry, registry.Deps, cfg.IndexerHealthInterval, logger)
	taskRegistry := system.NewTaskRegistry()
	taskRegistry.Register(system.TaskSpec{
		Name:        "indexer_health",
		Description: "Probe every enabled indexer and record the rollup",
		Kind:        system.TaskScheduled,
		Interval:    cfg.IndexerHealthInterval,
		Enabled:     true,
		Run: func(ctx context.Context) error {
			if err := healthWorker.TestAll(ctx); err != nil {
				logger.Warn("indexer health sweep failed", "error", err)
				return err
			}
			return nil
		},
	})
	// The PLAN.md §3 worker roster: the rest are planned (listed for the
	// Tasks page inventory; not yet implemented as supervised loops).
	for _, t := range []system.TaskSpec{
		{Name: "wanted_scan", Description: "Wanted/monitor scan (periodic + event-driven)", Kind: system.TaskPlanned},
		{Name: "indexer_search_refresh", Description: "Indexer search + result refresh", Kind: system.TaskPlanned},
		{Name: "download_queue_watch", Description: "Download queue watch → import trigger", Kind: system.TaskPlanned},
		{Name: "import", Description: "Import (parse client state, rename, move, metadata)", Kind: system.TaskPlanned},
		{Name: "metadata_refresh", Description: "Metadata refresh (per-item + global schedule)", Kind: system.TaskPlanned},
		{Name: "housekeeping", Description: "Housekeeping (history purge, cache eviction, temp cleanup)", Kind: system.TaskPlanned},
	} {
		taskRegistry.Register(t)
	}
	if cfg.IndexerHealthInterval > 0 {
		// Drives the initial sweep + the periodic ticks (matches Run).
		taskRegistry.Start(ctx, "indexer_health")
	} else {
		// Single initial sweep, matching the previous Run behavior.
		go func() { _ = healthWorker.TestAll(ctx) }()
	}

	const resultTTL = 10 * time.Minute

	// buildConfigured materializes every enabled definition into a cached
	// Searcher. A definition that references an unregistered kind is skipped
	// (and logged) rather than failing boot: a persisted indexer the running
	// build can't construct should degrade, not crash.
	buildConfigured := func(ctx context.Context) []indexers.Searcher {
		defs, err := indexerRepo.ListEnabled(ctx)
		if err != nil {
			logger.Warn("load indexer definitions", "error", err)
			return nil
		}
		var out []indexers.Searcher
		for _, def := range defs {
			p, err := registry.Build(def)
			if err != nil {
				logger.Warn("skip indexer definition", "name", def.Name, "kind", def.Kind, "error", err)
				continue
			}
			out = append(out, indexers.NewCacheSearcher(p, cache, resultTTL))
		}
		return out
	}
	configured := buildConfigured(ctx)
	if len(configured) > 0 {
		logger.Info("indexers ready",
			"configured", len(configured),
			"kinds", registry.Kinds(),
			"cache", "redis", "ttl", resultTTL.String())
	}

	// indexersFor composes the shared configured set with a per-domain dev fake,
	// wrapped in the same Redis result cache so the cache path is exercised on
	// the reference/dev composition too. It always returns a fresh slice so the
	// shared `configured` backing array is never aliased.
	indexersFor := func(dev indexers.Searcher) []indexers.Searcher {
		out := make([]indexers.Searcher, 0, len(configured)+1)
		out = append(out, configured...)
		out = append(out, indexers.NewCacheSearcher(dev, cache, resultTTL))
		return out
	}

	// --- Health + API ---------------------------------------------------
	srv := api.NewServer(checker)
	// Prometheus scrape endpoint (M7 #46), auth-gated like the rest of the
	// API (single-user v1).
	srv.SetMetrics(&api.MetricsDeps{M: m})
	// Indexer endpoints (#35 read/health, #44 write): list, on-demand test,
	// per-indexer stats, plus the create/update/enable/delete surface the
	// settings page uses. They share the same repo, worker, tracker, and
	// registry as the live search path (the registry validates a definition's
	// kind so a saved indexer can always be materialized).
	srv.SetIndexers(&api.IndexersDeps{
		Repo:        indexerRepo,
		Worker:      healthWorker,
		Tracker:     healthTracker,
		Admin:       registry,
		Definitions: definitionsRepo,
	})
	// Dashboard (#40): cross-library snapshot + indexer health at a glance,
	// and the WebSocket push channel for live queue/history updates.
	dashboardRepo := postgres.NewDashboardRepo(pg.DB())
	srv.SetDashboard(&api.DashboardDeps{
		Repo:    dashboardRepo,
		Tracker: healthTracker,
	})
	// Queue view (#43): the full filterable/paginated queue + per-parent
	// history, driven live by the same WebSocket push channel as the dashboard.
	queueRepo := postgres.NewQueueRepo(pg.DB())
	srv.SetQueue(&api.QueueDeps{Repo: queueRepo})
	// Settings (#44): the persistent download-client config + per-library
	// overrides over the settings table. The Postgres store is the layer that
	// encrypts download-client credentials at rest with the same app vault the
	// indexer repo uses (PLAN §7).
	settingsRepo := postgres.NewSettingsRepo(pg.DB(), vault)
	srv.SetSettings(&api.SettingsDeps{Store: settingsRepo})
	// P5 settings shell: generic per-section documents + tag usage.
	srv.SetSettingsSections(&api.SettingsSectionDeps{
		Store:    settingsRepo,
		TagUsage: postgres.NewTagUsageRepo(pg.DB()),
	})
	// Prowlarr PR4: saved notification sinks (token vault-encrypted at rest)
	// and indexer proxies — FlareSolverr instances (password vault-encrypted
	// at rest), each with its own CRUD + test endpoint.
	srv.SetNotifications(&api.NotificationsDeps{
		Repo: postgres.NewNotificationsRepo(pg.DB(), vault),
	})
	srv.SetIndexerProxies(&api.IndexerProxiesDeps{
		Repo: postgres.NewIndexerProxiesRepo(pg.DB(), vault),
	})
	authRepo := auth.NewRepo(pg.DB())
	srv.SetAuth(&api.AuthDeps{Repo: authRepo})
	srv.SetEvents(&api.EventsDeps{Repo: authRepo, Events: events})

	// --- Movies pipeline -----------------------------------------------
	// M1: wire the movie pipeline end-to-end. The running service uses a fake
	// indexer + mock download client to prove the architecture; real indexer
	// and download-client adapters (qBittorrent/SABnzbd, TorrentRSS/Torznab)
	// replace them in M5. The mock client writes into cfg.DownloadsDir and
	// import moves files under cfg.MediaRoot.
	//
	// The movie endpoints (add/list/get) only need the repository, so they are
	// ALWAYS registered. The download client is optional: when it can't be
	// created (e.g. the downloads dir isn't writable), the movie routes still
	// work and only the pipeline reports that no download client is available.
	fake := indexers.NewFakeIndexer("fake")
	// Dev-only demo pool: the live fake indexer is empty by default (so tests
	// register exactly what they assert). Seed a few plausible releases so a
	// running dev server's "search → match" actually finds candidates for a
	// handful of popular titles, letting the full download+import path be
	// exercised via the API. This does not affect tests, which build their own.
	fake.AddRelease(indexers.SearchResult{Title: "Inception.2010.1080p.WEB.x264", SizeBytes: 4_300_000_000})
	fake.AddRelease(indexers.SearchResult{Title: "Inception.2010.2160p.WEB-DL.x265", SizeBytes: 9_800_000_000})
	fake.AddRelease(indexers.SearchResult{Title: "Dune.Part.Two.2024.1080p.WEB.x264", SizeBytes: 5_100_000_000})
	fake.AddRelease(indexers.SearchResult{Title: "Dune.Part.Two.2024.2160p.WEB-DL.x265", SizeBytes: 11_400_000_000})
	fake.AddRelease(indexers.SearchResult{Title: "Interstellar.2014.1080p.WEB.x264", SizeBytes: 4_700_000_000})
	fake.AddRelease(indexers.SearchResult{Title: "Tenet.2020.1080p.WEB.x264", SizeBytes: 4_900_000_000})
	// Download client (#36/#37): config-driven swap. "mock" (default) keeps
	// the M1 behaviour (in-process, no external dependency); "qbittorrent" /
	// "sabnzbd" build the real adapter. All four media pipelines share this
	// one client. Init failure leaves dlClient nil (movie pipeline disabled,
	// logged) — matching the previous mock-failure behaviour.
	var dlClient downloads.Client
	if c, clientErr := downloads.NewClient(downloads.ClientOptions{
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
	}); clientErr == nil {
		dlClient = c
	} else {
		logger.Warn("download client init failed; movie pipeline disabled", "error", clientErr)
	}
	// Prowlarr PR3: manual search, grab, query history, and the stats summary.
	// The fan-out reuses the same indexer repo + registry as the hub (and the
	// live search path); the activity store is migration 0022; a grab routes by
	// protocol to the configured download client (nil client → 503, unhandled
	// protocol → 409).
	srv.SetIndexerSearch(&api.IndexerSearchDeps{
		Indexers:   indexerRepo,
		Registry:   registry,
		Activity:   postgres.NewSearchActivityRepo(pg.DB()),
		Client:     dlClient,
		ClientKind: downloads.ClientKind(cfg.ClientType),
	})
	tmdbClient := providers.NewTMDB(cfg.Providers.TMDBAPIKey)
	movieSvc := moviesvc.New(moviesvc.Deps{
		Repo:           postgres.NewMovieRepo(pg.DB()),
		Indexers:       indexersFor(fake),
		Client:         dlClient,
		MediaRoot:      cfg.MediaRoot,
		DefaultProfile: "HD-1080p",
		DownloadsDir:   cfg.DownloadsDir,
		Events:         events,
		Metrics:        m,
		// Radarr R1: display enrichment on the detail read. Keyless (empty
		// TMDB_API_KEY) degrades gracefully: the enricher returns nil and
		// the UI shows em-dashes.
		Enricher: &moviesvc.TMDBEnricher{Client: tmdbClient},
		// Radarr R3: collection parts for the collections page's missing
		// counts. Keyless: GetCollectionParts returns ErrNotConfigured,
		// which the service treats as "no data" (missing_count 0).
		CollectionParts: tmdbClient,
		// Radarr R3: the Discover page's trending feed. Keyless: Discover
		// answers 503 (same shape as /search/movies).
		TMDB: tmdbClient,
	})
	srv.SetMovies(&api.MoviesDeps{Svc: movieSvc})
	srv.SetDiscover(&api.DiscoverDeps{Svc: movieSvc})
	logger.Info("movie pipeline ready",
		"media_root", cfg.MediaRoot,
		"downloads_dir", cfg.DownloadsDir,
		"indexers", "fake",
		"client", func() string {
			if dlClient != nil {
				return dlClient.Name()
			}
			return "none"
		}())

	// --- TV pipeline ---------------------------------------------------
	// M2: wire the TV pipeline end-to-end, mirroring the movie pipeline. The
	// TV service shares the same fake indexer + mock download client so a
	// running dev server can exercise the full add → want → search → match →
	// download → import path for series/episodes. A TV media root defaults to
	// <MediaRoot>/tv so the two domains land in separate trees.
	// Root-folder registry (P2 parity): on-disk media roots consumed by the
	// Add-New flow and Library Import.
	rfRepo := postgres.NewRootFoldersRepo(pg.DB())
	rfSvc := rootfolders.New(rfRepo)
	srv.SetRootFolders(&api.RootFolderDeps{Svc: rfSvc})
	tvFake := indexers.NewFakeIndexer("fake")
	// Dev-only demo pool (mirrors the movie pool): a few plausible TV release
	// names so a live "search" returns candidates for a handful of series.
	tvFake.AddRelease(indexers.SearchResult{Title: "Breaking.Bad.S01E01.Piloto.1080p.WEB.x264", SizeBytes: 1_900_000_000})
	tvFake.AddRelease(indexers.SearchResult{Title: "Breaking.Bad.S01E02.1080p.WEB.x264", SizeBytes: 1_800_000_000})
	tvFake.AddRelease(indexers.SearchResult{Title: "Breaking.Bad.S01.COMPLETE.1080p.WEB.x264", SizeBytes: 8_400_000_000})
	tvFake.AddRelease(indexers.SearchResult{Title: "The.Wire.S01E01.1080p.WEB.x264", SizeBytes: 1_700_000_000})
	tvFake.AddRelease(indexers.SearchResult{Title: "The.Wire.S01E01E02.1080p.WEB.x264", SizeBytes: 3_300_000_000})

	tvMediaRoot := tvMediaRoot(cfg.MediaRoot)
	tvRepo := postgres.NewTVRepo(pg.DB())
	tvSvc := tvs.New(tvs.Deps{
		Repo:           tvRepo,
		Indexers:       indexersFor(tvFake),
		Client:         dlClient,
		MediaRoot:      tvMediaRoot,
		DefaultProfile: "HD-1080p",
		Events:         events,
		Metrics:        m,
	})
	srv.SetTV(&api.TVDeps{Svc: tvSvc, RootFolders: rfSvc})
	logger.Info("tv pipeline ready",
		"media_root", tvMediaRoot,
		"indexers", "fake",
		"client", func() string {
			if dlClient != nil {
				return "mock"
			}
			return "none"
		}())

	// --- Activity + Wanted (P3 parity) --------------------------------
	// The global Activity pages (History feed + Blocklist) read the
	// per-kind history tables and the blocklist table through their own
	// repositories. The global Wanted view + its actions (Search All,
	// Unmonitor Selected, Manual Import) operate on the TV tables, so the
	// wanted service is wired here, after the TV pipeline, sharing the
	// TV repo for reads.
	historyFeedSvc := historysvc.New(postgres.NewHistoryRepo(pg.DB()))
	srv.SetHistory(&api.HistoryDeps{Svc: historyFeedSvc})
	srv.SetBlocklist(&api.BlocklistDeps{Svc: blocklistsvc.New(postgres.NewBlocklistRepo(pg.DB()))})
	wantedRepo := postgres.NewWantedRepo(pg.DB())
	wantedSvc := wantedsvc.New(wantedRepo, tvMediaRoot)
	srv.SetWanted(&api.WantedDeps{Svc: wantedSvc})
	// The manual-import endpoint also serves kind=music (L4), so it is
	// wired after the music pipeline below (both services in one call).

	// P4: Calendar — upcoming monitored TV air dates (calendar page + iCal feed).
	calendarRepo := postgres.NewCalendarRepo(pg.DB())
	calendarSvc := calendarsvc.New(calendarRepo)
	srv.SetCalendar(&api.CalendarDeps{Svc: calendarSvc})

	// --- Music pipeline --------------------------------------------
	// M3: wire the music pipeline end-to-end, mirroring the TV pipeline. The
	// music service shares the same fake indexer + mock download client so a
	// running dev server can exercise the full add → want → search → match →
	// download → import path for artists/albums/tracks. A music media root
	// defaults to <MediaRoot>/music so the three domains land in separate
	// trees.
	musicFake := indexers.NewFakeIndexer("fake")
	// Dev-only demo pool (mirrors the movie/TV pools): a few plausible music
	// release names so a live "search" returns candidates for a handful of
	// artists/albums.
	musicFake.AddRelease(indexers.SearchResult{Title: "Bob Marley (1977) - Legend [FLAC 998kbps Lossless]", SizeBytes: 4_200_000_000})
	musicFake.AddRelease(indexers.SearchResult{Title: "The Beatles (1968) - The White Album [FLAC Lossless]", SizeBytes: 6_100_000_000})
	musicFake.AddRelease(indexers.SearchResult{Title: "Daft Punk (2001) - Discovery [FLAC 1411kbps Lossless]", SizeBytes: 5_800_000_000})
	musicFake.AddRelease(indexers.SearchResult{Title: "Nirvana (1991) - Nevermind [MP3 320kbps]", SizeBytes: 1_100_000_000})

	musicMediaRoot := musicMediaRoot(cfg.MediaRoot)
	musicSvc := musicsvc.New(musicsvc.Deps{
		Repo:           postgres.NewMusicRepo(pg.DB()),
		Indexers:       indexersFor(musicFake),
		Client:         dlClient,
		MediaRoot:      musicMediaRoot,
		DefaultProfile: "Lossless",
		Events:         events,
		Metrics:        m,
		// L4 (Lidarr parity): keyless MusicBrainz + Cover Art Archive metadata
		// adapter for album/track enrichment and the artist refresh endpoint.
		Metadata: providers.NewMusicBrainz(),
	})
	srv.SetMusic(&api.MusicDeps{Svc: musicSvc, RootFolders: rfSvc})
	// L4: the manual-import endpoint (kind=tv + kind=music) needs both the
	// wanted service and the music service — wired now that the music
	// pipeline exists above.
	srv.SetImport(&api.ImportDeps{Wanted: wantedSvc, Music: musicSvc})
	logger.Info("music pipeline ready",
		"media_root", musicMediaRoot,
		"indexers", "fake",
		"client", func() string {
			if dlClient != nil {
				return "mock"
			}
			return "none"
		}())

	// Books pipeline (M4): a books media root defaults to <MediaRoot>/books so
	// the four domains land in separate trees. A dev-only fake indexer pool
	// (mirrors the movie/TV/music pools) lets a running dev server exercise the
	// full add author → add title/edition → want → search → format match →
	// download → import path.
	booksFake := indexers.NewFakeIndexer("fake")
	booksFake.AddRelease(indexers.SearchResult{Title: "Ursula K. Le Guin - The Dispossessed [EPUB]", SizeBytes: 1_100_000})
	booksFake.AddRelease(indexers.SearchResult{Title: "Brandon Sanderson - Mistborn [AZW3]", SizeBytes: 1_300_000})
	booksFake.AddRelease(indexers.SearchResult{Title: "Aldous Huxley - Brave New World [MOBI]", SizeBytes: 900_000})

	booksMediaRoot := booksMediaRoot(cfg.MediaRoot)
	booksSvc := bookssvc.New(bookssvc.Deps{
		Repo:           postgres.NewBooksRepo(pg.DB()),
		Indexers:       indexersFor(booksFake),
		Client:         dlClient,
		MediaRoot:      booksMediaRoot,
		DefaultProfile: "Best",
		Metrics:        m,
	})
	srv.SetBooks(&api.BooksDeps{Svc: booksSvc})
	logger.Info("books pipeline ready",
		"media_root", booksMediaRoot,
		"indexers", "fake",
		"client", func() string {
			if dlClient != nil {
				return "mock"
			}
			return "none"
		}())

	// Add-flow provider search (M6 #42). Every adapter is optional: a
	// missing key leaves the adapter nil and the endpoint answers 503,
	// which the UI surfaces inline. MusicBrainz and Open Library are
	// keyless, so they are always available.
	srv.SetSearch(&api.SearchDeps{
		TMDB:        providers.NewTMDB(cfg.Providers.TMDBAPIKey),
		TVDB:        providers.NewTVDB(cfg.Providers.TVDBAPIKey),
		MusicBrainz: providers.NewMusicBrainz(),
		OpenLibrary: providers.NewOpenLibrary(),
	})
	if cfg.Providers.TMDBAPIKey == "" || cfg.Providers.TVDBAPIKey == "" {
		logger.Warn("metadata providers partially configured",
			"tmdb_key_set", cfg.Providers.TMDBAPIKey != "",
			"tvdb_key_set", cfg.Providers.TVDBAPIKey != "")
	}

	// P5 (waves 2/3): the settings extension endpoints — custom-format
	// scoring, the connect test dispatch, the metadata-source fallback
	// search (P5.7), and import lists. The keyless adapters (TVmaze,
	// Wikidata) make a zero-key install fully searchable.
	srv.SetSettingsP5(&api.SettingsP5Deps{
		Store:       settingsRepo,
		TVDB:        providers.NewTVDB(cfg.Providers.TVDBAPIKey),
		TMDB:        providers.NewTMDB(cfg.Providers.TMDBAPIKey),
		TVmaze:      providers.NewTVmaze(),
		Wikidata:    providers.NewWikidata(),
		MusicBrainz: providers.NewMusicBrainz(),
		OpenLibrary: providers.NewOpenLibrary(),
		TV:          tvSvc,
		Movies:      movieSvc,
	})

	// P6 (System): the System area — structured health, the worker-inventory
	// task registry, disk usage for the configured roots, the About report,
	// backup export/import, the event log, the changelog, the streaming log
	// viewer, and the Sonarr drop-in migration.
	{
		probes := []system.Probe{
			{Name: "postgres", Ping: func(ctx context.Context) error { return pg.Ping(ctx) }, WikiLink: "docs/sonarr-parity-plan.md#p6"},
			{Name: "redis", Ping: func(ctx context.Context) error { return cache.Ping(ctx) }, WikiLink: "docs/sonarr-parity-plan.md#p6"},
			{Name: "media_root", WikiLink: "docs/sonarr-parity-plan.md#p6", Static: func() (bool, string) {
				if _, err := os.Stat(cfg.MediaRoot); err != nil {
					return false, "media root " + cfg.MediaRoot + " is missing or unreadable"
				}
				return true, cfg.MediaRoot
			}},
			{Name: "downloads_dir", WikiLink: "docs/sonarr-parity-plan.md#p6", Static: func() (bool, string) {
				if _, err := os.Stat(cfg.DownloadsDir); err != nil {
					return false, "downloads dir " + cfg.DownloadsDir + " is missing or unreadable"
				}
				return true, cfg.DownloadsDir
			}},
			{Name: "encryption_key", WikiLink: "docs/sonarr-parity-plan.md#p6", Static: func() (bool, string) {
				if cfg.EncryptionKey == "" {
					return false, "no at-rest encryption key (indexer/API secrets stored in plaintext)"
				}
				return true, "AES-256-GCM key configured"
			}},
			{Name: "tmdb", Soft: true, Static: func() (bool, string) {
				if cfg.Providers.TMDBAPIKey == "" {
					return false, "TMDB API key not set (search uses keyless fallbacks)"
				}
				return true, "TMDB API key set"
			}},
			{Name: "tvdb", Soft: true, Static: func() (bool, string) {
				if cfg.Providers.TVDBAPIKey == "" {
					return false, "TVDB API key not set (search uses keyless fallbacks)"
				}
				return true, "TVDB API key set"
			}},
		}
		mode := os.Getenv("MEDIARR_MODE")
		if mode == "" {
			mode = "production"
		}
		// P6 Wave 3: the Sonarr drop-in migration stores. The target maps the
		// six entity classes onto the real Mediarr stores: profiles / tags /
		// download client over the settings repo, indexers over the indexer
		// repo, series + episodes over the TV repo. The source is the live
		// Sonarr v3 API (admin-configured URL + API key).
		migrationIndexers := &migrationIndexerStore{repo: indexerRepo}
		migrationTV := &migrationTVStore{repo: tvRepo}

		// Radarr R6: the Radarr source of the drop-in migration. The target
		// maps movies + editions onto the movie domain (title+year identity,
		// like the R5 import bridge), Radarr v6 quality profiles onto the
		// profiles section's movie_profiles array, and tags onto the tags
		// section. The source is the live Radarr v3 API (admin-configured URL
		// + API key).
		migrationRadarrIndexers := &migrationRadarrIndexerStore{repo: indexerRepo}
		migrationRadarrMovies := &migrationRadarrMovieStore{repo: postgres.NewMovieRepo(pg.DB())}
		radarrTarget := system.NewRadarrPostgresTarget(settingsRepo, migrationRadarrMovies, migrationRadarrIndexers)

		// Lidarr L5: the Lidarr source of the drop-in migration. The target
		// maps artists/albums/tracks onto the music domain, the music
		// quality + metadata profiles + quality ladder onto the seeded
		// profile/quality sections, tags onto the tags section, root folders
		// (with derived per-root default profiles) onto the registry,
		// indexers onto the indexer store, and the download client +
		// media-management subset onto the settings documents.
		migrationLidarrMusic := &migrationLidarrMusicStore{repo: postgres.NewMusicRepo(pg.DB())}
		migrationLidarrRoots := &migrationLidarrRootFolderStore{svc: rfSvc}
		lidarrTarget := system.NewLidarrPostgresTarget(settingsRepo, migrationLidarrMusic, migrationLidarrRoots, migrationRadarrIndexers)

		// P6 Wave 2: the System depth services. Backup exports/imports the
		// configuration over the settings repo (queue / series snapshots are
		// optional attachments; the series snapshot can be imported back
		// idempotently). The Events page is the global history feed, the
		// Updates page the release changelog, and Log Files the log service
		// bridged into the app logger in main().
		backupSvc := &system.BackupService{
			Store:   settingsRepo,
			Version: version,
			QueueSnap: func(ctx context.Context) (json.RawMessage, error) {
				items, err := queueRepo.List(ctx)
				if err != nil {
					return nil, err
				}
				return json.Marshal(items)
			},
			SeriesSnap: func(ctx context.Context) (json.RawMessage, error) {
				series, err := tvRepo.ListSeries(ctx)
				if err != nil {
					return nil, err
				}
				return json.Marshal(series)
			},
			SeriesImport: func(ctx context.Context, raw json.RawMessage) (int, error) {
				var series []tvd.Series
				if err := json.Unmarshal(raw, &series); err != nil {
					return 0, err
				}
				n := 0
				for _, sr := range series {
					if sr.Title == "" {
						continue
					}
					if _, found := migrationTV.FindSeries(ctx, sr.TVDBID, sr.Title, sr.Year); found {
						continue
					}
					if _, err := migrationTV.AddSeries(ctx, system.SeriesRef{
						Title:          sr.Title,
						Year:           sr.Year,
						TVDBID:         sr.TVDBID,
						QualityProfile: sr.QualityProfile,
						Monitored:      sr.Monitored,
						SeasonFolder:   sr.SeasonFolder,
						Tags:           sr.Tags,
					}); err != nil {
						return n, err
					}
					n++
				}
				return n, nil
			},
		}
		// P6 Wave 3 / Radarr R6 / Lidarr L5: the migrators (Sonarr: source
		// factory = the live Sonarr v3 client, target = the real Mediarr
		// import sink; Radarr: the live Radarr v3 client into the movie
		// domain; Lidarr: the live Lidarr v3 client into the music domain).
		// All three are routed through MigrationService by the request's
		// source.
		migrator := system.NewMigrator(system.NewPostgresTarget(settingsRepo, migrationIndexers, migrationTV))
		radarrMigrator := system.NewRadarrMigrator(radarrTarget)
		lidarrMigrator := system.NewLidarrMigrator(lidarrTarget)
		migrationSvc := system.NewMigrationService(migrator, radarrMigrator, lidarrMigrator)

		srv.SetSystem(&api.SystemDeps{
			Health:       system.NewHealth(probes),
			Tasks:        taskRegistry,
			About:        system.AboutDeps{Version: version, Commit: commit, BuildTime: buildTime, Mode: mode, StartedAt: startedAt},
			MediaRoot:    cfg.MediaRoot,
			DownloadsDir: cfg.DownloadsDir,
			Backup:       backupSvc,
			Events:       historyFeedSvc,
			Changelog:    system.NewChangelogService(version),
			Logs:         logSvc,
			Migration:    migrationSvc,
			RootFolderPaths: func() []string {
				roots, err := rfSvc.List()
				if err != nil {
					return nil
				}
				paths := make([]string, 0, len(roots))
				for _, r := range roots {
					paths = append(paths, r.Path)
				}
				return paths
			},
		})
	}

	// P5 metadata file writer: .nfo sidecars are generated on manual
	// import, gated by the metadata section's generate_nfo (a corrupt or
	// missing document keeps the default: NFOs on for tv).
	if mdRaw, err := settingsRepo.GetSectionDocument(ctx, settings.SectionMetadata); err == nil {
		if mdDoc, err := settings.SectionMetadata.DecodeDocument(mdRaw); err == nil {
			md := mdDoc.(*settings.MetadataSettings)
			wantedSvc.SetNFOWriter(&metawrite.Writer{
				Enabled:  md.GenerateNFO && md.Kinds["tv"],
				Language: md.Language,
			})
		}
	} else {
		wantedSvc.SetNFOWriter(&metawrite.Writer{Enabled: true, Language: "en"})
	}

	logger.Info("server starting", "addr", cfg.HTTPAddr)
	return srv.Serve(ctx,
		cfg.HTTPAddr,
		time.Duration(cfg.ReadTimeout)*time.Second,
		time.Duration(cfg.WriteTimeout)*time.Second,
		time.Duration(cfg.ShutdownTimeout)*time.Second,
	)
}

// migrationIndexerStore adapts the Postgres indexer repo to the migration
// target's indexer seam (system.IndexerStore).
type migrationIndexerStore struct {
	repo *postgres.IndexersRepo
}

func (s *migrationIndexerStore) FindIndexer(ctx context.Context, name string) (int64, bool) {
	def, err := s.repo.GetByName(ctx, name)
	if err != nil {
		return 0, false
	}
	return def.ID, true
}

func (s *migrationIndexerStore) CreateIndexer(ctx context.Context, name, kind, baseURL, apiKey string, enabled bool) (int64, error) {
	return s.repo.Create(ctx, indexers.Definition{
		Name: name, Kind: kind, BaseURL: baseURL, APIKey: apiKey, Enabled: enabled,
	})
}

// migrationRadarrIndexerStore adapts the Postgres indexer repo to the
// Radarr migration target's indexer seam (system.RadarrIndexerStore),
// carrying the R5a per-indexer toggles in the definition's settings bag.
type migrationRadarrIndexerStore struct {
	repo *postgres.IndexersRepo
}

func (s *migrationRadarrIndexerStore) FindIndexer(ctx context.Context, name string) (int64, bool) {
	def, err := s.repo.GetByName(ctx, name)
	if err != nil {
		return 0, false
	}
	return def.ID, true
}

func (s *migrationRadarrIndexerStore) CreateIndexer(ctx context.Context, name, kind, baseURL, apiK string, enabled, enableRss, enableAutomatic, enableInteractive bool) (int64, error) {
	def := indexers.Definition{
		Name: name, Kind: kind, BaseURL: baseURL, APIKey: apiK, Enabled: enabled,
		SettingsJSON: indexers.WithIndexerToggles("", indexers.IndexerSettings{
			RSS:               enableRss,
			AutoSearch:        enableAutomatic,
			InteractiveSearch: enableInteractive,
		}),
	}
	return s.repo.Create(ctx, def)
}

// migrationRadarrMovieStore adapts the Postgres movie repo to the Radarr
// migration target's movie seam (system.RadarrMovieStore). It writes through
// the repo directly (not the movie service): the migration may reference a
// quality-profile name that was just created in the profiles document, which
// the service's add validation (the seeded Radarr v6 name set) would reject.
type migrationRadarrMovieStore struct {
	repo *postgres.MovieRepo
}

func (s *migrationRadarrMovieStore) FindMovie(ctx context.Context, title string, year int) (int64, bool) {
	return s.repo.FindMovieByTitleYear(ctx, title, year)
}

func (s *migrationRadarrMovieStore) AddMovie(ctx context.Context, ref system.RadarrMovieRef) (int64, error) {
	m := moviedom.Movie{
		Title:          ref.Title,
		Year:           ref.Year,
		QualityProfile: ref.QualityProfile,
		Monitored:      ref.Monitored,
		Tags:           ref.Tags,
	}
	if ref.SeedEditionMonitored != nil {
		m.SeedEditionMonitored = ref.SeedEditionMonitored
	}
	id, err := s.repo.CreateMovie(ctx, m)
	if err != nil {
		return 0, err
	}
	// A monitored movie is wanted (the add use-case's semantics).
	if ref.Monitored {
		if err := s.repo.EnsureWanted(ctx, id); err != nil {
			return 0, err
		}
	}
	_ = s.repo.AddHistory(ctx, moviedom.HistoryEntry{MovieID: id, Event: "added", Detail: "migrated from Radarr"})
	return id, nil
}

func (s *migrationRadarrMovieStore) EnsureEdition(ctx context.Context, movieID int64, e system.RadarrEditionRef) (int64, bool, error) {
	return s.repo.UpsertEdition(ctx, movieID, e.Label, e.Monitored, e.Quality, e.Size)
}

// migrationLidarrMusicStore adapts the Postgres music repo to the Lidarr
// migration target's music seam (system.LidarrMusicStore). It writes through
// the repo directly (not the music service): the migration may reference a
// quality-profile name that was just created in the profiles document, which
// the service's add validation (the seeded name set) would reject. Identity
// matches the music domain: artists by name, albums by title+year, tracks by
// disc+number.
type migrationLidarrMusicStore struct {
	repo *postgres.MusicRepo
}

func (s *migrationLidarrMusicStore) FindArtist(ctx context.Context, name string) (int64, bool) {
	artists, err := s.repo.ListArtists(ctx)
	if err != nil {
		return 0, false
	}
	want := strings.ToLower(name)
	for i := range artists {
		if strings.ToLower(artists[i].Name) == want {
			return artists[i].ID, true
		}
	}
	return 0, false
}

func (s *migrationLidarrMusicStore) AddArtist(ctx context.Context, ref system.LidarrArtistRef) (int64, error) {
	id, err := s.repo.CreateArtist(ctx, music.Artist{
		Name:             ref.Name,
		MBID:             ref.MBID,
		Status:           ref.Status,
		Path:             ref.Path,
		Monitored:        ref.Monitored,
		QualityProfile:   ref.QualityProfile,
		MonitorPolicy:    ref.MonitorPolicy,
		MonitorNewAlbums: ref.MonitorNewAlbums,
		Tags:             ref.Tags,
		Genre:            ref.Genre,
		Rating:           ref.Rating,
	})
	if err != nil {
		return 0, err
	}
	_ = s.repo.AddHistory(ctx, music.HistoryEntry{ArtistID: id, Event: "added", Detail: "migrated from Lidarr"})
	return id, nil
}

func (s *migrationLidarrMusicStore) FindAlbum(ctx context.Context, artistID int64, name string, year int) (int64, bool) {
	albums, err := s.repo.ListAlbums(ctx, artistID)
	if err != nil {
		return 0, false
	}
	for i := range albums {
		if albums[i].Name == name && albums[i].Year == year {
			return albums[i].ID, true
		}
	}
	return 0, false
}

func (s *migrationLidarrMusicStore) AddAlbum(ctx context.Context, artistID int64, ref system.LidarrAlbumRef) (int64, error) {
	return s.repo.CreateAlbum(ctx, music.Album{
		ArtistID:       artistID,
		Name:           ref.Name,
		Year:           ref.Year,
		AlbumType:      ref.AlbumType,
		ReleaseDate:    ref.ReleaseDate,
		QualityProfile: ref.QualityProfile,
		Monitored:      ref.Monitored,
		Size:           ref.Size,
	})
}

func (s *migrationLidarrMusicStore) TrackKeys(ctx context.Context, albumID int64) (map[string]bool, error) {
	tracks, err := s.repo.ListTracks(ctx, albumID)
	if err != nil {
		return nil, err
	}
	keys := make(map[string]bool, len(tracks))
	for _, t := range tracks {
		keys[fmt.Sprintf("%d-%d", t.Disc, t.Number)] = true
	}
	return keys, nil
}

func (s *migrationLidarrMusicStore) AddTrack(ctx context.Context, albumID int64, ref system.LidarrTrackRef) (int64, error) {
	// ref.Quality is the imported file's quality string; "" = no file yet.
	return s.repo.CreateTrack(ctx, music.Track{
		AlbumID:   albumID,
		Disc:      ref.Disc,
		Number:    ref.Number,
		Title:     ref.Title,
		Duration:  ref.Duration,
		Monitored: ref.Monitored,
		Size:      ref.Size,
		Status:    ref.Quality,
	})
}

// migrationLidarrRootFolderStore adapts the root-folders service to the
// Lidarr migration target's root-folder seam (system.LidarrRootFolderStore).
type migrationLidarrRootFolderStore struct {
	svc *rootfolders.Service
}

func (s *migrationLidarrRootFolderStore) FindRootFolder(ctx context.Context, path string) (int64, bool) {
	all, err := s.svc.List()
	if err != nil {
		return 0, false
	}
	clean := filepath.Clean(path)
	for i := range all {
		if all[i].Path == clean {
			return all[i].ID, true
		}
	}
	return 0, false
}

func (s *migrationLidarrRootFolderStore) CreateRootFolder(ctx context.Context, path, name, defaultQuality, defaultMetadata string) (int64, error) {
	return s.svc.Create(rfdom.RootFolder{
		Path:                   path,
		Name:                   name,
		DefaultQualityProfile:  defaultQuality,
		DefaultMetadataProfile: defaultMetadata,
	})
}

// migrationTVStore adapts the Postgres TV repo to the migration target's TV
// seam (system.TVStore). FindSeries matches by TVDB id (when set) or
// title+year, which is the identity the migration uses for idempotency.
type migrationTVStore struct {
	repo *postgres.TVRepo
}

func (s *migrationTVStore) FindSeries(ctx context.Context, tvdbID int, title string, year int) (int64, bool) {
	all, err := s.repo.ListSeries(ctx)
	if err != nil {
		return 0, false
	}
	for _, sr := range all {
		if tvdbID > 0 && sr.TVDBID == tvdbID {
			return sr.ID, true
		}
		if sr.Title == title && sr.Year == year {
			return sr.ID, true
		}
	}
	return 0, false
}

func (s *migrationTVStore) AddSeries(ctx context.Context, ref system.SeriesRef) (int64, error) {
	return s.repo.CreateSeries(ctx, tvd.Series{
		TVDBID:         ref.TVDBID,
		Title:          ref.Title,
		Year:           ref.Year,
		QualityProfile: ref.QualityProfile,
		Monitored:      ref.Monitored,
		SeasonFolder:   ref.SeasonFolder,
		Tags:           ref.Tags,
		AddedAt:        time.Now(),
	})
}

// EpisodeKeys returns the "S<season>xE<episode>" keys of the series' known
// episodes (the same key format the migration target probes).
func (s *migrationTVStore) EpisodeKeys(ctx context.Context, seriesID int64) (map[string]bool, error) {
	eps, err := s.repo.ListEpisodes(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	keys := make(map[string]bool, len(eps))
	for _, e := range eps {
		keys[fmt.Sprintf("S%dxE%d", e.Season, e.Episode)] = true
	}
	return keys, nil
}

func (s *migrationTVStore) AddEpisode(ctx context.Context, seriesID int64, season, episode int, title string, airDate *time.Time, monitored bool) (int64, error) {
	return s.repo.CreateEpisode(ctx, tvd.Episode{
		SeriesID:  seriesID,
		Season:    season,
		Episode:   episode,
		Title:     title,
		AirDate:   airDate,
		Monitored: monitored,
		AddedAt:   time.Now(),
	})
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

// tvMediaRoot returns the on-disk root for imported TV files. It defaults to a
// "tv" subdirectory under the shared media root so the TV and movie libraries
// live in separate trees. If the shared media root is empty, it falls back to
// a plain "tv" directory.
func tvMediaRoot(mediaRoot string) string {
	if mediaRoot == "" {
		return "tv"
	}
	return mediaRoot + "/tv"
}

// musicMediaRoot returns the on-disk root for imported music files. It
// defaults to a "music" subdirectory under the shared media root so the music
// library lives in a separate tree from the movie and TV libraries. If the
// shared media root is empty, it falls back to a plain "music" directory.
func musicMediaRoot(mediaRoot string) string {
	if mediaRoot == "" {
		return "music"
	}
	return mediaRoot + "/music"
}

// booksMediaRoot returns the on-disk root for imported book files. It defaults
// to a "books" subdirectory under the shared media root so the books library
// lives in a separate tree from the movie, TV, and music libraries. If the
// shared media root is empty, it falls back to a plain "books" directory.
func booksMediaRoot(mediaRoot string) string {
	if mediaRoot == "" {
		return "books"
	}
	return mediaRoot + "/books"
}
