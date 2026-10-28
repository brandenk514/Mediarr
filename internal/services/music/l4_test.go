package music

// L4 (Lidarr parity) service tests: AddAlbum enrichment from the keyless
// MusicBrainz + CAA adapter (success, absent id, adapter failure, missing
// adapter), and RefreshArtist (artist + album re-fetch, track replacement
// with monitored-flag preservation). All against memRepo + httptest — no
// Postgres, no network.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/music"
	"github.com/brandenk514/mediarr/internal/providers"
)

// newL4MBServer serves the group + release detail + artist detail + CAA
// images and logs request paths.
func newL4MBServer(t *testing.T, releaseStatus int) (*providers.MusicBrainz, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/release-group/group-uuid":
			_, _ = w.Write([]byte(`{
  "id": "group-uuid", "title": "OK Computer", "primary-type": "Album",
  "releases": [
    {"id": "rel-official", "title": "OK Computer", "status": "Official", "date": "1997-06-16"}
  ]}`))
		case "/release/rel-official":
			if releaseStatus != http.StatusOK {
				w.WriteHeader(releaseStatus)
				return
			}
			_, _ = w.Write([]byte(`{
  "id": "rel-official", "title": "OK Computer", "status": "Official", "date": "1997-06-16",
  "media": [{"position": 1, "tracks": [
    {"id": "tr-1", "position": 1, "title": "Airbag", "length": 237120},
    {"id": "tr-2new", "position": 2, "title": "Lust", "length": 243750},
    {"id": "tr-3", "position": 3, "title": "Fitter", "length": 137000}
  ]}],
  "labels": [{"name": "Parlophone"}],
  "genres": [{"name": "Alternative Rock"}]}`))
		case "/artist/artist-uuid":
			_, _ = w.Write([]byte(`{
  "id": "artist-uuid", "name": "Radiohead", "status": "continuing",
  "genres": [{"name": "Alternative Rock"}, {"name": "Art Rock"}],
  "relations": [{"target-type": "url", "type": "last.fm", "url": {"resource": "https://www.last.fm/music/Radiohead"}}]
}`))
		case "/release/rel-official.json": // CAA: the 500 thumbnail
			_, _ = w.Write([]byte(`{"images": [
				{"url": "https://cover/2500.png", "sizes": {"front": 2500}, "formats": {"front": ["png"]}},
				{"url": "https://cover/500.jpg", "sizes": {"front": 500}, "formats": {"front": ["jpg"]}}
			]}`))
		case "/artist/artist-uuid.json": // CAA: no artist cover
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	adapter := &providers.MusicBrainz{}
	adapter.BaseURL = srv.URL
	adapter.CAABaseURL = srv.URL
	return adapter, &paths
}

func TestL4_AddAlbum_WithReleaseGroup(t *testing.T) {
	repo := newMemRepo()
	adapter, paths := newL4MBServer(t, http.StatusOK)
	svc := New(Deps{Repo: repo, Metadata: adapter})
	ctx := context.Background()

	artistID, err := svc.AddArtist(ctx, AddArtistInput{Name: "Radiohead", Monitored: true})
	if err != nil {
		t.Fatalf("AddArtist: %v", err)
	}
	albumID, warnings, err := svc.AddAlbum(ctx, artistID, "OK Computer", 0, "Lossless", boolPtr(true), "group-uuid")
	if err != nil {
		t.Fatalf("AddAlbum: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}

	a, err := svc.GetAlbum(ctx, albumID)
	if err != nil {
		t.Fatalf("GetAlbum: %v", err)
	}
	if a.MBID != "rel-official" {
		t.Errorf("MBID = %q, want the resolved release rel-official", a.MBID)
	}
	if a.Year != 1997 || a.ReleaseDate != "1997-06-16" || a.AlbumType != "album" {
		t.Errorf("year/date/type = %d/%q/%q, want 1997/1997-06-16/album", a.Year, a.ReleaseDate, a.AlbumType)
	}
	if a.Duration != 237+243+137 {
		t.Errorf("Duration = %d, want the track sum %d", a.Duration, 237+243+137)
	}
	if len(a.Labels) != 1 || a.Labels[0] != "Parlophone" {
		t.Errorf("Labels = %v, want [Parlophone]", a.Labels)
	}
	if len(a.Genres) != 1 || a.Genres[0] != "Alternative Rock" {
		t.Errorf("Genres = %v, want [Alternative Rock]", a.Genres)
	}
	if a.Cover != "https://cover/500.jpg" {
		t.Errorf("Cover = %q, want the CAA 500 thumbnail", a.Cover)
	}

	tracks, err := svc.ListTracks(ctx, albumID)
	if err != nil {
		t.Fatalf("ListTracks: %v", err)
	}
	if len(tracks) != 3 {
		t.Fatalf("tracks = %+v, want 3 (one per release track)", tracks)
	}
	if !tracks[0].Monitored || tracks[0].MBID != "tr-1" || tracks[0].Duration != 237 {
		t.Errorf("track 0 = %+v, want monitored tr-1/237s", tracks[0])
	}
	// Monitored tracks each get a wanted row, plus the whole-album row.
	wanted, err := repo.ListWanted(ctx, albumID)
	if err != nil {
		t.Fatalf("ListWanted: %v", err)
	}
	if len(wanted) != 4 { // 1 album + 3 tracks
		t.Errorf("wanted = %+v, want 4 rows (album + 3 tracks)", wanted)
	}
	if len(*paths) != 3 || (*paths)[0] != "/release-group/group-uuid" || (*paths)[1] != "/release/rel-official" || (*paths)[2] != "/release/rel-official.json" {
		t.Errorf("paths = %v, want group, release, CAA", *paths)
	}
}

func TestL4_AddAlbum_WithoutMBID(t *testing.T) {
	repo := newMemRepo()
	adapter, paths := newL4MBServer(t, http.StatusOK)
	svc := New(Deps{Repo: repo, Metadata: adapter})
	ctx := context.Background()

	artistID, err := svc.AddArtist(ctx, AddArtistInput{Name: "Radiohead", Monitored: true})
	if err != nil {
		t.Fatalf("AddArtist: %v", err)
	}
	albumID, warnings, err := svc.AddAlbum(ctx, artistID, "No Metadata", 2010, "Lossless", boolPtr(false), "")
	if err != nil {
		t.Fatalf("AddAlbum: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	a, err := svc.GetAlbum(ctx, albumID)
	if err != nil {
		t.Fatalf("GetAlbum: %v", err)
	}
	if a.MBID != "" || a.Year != 2010 || a.ReleaseDate != "" || a.Duration != 0 || len(a.Labels) != 0 || len(a.Genres) != 0 || a.Cover != "" {
		t.Errorf("album without mbid must keep L1/L2 behavior: %+v", a)
	}
	tracks, err := svc.ListTracks(ctx, albumID)
	if err != nil {
		t.Fatalf("ListTracks: %v", err)
	}
	if len(tracks) != 0 {
		t.Errorf("tracks = %+v, want none", tracks)
	}
	if len(*paths) != 0 {
		t.Errorf("paths = %v, want no metadata requests", *paths)
	}
}

func TestL4_AddAlbum_AdapterFailure(t *testing.T) {
	repo := newMemRepo()
	adapter, _ := newL4MBServer(t, http.StatusInternalServerError) // release lookup 500s
	svc := New(Deps{Repo: repo, Metadata: adapter})
	ctx := context.Background()

	artistID, err := svc.AddArtist(ctx, AddArtistInput{Name: "Radiohead", Monitored: true})
	if err != nil {
		t.Fatalf("AddArtist: %v", err)
	}
	albumID, warnings, err := svc.AddAlbum(ctx, artistID, "OK Computer", 0, "Lossless", boolPtr(true), "group-uuid")
	if err != nil {
		t.Fatalf("AddAlbum must succeed even when enrichment fails: %v", err)
	}
	if len(warnings) != 1 || warnings[0][:3] != "met" { // "metadata enrichment failed: ..."
		t.Fatalf("warnings = %v, want one enrichment-failure warning", warnings)
	}
	a, err := svc.GetAlbum(ctx, albumID)
	if err != nil {
		t.Fatalf("GetAlbum: %v", err)
	}
	if a.MBID != "" || a.Year != 0 {
		t.Errorf("album created un-enriched: %+v", a)
	}
	tracks, _ := svc.ListTracks(ctx, albumID)
	if len(tracks) != 0 {
		t.Errorf("tracks = %+v, want none", tracks)
	}
}

func TestL4_AddAlbum_MetadataNotConfigured(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo}) // Metadata nil
	ctx := context.Background()

	artistID, err := svc.AddArtist(ctx, AddArtistInput{Name: "Radiohead", Monitored: true})
	if err != nil {
		t.Fatalf("AddArtist: %v", err)
	}
	albumID, warnings, err := svc.AddAlbum(ctx, artistID, "OK Computer", 1997, "Lossless", boolPtr(true), "group-uuid")
	if err != nil {
		t.Fatalf("AddAlbum: %v", err)
	}
	if len(warnings) != 1 || warnings[0] != "metadata enrichment skipped: metadata provider not configured" {
		t.Fatalf("warnings = %v, want the not-configured warning", warnings)
	}
	if _, err := svc.GetAlbum(ctx, albumID); err != nil {
		t.Fatalf("GetAlbum: %v", err)
	}
}

func TestL4_RefreshArtist(t *testing.T) {
	repo := newMemRepo()
	adapter, _ := newL4MBServer(t, http.StatusOK)
	svc := New(Deps{Repo: repo, Metadata: adapter})
	ctx := context.Background()

	artistID, err := svc.AddArtist(ctx, AddArtistInput{Name: "Radiohead", MBID: "artist-uuid", Monitored: true})
	if err != nil {
		t.Fatalf("AddArtist: %v", err)
	}
	albumID, _, err := svc.AddAlbum(ctx, artistID, "OK Computer", 0, "Lossless", boolPtr(true), "group-uuid")
	if err != nil {
		t.Fatalf("AddAlbum: %v", err)
	}
	// The enriched album now has tracks 1..3 (tr-1, tr-2new, tr-3), all
	// monitored. Un-monitor track 2 (matched by position after the refresh
	// sees a different MBID for it) so the refresh must preserve that flag.
	if err := svc.SetTrackMonitored(ctx, albumID, 1, 2, false); err != nil {
		t.Fatalf("SetTrackMonitored: %v", err)
	}

	fresh, report, err := svc.RefreshArtist(ctx, artistID)
	if err != nil {
		t.Fatalf("RefreshArtist: %v", err)
	}
	if report.AlbumsRefreshed != 1 || report.AlbumsFailed != 0 {
		t.Errorf("report = %+v, want {1, 0}", report)
	}
	// Artist metadata refreshed: genre joined, one link, continuing, no
	// artist cover (CAA 404).
	if fresh.Genre != "Alternative Rock, Art Rock" {
		t.Errorf("genre = %q, want \"Alternative Rock, Art Rock\"", fresh.Genre)
	}
	if len(fresh.Links) != 1 || fresh.Links[0].URL != "https://www.last.fm/music/Radiohead" {
		t.Errorf("links = %+v, want the one last.fm URL", fresh.Links)
	}
	if fresh.Status != dom.StatusContinuing {
		t.Errorf("status = %q, want continuing", fresh.Status)
	}
	if fresh.Cover != "" {
		t.Errorf("cover = %q, want \"\" (CAA 404)", fresh.Cover)
	}

	// Album refreshed from the release.
	a, err := svc.GetAlbum(ctx, albumID)
	if err != nil {
		t.Fatalf("GetAlbum: %v", err)
	}
	if a.MBID != "rel-official" || a.Year != 1997 || a.Duration != 237+243+137 || a.Cover != "https://cover/500.jpg" ||
		len(a.Labels) != 1 || len(a.Genres) != 1 {
		t.Errorf("album after refresh = %+v", a)
	}
	// Tracks replaced: 3 rows; monitored flags — track 1 preserved via MBID,
	// track 2 preserved via disc/number, track 3 (new) follows the album.
	tracks, err := svc.ListTracks(ctx, albumID)
	if err != nil {
		t.Fatalf("ListTracks: %v", err)
	}
	if len(tracks) != 3 {
		t.Fatalf("tracks after refresh = %+v, want 3", tracks)
	}
	byNumber := map[int]dom.Track{}
	for _, tr := range tracks {
		byNumber[tr.Number] = tr
	}
	if !byNumber[1].Monitored {
		t.Error("track 1 lost its monitored flag (MBID match)")
	}
	if byNumber[2].Monitored {
		t.Error("track 2 must stay unmonitored (disc/number match)")
	}
	if !byNumber[3].Monitored {
		t.Error("track 3 (new) should follow the album's monitored flag")
	}
}

func TestL4_RefreshArtist_UnknownArtist(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo, Metadata: &providers.MusicBrainz{}})
	if _, _, err := svc.RefreshArtist(context.Background(), 42); err != dom.ErrArtistNotFound {
		t.Errorf("err = %v, want ErrArtistNotFound", err)
	}
}

func TestL4_RefreshArtist_NoMBID(t *testing.T) {
	repo := newMemRepo()
	adapter, paths := newL4MBServer(t, http.StatusOK)
	svc := New(Deps{Repo: repo, Metadata: adapter})
	ctx := context.Background()

	artistID, err := svc.AddArtist(ctx, AddArtistInput{Name: "Radiohead", Monitored: true}) // no MBID
	if err != nil {
		t.Fatalf("AddArtist: %v", err)
	}
	if _, _, err := svc.AddAlbum(ctx, artistID, "No Metadata", 2010, "Lossless", boolPtr(false), ""); err != nil {
		t.Fatalf("AddAlbum: %v", err)
	}
	fresh, report, err := svc.RefreshArtist(ctx, artistID)
	if err != nil {
		t.Fatalf("RefreshArtist: %v", err)
	}
	if report.AlbumsRefreshed != 0 || report.AlbumsFailed != 0 {
		t.Errorf("report = %+v, want {0, 0}", report)
	}
	if fresh.Genre != "" || len(fresh.Links) != 0 {
		t.Errorf("artist must be unchanged: %+v", fresh)
	}
	if len(*paths) != 0 {
		t.Errorf("paths = %v, want no adapter calls for an artist without an MBID", *paths)
	}
}
