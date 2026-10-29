package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/indexers"
	indexerdefinitions "github.com/brandenk514/mediarr/internal/indexers/definitions"
)

// fakeDefStore is an in-memory definitions.Store over the embedded catalog:
// the API contract tests exercise the endpoint shape and filters without
// Postgres (the Postgres repo is covered by its own integration tests).
type fakeDefStore struct {
	cat *indexerdefinitions.Catalog
}

func newFakeDefStore(t *testing.T) *fakeDefStore {
	t.Helper()
	cat, err := indexerdefinitions.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return &fakeDefStore{cat: cat}
}

func (s *fakeDefStore) List(ctx context.Context) ([]indexerdefinitions.Definition, error) {
	defs := append([]indexerdefinitions.Definition(nil), s.cat.Definitions...)
	indexerdefinitions.SortByDisplay(defs)
	return defs, nil
}

func (s *fakeDefStore) Get(ctx context.Context, id string) (*indexerdefinitions.Definition, error) {
	d := s.cat.GetByID(id)
	if d == nil {
		return nil, errors.New("no such definition")
	}
	return d, nil
}

// newIndexerDefsTestServer builds a FRESH authenticated server with the
// definition-library endpoint and the write endpoints wired (a fresh server
// per test: the mux panics on double-registering a route).
func newIndexerDefsTestServer(t *testing.T) (*Server, *fakeWriteIdxRepo, string) {
	t.Helper()
	repo := &fakeWriteIdxRepo{}

	store := newFakeStore()
	u, _ := store.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, err := auth.NewToken("ma_live_")
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	_, _ = store.CreateToken(context.Background(), u, "test", pair, nil)

	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	s.SetIndexers(&IndexersDeps{
		Repo:        repo,
		Admin:       indexers.NewDefaultRegistry(),
		Definitions: newFakeDefStore(t),
	})
	return s, repo, pair.Raw
}

// doGet runs a GET with query string through the server.
func doGet(t *testing.T, s *Server, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// TestIndexerDefinitions_RequiresAuth: the library endpoint is behind the
// same auth as the rest of the API.
func TestIndexerDefinitions_RequiresAuth(t *testing.T) {
	s, _, _ := newIndexerDefsTestServer(t)
	rec := doGet(t, s, "/api/v1/indexer/definitions", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestIndexerDefinitions_List: the full library is served with the response
// shape the Add/Edit Indexer modal renders.
func TestIndexerDefinitions_List(t *testing.T) {
	s, _, token := newIndexerDefsTestServer(t)
	rec := doGet(t, s, "/api/v1/indexer/definitions", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var items []definitionJSON
	if err := json.NewDecoder(rec.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) < 500 {
		t.Fatalf("got %d definitions, want at least 500", len(items))
	}
	byID := map[string]definitionJSON{}
	for _, it := range items {
		byID[it.ID] = it
		if it.Presets == nil || len(it.Presets) != 0 {
			t.Errorf("%s: presets = %v, want [] (placeholder for now)", it.ID, it.Presets)
		}
		if it.Fields == nil || it.Categories == nil || it.URLs == nil || it.LegacyURLs == nil {
			t.Errorf("%s: a nil slice leaked into the response", it.ID)
		}
	}
	// Well-known community + built-in entries.
	x, ok := byID["1337x"]
	if !ok || x.Protocol != "torrent" {
		t.Errorf("1337x missing or wrong protocol: %+v", x)
	}
	gn, ok := byID["generic-newznab"]
	if !ok || gn.Protocol != "usenet" || len(gn.Fields) != 2 {
		t.Errorf("generic-newznab missing or wrong: %+v", gn)
	}
	gt, ok := byID["generic-torznab"]
	if !ok || gt.Protocol != "torrent" {
		t.Errorf("generic-torznab missing or wrong: %+v", gt)
	}
	// Display-name ordering: first item should sort before the rest.
	for i := 1; i < len(items); i++ {
		if strings.ToLower(items[i-1].Name) > strings.ToLower(items[i].Name) {
			t.Fatalf("not sorted by display name at %d: %q > %q", i, items[i-1].Name, items[i].Name)
		}
	}
}

// TestIndexerDefinitions_Filters: q / protocol / privacy / category, alone
// and combined.
func TestIndexerDefinitions_Filters(t *testing.T) {
	s, _, token := newIndexerDefsTestServer(t)

	list := func(query string) []definitionJSON {
		t.Helper()
		rec := doGet(t, s, "/api/v1/indexer/definitions?"+query, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", query, rec.Code)
		}
		var items []definitionJSON
		if err := json.NewDecoder(rec.Body).Decode(&items); err != nil {
			t.Fatalf("%s: decode: %v", query, err)
		}
		return items
	}

	t.Run("protocol", func(t *testing.T) {
		for _, it := range list("protocol=usenet") {
			if it.Protocol != "usenet" {
				t.Errorf("protocol filter leaked %q (%s)", it.Protocol, it.ID)
			}
		}
	})
	t.Run("q", func(t *testing.T) {
		items := list("q=1337")
		if len(items) < 1 {
			t.Fatal("q=1337 matched nothing")
		}
		for _, it := range items {
			if !strings.Contains(strings.ToLower(it.Name), "1337") && !strings.Contains(strings.ToLower(it.ID), "1337") {
				t.Errorf("q=1337 leaked %q", it.Name)
			}
		}
	})
	t.Run("privacy", func(t *testing.T) {
		for _, it := range list("privacy=private") {
			if it.Privacy != "private" {
				t.Errorf("privacy filter leaked %q (%s)", it.Privacy, it.ID)
			}
		}
	})
	t.Run("category", func(t *testing.T) {
		for _, it := range list("category=TV/Anime") {
			found := false
			for _, c := range it.Categories {
				if strings.EqualFold(c, "TV/Anime") {
					found = true
				}
			}
			if !found {
				t.Errorf("category filter leaked %q (%v)", it.ID, it.Categories)
			}
		}
	})
	t.Run("combined", func(t *testing.T) {
		items := list("protocol=torrent&privacy=public&q=1337")
		for _, it := range items {
			if it.Protocol != "torrent" || it.Privacy != "public" {
				t.Errorf("combined filter leaked %+v", it)
			}
		}
	})
}

// TestIndexerCreate_ConfigFields: the create happy path (contract values
// validated + normalized + secrets masked in the response) and the 400
// contract violations.
func TestIndexerCreate_ConfigFields(t *testing.T) {
	t.Run("valid create masks secrets and mirrors base_url", func(t *testing.T) {
		s, repo, token := newIndexerDefsTestServer(t)
		body := `{"name":"My NB","kind":"fake","base_url":"https://nb.example",` +
			`"definition_name":"generic-newznab","config_fields":{"apikey":"top-secret-1"}}`
		rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/indexers", token, body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
		}
		var out indexerJSON
		if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if out.ConfigFields["baseurl"] != "https://nb.example" {
			t.Errorf("baseurl = %v, want the form's base_url", out.ConfigFields["baseurl"])
		}
		if out.ConfigFields["apikey"] != "" {
			t.Errorf("apikey was echoed: %q", out.ConfigFields["apikey"])
		}
		if out.DefinitionName == nil || *out.DefinitionName != "generic-newznab" {
			t.Errorf("definition_name = %v, want generic-newznab", out.DefinitionName)
		}
		// The stored (plaintext) value is intact for the repo layer.
		def, err := repo.Get(context.Background(), out.ID)
		if err != nil {
			t.Fatalf("repo.Get: %v", err)
		}
		if def.ConfigFields["apikey"] != "top-secret-1" {
			t.Errorf("stored apikey = %v, want the plaintext round-trip", def.ConfigFields["apikey"])
		}
	})

	t.Run("valid create with display name normalizes to id", func(t *testing.T) {
		s, _, token := newIndexerDefsTestServer(t)
		body := `{"name":"My NB2","kind":"fake","base_url":"https://nb2.example",` +
			`"definition_name":"Generic Newznab","config_fields":{"apikey":"k"}}`
		rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/indexers", token, body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
		}
		var out indexerJSON
		_ = json.NewDecoder(rec.Body).Decode(&out)
		if out.DefinitionName == nil || *out.DefinitionName != "generic-newznab" {
			t.Errorf("definition_name = %v, want normalized id", out.DefinitionName)
		}
	})

	for _, tc := range []struct {
		name string
		body string
		want string // substring of the 400 message ("" = any 400)
	}{
		{
			name: "unknown definition",
			body: `{"name":"X","kind":"fake","base_url":"https://x.example","definition_name":"no-such-site","config_fields":{"baseurl":"https://x.example"}}`,
			want: "unknown definition",
		},
		{
			name: "missing definition name",
			body: `{"name":"X","kind":"fake","base_url":"https://x.example","config_fields":{"baseurl":"https://x.example"}}`,
			want: "definition_name is required",
		},
		{
			name: "missing required baseurl",
			body: `{"name":"X","kind":"fake","definition_name":"generic-newznab","config_fields":{"apikey":"k"}}`,
			want: "baseurl",
		},
		{
			name: "unknown field name",
			body: `{"name":"X","kind":"fake","definition_name":"1337x","config_fields":{"bogus":"1"}}`,
			want: "unknown field",
		},
		{
			name: "select out of options",
			body: `{"name":"X","kind":"fake","definition_name":"1337x","config_fields":{"primarydownloadlink":"ftp://nope"}}`,
			want: "not a valid option",
		},
		{
			name: "info field is not an input",
			body: `{"name":"X","kind":"fake","definition_name":"1337x","config_fields":{"info_uploader":"x"}}`,
			want: "not an input",
		},
		{
			name: "checkbox must be boolean",
			body: `{"name":"X","kind":"fake","definition_name":"0dayfiles-api","config_fields":{"freeleech":"maybe"}}`,
			want: "boolean",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, token := newIndexerDefsTestServer(t)
			rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/indexers", token, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body %s, want 400", rec.Code, rec.Body.String())
			}
			if tc.want != "" && !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("message %q does not contain %q", rec.Body.String(), tc.want)
			}
		})
	}
}

// TestIndexerUpdate_ConfigFields: the masked-edit round-trip (empty password
// keeps the stored value), definition-name fallback, and a legacy PUT that
// omits config_fields entirely (stored contract must survive).
func TestIndexerUpdate_ConfigFields(t *testing.T) {
	s, repo, token := newIndexerDefsTestServer(t)

	body := `{"name":"My NB","kind":"fake","base_url":"https://nb.example",` +
		`"definition_name":"generic-newznab","config_fields":{"apikey":"stored-secret"}}`
	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/indexers", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup create: %d %s", rec.Code, rec.Body.String())
	}
	var created indexerJSON
	_ = json.NewDecoder(rec.Body).Decode(&created)

	// Masked edit: empty apikey + a new base_url, definition_name omitted.
	putBody := `{"name":"My NB","kind":"fake","base_url":"https://nb2.example","config_fields":{"apikey":""}}`
	rec = doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/indexers/"+strconv.FormatInt(created.ID, 10), token, putBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	var updated indexerJSON
	_ = json.NewDecoder(rec.Body).Decode(&updated)
	if updated.ConfigFields["baseurl"] != "https://nb2.example" {
		t.Errorf("baseurl = %v, want https://nb2.example", updated.ConfigFields["baseurl"])
	}
	if updated.DefinitionName == nil || *updated.DefinitionName != "generic-newznab" {
		t.Errorf("definition_name = %v, want fallback to the stored name", updated.DefinitionName)
	}
	def, err := repo.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("repo.Get: %v", err)
	}
	if def.ConfigFields["apikey"] != "stored-secret" {
		t.Errorf("masked edit clobbered the stored secret: %v", def.ConfigFields["apikey"])
	}

	// A new secret replaces it.
	putBody = `{"name":"My NB","kind":"fake","base_url":"https://nb2.example","config_fields":{"apikey":"new-secret"}}`
	rec = doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/indexers/"+strconv.FormatInt(created.ID, 10), token, putBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("update 2: %d %s", rec.Code, rec.Body.String())
	}
	def, _ = repo.Get(context.Background(), created.ID)
	if def.ConfigFields["apikey"] != "new-secret" {
		t.Errorf("new secret not stored: %v", def.ConfigFields["apikey"])
	}

	// Legacy PUT without config_fields: the contract must survive untouched.
	putBody = `{"name":"My NB","kind":"fake","base_url":"https://nb2.example"}`
	rec = doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/indexers/"+strconv.FormatInt(created.ID, 10), token, putBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy update: %d %s", rec.Code, rec.Body.String())
	}
	def, _ = repo.Get(context.Background(), created.ID)
	if def.ConfigFields == nil || def.ConfigFields["apikey"] != "new-secret" {
		t.Errorf("legacy update wiped the stored contract: %v", def.ConfigFields)
	}
	if def.DefinitionName != "generic-newznab" {
		t.Errorf("legacy update dropped definition_name: %q", def.DefinitionName)
	}
}

// TestIndexerList_ConfigFields: the list endpoint masks secrets and exposes
// the contract shape ({} + null definition_name on the legacy path).
func TestIndexerList_ConfigFields(t *testing.T) {
	s, _, token := newIndexerDefsTestServer(t)
	body := `{"name":"My NB","kind":"fake","base_url":"https://nb.example",` +
		`"definition_name":"generic-newznab","config_fields":{"apikey":"list-secret"}}`
	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/indexers", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body.String())
	}
	// A legacy indexer alongside.
	rec = doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/indexers", token,
		`{"name":"Legacy","kind":"fake","base_url":"https://l.example"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup legacy: %d %s", rec.Code, rec.Body.String())
	}

	rec = doGet(t, s, "/api/v1/indexers", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	var items []indexerJSON
	if err := json.NewDecoder(rec.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byName := map[string]indexerJSON{}
	for _, it := range items {
		byName[it.Name] = it
	}
	nb := byName["My NB"]
	if nb.ConfigFields["apikey"] != "" {
		t.Errorf("list echoed the secret: %q", nb.ConfigFields["apikey"])
	}
	if nb.ConfigFields["baseurl"] != "https://nb.example" {
		t.Errorf("list baseurl = %v", nb.ConfigFields["baseurl"])
	}
	if nb.DefinitionName == nil || *nb.DefinitionName != "generic-newznab" {
		t.Errorf("list definition_name = %v", nb.DefinitionName)
	}
	leg := byName["Legacy"]
	if leg.ConfigFields == nil || len(leg.ConfigFields) != 0 {
		t.Errorf("legacy config_fields = %v, want {}", leg.ConfigFields)
	}
	if leg.DefinitionName != nil {
		t.Errorf("legacy definition_name = %v, want null", *leg.DefinitionName)
	}
}
