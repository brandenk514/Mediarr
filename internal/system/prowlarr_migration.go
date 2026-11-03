// Prowlarr (indexer manager) migration for Mediarr — Prowlarr PR6 of
// docs/prowlarr-parity-plan.md.
//
// Prowlarr is the *arr index manager: its API is the authoritative source of
// the indexers (with their credentials in plaintext), download clients, tags,
// and indexer proxies (FlareSolverr endpoints) the user wants in Mediarr.
// Unlike Sonarr/Radarr/Lidarr there are no series, episodes, or quality
// profiles — the Prowlarr "Apps" section is NOT imported either: Mediarr
// replaces it with per-kind settings (the download-client and indexer-proxy
// stores plus per-protocol settings). The mapping report states this
// explicitly.
//
// Credential handling: Prowlarr stores its own config fields in plaintext.
// This migrator passes the PLAINTEXT values through the
// ProwlarrMigrationTarget seam exactly as received — the Postgres target
// hands them to the vault-aware stores (internal/postgres indexer,
// download-client, and proxy repos), which encrypt them with the
// deployment's AES-256-GCM vault (internal/secrets) before they touch the
// database. The migrator NEVER logs a config-field value: its report items
// and error messages carry names, definition ids, and counts only.

package system

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brandenk514/mediarr/internal/downloadclients"
	indexerdefinitions "github.com/brandenk514/mediarr/internal/indexers/definitions"
)

// SourceProwlarr is the MigrationRequest.Source value that routes to the
// Prowlarr migration (System + Prowlarr-API migration, Prowlarr PR6).
const SourceProwlarr = "prowlarr"

// ProwlarrTag is a Prowlarr v1 /api/v1/tag object.
type ProwlarrTag struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

// ProwlarrIndexer is a Prowlarr v1 /api/v1/indexer object. The credential
// values in ConfigFields are PLAINTEXT in the Prowlarr API (Prowlarr does not
// mask them there); they must be re-encrypted by the Mediarr stores at rest.
//
// Prowlarr exposes the per-indexer config as a "fields" array
// ([{"name": "apikey", "value": "..."}]); this DTO also accepts a
// "configFields" object so both shapes normalize to ConfigFields.
type ProwlarrIndexer struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Enable bool   `json:"enable"`
	// EnableRss / EnableAutomatic / EnableInteractive are best-effort: recent
	// Prowlarr versions omit them on the indexer resource (search toggles are
	// a per-*arr concern there). Nil = unspecified → keep the target default
	// (enabled), a present false is imported as disabled.
	EnableRss         *bool          `json:"enableRss"`
	EnableAutomatic   *bool          `json:"enableAutomatic"`
	EnableInteractive *bool          `json:"enableInteractive"`
	IndexerID         int            `json:"indexerId"`
	DefinitionName    string         `json:"definitionName"`
	Redirect          bool           `json:"redirect"`
	Priority          int            `json:"priority"`
	SyncLevel         string         `json:"syncLevel"`
	Privacy           string         `json:"privacy"`
	BaseURL           string         `json:"baseUrl"`
	Tags              []int          `json:"tags"`
	ConfigFields      map[string]any `json:"-"` // normalized from "configFields" or "fields"
}

// unmarshalProwlarrIndexer accepts both the "configFields" object shape and
// the Prowlarr "fields" array shape, normalizing every value to a string
// (Prowlarr sends values as strings; numbers/bools are coerced defensively).
func unmarshalProwlarrIndexer(raw json.RawMessage) (ProwlarrIndexer, error) {
	var base struct {
		ConfigFields map[string]any  `json:"configFields"`
		Fields       []ProwlarrField `json:"fields"`
	}
	var ix ProwlarrIndexer
	if err := json.Unmarshal(raw, &ix); err != nil {
		return ix, err
	}
	if err := json.Unmarshal(raw, &base); err != nil {
		return ix, err
	}
	fields := map[string]any{}
	for k, v := range base.ConfigFields {
		fields[k] = v
	}
	if len(fields) == 0 {
		for _, f := range base.Fields {
			fields[f.Name] = prowlarrFieldString(f.Value)
		}
	}
	ix.ConfigFields = fields
	return ix, nil
}

// ProwlarrDownloadClient is a Prowlarr v1 /api/v1/downloadclient object.
// ClientType is the Prowlarr client kind (e.g. "qBittorrent", "SABnzbd",
// "Transmission"). Credential-bearing field values are PLAINTEXT.
type ProwlarrDownloadClient struct {
	ID         int               `json:"id"`
	Name       string            `json:"name"`
	Enable     bool              `json:"enable"`
	ClientType string            `json:"clientType"`
	BaseURL    string            `json:"baseUrl"`
	Tags       []int             `json:"clientTags"`
	Fields     map[string]string `json:"-"` // normalized from "fields" or "configFields"
}

// unmarshalProwlarrDownloadClient normalizes the "fields" array (or a
// "configFields" object) to a string map.
func unmarshalProwlarrDownloadClient(raw json.RawMessage) (ProwlarrDownloadClient, error) {
	var base struct {
		ConfigFields map[string]any  `json:"configFields"`
		Fields       []ProwlarrField `json:"fields"`
	}
	var c ProwlarrDownloadClient
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, err
	}
	if err := json.Unmarshal(raw, &base); err != nil {
		return c, err
	}
	fields := map[string]string{}
	for k, v := range base.ConfigFields {
		fields[k] = prowlarrAnyString(v)
	}
	if len(fields) == 0 {
		for _, f := range base.Fields {
			fields[f.Name] = prowlarrFieldString(f.Value)
		}
	}
	c.Fields = fields
	return c, nil
}

// ProwlarrIndexerProxy is a Prowlarr v1 /api/v1/indexerproxy object
// (a FlareSolverr endpoint: name, enable, baseUrl, optional auth fields).
type ProwlarrIndexerProxy struct {
	ID      int               `json:"id"`
	Name    string            `json:"name"`
	Enable  bool              `json:"enable"`
	BaseURL string            `json:"baseUrl"`
	Tags    []int             `json:"tags"`
	Fields  map[string]string `json:"-"` // normalized from "fields" or "configFields"
}

// unmarshalProwlarrIndexerProxy normalizes the "fields" array (or a
// "configFields" object) to a string map.
func unmarshalProwlarrIndexerProxy(raw json.RawMessage) (ProwlarrIndexerProxy, error) {
	var base struct {
		ConfigFields map[string]any  `json:"configFields"`
		Fields       []ProwlarrField `json:"fields"`
	}
	var p ProwlarrIndexerProxy
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, err
	}
	if err := json.Unmarshal(raw, &base); err != nil {
		return p, err
	}
	fields := map[string]string{}
	for k, v := range base.ConfigFields {
		fields[k] = prowlarrAnyString(v)
	}
	if len(fields) == 0 {
		for _, f := range base.Fields {
			fields[f.Name] = prowlarrFieldString(f.Value)
		}
	}
	if p.BaseURL == "" {
		p.BaseURL = fields["baseUrl"]
	}
	p.Fields = fields
	return p, nil
}

// prowlarrAnyString coerces a normalized "configFields" object value to a
// string (numbers/bools via their text form, null as "").
func prowlarrAnyString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", s)
	}
}

// ProwlarrField is one entry of a Prowlarr "fields" array.
type ProwlarrField struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

// prowlarrFieldString coerces a raw Prowlarr field value to a string: string
// as-is, numbers/bools via their text form, null/missing as "".
func prowlarrFieldString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String()
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return strconv.FormatBool(b)
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return ""
}

// ProwlarrClient is the read-only Prowlarr v1 API surface the migrator
// needs (base URL + X-Api-Key auth; GETs only). The httpProwlarrClient below
// is the production implementation; unit tests swap in a fake so no live
// HTTP happens.
type ProwlarrClient interface {
	GetTags(ctx context.Context) ([]ProwlarrTag, error)
	GetIndexers(ctx context.Context) ([]ProwlarrIndexer, error)
	GetDownloadClients(ctx context.Context) ([]ProwlarrDownloadClient, error)
	GetIndexerProxies(ctx context.Context) ([]ProwlarrIndexerProxy, error)
}

// httpProwlarrClient is the real ProwlarrClient: the v1 REST endpoints under
// the instance base URL, authenticated with the X-Api-Key header.
type httpProwlarrClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewProwlarrClient builds the production ProwlarrClient for a Prowlarr
// instance (baseURL, e.g. https://prowlarr.local:9696, plus its API key).
func NewProwlarrClient(baseURL, apiKey string) ProwlarrClient {
	return &httpProwlarrClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *httpProwlarrClient) getJSON(ctx context.Context, path string, out any) error {
	u := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Read and discard the body: it can contain PLAINTEXT credentials
		// (indexer config fields) and must never surface in an error or log.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		// Deliberately no response body / status text in the message: the
		// 401 body is "Unauthorized" and a 4xx/5xx body may echo request or
		// credential data. Status code only.
		return fmt.Errorf("prowlarr: %s returned %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("prowlarr: decode %s: %w", path, err)
	}
	return nil
}

func (c *httpProwlarrClient) GetTags(ctx context.Context) ([]ProwlarrTag, error) {
	var tags []ProwlarrTag
	err := c.getJSON(ctx, "/api/v1/tag", &tags)
	return tags, err
}

func (c *httpProwlarrClient) GetIndexers(ctx context.Context) ([]ProwlarrIndexer, error) {
	var raw []json.RawMessage
	if err := c.getJSON(ctx, "/api/v1/indexer", &raw); err != nil {
		return nil, err
	}
	out := make([]ProwlarrIndexer, 0, len(raw))
	for _, r := range raw {
		ix, err := unmarshalProwlarrIndexer(r)
		if err != nil {
			return nil, fmt.Errorf("prowlarr: indexer: %w", err)
		}
		out = append(out, ix)
	}
	return out, nil
}

func (c *httpProwlarrClient) GetDownloadClients(ctx context.Context) ([]ProwlarrDownloadClient, error) {
	var raw []json.RawMessage
	if err := c.getJSON(ctx, "/api/v1/downloadclient", &raw); err != nil {
		return nil, err
	}
	out := make([]ProwlarrDownloadClient, 0, len(raw))
	for _, r := range raw {
		c, err := unmarshalProwlarrDownloadClient(r)
		if err != nil {
			return nil, fmt.Errorf("prowlarr: downloadclient: %w", err)
		}
		out = append(out, c)
	}
	return out, nil
}

func (c *httpProwlarrClient) GetIndexerProxies(ctx context.Context) ([]ProwlarrIndexerProxy, error) {
	var raw []json.RawMessage
	if err := c.getJSON(ctx, "/api/v1/indexerproxy", &raw); err != nil {
		return nil, err
	}
	out := make([]ProwlarrIndexerProxy, 0, len(raw))
	for _, r := range raw {
		p, err := unmarshalProwlarrIndexerProxy(r)
		if err != nil {
			return nil, fmt.Errorf("prowlarr: indexerproxy: %w", err)
		}
		out = append(out, p)
	}
	return out, nil
}

// ProwlarrIndexerRef is the resolved (post-fallback) write payload for one
// indexer: the definition library id (or a generic-torznab/generic-newznab
// fallback id), the wire kind, and the PLAINTEXT config fields the target
// hands to the vault-aware indexer store for at-rest re-encryption.
type ProwlarrIndexerRef struct {
	Name           string
	Kind           string // "torznab" | "nzb"
	BaseURL        string
	APIKey         string         // PLAINTEXT source value; re-encrypted by the store
	DefinitionName string         // definition library id (or generic fallback)
	ConfigFields   map[string]any // PLAINTEXT contract values
	Enable         bool
	Priority       int
	Privacy        string
	SyncLevel      string
	Redirect       bool
	Tags           []string // resolved tag labels
}

// ProwlarrDCRef is the resolved write payload for one download client.
type ProwlarrDCRef struct {
	Name        string
	Kind        downloadclients.Kind
	Enabled     bool
	Position    int
	BaseURL     string
	Username    string
	APIKey      string // PLAINTEXT source value; re-encrypted by the store
	CompleteDir string
}

// ProwlarrProxyRef is the resolved write payload for one indexer proxy.
type ProwlarrProxyRef struct {
	Name     string
	URL      string
	Username string
	Password string // PLAINTEXT source value; re-encrypted by the store
	Enabled  bool
	Tags     []string // resolved tag labels
}

// ProwlarrMigrationTarget is the write seam the Prowlarr migrator drives.
// The Postgres target (ProwlarrPostgresTarget) implements it against the
// existing tag settings, indexer, download-client, and indexer-proxy
// stores; unit tests use a fake to assert dry-run / idempotency / resume.
type ProwlarrMigrationTarget interface {
	// EnsureTag upserts a tag by id (Prowlarr tag ids are preserved so
	// indexer/proxy references resolve) and returns (id, action).
	EnsureTag(ctx context.Context, t ProwlarrTag) (int64, TargetAction, error)
	// EnsureIndexer upserts an indexer by name and returns (id, action).
	EnsureIndexer(ctx context.Context, ref ProwlarrIndexerRef) (int64, TargetAction, error)
	// EnsureDownloadClient upserts a download client by (name, kind) and
	// returns the action (Prowlarr runs several clients; each is ensured).
	EnsureDownloadClient(ctx context.Context, ref ProwlarrDCRef) (TargetAction, error)
	// EnsureIndexerProxy upserts a proxy by name and returns (id, action).
	EnsureIndexerProxy(ctx context.Context, ref ProwlarrProxyRef) (int64, TargetAction, error)
}

// ProwlarrMigrator drives Prowlarr -> Mediarr: it reads the source through
// the ProwlarrClient seam, resolves definition names against the built-in
// definition library (with the generic-torznab/generic-newznab fallback),
// and writes through the ProwlarrMigrationTarget seam. It is dry-run,
// idempotent, and resumable like the other *arr migrators.
type ProwlarrMigrator struct {
	NewSource func(baseURL, apiKey string) ProwlarrClient
	Target    ProwlarrMigrationTarget

	mu         sync.Mutex
	status     string
	running    bool
	startedAt  *time.Time
	finishedAt *time.Time
	done       int
	total      int
	lastResult *MigrationResult
	migrated   map[string]bool
}

// NewProwlarrMigrator builds the Prowlarr migrator over a real target.
func NewProwlarrMigrator(target ProwlarrMigrationTarget) *ProwlarrMigrator {
	return &ProwlarrMigrator{
		NewSource: NewProwlarrClient,
		Target:    target,
		migrated:  map[string]bool{},
	}
}

// Status implements Migrator.
func (m *ProwlarrMigrator) Status() MigrationStatus {
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

// Run executes a Prowlarr migration. On a target/source error mid-run it
// records a partial (resumable) status and returns the mapping report for
// what completed.
func (m *ProwlarrMigrator) Run(ctx context.Context, req MigrationRequest) (*MigrationResult, error) {
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
	// NOTE: m.migrated is deliberately NOT reset here — a resumed run must
	// see the entities an earlier partial run already wrote.
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

// runLocked performs the migration. The caller holds nothing; this is the
// single implementation Run wraps.
func (m *ProwlarrMigrator) runLocked(ctx context.Context, req MigrationRequest) (*MigrationResult, error) {
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

	// 1. Tags: fetched whenever they are requested OR needed to resolve
	// indexer/proxy tag references, and imported when wanted.
	tags := []ProwlarrTag{}
	tagsByID := map[int]string{}
	if inc.wantTags() || inc.wantIndexers() || inc.wantIndexerProxies() {
		fetched, err := src.GetTags(ctx)
		if err != nil {
			return result, fmt.Errorf("fetching prowlarr tags: %w", err)
		}
		tags = fetched
		for _, t := range tags {
			tagsByID[t.ID] = t.Label
		}
	}
	if inc.wantTags() {
		for _, t := range tags {
			if req.DryRun {
				result.Items = append(result.Items, MigrationItem{Entity: "tag", SourceTitle: t.Label, Action: "dry-run"})
				result.Counts.Total++
				continue
			}
			_, action, err := m.Target.EnsureTag(ctx, t)
			if err != nil {
				result.Counts.Total++
				result.Counts.Errors++
				result.Items = append(result.Items, MigrationItem{Entity: "tag", SourceTitle: t.Label, Action: "error", Message: err.Error()})
				result.Resumable = true
				return result, err
			}
			addMigrated(result, MigrationItem{Entity: "tag", SourceTitle: t.Label, Action: string(action)})
		}
	}

	// 2. Indexers: the primary entity (resume ledger + progress denominator).
	if inc.wantIndexers() {
		fetched, err := src.GetIndexers(ctx)
		if err != nil {
			return result, fmt.Errorf("fetching prowlarr indexers: %w", err)
		}
		indexers := fetched
		m.mu.Lock()
		m.total = len(indexers)
		m.mu.Unlock()

		for _, ix := range indexers {
			key := prowlarrIndexerKey(ix)
			m.mu.Lock()
			resumeSkip := req.Resume && m.migrated[key]
			m.done++
			m.mu.Unlock()

			if resumeSkip {
				result.Items = append(result.Items, MigrationItem{Entity: "indexer", ExternalID: key, SourceTitle: ix.Name, Action: "skipped", Message: "already migrated (resume)"})
				result.Counts.Total++
				result.Counts.Skipped++
				continue
			}
			if req.DryRun {
				result.Items = append(result.Items, MigrationItem{Entity: "indexer", ExternalID: key, SourceTitle: ix.Name, Action: "dry-run"})
				result.Counts.Total++
				continue
			}
			ref, notes := resolveProwlarrIndexer(ix, tagsByID)
			id, action, err := m.Target.EnsureIndexer(ctx, ref)
			if err != nil {
				result.Counts.Total++
				result.Counts.Errors++
				result.Items = append(result.Items, MigrationItem{Entity: "indexer", ExternalID: key, SourceTitle: ix.Name, Action: "error", Message: strings.Join(notes, "; ") + " " + err.Error()})
				// An indexer failure is partial + resumable: stop, remember
				// what succeeded so far, and let a resume continue.
				result.Resumable = true
				return result, err
			}
			item := MigrationItem{Entity: "indexer", ExternalID: key, SourceTitle: ix.Name, Action: string(action), TargetID: id}
			if len(notes) > 0 {
				item.Message = strings.Join(notes, "; ")
			}
			addMigrated(result, item)
			m.mu.Lock()
			m.migrated[key] = true
			m.mu.Unlock()
		}
	}

	// 3. Download clients: Prowlarr runs several; ensure each mappable one.
	if inc.wantDownloadClient() {
		fetched, err := src.GetDownloadClients(ctx)
		if err != nil {
			return result, fmt.Errorf("fetching prowlarr download clients: %w", err)
		}
		for pos, c := range fetched {
			ref, skipMsg := mapProwlarrDownloadClient(c, pos+1)
			if ref == nil {
				addMigrated(result, MigrationItem{Entity: "download_client", SourceTitle: c.Name, Action: "skipped", Message: skipMsg})
				continue
			}
			if req.DryRun {
				result.Items = append(result.Items, MigrationItem{Entity: "download_client", SourceTitle: c.Name, Action: "dry-run"})
				result.Counts.Total++
				continue
			}
			action, err := m.Target.EnsureDownloadClient(ctx, *ref)
			if err != nil {
				result.Counts.Total++
				result.Counts.Errors++
				result.Items = append(result.Items, MigrationItem{Entity: "download_client", SourceTitle: c.Name, Action: "error", Message: err.Error()})
				result.Resumable = true
				return result, err
			}
			addMigrated(result, MigrationItem{Entity: "download_client", SourceTitle: c.Name, Action: string(action)})
		}
	}

	// 4. Indexer proxies (FlareSolverr endpoints).
	if inc.wantIndexerProxies() {
		fetched, err := src.GetIndexerProxies(ctx)
		if err != nil {
			return result, fmt.Errorf("fetching prowlarr indexer proxies: %w", err)
		}
		for _, p := range fetched {
			if req.DryRun {
				result.Items = append(result.Items, MigrationItem{Entity: "indexer_proxy", SourceTitle: p.Name, Action: "dry-run"})
				result.Counts.Total++
				continue
			}
			ref := mapProwlarrIndexerProxy(p, tagsByID)
			id, action, err := m.Target.EnsureIndexerProxy(ctx, ref)
			if err != nil {
				result.Counts.Total++
				result.Counts.Errors++
				result.Items = append(result.Items, MigrationItem{Entity: "indexer_proxy", SourceTitle: p.Name, Action: "error", Message: err.Error()})
				result.Resumable = true
				return result, err
			}
			addMigrated(result, MigrationItem{Entity: "indexer_proxy", SourceTitle: p.Name, Action: string(action), TargetID: id})
		}
	}

	// 5. The mapping report must state explicitly what Prowlarr exports that
	// Mediarr deliberately does not import (spec: the Apps section is
	// replaced by per-kind Mediarr settings).
	addMigrated(result, MigrationItem{
		Entity:  "apps",
		Action:  "skipped",
		Message: "Prowlarr apps are not imported: Mediarr replaces the apps section with per-kind settings (download clients, indexer proxies, and per-protocol settings)",
	})

	return result, nil
}

// prowlarrIndexerKey is the stable resume bookkeeping key for an indexer:
// the name is the Mediarr-local identity the import converges on.
func prowlarrIndexerKey(ix ProwlarrIndexer) string {
	if ix.Name != "" {
		return "indexer:" + ix.Name
	}
	return fmt.Sprintf("indexer:id%d", ix.ID)
}

// resolveProwlarrIndexer resolves a source indexer into a write payload:
// the definition library lookup with the spec's fallback — if the
// definitionName is not in Mediarr's built-in definition library the
// indexer is imported as Generic Torznab (torrent) or Generic Newznab
// (usenet) carrying its stored config fields — and tag-id → label
// resolution. It returns the ref plus report notes; the notes never carry
// credential values.
func resolveProwlarrIndexer(ix ProwlarrIndexer, tagsByID map[int]string) (ProwlarrIndexerRef, []string) {
	notes := []string{}

	tags := make([]string, 0, len(ix.Tags))
	for _, id := range ix.Tags {
		if label, ok := tagsByID[id]; ok {
			tags = append(tags, label)
		} else {
			notes = append(notes, fmt.Sprintf("tag id %d not found in source tags; dropped", id))
		}
	}

	// Config fields pass through PLAINTEXT (the store re-encrypts at rest);
	// copy the map so baseurl mirroring below never mutates the source DTO.
	fields := map[string]any{}
	for k, v := range ix.ConfigFields {
		fields[k] = v
	}

	// Resolve the definition name against the built-in library.
	def := indexerdefinitions.Default().ResolveDefinition(ix.DefinitionName)
	var kind, defID string
	switch {
	case def != nil:
		defID = def.ID
		kind = kindForProtocol(def.Protocol)
		if def.Field("baseurl") != nil {
			mirrorBaseURL(fields, ix.BaseURL)
		}
	case ix.DefinitionName != "":
		// Fallback (spec): unknown definition name → generic carrying the
		// stored config fields. Heuristic: usenet when the name or base URL
		// says so; everything else is treated as a torrent/Torznab index.
		if isUsenetHint(ix) {
			defID, kind = "generic-newznab", "nzb"
		} else {
			defID, kind = "generic-torznab", "torznab"
		}
		mirrorBaseURL(fields, ix.BaseURL)
		notes = append(notes, fmt.Sprintf("definition %q is not in the Mediarr definition library; imported as %s with the stored config fields", ix.DefinitionName, defID))
	default:
		defID, kind = "generic-torznab", "torznab"
		mirrorBaseURL(fields, ix.BaseURL)
		notes = append(notes, "source indexer has no definition name; imported as generic-torznab")
	}

	// Required contract fields missing from the source payload are noted (not
	// fatal: the source index works with its own definition revision). Only
	// applicable for a known definition — the generic fallbacks carry the
	// stored fields verbatim.
	if def != nil {
		for _, f := range def.Fields {
			if f.Required {
				if v, ok := fields[f.Name]; !ok || strOf(v) == "" {
					notes = append(notes, fmt.Sprintf("field %q required by %s is missing or empty in the source", f.Name, defID))
				}
			}
		}
	}

	ref := ProwlarrIndexerRef{
		Name:           ix.Name,
		Kind:           kind,
		BaseURL:        ix.BaseURL,
		APIKey:         strOf(fields["apikey"]),
		DefinitionName: defID,
		ConfigFields:   fields,
		Enable:         resolveProwlarrEnable(ix),
		Priority:       ix.Priority,
		Privacy:        ix.Privacy,
		SyncLevel:      ix.SyncLevel,
		Redirect:       ix.Redirect,
		Tags:           tags,
	}
	if ref.Priority == 0 {
		ref.Priority = 25
	}
	if ref.Privacy == "" {
		ref.Privacy = "public"
	}
	if ref.SyncLevel == "" {
		ref.SyncLevel = "standard"
	}
	return ref, notes
}

// kindForProtocol maps a definition library protocol to the Mediarr wire
// indexer kind.
func kindForProtocol(protocol string) string {
	switch protocol {
	case "usenet":
		return "nzb"
	default: // "torrent" (the library's only other protocol)
		return "torznab"
	}
}

// isUsenetHint is the fallback protocol heuristic: the definition name or
// base URL naming the index usenet-shaped. Torrent is the default.
func isUsenetHint(ix ProwlarrIndexer) bool {
	hay := strings.ToLower(ix.DefinitionName + " " + ix.BaseURL)
	for _, h := range []string{"newznab", "nzb", "usenet"} {
		if strings.Contains(hay, h) {
			return true
		}
	}
	return false
}

// mirrorBaseURL sets the contract's baseurl field to the source base URL
// (the PR2 write path convention: the stored base URL is authoritative).
func mirrorBaseURL(fields map[string]any, base string) {
	if base == "" {
		return
	}
	fields["baseurl"] = base
}

// resolveProwlarrEnable resolves the imported enabled flag: an explicit
// per-search toggle wins (when Prowlarr provides one), else the indexer's
// overall enable.
func resolveProwlarrEnable(ix ProwlarrIndexer) bool {
	for _, v := range []*bool{ix.EnableRss, ix.EnableAutomatic, ix.EnableInteractive} {
		if v != nil {
			return *v
		}
	}
	return ix.Enable
}

// strOf coerces a normalized config-field value to a string.
func strOf(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", s)
	}
}

// mapProwlarrDownloadClient resolves a Prowlarr download client into a
// Mediarr record, or (nil, reason) when the client type has no Mediarr
// equivalent. Field names are the Prowlarr contract names (host/port,
// username, apikey/password, completedDownloadDir).
func mapProwlarrDownloadClient(c ProwlarrDownloadClient, position int) (*ProwlarrDCRef, string) {
	name := c.Name
	if name == "" {
		name = c.ClientType
	}
	base := c.BaseURL
	if base == "" {
		base = hostPort(c.Fields)
	}
	switch {
	case containsFold(c.ClientType, "qbittorrent") || containsFold(name, "qbittorrent"):
		return &ProwlarrDCRef{
			Name:        "qBittorrent",
			Kind:        downloadclients.KindQB,
			Enabled:     c.Enable,
			Position:    position,
			BaseURL:     base,
			Username:    c.Fields["username"],
			APIKey:      firstNonEmpty(c.Fields["apikey"], c.Fields["api_key"], c.Fields["password"]),
			CompleteDir: c.Fields["completedDownloadDir"],
		}, ""
	case containsFold(c.ClientType, "sabnzbd") || containsFold(name, "sabnzbd"):
		return &ProwlarrDCRef{
			Name:        "SABnzbd",
			Kind:        downloadclients.KindSAB,
			Enabled:     c.Enable,
			Position:    position,
			BaseURL:     base,
			APIKey:      firstNonEmpty(c.Fields["apikey"], c.Fields["api_key"], c.Fields["username"]),
			CompleteDir: c.Fields["completedDownloadDir"],
		}, ""
	case containsFold(c.ClientType, "transmission") || containsFold(name, "transmission"):
		return &ProwlarrDCRef{
			Name:     "Transmission",
			Kind:     downloadclients.KindTransmission,
			Enabled:  c.Enable,
			Position: position,
			BaseURL:  base,
			Username: c.Fields["username"],
			APIKey:   firstNonEmpty(c.Fields["password"], c.Fields["apikey"]),
		}, ""
	default:
		return nil, fmt.Sprintf("client type %q is not imported (Mediarr supports qBittorrent, SABnzbd, and Transmission)", c.ClientType)
	}
}

// hostPort builds a host:port base URL from a Prowlarr client's fields when
// the client has no top-level baseUrl.
func hostPort(fields map[string]string) string {
	host, port := fields["host"], fields["port"]
	if host == "" {
		return ""
	}
	if port != "" {
		return host + ":" + port
	}
	if !strings.Contains(host, "://") {
		return "http://" + host
	}
	return host
}

// containsFold is a case-insensitive substring check.
func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// mapProwlarrIndexerProxy resolves a source proxy (FlareSolverr endpoint)
// into a Mediarr proxy payload.
func mapProwlarrIndexerProxy(p ProwlarrIndexerProxy, tagsByID map[int]string) ProwlarrProxyRef {
	tags := make([]string, 0, len(p.Tags))
	for _, id := range p.Tags {
		if label, ok := tagsByID[id]; ok {
			tags = append(tags, label)
		}
	}
	return ProwlarrProxyRef{
		Name:     p.Name,
		URL:      p.BaseURL,
		Username: p.Fields["username"],
		Password: p.Fields["password"],
		Enabled:  p.Enable,
		Tags:     tags,
	}
}
