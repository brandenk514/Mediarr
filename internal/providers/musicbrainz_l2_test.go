package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const mbTestUUID = "52615704-c35c-475d-bcd0-3b4ba1a4e151"

// TestExtractMbid covers the direct-lookup query forms: a bare 36-char
// MusicBrainz id and the client-side `lidarr:<id>` prefix (case-insensitive
// prefix, tolerant of stray whitespace); anything else is a name query.
func TestExtractMbid(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"   ", ""},
		{"Radiohead", ""},
		{"lidarr:radiohead", ""},
		{"lidarr:", ""},
		{mbTestUUID, mbTestUUID},
		{"lidarr:" + mbTestUUID, mbTestUUID},
		{"LIDARR:" + mbTestUUID, mbTestUUID},
		{"lidarr: " + mbTestUUID, mbTestUUID},
		{strings.ToUpper(mbTestUUID), strings.ToUpper(mbTestUUID)},
		{mbTestUUID[:35] + "Z", ""},                   // wrong final char
		{strings.ReplaceAll(mbTestUUID, "-", ""), ""}, // missing dashes
		{mbTestUUID + "0", ""},                        // too long
	}
	for _, tc := range cases {
		if got := extractMbid(tc.in); got != tc.want {
			t.Errorf("extractMbid(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestNormalizeReleaseGroupType checks the mapping of MusicBrainz
// release-group types onto the L2 enum (unmapped types → "other").
func TestNormalizeReleaseGroupType(t *testing.T) {
	cases := map[string]string{
		"Album": "album", "ALBUM": "album",
		"Single":      "single",
		"EP":          "ep",
		"Compilation": "compilation",
		"Live":        "live",
		"Remix":       "remix",
		"Broadcast":   "broadcast",
		"Demo":        "demo",
		"DJ-Mix":      "djmix",
		"Spoken Word": "other",
		"Soundtrack":  "other",
		"Video":       "other",
		"":            "other",
	}
	for in, want := range cases {
		if got := normalizeReleaseGroupType(in); got != want {
			t.Errorf("normalizeReleaseGroupType(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSearchArtistHits_EnrichesTopHits: the search payload supplies
// id/name/type/country/disambiguation; genres and external links come from
// the best-effort detail fetch, which is capped at mbDetailEnrichLimit hits
// (the rest ship with empty badge fields).
func TestSearchArtistHits_EnrichesTopHits(t *testing.T) {
	var detailPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/artist":
			_, _ = w.Write([]byte(`{"artists":[
				{"id":"a1","name":"Radiohead","type":"Group","country":"UK","disambiguation":"rock band"},
				{"id":"a2","name":"Radiohead Jr.","type":"Person"},
				{"id":"a3","name":"Radiohead III"},
				{"id":"a4","name":"Radiohead IV"}]}`))
		default:
			if strings.HasPrefix(r.URL.Path, "/artist/") {
				detailPaths = append(detailPaths, r.URL.Path)
				_, _ = w.Write([]byte(`{"id":"x","name":"x",
					"genres":[{"name":"Rock"},{"name":""}],
					"relations":[
					 {"target-type":"url","type":"official homepage","url":{"resource":"https://example.com/x"}},
					 {"target-type":"work","type":"other","url":{"resource":"http://other.example"}},
					 {"target-type":"url","type":"social network","url":{"resource":"https://social.example/x"}}]}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	mb := NewMusicBrainz()
	mb.BaseURL = srv.URL

	hits, err := mb.SearchArtistHits(context.Background(), "radiohead")
	if err != nil {
		t.Fatalf("SearchArtistHits: %v", err)
	}
	if len(hits) != 4 {
		t.Fatalf("want 4 hits, got %d", len(hits))
	}
	if hits[0].ID != "a1" || hits[0].Title != "Radiohead" || hits[0].Type != "Group" ||
		hits[0].Country != "UK" || hits[0].Disambiguation != "rock band" {
		t.Errorf("search fields: %+v", hits[0])
	}
	if len(hits[0].Genres) != 1 || hits[0].Genres[0] != "Rock" {
		t.Errorf("genres = %v, want [Rock] (blank genre name skipped)", hits[0].Genres)
	}
	wantLinks := []string{"https://example.com/x", "https://social.example/x"}
	if len(hits[0].Links) != 2 || hits[0].Links[0] != wantLinks[0] || hits[0].Links[1] != wantLinks[1] {
		t.Errorf("links = %v, want %v", hits[0].Links, wantLinks)
	}
	if hits[0].Score != 0 {
		t.Errorf("score = %v, want 0 (MusicBrainz has no popularity number)", hits[0].Score)
	}
	// Enrichment is capped: the first mbDetailEnrichLimit hits only.
	want := []string{"/artist/a1", "/artist/a2", "/artist/a3"}
	if len(detailPaths) != len(want) {
		t.Fatalf("detail fetches = %v, want %v (cap %d)", detailPaths, want, mbDetailEnrichLimit)
	}
	for i := range want {
		if detailPaths[i] != want[i] {
			t.Errorf("detail fetch %d = %s, want %s", i, detailPaths[i], want[i])
		}
	}
	if len(hits[3].Genres) != 0 || len(hits[3].Links) != 0 {
		t.Errorf("hit 4 (past the cap) must have empty badge fields: %+v", hits[3])
	}
}

// TestSearchArtistHits_DetailFailsDegrades: a failed detail fetch leaves the
// hit's search-payload fields in place (the spec's "empty when unknown")
// instead of failing the whole search.
func TestSearchArtistHits_DetailFailsDegrades(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/artist":
			_, _ = w.Write([]byte(`{"artists":[{"id":"a1","name":"Radiohead","type":"Group","country":"UK"}]}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
		}
	}))
	defer srv.Close()
	mb := NewMusicBrainz()
	mb.BaseURL = srv.URL

	hits, err := mb.SearchArtistHits(context.Background(), "radiohead")
	if err != nil {
		t.Fatalf("SearchArtistHits: %v (a detail failure must not fail the search)", err)
	}
	if len(hits) != 1 || hits[0].Title != "Radiohead" || hits[0].Type != "Group" ||
		len(hits[0].Genres) != 0 || len(hits[0].Links) != 0 {
		t.Errorf("degraded hit = %+v", hits)
	}
}

// TestSearchArtistHits_MbidDirect: a bare MusicBrainz id (or with the
// client-side `lidarr:` prefix) resolves directly to the artist via the
// detail endpoint — no name search request is made.
func TestSearchArtistHits_MbidDirect(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/artist" {
			t.Errorf("direct lookup must not issue a name search")
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"id":"` + mbTestUUID + `","name":"Radiohead","type":"Group",
			"country":"UK","disambiguation":"alternative rock band",
			"genres":[{"name":"Rock"},{"name":"Alternative"}],
			"relations":[{"target-type":"url","type":"official homepage","url":{"resource":"https://en.wikipedia.org/wiki/Radiohead"}}]}`))
	}))
	defer srv.Close()
	mb := NewMusicBrainz()
	mb.BaseURL = srv.URL

	for _, q := range []string{mbTestUUID, "lidarr:" + mbTestUUID} {
		paths = nil
		hits, err := mb.SearchArtistHits(context.Background(), q)
		if err != nil {
			t.Fatalf("SearchArtistHits(%q): %v", q, err)
		}
		if len(hits) != 1 {
			t.Fatalf("want 1 hit, got %d", len(hits))
		}
		h := hits[0]
		if h.ID != mbTestUUID || h.Title != "Radiohead" || h.Type != "Group" || h.Country != "UK" {
			t.Errorf("hit = %+v", h)
		}
		if len(h.Genres) != 2 || h.Genres[0] != "Rock" || h.Genres[1] != "Alternative" {
			t.Errorf("genres = %v", h.Genres)
		}
		if len(h.Links) != 1 || h.Links[0] != "https://en.wikipedia.org/wiki/Radiohead" {
			t.Errorf("links = %v", h.Links)
		}
		if len(paths) != 1 || paths[0] != "/artist/"+mbTestUUID {
			t.Errorf("requests = %v, want [/%s]", paths, "artist/"+mbTestUUID)
		}
	}
}

// TestSearchReleaseGroupHits: the release-group search payload maps to
// display hits — first-release year, release count, the normalized type, and
// the joined artist credit ("" when the payload has none).
func TestSearchReleaseGroupHits(t *testing.T) {
	var lastQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/release-group" {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		lastQuery = r.URL.Query().Get("query")
		_, _ = w.Write([]byte(`{"release-groups":[
			{"id":"g1","name":"OK Computer","first-release-year":1997,"type":"Album",
			 "releases":[{"id":"r1"},{"id":"r2"}],"artist-credit":[{"artist":{"name":"Radiohead"},"joinphrase":""}]},
			{"id":"g2","name":"Mixes","type":"DJ-Mix","releases":[{"id":"r1"}],"artist-credit":[]},
			{"id":"g3","name":"Words","type":"Spoken Word","releases":[],
			 "artist-credit":[{"artist":{"name":"A"}},{"artist":{"name":"B"}}]}]}`))
	}))
	defer srv.Close()
	mb := NewMusicBrainz()
	mb.BaseURL = srv.URL

	hits, err := mb.SearchReleaseGroupHits(context.Background(), "radiohead")
	if err != nil {
		t.Fatalf("SearchReleaseGroupHits: %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("want 3 hits, got %d", len(hits))
	}
	if lastQuery != "release-group:radiohead" {
		t.Errorf("query = %q, want release-group:radiohead", lastQuery)
	}
	g0, g1, g2 := hits[0], hits[1], hits[2]
	if g0.ID != "g1" || g0.Title != "OK Computer" || g0.Year != 1997 || g0.ReleaseCount != 2 ||
		g0.Type != "album" || g0.Artist != "Radiohead" {
		t.Errorf("hit 1 = %+v", g0)
	}
	if g1.Type != "djmix" || g1.ReleaseCount != 1 || g1.Artist != "" || g1.Year != 0 {
		t.Errorf("hit 2 = %+v", g1)
	}
	if g2.Type != "other" || g2.ReleaseCount != 0 || g2.Artist != "A, B" {
		t.Errorf("hit 3 = %+v", g2)
	}
}

// TestSearchArtistHits_EmptyQuery: a blank query returns empty results
// without hitting the provider.
func TestSearchArtistHits_EmptyQuery(t *testing.T) {
	hits, err := NewMusicBrainz().SearchArtistHits(context.Background(), "  ")
	if err != nil || len(hits) != 0 {
		t.Fatalf("want ([], nil), got (%v, %v)", hits, err)
	}
	groups, err := NewMusicBrainz().SearchReleaseGroupHits(context.Background(), "")
	if err != nil || len(groups) != 0 {
		t.Fatalf("want ([], nil), got (%v, %v)", groups, err)
	}
}
