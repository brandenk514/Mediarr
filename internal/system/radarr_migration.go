package system

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Radarr R6: the Radarr drop-in migration (the final Radarr phase).
//
// RadarrMigrator is the Radarr-side counterpart of the P6 Sonarr Migrator:
// it pulls a live Radarr instance over the v3 REST API (movies + editions +
// quality profiles + tags + indexers + download client + a best-effort
// settings subset) and imports it into Mediarr's movie domain, returning a
// mapping report. It carries the same guarantees as the Sonarr run:
//
//   - dry-run:  report what WOULD be imported without writing anything.
//   - idempotent: re-running converges — an already-imported entity maps to
//     action "exists" and is never duplicated (movies are keyed by
//     title+year, the movie domain's identity, like the R5 import bridge).
//   - resumable: a partial run records which movies were migrated;
//     resume=true skips those and continues.
//
// The Radarr API is reached through the swappable RadarrClient seam, so the
// mapping / dry-run / idempotency / resume logic is unit-tested against a
// mocked Radarr API (no live external calls in tests). Importing into
// Mediarr is the swappable RadarrMigrationTarget seam.
//
// The shared MigrationInclude flags apply to Radarr as series -> movies and
// episodes -> editions (the spec's include shape is unchanged in R6).
// ---------------------------------------------------------------------------

// Migration source applications for the shared MigrationRequest.source.
const (
	SourceSonarr = "sonarr"
	SourceRadarr = "radarr"
)

// ---------------------------------------------------------------------------
// Radarr v3 API source types (the shapes Radarr's REST endpoints return).
// The shapes are modeled on the documented Radarr v3 surface and kept
// lenient: unknown fields are ignored on decode.
// ---------------------------------------------------------------------------

// RadarrQuality is one quality level reference in a Radarr quality profile.
type RadarrQuality struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// RadarrProfileItem is one approved/not-approved quality in a Radarr profile.
type RadarrProfileItem struct {
	Quality  RadarrQuality `json:"quality"`
	Approved bool          `json:"approved"`
}

// RadarrQualityProfile is a Radarr movie quality profile.
type RadarrQualityProfile struct {
	ID     int                 `json:"id"`
	Name   string              `json:"name"`
	Cutoff int                 `json:"cutoff"`
	Items  []RadarrProfileItem `json:"items"`
}

// RadarrTag is a Radarr tag.
type RadarrTag struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

// RadarrIndexer is a Radarr indexer. The four booleans are the per-indexer
// search toggles (Radarr R5a in Mediarr stores them as the R5a indexer
// settings: rss / auto_search / interactive_search).
type RadarrIndexer struct {
	ID                int    `json:"id"`
	Name              string `json:"name"`
	Enable            bool   `json:"enable"`
	EnableRss         bool   `json:"enableRss"`
	EnableAutomatic   bool   `json:"enableAutomatic"`
	EnableInteractive bool   `json:"enableInteractive"`
	IndexerType       string `json:"indexerType"`
	APIKey            string `json:"apiKey"`
	BaseURL           string `json:"baseUrl"`
}

// RadarrDownloadClient is a Radarr download client.
type RadarrDownloadClient struct {
	ID        int                        `json:"id"`
	Name      string                     `json:"name"`
	Enable    bool                       `json:"enable"`
	ClientKey string                     `json:"clientKey"`
	Fields    map[string]json.RawMessage `json:"fields,omitempty"`
}

// RadarrEdition is one edition of a Radarr movie (Radarr v6 editions).
type RadarrEdition struct {
	ID               int    `json:"id"`
	Label            string `json:"label"`
	Monitored        bool   `json:"monitored"`
	Quality          int    `json:"quality"`
	QualityProfileID int    `json:"qualityProfileId"`
	Size             int64  `json:"size"`
	HasFile          bool   `json:"hasFile"`
}

// RadarrMovie is a Radarr movie (the v6 movie resource).
type RadarrMovie struct {
	ID               int             `json:"id"`
	Title            string          `json:"title"`
	OriginalTitle    string          `json:"originalTitle"`
	SortTitle        string          `json:"sortTitle"`
	Year             int             `json:"year"`
	Monitored        bool            `json:"monitored"`
	QualityProfileID int             `json:"qualityProfileId"`
	CollectionID     int             `json:"collectionId"`
	Genres           []string        `json:"genres"`
	Tags             []int           `json:"tags"`
	Path             string          `json:"path"`
	Status           string          `json:"status"`
	Editions         []RadarrEdition `json:"editions"`
}

// RadarrMovieNaming is the Radarr movieNaming config block.
type RadarrMovieNaming struct {
	RenameMovies             bool   `json:"renameMovies"`
	ReplaceIllegalCharacters bool   `json:"replaceIllegalCharacters"`
	ColonReplacement         string `json:"colonReplacement"`
	StandardFormat           string `json:"standardFormat"`
}

// RadarrFileManagement is the Radarr fileManagement config block (the
// subset the Mediarr media-management document can hold).
type RadarrFileManagement struct {
	MinFileAgeMinutes      int  `json:"minFileAge"`
	ImportExtraFiles       bool `json:"importExtraFiles"`
	UnmonitorDeletedMovies bool `json:"unmonitorDeletedMovies"`
}

// RadarrMediaManagement is GET /api/v3/config/mediaManagement.
type RadarrMediaManagement struct {
	MovieNaming    RadarrMovieNaming    `json:"movieNaming"`
	FileManagement RadarrFileManagement `json:"fileManagement"`
}

// RadarrImportExport is GET /api/v3/config/importexport when present
// (best-effort: a Radarr without it degrades to a report note).
type RadarrImportExport struct {
	Script string `json:"script"`
	Sync   bool   `json:"sync"`
}

// ---------------------------------------------------------------------------
// RadarrClient: the swappable source seam.
// ---------------------------------------------------------------------------

// RadarrClient is the read surface of a Radarr instance's v3 API. It is the
// seam that lets the RadarrMigrator run against a mocked Radarr in tests.
type RadarrClient interface {
	GetMovies(ctx context.Context) ([]RadarrMovie, error)
	GetQualityProfiles(ctx context.Context) ([]RadarrQualityProfile, error)
	GetTags(ctx context.Context) ([]RadarrTag, error)
	GetIndexers(ctx context.Context) ([]RadarrIndexer, error)
	GetDownloadClients(ctx context.Context) ([]RadarrDownloadClient, error)
	GetMediaManagement(ctx context.Context) (*RadarrMediaManagement, error)
	// GetImportExport is best-effort: a 404 (endpoint absent) yields
	// (nil, nil) so a missing config degrades to a report note instead of
	// failing the run.
	GetImportExport(ctx context.Context) (*RadarrImportExport, error)
}

// NewRadarrClient builds the default HTTP RadarrClient for a live instance.
// apiKey is sent as the X-Api-Key header on every call.
func NewRadarrClient(baseURL, apiKey string) RadarrClient {
	return &httpRadarrClient{base: baseURL, apiKey: apiKey, http: &http.Client{Timeout: 30 * time.Second}}
}

type httpRadarrClient struct {
	base   string
	apiKey string
	http   *http.Client
}

func (c *httpRadarrClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("radarr: %s returned %d: %s", path, resp.StatusCode, truncate(string(body), 200))
	}
	return json.Unmarshal(body, out)
}

func (c *httpRadarrClient) GetMovies(ctx context.Context) ([]RadarrMovie, error) {
	var out []RadarrMovie
	if err := c.get(ctx, "/api/v3/movie", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *httpRadarrClient) GetQualityProfiles(ctx context.Context) ([]RadarrQualityProfile, error) {
	var out []RadarrQualityProfile
	if err := c.get(ctx, "/api/v3/profile", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *httpRadarrClient) GetTags(ctx context.Context) ([]RadarrTag, error) {
	var out []RadarrTag
	if err := c.get(ctx, "/api/v3/tag", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *httpRadarrClient) GetIndexers(ctx context.Context) ([]RadarrIndexer, error) {
	var out []RadarrIndexer
	if err := c.get(ctx, "/api/v3/indexer", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *httpRadarrClient) GetDownloadClients(ctx context.Context) ([]RadarrDownloadClient, error) {
	var out []RadarrDownloadClient
	if err := c.get(ctx, "/api/v3/downloadclient", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *httpRadarrClient) GetMediaManagement(ctx context.Context) (*RadarrMediaManagement, error) {
	var out RadarrMediaManagement
	if err := c.get(ctx, "/api/v3/config/mediaManagement", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *httpRadarrClient) GetImportExport(ctx context.Context) (*RadarrImportExport, error) {
	var out RadarrImportExport
	err := c.get(ctx, "/api/v3/config/importexport", &out)
	if err != nil {
		// A 404 means the endpoint is absent on this Radarr — degrade to a
		// report note (the caller gets nil, nil). Any other failure is real.
		if isHTTPStatusError(err, http.StatusNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

// isHTTPStatusError reports whether err is a Radarr client non-2xx error for
// the given status (the client formats "radarr: <path> returned <status>: …").
func isHTTPStatusError(err error, status int) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), fmt.Sprintf("returned %d", status))
}

// ---------------------------------------------------------------------------
// RadarrMigrationTarget: the swappable Mediarr import seam (movie domain).
// ---------------------------------------------------------------------------

// RadarrMovieRef is the minimal movie row the RadarrMovieStore needs to
// import. QualityProfile is the resolved movie quality-profile NAME (empty
// = the target's default); the store must not see source profile ids.
type RadarrMovieRef struct {
	Title          string
	Year           int
	QualityProfile string
	Monitored      bool
	Tags           []string
	// SeedEditionMonitored is the monitored state for the 'Theatrical'
	// edition the store seeds on create; nil follows the movie's flag.
	SeedEditionMonitored *bool
}

// RadarrEditionRef is one source edition to ensure on the target. Quality is
// the resolved quality-profile NAME for the edition.
type RadarrEditionRef struct {
	Label     string
	Monitored bool
	Quality   string
	Size      int64
}

// RadarrMigrationTarget is the write surface for importing a Radarr
// instance into Mediarr. Each Ensure* is idempotent: it creates the entity
// when absent (returns ActionCreated) and reports ActionExists when already
// present (never duplicating). The Postgres-backed target implements this;
// tests use a fake.
type RadarrMigrationTarget interface {
	EnsureMovieProfile(ctx context.Context, p RadarrQualityProfile) (id int64, action TargetAction, err error)
	EnsureTag(ctx context.Context, t RadarrTag) (id int64, action TargetAction, err error)
	EnsureIndexer(ctx context.Context, ix RadarrIndexer) (id int64, action TargetAction, err error)
	SetDownloadClient(ctx context.Context, c RadarrDownloadClient) (action TargetAction, err error)
	// ApplyMediaManagement maps the best-effort media-management subset and
	// returns the number of fields written.
	ApplyMediaManagement(ctx context.Context, mm RadarrMediaManagement) (int, error)
	EnsureMovie(ctx context.Context, ref RadarrMovieRef) (id int64, action TargetAction, err error)
	EnsureEditions(ctx context.Context, movieID int64, eds []RadarrEditionRef) (created int, err error)
}

// RadarrIndexerStore is the indexer-write surface (the R5a per-indexer
// booleans travel with the create). main.go adapts the Postgres indexer repo
// to it.
type RadarrIndexerStore interface {
	// FindIndexer returns the id of the indexer named name, or found=false.
	FindIndexer(ctx context.Context, name string) (id int64, found bool)
	// CreateIndexer inserts a new indexer definition and returns its id.
	CreateIndexer(ctx context.Context, name, kind, baseURL, apiKey string, enabled, enableRss, enableAutomatic, enableInteractive bool) (int64, error)
}

// RadarrMovieStore is the movie-domain write surface. main.go adapts the
// Postgres movie repo to it. AddMovie must create the movie with a seeded
// 'Theatrical' edition (following ref.SeedEditionMonitored, or the movie's
// monitored state when nil) and ensure a wanted row for a monitored movie —
// the same semantics as the movie add use-case.
type RadarrMovieStore interface {
	// FindMovie returns the id of an existing movie matching title+year
	// (the movie domain's identity), or found=false.
	FindMovie(ctx context.Context, title string, year int) (id int64, found bool)
	// AddMovie creates the movie and returns its id.
	AddMovie(ctx context.Context, ref RadarrMovieRef) (id int64, err error)
	// EnsureEdition upserts an edition by (movie id, label) and reports
	// whether the row was newly created.
	EnsureEdition(ctx context.Context, movieID int64, e RadarrEditionRef) (id int64, created bool, err error)
}

// ---------------------------------------------------------------------------
// RadarrMigrator.
// ---------------------------------------------------------------------------

// RadarrMigrator runs Radarr migrations. NewSource is the swappable client
// factory (tests inject a mock); Target is the Mediarr movie import surface.
type RadarrMigrator struct {
	NewSource func(baseURL, apiKey string) RadarrClient
	Target    RadarrMigrationTarget

	mu         sync.Mutex
	status     string
	running    bool
	startedAt  *time.Time
	finishedAt *time.Time
	done       int
	total      int
	lastResult *MigrationResult
	// migrated remembers migrated movie external keys across runs for resume.
	migrated map[string]bool
}

// NewRadarrMigrator builds a RadarrMigrator with the default HTTP source
// factory.
func NewRadarrMigrator(target RadarrMigrationTarget) *RadarrMigrator {
	return &RadarrMigrator{
		NewSource: NewRadarrClient,
		Target:    target,
		status:    "idle",
		migrated:  map[string]bool{},
	}
}

// Status returns a snapshot of the current Radarr migration status.
func (m *RadarrMigrator) Status() MigrationStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	var last json.RawMessage
	if m.lastResult != nil {
		b, _ := json.Marshal(m.lastResult)
		last = b
	}
	return MigrationStatus{
		Status:     m.status,
		Running:    m.running,
		StartedAt:  m.startedAt,
		FinishedAt: m.finishedAt,
		Done:       m.done,
		Total:      m.total,
		LastResult: last,
	}
}

// Run executes a Radarr migration. On a target/source error mid-run it
// records a partial (resumable) status and returns the mapping report for
// what completed.
func (m *RadarrMigrator) Run(ctx context.Context, req MigrationRequest) (*MigrationResult, error) {
	if req.SourceURL == "" {
		return nil, fmt.Errorf("migration: source_url is required")
	}
	if u, err := url.Parse(req.SourceURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("migration: source_url must be an http/https URL")
	}

	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return nil, fmt.Errorf("migration: a migration is already running")
	}
	now := time.Now()
	m.running = true
	m.status = "running"
	m.startedAt = &now
	m.finishedAt = nil
	m.done = 0
	m.mu.Unlock()

	result, runErr := m.runLocked(ctx, req)

	m.mu.Lock()
	m.running = false
	finished := time.Now()
	m.finishedAt = &finished
	// A partial run returns both a result (Resumable=true) and an error: the
	// resumable contract wins, so check it before the plain error case.
	if result != nil && result.Resumable {
		m.status = "partial"
	} else if runErr != nil {
		m.status = "failed"
	} else {
		m.status = "completed"
	}
	m.lastResult = result
	m.mu.Unlock()

	return result, runErr
}

func (m *RadarrMigrator) runLocked(ctx context.Context, req MigrationRequest) (*MigrationResult, error) {
	inc := req.Include
	if inc == nil {
		inc = &MigrationInclude{}
	}
	src := m.NewSource(req.SourceURL, req.APIKey)
	if m.migrated == nil {
		m.migrated = map[string]bool{}
	}
	result := &MigrationResult{
		DryRun:    req.DryRun,
		Resume:    req.Resume,
		SourceURL: req.SourceURL,
		Items:     []MigrationItem{},
	}

	// 1. Movie quality profiles. The target maps the source profile NAME
	// into the movie quality-profile document; a source profile that is not
	// among the seeded Radarr v6 defaults is created there and noted. The
	// id -> name map lets movie/edition qualityProfileId references resolve
	// to a local profile name.
	profileNamesByID := map[int]string{}
	if inc.wantProfiles() {
		profiles, err := src.GetQualityProfiles(ctx)
		if err != nil {
			return result, err
		}
		for _, p := range profiles {
			profileNamesByID[p.ID] = p.Name
			if req.DryRun {
				result.Items = append(result.Items, MigrationItem{
					Entity: "quality_profile", ExternalID: fmt.Sprint(p.ID), SourceTitle: p.Name, Action: "dry-run",
				})
				result.Counts.Total++
				continue
			}
			id, action, err := m.Target.EnsureMovieProfile(ctx, p)
			if err != nil {
				result.Counts.Total++
				result.Counts.Errors++
				result.Items = append(result.Items, MigrationItem{Entity: "quality_profile", ExternalID: fmt.Sprint(p.ID), SourceTitle: p.Name, Action: "error", Message: err.Error()})
				return result, err
			}
			it := MigrationItem{Entity: "quality_profile", ExternalID: fmt.Sprint(p.ID), SourceTitle: p.Name, Action: string(action), TargetID: id}
			if action == ActionCreated {
				// A created (not seeded) profile is the "unknown source
				// profile" case: note that it was created in the document.
				it.Message = "created in the movie profiles document (not a seeded Radarr v6 profile)"
			}
			addMigrated(result, it)
		}
	}

	// 2. Tags (tag registry, source ids preserved for movie references).
	if inc.wantTags() {
		tags, err := src.GetTags(ctx)
		if err != nil {
			return result, err
		}
		for _, t := range tags {
			if req.DryRun {
				result.Items = append(result.Items, MigrationItem{Entity: "tag", ExternalID: fmt.Sprint(t.ID), SourceTitle: t.Label, Action: "dry-run"})
				result.Counts.Total++
				continue
			}
			id, action, err := m.Target.EnsureTag(ctx, t)
			if err != nil {
				result.Counts.Total++
				result.Counts.Errors++
				result.Items = append(result.Items, MigrationItem{Entity: "tag", ExternalID: fmt.Sprint(t.ID), SourceTitle: t.Label, Action: "error", Message: err.Error()})
				return result, err
			}
			addMigrated(result, MigrationItem{Entity: "tag", ExternalID: fmt.Sprint(t.ID), SourceTitle: t.Label, Action: string(action), TargetID: id})
		}
	}

	// 3. Indexers (the Radarr R5a per-indexer booleans: enableRss -> rss,
	// enableAutomatic -> auto_search, enableInteractive -> interactive_search).
	if inc.wantIndexers() {
		indexers, err := src.GetIndexers(ctx)
		if err != nil {
			return result, err
		}
		for _, ix := range indexers {
			if req.DryRun {
				result.Items = append(result.Items, MigrationItem{Entity: "indexer", ExternalID: fmt.Sprint(ix.ID), SourceTitle: ix.Name, Action: "dry-run"})
				result.Counts.Total++
				continue
			}
			id, action, err := m.Target.EnsureIndexer(ctx, ix)
			if err != nil {
				result.Counts.Total++
				result.Counts.Errors++
				result.Items = append(result.Items, MigrationItem{Entity: "indexer", ExternalID: fmt.Sprint(ix.ID), SourceTitle: ix.Name, Action: "error", Message: err.Error()})
				return result, err
			}
			addMigrated(result, MigrationItem{Entity: "indexer", ExternalID: fmt.Sprint(ix.ID), SourceTitle: ix.Name, Action: string(action), TargetID: id})
		}
	}

	// 4. Download client (single config, like Sonarr).
	if inc.wantDownloadClient() {
		clients, err := src.GetDownloadClients(ctx)
		if err != nil {
			return result, err
		}
		if len(clients) > 0 {
			dc := clients[0]
			if req.DryRun {
				result.Items = append(result.Items, MigrationItem{Entity: "download_client", ExternalID: fmt.Sprint(dc.ID), SourceTitle: dc.Name, Action: "dry-run"})
				result.Counts.Total++
			} else {
				action, err := m.Target.SetDownloadClient(ctx, dc)
				if err != nil {
					result.Counts.Total++
					result.Counts.Errors++
					result.Items = append(result.Items, MigrationItem{Entity: "download_client", ExternalID: fmt.Sprint(dc.ID), SourceTitle: dc.Name, Action: "error", Message: err.Error()})
					return result, err
				}
				addMigrated(result, MigrationItem{Entity: "download_client", ExternalID: fmt.Sprint(dc.ID), SourceTitle: dc.Name, Action: string(action)})
			}
		}
	}

	// 5. Settings (Radarr R6 only; a best-effort subset: media management
	// fields the Mediarr document can hold + an importexport note). A
	// failure degrades to a report note and never fails the run.
	if req.DryRun {
		result.Items = append(result.Items, MigrationItem{Entity: "settings", Action: "dry-run"})
		result.Counts.Total++
	} else {
		notes := make([]string, 0, 2)
		action := TargetAction("exists")
		if mm, err := src.GetMediaManagement(ctx); err != nil {
			notes = append(notes, "media management unavailable: "+err.Error())
		} else if n, err := m.Target.ApplyMediaManagement(ctx, *mm); err != nil {
			notes = append(notes, "media management failed: "+err.Error())
			result.Counts.Errors++
		} else {
			if n > 0 {
				action = ActionUpdated
			}
			notes = append(notes, fmt.Sprintf("media management: %d fields mapped", n))
		}
		if ie, err := src.GetImportExport(ctx); err != nil {
			notes = append(notes, "importexport unavailable: "+err.Error())
		} else if ie != nil {
			notes = append(notes, "importexport: script configured on source (not portable; import manually)")
		} else {
			notes = append(notes, "importexport: not configured on source")
		}
		addMigrated(result, MigrationItem{Entity: "settings", Action: string(action), Message: strings.Join(notes, "; ")})
	}

	// 6. Movies + editions (include.series -> movies, include.episodes ->
	// editions).
	if inc.wantSeries() {
		movies, err := src.GetMovies(ctx)
		if err != nil {
			return result, err
		}
		m.mu.Lock()
		m.total = len(movies)
		m.mu.Unlock()

		for _, mv := range movies {
			key := movieKey(mv)
			m.mu.Lock()
			migrated := m.migrated[key]
			resumeSkip := req.Resume && migrated
			m.done++
			m.mu.Unlock()

			if resumeSkip {
				result.Items = append(result.Items, MigrationItem{Entity: "movie", ExternalID: key, SourceTitle: mv.Title, Action: "skipped", Message: "already migrated (resume)"})
				result.Counts.Total++
				result.Counts.Skipped++
				continue
			}

			if req.DryRun {
				result.Items = append(result.Items, MigrationItem{Entity: "movie", ExternalID: key, SourceTitle: mv.Title, Action: "dry-run"})
				result.Counts.Total++
				if inc.wantEpisodes() {
					for _, e := range mv.Editions {
						result.Items = append(result.Items, MigrationItem{Entity: "edition", ExternalID: editionKey(e), SourceTitle: mv.Title + " " + e.Label, Action: "dry-run"})
						result.Counts.Total++
					}
				}
				continue
			}

			movieName, note := resolveMovieProfile(profileNamesByID, mv.QualityProfileID)
			labels, err := tagLabelList(m.Target, ctx, mv.Tags)
			if err != nil {
				return result, err
			}
			ref := RadarrMovieRef{
				Title:          mv.Title,
				Year:           mv.Year,
				QualityProfile: movieName,
				Monitored:      mv.Monitored,
				Tags:           labels,
			}
			// Seed the 'Theatrical' edition with the source's Theatrical
			// monitored state when present, else the movie-level flag.
			if th := theatricalMonitored(mv.Editions); th != nil {
				ref.SeedEditionMonitored = th
			}
			id, action, err := m.Target.EnsureMovie(ctx, ref)
			if err != nil {
				result.Counts.Total++
				result.Counts.Errors++
				it := MigrationItem{Entity: "movie", ExternalID: key, SourceTitle: mv.Title, Action: "error", Message: err.Error()}
				if note != "" {
					it.Message = note + "; " + err.Error()
				}
				result.Items = append(result.Items, it)
				// A movie failure is partial + resumable: stop, remember
				// what succeeded so far, and let a resume continue.
				result.Resumable = true
				return result, err
			}
			item := MigrationItem{Entity: "movie", ExternalID: key, SourceTitle: mv.Title, Action: string(action), TargetID: id}
			if note != "" {
				item.Message = note
			}
			addMigrated(result, item)

			if inc.wantEpisodes() && len(mv.Editions) > 0 {
				eds := make([]RadarrEditionRef, 0, len(mv.Editions))
				for _, e := range mv.Editions {
					eds = append(eds, RadarrEditionRef{
						Label:     e.Label,
						Monitored: e.Monitored,
						Quality:   resolveEditionProfile(profileNamesByID, e, movieName),
						Size:      e.Size,
					})
				}
				created, err := m.Target.EnsureEditions(ctx, id, eds)
				if err != nil {
					result.Counts.Total++
					result.Counts.Errors++
					result.Items = append(result.Items, MigrationItem{Entity: "edition", ExternalID: key, SourceTitle: mv.Title, Action: "error", Message: err.Error()})
					result.Resumable = true
					return result, err
				}
				// Map the created count to a per-edition action so an
				// idempotent re-run reports "exists" (0 created) and a
				// fresh run reports "created" (all created).
				edAction := TargetAction("exists")
				if created == len(eds) && len(eds) > 0 {
					edAction = ActionCreated
				} else if created > 0 && created < len(eds) {
					edAction = ActionUpdated
				}
				for _, e := range mv.Editions {
					result.Items = append(result.Items, MigrationItem{Entity: "edition", ExternalID: editionKey(e), SourceTitle: mv.Title + " " + e.Label, Action: string(edAction), TargetID: id})
					result.Counts.Total++
					switch edAction {
					case ActionCreated:
						result.Counts.Created++
					case ActionExists, ActionUpdated:
						result.Counts.Existing++
					}
				}
			}

			m.mu.Lock()
			m.migrated[key] = true
			m.mu.Unlock()
		}
	}

	return result, nil
}

// movieKey is the stable external key for a migrated movie: title+year, the
// movie domain's identity (the same keying the R5 import bridge uses).
func movieKey(m RadarrMovie) string {
	return fmt.Sprintf("movie:%s|%d", m.Title, m.Year)
}

// editionKey is the stable external key for a report item: the source
// edition id when set, else the label.
func editionKey(e RadarrEdition) string {
	if e.ID > 0 {
		return fmt.Sprintf("edition:%d", e.ID)
	}
	return fmt.Sprintf("edition:%s", e.Label)
}

// resolveMovieProfile maps a movie's source qualityProfileId to the local
// movie quality-profile name. An id that is not in the source profile list
// (or absent) resolves to "" — the target applies its own document default —
// and a report note for the unexpected id case.
func resolveMovieProfile(namesByID map[int]string, sourceID int) (name, note string) {
	if n, ok := namesByID[sourceID]; ok {
		return n, ""
	}
	if sourceID > 0 {
		return "", fmt.Sprintf("quality profile id %d not in source profiles; using the target's default profile", sourceID)
	}
	return "", ""
}

// resolveEditionProfile maps an edition's per-edition qualityProfileId to a
// local profile name, falling back to the movie's resolved name.
func resolveEditionProfile(namesByID map[int]string, e RadarrEdition, movieName string) string {
	if e.QualityProfileID > 0 {
		if n, ok := namesByID[e.QualityProfileID]; ok {
			return n
		}
	}
	return movieName
}

// theatricalMonitored returns the monitored state of a 'Theatrical' edition
// (the one the target seeds on create), or nil when the source movie has no
// such edition.
func theatricalMonitored(eds []RadarrEdition) *bool {
	for _, e := range eds {
		if equalFoldASCII(e.Label, "Theatrical") {
			b := e.Monitored
			return &b
		}
	}
	return nil
}

func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// tagLabelList resolves source tag ids to labels via the target's tag
// registry (the target preserves source tag ids on import).
func tagLabelList(t RadarrMigrationTarget, ctx context.Context, ids []int) ([]string, error) {
	// The target's EnsureTag calls are the write path; for label lookup the
	// target exposes the registry through RadarrTagStore when present.
	if s, ok := t.(RadarrTagStore); ok {
		return s.TagLabels(ctx, ids)
	}
	out := make([]string, 0, len(ids))
	return out, nil
}

// RadarrTagStore is an optional target capability: label lookup for source
// tag ids (the real target has the tag registry in the settings document).
type RadarrTagStore interface {
	TagLabels(ctx context.Context, ids []int) ([]string, error)
}

// ---------------------------------------------------------------------------
// MigrationService: routes the shared migration endpoints to the source.
// ---------------------------------------------------------------------------

// MigrationRunner runs one source's drop-in migration and reports its status.
type MigrationRunner interface {
	Run(ctx context.Context, req MigrationRequest) (*MigrationResult, error)
	Status() MigrationStatus
}

// MigrationService routes the drop-in migration endpoints to the source
// selected by MigrationRequest.source (R6: 'sonarr' by default, or
// 'radarr'). Status reflects the most recently run source, so
// GET /system/migration/status shows the run the user just kicked off.
type MigrationService struct {
	Sonarr MigrationRunner
	Radarr MigrationRunner

	mu   sync.Mutex
	last MigrationRunner
}

// NewMigrationService builds the router. A nil runner for a source answers a
// validation error (400 at the API) when that source is selected — main.go
// wires both.
func NewMigrationService(sonarr, radarr MigrationRunner) *MigrationService {
	return &MigrationService{Sonarr: sonarr, Radarr: radarr, last: sonarr}
}

// Run validates the source, routes to that source's runner, and records it
// as the last run.
func (s *MigrationService) Run(ctx context.Context, req MigrationRequest) (*MigrationResult, error) {
	if req.Source == "" {
		req.Source = SourceSonarr
	}
	var r MigrationRunner
	switch req.Source {
	case SourceSonarr:
		r = s.Sonarr
	case SourceRadarr:
		r = s.Radarr
	default:
		return nil, fmt.Errorf("migration: unknown source %q (want sonarr or radarr)", req.Source)
	}
	if r == nil {
		return nil, fmt.Errorf("migration: the %s migration is not wired", req.Source)
	}
	s.mu.Lock()
	s.last = r
	s.mu.Unlock()
	return r.Run(ctx, req)
}

// Status reports the most recently run source's status (idle until the first
// run).
func (s *MigrationService) Status() MigrationStatus {
	s.mu.Lock()
	r := s.last
	s.mu.Unlock()
	if r == nil {
		return MigrationStatus{Status: "idle"}
	}
	return r.Status()
}
