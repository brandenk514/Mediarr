package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/brandenk514/mediarr/internal/indexers"
	indexerdefinitions "github.com/brandenk514/mediarr/internal/indexers/definitions"
	"github.com/brandenk514/mediarr/internal/secrets"
)

func newTestVault(t *testing.T) *secrets.Vault {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("random key: %v", err)
	}
	v, err := secrets.NewVaultFromBytes(key)
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	return v
}

// TestIndexerDefinitions_SeedIfEmpty covers the boot seeding contract: the
// full embedded catalog lands on first run, re-seeding is a no-op, and
// query paths work.
func TestIndexerDefinitions_SeedIfEmpty(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()
	st := NewWithDB(db)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	repo := NewIndexerDefinitionsRepo(db)

	cat, err := indexerdefinitions.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	n, err := repo.SeedIfEmpty(ctx, cat.Definitions, cat.Revision)
	if err != nil {
		t.Fatalf("SeedIfEmpty: %v", err)
	}
	if n != len(cat.Definitions) {
		t.Fatalf("seeded %d, want %d", n, len(cat.Definitions))
	}

	// Idempotent: nothing to insert the second time.
	n2, err := repo.SeedIfEmpty(ctx, cat.Definitions, cat.Revision)
	if err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("re-seed inserted %d rows, want 0", n2)
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != len(cat.Definitions) {
		t.Fatalf("List returned %d rows, want %d", len(list), len(cat.Definitions))
	}

	got, err := repo.Get(ctx, "1337x")
	if err != nil {
		t.Fatalf("Get(1337x): %v", err)
	}
	if got.Protocol != "torrent" || got.Name != "1337x" {
		t.Errorf("1337x = %+v", got)
	}
	if len(got.Fields) == 0 {
		t.Error("1337x lost its fields through the DB round-trip")
	}
	if got.Raw == "" {
		t.Error("1337x lost its raw YAML through the DB round-trip")
	}

	gn, err := repo.Get(ctx, "generic-newznab")
	if err != nil {
		t.Fatalf("Get(generic-newznab): %v", err)
	}
	if gn.Protocol != "usenet" {
		t.Errorf("generic-newznab protocol = %q, want usenet", gn.Protocol)
	}

	if _, err := repo.Get(ctx, "no-such-site"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Get(no-such-site) = %v, want sql.ErrNoRows", err)
	}

	// Seed revision is recorded for PR6 staleness detection.
	var rev string
	if err := db.QueryRow(`SELECT seed_revision FROM indexer_definitions WHERE id = '1337x'`).Scan(&rev); err != nil {
		t.Fatalf("read seed_revision: %v", err)
	}
	if !strings.HasPrefix(rev, "prowlarr/indexers@") {
		t.Errorf("seed_revision = %q", rev)
	}
}

// TestIndexerConfigFields_EncryptRoundTrip proves the repo layer is the only
// ciphertext toucher: password values leave as plaintext, live encrypted in
// the column, and survive updates.
func TestIndexerConfigFields_EncryptRoundTrip(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()
	st := NewWithDB(db)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	repo := NewIndexersRepo(db, newTestVault(t))

	const secret = "s3cr3t-key"
	id, err := repo.Create(ctx, indexers.Definition{
		Name: "NB", Kind: "fake", BaseURL: "https://nb.example", Enabled: true,
		DefinitionName: "generic-newznab",
		ConfigFields:   map[string]any{"baseurl": "https://nb.example", "apikey": secret},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	def, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if def.ConfigFields["apikey"] != secret {
		t.Errorf("Get apikey = %q, want the plaintext %q", def.ConfigFields["apikey"], secret)
	}
	if def.ConfigFields["baseurl"] != "https://nb.example" {
		t.Errorf("Get baseurl = %v", def.ConfigFields["baseurl"])
	}
	if def.DefinitionName != "generic-newznab" {
		t.Errorf("Get definition_name = %q", def.DefinitionName)
	}

	// The raw column holds ciphertext, not the plaintext.
	var raw string
	if err := db.QueryRow(`SELECT config_fields_json FROM indexers WHERE id = $1`, id).Scan(&raw); err != nil {
		t.Fatalf("read raw: %v", err)
	}
	if raw == "{}" || strings.Contains(raw, secret) {
		t.Errorf("raw config_fields_json leaked plaintext: %s", raw)
	}
	var stored map[string]any
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		t.Fatalf("raw is not valid JSON: %v", err)
	}
	if s, _ := stored["apikey"].(string); s == "" || s == secret {
		t.Errorf("stored apikey = %q, want a distinct ciphertext", s)
	}

	// Update with a new secret.
	def.ConfigFields["apikey"] = "rotated-key"
	if err := repo.Update(ctx, *def); err != nil {
		t.Fatalf("Update: %v", err)
	}
	def2, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if def2.ConfigFields["apikey"] != "rotated-key" {
		t.Errorf("updated apikey = %q, want rotated-key", def2.ConfigFields["apikey"])
	}

	// Legacy indexer: no contract at all.
	id2, err := repo.Create(ctx, indexers.Definition{
		Name: "Legacy", Kind: "fake", BaseURL: "https://l.example", Enabled: true,
	})
	if err != nil {
		t.Fatalf("Create legacy: %v", err)
	}
	leg, err := repo.Get(ctx, id2)
	if err != nil {
		t.Fatalf("Get legacy: %v", err)
	}
	if leg.ConfigFields != nil {
		t.Errorf("legacy ConfigFields = %v, want nil", leg.ConfigFields)
	}
	if leg.DefinitionName != "" {
		t.Errorf("legacy DefinitionName = %q, want empty", leg.DefinitionName)
	}
}
