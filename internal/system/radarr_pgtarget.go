package system

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/brandenk514/mediarr/internal/settings"
)

// ---------------------------------------------------------------------------
// RadarrPostgresTarget: the real Mediarr import sink for the Radarr drop-in
// migration (R6). It maps the Radarr entity classes onto Mediarr's stores:
//
//	quality profiles -> the profiles section's movie_profiles array (R5a)
//	tags             -> the settings "tags" section
//	movies           -> the movie domain (the R5 import-bridge identity:
//	                     title+year; a movie maps to a local movie + editions
//	                     with the source monitored state)
//	editions         -> the movie domain's editions (label-keyed upsert)
//
// Every Ensure* is idempotent: an entity that already exists is returned
// with ActionExists and is not duplicated. Profiles are matched by NAME (the
// R5 profile store seeds the 14 Radarr v6 defaults, so most names map 1:1);
// an unknown source profile name is created in the document and the mapping
// report notes it.
// ---------------------------------------------------------------------------

// RadarrPostgresTarget implements RadarrMigrationTarget against real
// Mediarr stores.
type RadarrPostgresTarget struct {
	Settings SettingsStore
	Movies   RadarrMovieStore
	Indexers RadarrIndexerStore
}

// NewRadarrPostgresTarget builds the Radarr drop-in import sink.
func NewRadarrPostgresTarget(st SettingsStore, movies RadarrMovieStore, ix RadarrIndexerStore) *RadarrPostgresTarget {
	return &RadarrPostgresTarget{Settings: st, Movies: movies, Indexers: ix}
}

// EnsureMovieProfile upserts a Radarr movie quality profile into the
// profiles section's movie_profiles array. The source profile NAME is the
// key: a seeded (1:1) name is reported ActionExists; an unknown name is
// appended (the migrator notes it as "created in the document").
func (t *RadarrPostgresTarget) EnsureMovieProfile(ctx context.Context, p RadarrQualityProfile) (int64, TargetAction, error) {
	doc, err := t.movieProfilesDoc(ctx)
	if err != nil {
		return 0, "", err
	}
	for i := range doc.MovieProfiles {
		if doc.MovieProfiles[i].Name == p.Name {
			return int64(doc.MovieProfiles[i].ID), ActionExists, nil
		}
	}
	id := nextMovieProfileID(doc)
	doc.MovieProfiles = append(doc.MovieProfiles, settings.QualityProfile{
		ID:     id,
		Name:   p.Name,
		Cutoff: mapRadarrCutoff(p),
		Items:  mapRadarrProfileItems(p),
	})
	if err := t.putMovieProfiles(ctx, doc); err != nil {
		return 0, "", err
	}
	return int64(id), ActionCreated, nil
}

func (t *RadarrPostgresTarget) movieProfilesDoc(ctx context.Context) (*settings.ProfilesSettings, error) {
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

func (t *RadarrPostgresTarget) putMovieProfiles(ctx context.Context, doc *settings.ProfilesSettings) error {
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return t.Settings.PutSectionDocument(ctx, settings.SectionProfiles, raw)
}

// nextMovieProfileID is the next free profile id in the document (max id + 1
// across the TV and movie profile arrays).
func nextMovieProfileID(doc *settings.ProfilesSettings) int {
	max := 0
	for _, p := range doc.Profiles {
		if p.ID > max {
			max = p.ID
		}
	}
	for _, p := range doc.MovieProfiles {
		if p.ID > max {
			max = p.ID
		}
	}
	return max + 1
}

// mapRadarrProfileItems maps a Radarr profile's approval set onto Mediarr's
// 13 Radarr v6 movie levels (level id space 100+). Names are matched
// case-insensitively with "WebDL" -> "Web" normalization; a level the source
// profile does not mention is not approved.
func mapRadarrProfileItems(p RadarrQualityProfile) []settings.QualityProfileItem {
	approved := map[string]bool{}
	for _, it := range p.Items {
		approved[normalizeRadarrQualityName(it.Quality.Name)] = it.Approved
	}
	levels := settings.DefaultMovieLevels()
	out := make([]settings.QualityProfileItem, 0, len(levels))
	for _, l := range levels {
		a, _ := approved[normalizeRadarrQualityName(l.Name)]
		out = append(out, settings.QualityProfileItem{Quality: l.ID, Approved: a})
	}
	return out
}

// mapRadarrCutoff maps a Radarr profile's cutoff onto the local level id
// space: the highest approved movie level (the Radarr cutoff level id itself
// is source-space and cannot be translated without the source's quality
// levels endpoint, which the migration does not depend on).
func mapRadarrCutoff(p RadarrQualityProfile) int {
	approved := map[string]bool{}
	for _, it := range p.Items {
		approved[normalizeRadarrQualityName(it.Quality.Name)] = it.Approved
	}
	cutoff := 0
	for _, l := range settings.DefaultMovieLevels() {
		if approved[normalizeRadarrQualityName(l.Name)] && l.ID > cutoff {
			cutoff = l.ID
		}
	}
	return cutoff
}

// normalizeRadarrQualityName folds a quality level name for matching:
// lowercase, collapse internal whitespace, "webdl" -> "web" (Radarr spells
// the web tiers "WEB"; both sides normalize to the same key).
func normalizeRadarrQualityName(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "webdl", "web"))
	return strings.Join(strings.Fields(s), " ")
}

// EnsureTag upserts a Radarr tag into the tags section, preserving its id so
// movie tag references resolve back to the imported tag.
func (t *RadarrPostgresTarget) EnsureTag(ctx context.Context, tag RadarrTag) (int64, TargetAction, error) {
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

func (t *RadarrPostgresTarget) tagsDoc(ctx context.Context) (*settings.TagsSettings, error) {
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

func (t *RadarrPostgresTarget) putTags(ctx context.Context, doc *settings.TagsSettings) error {
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return t.Settings.PutSectionDocument(ctx, settings.SectionTags, raw)
}

// TagLabels resolves a set of Radarr tag ids to their labels (the RadarrTag
// Store capability the migrator uses for movie tag references).
func (t *RadarrPostgresTarget) TagLabels(ctx context.Context, ids []int) ([]string, error) {
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

// EnsureMovie imports a Radarr movie into the movie domain. It is
// idempotent: an existing movie (matched by title+year, the domain's
// identity) is returned with ActionExists. An empty quality-profile name
// resolves to the factory default movie profile; a name missing from the
// document is ensured defensively (the migrator ensures every source
// profile first).
func (t *RadarrPostgresTarget) EnsureMovie(ctx context.Context, ref RadarrMovieRef) (int64, TargetAction, error) {
	if t.Movies == nil {
		return 0, "", fmt.Errorf("migration: movie store not wired")
	}
	title := strings.TrimSpace(ref.Title)
	if title == "" {
		return 0, "", fmt.Errorf("migration: movie has no title")
	}
	if id, found := t.Movies.FindMovie(ctx, title, ref.Year); found {
		return id, ActionExists, nil
	}
	if ref.QualityProfile == "" {
		ref.QualityProfile = settings.DefaultMovieQualityProfile
	}
	if err := t.ensureMovieProfileName(ctx, ref.QualityProfile); err != nil {
		return 0, "", err
	}
	id, err := t.Movies.AddMovie(ctx, ref)
	if err != nil {
		return 0, "", err
	}
	return id, ActionCreated, nil
}

// ensureMovieProfileName appends a bare movie profile (every movie level
// approved) to the profiles document when the name is not there yet.
func (t *RadarrPostgresTarget) ensureMovieProfileName(ctx context.Context, name string) error {
	doc, err := t.movieProfilesDoc(ctx)
	if err != nil {
		return err
	}
	for i := range doc.MovieProfiles {
		if doc.MovieProfiles[i].Name == name {
			return nil
		}
	}
	levels := settings.DefaultMovieLevels()
	items := make([]settings.QualityProfileItem, 0, len(levels))
	cutoff := 0
	for _, l := range levels {
		items = append(items, settings.QualityProfileItem{Quality: l.ID, Approved: true})
		if l.ID > cutoff {
			cutoff = l.ID
		}
	}
	doc.MovieProfiles = append(doc.MovieProfiles, settings.QualityProfile{
		ID: nextMovieProfileID(doc), Name: name, Cutoff: cutoff, Items: items,
	})
	return t.putMovieProfiles(ctx, doc)
}

// EnsureEditions imports a movie's editions, upserting each by (movie id,
// label) — the 'Theatrical' edition seeded on movie create is updated, not
// duplicated. It returns the number of editions newly created.
func (t *RadarrPostgresTarget) EnsureEditions(ctx context.Context, movieID int64, eds []RadarrEditionRef) (int, error) {
	if t.Movies == nil {
		return 0, fmt.Errorf("migration: movie store not wired")
	}
	created := 0
	for _, e := range eds {
		e := e
		if e.Quality == "" {
			e.Quality = settings.DefaultMovieQualityProfile
		}
		_, isNew, err := t.Movies.EnsureEdition(ctx, movieID, e)
		if err != nil {
			return created, err
		}
		if isNew {
			created++
		}
	}
	return created, nil
}

// EnsureIndexer upserts a Radarr indexer into the indexer definition store.
// The Radarr R5a per-indexer booleans map onto the R5a indexer settings:
// enableRss -> rss, enableAutomatic -> auto_search,
// enableInteractive -> interactive_search.
func (t *RadarrPostgresTarget) EnsureIndexer(ctx context.Context, ix RadarrIndexer) (int64, TargetAction, error) {
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
	id, err := t.Indexers.CreateIndexer(ctx, ix.Name, kind, ix.BaseURL, ix.APIKey, ix.Enable, ix.EnableRss, ix.EnableAutomatic, ix.EnableInteractive)
	if err != nil {
		return 0, "", err
	}
	return id, ActionCreated, nil
}

// SetDownloadClient maps a Radarr download client onto the Mediarr
// download-client config (best-effort: qBittorrent and SABnzbd field names,
// the same mapping as the Sonarr run).
func (t *RadarrPostgresTarget) SetDownloadClient(ctx context.Context, c RadarrDownloadClient) (TargetAction, error) {
	existing, err := t.Settings.GetDownloadClient(ctx)
	if err != nil {
		return "", err
	}
	prevSet := existing != nil && existing.Kind != "" && existing.Kind != "mock"
	dc := &settings.DownloadClient{Kind: "mock"}
	switch {
	case strings.Contains(strings.ToLower(c.ClientKey), "qbit"):
		dc.Kind = "qbittorrent"
		dc.QBBase = sonarrURLField(c.Fields, "host", "port", "url", "baseUrl")
		dc.QBUsername = sonarrStringField(c.Fields, "username")
		dc.QBPassword = sonarrStringField(c.Fields, "password")
		dc.QBAPIKey = sonarrStringField(c.Fields, "apikey", "api_key")
	case strings.Contains(strings.ToLower(c.ClientKey), "sab"):
		dc.Kind = "sabnzbd"
		dc.SABBase = sonarrStringField(c.Fields, "host", "serverhost", "url", "baseUrl")
		dc.SABAPIKey = sonarrStringField(c.Fields, "apikey", "api_key")
		dc.SABCompleteDir = sonarrStringField(c.Fields, "completeDir", "complete_dir")
		dc.QBPassword = sonarrStringField(c.Fields, "password") // unused for sab; keep zero
	}
	if !c.Enable {
		// A disabled client is stored as the mock kind (Mediarr has no
		// per-client enable flag); the mapping report records the intent.
		dc.Kind = "mock"
	}
	if err := t.Settings.PutDownloadClient(ctx, dc); err != nil {
		return "", err
	}
	if prevSet {
		return ActionUpdated, nil
	}
	return ActionCreated, nil
}

// ApplyMediaManagement maps the Radarr media-management config onto the
// Mediarr media-management document (the best-effort subset: the fields the
// document can hold) and returns the number of fields written. An empty
// document starts from the defaults.
func (t *RadarrPostgresTarget) ApplyMediaManagement(ctx context.Context, mm RadarrMediaManagement) (int, error) {
	raw, err := t.Settings.GetSectionDocument(ctx, settings.SectionMediaManagement)
	if err != nil {
		return 0, err
	}
	d, err := settings.SectionMediaManagement.DecodeDocument(raw)
	if err != nil {
		return 0, fmt.Errorf("decode media management: %w", err)
	}
	doc := d.(*settings.MediaManagementSettings)
	applied := 0
	doc.MovieNaming.RenameMovies = mm.MovieNaming.RenameMovies
	applied++
	doc.MovieNaming.ReplaceIllegalCharacters = mm.MovieNaming.ReplaceIllegalCharacters
	applied++
	if mm.MovieNaming.ColonReplacement != "" {
		doc.MovieNaming.ColonReplacement = mm.MovieNaming.ColonReplacement
		applied++
	}
	if mm.MovieNaming.StandardFormat != "" {
		doc.MovieNaming.StandardFormat = mm.MovieNaming.StandardFormat
		applied++
	}
	doc.Management.ImportExtraFiles = mm.FileManagement.ImportExtraFiles
	applied++
	doc.Management.UnmonitorDeletedMovies = mm.FileManagement.UnmonitorDeletedMovies
	applied++
	// Radarr stores the min file age in minutes; the Mediarr document in
	// whole hours — only map a clean conversion.
	if mm.FileManagement.MinFileAgeMinutes > 0 && mm.FileManagement.MinFileAgeMinutes%60 == 0 {
		doc.Management.MinFileAgeHours = mm.FileManagement.MinFileAgeMinutes / 60
		applied++
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return 0, err
	}
	if err := t.Settings.PutSectionDocument(ctx, settings.SectionMediaManagement, out); err != nil {
		return 0, err
	}
	return applied, nil
}

var _ RadarrMigrationTarget = (*RadarrPostgresTarget)(nil)
var _ RadarrTagStore = (*RadarrPostgresTarget)(nil)
