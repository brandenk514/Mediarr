package api

import (
	"net/http"
	"testing"
)

// Issue #150: every per-kind list endpoint validates ?sort= against its
// server-side allowlist; an unknown field or direction is a 400, not a
// silent default. (The movies contract lives in lists_test.go; these
// cover the other three kinds.)

func TestListSeries_UnknownSort400(t *testing.T) {
	f := newTVImportServer(t, t.TempDir())
	rec := getJSON(t, f.s.Handler(), "/api/v1/tv?sort=genre-asc", f.token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown series sort status = %d, want 400", rec.Code)
	}
	// A known value keeps working.
	rec = getJSON(t, f.s.Handler(), "/api/v1/tv?sort=title-asc", f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("title-asc status = %d, want 200", rec.Code)
	}
}

func TestListArtists_UnknownSort400(t *testing.T) {
	f := newKindsServer(t, newKindsMusicRepo(), newKindsBooksRepo())
	rec := getJSON(t, f.s.Handler(), "/api/v1/music/artists?sort=tempo-asc", f.token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown artist sort status = %d, want 400", rec.Code)
	}
	rec = getJSON(t, f.s.Handler(), "/api/v1/music/artists?sort=rating-desc", f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("rating-desc status = %d, want 200", rec.Code)
	}
}

func TestListAuthors_UnknownSort400(t *testing.T) {
	f := newKindsServer(t, newKindsMusicRepo(), newKindsBooksRepo())
	rec := getJSON(t, f.s.Handler(), "/api/v1/books/authors?sort=publisher-asc", f.token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown author sort status = %d, want 400", rec.Code)
	}
	rec = getJSON(t, f.s.Handler(), "/api/v1/books/authors?sort=title-asc", f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("title-asc status = %d, want 200", rec.Code)
	}
}
