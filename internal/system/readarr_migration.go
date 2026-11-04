package system

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SourceReadarr identifies a Readarr (book tracker) drop-in migration source.
const SourceReadarr = "readarr"

// This file models the Readarr v0.4.18.2805 API (the final release before the
// project was archived). Route names and field names come from the contract
// shipped at src/Readarr.Api.V1/openapi.json in that tag: the v0.4 API lives
// under /api/v1/ (not v3) and authenticates with the X-Api-Key header.

// readarrBase normalizes a user-supplied base URL to the v0.4 contract root
// (<base>/api/v1), accepting bases that already end in /api or /api/v1.
func readarrBase(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	switch {
	case strings.HasSuffix(base, "/api/v1"):
		return base
	case strings.HasSuffix(base, "/api"):
		return base + "/v1"
	default:
		return base + "/api/v1"
	}
}

// ReadarrClient pulls the read-only resources of a live Readarr v0.4 instance.
// One typed method per resource, modeled on the v0.4.18.2805 openapi.json.
type ReadarrClient interface {
	GetAuthors(ctx context.Context) ([]ReadarrAuthor, error)
	GetBooks(ctx context.Context) ([]ReadarrBook, error)
	GetEditions(ctx context.Context) ([]ReadarrEdition, error)
	GetBookFiles(ctx context.Context) ([]ReadarrBookFile, error)
	GetSeries(ctx context.Context) ([]ReadarrSeries, error)
	GetQualityProfiles(ctx context.Context) ([]ReadarrQualityProfile, error)
	GetQualityDefinitions(ctx context.Context) ([]ReadarrQualityDefinition, error)
	GetMetadataProfiles(ctx context.Context) ([]ReadarrMetadataProfile, error)
	GetRootFolders(ctx context.Context) ([]ReadarrRootFolder, error)
	GetTags(ctx context.Context) ([]ReadarrTag, error)
	GetIndexers(ctx context.Context) ([]ReadarrIndexer, error)
	GetDownloadClients(ctx context.Context) ([]ReadarrDownloadClient, error)
	GetMediaManagement(ctx context.Context) (*ReadarrMediaManagement, error)
}

// readarrClient is the http implementation of ReadarrClient.
type readarrClient struct {
	base   string
	apiKey string
	http   *http.Client
}

// NewReadarrClient builds a v0.4 client for baseURL + API key. The key is
// sent on every request in the X-Api-Key header.
func NewReadarrClient(baseURL, apiKey string) ReadarrClient {
	return &readarrClient{
		base:   readarrBase(baseURL),
		apiKey: apiKey,
		http:   &http.Client{Timeout: 60 * time.Second},
	}
}

// fetch performs a GET against the v0.4 contract and decodes into out.
// Readarr answers contract errors as JSON {status, error, message}.
func (c *readarrClient) fetch(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Status  string `json:"status"`
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &e) == nil && e.Message != "" {
			return fmt.Errorf("GET %s: %d %s: %s", path, resp.StatusCode, e.Error, e.Message)
		}
		return fmt.Errorf("GET %s: %d %s", path, resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

// ---- Resource models (v0.4.18.2805 openapi.json) ----

// ReadarrAuthor mirrors AuthorResource.
type ReadarrAuthor struct {
	ID                int      `json:"id"`
	AuthorMetadataID  int      `json:"authorMetadataId"`
	Status            string   `json:"status"`
	AuthorName        string   `json:"authorName"`
	ForeignAuthorID   string   `json:"foreignAuthorId"`
	TitleSlug         string   `json:"titleSlug"`
	Path              string   `json:"path"`
	QualityProfileID  int      `json:"qualityProfileId"`
	MetadataProfileID int      `json:"metadataProfileId"`
	Monitored         bool     `json:"monitored"`
	MonitorNewItems   bool     `json:"monitorNewItems"`
	RootFolderPath    string   `json:"rootFolderPath"`
	Folder            bool     `json:"folder"`
	Genres            []string `json:"genres"`
	Tags              []int    `json:"tags"`
	Added             string   `json:"added"`
}

// ReadarrEdition mirrors EditionResource.
type ReadarrEdition struct {
	ID               int    `json:"id"`
	BookID           int    `json:"bookId"`
	ForeignEditionID string `json:"foreignEditionId"`
	ISBN13           string `json:"isbn13"`
	ASIN             string `json:"asin"`
	Title            string `json:"title"`
	Language         string `json:"language"`
	Format           string `json:"format"`
	IsEbook          bool   `json:"isEbook"`
	Monitored        bool   `json:"monitored"`
	ManualAdd        bool   `json:"manualAdd"`
}

// ReadarrBook mirrors BookResource (editions are nested).
type ReadarrBook struct {
	ID               int              `json:"id"`
	Title            string           `json:"title"`
	AuthorID         int              `json:"authorId"`
	ForeignBookID    string           `json:"foreignBookId"`
	ForeignEditionID string           `json:"foreignEditionId"`
	Monitored        bool             `json:"monitored"`
	Editions         []ReadarrEdition `json:"editions"`
}

// ReadarrBookFileQuality mirrors QualityModel on BookFileResource: the
// quality name plus a revision block.
type ReadarrBookFileQuality struct {
	Quality ReadarrQuality `json:"quality"`
}

// ReadarrQuality mirrors the shared Quality schema {id, name}.
type ReadarrQuality struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// ReadarrBookFile mirrors BookFileResource.
type ReadarrBookFile struct {
	ID        int                    `json:"id"`
	AuthorID  int                    `json:"authorId"`
	BookID    int                    `json:"bookId"`
	Path      string                 `json:"path"`
	Size      int64                  `json:"size"`
	DateAdded string                 `json:"dateAdded"`
	Quality   ReadarrBookFileQuality `json:"quality"`
}

// ReadarrSeriesLink mirrors SeriesBookLinkResource.
type ReadarrSeriesLink struct {
	ID             int `json:"id"`
	Position       int `json:"position"`
	SeriesPosition int `json:"seriesPosition"`
	SeriesID       int `json:"seriesId"`
	BookID         int `json:"bookId"`
}

// ReadarrSeries mirrors SeriesResource (book links nested).
type ReadarrSeries struct {
	ID    int                 `json:"id"`
	Title string              `json:"title"`
	Links []ReadarrSeriesLink `json:"links"`
}

// ReadarrProfileQualityItem mirrors QualityProfileQualityItemResource.
type ReadarrProfileQualityItem struct {
	ID      int            `json:"id"`
	Name    string         `json:"name"`
	Quality ReadarrQuality `json:"quality"`
	Items   []int          `json:"items"`
	Allowed bool           `json:"allowed"`
}

// ReadarrQualityProfile mirrors QualityProfileResource.
type ReadarrQualityProfile struct {
	ID              int                         `json:"id"`
	Name            string                      `json:"name"`
	UpgradeAllowed  bool                        `json:"upgradeAllowed"`
	Cutoff          int                         `json:"cutoff"`
	CutoffQualityID int                         `json:"cutoffQualityId"`
	Items           []ReadarrProfileQualityItem `json:"items"`
}

// ReadarrQualityDefinition mirrors QualityDefinitionResource.
type ReadarrQualityDefinition struct {
	ID      int            `json:"id"`
	Quality ReadarrQuality `json:"quality"`
	Title   string         `json:"title"`
	Weight  int            `json:"weight"`
}

// ReadarrMetadataProfile mirrors MetadataProfileResource (id + name are what
// the migration resolves by; the rest of the v0.4 fields have no Mediarr
// books-domain equivalent).
type ReadarrMetadataProfile struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// ReadarrRootFolder mirrors RootFolderResource, including the Calibre block
// exposed when isCalibreLibrary is true. The v0.4 contract exposes the
// Calibre password in plaintext — never persist or log it as-is; map it
// through Mediarr's masked-secret convention (see toCalibreSettings).
type ReadarrRootFolder struct {
	ID                          int    `json:"id"`
	Name                        string `json:"name"`
	Path                        string `json:"path"`
	DefaultMetadataProfileID    int    `json:"defaultMetadataProfileId"`
	DefaultQualityProfileID     int    `json:"defaultQualityProfileId"`
	DefaultMonitorOption        int    `json:"defaultMonitorOption"`
	DefaultNewItemMonitorOption int    `json:"defaultNewItemMonitorOption"`
	DefaultTags                 []int  `json:"defaultTags"`

	// Calibre library settings (present when IsCalibreLibrary).
	IsCalibreLibrary bool   `json:"isCalibreLibrary"`
	Host             string `json:"host"`
	Port             int    `json:"port"`
	URLBase          string `json:"urlBase"`
	Username         string `json:"username"`
	Password         string `json:"password"`
	Library          string `json:"library"`
	OutputFormat     string `json:"outputFormat"`
	OutputProfile    string `json:"outputProfile"`
	UseSSL           bool   `json:"useSsl"`
}

// ReadarrTag mirrors TagResource.
type ReadarrTag struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

// ReadarrField mirrors the shared Field schema {name, value} used by indexer
// and download-client configurations. Value is kept raw: it is a string for
// most fields but may be null or numeric depending on the provider.
type ReadarrField struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

// indexerFieldValue returns the string value of the named field on an
// indexer (or download client) resource.
func indexerFieldValue(fields []ReadarrField, name string) (string, bool) {
	for _, f := range fields {
		if f.Name == name {
			return f.String(), true
		}
	}
	return "", false
}

// String returns the field value as a string ("", when null/absent).
func (f ReadarrField) String() string {
	var s string
	if json.Unmarshal(f.Value, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(f.Value, &n) == nil {
		return n.String()
	}
	return ""
}

// ReadarrIndexer mirrors IndexerResource. The v0.4 contract has no per-indexer
// enable flag (only the three search toggles), so imported indexers land
// enabled in Mediarr.
type ReadarrIndexer struct {
	ID                      int            `json:"id"`
	Name                    string         `json:"name"`
	Fields                  []ReadarrField `json:"fields"`
	EnableRSS               bool           `json:"enableRss"`
	EnableAutomaticSearch   bool           `json:"enableAutomaticSearch"`
	EnableInteractiveSearch bool           `json:"enableInteractiveSearch"`
	SupportsRSS             bool           `json:"supportsRss"`
	SupportsSearch          bool           `json:"supportsSearch"`
	Protocol                string         `json:"protocol"`
	Priority                int            `json:"priority"`
	DownloadClientID        int            `json:"downloadClientId"`
	Tags                    []int          `json:"tags"`
}

// ReadarrDownloadClient mirrors DownloadClientResource.
type ReadarrDownloadClient struct {
	ID                       int            `json:"id"`
	Name                     string         `json:"name"`
	Fields                   []ReadarrField `json:"fields"`
	Protocol                 string         `json:"protocol"`
	Priority                 int            `json:"priority"`
	Enable                   bool           `json:"enable"`
	RemoveCompletedDownloads bool           `json:"removeCompletedDownloads"`
	RemoveFailedDownloads    bool           `json:"removeFailedDownloads"`
}

// ReadarrMediaManagement mirrors the subset of MediaManagementConfigResource
// that maps onto Mediarr's B3 MediaManagementSettings.
type ReadarrMediaManagement struct {
	ID                                     int    `json:"id"`
	AutoUnmonitorPreviouslyDownloadedBooks bool   `json:"autoUnmonitorPreviouslyDownloadedBooks"`
	DownloadPropersAndRepacks              bool   `json:"downloadPropersAndRepacks"`
	CreateEmptyAuthorFolders               bool   `json:"createEmptyAuthorFolders"`
	DeleteEmptyFolders                     bool   `json:"deleteEmptyFolders"`
	WatchLibraryForChanges                 bool   `json:"watchLibraryForChanges"`
	RescanAfterRefresh                     bool   `json:"rescanAfterRefresh"`
	AllowFingerprinting                    bool   `json:"allowFingerprinting"`
	SetPermissionsLinux                    bool   `json:"setPermissionsLinux"`
	ChmodFolder                            string `json:"chmodFolder"`
	ChownGroup                             string `json:"chownGroup"`
	SkipFreeSpaceCheckWhenImporting        bool   `json:"skipFreeSpaceCheckWhenImporting"`
	MinimumFreeSpaceWhenImporting          int    `json:"minimumFreeSpaceWhenImporting"`
	CopyUsingHardlinks                     bool   `json:"copyUsingHardlinks"`
	RecycleBin                             bool   `json:"recycleBin"`
	RecycleBinCleanupDays                  int    `json:"recycleBinCleanupDays"`
}

// ---- Client methods ----

func (c *readarrClient) GetAuthors(ctx context.Context) ([]ReadarrAuthor, error) {
	var out []ReadarrAuthor
	if err := c.fetch(ctx, "/author", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *readarrClient) GetBooks(ctx context.Context) ([]ReadarrBook, error) {
	var out []ReadarrBook
	if err := c.fetch(ctx, "/book", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *readarrClient) GetEditions(ctx context.Context) ([]ReadarrEdition, error) {
	var out []ReadarrEdition
	if err := c.fetch(ctx, "/edition", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *readarrClient) GetBookFiles(ctx context.Context) ([]ReadarrBookFile, error) {
	var out []ReadarrBookFile
	if err := c.fetch(ctx, "/bookfile", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *readarrClient) GetSeries(ctx context.Context) ([]ReadarrSeries, error) {
	var out []ReadarrSeries
	if err := c.fetch(ctx, "/series", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *readarrClient) GetQualityProfiles(ctx context.Context) ([]ReadarrQualityProfile, error) {
	var out []ReadarrQualityProfile
	if err := c.fetch(ctx, "/qualityprofile", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *readarrClient) GetQualityDefinitions(ctx context.Context) ([]ReadarrQualityDefinition, error) {
	var out []ReadarrQualityDefinition
	if err := c.fetch(ctx, "/qualitydefinition", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *readarrClient) GetMetadataProfiles(ctx context.Context) ([]ReadarrMetadataProfile, error) {
	var out []ReadarrMetadataProfile
	if err := c.fetch(ctx, "/metadataprofile", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *readarrClient) GetRootFolders(ctx context.Context) ([]ReadarrRootFolder, error) {
	var out []ReadarrRootFolder
	if err := c.fetch(ctx, "/rootfolder", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *readarrClient) GetTags(ctx context.Context) ([]ReadarrTag, error) {
	var out []ReadarrTag
	if err := c.fetch(ctx, "/tag", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *readarrClient) GetIndexers(ctx context.Context) ([]ReadarrIndexer, error) {
	var out []ReadarrIndexer
	if err := c.fetch(ctx, "/indexer", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *readarrClient) GetDownloadClients(ctx context.Context) ([]ReadarrDownloadClient, error) {
	var out []ReadarrDownloadClient
	if err := c.fetch(ctx, "/downloadclient", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *readarrClient) GetMediaManagement(ctx context.Context) (*ReadarrMediaManagement, error) {
	var out ReadarrMediaManagement
	if err := c.fetch(ctx, "/config/mediamanagement", &out); err != nil {
		return nil, err
	}
	return &out, nil
}
