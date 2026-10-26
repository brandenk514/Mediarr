package providers

import (
	"context"
	"net/http"
	"testing"
)

func TestTMDBGetMovieDetail(t *testing.T) {
	url, rec := newStub(t, http.StatusOK, `{
	  "overview": "A mind-bending heist.",
	  "runtime": 148,
	  "original_language": "en",
	  "vote_average": 8.4,
	  "release_date": "2010-07-16",
	  "belongs_to_collection": {"name": "Nolan Collection"},
	  "production_companies": [{"name": "Legendary Pictures"}, {"name": "Syncopy"}],
	  "genres": [{"id": 28, "name": "Action"}, {"id": 12, "name": "Adventure"}],
	  "credits": {
	    "cast": [
	      {"name": "Leonardo DiCaprio", "character": "Cobb"},
	      {"name": ""},
	      {"name": "Marion Cotillard", "character": "Mal"}
	    ],
	    "crew": [
	      {"name": "Christopher Nolan", "job": "Director"},
	      {"name": "Someone", "job": "Casting"},
	      {"name": "Christopher Nolan", "job": "Writer"}
	    ]
	  },
	  "release_dates": {"results": [
	    {"iso_3166_1": "US", "release_dates": [{"certification": "PG-13"}]},
	    {"iso_3166_1": "GB", "release_dates": [{"certification": "12A"}]}
	  ]}
	}`)
	m := NewTMDB("k-123")
	m.BaseURL = url

	d, err := m.GetMovieDetail(context.Background(), "157336")
	if err != nil {
		t.Fatalf("GetMovieDetail: %v", err)
	}
	if d.Overview != "A mind-bending heist." || d.Runtime != 148 || d.VoteAverage != 8.4 {
		t.Fatalf("basic fields: %+v", d)
	}
	if d.ReleaseDate != "2010-07-16" || d.OriginalLanguage != "en" {
		t.Fatalf("date/lang: %q/%q", d.ReleaseDate, d.OriginalLanguage)
	}
	if d.Certification != "PG-13" {
		t.Fatalf("certification = %q, want PG-13 (US)", d.Certification)
	}
	if d.Studio != "Legendary Pictures" || d.CollectionName != "Nolan Collection" {
		t.Fatalf("studio/collection: %q/%q", d.Studio, d.CollectionName)
	}
	if len(d.Genres) != 2 || d.Genres[0] != "Action" {
		t.Fatalf("genres: %v", d.Genres)
	}
	if len(d.Cast) != 2 || d.Cast[0].Name != "Leonardo DiCaprio" || d.Cast[0].Role != "Cobb" {
		t.Fatalf("cast: %+v (blank-named credit must be skipped)", d.Cast)
	}
	if len(d.Crew) != 2 || d.Crew[0].Job != "Director" {
		t.Fatalf("crew: %+v (non-key jobs must be skipped)", d.Crew)
	}
	// append_to_response must request the enrichment payload.
	if got := rec.Query["append_to_response"]; len(got) != 1 || got[0] != "credits,release_dates,genres" {
		t.Fatalf("append_to_response = %v", got)
	}
	if rec.Path != "/movie/157336" {
		t.Fatalf("path = %q", rec.Path)
	}
	if rec.Header.Get("X-Api-Key") != "k-123" {
		t.Fatalf("auth header missing")
	}
}

func TestTMDBGetMovieDetail_NotConfigured(t *testing.T) {
	m := NewTMDB("")
	if _, err := m.GetMovieDetail(context.Background(), "1"); err != ErrNotConfigured {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

func TestTMDBGetMovieDetail_BadID(t *testing.T) {
	m := NewTMDB("k")
	if _, err := m.GetMovieDetail(context.Background(), ""); err == nil {
		t.Fatal("empty id must error")
	}
}

func TestTMDBGetMovieDetail_BadResponse(t *testing.T) {
	url, _ := newStub(t, http.StatusNotFound, `{"status_message":"Not found"}`)
	m := NewTMDB("k")
	m.BaseURL = url
	if _, err := m.GetMovieDetail(context.Background(), "999"); err == nil {
		t.Fatal("404 must error")
	}
}
