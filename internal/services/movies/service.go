// Package movies (services) is the use-case layer for the movie domain. It
// orchestrates the full download pipeline — add → want → search → quality
// match → download → import — against the pure domain (domains/movies) and the
// swappable adapter interfaces (indexers, downloads). It depends on interfaces,
// never on Postgres/Redis concretely, so the pipeline is testable end-to-end
// with fakes and a real Postgres repository alike.
package movies

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/movies"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/indexers"
	"github.com/brandenk514/mediarr/internal/metrics"
	"github.com/brandenk514/mediarr/internal/paging"
	"github.com/brandenk514/mediarr/internal/queue"
	"github.com/brandenk514/mediarr/internal/store"
)

// ErrNoMatch is returned when no indexer release satisfies the quality profile.
var ErrNoMatch = fmt.Errorf("movies: no release matched the quality profile")

// ErrMovieNotFound is re-exported from the pure domain so API/transport layers
// can map it to 404 without importing the domain directly.
var ErrMovieNotFound = dom.ErrMovieNotFound

// ErrEditionNotFound is re-exported from the pure domain so API/transport
// layers can map it to 404 without importing the domain directly.
var ErrEditionNotFound = dom.ErrEditionNotFound

// Deps bundles the Service's collaborators. Everything is an interface (or
// the pure domain), so the service is testable with a fake indexer, a mock
// download client, and either a real Postgres repo or an in-memory fake.
type Deps struct {
	// Repo persists movies, wanted, queue, and history.
	Repo dom.Repo
	// Indexers are searched (fan-out) for candidate releases.
	Indexers []indexers.Searcher
	// Client is the download client that fetches the chosen release.
	Client downloads.Client
	// MediaRoot is where imported files are placed (e.g. /media/movies).
	MediaRoot string
	// DefaultProfile is used when a movie has no profile set.
	DefaultProfile string
	// DownloadsDir is where the download client writes completed files before
	// import moves them under MediaRoot. It is threaded into each Release as
	// PathDir so the client knows where to save (#36/#37).
	DownloadsDir string
	// Events, when non-nil, receives queue.state pub/sub events as the queue
	// advances (PLAN §5/§6, #36 "state transitions feed ... pub/sub events").
	// It is optional so the pipeline runs without a bus (e.g. tests).
	Events store.Events
	// Metrics, when non-nil, observes pipeline runs and imports (M7 #46).
	// Nil-safe: the pipeline runs identically without it.
	Metrics *metrics.Metrics
	// Enricher, when non-nil, fills the display-only movie detail fields
	// (TMDB overview/scores/cast/crew/release date) on the detail read path
	// (Radarr R1). Optional: nil (e.g. no TMDB key) leaves the fields empty
	// and the UI degrades to em-dashes.
	Enricher dom.MovieEnricher
	// CollectionParts, when non-nil, provides TMDB collection parts for the
	// collections page's missing counts (Radarr R3). Optional: nil (keyless)
	// or a failing fetch leaves missing_count 0.
	CollectionParts TMDBCollectionParter
	// TMDB, when non-nil, backs the Discover page's trending feed (Radarr
	// R3). Optional: nil (no TMDB key) makes Discover answer 503, the same
	// keyless shape the search endpoints use.
	TMDB TMDBDiscoverer
}

// Service implements the movie use-cases.
type Service struct {
	deps Deps
}

// New builds a Service.
func New(deps Deps) *Service {
	if deps.DefaultProfile == "" {
		deps.DefaultProfile = "HD-1080p"
	}
	return &Service{deps: deps}
}

// ErrInvalidAddParams is returned when the add request carries a monitor
// mode or minimum availability that is not one of the known values (R2);
// the API maps it to a 400.
var ErrInvalidAddParams = errors.New("movies: invalid add-movie parameter")

// AddMovieRequest is the movie add use-case input (Radarr R2 two-pane add).
// Omitted optional fields keep their pre-R2 behavior: monitor "" means "all",
// empty tags/root folder/minimum availability are stored as-is (unset).
type AddMovieRequest struct {
	Title          string
	Year           string
	QualityProfile string
	// Monitor is the edition-aware monitor mode (R2): all | movie_only |
	// unmonitored | all_except_3d; "" defaults to all.
	Monitor string
	// MinimumAvailability: announced | posters | in_production | released;
	// '' when unset.
	MinimumAvailability string
	// Tags are free-form labels (R2); nil = none.
	Tags []string
	// RootFolder is a root folder path (R2); '' = server default.
	RootFolder string
	// StartSearch runs the pipeline best-effort after the add (R2).
	StartSearch bool
}

// AddMovie creates a movie (if new), resolves the edition-aware monitor plan
// (R2), ensures a pending wanted row when the movie ends up monitored, and
// records the add in history. With StartSearch it additionally runs the
// pipeline best-effort: a pipeline failure is recorded in history and never
// fails the add — the movie stays in the library (Radarr R2).
func (s *Service) AddMovie(ctx context.Context, req AddMovieRequest) (int64, error) {
	if strings.TrimSpace(req.Title) == "" {
		return 0, fmt.Errorf("movies: title is required")
	}
	if req.QualityProfile == "" {
		req.QualityProfile = s.deps.DefaultProfile
	}
	plan, err := dom.MonitorPlanFor(dom.MonitorMode(req.Monitor))
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidAddParams, err)
	}
	switch req.MinimumAvailability {
	case "",
		dom.AvailabilityAnnounced, dom.AvailabilityPosters,
		dom.AvailabilityInProduction, dom.AvailabilityReleased:
	default:
		return 0, fmt.Errorf("%w: minimum_availability %q", ErrInvalidAddParams, req.MinimumAvailability)
	}
	// Radarr R2: minimum_availability defaults to 'released' for every add;
	// pre-R2 movies in the database may still carry '' (unset).
	if req.MinimumAvailability == "" {
		req.MinimumAvailability = dom.AvailabilityReleased
	}
	m := dom.Movie{
		Title:                strings.TrimSpace(req.Title),
		Year:                 parseYear(req.Year),
		QualityProfile:       req.QualityProfile,
		Monitored:            plan.MovieMonitored,
		Tags:                 req.Tags,
		RootFolder:           req.RootFolder,
		MinimumAvailability:  req.MinimumAvailability,
		SeedEditionMonitored: boolPtr(plan.EditionMonitored("Theatrical")),
	}
	id, err := s.deps.Repo.CreateMovie(ctx, m)
	if err != nil {
		return 0, err
	}
	// An unmonitored movie (R2 "unmonitored" mode) is not wanted: no pending
	// wanted row. Monitored movies keep the pre-R2 behavior.
	if plan.MovieMonitored {
		if err := s.deps.Repo.EnsureWanted(ctx, id); err != nil {
			return 0, err
		}
	}
	_ = s.deps.Repo.AddHistory(ctx, dom.HistoryEntry{
		MovieID: id, Event: "added", Detail: "movie added to library",
	})
	s.emitHistory(ctx, id, "added", "movie added to library")
	if req.StartSearch {
		// Best-effort pipeline run (R2): the add already succeeded, so a
		// failure here is only recorded — the movie stays as missing.
		if _, perr := s.RunPipeline(ctx, id); perr != nil {
			s.record(ctx, id, "start-search-failed", perr.Error())
		}
	}
	return id, nil
}

// boolPtr wraps a bool for the pointer-typed SeedEditionMonitored field.
func boolPtr(b bool) *bool { return &b }

// ListMovies returns all movies (used by GET /api/v1/movies).
func (s *Service) ListMovies(ctx context.Context) ([]dom.Movie, error) {
	return s.deps.Repo.ListMovies(ctx)
}

// ListMoviesQuery returns a server-side filtered and paginated page of
// movies (M6 #41). It reads the full list and applies the query in memory;
// the dataset is small (thousands, not millions) so a single read is cheaper
// than a per-query parameterized SQL build. R1: each page item also carries
// its embedded editions/files/status (the grid renders status ribbons).
// No provider enrichment here — that is detail-only, to keep the grid cheap
// without a TMDB key.
func (s *Service) ListMoviesQuery(ctx context.Context, q paging.Query) (paging.Page[dom.Movie], error) {
	all, err := s.deps.Repo.ListMovies(ctx)
	if err != nil {
		return paging.Page[dom.Movie]{}, err
	}
	page := paging.Apply(all, q, func(m dom.Movie) (string, bool) {
		return m.Title, m.Monitored
	})
	views, err := s.buildViews(ctx, page.Items, false)
	if err != nil {
		return paging.Page[dom.Movie]{}, err
	}
	page.Items = views
	return page, nil
}

// SetMovieMonitored toggles a movie's monitoring (M6 #41). On toggle-on it
// ensures a pending wanted row so a re-monitored movie is wanted again; the
// toggle is recorded in history and emitted live so the dashboard refreshes.
func (s *Service) SetMovieMonitored(ctx context.Context, id int64, monitored bool) error {
	if err := s.deps.Repo.SetMovieMonitored(ctx, id, monitored); err != nil {
		return err
	}
	if monitored {
		if err := s.deps.Repo.EnsureWanted(ctx, id); err != nil {
			return err
		}
	}
	event := "monitored"
	if !monitored {
		event = "unmonitored"
	}
	s.record(ctx, id, event, "movie-level toggle")
	return nil
}

// GetMovie loads a movie with its Radarr-R1 read state (editions, files,
// derived status) plus provider enrichment when an Enricher is configured
// (used by GET /api/v1/movies/{id}, the detail page).
func (s *Service) GetMovie(ctx context.Context, id int64) (*dom.Movie, error) {
	m, err := s.deps.Repo.GetMovie(ctx, id)
	if err != nil {
		return nil, err
	}
	views, err := s.buildViews(ctx, []dom.Movie{*m}, true)
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

// GetStats returns the Movies-page stats bar (Radarr R1).
func (s *Service) GetStats(ctx context.Context) (dom.MovieStats, error) {
	return s.deps.Repo.GetMovieStats(ctx)
}

// SetEditionMonitored toggles one edition's monitoring (Radarr v6 edition
// semantics, R1). The edition must exist and belong to the movie.
func (s *Service) SetEditionMonitored(ctx context.Context, movieID, editionID int64, monitored bool) (*dom.Edition, error) {
	if _, err := s.deps.Repo.GetMovie(ctx, movieID); err != nil {
		return nil, err
	}
	e, err := s.deps.Repo.GetEdition(ctx, editionID)
	if err != nil {
		return nil, err
	}
	if e.MovieID != movieID {
		return nil, dom.ErrEditionNotFound
	}
	if err := s.deps.Repo.SetEditionMonitored(ctx, editionID, monitored); err != nil {
		return nil, err
	}
	e.Monitored = monitored
	s.record(ctx, movieID, "edition-monitored",
		fmt.Sprintf("edition %q monitored=%v", e.Label, monitored))
	return e, nil
}

// buildViews fills each movie's Radarr-R1 read state: its editions, files,
// derived Path/Size and status. With enrich=true (the detail read) it also
// asks the optional metadata provider for the display enrichment; list reads
// skip it so the grid stays cheap. A failing enrichment never fails the read
// (keyless TMDB degrades gracefully — em-dashes in the UI).
func (s *Service) buildViews(ctx context.Context, in []dom.Movie, enrich bool) ([]dom.Movie, error) {
	editions, err := s.deps.Repo.ListEditions(ctx)
	if err != nil {
		return nil, err
	}
	files, err := s.deps.Repo.ListMovieFiles(ctx)
	if err != nil {
		return nil, err
	}
	wantedRows, err := s.deps.Repo.ListWanted(ctx)
	if err != nil {
		return nil, err
	}
	queueRows, err := s.deps.Repo.ListQueue(ctx)
	if err != nil {
		return nil, err
	}

	edByMovie := map[int64][]dom.Edition{}
	for _, e := range editions {
		edByMovie[e.MovieID] = append(edByMovie[e.MovieID], e)
	}
	fileByMovie := map[int64][]dom.MovieFile{}
	for _, f := range files {
		fileByMovie[f.MovieID] = append(fileByMovie[f.MovieID], f)
	}
	wantByMovie := map[int64]*dom.Wanted{}
	for i := range wantedRows {
		wantByMovie[wantedRows[i].MovieID] = &wantedRows[i]
	}
	activeQueue := map[int64]bool{}
	for _, q := range queueRows {
		if q.State == dom.QueueQueued || q.State == dom.QueueDownloading {
			activeQueue[q.MovieID] = true
		}
	}

	out := make([]dom.Movie, 0, len(in))
	for _, m := range in {
		view := m
		// Arrays must serialize as [] (Radarr parity; the OpenAPI contract
		// declares them non-nullable) — never null.
		if view.Genres == nil {
			view.Genres = []string{}
		}
		if view.Cast == nil {
			view.Cast = []dom.MovieCast{}
		}
		if view.Crew == nil {
			view.Crew = []dom.MovieCrew{}
		}
		if view.AlternativeTitles == nil {
			view.AlternativeTitles = []dom.AlternativeTitle{}
		}
		ed := edByMovie[m.ID]
		if ed == nil {
			ed = []dom.Edition{}
		}
		view.Editions = ed
		fl := fileByMovie[m.ID]
		if fl == nil {
			fl = []dom.MovieFile{}
		}
		view.Files = fl
		if len(fl) > 0 {
			view.Path = &fl[0].RelativePath
			var total int64
			for _, f := range fl {
				total += f.Size
			}
			view.Size = &total
		}
		if enrich && s.deps.Enricher != nil {
			preID, preName := view.CollectionID, collectionNameOf(view.CollectionName)
			preRelease := view.ReleaseDate
			_ = s.deps.Enricher.EnrichMovie(ctx, &view) // best effort
			// Radarr R3: persist the collection the provider reported so
			// the collections page can group without a live TMDB call.
			// Only write when the provider reported something new.
			newID, newName := view.CollectionID, collectionNameOf(view.CollectionName)
			if (newID != "" || newName != "") && (newID != preID || newName != preName) {
				_ = s.deps.Repo.SetMovieCollection(ctx, view.ID, newID, newName)
			}
			// Radarr R4: persist the release date the provider reported so
			// the movie calendar works without a live TMDB call.
			if view.ReleaseDate != "" && view.ReleaseDate != preRelease {
				_ = s.deps.Repo.SetMovieReleaseDate(ctx, view.ID, view.ReleaseDate)
			}
		}
		w := wantByMovie[m.ID]
		view.Status = dom.DeriveMovieStatus(
			w != nil && w.Status == dom.WantedSatisfied,
			activeQueue[m.ID],
			m.Monitored,
			dom.ParseReleaseDate(view.ReleaseDate),
			time.Now(),
		)
		out = append(out, view)
	}
	return out, nil
}

// GetWanted loads a movie's wanted (release) row (used by
// GET /api/v1/movies/{id}/wanted, P7 detail page). It returns
// ErrMovieNotFound when the movie does not exist, and (nil, nil) when the
// movie exists but has no wanted row yet — the API maps that to 204.
func (s *Service) GetWanted(ctx context.Context, movieID int64) (*dom.Wanted, error) {
	if _, err := s.deps.Repo.GetMovie(ctx, movieID); err != nil {
		return nil, err
	}
	w, err := s.deps.Repo.GetWanted(ctx, movieID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return w, nil
}

// RunPipeline drives the full pipeline for one movie: search indexers, match
// the best release against the quality profile, download it, import it, and
// mark the wanted entry satisfied. It is idempotent: if the movie's wanted row
// is already satisfied, it returns without re-searching.
func (s *Service) RunPipeline(ctx context.Context, movieID int64) (path string, err error) {
	start := time.Now()
	skip := false
	defer func() {
		result := metrics.ResultOK
		switch {
		case err != nil:
			result = metrics.ResultError
		case skip:
			result = metrics.ResultSkip
		}
		imported := 0
		if err == nil && !skip {
			imported = 1
		}
		s.deps.Metrics.RecordPipeline(string(queue.KindMovie), result, time.Since(start), imported)
	}()

	m, err := s.deps.Repo.GetMovie(ctx, movieID)
	if err != nil {
		return "", err
	}

	// Idempotency: a satisfied movie is done.
	w, err := s.deps.Repo.GetWanted(ctx, movieID)
	if err != nil {
		return "", err
	}
	if w.Status == dom.WantedSatisfied {
		skip = true
		return "", nil // already imported
	}

	// The download client is optional (e.g. its directory isn't writable).
	// Without it the pipeline can't fetch a file, so say so clearly.
	if s.deps.Client == nil {
		s.record(ctx, movieID, "no-client", "no download client configured")
		return "", fmt.Errorf("movies: no download client configured")
	}

	// 1. Search.
	results, err := s.searchAll(ctx, m.Title, m.Year)
	if err != nil {
		s.record(ctx, movieID, "search", "search failed: "+err.Error())
		return "", err
	}
	s.record(ctx, movieID, "searched", fmt.Sprintf("%d candidate release(s)", len(results)))

	// 2. Match + pick best.
	best, ok := s.selectBest(m, results)
	if !ok {
		s.record(ctx, movieID, "no-match", "no release satisfied the profile")
		return "", ErrNoMatch
	}
	s.record(ctx, movieID, "matched", fmt.Sprintf(
		"picked %q (score %d, indexer %s)", best.Result.Title, best.Score, best.Result.Indexer))

	// 3. Download.
	queueID, err := s.sendToClient(ctx, m, best)
	if err != nil {
		s.record(ctx, movieID, "download-failed", err.Error())
		return "", err
	}

	// 4. Import.
	imported, err := s.importRelease(ctx, m, best, queueID)
	if err != nil {
		s.record(ctx, movieID, "import-failed", err.Error())
		return "", err
	}

	// 5. Satisfy.
	if err := s.deps.Repo.MarkWantedSatisfied(ctx, movieID, best.Result.Title); err != nil {
		return "", err
	}
	s.record(ctx, movieID, "imported", "imported to "+imported)
	return imported, nil
}

// searchAll fans out to every indexer concurrently (via the shared M5 Fanout),
// merging and deduping by release title (first indexer wins). The domain pickers
// re-score and re-order the candidates, so the merged order does not affect which
// release is chosen.
func (s *Service) searchAll(ctx context.Context, title string, year int) ([]indexers.SearchResult, error) {
	q := indexers.SearchQuery{Term: title, Year: year}
	f := indexers.NewFanout(s.deps.Indexers...)
	return f.Search(ctx, q)
}

// selectBest parses every candidate release, matches it against the movie's
// quality profile, and returns the highest-scoring matched release. ok is
// false when nothing matches.
func (s *Service) selectBest(m *dom.Movie, results []indexers.SearchResult) (bestMatch, bool) {
	profile, ok := dom.ProfileByName(m.QualityProfile)
	if !ok {
		profile, ok = dom.ProfileByName(s.deps.DefaultProfile)
		if !ok {
			profile, _ = dom.ProfileByName("Any") // fallback profile; ok not needed past this point
		}
	}

	var candidates []bestMatch
	for _, r := range results {
		parsed := dom.ParseRelease(r.Title)
		match := dom.MatchRelease(profile, parsed)
		if !match.Matched {
			continue
		}
		candidates = append(candidates, bestMatch{
			Result: r,
			Parsed: parsed,
			Match:  match,
			Score:  match.Score,
		})
	}
	if len(candidates) == 0 {
		return bestMatch{}, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})
	return candidates[0], true
}

// bestMatch pairs a chosen release with its parsed form and profile match.
type bestMatch struct {
	Result indexers.SearchResult
	Parsed dom.ParsedRelease
	Match  dom.ProfileMatch
	Score  int
}

// sendToClient queues the release, sends it to the download client, and records
// the queue entry. Returns the queue entry id. The release's download URL and
// target directory come from the indexer result's ReleaseInfo (the real
// adapters populate it; the mock/fake leave it empty and the client falls back
// to its own defaults) — this is the M5 wiring that lets a real client fetch
// the release rather than only simulating it (#36/#37).
func (s *Service) sendToClient(ctx context.Context, m *dom.Movie, bm bestMatch) (int64, error) {
	qe := dom.QueueEntry{
		MovieID:        m.ID,
		ReleaseTitle:   bm.Result.Title,
		Indexer:        bm.Result.Indexer,
		DownloadClient: s.deps.Client.Name(),
		State:          dom.QueueQueued,
		Progress:       0,
	}
	id, err := s.deps.Repo.CreateQueue(ctx, qe)
	if err != nil {
		return 0, err
	}
	qe.ID = id
	s.emitQueue(ctx, qe)

	qe.State = dom.QueueDownloading
	_ = s.deps.Repo.UpdateQueue(ctx, qe)
	s.emitQueue(ctx, qe)

	if err := s.deps.Client.Add(ctx, downloads.Release{
		Title:    bm.Result.Title,
		FileName: fileNameFor(bm.Parsed),
		URL:      bm.Result.Info.URL,
		PathDir:  s.deps.DownloadsDir,
	}); err != nil {
		qe.State = dom.QueueFailed
		qe.Progress = 0
		_ = s.deps.Repo.UpdateQueue(ctx, qe)
		s.emitQueue(ctx, qe)
		return 0, err
	}
	return id, nil
}

// emitQueue publishes a "queue.updated" pub/sub event for a queue entry. It is
// a no-op when no bus is configured and never fails the pipeline: a dead Redis
// must not stop a download, so publish errors are deliberately ignored here
// (the queue table is the source of truth; the bus is a best-effort push).
func (s *Service) emitQueue(ctx context.Context, qe dom.QueueEntry) {
	if s.deps.Events == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"movie_id":      qe.MovieID,
		"queue_id":      qe.ID,
		"release_title": qe.ReleaseTitle,
		"indexer":       qe.Indexer,
		"client":        qe.DownloadClient,
		"state":         qe.State,
		"progress":      qe.Progress,
	})
	if err != nil {
		return
	}
	_ = s.deps.Events.Publish(ctx, store.Event{Type: "queue.updated", Payload: payload})
}

// importRelease polls the client until the release is complete, plans the
// import, moves the file under the media root, marks the queue complete, and —
// for clients that implement Remover — drops the release from the client's
// queue now that the file has been imported (PLAN §6 "removal after import",
// #36/#37).
func (s *Service) importRelease(ctx context.Context, m *dom.Movie, bm bestMatch, queueID int64) (string, error) {
	file, err := s.waitComplete(ctx, m, bm.Result.Title, queueID)
	if err != nil {
		s.markFailed(ctx, m, queueID, "download-failed", err)
		return "", err
	}
	if file == "" {
		s.markFailed(ctx, m, queueID, "download-failed", fmt.Errorf("download did not produce a file"))
		return "", fmt.Errorf("movies: download did not produce a file for %q", bm.Result.Title)
	}

	plan, err := dom.PlanImport(s.deps.MediaRoot, m.Title, m.Year, file)
	if err != nil {
		s.markFailed(ctx, m, queueID, "import-failed", err)
		return "", err
	}
	if err := moveFile(plan.SourcePath, plan.DestPath); err != nil {
		s.markFailed(ctx, m, queueID, "import-failed", err)
		return "", err
	}

	qe := dom.QueueEntry{ID: queueID, State: dom.QueueComplete, Progress: 100}
	if err := s.deps.Repo.UpdateQueue(ctx, qe); err != nil {
		return "", err
	}
	s.emitQueue(ctx, qe)

	// R1: record the imported file so the movie's Files/Path/Size reflect it
	// (best effort — a file-recording failure must not fail the import).
	if size := fileSizeOf(plan.DestPath); size > 0 {
		if rel, rerr := filepath.Rel(s.deps.MediaRoot, plan.DestPath); rerr == nil {
			if _, ferr := s.deps.Repo.CreateMovieFile(ctx, dom.MovieFile{
				MovieID:      m.ID,
				RelativePath: rel,
				VideoCodec:   codecFor(bm.Parsed),
				Size:         size,
				Quality:      bm.Parsed.QualityName,
				Formats:      formatsFor(bm.Parsed),
			}); ferr != nil {
				s.record(ctx, m.ID, "file-record-failed", ferr.Error())
			}
		}
	}

	// Removal after import: only when the client opted in (a mock or a
	// keep-for-seeding client doesn't implement Remover, so this is skipped).
	// A removal error is not fatal — the file is already imported safely; a
	// leftover queue entry is far less bad than losing an import.
	if rm, ok := s.deps.Client.(downloads.Remover); ok {
		if rerr := rm.Remove(ctx, bm.Result.Title); rerr != nil {
			s.record(ctx, m.ID, "remove-failed", "could not remove release from client: "+rerr.Error())
		} else {
			s.record(ctx, m.ID, "removed", "release removed from client after import")
		}
	}
	return plan.DestPath, nil
}

// fileSizeOf returns a file's size in bytes (0 when it can't be stat'ed).
func fileSizeOf(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// codecFor normalizes a parsed release codec to a display value
// ("any"/empty → "").
func codecFor(p dom.ParsedRelease) string {
	if p.Codec == dom.CodecAny || p.Codec == "" {
		return ""
	}
	return p.Codec
}

// formatsFor derives format tags from the parsed release source (e.g. the
// "web" container hint).
func formatsFor(p dom.ParsedRelease) []string {
	if p.Source == "" || p.Source == dom.SourceAny {
		return []string{}
	}
	return []string{p.Source}
}

// markFailed records a queue failure (state + event + history) without
// overwriting the caller's returned error. event is the history event name
// ("download-failed" / "import-failed").
func (s *Service) markFailed(ctx context.Context, m *dom.Movie, queueID int64, event string, err error) {
	qe := dom.QueueEntry{ID: queueID, State: dom.QueueFailed, Progress: 0}
	_ = s.deps.Repo.UpdateQueue(ctx, qe)
	s.emitQueue(ctx, qe)
	s.record(ctx, m.ID, event, err.Error())
}

// waitComplete polls the download client until the release reports complete
// (bounded by a deadline) and returns the file path. Each poll feeds the
// client's progress into the queue row and a pub/sub event (#36 "state
// transitions feed the queue table + pub/sub events").
func (s *Service) waitComplete(ctx context.Context, m *dom.Movie, title string, queueID int64) (string, error) {
	deadline := time.Now().Add(30 * time.Second)
	lastProgress := -1
	for {
		st, err := s.deps.Client.Status(ctx, title)
		if err != nil {
			return "", err
		}
		if st.Complete && st.File != "" {
			return st.File, nil
		}
		// Feed progress (only when it changes, to avoid chatty updates).
		if st.Progress != lastProgress {
			lastProgress = st.Progress
			qe := dom.QueueEntry{ID: queueID, State: dom.QueueDownloading, Progress: st.Progress}
			_ = s.deps.Repo.UpdateQueue(ctx, qe)
			s.emitQueue(ctx, qe)
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("movies: timed out waiting for download of %q", title)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// record appends a history entry, ignoring errors (history is best-effort),
// and emits a "history.appended" pub/sub event so the live dashboard's history
// feed refreshes (the queue is the source of truth; the bus is best-effort).
func (s *Service) record(ctx context.Context, movieID int64, event, detail string) {
	_ = s.deps.Repo.AddHistory(ctx, dom.HistoryEntry{
		MovieID: movieID, Event: event, Detail: detail,
	})
	s.emitHistory(ctx, movieID, event, detail)
}

// emitHistory publishes a "history.appended" pub/sub event. It is a no-op when
// no bus is configured and never fails the pipeline (best-effort, like the
// other emissions).
func (s *Service) emitHistory(ctx context.Context, movieID int64, event, detail string) {
	if s.deps.Events == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"movie_id": movieID,
		"event":    event,
		"detail":   detail,
	})
	if err != nil {
		return
	}
	_ = s.deps.Events.Publish(ctx, store.Event{Type: "history.appended", Payload: payload})
}

// fileNameFor derives a safe download-client file name from a parsed release:
// a cleaned scene name plus an assumed container extension.
func fileNameFor(p dom.ParsedRelease) string {
	name := p.Title
	if name == "" {
		name = "release"
	}
	name = strings.ReplaceAll(name, ".", " ")
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.Join(strings.Fields(name), " ")
	return name + ".mkv"
}

// moveFile moves src to dst, creating dst's directory as needed.
func moveFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil { //nolint:gosec // media tree dirs must stay group/world-readable for media-server users (Plex, etc.)
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		// Cross-device rename can fail; fall back to copy + remove.
		data, rerr := os.ReadFile(src) //nolint:gosec // src is the download client's output path, built by the pipeline
		if rerr != nil {
			return err
		}
		if werr := os.WriteFile(dst, data, 0o644); werr != nil { //nolint:gosec // media files must be world-readable for media-server users
			return werr
		}
		return os.Remove(src)
	}
	return nil
}

// parseYear converts a year string to an int (0 if absent/invalid).
func parseYear(s string) int {
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n); err != nil {
		return 0
	}
	return n
}
