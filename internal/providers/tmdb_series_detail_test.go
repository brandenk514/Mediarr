package providers

import (
	"context"
	"net/http"
	"testing"
)

func TestTMDBGetSeriesDetail(t *testing.T) {
	url, rec := newStub(t, http.StatusOK, `{
	  "overview": "A drug kingpin becomes a brilliant chemistry teacher.",
	  "original_language": "en",
	  "vote_average": 8.9,
	  "vote_count": 21543,
	  "first_air_date": "2008-01-20",
	  "status": "Ended",
	  "poster_path": "/bb-poster.jpg",
	  "backdrop_path": "/bb-backdrop.jpg",
	  "networks": [{"id": 49, "name": "AMC"}],
	  "production_countries": [
	    {"iso_3166_1": "US", "name": "United States of America"},
	    {"iso_3166_1": "MX", "name": "Mexico"}
	  ],
	  "genres": [{"id": 18, "name": "Drama"}, {"id": 80, "name": "Crime"}],
	  "keywords": {"results": [{"id": 1, "name": "drug"}, {"id": 2, "name": "mexico"}]},
	  "credits": {
	    "cast": [
	      {"name": "Bryan Cranston", "character": "Walter White"},
	      {"name": "Anna Torv", "character": "Saul Goodman"},
	      {"name": ""}
	    ],
	    "crew": [
	      {"name": "Vince Gilligan", "job": "Creator"},
	      {"name": "Glen Mazzara", "job": "Writer"}
	    ]
	  },
	  "videos": [
	    {"key": "abc123", "site": "YouTube", "type": "Teaser", "official": true},
	    {"key": "bb-trailer-2008", "site": "YouTube", "type": "Trailer", "official": true},
	    {"key": "vimeo-x", "site": "Vimeo", "type": "Trailer", "official": true}
	  ],
	  "external_ids": {"imdb_id": "tt0903747", "tvdb_id": 81016, "trakt": 1541}
	}`)
	m := NewTMDB("k-123")
	m.BaseURL = url

	d, err := m.GetSeriesDetail(context.Background(), "1396")
	if err != nil {
		t.Fatalf("GetSeriesDetail: %v", err)
	}
	// Basic fields.
	if d.Overview != "A drug kingpin becomes a brilliant chemistry teacher." || d.VoteAverage != 8.9 || d.VoteCount != 21543 {
		t.Fatalf("basic fields: %+v", d)
	}
	if d.FirstAirDate != "2008-01-20" || d.Status != "Ended" || d.OriginalLanguage != "en" {
		t.Fatalf("date/status/lang: %q/%q/%q", d.FirstAirDate, d.Status, d.OriginalLanguage)
	}
	if d.Network != "AMC" {
		t.Fatalf("network = %q, want AMC (first of networks[])", d.Network)
	}
	if len(d.ProductionCountries) != 2 || d.ProductionCountries[0] != "United States of America" {
		t.Fatalf("production countries: %v (want names, not ISO codes)", d.ProductionCountries)
	}
	if len(d.Genres) != 2 || d.Genres[0] != "Drama" {
		t.Fatalf("genres: %v", d.Genres)
	}
	if len(d.Keywords) != 2 || d.Keywords[0] != "drug" {
		t.Fatalf("keywords: %v", d.Keywords)
	}
	// External ids: id mapping only — TMDB does not serve IMDb/Trakt
	// ratings, so the payload maps ids, never invented scores.
	if d.ImdbID != "tt0903747" || d.TraktID != "1541" {
		t.Fatalf("external ids: %q/%q", d.ImdbID, d.TraktID)
	}
	// Full cast list (a blank-named credit is skipped); full plain-credits
	// crew (no movie-style key-job filter — #152 renders "View Full Crew").
	if len(d.Cast) != 2 || d.Cast[0].Name != "Bryan Cranston" || d.Cast[0].Role != "Walter White" {
		t.Fatalf("cast: %+v (blank-named credit must be skipped)", d.Cast)
	}
	if len(d.Crew) != 2 || d.Crew[0].Job != "Creator" {
		t.Fatalf("crew: %+v (full plain-credits list, no job filter)", d.Crew)
	}
	// Top trailer: the first YouTube video of type Trailer (the Teaser
	// comes first, the Vimeo trailer comes after — neither wins).
	if want := "https://www.youtube.com/watch?v=bb-trailer-2008"; d.TrailerURL != want {
		t.Fatalf("TrailerURL = %q, want %q", d.TrailerURL, want)
	}
	// Artwork paths ride in the same payload (one detail call, the movie
	// precedent).
	if d.PosterPath != "/bb-poster.jpg" || d.BackdropPath != "/bb-backdrop.jpg" {
		t.Fatalf("artwork paths: %q/%q", d.PosterPath, d.BackdropPath)
	}
	// append_to_response must request the EXACT reference set (the fork's
	// TV_DETAILS_APPEND_TO_RESPONSE, server/api/themoviedb/index.ts:147).
	if got := rec.Query["append_to_response"]; len(got) != 1 || got[0] != "aggregate_credits,credits,external_ids,keywords,videos,content_ratings,watch/providers" {
		t.Fatalf("append_to_response = %v", got)
	}
	if rec.Path != "/tv/1396" {
		t.Fatalf("path = %q", rec.Path)
	}
	if rec.Header.Get("X-Api-Key") != "k-123" {
		t.Fatalf("auth header missing")
	}
}

func TestTMDBGetSeriesDetail_NoTrailer(t *testing.T) {
	url, _ := newStub(t, http.StatusOK, `{"overview": "x", "videos": [
	  {"key": "abc", "site": "YouTube", "type": "Teaser", "official": true}
	]}`)
	m := NewTMDB("k")
	m.BaseURL = url
	d, err := m.GetSeriesDetail(context.Background(), "1")
	if err != nil {
		t.Fatalf("GetSeriesDetail: %v", err)
	}
	if d.TrailerURL != "" {
		t.Fatalf("TrailerURL = %q, want \"\" (no type-Trailer video)", d.TrailerURL)
	}
}

func TestTMDBGetSeriesDetail_NotConfigured(t *testing.T) {
	m := NewTMDB("")
	if _, err := m.GetSeriesDetail(context.Background(), "1"); err != ErrNotConfigured {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

func TestTMDBGetSeriesDetail_BadID(t *testing.T) {
	m := NewTMDB("k")
	if _, err := m.GetSeriesDetail(context.Background(), ""); err == nil {
		t.Fatal("empty id must error")
	}
}

func TestTMDBGetSeriesDetail_BadResponse(t *testing.T) {
	url, _ := newStub(t, http.StatusNotFound, `{"status_message":"Not found"}`)
	m := NewTMDB("k")
	m.BaseURL = url
	if _, err := m.GetSeriesDetail(context.Background(), "999"); err == nil {
		t.Fatal("404 must error")
	}
}
