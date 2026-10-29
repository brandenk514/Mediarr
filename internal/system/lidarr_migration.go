package system

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Lidarr L5: the Lidarr drop-in migration.
//
// LidarrMigrator is the music-side counterpart of the P6 Sonarr Migrator and
// the R6 Radarr Migrator: it pulls a live Lidarr 3.x instance over the v3
// REST API (artists + albums + tracks + quality profiles + quality
// definitions + metadata profiles + tags + root folders + indexers +
// download client + a best-effort settings subset) and imports it into
// Mediarr's music domain, returning a mapping report. It carries the same
// guarantees as the Sonarr and Radarr runs:
//
//   - dry-run:  report what WOULD be imported without writing anything.
//   - idempotent: re-running converges — an already-imported entity maps to
//     action "exists" and is never duplicated (artists are keyed by name,
//     albums by title+year, tracks by disc+number — the music domain's
//     identities).
//   - resumable: a partial run records which artists were migrated;
//     resume=true skips those and continues.
//
// The Lidarr API is reached through the swappable LidarrClient seam, so the
// mapping / dry-run / idempotency / resume logic is unit-tested against a
// mocked Lidarr (no live external calls in tests). Importing into Mediarr is
// the swappable LidarrMigrationTarget seam.
//
// The shared MigrationInclude flags apply to Lidarr as series -> artists,
// episodes -> albums + tracks, and root_folders -> root folders (the spec's
// include shape otherwise unchanged).
//
// Honest deltas (documented, not silently dropped):
//   - Lidarr artists carry a metadataProfileId, but the local music artist
//     has no per-artist metadata-profile field (the seeded metadata-profile
//     document holds the imported profiles). The mapping report notes the
//     artist's source metadata profile.
//   - Lidarr per-album quality-profile overrides ride onto the album's
//     quality profile name; the local album model holds a quality-profile
//     name, so those map 1:1.
// ---------------------------------------------------------------------------

// Migration source applications for the shared MigrationRequest.source.
const (
	SourceLidarr = "lidarr"
)

// ---------------------------------------------------------------------------
// Lidarr v3 API source types (the shapes Lidarr's REST endpoints return).
// The shapes are modeled on the documented Lidarr v3 surface and kept
// lenient: unknown fields are ignored on decode.
// ---------------------------------------------------------------------------

// LidarrQuality is one quality-level reference (profile items and the
// track's quality object share this shape).
type LidarrQuality struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// LidarrProfileItem is one approved/not-approved quality in a Lidarr music
// quality profile.
type LidarrProfileItem struct {
	Quality  LidarrQuality `json:"quality"`
	Approved bool          `json:"approved"`
}

// LidarrQualityProfile is a Lidarr music quality profile.
type LidarrQualityProfile struct {
	ID     int                 `json:"id"`
	Name   string              `json:"name"`
	Cutoff int                 `json:"cutoff"`
	Items  []LidarrProfileItem `json:"items"`
}

// LidarrQualityDefinition is a row of the Lidarr music quality ladder
// (sizeLimit is in MB, 0 = unlimited).
type LidarrQualityDefinition struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Source     string `json:"source"`
	Resolution string `json:"resolution"`
	SizeLimit  int    `json:"sizeLimit"`
}

// LidarrMetadataProfile is a Lidarr metadata profile (id + name + the
// metadata-file kinds it names).
type LidarrMetadataProfile struct {
	ID    int      `json:"id"`
	Name  string   `json:"name"`
	Items []string `json:"items"`
}

// LidarrTag is a Lidarr tag.
type LidarrTag struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

// LidarrIndexer is a Lidarr indexer. The four booleans are the per-indexer
// search toggles (they map onto the same R5a indexer settings the Radarr
// run uses: rss / auto_search / interactive_search).
type LidarrIndexer struct {
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

// LidarrDownloadClient is a Lidarr download client.
type LidarrDownloadClient struct {
	ID        int                        `json:"id"`
	Name      string                     `json:"name"`
	Enable    bool                       `json:"enable"`
	ClientKey string                     `json:"clientKey"`
	Fields    map[string]json.RawMessage `json:"fields,omitempty"`
}

// LidarrRootFolder is a Lidarr root folder (path + free space).
type LidarrRootFolder struct {
	Path      string `json:"path"`
	FreeSpace int64  `json:"freeSpace"`
}

// LidarrTrackQuality is the nested quality object on a Lidarr track
// ({quality, qualityType, revision}).
type LidarrTrackQuality struct {
	Quality     LidarrQuality `json:"quality"`
	QualityType LidarrQuality `json:"qualityType"`
	Revision    int           `json:"revision"`
}

// LidarrTrack is one track of a Lidarr album.
type LidarrTrack struct {
	ID          int                `json:"id"`
	AlbumID     int                `json:"albumId"`
	TrackNumber int                `json:"trackNumber"`
	DiscNumber  int                `json:"discNumber"`
	Title       string             `json:"title"`
	Duration    int                `json:"duration"`
	Monitored   bool               `json:"monitored"`
	HasFile     bool               `json:"hasFile"`
	Size        int64              `json:"size"`
	Quality     LidarrTrackQuality `json:"quality"`
}

// LidarrAlbum is a Lidarr album. Tracks are populated by the client (Lidarr
// has no bulk album+tracks endpoint; the client fetches them per album).
type LidarrAlbum struct {
	ID                int           `json:"id"`
	ArtistID          int           `json:"artistId"`
	Title             string        `json:"title"`
	Year              int           `json:"year"`
	ReleaseDate       string        `json:"releaseDate"`
	AlbumType         string        `json:"albumType"`
	Monitored         bool          `json:"monitored"`
	QualityProfileID  int           `json:"qualityProfileId"`
	MetadataProfileID int           `json:"metadataProfileId"`
	Size              int64         `json:"size"`
	Tracks            []LidarrTrack `json:"tracks"`
}

// LidarrArtist is a Lidarr artist.
type LidarrArtist struct {
	ID                int      `json:"id"`
	ArtistID          string   `json:"artistId"` // MusicBrainz artist id ("" = unset)
	Name              string   `json:"name"`
	Status            string   `json:"status"`
	Monitored         bool     `json:"monitored"`
	MonitorNewItems   bool     `json:"monitorNewItems"`
	QualityProfileID  int      `json:"qualityProfileId"`
	MetadataProfileID int      `json:"metadataProfileId"`
	RootFolderPath    string   `json:"rootFolderPath"`
	Path              string   `json:"path"` // subfolder under the root ("" = the artist name)
	Genres            []string `json:"genres"`
	Tags              []int    `json:"tags"`
	Rating            int      `json:"rating"`
}

// LidarrMusicNaming is the Lidarr musicNaming config block.
type LidarrMusicNaming struct {
	RenameTracks             bool   `json:"renameTracks"`
	ReplaceIllegalCharacters bool   `json:"replaceIllegalCharacters"`
	ColonReplacement         string `json:"colonReplacement"`
	StandardFormat           string `json:"standardFormat"`
	MultiDiscFormat          string `json:"multiDiscFormat"`
}

// LidarrFileManagement is the Lidarr fileManagement config block (the
// subset the Mediarr media-management document can hold).
type LidarrFileManagement struct {
	MinFileAge                    int  `json:"minFileAge"` // minutes
	ImportExtraFiles              bool `json:"importExtraFiles"`
	UnmonitorDeletedImportedFiles bool `json:"unmonitorDeletedImportedFiles"`
}

// LidarrMediaManagement is GET /api/v3/config/mediaManagement.
type LidarrMediaManagement struct {
	MusicNaming    LidarrMusicNaming    `json:"musicNaming"`
	FileManagement LidarrFileManagement `json:"fileManagement"`
}

// LidarrImportExport is GET /api/v3/config/importexport when present
// (best-effort: a Lidarr without it degrades to a report note).
type LidarrImportExport struct {
	Script string `json:"script"`
	Sync   bool   `json:"sync"`
}

// ---------------------------------------------------------------------------
// LidarrClient: the swappable source seam.
// ---------------------------------------------------------------------------

// LidarrClient is the read surface of a Lidarr instance's v3 API. It is the
// seam that lets the LidarrMigrator run against a mocked Lidarr in tests.
type LidarrClient interface {
	GetArtists(ctx context.Context) ([]LidarrArtist, error)
	// GetAlbums returns every album (GET /api/v3/album) with its tracks
	// fetched per album (GET /api/v3/album/{id}/tracks) and embedded.
	GetAlbums(ctx context.Context) ([]LidarrAlbum, error)
	GetQualityProfiles(ctx context.Context) ([]LidarrQualityProfile, error)
	GetQualityDefinitions(ctx context.Context) ([]LidarrQualityDefinition, error)
	GetMetadataProfiles(ctx context.Context) ([]LidarrMetadataProfile, error)
	GetTags(ctx context.Context) ([]LidarrTag, error)
	GetIndexers(ctx context.Context) ([]LidarrIndexer, error)
	GetDownloadClients(ctx context.Context) ([]LidarrDownloadClient, error)
	GetRootFolders(ctx context.Context) ([]LidarrRootFolder, error)
	GetMediaManagement(ctx context.Context) (*LidarrMediaManagement, error)
	// GetImportExport is best-effort: a 404 (endpoint absent) yields
	// (nil, nil) so a missing config degrades to a report note instead of
	// failing the run.
	GetImportExport(ctx context.Context) (*LidarrImportExport, error)
}

// NewLidarrClient builds the default HTTP LidarrClient for a live instance.
// apiKey is sent as the X-Api-Key header on every call.
func NewLidarrClient(baseURL, apiKey string) LidarrClient {
	return &httpLidarrClient{base: baseURL, apiKey: apiKey, http: &http.Client{Timeout: 30 * time.Second}}
}

type httpLidarrClient struct {
	base   string
	apiKey string
	http   *http.Client
}

func (c *httpLidarrClient) get(ctx context.Context, path string, out any) error {
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
		return fmt.Errorf("lidarr: %s returned %d: %s", path, resp.StatusCode, truncate(string(body), 200))
	}
	return json.Unmarshal(body, out)
}

func (c *httpLidarrClient) GetArtists(ctx context.Context) ([]LidarrArtist, error) {
	var out []LidarrArtist
	if err := c.get(ctx, "/api/v3/artist", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAlbums fetches the full album list and then each album's tracks (Lidarr
// has no bulk endpoint that nests tracks), embedding them. A per-album track
// fetch failure fails the whole fetch: the run is resumable and a re-fetch is
// idempotent.
func (c *httpLidarrClient) GetAlbums(ctx context.Context) ([]LidarrAlbum, error) {
	var albums []LidarrAlbum
	if err := c.get(ctx, "/api/v3/album", &albums); err != nil {
		return nil, err
	}
	for i := range albums {
		var tracks []LidarrTrack
		if err := c.get(ctx, fmt.Sprintf("/api/v3/album/%d/tracks", albums[i].ID), &tracks); err != nil {
			return nil, err
		}
		albums[i].Tracks = tracks
	}
	return albums, nil
}

func (c *httpLidarrClient) GetQualityProfiles(ctx context.Context) ([]LidarrQualityProfile, error) {
	var out []LidarrQualityProfile
	if err := c.get(ctx, "/api/v3/profile", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *httpLidarrClient) GetQualityDefinitions(ctx context.Context) ([]LidarrQualityDefinition, error) {
	var out []LidarrQualityDefinition
	if err := c.get(ctx, "/api/v3/qualitydefinition", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *httpLidarrClient) GetMetadataProfiles(ctx context.Context) ([]LidarrMetadataProfile, error) {
	var out []LidarrMetadataProfile
	if err := c.get(ctx, "/api/v3/metadataprofile", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *httpLidarrClient) GetTags(ctx context.Context) ([]LidarrTag, error) {
	var out []LidarrTag
	if err := c.get(ctx, "/api/v3/tag", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *httpLidarrClient) GetIndexers(ctx context.Context) ([]LidarrIndexer, error) {
	var out []LidarrIndexer
	if err := c.get(ctx, "/api/v3/indexer", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *httpLidarrClient) GetDownloadClients(ctx context.Context) ([]LidarrDownloadClient, error) {
	var out []LidarrDownloadClient
	if err := c.get(ctx, "/api/v3/downloadclient", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *httpLidarrClient) GetRootFolders(ctx context.Context) ([]LidarrRootFolder, error) {
	var out []LidarrRootFolder
	if err := c.get(ctx, "/api/v3/rootfolder", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *httpLidarrClient) GetMediaManagement(ctx context.Context) (*LidarrMediaManagement, error) {
	var out LidarrMediaManagement
	if err := c.get(ctx, "/api/v3/config/mediaManagement", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *httpLidarrClient) GetImportExport(ctx context.Context) (*LidarrImportExport, error) {
	var out LidarrImportExport
	err := c.get(ctx, "/api/v3/config/importexport", &out)
	if err != nil {
		// A 404 means the endpoint is absent on this Lidarr — degrade to a
		// report note (the caller gets nil, nil). Any other failure is real.
		if isHTTPStatusError(err, http.StatusNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// LidarrMigrationTarget: the swappable Mediarr import seam (music domain).
// ---------------------------------------------------------------------------

// LidarrArtistRef is the minimal artist row the LidarrMusicStore needs to
// import. QualityProfile is the resolved music quality-profile NAME (empty =
// the target's default); the store must not see source profile ids.
type LidarrArtistRef struct {
	Name             string
	MBID             string
	Status           string
	Path             string
	Monitored        bool
	MonitorPolicy    string
	MonitorNewAlbums string
	QualityProfile   string
	Tags             []string
	Genre            string
	Rating           float64
}

// LidarrTrackRef is one source track to ensure on the target. Quality is the
// name of the imported file's quality level ("" = no file).
type LidarrTrackRef struct {
	Disc      int
	Number    int
	Title     string
	Duration  int
	Monitored bool
	HasFile   bool
	Size      int64
	Quality   string
}

// LidarrAlbumRef is one source album to ensure on the target (keyed by
// title+year within its artist, the music domain's identity).
type LidarrAlbumRef struct {
	Name           string
	Year           int
	AlbumType      string
	ReleaseDate    string
	Monitored      bool
	QualityProfile string
	Size           int64
	Tracks         []LidarrTrackRef
}

// LidarrTrackResult reports the ensured state of one track (created when new,
// exists when already present).
type LidarrTrackResult struct {
	Disc   int
	Number int
	Action TargetAction
}

// LidarrAlbumResult reports the ensured state of one album and its tracks.
type LidarrAlbumResult struct {
	Album  LidarrAlbumRef
	Action TargetAction
	Tracks []LidarrTrackResult
}

// LidarrMigrationTarget is the write surface for importing a Lidarr instance
// into Mediarr. Each Ensure* is idempotent: it creates the entity when absent
// (returns ActionCreated) and reports ActionExists/ActionUpdated when already
// present (never duplicating). The Postgres-backed target implements this;
// tests use a fake.
type LidarrMigrationTarget interface {
	EnsureMusicProfile(ctx context.Context, p LidarrQualityProfile) (id int64, action TargetAction, err error)
	// ApplyQualityDefinitions reconciles the source quality ladder onto the
	// seeded music levels and returns the number of levels written.
	ApplyQualityDefinitions(ctx context.Context, defs []LidarrQualityDefinition) (int, error)
	EnsureMetadataProfile(ctx context.Context, p LidarrMetadataProfile) (id int64, action TargetAction, err error)
	EnsureTag(ctx context.Context, t LidarrTag) (id int64, action TargetAction, err error)
	// EnsureRootFolder upserts a root folder by path; defaultQuality /
	// defaultMetadata are the derived per-root default profile names
	// ("" = unset).
	EnsureRootFolder(ctx context.Context, r LidarrRootFolder, defaultQuality, defaultMetadata string) (id int64, action TargetAction, err error)
	EnsureIndexer(ctx context.Context, ix LidarrIndexer) (id int64, action TargetAction, err error)
	SetDownloadClient(ctx context.Context, c LidarrDownloadClient) (action TargetAction, err error)
	// ApplyMediaManagement maps the best-effort media-management subset and
	// returns the number of fields written.
	ApplyMediaManagement(ctx context.Context, mm LidarrMediaManagement) (int, error)
	EnsureArtist(ctx context.Context, ref LidarrArtistRef) (id int64, action TargetAction, err error)
	// EnsureAlbums upserts an artist's albums (title+year identity) and
	// their tracks (disc+number identity), returning the per-entity state.
	EnsureAlbums(ctx context.Context, artistID int64, albums []LidarrAlbumRef) ([]LidarrAlbumResult, error)
}

// LidarrMusicStore is the music-domain write surface. main.go adapts the
// Postgres music repo to it. AddArtist must create the artist with the
// source's monitored state (the add use-case's semantics: a monitored album
// / track gets a wanted row).
type LidarrMusicStore interface {
	// FindArtist returns the id of the tracked artist with the given name,
	// or found=false.
	FindArtist(ctx context.Context, name string) (id int64, found bool)
	// AddArtist creates the artist and returns its id.
	AddArtist(ctx context.Context, ref LidarrArtistRef) (id int64, err error)
	// FindAlbum returns the id of the album (name, year) within artistID,
	// or found=false.
	FindAlbum(ctx context.Context, artistID int64, name string, year int) (id int64, found bool)
	// AddAlbum creates the album within the artist and returns its id.
	AddAlbum(ctx context.Context, artistID int64, ref LidarrAlbumRef) (id int64, err error)
	// TrackKeys returns the set of "<disc>-<number>" keys already in the
	// album.
	TrackKeys(ctx context.Context, albumID int64) (map[string]bool, error)
	// AddTrack creates a known track in the album and returns its id.
	AddTrack(ctx context.Context, albumID int64, ref LidarrTrackRef) (id int64, err error)
}

// LidarrRootFolderStore is the root-folder-registry write surface. main.go
// adapts the root-folders service to it.
type LidarrRootFolderStore interface {
	// FindRootFolder returns the id of the registered folder with the given
	// (clean) path, or found=false.
	FindRootFolder(ctx context.Context, path string) (id int64, found bool)
	// CreateRootFolder registers a new root folder with its derived default
	// profiles ("" = unset) and returns its id.
	CreateRootFolder(ctx context.Context, path, name, defaultQuality, defaultMetadata string) (id int64, err error)
}

// ---------------------------------------------------------------------------
// LidarrMigrator.
// ---------------------------------------------------------------------------

// LidarrMigrator runs Lidarr migrations. NewSource is the swappable client
// factory (tests inject a mock); Target is the Mediarr music import surface.
type LidarrMigrator struct {
	NewSource func(baseURL, apiKey string) LidarrClient
	Target    LidarrMigrationTarget

	mu         sync.Mutex
	status     string
	running    bool
	startedAt  *time.Time
	finishedAt *time.Time
	done       int
	total      int
	lastResult *MigrationResult
	// migrated remembers migrated artist external keys across runs for
	// resume.
	migrated map[string]bool
}

// NewLidarrMigrator builds a LidarrMigrator with the default HTTP source
// factory.
func NewLidarrMigrator(target LidarrMigrationTarget) *LidarrMigrator {
	return &LidarrMigrator{
		NewSource: NewLidarrClient,
		Target:    target,
		status:    "idle",
		migrated:  map[string]bool{},
	}
}

// Status returns a snapshot of the current Lidarr migration status.
func (m *LidarrMigrator) Status() MigrationStatus {
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

// Run executes a Lidarr migration. On a target/source error mid-run it
// records a partial (resumable) status and returns the mapping report for
// what completed.
func (m *LidarrMigrator) Run(ctx context.Context, req MigrationRequest) (*MigrationResult, error) {
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

func (m *LidarrMigrator) runLocked(ctx context.Context, req MigrationRequest) (*MigrationResult, error) {
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

	// 1. Music quality profiles. The target maps the source profile NAME
	// into the music quality-profile document; a source profile that is not
	// among the seeded Lidarr defaults is created there and noted. The
	// id -> name map lets artist/album qualityProfileId references resolve
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
			id, action, err := m.Target.EnsureMusicProfile(ctx, p)
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
				it.Message = "created in the music profiles document (not a seeded Lidarr profile)"
			}
			addMigrated(result, it)
		}
	}

	// 2. Quality definitions: the source's music quality ladder reconciled
	// onto the seeded music levels (the L3 seed table is data, so a
	// non-seeded source level is appended with a 200+ local id).
	if inc.wantProfiles() {
		defs, err := src.GetQualityDefinitions(ctx)
		if err != nil {
			return result, err
		}
		if req.DryRun {
			result.Items = append(result.Items, MigrationItem{Entity: "quality_definition", Action: "dry-run"})
			result.Counts.Total++
		} else {
			n, err := m.Target.ApplyQualityDefinitions(ctx, defs)
			if err != nil {
				result.Counts.Total++
				result.Counts.Errors++
				result.Items = append(result.Items, MigrationItem{Entity: "quality_definition", Action: "error", Message: err.Error()})
				return result, err
			}
			action := TargetAction("exists")
			if n > 0 {
				action = ActionUpdated
			}
			addMigrated(result, MigrationItem{Entity: "quality_definition", Action: string(action), Message: fmt.Sprintf("%d quality definitions reconciled", n)})
		}
	}

	// 3. Metadata profiles (the seeded metadata-profile document).
	metaNamesByID := map[int]string{}
	if inc.wantProfiles() {
		profiles, err := src.GetMetadataProfiles(ctx)
		if err != nil {
			return result, err
		}
		for _, p := range profiles {
			metaNamesByID[p.ID] = p.Name
			if req.DryRun {
				result.Items = append(result.Items, MigrationItem{Entity: "metadata_profile", ExternalID: fmt.Sprint(p.ID), SourceTitle: p.Name, Action: "dry-run"})
				result.Counts.Total++
				continue
			}
			id, action, err := m.Target.EnsureMetadataProfile(ctx, p)
			if err != nil {
				result.Counts.Total++
				result.Counts.Errors++
				result.Items = append(result.Items, MigrationItem{Entity: "metadata_profile", ExternalID: fmt.Sprint(p.ID), SourceTitle: p.Name, Action: "error", Message: err.Error()})
				return result, err
			}
			it := MigrationItem{Entity: "metadata_profile", ExternalID: fmt.Sprint(p.ID), SourceTitle: p.Name, Action: string(action), TargetID: id}
			if action == ActionCreated {
				it.Message = "created in the metadata profiles document (not a seeded profile)"
			}
			addMigrated(result, it)
		}
	}

	// 4. Tags (tag registry, source ids preserved for artist references).
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

	// 5. Indexers (the per-indexer booleans: enableRss -> rss,
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

	// 6. Download client (single config, like Sonarr).
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

	// 7. Settings (a best-effort subset: the music media-management fields
	// the Mediarr document can hold + an importexport note). A failure
	// degrades to a report note and never fails the run.
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

	// 8. Root folders + artists (+ albums + tracks). The library fetch
	// (artists, and albums when imported) serves both: each root folder's
	// default profiles are derived from the first artist that lives under
	// it, and the artists are the run's primary entities.
	var (
		artists        []LidarrArtist
		albumsByArtist = map[int][]LidarrAlbum{}
	)
	if inc.wantRootFolders() || inc.wantSeries() {
		var err error
		artists, err = src.GetArtists(ctx)
		if err != nil {
			return result, err
		}
		if inc.wantSeries() {
			albums, err := src.GetAlbums(ctx)
			if err != nil {
				return result, err
			}
			for _, al := range albums {
				albumsByArtist[al.ArtistID] = append(albumsByArtist[al.ArtistID], al)
			}
		}
	}

	// 8a. Root folders (with the derived per-root default profiles).
	if inc.wantRootFolders() {
		defaults := rootProfileDefaults(artists, profileNamesByID, metaNamesByID)
		roots, err := src.GetRootFolders(ctx)
		if err != nil {
			return result, err
		}
		for _, r := range roots {
			if req.DryRun {
				result.Items = append(result.Items, MigrationItem{Entity: "root_folder", ExternalID: r.Path, SourceTitle: r.Path, Action: "dry-run"})
				result.Counts.Total++
				continue
			}
			dq, dm := "", ""
			if d, ok := defaults[filepath.Clean(r.Path)]; ok {
				dq, dm = d[0], d[1]
			}
			id, action, err := m.Target.EnsureRootFolder(ctx, r, dq, dm)
			if err != nil {
				result.Counts.Total++
				result.Counts.Errors++
				result.Items = append(result.Items, MigrationItem{Entity: "root_folder", ExternalID: r.Path, SourceTitle: r.Path, Action: "error", Message: err.Error()})
				return result, err
			}
			it := MigrationItem{Entity: "root_folder", ExternalID: r.Path, SourceTitle: r.Path, Action: string(action), TargetID: id}
			if dq != "" || dm != "" {
				it.Message = fmt.Sprintf("default profiles: quality=%q metadata=%q", dq, dm)
			}
			addMigrated(result, it)
		}
	}

	// 8b. Artists + albums + tracks (include.series -> artists,
	// include.episodes -> albums + tracks).
	if inc.wantSeries() {
		m.mu.Lock()
		m.total = len(artists)
		m.mu.Unlock()

		for _, a := range artists {
			key := artistKey(a)
			m.mu.Lock()
			migrated := m.migrated[key]
			resumeSkip := req.Resume && migrated
			m.done++
			m.mu.Unlock()

			if resumeSkip {
				result.Items = append(result.Items, MigrationItem{Entity: "artist", ExternalID: key, SourceTitle: a.Name, Action: "skipped", Message: "already migrated (resume)"})
				result.Counts.Total++
				result.Counts.Skipped++
				continue
			}

			if req.DryRun {
				result.Items = append(result.Items, MigrationItem{Entity: "artist", ExternalID: key, SourceTitle: a.Name, Action: "dry-run"})
				result.Counts.Total++
				if inc.wantEpisodes() {
					for _, al := range albumsByArtist[a.ID] {
						result.Items = append(result.Items, MigrationItem{Entity: "album", ExternalID: albumKey(al), SourceTitle: a.Name + " " + al.Title, Action: "dry-run"})
						result.Counts.Total++
						for _, tr := range al.Tracks {
							result.Items = append(result.Items, MigrationItem{Entity: "track", ExternalID: trackKey(tr), SourceTitle: al.Title + " " + tr.Title, Action: "dry-run"})
							result.Counts.Total++
						}
					}
				}
				continue
			}

			qName, notes := resolveArtistProfile(profileNamesByID, a.QualityProfileID)
			if n := metadataProfileNote(metaNamesByID, a.MetadataProfileID); n != "" {
				notes = append(notes, n)
			}
			labels, err := lidarrTagLabelList(m.Target, ctx, a.Tags)
			if err != nil {
				return result, err
			}
			ref := LidarrArtistRef{
				Name:             a.Name,
				MBID:             a.ArtistID,
				Status:           a.Status,
				Path:             a.Path,
				Monitored:        a.Monitored,
				MonitorNewAlbums: monitorNewAlbums(a.MonitorNewItems),
				QualityProfile:   qName,
				Tags:             labels,
				Genre:            strings.Join(a.Genres, ", "),
				Rating:           float64(a.Rating),
			}
			id, action, err := m.Target.EnsureArtist(ctx, ref)
			if err != nil {
				result.Counts.Total++
				result.Counts.Errors++
				result.Items = append(result.Items, MigrationItem{Entity: "artist", ExternalID: key, SourceTitle: a.Name, Action: "error", Message: strings.Join(notes, "; ") + " " + err.Error()})
				// An artist failure is partial + resumable: stop, remember
				// what succeeded so far, and let a resume continue.
				result.Resumable = true
				return result, err
			}
			item := MigrationItem{Entity: "artist", ExternalID: key, SourceTitle: a.Name, Action: string(action), TargetID: id}
			if len(notes) > 0 {
				item.Message = strings.Join(notes, "; ")
			}
			addMigrated(result, item)

			if inc.wantEpisodes() {
				if srcAlbums := albumsByArtist[a.ID]; len(srcAlbums) > 0 {
					refs := make([]LidarrAlbumRef, 0, len(srcAlbums))
					for _, al := range srcAlbums {
						refs = append(refs, LidarrAlbumRef{
							Name:           al.Title,
							Year:           al.Year,
							AlbumType:      al.AlbumType,
							ReleaseDate:    al.ReleaseDate,
							Monitored:      al.Monitored,
							QualityProfile: resolveAlbumProfile(profileNamesByID, al.QualityProfileID, qName),
							Size:           al.Size,
							Tracks:         lidarrTrackRefs(al.Tracks),
						})
					}
					results, err := m.Target.EnsureAlbums(ctx, id, refs)
					if err != nil {
						result.Counts.Total++
						result.Counts.Errors++
						result.Items = append(result.Items, MigrationItem{Entity: "album", ExternalID: key, SourceTitle: a.Name, Action: "error", Message: err.Error()})
						result.Resumable = true
						return result, err
					}
					for _, r := range results {
						addMigrated(result, MigrationItem{Entity: "album", ExternalID: albumRefKey(r.Album), SourceTitle: a.Name + " " + r.Album.Name, Action: string(r.Action), TargetID: id})
						for _, tr := range r.Tracks {
							addMigrated(result, MigrationItem{
								Entity: "track", ExternalID: fmt.Sprintf("track:%d-%d", tr.Disc, tr.Number),
								SourceTitle: r.Album.Name + " " + trackTitle(r.Album, tr),
								Action:      string(tr.Action), TargetID: id,
							})
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

// artistKey is the stable external key for a migrated artist: the name, the
// music domain's artist identity.
func artistKey(a LidarrArtist) string {
	return fmt.Sprintf("artist:%s", a.Name)
}

// albumKey is the stable external key for an album report item: title+year,
// the music domain's album identity.
func albumKey(al LidarrAlbum) string {
	return fmt.Sprintf("album:%s|%d", al.Title, al.Year)
}

// albumRefKey is the report key for an imported album ref: title+year (the
// music domain's album identity).
func albumRefKey(al LidarrAlbumRef) string {
	return fmt.Sprintf("album:%s|%d", al.Name, al.Year)
}

// trackKey is the stable external key for a track report item: the source
// track id when set, else disc+number (the music domain's track identity).
func trackKey(t LidarrTrack) string {
	if t.ID > 0 {
		return fmt.Sprintf("track:%d", t.ID)
	}
	return fmt.Sprintf("track:%d-%d", t.DiscNumber, t.TrackNumber)
}

// trackTitle finds the track's title by disc+number in the album's refs (the
// result rows carry disc/number but not the title).
func trackTitle(al LidarrAlbumRef, tr LidarrTrackResult) string {
	for _, t := range al.Tracks {
		if t.Disc == tr.Disc && t.Number == tr.Number {
			return t.Title
		}
	}
	return ""
}

// monitorNewAlbums maps Lidarr's artist monitorNewItems flag onto the local
// monitor_new_albums value.
func monitorNewAlbums(b bool) string {
	if b {
		return "all_albums"
	}
	return "none"
}

// rootProfileDefaults derives each root folder's default quality/metadata
// profile names from the first artist that lives under it (in source order):
// the artist's own profile references are the best available per-root
// default. Roots with no resolvable artist reference stay unset ("").
func rootProfileDefaults(artists []LidarrArtist, qualityNames, metadataNames map[int]string) map[string][2]string {
	out := map[string][2]string{}
	for _, a := range artists {
		if a.RootFolderPath == "" {
			continue
		}
		path := filepath.Clean(a.RootFolderPath)
		if _, ok := out[path]; ok {
			continue
		}
		out[path] = [2]string{qualityNames[a.QualityProfileID], metadataNames[a.MetadataProfileID]}
	}
	return out
}

// resolveArtistProfile maps an artist's source qualityProfileId to the local
// music quality-profile name. An id that is not in the source profile list
// (or absent) resolves to "" — the target applies its own document default —
// and a report note for the unexpected id case.
func resolveArtistProfile(namesByID map[int]string, sourceID int) (name string, notes []string) {
	if n, ok := namesByID[sourceID]; ok {
		return n, nil
	}
	if sourceID > 0 {
		return "", []string{fmt.Sprintf("quality profile id %d not in source profiles; using the target's default profile", sourceID)}
	}
	return "", nil
}

// resolveAlbumProfile maps an album's per-album qualityProfileId to a local
// profile name, falling back to the artist's resolved name.
func resolveAlbumProfile(namesByID map[int]string, sourceID int, artistName string) string {
	if sourceID > 0 {
		if n, ok := namesByID[sourceID]; ok {
			return n
		}
	}
	return artistName
}

// metadataProfileNote returns the report note for an artist's source
// metadata-profile reference (honest delta: the local artist has no
// per-artist metadata-profile field; the profile itself is imported into the
// seeded document).
func metadataProfileNote(namesByID map[int]string, sourceID int) string {
	if sourceID <= 0 {
		return ""
	}
	if n, ok := namesByID[sourceID]; ok {
		return fmt.Sprintf("metadata profile: %s (imported into the metadata profiles document; not stored per artist)", n)
	}
	return fmt.Sprintf("metadata profile id %d not in source metadata profiles", sourceID)
}

// lidarrTrackRefs maps source tracks to their target refs: the imported file's
// quality (the nested quality object's name) becomes the track's status.
func lidarrTrackRefs(tracks []LidarrTrack) []LidarrTrackRef {
	out := make([]LidarrTrackRef, 0, len(tracks))
	for _, t := range tracks {
		out = append(out, LidarrTrackRef{
			Disc:      t.DiscNumber,
			Number:    t.TrackNumber,
			Title:     t.Title,
			Duration:  t.Duration,
			Monitored: t.Monitored,
			HasFile:   t.HasFile,
			Size:      t.Size,
			Quality:   t.Quality.Quality.Name,
		})
	}
	return out
}

// lidarrTagLabelList resolves source tag ids to labels via the target's tag
// registry (the target preserves source tag ids on import).
func lidarrTagLabelList(t LidarrMigrationTarget, ctx context.Context, ids []int) ([]string, error) {
	// The target's EnsureTag calls are the write path; for label lookup the
	// target exposes the registry through LidarrTagStore when present.
	if s, ok := t.(LidarrTagStore); ok {
		return s.TagLabels(ctx, ids)
	}
	return make([]string, 0, len(ids)), nil
}

// LidarrTagStore is an optional target capability: label lookup for source
// tag ids (the real target has the tag registry in the settings document).
type LidarrTagStore interface {
	TagLabels(ctx context.Context, ids []int) ([]string, error)
}
