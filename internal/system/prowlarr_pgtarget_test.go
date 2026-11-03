package system

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/brandenk514/mediarr/internal/downloadclients"
	"github.com/brandenk514/mediarr/internal/indexers"
	"github.com/brandenk514/mediarr/internal/secrets"
	"github.com/brandenk514/mediarr/internal/settings"
)

// fakeProwlarrSettingsStore is an in-memory SettingsStore.
type fakeProwlarrSettingsStore struct {
	docs map[settings.Section][]byte
}

func newFakeProwlarrSettingsStore() *fakeProwlarrSettingsStore {
	return &fakeProwlarrSettingsStore{docs: map[settings.Section][]byte{}}
}

func (s *fakeProwlarrSettingsStore) GetSectionDocument(ctx context.Context, sec settings.Section) ([]byte, error) {
	if b, ok := s.docs[sec]; ok {
		return b, nil
	}
	return nil, nil
}

func (s *fakeProwlarrSettingsStore) PutSectionDocument(ctx context.Context, sec settings.Section, doc []byte) error {
	s.docs[sec] = doc
	return nil
}

// fakeProwlarrIndexerRepo implements ProwlarrIndexerStore and records the
// full Definition handed to Create (so the test can assert the target passed
// the PLAINTEXT credential through for store-side re-encryption).
type fakeProwlarrIndexerRepo struct {
	byName  map[string]int64
	created map[string]indexers.Definition
}

func newFakeProwlarrIndexerRepo() *fakeProwlarrIndexerRepo {
	return &fakeProwlarrIndexerRepo{byName: map[string]int64{}, created: map[string]indexers.Definition{}}
}

func (r *fakeProwlarrIndexerRepo) FindIndexer(ctx context.Context, name string) (int64, bool) {
	id, ok := r.byName[name]
	return id, ok
}

func (r *fakeProwlarrIndexerRepo) CreateIndexer(ctx context.Context, def indexers.Definition) (int64, error) {
	if id, ok := r.byName[def.Name]; ok {
		return id, errors.New("duplicate indexer name")
	}
	id := int64(len(r.byName) + 1)
	r.byName[def.Name] = id
	r.created[def.Name] = def
	return id, nil
}

// fakeProwlarrProxyRepo implements ProwlarrProxyStore.
type fakeProwlarrProxyRepo struct {
	byName  map[string]int64
	created map[string]indexers.Proxy
}

func newFakeProwlarrProxyRepo() *fakeProwlarrProxyRepo {
	return &fakeProwlarrProxyRepo{byName: map[string]int64{}, created: map[string]indexers.Proxy{}}
}

func (r *fakeProwlarrProxyRepo) FindProxy(ctx context.Context, name string) (int64, bool) {
	id, ok := r.byName[name]
	return id, ok
}

func (r *fakeProwlarrProxyRepo) CreateProxy(ctx context.Context, p indexers.Proxy) (int64, error) {
	id := int64(len(r.byName) + 1)
	r.byName[p.Name] = id
	r.created[p.Name] = p
	return id, nil
}

// newTestProwlarrTarget wires the fakes into a ProwlarrPostgresTarget.
func newTestProwlarrTarget() (*ProwlarrPostgresTarget, *fakeProwlarrSettingsStore, *fakeProwlarrIndexerRepo, *fakeProwlarrProxyRepo, *fakeDCRepo) {
	st := newFakeProwlarrSettingsStore()
	ix := newFakeProwlarrIndexerRepo()
	px := newFakeProwlarrProxyRepo()
	dc := &fakeDCRepo{}
	t := NewProwlarrPostgresTarget(st, ix, px)
	t.DCRepo = dc
	return t, st, ix, px, dc
}

// findDCRecord looks up a record in the in-memory repo by name.
func findDCRecord(repo *fakeDCRepo, name string) *downloadclients.Record {
	for i := range repo.recs {
		if repo.recs[i].Name == name {
			r := repo.recs[i]
			return &r
		}
	}
	return nil
}

// TestProwlarrTargetImportReencryptsCredentials: the target passes the
// source's PLAINTEXT credential through to the indexer store untouched
// (the store's vault re-encrypts it at rest) — and a round trip through a
// real AES-256-GCM vault proves the ciphertext at rest never contains the
// plaintext.
func TestProwlarrTargetImportReencryptsCredentials(t *testing.T) {
	tgt, _, ixRepo, _, _ := newTestProwlarrTarget()
	ctx := context.Background()

	const plain = "super-secret-api-key"
	_, action, err := tgt.EnsureIndexer(ctx, ProwlarrIndexerRef{
		Name: "1337x", Kind: "torznab", BaseURL: "https://1337x.to",
		APIKey:         plain,
		DefinitionName: "1337x",
		ConfigFields:   map[string]any{"baseurl": "https://1337x.to", "apikey": plain},
		Enable:         true,
		Priority:       25,
		Privacy:        "public",
		SyncLevel:      "standard",
	})
	if err != nil {
		t.Fatalf("EnsureIndexer: %v", err)
	}
	if action != ActionCreated {
		t.Fatalf("action = %q, want created", action)
	}
	def := ixRepo.created["1337x"]
	if def.APIKey != plain {
		t.Errorf("target must hand the PLAINTEXT key to the store (store re-encrypts), got %q", def.APIKey)
	}
	if def.ConfigFields["apikey"] != plain {
		t.Errorf("target must hand the PLAINTEXT config field to the store, got %v", def.ConfigFields)
	}
	if def.DefinitionName != "1337x" || def.Kind != "torznab" || def.Priority != 25 {
		t.Errorf("definition mapping = %+v", def)
	}

	// The store-side re-encryption round trip (the same vault type the
	// postgres indexer repo wraps): encrypt → ciphertext never contains the
	// plaintext; decrypt → back to the plaintext.
	key := make([]byte, 32)
	key[0] = 7 // fixed test key; not a secret
	vault, err := secrets.NewVaultFromBytes(key)
	if err != nil {
		t.Fatalf("vault: %v", err)
	}
	ct, err := vault.EncryptString(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if ct == plain || strings.Contains(ct, plain) {
		t.Errorf("ciphertext must not contain the plaintext: %q", ct)
	}
	rt, err := vault.DecryptString(ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if rt != plain {
		t.Errorf("round trip = %q, want %q", rt, plain)
	}
}

// TestProwlarrTargetIdempotent: re-importing the same entity reports
// exists and performs no second write.
func TestProwlarrTargetIdempotent(t *testing.T) {
	tgt, _, _, _, _ := newTestProwlarrTarget()
	ctx := context.Background()

	id1, a1, err := tgt.EnsureIndexer(ctx, ProwlarrIndexerRef{Name: "X", Kind: "torznab", APIKey: "k"})
	if err != nil || a1 != ActionCreated {
		t.Fatalf("first: %v %q", err, a1)
	}
	id2, a2, err := tgt.EnsureIndexer(ctx, ProwlarrIndexerRef{Name: "X", Kind: "torznab", APIKey: "k"})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if a2 != ActionExists || id2 != id1 {
		t.Fatalf("second run = %q id %d, want exists id %d", a2, id2, id1)
	}

	// Tag: create, then exists with the same label.
	_, ta, err := tgt.EnsureTag(ctx, ProwlarrTag{ID: 5, Label: "hd"})
	if err != nil || ta != ActionCreated {
		t.Fatalf("tag create: %v %q", err, ta)
	}
	_, ta2, err := tgt.EnsureTag(ctx, ProwlarrTag{ID: 5, Label: "hd"})
	if err != nil || ta2 != ActionExists {
		t.Fatalf("tag re-apply: %v %q", err, ta2)
	}

	// Proxy: create, then exists.
	_, pa, err := tgt.EnsureIndexerProxy(ctx, ProwlarrProxyRef{Name: "FS", URL: "http://fs:8191"})
	if err != nil || pa != ActionCreated {
		t.Fatalf("proxy create: %v %q", err, pa)
	}
	_, pa2, err := tgt.EnsureIndexerProxy(ctx, ProwlarrProxyRef{Name: "FS", URL: "http://fs:8191"})
	if err != nil || pa2 != ActionExists {
		t.Fatalf("proxy re-apply: %v %q", err, pa2)
	}
}

// TestProwlarrTargetDownloadClientUpsert: a new client is created; an
// identical re-apply is a no-op exists; a changed key is an update.
func TestProwlarrTargetDownloadClientUpsert(t *testing.T) {
	tgt, _, _, _, dcRepo := newTestProwlarrTarget()
	ctx := context.Background()

	ref := ProwlarrDCRef{
		Name: "qBittorrent", Kind: downloadclients.KindQB, Enabled: true, Position: 1,
		BaseURL: "qb.local:8080", Username: "u", APIKey: "k1", CompleteDir: "/downloads",
	}
	a, err := tgt.EnsureDownloadClient(ctx, ref)
	if err != nil || a != ActionCreated {
		t.Fatalf("create: %v %q", err, a)
	}
	a, err = tgt.EnsureDownloadClient(ctx, ref)
	if err != nil || a != ActionExists {
		t.Fatalf("re-apply: %v %q", err, a)
	}
	ref.APIKey = "k2"
	a, err = tgt.EnsureDownloadClient(ctx, ref)
	if err != nil || a != ActionUpdated {
		t.Fatalf("update: %v %q", err, a)
	}
	rec := findDCRecord(dcRepo, "qBittorrent")
	if rec == nil || rec.APIKey != "k2" || rec.BaseURL != "qb.local:8080" {
		t.Errorf("stored record = %+v", rec)
	}
}

// TestProwlarrTargetProxyCarriesCredential: the proxy password is passed
// through to the store (for store-side encryption), never transformed.
func TestProwlarrTargetProxyCarriesCredential(t *testing.T) {
	tgt, _, _, pxRepo, _ := newTestProwlarrTarget()
	_, _, err := tgt.EnsureIndexerProxy(context.Background(), ProwlarrProxyRef{
		Name: "FS", URL: "http://fs:8191", Username: "fsuser", Password: "fspass",
		Tags: []string{"auto"},
	})
	if err != nil {
		t.Fatalf("EnsureIndexerProxy: %v", err)
	}
	p := pxRepo.created["FS"]
	if p.Password != "fspass" || p.Username != "fsuser" || p.URL != "http://fs:8191" || len(p.Tags) != 1 {
		t.Errorf("proxy = %+v", p)
	}
}
