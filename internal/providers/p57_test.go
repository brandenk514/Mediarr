package providers

import (
	"context"
	"net/http"
	"testing"
)

func TestTVmazeSearch(t *testing.T) {
	url, rec := newStub(t, http.StatusOK, `{
	  "shows": [
	    {"score": 1, "show": {"id": 100, "name": "Breaking Bad", "premiered": "2008-01-20", "genres": ["Drama", "Crime"], "status": "Ended", "language": "en"}},
	    {"score": 2, "show": {"id": 101, "name": ""}}
	  ]
	}`)
	m := NewTVmaze()
	m.BaseURL = url
	res, err := m.Search(context.Background(), "breaking bad")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("got %d results, want 1 (blank name skipped)", len(res))
	}
	if res[0].ID != "100" || res[0].Title != "Breaking Bad" || res[0].Year != 2008 {
		t.Fatalf("bad result: %+v", res[0])
	}
	if res[0].Extra["genres"] != "Drama,Crime" || res[0].Extra["status"] != "Ended" {
		t.Fatalf("bad extra: %+v", res[0].Extra)
	}
	if rec.Query["q"][0] != "breaking bad" {
		t.Fatalf("query = %v", rec.Query["q"])
	}
}

func TestTVmazeShowEmbeddedEpisodes(t *testing.T) {
	url, _ := newStub(t, http.StatusOK, `{
	  "id": 100, "name": "Breaking Bad", "premiered": "2008-01-20",
	  "summary": "<p>A chemist.</p>",
	  "_embedded": {"episodes": [
	    {"id": 1, "season": 1, "number": 1, "title": "Pilot", "airdate": "2008-01-20"},
	    {"id": 2, "season": 1, "number": 2, "title": "Cat's in the Bag...", "airdate": "2008-01-27"}
	  ]}
	}`)
	m := NewTVmaze()
	m.BaseURL = url
	show, err := m.Show(context.Background(), 100)
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if show.Name != "Breaking Bad" || len(show.Embedded.Episodes) != 2 {
		t.Fatalf("bad show: name=%q episodes=%d", show.Name, len(show.Embedded.Episodes))
	}
	ep := show.Embedded.Episodes[0]
	if ep.Season != 1 || ep.Number != 1 || ep.AirDate != "2008-01-20" {
		t.Fatalf("bad episode: %+v", ep)
	}
}

func TestTVmazeEmptyQuery(t *testing.T) {
	res, err := NewTVmaze().Search(context.Background(), "   ")
	if err != nil || res != nil {
		t.Fatalf("empty query must short-circuit, got %v %v", res, err)
	}
}

func TestWikidataSearch(t *testing.T) {
	// opensearch returns [prefix, [titles], [descriptions], [urls]]
	wiki, _ := newStub(t, http.StatusOK, `["", ["Breaking Bad", "Breaking Bad (season 1)"], ["American TV series", "TV series"], [""] ]`)
	w := NewWikidata()
	w.WikipediaBase = wiki
	res, err := w.Search(context.Background(), "breaking bad")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("got %d results, want 2", len(res))
	}
	if res[0].ID != "Breaking Bad" || res[0].Title != "Breaking Bad" {
		t.Fatalf("bad first result: %+v", res[0])
	}
	if res[0].Extra["description"] != "American TV series" {
		t.Fatalf("bad description: %+v", res[0].Extra)
	}
}

func TestWikidataResolve(t *testing.T) {
	wd, _ := newStub(t, http.StatusOK, `{
	  "entities": {
	    "Q1162946": {"id": "Q1162946", "labels": {"en": {"value": "Breaking Bad"}}},
	    "missing0": {"id": "missing0", "labels": {}}
	  }
	}`)
	w := NewWikidata()
	w.WikidataBase = wd
	id, err := w.Resolve(context.Background(), "Breaking_Bad")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if id != "Q1162946" {
		t.Fatalf("id = %q", id)
	}
}

func TestChain_FallbackToKeyless(t *testing.T) {
	// tvdb (keyed) fails, tvmaze (keyless) succeeds -> the keyless fallback
	// wins and the attempt log records both.
	failing := &stubSearcher{err: errStub("tvdb down")}
	working := &stubSearcher{results: []Result{{ID: "100", Title: "Breaking Bad"}}}
	chain := NewChain(
		Source{Name: "tvdb", Searcher: failing},
		Source{Name: "tvmaze", Searcher: working},
	)
	name, res, attempts, err := chain.Search(context.Background(), "breaking bad")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if name != "tvmaze" || len(res) != 1 || res[0].ID != "100" {
		t.Fatalf("got source=%q results=%v", name, res)
	}
	if len(attempts) != 2 || attempts[0].OK || attempts[0].Error == "" || !attempts[1].OK {
		t.Fatalf("attempts = %+v", attempts)
	}
}

func TestChain_SkipsUnconfigured(t *testing.T) {
	working := &stubSearcher{results: []Result{{ID: "1", Title: "x"}}}
	chain := NewChain(
		Source{Name: "tmdb", Searcher: nil}, // no key -> skipped
		Source{Name: "tvmaze", Searcher: working},
	)
	name, _, attempts, err := chain.Search(context.Background(), "x")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if name != "tvmaze" {
		t.Fatalf("source = %q", name)
	}
	if attempts[0].Error != "not configured" {
		t.Fatalf("skipped source must log 'not configured': %+v", attempts[0])
	}
}

func TestChain_AllFailReturnsErr(t *testing.T) {
	chain := NewChain(
		Source{Name: "tvdb", Searcher: &stubSearcher{err: errStub("boom")}},
		Source{Name: "tvmaze", Searcher: nil},
	)
	_, _, attempts, err := chain.Search(context.Background(), "x")
	if err != ErrAllSourcesFailed {
		t.Fatalf("err = %v, want ErrAllSourcesFailed", err)
	}
	if len(attempts) != 2 {
		t.Fatalf("attempts = %+v", attempts)
	}
}

func TestChain_EmptyAnswerWins(t *testing.T) {
	// A source that answers (with zero results) ends the walk — the next
	// source is not consulted.
	chain := NewChain(
		Source{Name: "a", Searcher: &stubSearcher{results: []Result{}}},
		Source{Name: "b", Searcher: &stubSearcher{results: []Result{{ID: "9", Title: "y"}}}},
	)
	name, res, _, err := chain.Search(context.Background(), "x")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if name != "a" || len(res) != 0 {
		t.Fatalf("got source=%q len=%d, want a/0", name, len(res))
	}
}

type stubSearcher struct {
	results []Result
	err     error
}

func (s *stubSearcher) Name() string { return "stub" }
func (s *stubSearcher) Search(context.Context, string) ([]Result, error) {
	return s.results, s.err
}

func errStub(msg string) error {
	return &stubErr{msg}
}

type stubErr struct{ msg string }

func (e *stubErr) Error() string { return e.msg }
