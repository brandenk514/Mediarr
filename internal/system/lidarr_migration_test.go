package system

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---- mocked Lidarr v3 API (the swappable source seam) ----

type fakeLidarr struct {
	artists      []LidarrArtist
	albums       []LidarrAlbum
	profiles     []LidarrQualityProfile
	defs         []LidarrQualityDefinition
	metaProfiles []LidarrMetadataProfile
	tags         []LidarrTag
	indexers     []LidarrIndexer
	dcs          []LidarrDownloadClient
	roots        []LidarrRootFolder
}

func (f *fakeLidarr) GetArtists(context.Context) ([]LidarrArtist, error) { return f.artists, nil }
func (f *fakeLidarr) GetAlbums(context.Context) ([]LidarrAlbum, error)   { return f.albums, nil }
func (f *fakeLidarr) GetQualityProfiles(context.Context) ([]LidarrQualityProfile, error) {
	return f.profiles, nil
}
func (f *fakeLidarr) GetQualityDefinitions(context.Context) ([]LidarrQualityDefinition, error) {
	return f.defs, nil
}
func (f *fakeLidarr) GetMetadataProfiles(context.Context) ([]LidarrMetadataProfile, error) {
	return f.metaProfiles, nil
}
func (f *fakeLidarr) GetTags(context.Context) ([]LidarrTag, error)         { return f.tags, nil }
func (f *fakeLidarr) GetIndexers(context.Context) ([]LidarrIndexer, error) { return f.indexers, nil }
func (f *fakeLidarr) GetDownloadClients(context.Context) ([]LidarrDownloadClient, error) {
	return f.dcs, nil
}
func (f *fakeLidarr) GetRootFolders(context.Context) ([]LidarrRootFolder, error) {
	return f.roots, nil
}
func (f *fakeLidarr) GetMediaManagement(context.Context) (*LidarrMediaManagement, error) {
	return &LidarrMediaManagement{}, nil
}
func (f *fakeLidarr) GetImportExport(context.Context) (*LidarrImportExport, error) {
	return nil, nil
}

func sampleLidarr() *fakeLidarr {
	return &fakeLidarr{
		profiles: []LidarrQualityProfile{
			{ID: 1, Name: "Lossless", Cutoff: 3, Items: []LidarrProfileItem{
				{Quality: LidarrQuality{ID: 3, Name: "FLAC"}, Approved: true},
				{Quality: LidarrQuality{ID: 4, Name: "ALAC"}, Approved: true},
			}},
			{ID: 2, Name: "320", Cutoff: 9, Items: []LidarrProfileItem{
				{Quality: LidarrQuality{ID: 9, Name: "MP3 320"}, Approved: true},
			}},
		},
		defs: []LidarrQualityDefinition{
			{ID: 3, Name: "FLAC", Source: "Digital", Resolution: "Digital", SizeLimit: 0},
		},
		metaProfiles: []LidarrMetadataProfile{{ID: 1, Name: "Standard", Items: []string{"cover"}}},
		tags:         []LidarrTag{{ID: 1, Label: "classics"}, {ID: 2, Label: "live"}},
		indexers: []LidarrIndexer{
			{ID: 1, Name: "LidarrRar", Enable: true, EnableRss: true, EnableAutomatic: false, EnableInteractive: true, IndexerType: "indexer", BaseURL: "https://lidarr-rar.example"},
		},
		dcs:   []LidarrDownloadClient{{ID: 1, Name: "qbit", Enable: true, ClientKey: "qBittorrent"}},
		roots: []LidarrRootFolder{{Path: "/media/music", FreeSpace: 1 << 40}},
		artists: []LidarrArtist{
			{
				ID: 11, ArtistID: "mb-11", Name: "Radiohead", Status: "continuing", Monitored: true,
				MonitorNewItems: true, QualityProfileID: 1, MetadataProfileID: 1,
				RootFolderPath: "/media/music", Path: "Radiohead",
				Genres: []string{"Alternative"}, Tags: []int{1}, Rating: 8,
			},
			{
				ID: 12, ArtistID: "mb-12", Name: "Daft Punk", Status: "ended", Monitored: false,
				RootFolderPath: "/media/music", Path: "Daft Punk", QualityProfileID: 2,
			},
		},
		albums: []LidarrAlbum{
			{
				ID: 101, ArtistID: 11, Title: "OK Computer", Year: 1997, Monitored: true,
				QualityProfileID: 1, MetadataProfileID: 1, Size: 600 << 20,
				Tracks: []LidarrTrack{
					{ID: 5001, AlbumID: 101, TrackNumber: 1, DiscNumber: 1, Title: "Airbag", Duration: 243, Monitored: true, HasFile: true, Size: 20 << 20, Quality: LidarrTrackQuality{Quality: LidarrQuality{ID: 3, Name: "FLAC"}}},
					{ID: 5002, AlbumID: 101, TrackNumber: 2, DiscNumber: 1, Title: "How to Disappear", Duration: 196, Monitored: true, HasFile: false, Size: 0},
				},
			},
			{
				ID: 102, ArtistID: 12, Title: "Discovery", Year: 2001, Monitored: false, QualityProfileID: 2,
				Tracks: []LidarrTrack{
					{ID: 5003, AlbumID: 102, TrackNumber: 1, DiscNumber: 1, Title: "Veridis Quo", Duration: 222, Monitored: false, HasFile: false, Size: 0},
				},
			},
		},
	}
}

// ---- fake Mediarr music import target ----

type fakeLidarrTarget struct {
	musicProfiles   map[string]int64
	createdProfiles map[string]bool
	metaProfiles    map[string]int64
	levelsWritten   int
	levels          map[int]bool // quality level ids already written (idempotency)
	tags            map[int]int64
	indexersByName  map[string]int64
	indexers        map[string]LidarrIndexer
	dcSet           bool
	dc              LidarrDownloadClient
	mediaMgmt       int
	mmApplied       bool // media management already applied (idempotency)
	roots           map[string]int64
	artists         map[string]int64           // name -> id
	albums          map[int64]map[string]int64 // artistID -> (title|year -> id)
	tracks          map[int64]map[string]int64 // albumID -> (disc-number -> id)
	failArtists     int                        // if >0, EnsureArtist fails once this many artists exist
	nextID          int64
}

func newFakeLidarrTarget() *fakeLidarrTarget {
	return &fakeLidarrTarget{
		musicProfiles:   map[string]int64{},
		createdProfiles: map[string]bool{},
		metaProfiles:    map[string]int64{},
		levels:          map[int]bool{},
		tags:            map[int]int64{},
		indexersByName:  map[string]int64{},
		indexers:        map[string]LidarrIndexer{},
		roots:           map[string]int64{},
		artists:         map[string]int64{},
		albums:          map[int64]map[string]int64{},
		tracks:          map[int64]map[string]int64{},
		nextID:          1,
	}
}

func (f *fakeLidarrTarget) next() int64 { id := f.nextID; f.nextID++; return id }

func (f *fakeLidarrTarget) EnsureMusicProfile(ctx context.Context, p LidarrQualityProfile) (int64, TargetAction, error) {
	if id, ok := f.musicProfiles[p.Name]; ok {
		return id, ActionExists, nil
	}
	id := f.next()
	f.musicProfiles[p.Name] = id
	f.createdProfiles[p.Name] = true
	return id, ActionCreated, nil
}
func (f *fakeLidarrTarget) ApplyQualityDefinitions(ctx context.Context, defs []LidarrQualityDefinition) (int, error) {
	written := 0
	for _, def := range defs {
		if !f.levels[def.ID] {
			f.levels[def.ID] = true
			written++
		}
	}
	f.levelsWritten = written
	return written, nil
}
func (f *fakeLidarrTarget) EnsureMetadataProfile(ctx context.Context, p LidarrMetadataProfile) (int64, TargetAction, error) {
	if id, ok := f.metaProfiles[p.Name]; ok {
		return id, ActionExists, nil
	}
	id := f.next()
	f.metaProfiles[p.Name] = id
	return id, ActionCreated, nil
}
func (f *fakeLidarrTarget) EnsureTag(ctx context.Context, t LidarrTag) (int64, TargetAction, error) {
	if id, ok := f.tags[t.ID]; ok {
		return id, ActionExists, nil
	}
	id := f.next()
	f.tags[t.ID] = id
	return id, ActionCreated, nil
}
func (f *fakeLidarrTarget) EnsureRootFolder(ctx context.Context, r LidarrRootFolder, dq, dm string) (int64, TargetAction, error) {
	if id, ok := f.roots[r.Path]; ok {
		return id, ActionExists, nil
	}
	id := f.next()
	f.roots[r.Path] = id
	return id, ActionCreated, nil
}
func (f *fakeLidarrTarget) EnsureIndexer(ctx context.Context, ix LidarrIndexer) (int64, TargetAction, error) {
	if id, ok := f.indexersByName[ix.Name]; ok {
		return id, ActionExists, nil
	}
	id := f.next()
	f.indexersByName[ix.Name] = id
	f.indexers[ix.Name] = ix
	return id, ActionCreated, nil
}
func (f *fakeLidarrTarget) SetDownloadClient(ctx context.Context, c LidarrDownloadClient) (TargetAction, error) {
	if f.dcSet && f.dc.ID == c.ID && f.dc.Name == c.Name && f.dc.ClientKey == c.ClientKey && f.dc.Enable == c.Enable {
		return ActionExists, nil // re-applying the identical client is a no-op
	}
	prevSet := f.dcSet
	f.dcSet = true
	f.dc = c
	if prevSet {
		return ActionUpdated, nil
	}
	return ActionCreated, nil
}
func (f *fakeLidarrTarget) ApplyMediaManagement(ctx context.Context, mm LidarrMediaManagement) (int, error) {
	if f.mmApplied {
		f.mediaMgmt = 0
		return 0, nil // no change on re-apply (idempotent)
	}
	f.mmApplied = true
	f.mediaMgmt = 6
	return 6, nil
}
func (f *fakeLidarrTarget) TagLabels(ctx context.Context, ids []int) ([]string, error) {
	out := make([]string, 0, len(ids))
	for range ids {
		out = append(out, "label")
	}
	return out, nil
}
func (f *fakeLidarrTarget) EnsureArtist(ctx context.Context, ref LidarrArtistRef) (int64, TargetAction, error) {
	if id, ok := f.artists[ref.Name]; ok {
		return id, ActionExists, nil
	}
	if f.failArtists > 0 && len(f.artists) >= f.failArtists {
		return 0, "", fmt.Errorf("target: simulated failure on artist %q", ref.Name)
	}
	id := f.next()
	f.artists[ref.Name] = id
	f.albums[id] = map[string]int64{}
	return id, ActionCreated, nil
}
func (f *fakeLidarrTarget) EnsureAlbums(ctx context.Context, artistID int64, albums []LidarrAlbumRef) ([]LidarrAlbumResult, error) {
	byArtist := f.albums[artistID]
	if byArtist == nil {
		byArtist = map[string]int64{}
		f.albums[artistID] = byArtist
	}
	results := make([]LidarrAlbumResult, 0, len(albums))
	for _, a := range albums {
		key := fmt.Sprintf("%s|%d", a.Name, a.Year)
		res := LidarrAlbumResult{Album: a}
		albumID, ok := byArtist[key]
		if !ok {
			albumID = f.next()
			byArtist[key] = albumID
			f.tracks[albumID] = map[string]int64{}
			res.Action = ActionCreated
		} else {
			res.Action = ActionExists
		}
		tk := f.tracks[albumID]
		for _, tr := range a.Tracks {
			tres := LidarrTrackResult{Disc: tr.Disc, Number: tr.Number}
			tkey := fmt.Sprintf("%d-%d", tr.Disc, tr.Number)
			if _, ok := tk[tkey]; !ok {
				tk[tkey] = f.next()
				tres.Action = ActionCreated
			} else {
				tres.Action = ActionExists
			}
			res.Tracks = append(res.Tracks, tres)
		}
		results = append(results, res)
	}
	return results, nil
}

var _ LidarrMigrationTarget = (*fakeLidarrTarget)(nil)
var _ LidarrTagStore = (*fakeLidarrTarget)(nil)

// ---- W1 tests: mapping report + dry-run (no writes) ----

func TestLidarrMigrationDryRunReportsWithoutWriting(t *testing.T) {
	src := sampleLidarr()
	target := newFakeLidarrTarget()
	m := &LidarrMigrator{NewSource: func(b, k string) LidarrClient { return src }, Target: target}

	res, err := m.Run(context.Background(), MigrationRequest{SourceURL: "https://lidarr.local:8281", DryRun: true, Source: SourceLidarr})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	// Nothing written to the target.
	if len(target.artists) != 0 || len(target.musicProfiles) != 0 || len(target.tags) != 0 ||
		len(target.roots) != 0 || len(target.indexersByName) != 0 {
		t.Fatalf("dry-run must not write to the target: artists=%d profiles=%d tags=%d roots=%d indexers=%d",
			len(target.artists), len(target.musicProfiles), len(target.tags), len(target.roots), len(target.indexersByName))
	}
	if len(res.Items) == 0 {
		t.Fatal("expected mapping items")
	}
	entities := map[string]bool{}
	for _, it := range res.Items {
		if it.Action != "dry-run" {
			t.Errorf("item %s/%s action = %q, want dry-run", it.Entity, it.ExternalID, it.Action)
		}
		entities[it.Entity] = true
	}
	for _, want := range []string{
		"quality_profile", "quality_definition", "metadata_profile", "tag", "indexer",
		"download_client", "settings", "root_folder", "artist", "album", "track",
	} {
		if !entities[want] {
			t.Errorf("mapping report missing entity class %q", want)
		}
	}
	// 2 profiles + 1 def + 1 meta + 2 tags + 1 indexer + 1 dc + 1 settings +
	// 1 root + 2 artists + 2 albums + 3 tracks = 17.
	if res.Counts.Total != 17 {
		t.Errorf("total = %d, want 17", res.Counts.Total)
	}
	if !res.DryRun {
		t.Error("expected dry_run=true")
	}
}

func TestLidarrMigrationHappyPath(t *testing.T) {
	src := sampleLidarr()
	target := newFakeLidarrTarget()
	m := &LidarrMigrator{NewSource: func(b, k string) LidarrClient { return src }, Target: target}

	res, err := m.Run(context.Background(), MigrationRequest{SourceURL: "https://lidarr.local:8281", Source: SourceLidarr})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// Music quality profiles: both source profiles created in the target.
	if len(target.musicProfiles) != 2 {
		t.Errorf("music profiles = %d, want 2", len(target.musicProfiles))
	}
	// Quality definitions reconciled onto the seeded ladder.
	if target.levelsWritten != 1 {
		t.Errorf("quality definitions = %d, want 1", target.levelsWritten)
	}
	// Metadata profiles: the seeded one imported.
	if len(target.metaProfiles) != 1 {
		t.Errorf("metadata profiles = %d, want 1", len(target.metaProfiles))
	}
	// Tags: both source tags imported (source ids preserved).
	if len(target.tags) != 2 {
		t.Errorf("tags = %d, want 2", len(target.tags))
	}
	// Root folder: the single source root imported with a derived default
	// profile (Radiohead is the first artist under it, Lossless / Standard).
	if id, ok := target.roots["/media/music"]; !ok || id == 0 {
		t.Errorf("root folder not imported: %v", target.roots)
	}
	// Indexers: the per-indexer toggles carried through to the target.
	ix, ok := target.indexers["LidarrRar"]
	if !ok {
		t.Fatal("indexer LidarrRar not imported")
	}
	if !ix.Enable || !ix.EnableRss || ix.EnableAutomatic || !ix.EnableInteractive {
		t.Errorf("indexer toggles = %+v, want enable/rss on, automatic off, interactive on", ix)
	}
	// Download client: mapped (qBittorrent).
	if !target.dcSet || target.dc.ClientKey != "qBittorrent" {
		t.Errorf("download client = %+v, want qBittorrent", target.dc)
	}
	// Settings: best-effort subset applied + noted in the report.
	if target.mediaMgmt != 6 {
		t.Errorf("media management fields = %d, want 6", target.mediaMgmt)
	}
	var settingsNote string
	for _, it := range res.Items {
		if it.Entity == "settings" {
			settingsNote = it.Message
		}
	}
	if !strings.Contains(settingsNote, "media management: 6 fields mapped") {
		t.Errorf("settings note = %q, want the media-management mapping note", settingsNote)
	}
	if !strings.Contains(settingsNote, "importexport: not configured") {
		t.Errorf("settings note = %q, want the importexport note", settingsNote)
	}
	// Artists: both created (name identity).
	if len(target.artists) != 2 {
		t.Fatalf("artists = %d, want 2: %v", len(target.artists), target.artists)
	}
	// Albums: Radiohead has one (OK Computer), Daft Punk one (Discovery).
	var radioheadID int64
	for name, id := range target.artists {
		if name == "Radiohead" {
			radioheadID = id
		}
	}
	if len(target.albums[radioheadID]) != 1 {
		t.Errorf("radiohead albums = %d, want 1", len(target.albums[radioheadID]))
	}
	// Tracks: OK Computer has two.
	albumKeys := map[string]int64{}
	for k, id := range target.albums[radioheadID] {
		albumKeys[k] = id
	}
	var okAlbumID int64
	for _, id := range albumKeys {
		okAlbumID = id
	}
	if len(target.tracks[okAlbumID]) != 2 {
		t.Errorf("ok computer tracks = %d, want 2", len(target.tracks[okAlbumID]))
	}
	// Counts: 17 total; the settings apply + quality-definition reconcile are
	// "existing" (updated), everything else created on the first run.
	if res.Counts.Total != 17 {
		t.Errorf("total = %d, want 17", res.Counts.Total)
	}
	if res.Counts.Created != 15 {
		t.Errorf("created = %d, want 15", res.Counts.Created)
	}
	if res.Counts.Existing != 2 {
		t.Errorf("existing = %d, want 2 (settings + quality definition)", res.Counts.Existing)
	}
	// Status reflects the run.
	if st := m.Status(); st.Status != "completed" || st.Total != 2 || st.Done != 2 {
		t.Errorf("status = %+v, want completed 2/2", st)
	}
}

// ---- W2 tests: idempotent + resumable ----

func TestLidarrMigrationIdempotentNoDupes(t *testing.T) {
	src := sampleLidarr()
	target := newFakeLidarrTarget()
	m := &LidarrMigrator{NewSource: func(b, k string) LidarrClient { return src }, Target: target}
	req := MigrationRequest{SourceURL: "https://lidarr.local:8281", Source: SourceLidarr}

	// First run: everything created (plus the settings/quality-def apply).
	res1, err := m.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if res1.Counts.Created == 0 {
		t.Fatal("expected created > 0 on first run")
	}
	artistCount1, albumCount1 := len(target.artists), len(target.musicProfiles)

	// Second run (same source, same target): everything exists, nothing new.
	res2, err := m.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if res2.Counts.Created != 0 {
		t.Errorf("second run created = %d, want 0 (idempotent)", res2.Counts.Created)
	}
	if len(target.artists) != artistCount1 {
		t.Errorf("artist count changed: %d -> %d (dupes)", artistCount1, len(target.artists))
	}
	if len(target.musicProfiles) != albumCount1 {
		t.Errorf("profile count changed: %d -> %d (dupes)", albumCount1, len(target.musicProfiles))
	}
	// Spot-check: every entity item is "exists" on the second run (never a
	// duplicate), and albums/tracks report "exists" too.
	for _, it := range res2.Items {
		if it.Action != "exists" {
			t.Errorf("second run item %s/%s action = %q, want exists", it.Entity, it.ExternalID, it.Action)
		}
	}
}

func TestLidarrMigrationResumablePartialThenResume(t *testing.T) {
	src := sampleLidarr()
	target := newFakeLidarrTarget()
	target.failArtists = 1 // fail after the first artist is created

	m := &LidarrMigrator{NewSource: func(b, k string) LidarrClient { return src }, Target: target}
	req := MigrationRequest{SourceURL: "https://lidarr.local:8281", Source: SourceLidarr}

	// First run: partial (artist 1 + album created, artist 2 fails).
	res1, err := m.Run(context.Background(), req)
	if err == nil {
		t.Fatal("expected an error from the partial run")
	}
	if res1 == nil || !res1.Resumable {
		t.Fatalf("expected a resumable partial result, got %+v", res1)
	}
	if st := m.Status(); st.Status != "partial" {
		t.Errorf("status = %q, want partial", st.Status)
	}
	if len(target.artists) != 1 {
		t.Fatalf("expected 1 artist created before failure, got %d", len(target.artists))
	}

	// Clear the failure and resume: artist 1 is skipped, artist 2 is created.
	target.failArtists = 0
	res2, err := m.Run(context.Background(), MigrationRequest{SourceURL: "https://lidarr.local:8281", Source: SourceLidarr, Resume: true})
	if err != nil {
		t.Fatalf("resume run: %v", err)
	}
	if res2 == nil {
		t.Fatal("resume returned no result")
	}
	skipped, created := 0, 0
	for _, it := range res2.Items {
		if it.Entity != "artist" {
			continue
		}
		switch it.Action {
		case "skipped":
			skipped++
		case "created":
			created++
		}
	}
	if skipped != 1 || created != 1 {
		t.Errorf("resume: skipped=%d created=%d, want 1/1", skipped, created)
	}
	if len(target.artists) != 2 {
		t.Errorf("after resume, artists = %d, want 2", len(target.artists))
	}
	if m.Status().Status != "completed" {
		t.Errorf("final status = %q, want completed", m.Status().Status)
	}
}

func TestLidarrMigrationSourceFailure(t *testing.T) {
	// A source 404/500 surfaces as a run error carrying the reason (the API
	// maps it to a 502 with the source name).
	for status, body := range map[int]string{
		http.StatusNotFound:            `{"error":"not found"}`,
		http.StatusInternalServerError: `{"error":"boom"}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		m := NewLidarrMigrator(newFakeLidarrTarget()) // default HTTP source factory
		_, err := m.Run(context.Background(), MigrationRequest{SourceURL: srv.URL, Source: SourceLidarr})
		srv.Close()
		if err == nil {
			t.Fatalf("status %d: expected an error", status)
		}
		if !strings.Contains(err.Error(), fmt.Sprintf("returned %d", status)) {
			t.Errorf("status %d: error %q should carry the HTTP reason", status, err.Error())
		}
	}
}

func TestLidarrMigrationRequiresValidSourceURL(t *testing.T) {
	m := &LidarrMigrator{NewSource: func(b, k string) LidarrClient { return &fakeLidarr{} }, Target: newFakeLidarrTarget()}
	if _, err := m.Run(context.Background(), MigrationRequest{SourceURL: ""}); err == nil {
		t.Error("expected error for empty source_url")
	}
	if _, err := m.Run(context.Background(), MigrationRequest{SourceURL: "not a url"}); err == nil {
		t.Error("expected error for non-http source_url")
	}
}
