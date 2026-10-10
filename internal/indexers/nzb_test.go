package indexers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// nzbFixture exercises the usenet-specific fields (#33): age (both numeric
// age_hours and a human "age" string), article count, and parity. It also
// covers the numeric-size and human-string-size encodings.
const nzbFixture = `{
  "results": [
    {
      "title": "Show.Name.2024.S01E01.1080p.NZB",
      "guid": "u1",
      "link": "http://usenet.example/getnzb/u1",
      "size": 1073741824,
      "age": "2 hours",
      "articles": 5000,
      "parity": true,
      "indexer": "testusenet"
    },
    {
      "title": "Human.Size.Release",
      "guid": "u2",
      "link": "http://usenet.example/getnzb/u2",
      "size": "1.5 GB",
      "age_hours": 4.5,
      "articles": 12000,
      "parity": false,
      "indexer": "testusenet"
    },
    {
      "title": "",
      "guid": "u3",
      "link": "http://usenet.example/getnzb/u3"
    }
  ]
}`

func newNZBAdapter(t *testing.T, body string, code int) *NZBIndexer {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if code != http.StatusOK {
			w.WriteHeader(code)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return NewNZBIndexer(Definition{Name: "nzb", BaseURL: srv.URL, APIKey: "key"}, AdapterDeps{})
}

// TestNZB_Search_ParsesUsenetFields is the #33 core test: usenet-specific
// fields (age, article count, parity) are surfaced into ReleaseInfo, and the
// protocol is ProtocolNZB.
func TestNZB_Search_ParsesUsenetFields(t *testing.T) {
	adp := newNZBAdapter(t, nzbFixture, 200)
	res, err := adp.Search(context.Background(), SearchQuery{Term: "Show"})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	// 3 items, one with empty title skipped → 2 results.
	if len(res) != 2 {
		t.Fatalf("expected 2 results (empty-title skipped), got %d: %+v", len(res), res)
	}

	byTitle := map[string]SearchResult{}
	for _, r := range res {
		byTitle[r.Title] = r
	}

	a := byTitle["Show.Name.2024.S01E01.1080p.NZB"]
	if a.Info.Protocol != ProtocolNZB {
		t.Errorf("expected ProtocolNZB, got %q", a.Info.Protocol)
	}
	if a.Info.URL != "http://usenet.example/getnzb/u1" {
		t.Errorf("expected nzb download url, got %q", a.Info.URL)
	}
	if a.Info.AgeHours != 2.0 { // "2 hours" → 2
		t.Errorf("expected age 2.0h, got %v", a.Info.AgeHours)
	}
	if a.Info.Articles != 5000 {
		t.Errorf("expected 5000 articles, got %d", a.Info.Articles)
	}
	if !a.Info.Parity {
		t.Error("expected parity true")
	}
	if a.SizeBytes != 1073741824 {
		t.Errorf("expected numeric size 1073741824, got %d", a.SizeBytes)
	}

	b := byTitle["Human.Size.Release"]
	if b.Info.AgeHours != 4.5 { // explicit age_hours
		t.Errorf("expected age 4.5h, got %v", b.Info.AgeHours)
	}
	if b.SizeBytes != int64(1.5*1024*1024*1024) {
		t.Errorf("expected 1.5 GB in bytes, got %d", b.SizeBytes)
	}
}

// TestNZB_Search_BadJSON maps a corrupt JSON body to ErrBadResponse.
func TestNZB_Search_BadJSON(t *testing.T) {
	adp := newNZBAdapter(t, "this is not json", 200)
	_, err := adp.Search(context.Background(), SearchQuery{})
	if !errors.Is(err, ErrBadResponse) {
		t.Fatalf("expected ErrBadResponse, got %v", err)
	}
}

// TestNZB_Search_ErrorMapping confirms a non-2xx status is classified.
func TestNZB_Search_ErrorMapping(t *testing.T) {
	adp := newNZBAdapter(t, "", 401)
	_, err := adp.Search(context.Background(), SearchQuery{})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

// TestNZB_Test_Probe confirms Test() does a cheap liveness request.
func TestNZB_Test_Probe(t *testing.T) {
	adp := newNZBAdapter(t, `{"results":[]}`, 200)
	if err := adp.Test(context.Background()); err != nil {
		t.Fatalf("Test on 200 should pass, got %v", err)
	}
}

// TestParseHumanSize covers the human-size parser across units.
func TestParseHumanSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"1 GB", 1024 * 1024 * 1024},
		{"1.5 GB", int64(1.5 * 1024 * 1024 * 1024)},
		{"500 MB", 500 * 1024 * 1024},
		{"2 KB", 2048},
		{"1024", 1024},
	}
	for _, tc := range cases {
		got, err := parseHumanSize(tc.in)
		if err != nil {
			t.Fatalf("parseHumanSize(%q) error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("parseHumanSize(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestParseNZBAge covers the human-age parser.
func TestParseNZBAge(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"2 hours", 2},
		{"3 days", 72},
		{"45 min", 0.75},
		{"1 hour", 1},
		{"", 0},
		{"unknown", 0},
	}
	for _, tc := range cases {
		if got := parseNZBAge(tc.in); got != tc.want {
			t.Errorf("parseNZBAge(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
