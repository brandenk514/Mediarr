package providers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brandenk514/mediarr/internal/providers"
)

// L4 provider tests (Lidarr parity, phase L4): keyless release detail
// (group preferred, release fallback), date/type/label/genre normalization,
// and Cover Art Archive front-cover selection. All against httptest stubs —
// no network, no tokens.

const (
	l4GroupJSON = `{
  "id": "group-uuid",
  "title": "OK Computer",
  "primary-type": "Album",
  "releases": [
    {"id": "rel-promo", "title": "OK Computer (promo)", "status": "Promotion", "date": "1997-06-01"},
    {"id": "rel-official", "title": "OK Computer", "status": "Official", "date": "1997-06-16"},
    {"id": "rel-reissue", "title": "OK Computer (25th)", "status": "Official", "date": "2022-03-25"}
  ]
}`

	l4ReleaseJSON = `{
  "id": "rel-official",
  "title": "OK Computer",
  "status": "Official",
  "date": "1997-06-16",
  "media": [
    {
      "position": 1,
      "tracks": [
        {"id": "tr-1", "position": 1, "title": "Airbag", "length": 237120},
        {"id": "tr-2", "position": 2, "title": "Lust", "length": 243750}
      ]
    }
  ],
  "labels": [{"name": "Parlophone", "catno": "CDP 244 140 2"}],
  "genres": [{"name": "Alternative Rock", "count": 42}, {"name": "Alternative Rock", "count": 12}]
}`

	l4ReleasePartialJSON = `{
  "id": "rel-partial",
  "title": "Strangelove",
  "status": "Official",
  "date": "1984-04",
  "media": [
    {"position": 1, "tracks": [{"id": "tr-s1", "position": 1, "title": "Panic", "length": 183000}]}
  ],
  "labels": [],
  "genres": []
}`
)

// newL4Server returns a stub serving one release group, three releases, and
// CAA images, with request-path logging.
func newL4Server(t *testing.T, group404 bool) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/release-group/comp-group":
			_, _ = w.Write([]byte(`{
  "id": "comp-group",
  "title": "Now That's What I Call Music",
  "primary-type": "Compilation",
  "releases": [
    {"id": "rel-official", "title": "Now That's What I Call Music", "status": "Official", "date": "1997-06-16"}
  ]
}`))
		case "/release-group/group-uuid":
			if group404 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(l4GroupJSON))
		case "/release/rel-official":
			_, _ = w.Write([]byte(l4ReleaseJSON))
		case "/release/rel-partial":
			_, _ = w.Write([]byte(l4ReleasePartialJSON))
		case "/release/unknown-release":
			w.WriteHeader(http.StatusNotFound)
		case "/release/rel-official.json": // CAA for a release id
			_, _ = w.Write([]byte(`{"images": [
				{"url": "https://cover/front-2500.png", "sizes": {"front": 2500}, "formats": {"front": ["png"]}},
				{"url": "https://cover/front-500.jpg", "sizes": {"front": 500}, "formats": {"front": ["jpg"]}}
			]}`))
		case "/release/rel-small.json": // CAA: only a large front
			_, _ = w.Write([]byte(`{"images": [
				{"url": "https://cover/front-1200.png", "sizes": {"front": 1200}, "formats": {"front": ["png"]}}
			]}`))
		case "/release/rel-none.json": // CAA: no front images
			_, _ = w.Write([]byte(`{"images": [
				{"url": "https://cover/back.png", "sizes": {"back": 500}, "formats": {"back": ["png"]}}
			]}`))
		case "/artist/artist-uuid":
			_, _ = w.Write([]byte(`{
  "id": "artist-uuid",
  "name": "Radiohead",
  "status": "continuing",
  "genres": [{"name": "Alternative Rock", "count": 10}, {"name": "Art Rock", "count": 4}],
  "relations": [
    {"target-type": "url", "type": "last.fm", "url": {"resource": "https://www.last.fm/music/Radiohead"}},
    {"target-type": "url", "type": "wikipedia", "url": {"resource": "https://en.wikipedia.org/wiki/Radiohead"}},
    {"target-type": "work", "type": "cover-art"}
  ]
}`))
		case "/artist/artist-uuid.json": // CAA: 404 for an artist without art
			w.WriteHeader(http.StatusNotFound)
		case "/release/rel-404.json":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &paths
}

func TestL4_ReleaseDetail_GroupPrefersOfficial(t *testing.T) {
	srv, paths := newL4Server(t, false)
	adapter := &providers.MusicBrainz{}
	adapter.BaseURL = srv.URL
	adapter.CAABaseURL = srv.URL

	detail, err := adapter.ReleaseDetail(context.Background(), "group-uuid")
	if err != nil {
		t.Fatalf("ReleaseDetail = %v", err)
	}
	if detail.MBID != "rel-official" {
		t.Fatalf("MBID = %q, want the official release rel-official (not the earlier promo)", detail.MBID)
	}
	if detail.Name != "OK Computer" || detail.ReleaseDate != "1997-06-16" || detail.Year != 1997 {
		t.Errorf("name/date/year = %q/%q/%d, want OK Computer/1997-06-16/1997", detail.Name, detail.ReleaseDate, detail.Year)
	}
	if detail.AlbumType != "album" {
		t.Errorf("AlbumType = %q, want album (from group primary-type)", detail.AlbumType)
	}
	if len(detail.Labels) != 1 || detail.Labels[0] != "Parlophone" {
		t.Errorf("Labels = %v, want [Parlophone]", detail.Labels)
	}
	if len(detail.Genre) != 1 || detail.Genre[0] != "Alternative Rock" {
		t.Errorf("Genre = %v, want deduped [Alternative Rock]", detail.Genre)
	}
	if len(detail.Tracks) != 2 {
		t.Fatalf("len(Tracks) = %d, want 2", len(detail.Tracks))
	}
	tr := detail.Tracks[0]
	if tr.Disc != 1 || tr.Number != 1 || tr.Title != "Airbag" || tr.Duration != 237 || tr.MBID != "tr-1" {
		t.Errorf("track 0 = %+v, want disc1/num1/Airbag/237s/tr-1", tr)
	}
	if detail.Tracks[1].Title != "Lust" || detail.Tracks[1].Duration != 243 {
		t.Errorf("track 1 = %+v, want Lust/243s", detail.Tracks[1])
	}
	// Bounded: exactly one group lookup + one release lookup.
	if len(*paths) != 2 || (*paths)[0] != "/release-group/group-uuid" || (*paths)[1] != "/release/rel-official" {
		t.Errorf("request paths = %v, want [group, release]", *paths)
	}
}

func TestL4_ReleaseDetail_FallsBackToReleaseID(t *testing.T) {
	srv, paths := newL4Server(t, true) // the group lookup 404s
	adapter := &providers.MusicBrainz{}
	adapter.BaseURL = srv.URL
	adapter.CAABaseURL = srv.URL

	// The same id resolves as a bare release (partial date "1984-04").
	detail, err := adapter.ReleaseDetail(context.Background(), "rel-partial")
	if err != nil {
		t.Fatalf("ReleaseDetail = %v", err)
	}
	if detail.MBID != "rel-partial" {
		t.Fatalf("MBID = %q, want rel-partial", detail.MBID)
	}
	if detail.ReleaseDate != "" || detail.Year != 1984 {
		t.Errorf("ReleaseDate/Year = %q/%d, want \"\"/1984 (partial date keeps the year only)", detail.ReleaseDate, detail.Year)
	}
	if detail.AlbumType != "album" {
		t.Errorf("AlbumType = %q, want the album default for a bare release", detail.AlbumType)
	}
	if len(detail.Labels) != 0 || len(detail.Genre) != 0 {
		t.Errorf("Labels/Genres = %v/%v, want empty (non-nil) slices", detail.Labels, detail.Genre)
	}
	if len(detail.Tracks) != 1 || detail.Tracks[0].Duration != 183 {
		t.Errorf("Tracks = %+v, want one 183s track", detail.Tracks)
	}
	if len(*paths) != 2 || (*paths)[0] != "/release-group/rel-partial" || (*paths)[1] != "/release/rel-partial" {
		t.Errorf("request paths = %v, want group attempt then release", *paths)
	}
}

func TestL4_ReleaseDetail_UnknownIDFails(t *testing.T) {
	srv, _ := newL4Server(t, true)
	adapter := &providers.MusicBrainz{}
	adapter.BaseURL = srv.URL

	if _, err := adapter.ReleaseDetail(context.Background(), "unknown-release"); err == nil {
		t.Fatal("ReleaseDetail(unknown) should fail when both the group and release lookups 404")
	}
	if _, err := adapter.ReleaseDetail(context.Background(), "   "); err == nil {
		t.Fatal("ReleaseDetail(empty id) should fail without a network call")
	}
}

func TestL4_ReleaseDetail_TypeMapping(t *testing.T) {
	// Group-driven type mapping: a "Compilation" group maps to "compilation";
	// everything else keeps the L2 vocabulary (the "Album" case is covered by
	// the official-preference test above).
	srv, _ := newL4Server(t, false)
	adapter := &providers.MusicBrainz{}
	adapter.BaseURL = srv.URL
	detail, err := adapter.ReleaseDetail(context.Background(), "comp-group")
	if err != nil {
		t.Fatal(err)
	}
	if detail.AlbumType != "compilation" {
		t.Errorf("AlbumType = %q, want compilation (from group primary-type)", detail.AlbumType)
	}
	if detail.Status != "Official" {
		t.Errorf("Status = %q, want Official", detail.Status)
	}
}

func TestL4_ArtistDetail(t *testing.T) {
	srv, _ := newL4Server(t, false)
	adapter := &providers.MusicBrainz{}
	adapter.BaseURL = srv.URL

	d, err := adapter.GetArtistDetail(context.Background(), "artist-uuid")
	if err != nil {
		t.Fatalf("GetArtistDetail = %v", err)
	}
	if d.ID != "artist-uuid" || d.Name != "Radiohead" || d.Status != "continuing" {
		t.Errorf("artist = %+v", d)
	}
	if len(d.Genres) != 2 || d.Genres[0] != "Alternative Rock" || d.Genres[1] != "Art Rock" {
		t.Errorf("genres = %v", d.Genres)
	}
	if len(d.Links) != 2 || d.Links[0] != "https://www.last.fm/music/Radiohead" {
		t.Errorf("links = %v", d.Links)
	}
	if _, err := adapter.GetArtistDetail(context.Background(), ""); err == nil {
		t.Error("GetArtistDetail(empty) should fail")
	}
}

func TestL4_CoverArt_FrontSelection(t *testing.T) {
	srv, _ := newL4Server(t, false)
	adapter := &providers.MusicBrainz{}
	adapter.BaseURL = srv.URL
	adapter.CAABaseURL = srv.URL

	if got := adapter.ReleaseCoverArt(context.Background(), "rel-official"); got != "https://cover/front-500.jpg" {
		t.Errorf("ReleaseCoverArt = %q, want the 500px front thumbnail", got)
	}
	if got := adapter.ReleaseCoverArt(context.Background(), "rel-small"); got != "https://cover/front-1200.png" {
		t.Errorf("ReleaseCoverArt(only 1200) = %q, want the smallest available", got)
	}
	if got := adapter.ReleaseCoverArt(context.Background(), "rel-none"); got != "" {
		t.Errorf("ReleaseCoverArt(no front) = %q, want \"\"", got)
	}
	if got := adapter.ReleaseCoverArt(context.Background(), "rel-404"); got != "" {
		t.Errorf("ReleaseCoverArt(404) = %q, want \"\"", got)
	}
	if got := adapter.ReleaseCoverArt(context.Background(), ""); got != "" {
		t.Errorf("ReleaseCoverArt(empty) = %q, want \"\"", got)
	}
	if got := adapter.ArtistCoverArt(context.Background(), "artist-uuid"); got != "" {
		t.Errorf("ArtistCoverArt(404) = %q, want \"\"", got)
	}
}
