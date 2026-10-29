package system

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/brandenk514/mediarr/internal/settings"
)

// ---------------------------------------------------------------------------
// LidarrPostgresTarget: the real Mediarr import sink for the Lidarr drop-in
// migration (L5). It maps the Lidarr entity classes onto Mediarr's stores:
//
//	quality profiles  -> the profiles section's music_profiles array (L3)
//	quality defs      -> the quality section's music_levels (the L3 seed
//	                     ladder; reconciled by the 200+Lidarr-id mapping)
//	metadata profiles -> the profiles section's metadata_profiles array (L3)
//	tags              -> the settings "tags" section
//	root folders      -> the root-folder registry (with derived per-root
//	                     default profiles)
//	indexers          -> the indexer definition store
//	download client   -> the settings download-client config
//	settings subset   -> the media-management document (music naming + file
//	                     management)
//	artists           -> the music domain (name identity)
//	albums + tracks   -> the music domain (title+year / disc+number identity)
//
// Every Ensure* is idempotent: an entity that already exists is returned
// with ActionExists and is not duplicated. Profiles are matched by NAME (the
// L3 profile store seeds the four Lidarr defaults, so most names map 1:1);
// an unknown source profile name is created in the document and the mapping
// report notes it.
// ---------------------------------------------------------------------------

// LidarrPostgresTarget implements LidarrMigrationTarget against real
// Mediarr stores.
type LidarrPostgresTarget struct {
	Settings    SettingsStore
	Music       LidarrMusicStore
	RootFolders LidarrRootFolderStore
	// Indexers reuses the Radarr R5a indexer seam: the per-indexer toggles
	// (rss / auto_search / interactive_search) are the same on both *arrs
	// and land in the same indexer definition.
	Indexers RadarrIndexerStore
}

// NewLidarrPostgresTarget builds the Lidarr drop-in import sink.
func NewLidarrPostgresTarget(
	st SettingsStore,
	music LidarrMusicStore,
	roots LidarrRootFolderStore,
	ix RadarrIndexerStore,
) *LidarrPostgresTarget {
	return &LidarrPostgresTarget{Settings: st, Music: music, RootFolders: roots, Indexers: ix}
}

// ---------------------------------------------------------------------------
// Profiles document (music profiles + metadata profiles).
// ---------------------------------------------------------------------------

func (t *LidarrPostgresTarget) profilesDoc(ctx context.Context) (*settings.ProfilesSettings, error) {
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

func (t *LidarrPostgresTarget) putProfiles(ctx context.Context, doc *settings.ProfilesSettings) error {
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return t.Settings.PutSectionDocument(ctx, settings.SectionProfiles, raw)
}

// EnsureMusicProfile upserts a Lidarr music quality profile into the profiles
// section's music_profiles array. The source profile NAME is the key: a
// seeded (1:1) name is reported ActionExists; an unknown name is appended
// (the migrator notes it as "created in the document").
func (t *LidarrPostgresTarget) EnsureMusicProfile(ctx context.Context, p LidarrQualityProfile) (int64, TargetAction, error) {
	doc, err := t.profilesDoc(ctx)
	if err != nil {
		return 0, "", err
	}
	for i := range doc.MusicProfiles {
		if doc.MusicProfiles[i].Name == p.Name {
			return int64(doc.MusicProfiles[i].ID), ActionExists, nil
		}
	}
	id := nextMusicProfileID(doc)
	doc.MusicProfiles = append(doc.MusicProfiles, settings.QualityProfile{
		ID:     id,
		Name:   p.Name,
		Cutoff: mapLidarrCutoff(p),
		Items:  mapLidarrProfileItems(p),
	})
	if err := t.putProfiles(ctx, doc); err != nil {
		return 0, "", err
	}
	return int64(id), ActionCreated, nil
}

// nextMusicProfileID is the next free music-profile id (max music-profile id +
// 1, floored so new ids stay in the 200+ music space).
func nextMusicProfileID(doc *settings.ProfilesSettings) int {
	max := 199
	for _, p := range doc.MusicProfiles {
		if p.ID > max {
			max = p.ID
		}
	}
	return max + 1
}

// nextMetadataProfileID is the next free metadata-profile id.
func nextMetadataProfileID(doc *settings.ProfilesSettings) int {
	max := 0
	for _, p := range doc.MetadataProfiles {
		if p.ID > max {
			max = p.ID
		}
	}
	return max + 1
}

// localMusicLevelID maps a Lidarr music quality id onto Mediarr's music-level
// id space (L3: "music level id = 200 + Lidarr quality id").
func localMusicLevelID(lidarrID int) int {
	return settings.MusicLevelUnknown + lidarrID
}

// isSeededMusicLevel reports whether id is one of the seeded music levels.
func isSeededMusicLevel(id int) bool {
	for _, l := range settings.DefaultMusicLevels() {
		if l.ID == id {
			return true
		}
	}
	return false
}

// mapLidarrProfileItems maps a source profile's approval set onto the local
// music-level id space (200+). A source quality id outside the seeded ladder
// is dropped (lenient: the document must only reference seeded levels); a
// profile with no mappable items falls back to the default profile's approved
// set so the document stays valid (at least one item is required).
func mapLidarrProfileItems(p LidarrQualityProfile) []settings.QualityProfileItem {
	items := []settings.QualityProfileItem{}
	for _, it := range p.Items {
		id := localMusicLevelID(it.Quality.ID)
		if !isSeededMusicLevel(id) {
			continue
		}
		items = append(items, settings.QualityProfileItem{Quality: id, Approved: it.Approved})
	}
	if len(items) == 0 {
		for _, dp := range settings.DefaultMusicProfiles() {
			if dp.Name == settings.DefaultMusicQualityProfile {
				return dp.Items
			}
		}
	}
	return items
}

// mapLidarrCutoff maps a source profile's cutoff (a source quality id) onto
// the local music-level id space; an out-of-ladder cutoff falls back to the
// highest approved level, else the Unknown level.
func mapLidarrCutoff(p LidarrQualityProfile) int {
	if isSeededMusicLevel(localMusicLevelID(p.Cutoff)) {
		return localMusicLevelID(p.Cutoff)
	}
	highest := 0
	for _, it := range p.Items {
		id := localMusicLevelID(it.Quality.ID)
		if it.Approved && isSeededMusicLevel(id) && id > highest {
			highest = id
		}
	}
	if highest > 0 {
		return highest
	}
	return settings.MusicLevelUnknown
}

// ApplyQualityDefinitions reconciles the source's music quality ladder onto
// the seeded music levels: a level matched by the 200+Lidarr-id mapping has
// its size limit updated when the source provides a non-zero one; a source
// level outside the seeded ladder is appended. It returns the number of
// levels written (updated or appended).
func (t *LidarrPostgresTarget) ApplyQualityDefinitions(ctx context.Context, defs []LidarrQualityDefinition) (int, error) {
	raw, err := t.Settings.GetSectionDocument(ctx, settings.SectionQuality)
	if err != nil {
		return 0, err
	}
	d, err := settings.SectionQuality.DecodeDocument(raw)
	if err != nil {
		return 0, fmt.Errorf("decode quality section: %w", err)
	}
	doc := d.(*settings.QualitySettings)
	written := 0
	byID := map[int]*settings.QualityLevel{}
	for i := range doc.MusicLevels {
		byID[doc.MusicLevels[i].ID] = &doc.MusicLevels[i]
	}
	for _, def := range defs {
		localID := localMusicLevelID(def.ID)
		if l, ok := byID[localID]; ok {
			if def.SizeLimit > 0 && l.SizeLimitMB != def.SizeLimit {
				l.SizeLimitMB = def.SizeLimit
				written++
			}
			continue
		}
		doc.MusicLevels = append(doc.MusicLevels, settings.QualityLevel{
			ID:          localID,
			Name:        def.Name,
			Source:      def.Source,
			Resolution:  def.Resolution,
			SizeLimitMB: def.SizeLimit,
		})
		written++
	}
	if written > 0 {
		out, err := json.Marshal(doc)
		if err != nil {
			return 0, err
		}
		if err := t.Settings.PutSectionDocument(ctx, settings.SectionQuality, out); err != nil {
			return 0, err
		}
	}
	return written, nil
}

// EnsureMetadataProfile upserts a Lidarr metadata profile into the profiles
// section's metadata_profiles array (matched by name, like the music
// profiles).
func (t *LidarrPostgresTarget) EnsureMetadataProfile(ctx context.Context, p LidarrMetadataProfile) (int64, TargetAction, error) {
	doc, err := t.profilesDoc(ctx)
	if err != nil {
		return 0, "", err
	}
	for i := range doc.MetadataProfiles {
		if doc.MetadataProfiles[i].Name == p.Name {
			return int64(doc.MetadataProfiles[i].ID), ActionExists, nil
		}
	}
	id := nextMetadataProfileID(doc)
	items := p.Items
	if items == nil {
		items = []string{}
	}
	doc.MetadataProfiles = append(doc.MetadataProfiles, settings.MetadataProfile{
		ID: id, Name: p.Name, Items: items,
	})
	if err := t.putProfiles(ctx, doc); err != nil {
		return 0, "", err
	}
	return int64(id), ActionCreated, nil
}

// ---------------------------------------------------------------------------
// Tags.
// ---------------------------------------------------------------------------

// EnsureTag upserts a Lidarr tag into the tags section, preserving its id so
// artist tag references resolve back to the imported tag.
func (t *LidarrPostgresTarget) EnsureTag(ctx context.Context, tag LidarrTag) (int64, TargetAction, error) {
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

func (t *LidarrPostgresTarget) tagsDoc(ctx context.Context) (*settings.TagsSettings, error) {
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

func (t *LidarrPostgresTarget) putTags(ctx context.Context, doc *settings.TagsSettings) error {
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return t.Settings.PutSectionDocument(ctx, settings.SectionTags, raw)
}

// TagLabels resolves a set of Lidarr tag ids to their labels (the
// LidarrTagStore capability the migrator uses for artist tag references).
func (t *LidarrPostgresTarget) TagLabels(ctx context.Context, ids []int) ([]string, error) {
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

// ---------------------------------------------------------------------------
// Root folders / indexers / download client / media management.
// ---------------------------------------------------------------------------

// EnsureRootFolder upserts a root folder by (clean) path. An existing folder
// is returned ActionExists (its per-root default profiles are left as-is); a
// new one is registered with the derived default profiles.
func (t *LidarrPostgresTarget) EnsureRootFolder(ctx context.Context, r LidarrRootFolder, defaultQuality, defaultMetadata string) (int64, TargetAction, error) {
	if t.RootFolders == nil {
		return 0, "", fmt.Errorf("migration: root folder store not wired")
	}
	path := filepath.Clean(r.Path)
	if path == "" {
		return 0, "", fmt.Errorf("migration: root folder has no path")
	}
	if id, found := t.RootFolders.FindRootFolder(ctx, path); found {
		return id, ActionExists, nil
	}
	name := filepath.Base(path)
	id, err := t.RootFolders.CreateRootFolder(ctx, path, name, defaultQuality, defaultMetadata)
	if err != nil {
		return 0, "", err
	}
	return id, ActionCreated, nil
}

// EnsureIndexer upserts a Lidarr indexer into the indexer definition store.
// The per-indexer booleans map onto the R5a indexer settings: enableRss ->
// rss, enableAutomatic -> auto_search, enableInteractive -> interactive_search.
func (t *LidarrPostgresTarget) EnsureIndexer(ctx context.Context, ix LidarrIndexer) (int64, TargetAction, error) {
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

// SetDownloadClient maps a Lidarr download client onto the Mediarr
// download-client config (best-effort: qBittorrent and SABnzbd field names,
// the same mapping as the Sonarr and Radarr runs).
func (t *LidarrPostgresTarget) SetDownloadClient(ctx context.Context, c LidarrDownloadClient) (TargetAction, error) {
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
	}
	if !c.Enable {
		// A disabled client is stored as the mock kind (Mediarr has no
		// per-client enable flag); the mapping report records the intent.
		dc.Kind = "mock"
	}
	if existing != nil && *existing == *dc {
		// Re-applying the identical client is a no-op (idempotent).
		return ActionExists, nil
	}
	if err := t.Settings.PutDownloadClient(ctx, dc); err != nil {
		return "", err
	}
	if prevSet {
		return ActionUpdated, nil
	}
	return ActionCreated, nil
}

// ApplyMediaManagement maps the Lidarr media-management config onto the
// Mediarr media-management document (the music naming block + the file
// management fields the document can hold) and returns the number of fields
// written. An empty document starts from the defaults.
func (t *LidarrPostgresTarget) ApplyMediaManagement(ctx context.Context, mm LidarrMediaManagement) (int, error) {
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
	if doc.MusicNaming.RenameTracks != mm.MusicNaming.RenameTracks {
		doc.MusicNaming.RenameTracks = mm.MusicNaming.RenameTracks
		applied++
	}
	if doc.MusicNaming.ReplaceIllegalCharacters != mm.MusicNaming.ReplaceIllegalCharacters {
		doc.MusicNaming.ReplaceIllegalCharacters = mm.MusicNaming.ReplaceIllegalCharacters
		applied++
	}
	if mm.MusicNaming.ColonReplacement != "" && doc.MusicNaming.ColonReplacement != mm.MusicNaming.ColonReplacement {
		doc.MusicNaming.ColonReplacement = mm.MusicNaming.ColonReplacement
		applied++
	}
	if mm.MusicNaming.StandardFormat != "" && doc.MusicNaming.StandardFormat != mm.MusicNaming.StandardFormat {
		doc.MusicNaming.StandardFormat = mm.MusicNaming.StandardFormat
		applied++
	}
	if mm.MusicNaming.MultiDiscFormat != "" && doc.MusicNaming.MultiDiscFormat != mm.MusicNaming.MultiDiscFormat {
		doc.MusicNaming.MultiDiscFormat = mm.MusicNaming.MultiDiscFormat
		applied++
	}
	if doc.Management.ImportExtraFiles != mm.FileManagement.ImportExtraFiles {
		doc.Management.ImportExtraFiles = mm.FileManagement.ImportExtraFiles
		applied++
	}
	if doc.Management.UnmonitorDeletedMovies != mm.FileManagement.UnmonitorDeletedImportedFiles {
		doc.Management.UnmonitorDeletedMovies = mm.FileManagement.UnmonitorDeletedImportedFiles
		applied++
	}
	// Lidarr stores the min file age in minutes; the Mediarr document in
	// whole hours — only map a clean conversion.
	if mm.FileManagement.MinFileAge > 0 && mm.FileManagement.MinFileAge%60 == 0 {
		if h := mm.FileManagement.MinFileAge / 60; doc.Management.MinFileAgeHours != h {
			doc.Management.MinFileAgeHours = h
			applied++
		}
	}
	if applied > 0 {
		out, err := json.Marshal(doc)
		if err != nil {
			return 0, err
		}
		if err := t.Settings.PutSectionDocument(ctx, settings.SectionMediaManagement, out); err != nil {
			return 0, err
		}
	}
	return applied, nil
}

// ---------------------------------------------------------------------------
// Music domain (artists + albums + tracks).
// ---------------------------------------------------------------------------

// EnsureArtist imports a Lidarr artist into the music domain. It is
// idempotent: an existing artist (matched by name, the domain's identity) is
// returned with ActionExists. An empty quality-profile name resolves to the
// factory default music profile.
func (t *LidarrPostgresTarget) EnsureArtist(ctx context.Context, ref LidarrArtistRef) (int64, TargetAction, error) {
	if t.Music == nil {
		return 0, "", fmt.Errorf("migration: music store not wired")
	}
	name := strings.TrimSpace(ref.Name)
	if name == "" {
		return 0, "", fmt.Errorf("migration: artist has no name")
	}
	if id, found := t.Music.FindArtist(ctx, name); found {
		return id, ActionExists, nil
	}
	if ref.QualityProfile == "" {
		ref.QualityProfile = settings.DefaultMusicQualityProfile
	}
	id, err := t.Music.AddArtist(ctx, ref)
	if err != nil {
		return 0, "", err
	}
	return id, ActionCreated, nil
}

// EnsureAlbums upserts an artist's albums (title+year identity) and their
// tracks (disc+number identity), returning the per-entity ensured state. An
// existing album/track is reported ActionExists and is not duplicated.
func (t *LidarrPostgresTarget) EnsureAlbums(ctx context.Context, artistID int64, albums []LidarrAlbumRef) ([]LidarrAlbumResult, error) {
	if t.Music == nil {
		return nil, fmt.Errorf("migration: music store not wired")
	}
	results := make([]LidarrAlbumResult, 0, len(albums))
	for _, a := range albums {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			continue
		}
		res := LidarrAlbumResult{Album: a}
		albumID, found := t.Music.FindAlbum(ctx, artistID, name, a.Year)
		if !found {
			newID, err := t.Music.AddAlbum(ctx, artistID, a)
			if err != nil {
				return nil, err
			}
			albumID = newID
			res.Action = ActionCreated
		} else {
			res.Action = ActionExists
		}
		// Tracks (disc+number identity).
		if len(a.Tracks) > 0 {
			keys, err := t.Music.TrackKeys(ctx, albumID)
			if err != nil {
				return nil, err
			}
			for _, tr := range a.Tracks {
				tres := LidarrTrackResult{Disc: tr.Disc, Number: tr.Number}
				key := fmt.Sprintf("%d-%d", tr.Disc, tr.Number)
				if !keys[key] {
					if _, err := t.Music.AddTrack(ctx, albumID, tr); err != nil {
						return nil, err
					}
					tres.Action = ActionCreated
				} else {
					tres.Action = ActionExists
				}
				res.Tracks = append(res.Tracks, tres)
			}
		}
		results = append(results, res)
	}
	return results, nil
}

var _ LidarrMigrationTarget = (*LidarrPostgresTarget)(nil)
var _ LidarrTagStore = (*LidarrPostgresTarget)(nil)
