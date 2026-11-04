package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/brandenk514/mediarr/internal/auth"
	rfdom "github.com/brandenk514/mediarr/internal/domains/rootfolders"
	"github.com/brandenk514/mediarr/internal/health"
	rfsvc "github.com/brandenk514/mediarr/internal/services/rootfolders"
	"github.com/brandenk514/mediarr/internal/settings"
)

// b5FakeRootFolders is an in-memory rfdom.Repo for the B5 root-folder
// endpoint tests (it stores the domain structs so the tests can assert on
// what was actually persisted, e.g. the unmasked password).
type b5FakeRootFolders struct {
	folders map[int64]rfdom.RootFolder
	nextID  int64
}

func newB5FakeRootFolders() *b5FakeRootFolders {
	return &b5FakeRootFolders{folders: map[int64]rfdom.RootFolder{}, nextID: 1}
}

func (f *b5FakeRootFolders) List() ([]rfdom.RootFolder, error) {
	out := make([]rfdom.RootFolder, 0, len(f.folders))
	for i := 1; i < int(f.nextID); i++ {
		if rf, ok := f.folders[int64(i)]; ok {
			out = append(out, rf)
		}
	}
	return out, nil
}

func (f *b5FakeRootFolders) Get(id int64) (*rfdom.RootFolder, error) {
	rf, ok := f.folders[id]
	if !ok {
		return nil, errors.New("no row")
	}
	return &rf, nil
}

func (f *b5FakeRootFolders) Create(rf rfdom.RootFolder) (int64, error) {
	id := f.nextID
	f.nextID++
	rf.ID = id
	f.folders[id] = rf
	return id, nil
}

func (f *b5FakeRootFolders) Update(rf rfdom.RootFolder) error {
	if _, ok := f.folders[rf.ID]; !ok {
		return errors.New("no row")
	}
	f.folders[rf.ID] = rf
	return nil
}

func (f *b5FakeRootFolders) Delete(id int64) error {
	delete(f.folders, id)
	return nil
}

// newRootFoldersB5Server builds a fresh authenticated server wired with the
// root-folder registry endpoints over an in-memory registry.
func newRootFoldersB5Server(t *testing.T) (*Server, *b5FakeRootFolders, string) {
	t.Helper()
	store := newFakeStore()
	u, _ := store.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, err := auth.NewToken("ma_live_")
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	_, _ = store.CreateToken(context.Background(), u, "test", pair, nil)

	fake := newB5FakeRootFolders()
	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	s.SetRootFolders(&RootFolderDeps{Svc: rfsvc.New(fake)})
	return s, fake, pair.Raw
}

// wireRootFolder is the B5 wire shape of a root folder (the existing
// capitalized convention + the masked Calibre block + the hard-links rule).
type wireRootFolder struct {
	ID                int64                     `json:"ID"`
	Path              string                    `json:"Path"`
	Calibre           *settings.CalibreSettings `json:"Calibre"`
	HardlinksDisabled bool                      `json:"HardlinksDisabled"`
}

// B5/D: POST a root with an enabled Calibre block, then read it back: the
// block round-trips with a MASKED password (never echoed) and the
// hard-links rule is true.
func TestRootFoldersB5_PostAndMaskedList(t *testing.T) {
	s, fake, token := newRootFoldersB5Server(t)
	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/root-folders", token, `{
		"path": "/data/books",
		"calibre": {
			"enabled": true, "host": "calibre.example", "port": 8080,
			"username": "admin", "password": "the-secret", "library": "Library"
		}
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST: %d %s", rec.Code, rec.Body.String())
	}

	// The persisted block carries the plaintext password (the vault-free
	// dev path; the API never echoes it).
	stored := fake.folders[1]
	if stored.Calibre == nil || stored.Calibre.Password != "the-secret" || !stored.Calibre.Enabled {
		t.Fatalf("stored calibre = %+v, want the stored password", stored.Calibre)
	}

	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/root-folders", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body.String())
	}
	var out []wireRootFolder
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("list len = %d, want 1", len(out))
	}
	got := out[0]
	if got.Calibre == nil {
		t.Fatalf("wire calibre = nil, want the masked block: %s", rec.Body.String())
	}
	if got.Calibre.Password != "" {
		t.Fatalf("wire leaked the calibre password: %q", got.Calibre.Password)
	}
	if !got.Calibre.PasswordSet || got.Calibre.Host != "calibre.example" || got.Calibre.Port != 8080 || got.Calibre.Library != "Library" {
		t.Fatalf("wire calibre = %+v, want masked-but-present block", got.Calibre)
	}
	if !got.HardlinksDisabled {
		t.Fatal("enabled calibre must surface HardlinksDisabled=true")
	}
}

// B5/D: the masked-password round-trip on PUT — a blank incoming password
// keeps the stored key; a new incoming password replaces it; an omitted
// block keeps the stored one.
func TestRootFoldersB5_PutMaskedPasswordRoundTrip(t *testing.T) {
	s, fake, token := newRootFoldersB5Server(t)
	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/root-folders", token, `{
		"path": "/data/books",
		"calibre": {"enabled": true, "host": "h.example", "port": 8080,
			"username": "u", "password": "stored-pw"}
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST: %d %s", rec.Code, rec.Body.String())
	}

	// PUT a changed block with a BLANK password → the stored key is kept.
	rec = doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/root-folders/1", token, `{
		"path": "/data/books",
		"calibre": {"enabled": true, "host": "h.example", "port": 8081,
			"username": "u", "password": ""}
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "stored-pw") {
		t.Fatalf("PUT response leaked the stored password: %s", rec.Body.String())
	}
	stored := fake.folders[1]
	if stored.Calibre.Password != "stored-pw" || stored.Calibre.Port != 8081 {
		t.Fatalf("after blank-password PUT, stored = %+v, want the stored key kept + port replaced", stored.Calibre)
	}

	// A NEW password replaces the stored one.
	rec = doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/root-folders/1", token, `{
		"calibre": {"enabled": true, "host": "h.example", "port": 8081,
			"username": "u", "password": "new-pw"}
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT new password: %d %s", rec.Code, rec.Body.String())
	}
	if stored = fake.folders[1]; stored.Calibre.Password != "new-pw" {
		t.Fatalf("stored password = %q, want new-pw", stored.Calibre.Password)
	}

	// An OMITTED block keeps the stored one.
	rec = doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/root-folders/1", token, `{"path": "/data/books", "name": "Books"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT omitted block: %d %s", rec.Code, rec.Body.String())
	}
	if stored = fake.folders[1]; stored.Calibre == nil || stored.Calibre.Password != "new-pw" {
		t.Fatalf("stored = %+v, want the kept block", stored.Calibre)
	}
}

// B5/D: an enabled Calibre block missing its required fields is rejected
// (400) by the endpoint validation.
func TestRootFoldersB5_PostInvalidCalibre(t *testing.T) {
	s, _, token := newRootFoldersB5Server(t)
	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/root-folders", token, `{
		"path": "/data/books",
		"calibre": {"enabled": true, "port": 8080, "username": "u"}
	}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("enabled calibre without host: got %d, want 400: %s", rec.Code, rec.Body.String())
	}

	// A bad output format is rejected too.
	rec = doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/root-folders", token, `{
		"path": "/data/books2",
		"calibre": {"enabled": true, "host": "h", "port": 8080,
			"username": "u", "output_format": "PDF"}
	}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad output format: got %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// B5/D: a root without any Calibre block round-trips with a null block and
// HardlinksDisabled=false (no rule applies).
func TestRootFoldersB5_NoCalibreRoot(t *testing.T) {
	s, _, token := newRootFoldersB5Server(t)
	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/root-folders", token, `{"path": "/plain"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST: %d %s", rec.Code, rec.Body.String())
	}
	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/root-folders", token)
	var out []wireRootFolder
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Calibre != nil || out[0].HardlinksDisabled {
		t.Fatalf("plain root = %+v, want null calibre + hard links on", out)
	}
}
