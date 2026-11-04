package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// B5/C: both book profile kinds are reachable on the settings endpoints —
// the book quality profiles (the 4 Readarr factory defaults) and the book
// metadata profiles, seeded on the profiles section document.
func TestBooksB5_ProfilesSection_Defaults(t *testing.T) {
	s, _, _, token := newSettingsSectionTestServer(t)
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/settings/profiles", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var doc struct {
		BookMetadataProfiles []struct {
			ID          int      `json:"id"`
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Languages   []string `json:"allowed_languages"`
		} `json:"book_metadata_profiles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	// The book quality profile names: the wire is an array of objects.
	var raw struct {
		BookProfiles         []map[string]any `json:"book_profiles"`
		BookMetadataProfiles []map[string]any `json:"book_metadata_profiles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	var bookNames []string
	for _, p := range raw.BookProfiles {
		bookNames = append(bookNames, p["name"].(string))
	}
	want := []string{"Best", "EPUB", "Kindle", "Any"}
	if len(bookNames) != len(want) {
		t.Fatalf("book_profiles = %v, want %v", bookNames, want)
	}
	for i := range want {
		if bookNames[i] != want[i] {
			t.Errorf("book_profiles[%d] = %q, want %q", i, bookNames[i], want[i])
		}
	}
	if len(raw.BookMetadataProfiles) != 1 {
		t.Fatalf("book_metadata_profiles = %v, want the single Default profile", raw.BookMetadataProfiles)
	}
	bmp := raw.BookMetadataProfiles[0]
	if bmp["name"] != "Default" || bmp["id"] != float64(1) {
		t.Fatalf("book_metadata_profiles[0] = %v, want Default id 1", bmp)
	}
	if got := doc.BookMetadataProfiles[0]; got.ID != 1 || got.Name != "Default" || len(got.Languages) != 0 {
		t.Fatalf("book metadata default = %+v, want Default with no language filters", got)
	}
}

// B5/C: a full-document PUT on the profiles section persists BOTH book
// profile kinds (a new book quality profile + a new book metadata profile)
// without disturbing the other kinds.
func TestBooksB5_ProfilesSection_PutRoundTrip(t *testing.T) {
	s, _, _, token := newSettingsSectionTestServer(t)
	get := func() map[string]any {
		rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/settings/profiles", token)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET: %d %s", rec.Code, rec.Body.String())
		}
		var doc map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		return doc
	}

	doc := get()
	// Add a fifth book quality profile (a copy of the first, renamed).
	books := doc["book_profiles"].([]any)
	base := books[0].(map[string]any)
	fifth := map[string]any{
		"id":     float64(5),
		"name":   "Custom Format",
		"cutoff": base["cutoff"],
		"items":  base["items"],
	}
	doc["book_profiles"] = append(books, fifth)
	// Add a second book metadata profile with filters.
	meta := doc["book_metadata_profiles"].([]any)
	doc["book_metadata_profiles"] = append(meta, map[string]any{
		"id":                        float64(2),
		"name":                      "Filtered",
		"description":               "Only popular, complete books.",
		"minimum_popularity":        float64(5),
		"skip_missing_release_date": true,
		"skip_no_isbn_asin":         true,
		"skip_broken_parts":         true,
		"skip_secondary_series":     true,
		"allowed_languages":         []any{"en"},
	})
	rec := doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/profiles", token, string(mustJSON(t, doc)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}

	back := get()
	books = back["book_profiles"].([]any)
	if len(books) != 5 {
		t.Fatalf("round-trip book_profiles = %d entries, want 5", len(books))
	}
	last := books[4].(map[string]any)
	if last["name"] != "Custom Format" || last["id"] != float64(5) {
		t.Fatalf("round-trip book_profiles[4] = %v, want Custom Format id 5", last)
	}
	meta = back["book_metadata_profiles"].([]any)
	if len(meta) != 2 {
		t.Fatalf("round-trip book_metadata_profiles = %d entries, want 2", len(meta))
	}
	if got := meta[1].(map[string]any); got["name"] != "Filtered" || got["minimum_popularity"] != float64(5) || got["allowed_languages"].([]any)[0] != "en" {
		t.Fatalf("round-trip book_metadata_profiles[1] = %v, want the Filtered profile", got)
	}
	// The non-book kinds are untouched by the round trip.
	if n := len(back["music_profiles"].([]any)); n != 4 {
		t.Errorf("music_profiles = %d entries, want the 4 seeded music profiles", n)
	}
	if n := len(back["profiles"].([]any)); n != 1 {
		t.Errorf("profiles = %d entries, want the single TV default", n)
	}
}

// B5/C: the profiles section rejects a duplicate book profile name (the
// book-kind validation runs on PUT).
func TestBooksB5_ProfilesSection_PutValidation(t *testing.T) {
	s, _, _, token := newSettingsSectionTestServer(t)
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/settings/profiles", token)
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	books := doc["book_profiles"].([]any)
	for _, e := range books {
		if e.(map[string]any)["name"] == "EPUB" {
			e.(map[string]any)["name"] = "Best" // duplicate of the first
		}
	}
	rec = doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/profiles", token, string(mustJSON(t, doc)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT duplicate name: got %d, want 400: %s", rec.Code, rec.Body.String())
	}
}
