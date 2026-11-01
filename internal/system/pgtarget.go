package system

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/brandenk514/mediarr/internal/downloadclients"
	"github.com/brandenk514/mediarr/internal/settings"
)

// ---------------------------------------------------------------------------
// PostgresTarget: the real Mediarr import sink for the Sonarr drop-in
// migration. It maps the six entity classes onto Mediarr's stores:
//
//	quality profiles -> the settings "profiles" section
//	tags             -> the settings "tags" section
//	indexers         -> the indexer definition store
//	download client  -> the settings download-client config
//	series           -> the TV library (through the tv service)
//	episodes         -> the TV library (through the tv service)
//
// Every Ensure* is idempotent: an entity that already exists is returned with
// ActionExists and is not duplicated. The target preserves a Sonarr entity's
// original id (for profiles and tags) so a series' qualityProfileId/tags
// reference back to the imported profile/tag by id.
// ---------------------------------------------------------------------------

// SettingsStore is the settings-store surface the Postgres target uses. The
// Postgres settings repo (settings.Store) satisfies it directly. (Download
// clients are not settings documents any more — since Prowlarr PR5 the target
// upserts them through the DCRepo field on each target struct.)
type SettingsStore interface {
	GetSectionDocument(ctx context.Context, s settings.Section) ([]byte, error)
	PutSectionDocument(ctx context.Context, s settings.Section, doc []byte) error
}

// IndexerStore is the indexer-write surface. main.go adapts the Postgres
// indexer repo (indexers.IndexerRepo) to it.
type IndexerStore interface {
	// FindIndexer returns the id of the indexer named name, or found=false.
	FindIndexer(ctx context.Context, name string) (id int64, found bool)
	// CreateIndexer inserts a new indexer definition and returns its id.
	CreateIndexer(ctx context.Context, name, kind, baseURL, apiKey string, enabled bool) (int64, error)
}

// TVStore is the TV-library write surface. main.go adapts the tv service to it.
type TVStore interface {
	// FindSeries returns the id of an existing series matching tvdbID (when
	// nonzero) or title+year, or found=false.
	FindSeries(ctx context.Context, tvdbID int, title string, year int) (id int64, found bool)
	// AddSeries creates the series and returns its id.
	AddSeries(ctx context.Context, ref SeriesRef) (id int64, err error)
	// EpisodeKeys returns the set of "SxEx" episode keys already in the series.
	EpisodeKeys(ctx context.Context, seriesID int64) (map[string]bool, error)
	// AddEpisode creates a known episode in the series and returns its id.
	AddEpisode(ctx context.Context, seriesID int64, season, episode int, title string, airDate *time.Time, monitored bool) (id int64, err error)
}

// SeriesRef is the minimal series row the TVStore needs to import.
type SeriesRef struct {
	Title          string
	Year           int
	TVDBID         int
	QualityProfile string
	Monitored      bool
	SeasonFolder   bool
	Tags           []string
}

// PostgresTarget implements MigrationTarget against real Mediarr stores.
type PostgresTarget struct {
	Settings SettingsStore
	Indexers IndexerStore
	TV       TVStore
	// DCRepo is the plural saved download-client store (Prowlarr PR5) that
	// SetDownloadClient upserts into. Nil -> SetDownloadClient errors.
	DCRepo downloadclients.Repo
}

// NewPostgresTarget builds the drop-in import sink.
func NewPostgresTarget(st SettingsStore, ix IndexerStore, tv TVStore) *PostgresTarget {
	return &PostgresTarget{Settings: st, Indexers: ix, TV: tv}
}

// EnsureQualityProfile upserts a Sonarr quality profile into the profiles
// section, preserving its Sonarr id so series can reference it by id.
func (t *PostgresTarget) EnsureQualityProfile(ctx context.Context, p SonarrQualityProfile) (int64, TargetAction, error) {
	doc, err := t.profilesDoc(ctx)
	if err != nil {
		return 0, "", err
	}
	for i := range doc.Profiles {
		if doc.Profiles[i].ID == p.ID {
			if doc.Profiles[i].Name != p.Name {
				doc.Profiles[i].Name = p.Name
				if err := t.putProfiles(ctx, doc); err != nil {
					return 0, "", err
				}
			}
			return int64(p.ID), ActionExists, nil
		}
	}
	qp := settings.QualityProfile{
		ID:     p.ID,
		Name:   p.Name,
		Cutoff: p.Cutoff,
		Items:  mapProfileItems(p.Items),
	}
	doc.Profiles = append(doc.Profiles, qp)
	if err := t.putProfiles(ctx, doc); err != nil {
		return 0, "", err
	}
	return int64(p.ID), ActionCreated, nil
}

func (t *PostgresTarget) profilesDoc(ctx context.Context) (*settings.ProfilesSettings, error) {
	raw, err := t.Settings.GetSectionDocument(ctx, settings.SectionProfiles)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		d := settings.DefaultProfiles()
		return &d, nil
	}
	var doc settings.ProfilesSettings
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode profiles section: %w", err)
	}
	return &doc, nil
}

func (t *PostgresTarget) putProfiles(ctx context.Context, doc *settings.ProfilesSettings) error {
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return t.Settings.PutSectionDocument(ctx, settings.SectionProfiles, raw)
}

// profileNameBySonarrID resolves a Sonarr qualityProfileId to the imported
// profile name (the target preserves Sonarr ids on import).
func (t *PostgresTarget) profileNameBySonarrID(ctx context.Context, sonarrID int) (string, error) {
	doc, err := t.profilesDoc(ctx)
	if err != nil {
		return "", err
	}
	for _, p := range doc.Profiles {
		if p.ID == sonarrID {
			return p.Name, nil
		}
	}
	return "", nil // unknown -> empty; the TV store falls back to its default
}

// EnsureTag upserts a Sonarr tag into the tags section, preserving its id.
func (t *PostgresTarget) EnsureTag(ctx context.Context, tag SonarrTag) (int64, TargetAction, error) {
	doc, err := t.tagsDoc(ctx)
	if err != nil {
		return 0, "", err
	}
	for i := range doc.Tags {
		if doc.Tags[i].ID == tag.ID {
			if doc.Tags[i].Label != tag.Label {
				doc.Tags[i].Label = tag.Label
				if err := t.putTags(ctx, doc); err != nil {
					return 0, "", err
				}
			}
			return int64(tag.ID), ActionExists, nil
		}
	}
	doc.Tags = append(doc.Tags, settings.TagInfo{ID: tag.ID, Label: tag.Label})
	if err := t.putTags(ctx, doc); err != nil {
		return 0, "", err
	}
	return int64(tag.ID), ActionCreated, nil
}

func (t *PostgresTarget) tagsDoc(ctx context.Context) (*settings.TagsSettings, error) {
	raw, err := t.Settings.GetSectionDocument(ctx, settings.SectionTags)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		d := settings.DefaultTags()
		return &d, nil
	}
	var doc settings.TagsSettings
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode tags section: %w", err)
	}
	return &doc, nil
}

func (t *PostgresTarget) putTags(ctx context.Context, doc *settings.TagsSettings) error {
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return t.Settings.PutSectionDocument(ctx, settings.SectionTags, raw)
}

// tagLabelsBySonarrIDs resolves a set of Sonarr tag ids to their labels.
func (t *PostgresTarget) tagLabelsBySonarrIDs(ctx context.Context, ids []int) ([]string, error) {
	if len(ids) == 0 {
		return []string{}, nil
	}
	doc, err := t.tagsDoc(ctx)
	if err != nil {
		return nil, err
	}
	labelByID := map[int]string{}
	for _, tg := range doc.Tags {
		labelByID[tg.ID] = tg.Label
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if lbl, ok := labelByID[id]; ok {
			out = append(out, lbl)
		}
	}
	return out, nil
}

// EnsureIndexer upserts a Sonarr indexer into the indexer definition store.
func (t *PostgresTarget) EnsureIndexer(ctx context.Context, ix SonarrIndexer) (int64, TargetAction, error) {
	if t.Indexers == nil {
		return 0, "", fmt.Errorf("migration: indexer store not wired")
	}
	if id, found := t.Indexers.FindIndexer(ctx, ix.Name); found {
		return id, ActionExists, nil
	}
	kind := ix.IndexerType
	if kind == "" {
		kind = "indexer"
	}
	id, err := t.Indexers.CreateIndexer(ctx, ix.Name, kind, ix.BaseURL, ix.APIKey, ix.Enable)
	if err != nil {
		return 0, "", err
	}
	return id, ActionCreated, nil
}

// seedDownloadClientRecord maps a source *arr download client's identity +
// fields onto a Mediarr saved-client record (the best-effort mapping since
// #36: qBittorrent and SABnzbd field names). It returns nil for a client kind
// Mediarr does not import (skipped, not stored) — the legacy "kind=mock"
// placeholder is gone with the single-client config. A disabled mapped client
// is still recorded, with Enabled=false (the plural model has a real enable
// flag). The source password field is dropped: the saved model authenticates
// qBittorrent via its API key.
func seedDownloadClientRecord(clientKey string, fields map[string]json.RawMessage, enable bool) *downloadclients.Record {
	name := ""
	var kind downloadclients.Kind
	var base, username, apiKey, completeDir string
	switch {
	case strings.Contains(strings.ToLower(clientKey), "qbit"):
		name, kind = "qBittorrent", downloadclients.KindQB
		base = sonarrURLField(fields, "host", "port", "url", "baseUrl")
		username = sonarrStringField(fields, "username")
		apiKey = sonarrStringField(fields, "apikey", "api_key")
	case strings.Contains(strings.ToLower(clientKey), "sab"):
		name, kind = "SABnzbd", downloadclients.KindSAB
		base = sonarrStringField(fields, "host", "serverhost", "url", "baseUrl")
		apiKey = sonarrStringField(fields, "apikey", "api_key")
		completeDir = sonarrStringField(fields, "completeDir", "complete_dir")
	}
	if name == "" {
		return nil
	}
	return &downloadclients.Record{
		Name:        name,
		Kind:        kind,
		Enabled:     enable,
		Position:    1,
		BaseURL:     base,
		Username:    username,
		APIKey:      apiKey,
		CompleteDir: completeDir,
	}
}

// upsertDownloadClient inserts or updates a named saved client, returning
// ActionCreated / ActionUpdated, or ActionExists for an unmapped client (nil
// rec) or an identical re-apply (idempotency, like every other Ensure*).
func upsertDownloadClient(ctx context.Context, repo downloadclients.Repo, rec *downloadclients.Record) (TargetAction, error) {
	if rec == nil {
		return ActionExists, nil
	}
	if repo == nil {
		return "", fmt.Errorf("migration: download client store not wired")
	}
	existing, err := repo.GetByName(ctx, rec.Name)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
		if _, _, err := repo.Create(ctx, *rec); err != nil {
			return "", err
		}
		return ActionCreated, nil
	}
	if existing.Kind == rec.Kind && existing.BaseURL == rec.BaseURL && existing.Username == rec.Username &&
		existing.APIKey == rec.APIKey && existing.CompleteDir == rec.CompleteDir && existing.Enabled == rec.Enabled {
		return ActionExists, nil
	}
	rec.ID = existing.ID
	rec.CreatedAt = existing.CreatedAt
	rec.UpdatedAt = existing.UpdatedAt
	if err := repo.Update(ctx, *rec); err != nil {
		return "", err
	}
	return ActionUpdated, nil
}

// SetDownloadClient maps a Sonarr download client into the Mediarr saved
// download-client list (Prowlarr PR5: a named row upserted by name, best-
// effort qBittorrent / SABnzbd field mapping).
func (t *PostgresTarget) SetDownloadClient(ctx context.Context, c SonarrDownloadClient) (TargetAction, error) {
	return upsertDownloadClient(ctx, t.DCRepo, seedDownloadClientRecord(c.ClientKey, c.Fields, c.Enable))
}

// EnsureSeries imports a Sonarr series into the TV library. It is idempotent:
// an existing series (matched by TVDB id, else title+year) is returned with
// ActionExists.
func (t *PostgresTarget) EnsureSeries(ctx context.Context, s SonarrSeries) (int64, TargetAction, error) {
	title := strings.TrimSpace(s.Title)
	if title == "" {
		return 0, "", fmt.Errorf("migration: series has no title")
	}
	if id, found := t.TV.FindSeries(ctx, s.SeriesID, title, s.Year); found {
		return id, ActionExists, nil
	}
	profile, err := t.profileNameBySonarrID(ctx, s.QualityProfileID)
	if err != nil {
		return 0, "", err
	}
	labels, err := t.tagLabelsBySonarrIDs(ctx, s.Tags)
	if err != nil {
		return 0, "", err
	}
	ref := SeriesRef{
		Title:          title,
		Year:           s.Year,
		TVDBID:         s.SeriesID,
		QualityProfile: profile,
		Monitored:      s.Monitored,
		SeasonFolder:   s.SeasonFolder,
		Tags:           labels,
	}
	id, err := t.TV.AddSeries(ctx, ref)
	if err != nil {
		return 0, "", err
	}
	return id, ActionCreated, nil
}

// EnsureEpisodes imports the episodes of a series, skipping any that already
// exist (matched by season+episode). It returns the number created.
func (t *PostgresTarget) EnsureEpisodes(ctx context.Context, seriesID int64, eps []SonarrEpisode) (int, error) {
	keys, err := t.TV.EpisodeKeys(ctx, seriesID)
	if err != nil {
		return 0, err
	}
	created := 0
	for _, e := range eps {
		key := fmt.Sprintf("S%dxE%d", e.SeasonNumber, e.EpisodeNumber)
		if keys[key] {
			continue
		}
		var air *time.Time
		if e.Airs != "" {
			if ts, err := time.Parse(time.RFC3339, e.Airs); err == nil {
				air = &ts
			}
		}
		if _, err := t.TV.AddEpisode(ctx, seriesID, e.SeasonNumber, e.EpisodeNumber, e.Title, air, e.Monitored); err != nil {
			return created, err
		}
		keys[key] = true
		created++
	}
	return created, nil
}

func mapProfileItems(items []SonarrProfileItem) []settings.QualityProfileItem {
	out := make([]settings.QualityProfileItem, 0, len(items))
	for _, it := range items {
		out = append(out, settings.QualityProfileItem{
			Quality:  it.Quality.ID,
			Approved: it.Approved,
		})
	}
	return out
}

func sonarrStringField(fields map[string]json.RawMessage, keys ...string) string {
	for k := range fields {
		for _, want := range keys {
			if strings.EqualFold(k, want) {
				var v string
				if err := json.Unmarshal(fields[k], &v); err == nil {
					return v
				}
				var vn int
				if err := json.Unmarshal(fields[k], &vn); err == nil {
					return strconv.Itoa(vn)
				}
			}
		}
	}
	return ""
}

// sonarrURLField builds a "scheme://host:port" base URL from host/port fields
// (Sonarr stores qBittorrent's host and port separately).
func sonarrURLField(fields map[string]json.RawMessage, keys ...string) string {
	host := sonarrStringField(fields, "host", "serverhost")
	port := sonarrStringField(fields, "port")
	if base := sonarrStringField(fields, "url", "baseUrl"); base != "" {
		return base
	}
	if host == "" {
		return ""
	}
	if port != "" {
		return "http://" + host + ":" + port
	}
	return "http://" + host
}

var _ MigrationTarget = (*PostgresTarget)(nil)
