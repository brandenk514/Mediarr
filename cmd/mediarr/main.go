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
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/brandenk514/mediarr/internal/api"
	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/config"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/indexers"
	"github.com/brandenk514/mediarr/internal/postgres"
	"github.com/brandenk514/mediarr/internal/redis"
	"github.com/brandenk514/mediarr/internal/secrets"
	bookssvc "github.com/brandenk514/mediarr/internal/services/books"
	moviesvc "github.com/brandenk514/mediarr/internal/services/movies"
	musicsvc "github.com/brandenk514/mediarr/internal/services/music"
	tvs "github.com/brandenk514/mediarr/internal/services/tv"
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
	registry := indexers.NewDefaultRegistry()

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
	checker := health.NewChecker(pg, cache)
	srv := api.NewServer(checker)
	srv.SetAuth(&api.AuthDeps{Repo: auth.NewRepo(pg.DB())})

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
	var mockClient *downloads.MockClient
	var clientErr error
	mockClient, clientErr = downloads.NewMockClient("mock", cfg.DownloadsDir)
	if clientErr != nil {
		logger.Warn("download client init failed; movie pipeline disabled", "error", clientErr)
	}
	var dlClient downloads.Client
	if mockClient != nil {
		dlClient = mockClient
	}
	movieSvc := moviesvc.New(moviesvc.Deps{
		Repo:           postgres.NewMovieRepo(pg.DB()),
		Indexers:       indexersFor(fake),
		Client:         dlClient,
		MediaRoot:      cfg.MediaRoot,
		DefaultProfile: "HD-1080p",
	})
	srv.SetMovies(&api.MoviesDeps{Svc: movieSvc})
	logger.Info("movie pipeline ready",
		"media_root", cfg.MediaRoot,
		"downloads_dir", cfg.DownloadsDir,
		"indexers", "fake",
		"client", func() string {
			if dlClient != nil {
				return "mock"
			}
			return "none"
		}())

	// --- TV pipeline ---------------------------------------------------
	// M2: wire the TV pipeline end-to-end, mirroring the movie pipeline. The
	// TV service shares the same fake indexer + mock download client so a
	// running dev server can exercise the full add → want → search → match →
	// download → import path for series/episodes. A TV media root defaults to
	// <MediaRoot>/tv so the two domains land in separate trees.
	tvFake := indexers.NewFakeIndexer("fake")
	// Dev-only demo pool (mirrors the movie pool): a few plausible TV release
	// names so a live "search" returns candidates for a handful of series.
	tvFake.AddRelease(indexers.SearchResult{Title: "Breaking.Bad.S01E01.Piloto.1080p.WEB.x264", SizeBytes: 1_900_000_000})
	tvFake.AddRelease(indexers.SearchResult{Title: "Breaking.Bad.S01E02.1080p.WEB.x264", SizeBytes: 1_800_000_000})
	tvFake.AddRelease(indexers.SearchResult{Title: "Breaking.Bad.S01.COMPLETE.1080p.WEB.x264", SizeBytes: 8_400_000_000})
	tvFake.AddRelease(indexers.SearchResult{Title: "The.Wire.S01E01.1080p.WEB.x264", SizeBytes: 1_700_000_000})
	tvFake.AddRelease(indexers.SearchResult{Title: "The.Wire.S01E01E02.1080p.WEB.x264", SizeBytes: 3_300_000_000})

	tvMediaRoot := tvMediaRoot(cfg.MediaRoot)
	tvSvc := tvs.New(tvs.Deps{
		Repo:           postgres.NewTVRepo(pg.DB()),
		Indexers:       indexersFor(tvFake),
		Client:         dlClient,
		MediaRoot:      tvMediaRoot,
		DefaultProfile: "HD-1080p",
	})
	srv.SetTV(&api.TVDeps{Svc: tvSvc})
	logger.Info("tv pipeline ready",
		"media_root", tvMediaRoot,
		"indexers", "fake",
		"client", func() string {
			if dlClient != nil {
				return "mock"
			}
			return "none"
		}())

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
	})
	srv.SetMusic(&api.MusicDeps{Svc: musicSvc})
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
