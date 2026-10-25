package system

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/settings"
)

// ---- fake seams for the Postgres target ----

type fakeSettings struct {
	dc       *settings.DownloadClient
	sections map[settings.Section][]byte
	dcPut    int
}

func (f *fakeSettings) GetDownloadClient(context.Context) (*settings.DownloadClient, error) {
	return f.dc, nil
}
func (f *fakeSettings) PutDownloadClient(_ context.Context, dc *settings.DownloadClient) error {
	f.dc = dc
	f.dcPut++
	return nil
}
func (f *fakeSettings) GetSectionDocument(_ context.Context, s settings.Section) ([]byte, error) {
	if b, ok := f.sections[s]; ok {
		return b, nil
	}
	return nil, nil
}
func (f *fakeSettings) PutSectionDocument(_ context.Context, s settings.Section, doc []byte) error {
	if f.sections == nil {
		f.sections = map[settings.Section][]byte{}
	}
	f.sections[s] = doc
	return nil
}

type fakeIndexers struct {
	byName map[string]int64
	next   int64
}

func (f *fakeIndexers) FindIndexer(_ context.Context, name string) (int64, bool) {
	id, ok := f.byName[name]
	return id, ok
}
func (f *fakeIndexers) CreateIndexer(_ context.Context, name, kind, baseURL, apiKey string, enabled bool) (int64, error) {
	f.next++
	f.byName[name] = f.next
	return f.next, nil
}

type fakeTV struct {
	series   []SeriesRef
	byTVDB   map[int]struct{}
	episodes map[int64]map[string]bool
	nextID   int64
}

func newFakeTV() *fakeTV {
	return &fakeTV{episodes: map[int64]map[string]bool{}, byTVDB: map[int]struct{}{}, nextID: 1}
}
func (f *fakeTV) FindSeries(_ context.Context, tvdbID int, title string, year int) (int64, bool) {
	if tvdbID != 0 {
		if _, ok := f.byTVDB[tvdbID]; ok {
			for i := range f.series {
				if f.series[i].TVDBID == tvdbID {
					return f.nextID, true
				}
			}
		}
		return 0, false
	}
	return 0, false
}
func (f *fakeTV) AddSeries(_ context.Context, ref SeriesRef) (int64, error) {
	f.nextID++
	id := f.nextID
	f.series = append(f.series, ref)
	if ref.TVDBID != 0 {
		f.byTVDB[ref.TVDBID] = struct{}{}
	}
	f.episodes[id] = map[string]bool{}
	return id, nil
}
func (f *fakeTV) EpisodeKeys(_ context.Context, seriesID int64) (map[string]bool, error) {
	return f.episodes[seriesID], nil
}
func (f *fakeTV) AddEpisode(_ context.Context, seriesID int64, season, episode int, title string, airDate *time.Time, monitored bool) (int64, error) {
	m := f.episodes[seriesID]
	if m == nil {
		m = map[string]bool{}
		f.episodes[seriesID] = m
	}
	m[fmt.Sprintf("S%dxE%d", season, episode)] = true
	return 0, nil
}

// ---- tests ----

func TestPostgresTargetQualityProfileIdempotent(t *testing.T) {
	st := &fakeSettings{}
	tgt := NewPostgresTarget(st, &fakeIndexers{}, newFakeTV())
	p := SonarrQualityProfile{ID: 7, Name: "Default", Cutoff: 6, Items: []SonarrProfileItem{{Quality: SonarrQuality{ID: 6, Name: "1080p"}, Approved: true}}}

	id1, act1, err := tgt.EnsureQualityProfile(context.Background(), p)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if act1 != ActionCreated || id1 != 7 {
		t.Fatalf("first = (%d,%q), want (7,created)", id1, act1)
	}
	id2, act2, err := tgt.EnsureQualityProfile(context.Background(), p)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if act2 != ActionExists || id2 != 7 {
		t.Fatalf("second = (%d,%q), want (7,exists)", id2, act2)
	}
	var doc settings.ProfilesSettings
	if err := json.Unmarshal(st.sections[settings.SectionProfiles], &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	count := 0
	for _, p := range doc.Profiles {
		if p.ID == 7 {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("profiles with id 7 = %d, want 1 (no dupes)", count)
	}
}

func TestPostgresTargetSeriesResolvesProfileAndTags(t *testing.T) {
	st := &fakeSettings{}
	tgt := NewPostgresTarget(st, &fakeIndexers{}, newFakeTV())
	ctx := context.Background()

	// Seed a profile (id 1 -> "Default") and two tags (1 -> "4k", 2 -> "kids").
	if _, _, err := tgt.EnsureQualityProfile(ctx, SonarrQualityProfile{ID: 1, Name: "Default"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := tgt.EnsureTag(ctx, SonarrTag{ID: 1, Label: "4k"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := tgt.EnsureTag(ctx, SonarrTag{ID: 2, Label: "kids"}); err != nil {
		t.Fatal(err)
	}

	s := SonarrSeries{ID: 10, SeriesID: 100, Title: "Show A", Year: 2020, QualityProfileID: 1, Monitored: true, Tags: []int{1, 2}}
	id, act, err := tgt.EnsureSeries(ctx, s)
	if err != nil {
		t.Fatalf("EnsureSeries: %v", err)
	}
	if act != ActionCreated || id == 0 {
		t.Fatalf("EnsureSeries = (%d,%q), want created/id>0", id, act)
	}
	// Re-run: idempotent.
	if _, act2, err := tgt.EnsureSeries(ctx, s); err != nil || act2 != ActionExists {
		t.Fatalf("second EnsureSeries = (%q,%v), want exists", act2, err)
	}
}

func TestPostgresTargetEpisodesIdempotent(t *testing.T) {
	st := &fakeSettings{}
	tv := newFakeTV()
	tgt := NewPostgresTarget(st, &fakeIndexers{}, tv)
	ctx := context.Background()

	seed, _, _ := tgt.EnsureSeries(ctx, SonarrSeries{ID: 1, SeriesID: 100, Title: "Show A", Year: 2020})
	eps := []SonarrEpisode{
		{ID: 1001, SeriesID: 100, SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot", Monitored: true},
		{ID: 1002, SeriesID: 100, SeasonNumber: 1, EpisodeNumber: 2, Title: "Two", Monitored: true},
	}
	n1, err := tgt.EnsureEpisodes(ctx, seed, eps)
	if err != nil {
		t.Fatalf("first EnsureEpisodes: %v", err)
	}
	if n1 != 2 {
		t.Fatalf("first created = %d, want 2", n1)
	}
	n2, err := tgt.EnsureEpisodes(ctx, seed, eps)
	if err != nil {
		t.Fatalf("second EnsureEpisodes: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("second created = %d, want 0 (idempotent)", n2)
	}
}

func TestPostgresTargetIndexerIdempotent(t *testing.T) {
	ix := &fakeIndexers{byName: map[string]int64{}}
	tgt := NewPostgresTarget(&fakeSettings{}, ix, newFakeTV())
	in := SonarrIndexer{ID: 1, Name: "RAR", Enable: true, IndexerType: "indexer", APIKey: "k", BaseURL: "https://rar.example"}
	id1, act1, err := tgt.EnsureIndexer(context.Background(), in)
	if err != nil || act1 != ActionCreated || id1 != 1 {
		t.Fatalf("first = (%d,%q,%v), want (1,created,nil)", id1, act1, err)
	}
	id2, act2, err := tgt.EnsureIndexer(context.Background(), in)
	if err != nil || act2 != ActionExists {
		t.Fatalf("second = (%d,%q,%v), want exists", id2, act2, err)
	}
	if len(ix.byName) != 1 {
		t.Fatalf("indexers = %d, want 1 (no dupes)", len(ix.byName))
	}
}

func TestPostgresTargetDownloadClientMapping(t *testing.T) {
	st := &fakeSettings{}
	tgt := NewPostgresTarget(st, &fakeIndexers{}, newFakeTV())
	fields := map[string]json.RawMessage{
		"host":     json.RawMessage(`"10.0.0.5"`),
		"port":     json.RawMessage(`8080`),
		"username": json.RawMessage(`"admin"`),
		"password": json.RawMessage(`"s3cr3t"`),
	}
	c := SonarrDownloadClient{ID: 1, Name: "qbit", Enable: true, ClientKey: "qBittorrent", Fields: fields}
	act, err := tgt.SetDownloadClient(context.Background(), c)
	if err != nil {
		t.Fatalf("SetDownloadClient: %v", err)
	}
	if act != ActionCreated {
		t.Fatalf("action = %q, want created", act)
	}
	if st.dc == nil || st.dc.Kind != "qbittorrent" {
		t.Fatalf("dc = %+v, want kind qbittorrent", st.dc)
	}
	if st.dc.QBBase != "http://10.0.0.5:8080" {
		t.Fatalf("QBBase = %q, want http://10.0.0.5:8080", st.dc.QBBase)
	}
	if st.dc.QBUsername != "admin" || st.dc.QBPassword != "s3cr3t" {
		t.Fatalf("QB creds = %q/%q, want admin/s3cr3t", st.dc.QBUsername, st.dc.QBPassword)
	}
	// Second run: existing -> updated.
	if act, _ := tgt.SetDownloadClient(context.Background(), c); act != ActionUpdated {
		t.Fatalf("second action = %q, want updated", act)
	}
}
