package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// R5a: per-indexer toggles (rss / auto_search / interactive_search).
//
// Create: omitted toggles default to true; explicit values are honored.
func TestIndexerWrite_R5aTogglesCreate(t *testing.T) {
	s, _, token := newIndexerWriteTestServer(t)

	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/indexers", token,
		`{"name":"explicit","kind":"fake","rss":false,"auto_search":false,"interactive_search":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["rss"] != false || out["auto_search"] != false || out["interactive_search"] != true {
		t.Errorf("explicit toggles not honored: %v", out)
	}

	rec = doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/indexers", token,
		`{"name":"omitted","kind":"fake"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["rss"] != true || out["auto_search"] != true || out["interactive_search"] != true {
		t.Errorf("omitted toggles must default true: %v", out)
	}
}

// R5a: reads of a legacy indexer (no settings bag) apply the all-true
// defaults; an update with a single omitted-toggle body keeps the other
// stored toggles.
func TestIndexerWrite_R5aTogglesUpdate(t *testing.T) {
	s, _, token := newIndexerWriteTestServer(t)
	// The seeded "nyaa" indexer has no settings bag: the list read applies
	// factory defaults.
	rec := doJSONReq(t, s.Handler(), http.MethodGet, "/api/v1/indexers", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var listed []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	for _, l := range listed {
		if l["id"] == float64(1) {
			out = l
		}
	}
	if out == nil {
		t.Fatal("seeded indexer id 1 not in list")
	}
	if out["rss"] != true || out["auto_search"] != true || out["interactive_search"] != true {
		t.Errorf("legacy entry must read all-true defaults: %v", out)
	}

	// Flip only rss off; the other toggles must come from the defaults.
	rec = doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/indexers/1", token,
		`{"name":"nyaa","kind":"fake","rss":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["rss"] != false || out["auto_search"] != true || out["interactive_search"] != true {
		t.Errorf("partial update must keep the other toggles: %v", out)
	}

	// A subsequent update with no toggles keeps the stored bag.
	rec = doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/indexers/1", token,
		`{"name":"nyaa-renamed","kind":"fake"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["rss"] != false || out["auto_search"] != true || out["interactive_search"] != true {
		t.Errorf("no-toggle update must preserve stored toggles: %v", out)
	}
}
