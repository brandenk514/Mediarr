// Package tv (services) is the use-case layer for the TV domain. It
// orchestrates the full download pipeline — add series → add episode → want →
// search → quality match → pick best (grouped by episode coverage) → download →
// import → satisfy — against the pure domain (domains/tv) and the swappable
// adapter interfaces (indexers, downloads). It depends on interfaces, never on
// Postgres/Redis concretely, so the pipeline is testable end-to-end with fakes
// and a real Postgres repository alike (the same pattern as services/movies).
package tv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/tv"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/indexers"
)

// ErrNoMatch is returned when no indexer release satisfies a wanted episode's
// quality profile.
var ErrNoMatch = fmt.Errorf("tv: no release matched the quality profile")

// ErrNoWanted is returned when a series has no pending wanted episodes to
// service (nothing to do).
var ErrNoWanted = fmt.Errorf("tv: no pending wanted episodes")

// ErrSeriesNotFound and ErrEpisodeNotFound are re-exported from the pure
// domain so API/transport layers can map them to 404 without importing the
// domain directly.
var (
	ErrSeriesNotFound  = dom.ErrSeriesNotFound
	ErrEpisodeNotFound = dom.ErrEpisodeNotFound
)

// Deps bundles the Service's collaborators. Everything is an interface (or the
// pure domain), so the service is testable with a fake indexer, a mock download
// client, and either a real Postgres repo or an in-memory fake.
type Deps struct {
	// Repo persists series, episodes, wanted, queue, and history.
	Repo dom.Repo
	// Indexers are searched (fan-out) for candidate releases.
	Indexers []indexers.Searcher
	// Client is the download client that fetches the chosen release.
	Client downloads.Client
	// MediaRoot is where imported files are placed (e.g. /media/tv).
	MediaRoot string
	// DefaultProfile is used when a series has no profile set.
	DefaultProfile string
}

// Service implements the TV use-cases.
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

// AddSeries creates a series (if new) and records the add in history. The
// monitored flag sets the series-level monitor toggle (PLAN §4): a monitored
// series is wanted by default; an unmonitored series is added quietly and is
// wanted only when a season or episode is explicitly monitored. Returns the
// series id.
func (s *Service) AddSeries(ctx context.Context, title, year, qualityProfile string, monitored bool) (int64, error) {
	if strings.TrimSpace(title) == "" {
		return 0, fmt.Errorf("tv: title is required")
	}
	if qualityProfile == "" {
		qualityProfile = s.deps.DefaultProfile
	}
	series := dom.Series{
		Title:          strings.TrimSpace(title),
		Year:           parseYear(year),
		QualityProfile: qualityProfile,
		Monitored:      monitored,
	}
	id, err := s.deps.Repo.CreateSeries(ctx, series)
	if err != nil {
		return 0, err
	}
	_ = s.deps.Repo.AddHistory(ctx, dom.HistoryEntry{
		SeriesID: id, Event: "added", Detail: "series added to library",
	})
	return id, nil
}

// AddEpisode creates a known episode within a series (if new) and, when the
// episode is monitored, ensures a pending wanted row for it. Returns the
// episode id.
func (s *Service) AddEpisode(ctx context.Context, seriesID int64, season, episode int, title string, monitored bool) (int64, error) {
	e := dom.Episode{
		SeriesID:  seriesID,
		Season:    season,
		Episode:   episode,
		Title:     title,
		Monitored: monitored,
	}
	id, err := s.deps.Repo.CreateEpisode(ctx, e)
	if err != nil {
		return 0, err
	}
	if monitored {
		if err := s.deps.Repo.EnsureWanted(ctx, seriesID, season, episode); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// ListSeries returns all series (used by GET /api/v1/tv).
func (s *Service) ListSeries(ctx context.Context) ([]dom.Series, error) {
	return s.deps.Repo.ListSeries(ctx)
}

// GetSeries loads a series (used by GET /api/v1/tv/{id}).
func (s *Service) GetSeries(ctx context.Context, id int64) (*dom.Series, error) {
	return s.deps.Repo.GetSeries(ctx, id)
}

// ListEpisodes returns a series' known episodes (used by
// GET /api/v1/tv/{id}/episodes).
func (s *Service) ListEpisodes(ctx context.Context, seriesID int64) ([]dom.Episode, error) {
	return s.deps.Repo.ListEpisodes(ctx, seriesID)
}

// ListWanted returns a series' wanted episodes with their status (used by
// GET /api/v1/tv/{id}/wanted).
func (s *Service) ListWanted(ctx context.Context, seriesID int64) ([]dom.Wanted, error) {
	return s.deps.Repo.ListWanted(ctx, seriesID)
}

// SetEpisodeMonitored toggles a single episode's monitoring; when turning on,
// it also ensures a pending wanted row (so a re-monitored episode is wanted
// again).
func (s *Service) SetEpisodeMonitored(ctx context.Context, seriesID int64, season, episode int, monitored bool) error {
	if err := s.deps.Repo.SetEpisodeMonitored(ctx, seriesID, season, episode, monitored); err != nil {
		return err
	}
	if monitored {
		return s.deps.Repo.EnsureWanted(ctx, seriesID, season, episode)
	}
	return nil
}

// SetSeriesMonitored toggles the series-level monitor (PLAN §4) and propagates
// it to every known episode of the series, ensuring a pending wanted row for
// each on toggle-on. The wanted rows are the pipeline's work queue (PLAN §4:
// the worker creates/updates wanted entries). A series with no known episodes
// simply records the toggle; episodes added later default to monitored via the
// API, so the intent is not lost.
func (s *Service) SetSeriesMonitored(ctx context.Context, seriesID int64, monitored bool) error {
	if err := s.deps.Repo.SetSeriesMonitored(ctx, seriesID, monitored); err != nil {
		return err
	}
	eps, err := s.deps.Repo.ListEpisodes(ctx, seriesID)
	if err != nil {
		return err
	}
	for _, e := range eps {
		if err := s.SetEpisodeMonitored(ctx, seriesID, e.Season, e.Episode, monitored); err != nil {
			return err
		}
	}
	event := "monitored"
	if !monitored {
		event = "unmonitored"
	}
	s.record(ctx, seriesID, event, "series-level toggle")
	return nil
}

// SetSeasonMonitored toggles monitoring for a single season (PLAN §4) by
// fanning out to every known episode in that season, ensuring a pending wanted
// row for each on toggle-on. Seasons are implicit (the model has no season
// rows), so a season with no known episodes yet is a no-op — episodes added
// later default to monitored via the API.
func (s *Service) SetSeasonMonitored(ctx context.Context, seriesID int64, season int, monitored bool) error {
	eps, err := s.deps.Repo.ListEpisodes(ctx, seriesID)
	if err != nil {
		return err
	}
	matched := false
	for _, e := range eps {
		if e.Season != season {
			continue
		}
		matched = true
		if err := s.SetEpisodeMonitored(ctx, seriesID, e.Season, e.Episode, monitored); err != nil {
			return err
		}
	}
	if matched {
		event := "season-monitored"
		if !monitored {
			event = "season-unmonitored"
		}
		s.record(ctx, seriesID, event, fmt.Sprintf("season %d", season))
	}
	return nil
}

// RunPipeline drives the full pipeline for one series: it services every
// pending wanted episode by searching indexers, matching the best release
// (grouped by the episodes each release covers), downloading it, importing the
// covered episodes under the media root, and marking them satisfied.
//
// It is idempotent: episodes already satisfied are skipped, and a series with
// no pending wanted episodes returns ErrNoWanted without searching.
func (s *Service) RunPipeline(ctx context.Context, seriesID int64) (int, error) {
	series, err := s.deps.Repo.GetSeries(ctx, seriesID)
	if err != nil {
		return 0, err
	}

	pending, err := s.pendingWanted(ctx, seriesID)
	if err != nil {
		return 0, err
	}
	if len(pending) == 0 {
		return 0, ErrNoWanted
	}

	if s.deps.Client == nil {
		s.record(ctx, seriesID, "no-client", "no download client configured")
		return 0, fmt.Errorf("tv: no download client configured")
	}

	// 1. Search.
	results, err := s.searchAll(ctx, series.Title, series.Year)
	if err != nil {
		s.record(ctx, seriesID, "search", "search failed: "+err.Error())
		return 0, err
	}
	s.record(ctx, seriesID, "searched", fmt.Sprintf("%d candidate release(s)", len(results)))

	// 2. Build quality-matched candidates.
	cands := s.buildCandidates(series, results)
	if len(cands) == 0 {
		s.record(ctx, seriesID, "no-match", "no release satisfied the profile")
		return 0, ErrNoMatch
	}

	// 3. Group by the episodes each release covers, and for each wanted
	//    episode pick the best covering release. A single multi-episode release
	//    can therefore satisfy several wanted entries at once.
	imported, err := s.serviceEpisodes(ctx, series, cands, pending)
	if err != nil {
		return 0, err
	}
	return imported, nil
}

// pendingWanted returns the series' monitored episodes that still have a
// pending wanted row (i.e. the ones the pipeline must service).
func (s *Service) pendingWanted(ctx context.Context, seriesID int64) ([]dom.Wanted, error) {
	wants, err := s.deps.Repo.ListWanted(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	var out []dom.Wanted
	for _, w := range wants {
		if w.Status != dom.WantedPending {
			continue
		}
		out = append(out, w)
	}
	return out, nil
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

// buildCandidates parses and quality-matches every indexer result, dropping
// releases that do not parse or do not satisfy the series' profile.
func (s *Service) buildCandidates(series *dom.Series, results []indexers.SearchResult) []dom.CandidateRelease {
	profileName := series.QualityProfile
	if profileName == "" {
		profileName = s.deps.DefaultProfile
	}
	var cands []dom.CandidateRelease
	for _, r := range results {
		c, ok := dom.BuildCandidate(r.Title, r.Indexer, profileName)
		if !ok {
			continue
		}
		cands = append(cands, c)
	}
	return cands
}

// serviceEpisodes picks the best covering release for each pending wanted
// episode, downloads it once per distinct release, imports the covered
// episodes, and marks them satisfied. It returns the number of episodes
// imported.
func (s *Service) serviceEpisodes(ctx context.Context, series *dom.Series, cands []dom.CandidateRelease, pending []dom.Wanted) (int, error) {
	// Best release per wanted episode.
	type pick struct {
		rel dom.CandidateRelease
		// epTitles maps covered (season,episode) -> episode title for import naming.
	}
	var picks []pick
	for _, w := range pending {
		best, ok := dom.BestForEpisode(cands, w.Season, w.Episode)
		if !ok {
			// Nothing covers this episode; leave it pending.
			continue
		}
		picks = append(picks, pick{rel: best})
	}
	if len(picks) == 0 {
		return 0, ErrNoMatch
	}

	// Collapse to distinct releases (a multi-ep release satisfies several
	// wanted episodes; we only download it once).
	type key struct {
		title   string
		indexer string
	}
	byRelease := make(map[key][]pick)
	var order []key
	for _, p := range picks {
		k := key{title: p.rel.Title, indexer: p.rel.Indexer}
		if _, seen := byRelease[k]; !seen {
			order = append(order, k)
		}
		byRelease[k] = append(byRelease[k], p)
	}

	imported := 0
	for _, k := range order {
		group := byRelease[k]
		rel := group[0].rel

		// The wanted episodes this release covers (intersect with what was
		// actually wanted and is pending).
		covered := coveredWanted(rel.Parsed, pending)
		if len(covered) == 0 {
			continue
		}

		// Download once.
		queueID, err := s.sendToClient(ctx, series, rel)
		if err != nil {
			s.record(ctx, series.ID, "download-failed", err.Error())
			continue
		}

		// Import each covered episode from the downloaded file.
		didImport, err := s.importCovered(ctx, series, rel, covered, queueID)
		if err != nil {
			s.record(ctx, series.ID, "import-failed", err.Error())
			continue
		}
		if didImport == 0 {
			// Nothing actually landed (e.g. all collisions); leave the queue
			// as complete but don't satisfy.
			continue
		}

		// Satisfy every covered episode.
		for _, w := range covered {
			if err := s.deps.Repo.MarkWantedSatisfied(ctx, series.ID, w.Season, w.Episode, rel.Title); err != nil {
				s.record(ctx, series.ID, "satisfy-failed", err.Error())
				continue
			}
			imported++
		}
		s.record(ctx, series.ID, "imported",
			fmt.Sprintf("imported %d episode(s) from %q", didImport, rel.Title))
	}

	if imported == 0 {
		return 0, ErrNoMatch
	}
	return imported, nil
}

// coveredWanted returns the pending wanted episodes that the given parsed
// release actually delivers. For discrete-episode releases it matches on the
// (season, episode) list; for a season pack it matches every pending wanted in
// the same season.
func coveredWanted(parsed dom.ParsedRelease, pending []dom.Wanted) []dom.Wanted {
	if len(parsed.Episodes) > 0 {
		set := make(map[dom.EpisodeRef]bool)
		for _, e := range dom.CoveredEpisodes(parsed) {
			set[e] = true
		}
		var out []dom.Wanted
		for _, w := range pending {
			if set[dom.NewEpisodeRef(w.Season, w.Episode)] {
				out = append(out, w)
			}
		}
		return out
	}
	if dom.IsSeasonPack(parsed) {
		var out []dom.Wanted
		for _, w := range pending {
			if w.Season == parsed.Season {
				out = append(out, w)
			}
		}
		return out
	}
	return nil
}

// sendToClient queues the release, sends it to the download client, and records
// the queue entry. Returns the queue entry id.
func (s *Service) sendToClient(ctx context.Context, series *dom.Series, rel dom.CandidateRelease) (int64, error) {
	qe := dom.QueueEntry{
		SeriesID:       series.ID,
		Season:         rel.Parsed.Season,
		ReleaseTitle:   rel.Title,
		Indexer:        rel.Indexer,
		DownloadClient: s.deps.Client.Name(),
		State:          dom.QueueQueued,
		Progress:       0,
	}
	id, err := s.deps.Repo.CreateQueue(ctx, qe)
	if err != nil {
		return 0, err
	}
	qe.ID = id
	qe.State = dom.QueueDownloading
	_ = s.deps.Repo.UpdateQueue(ctx, qe)

	if err := s.deps.Client.Add(ctx, downloads.Release{
		Title:    rel.Title,
		FileName: fileNameFor(rel.Parsed, series),
	}); err != nil {
		qe.State = dom.QueueFailed
		_ = s.deps.Repo.UpdateQueue(ctx, qe)
		return 0, err
	}
	return id, nil
}

// importCovered polls the client until the release is complete, then imports
// every covered episode (a multi-episode release is distributed across its
// covered episodes). Returns how many episodes were imported and marks the
// queue complete. Collisions on individual episodes are skipped (the existing
// file is kept) without failing the whole import.
func (s *Service) importCovered(ctx context.Context, series *dom.Series, rel dom.CandidateRelease, covered []dom.Wanted, queueID int64) (int, error) {
	file, err := s.waitComplete(ctx, rel.Title)
	if err != nil {
		return 0, err
	}
	if file == "" {
		return 0, fmt.Errorf("tv: download did not produce a file for %q", rel.Title)
	}

	// Load episode titles for import naming (best-effort).
	epTitles := s.episodeTitles(ctx, series.ID, covered)

	imported := 0
	for _, w := range covered {
		plan, err := dom.PlanImport(s.deps.MediaRoot, series.Title, series.Year,
			w.Season, w.Episode, epTitles[w.Season][w.Episode], file)
		if err != nil {
			if errors.Is(err, dom.ErrCollision) {
				// An episode file already exists; keep it, skip this one.
				imported++
				continue
			}
			return imported, err
		}
		if err := copyFile(plan.SourcePath, plan.DestPath); err != nil {
			return imported, err
		}
		imported++
	}

	// Remove the source file now that it has been distributed.
	_ = os.Remove(file)

	qe := dom.QueueEntry{ID: queueID, State: dom.QueueComplete, Progress: 100}
	if err := s.deps.Repo.UpdateQueue(ctx, qe); err != nil {
		return imported, err
	}
	return imported, nil
}

// episodeTitles loads the stored title for each covered (season, episode).
// Episodes that were never registered (e.g. discovered only via the release)
// map to "".
func (s *Service) episodeTitles(ctx context.Context, seriesID int64, covered []dom.Wanted) map[int]map[int]string {
	out := make(map[int]map[int]string)
	for _, w := range covered {
		ep, err := s.deps.Repo.GetEpisode(ctx, seriesID, w.Season, w.Episode)
		if err != nil {
			continue
		}
		if out[w.Season] == nil {
			out[w.Season] = make(map[int]string)
		}
		out[w.Season][w.Episode] = ep.Title
	}
	return out
}

// waitComplete polls the download client until the release reports complete
// (bounded by a deadline) and returns the file path.
func (s *Service) waitComplete(ctx context.Context, title string) (string, error) {
	deadline := time.Now().Add(30 * time.Second)
	for {
		st, err := s.deps.Client.Status(ctx, title)
		if err != nil {
			return "", err
		}
		if st.Complete && st.File != "" {
			return st.File, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("tv: timed out waiting for download of %q", title)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// record appends a history entry, ignoring errors (history is best-effort).
func (s *Service) record(ctx context.Context, seriesID int64, event, detail string) {
	_ = s.deps.Repo.AddHistory(ctx, dom.HistoryEntry{
		SeriesID: seriesID, Event: event, Detail: detail,
	})
}

// fileNameFor derives a safe download-client file name from a parsed release
// and series: a cleaned scene name plus an assumed container extension.
func fileNameFor(p dom.ParsedRelease, series *dom.Series) string {
	name := series.Title
	if name == "" {
		name = p.Title
	}
	if name == "" {
		name = "release"
	}
	name = strings.ReplaceAll(name, ".", " ")
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.Join(strings.Fields(name), " ")
	season := ""
	if p.Season > 0 {
		season = fmt.Sprintf(" S%02d", p.Season)
	}
	return strings.ToLower(name) + season + ".mkv"
}

// copyFile copies src to dst, creating dst's directory as needed.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// parseYear converts a year string to an int (0 if absent/invalid).
func parseYear(s string) int {
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n); err != nil {
		return 0
	}
	return n
}
