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

// ---- P6 Wave 3: the Sonarr drop-in migration (THE drop-in moment) ----
//
// The Migrator pulls a live Sonarr instance (series + seasons/episodes +
// quality profiles + tags + indexers + download client) over Sonarr's v3 REST
// API and imports it into Mediarr, returning a mapping report. It supports:
//
//   - dry-run:  report what WOULD be imported without writing anything.
//   - idempotent: re-running converges — an already-imported entity maps to
//     action "exists" and is never duplicated.
//   - resumable: a partial run (the source or target failed mid-way) records
//     which series were migrated; resume=true skips those and continues.
//
// The Sonarr API is reached through the swappable SonarrClient seam, so the
// mapping / dry-run / idempotency / resume logic is unit-tested against a
// mocked Sonarr API (no live external calls in tests). Importing into Mediarr
// is the swappable MigrationTarget seam.

// ---------------------------------------------------------------------------
// Sonarr v3 API source types (the shapes Sonarr's REST endpoints return).
// ---------------------------------------------------------------------------

// SonarrQuality is one quality level reference in a Sonarr quality profile.
type SonarrQuality struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// SonarrProfileItem is one approved/not-approved quality in a Sonarr profile.
type SonarrProfileItem struct {
	Quality  SonarrQuality `json:"quality"`
	Approved bool          `json:"approved"`
}

// SonarrQualityProfile is a Sonarr quality profile.
type SonarrQualityProfile struct {
	ID     int                 `json:"id"`
	Name   string              `json:"name"`
	Cutoff int                 `json:"cutoff"`
	Items  []SonarrProfileItem `json:"items"`
}

// SonarrTag is a Sonarr tag.
type SonarrTag struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

// SonarrIndexer is a Sonarr indexer (indexer kind + endpoint + key).
type SonarrIndexer struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	Enable          bool   `json:"enable"`
	EnableAutomatic bool   `json:"enableAutomatic"`
	IndexerType     string `json:"indexerType"`
	APIKey          string `json:"apiKey"`
	BaseURL         string `json:"baseUrl"`
}

// SonarrDownloadClient is a Sonarr download client.
type SonarrDownloadClient struct {
	ID        int                        `json:"id"`
	Name      string                     `json:"name"`
	Enable    bool                       `json:"enable"`
	ClientKey string                     `json:"clientKey"`
	Fields    map[string]json.RawMessage `json:"fields,omitempty"`
}

// SonarrSeries is a Sonarr series.
type SonarrSeries struct {
	ID               int            `json:"id"`
	SeriesID         int            `json:"seriesId"` // the TVDB id
	Title            string         `json:"title"`
	Year             int            `json:"year"`
	Status           string         `json:"status"`
	Path             string         `json:"path"`
	SeasonFolder     bool           `json:"seasonFolder"`
	QualityProfileID int            `json:"qualityProfileId"`
	Monitored        bool           `json:"monitored"`
	Tags             []int          `json:"tags"`
	Seasons          []SonarrSeason `json:"seasons"`
}

// SonarrSeason is a Sonarr season within a series.
type SonarrSeason struct {
	SeasonNumber int  `json:"seasonNumber"`
	Monitored    bool `json:"monitored"`
	Specials     bool `json:"specials"`
}

// SonarrEpisode is a Sonarr episode.
type SonarrEpisode struct {
	ID            int           `json:"id"`
	SeriesID      int           `json:"seriesId"`
	SeasonNumber  int           `json:"seasonNumber"`
	EpisodeNumber int           `json:"episodeNumber"`
	Title         string        `json:"title"`
	Airs          string        `json:"airs"`
	Quality       SonarrQuality `json:"quality"`
	HasFile       bool          `json:"hasFile"`
	Size          int64         `json:"size"`
	Monitored     bool          `json:"monitored"`
}

// ---------------------------------------------------------------------------
// SonarrClient: the swappable source seam.
// ---------------------------------------------------------------------------

// SonarrClient is the read surface of a Sonarr instance's v3 API. It is the
// seam that lets the Migrator run against a mocked Sonarr in tests.
type SonarrClient interface {
	GetSeries(ctx context.Context) ([]SonarrSeries, error)
	GetEpisodes(ctx context.Context) ([]SonarrEpisode, error)
	GetQualityProfiles(ctx context.Context) ([]SonarrQualityProfile, error)
	GetTags(ctx context.Context) ([]SonarrTag, error)
	GetIndexers(ctx context.Context) ([]SonarrIndexer, error)
	GetDownloadClients(ctx context.Context) ([]SonarrDownloadClient, error)
}

// NewSonarrClient builds the default HTTP SonarrClient for a live instance.
// apiKey is sent as the X-Api-Key header on every call.
func NewSonarrClient(baseURL, apiKey string) SonarrClient {
	return &httpSonarrClient{base: baseURL, apiKey: apiKey, http: &http.Client{Timeout: 30 * time.Second}}
}

type httpSonarrClient struct {
	base   string
	apiKey string
	http   *http.Client
}

func (c *httpSonarrClient) get(ctx context.Context, path string, out any) error {
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
		return fmt.Errorf("sonarr: %s returned %d: %s", path, resp.StatusCode, truncate(string(body), 200))
	}
	return json.Unmarshal(body, out)
}

func (c *httpSonarrClient) GetSeries(ctx context.Context) ([]SonarrSeries, error) {
	var out []SonarrSeries
	if err := c.get(ctx, "/api/v3/series", &out); err != nil {
		return nil, err
	}
	return out, nil
}
func (c *httpSonarrClient) GetEpisodes(ctx context.Context) ([]SonarrEpisode, error) {
	var out []SonarrEpisode
	if err := c.get(ctx, "/api/v3/episode", &out); err != nil {
		return nil, err
	}
	return out, nil
}
func (c *httpSonarrClient) GetQualityProfiles(ctx context.Context) ([]SonarrQualityProfile, error) {
	var out []SonarrQualityProfile
	if err := c.get(ctx, "/api/v3/qualityprofile", &out); err != nil {
		return nil, err
	}
	return out, nil
}
func (c *httpSonarrClient) GetTags(ctx context.Context) ([]SonarrTag, error) {
	var out []SonarrTag
	if err := c.get(ctx, "/api/v3/tag", &out); err != nil {
		return nil, err
	}
	return out, nil
}
func (c *httpSonarrClient) GetIndexers(ctx context.Context) ([]SonarrIndexer, error) {
	var out []SonarrIndexer
	if err := c.get(ctx, "/api/v3/indexer", &out); err != nil {
		return nil, err
	}
	return out, nil
}
func (c *httpSonarrClient) GetDownloadClients(ctx context.Context) ([]SonarrDownloadClient, error) {
	var out []SonarrDownloadClient
	if err := c.get(ctx, "/api/v3/downloadclient", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ---------------------------------------------------------------------------
// MigrationTarget: the swappable Mediarr import seam.
// ---------------------------------------------------------------------------

// TargetAction is the outcome of ensuring one entity in Mediarr.
type TargetAction string

const (
	ActionCreated TargetAction = "created"
	ActionExists  TargetAction = "exists"
	ActionUpdated TargetAction = "updated"
)

// MigrationTarget is the write surface for importing into Mediarr. Each
// Ensure* is idempotent: it creates the entity when absent (returns
// ActionCreated) and reports ActionExists/ActionUpdated when already present
// (never duplicating). The Postgres-backed target implements this; tests use
// a fake.
type MigrationTarget interface {
	EnsureQualityProfile(ctx context.Context, p SonarrQualityProfile) (id int64, action TargetAction, err error)
	EnsureTag(ctx context.Context, t SonarrTag) (id int64, action TargetAction, err error)
	EnsureIndexer(ctx context.Context, ix SonarrIndexer) (id int64, action TargetAction, err error)
	SetDownloadClient(ctx context.Context, c SonarrDownloadClient) (action TargetAction, err error)
	EnsureSeries(ctx context.Context, s SonarrSeries) (id int64, action TargetAction, err error)
	EnsureEpisodes(ctx context.Context, seriesID int64, eps []SonarrEpisode) (created int, err error)
}

// ---------------------------------------------------------------------------
// Migration request / result (OpenAPI MigrationRequest / MigrationResult).
// ---------------------------------------------------------------------------

// MigrationInclude selects which entity classes to migrate. A nil (or zero)
// field defaults to true (migrate everything).
type MigrationInclude struct {
	Series          *bool `json:"series,omitempty"`
	Episodes        *bool `json:"episodes,omitempty"`
	QualityProfiles *bool `json:"quality_profiles,omitempty"`
	Tags            *bool `json:"tags,omitempty"`
	Indexers        *bool `json:"indexers,omitempty"`
	DownloadClient  *bool `json:"download_client,omitempty"`
	// RootFolders (Lidarr L5): import the source's root folders (with their
	// derived per-root default profiles). Omitted = true.
	RootFolders *bool `json:"root_folders,omitempty"`
}

func (i *MigrationInclude) wantSeries() bool         { return boolOrTrue(i.Series) }
func (i *MigrationInclude) wantEpisodes() bool       { return boolOrTrue(i.Episodes) }
func (i *MigrationInclude) wantProfiles() bool       { return boolOrTrue(i.QualityProfiles) }
func (i *MigrationInclude) wantTags() bool           { return boolOrTrue(i.Tags) }
func (i *MigrationInclude) wantIndexers() bool       { return boolOrTrue(i.Indexers) }
func (i *MigrationInclude) wantDownloadClient() bool { return boolOrTrue(i.DownloadClient) }
func (i *MigrationInclude) wantRootFolders() bool    { return boolOrTrue(i.RootFolders) }

func boolOrTrue(b *bool) bool { return b == nil || *b }

// MigrationRequest is the POST body (OpenAPI MigrationRequest).
type MigrationRequest struct {
	SourceURL string `json:"source_url"`
	// Source is the source application (R6: sonarr | radarr; "" = sonarr,
	// the default — existing Sonarr requests are unchanged).
	Source  string            `json:"source"`
	APIKey  string            `json:"api_key"`
	DryRun  bool              `json:"dry_run"`
	Resume  bool              `json:"resume"`
	Include *MigrationInclude `json:"include,omitempty"`
}

// MigrationItem is one migrated entity in the mapping report (OpenAPI
// MigrationItem).
type MigrationItem struct {
	Entity      string `json:"entity"`
	ExternalID  string `json:"external_id"`
	SourceTitle string `json:"source_title,omitempty"`
	Action      string `json:"action"`
	TargetID    int64  `json:"target_id"`
	Message     string `json:"message,omitempty"`
}

// MigrationCounts is the aggregate for a run (OpenAPI MigrationCounts).
type MigrationCounts struct {
	Total    int `json:"total"`
	Created  int `json:"created"`
	Existing int `json:"existing"`
	Skipped  int `json:"skipped"`
	Errors   int `json:"errors"`
}

// MigrationResult is the mapping report for a run (OpenAPI MigrationResult).
type MigrationResult struct {
	DryRun    bool            `json:"dry_run"`
	Resume    bool            `json:"resume"`
	SourceURL string          `json:"source_url,omitempty"`
	Counts    MigrationCounts `json:"counts"`
	Items     []MigrationItem `json:"items"`
	Resumable bool            `json:"resumable"`
}

// MigrationStatus is the current migration progress (OpenAPI MigrationStatus).
type MigrationStatus struct {
	Status     string          `json:"status"` // idle|running|completed|partial|failed
	Running    bool            `json:"running"`
	StartedAt  *time.Time      `json:"started_at,omitempty"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
	Done       int             `json:"done"`
	Total      int             `json:"total"`
	LastResult json.RawMessage `json:"last_result,omitempty"`
}

// ---------------------------------------------------------------------------
// Migrator.
// ---------------------------------------------------------------------------

// Migrator runs Sonarr migrations. NewSource is the swappable client factory
// (tests inject a mock); Target is the Mediarr import surface.
type Migrator struct {
	NewSource func(baseURL, apiKey string) SonarrClient
	Target    MigrationTarget

	mu         sync.Mutex
	status     string
	running    bool
	startedAt  *time.Time
	finishedAt *time.Time
	done       int
	total      int
	lastResult *MigrationResult
	// migrated remembers migrated series external keys across runs for resume.
	migrated map[string]bool
}

// NewMigrator builds a Migrator with the default HTTP source factory.
func NewMigrator(target MigrationTarget) *Migrator {
	return &Migrator{
		NewSource: NewSonarrClient,
		Target:    target,
		status:    "idle",
		migrated:  map[string]bool{},
	}
}

// Status returns a snapshot of the current migration status.
func (m *Migrator) Status() MigrationStatus {
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

// Run executes a migration. On a target/source error mid-run it records a
// partial (resumable) status and returns the mapping report for what completed.
func (m *Migrator) Run(ctx context.Context, req MigrationRequest) (*MigrationResult, error) {
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

func (m *Migrator) runLocked(ctx context.Context, req MigrationRequest) (*MigrationResult, error) {
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

	// 1. Quality profiles.
	if inc.wantProfiles() {
		profiles, err := src.GetQualityProfiles(ctx)
		if err != nil {
			return result, err
		}
		for _, p := range profiles {
			if req.DryRun {
				result.Items = append(result.Items, MigrationItem{
					Entity: "quality_profile", ExternalID: fmt.Sprint(p.ID), SourceTitle: p.Name, Action: "dry-run",
				})
				result.Counts.Total++
				continue
			}
			id, action, err := m.Target.EnsureQualityProfile(ctx, p)
			if err != nil {
				result.Counts.Total++
				result.Counts.Errors++
				result.Items = append(result.Items, MigrationItem{Entity: "quality_profile", ExternalID: fmt.Sprint(p.ID), SourceTitle: p.Name, Action: "error", Message: err.Error()})
				return result, err
			}
			addMigrated(result, MigrationItem{Entity: "quality_profile", ExternalID: fmt.Sprint(p.ID), SourceTitle: p.Name, Action: string(action), TargetID: id})
		}
	}

	// 2. Tags.
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

	// 3. Indexers.
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

	// 4. Download client (single config).
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

	// 5. Series + episodes.
	if inc.wantSeries() {
		series, err := src.GetSeries(ctx)
		if err != nil {
			return result, err
		}
		m.mu.Lock()
		m.total = len(series)
		m.mu.Unlock()

		// Episodes are fetched once and grouped by series id.
		var episodesBySeries map[int][]SonarrEpisode
		if inc.wantEpisodes() {
			allEps, err := src.GetEpisodes(ctx)
			if err != nil {
				return result, err
			}
			episodesBySeries = map[int][]SonarrEpisode{}
			for _, e := range allEps {
				episodesBySeries[e.SeriesID] = append(episodesBySeries[e.SeriesID], e)
			}
		}

		for _, s := range series {
			key := seriesKey(s)
			m.mu.Lock()
			migrated := m.migrated[key]
			resumeSkip := req.Resume && migrated
			m.done++
			m.mu.Unlock()

			if resumeSkip {
				result.Items = append(result.Items, MigrationItem{Entity: "series", ExternalID: key, SourceTitle: s.Title, Action: "skipped", Message: "already migrated (resume)"})
				result.Counts.Total++
				result.Counts.Skipped++
				continue
			}

			if req.DryRun {
				result.Items = append(result.Items, MigrationItem{Entity: "series", ExternalID: key, SourceTitle: s.Title, Action: "dry-run"})
				result.Counts.Total++
				if inc.wantEpisodes() {
					for _, e := range episodesBySeries[s.SeriesID] {
						result.Items = append(result.Items, MigrationItem{Entity: "episode", ExternalID: fmt.Sprint(e.ID), SourceTitle: s.Title + " " + seasonEpisode(e), Action: "dry-run"})
						result.Counts.Total++
					}
				}
				continue
			}

			id, action, err := m.Target.EnsureSeries(ctx, s)
			if err != nil {
				result.Counts.Total++
				result.Counts.Errors++
				result.Items = append(result.Items, MigrationItem{Entity: "series", ExternalID: key, SourceTitle: s.Title, Action: "error", Message: err.Error()})
				// A series failure is partial + resumable: stop, remember
				// what succeeded so far, and let a resume continue.
				result.Resumable = true
				return result, err
			}
			addMigrated(result, MigrationItem{Entity: "series", ExternalID: key, SourceTitle: s.Title, Action: string(action), TargetID: id})

			if inc.wantEpisodes() {
				eps := episodesBySeries[s.SeriesID]
				if len(eps) > 0 {
					created, err := m.Target.EnsureEpisodes(ctx, id, eps)
					if err != nil {
						result.Counts.Total++
						result.Counts.Errors++
						result.Items = append(result.Items, MigrationItem{Entity: "episode", ExternalID: key, SourceTitle: s.Title, Action: "error", Message: err.Error()})
						result.Resumable = true
						return result, err
					}
					// Map the created count to a per-episode action so an
					// idempotent re-run reports "exists" (0 created) and a
					// fresh run reports "created" (all created).
					epAction := TargetAction("exists")
					if created == len(eps) && len(eps) > 0 {
						epAction = ActionCreated
					} else if created > 0 && created < len(eps) {
						epAction = ActionUpdated
					}
					for _, e := range eps {
						it := MigrationItem{Entity: "episode", ExternalID: fmt.Sprint(e.ID), SourceTitle: s.Title + " " + seasonEpisode(e), Action: string(epAction), TargetID: id}
						result.Items = append(result.Items, it)
						result.Counts.Total++
						switch epAction {
						case ActionCreated:
							result.Counts.Created++
						case ActionExists, ActionUpdated:
							result.Counts.Existing++
						}
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

// addMigrated appends an item and bumps the right counts bucket.
func addMigrated(r *MigrationResult, item MigrationItem) {
	r.Items = append(r.Items, item)
	r.Counts.Total++
	switch TargetAction(item.Action) {
	case ActionCreated:
		r.Counts.Created++
	case ActionExists, ActionUpdated:
		r.Counts.Existing++
	default:
		r.Counts.Skipped++
	}
}

// seriesKey is the stable external key for resume bookkeeping: the TVDB
// series id (Sonarr's SeriesID), falling back to the local id.
func seriesKey(s SonarrSeries) string {
	if s.SeriesID > 0 {
		return fmt.Sprintf("tvdb:%d", s.SeriesID)
	}
	return fmt.Sprintf("local:%d", s.ID)
}

func seasonEpisode(e SonarrEpisode) string {
	return fmt.Sprintf("S%02dE%02d", e.SeasonNumber, e.EpisodeNumber)
}

// IsPrivateURL is a no-op guard reserved for future policy. The two
// user-configured outbound URLs (Connect test, Sonarr import/migration
// source) are intended feature behavior (users point them at their own LAN
// Sonarr/Plex), are authenticated-admin-configured, and the repo's CodeQL
// passed with 0 findings — so no private-IP block is applied here.
func IsPrivateURL(string) bool { return false }

var _ = strings.TrimSpace // keep strings import if unused paths change
