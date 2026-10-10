// Package movies (services) is the use-case layer for the movie domain. It
// orchestrates the full download pipeline — add → want → search → quality
// match → download → import — against the pure domain (domains/movies) and the
// swappable adapter interfaces (indexers, downloads). It depends on interfaces,
// never on Postgres/Redis concretely, so the pipeline is testable end-to-end
// with fakes and a real Postgres repository alike.
package movies

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/movies"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/indexers"
	"github.com/brandenk514/mediarr/internal/store"
)

// ErrNoMatch is returned when no indexer release satisfies the quality profile.
var ErrNoMatch = fmt.Errorf("movies: no release matched the quality profile")

// Deps bundles the Service's collaborators. Everything is an interface (or the
// pure domain), so the service is testable with a fake indexer, a mock download
// client, and either a real Postgres repo or an in-memory fake.
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

// AddMovie creates a movie (if new), ensures a pending wanted row, and records
// the add in history. Returns the movie id.
func (s *Service) AddMovie(ctx context.Context, title, year string, qualityProfile string) (int64, error) {
	if strings.TrimSpace(title) == "" {
		return 0, fmt.Errorf("movies: title is required")
	}
	if qualityProfile == "" {
		qualityProfile = s.deps.DefaultProfile
	}
	m := dom.Movie{
		Title:          strings.TrimSpace(title),
		Year:           parseYear(year),
		QualityProfile: qualityProfile,
		Monitored:      true,
	}
	id, err := s.deps.Repo.CreateMovie(ctx, m)
	if err != nil {
		return 0, err
	}
	if err := s.deps.Repo.EnsureWanted(ctx, id); err != nil {
		return 0, err
	}
	_ = s.deps.Repo.AddHistory(ctx, dom.HistoryEntry{
		MovieID: id, Event: "added", Detail: "movie added to library",
	})
	return id, nil
}

// ListMovies returns all movies (used by GET /api/v1/movies).
func (s *Service) ListMovies(ctx context.Context) ([]dom.Movie, error) {
	return s.deps.Repo.ListMovies(ctx)
}

// GetMovie loads a movie (used by GET /api/v1/movies/{id}).
func (s *Service) GetMovie(ctx context.Context, id int64) (*dom.Movie, error) {
	return s.deps.Repo.GetMovie(ctx, id)
}

// RunPipeline drives the full pipeline for one movie: search indexers, match
// the best release against the quality profile, download it, import it, and
// mark the wanted entry satisfied. It is idempotent: if the movie's wanted row
// is already satisfied, it returns without re-searching.
func (s *Service) RunPipeline(ctx context.Context, movieID int64) (string, error) {
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
			profile, ok = dom.ProfileByName("Any")
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

// record appends a history entry, ignoring errors (history is best-effort).
func (s *Service) record(ctx context.Context, movieID int64, event, detail string) {
	_ = s.deps.Repo.AddHistory(ctx, dom.HistoryEntry{
		MovieID: movieID, Event: event, Detail: detail,
	})
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
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		// Cross-device rename can fail; fall back to copy + remove.
		data, rerr := os.ReadFile(src)
		if rerr != nil {
			return err
		}
		if werr := os.WriteFile(dst, data, 0o644); werr != nil {
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
