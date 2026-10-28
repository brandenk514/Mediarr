package e2e_test

// Temporary L4 spec-shape check (no Docker needed): loads openapi.yaml with
// the same kin-openapi loader the drift guard uses and asserts the L4
// additions resolve. Deleted after the L4 backend lands.

import (
	"os"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestL4_OpenAPISpecLoads(t *testing.T) {
	data, err := os.ReadFile("../../openapi.yaml")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(data)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	if err := doc.Validate(loader.Context); err != nil {
		t.Fatalf("validate spec: %v", err)
	}
	for _, p := range []string{"/api/v1/music/import", "/api/v1/music/artists/{id}/refresh"} {
		if doc.Paths.Find(p) == nil {
			t.Errorf("missing path %s", p)
		}
	}
	for _, s := range []string{"AlbumAdded", "RefreshArtistResponse", "MusicImportReport", "MusicImportCandidate"} {
		if _, ok := doc.Components.Schemas[s]; !ok {
			t.Errorf("missing schema %s", s)
		}
	}
	// The add-album 201 must reference AlbumAdded, and the schemas must carry the L4 fields.
	aa := doc.Components.Schemas["AlbumAdded"].Value
	if _, ok := aa.Properties["warnings"]; !ok {
		t.Error("AlbumAdded missing warnings")
	}
	album := doc.Components.Schemas["Album"].Value
	for _, f := range []string{"Cover", "Labels", "Genres"} {
		if _, ok := album.Properties[f]; !ok {
			t.Errorf("Album missing %s", f)
		}
	}
	artist := doc.Components.Schemas["Artist"].Value
	if _, ok := artist.Properties["Cover"]; !ok {
		t.Error("Artist missing Cover")
	}
}
