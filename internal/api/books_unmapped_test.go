package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/brandenk514/mediarr/internal/auth"
	booksdom "github.com/brandenk514/mediarr/internal/domains/books"
	"github.com/brandenk514/mediarr/internal/health"
	bookssvc "github.com/brandenk514/mediarr/internal/services/books"
)

// newBooksUnmappedServer builds a FRESH authenticated server (the
// mux-double-register guard: every endpoint test gets its own NewServer)
// with a books service backed by the in-memory kinds repo and a configured
// media root — the books twin of the music L5 unmapped harness.
func newBooksUnmappedServer(t *testing.T, repo *kindsBooksRepo, mediaRoot string) (*Server, string) {
	t.Helper()
	s := NewServer(health.NewChecker(nil, nil))
	store := newFakeStore()
	u, _ := store.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, err := auth.NewToken("ma_live_")
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	_, _ = store.CreateToken(context.Background(), u, "test", pair, nil)
	s.SetAuth(&AuthDeps{Repo: store})
	svc := bookssvc.New(bookssvc.Deps{Repo: repo, DefaultProfile: "Best", MediaRoot: mediaRoot})
	s.SetBooks(&BooksDeps{Svc: svc})
	return s, pair.Raw
}

// TestBooksUnmappedFiles is the B3 "Unmapped Files" endpoint contract: a
// read-only walk of the configured books media root that lists book files
// NOT mapped to a monitored author, with the Readarr columns (Path / Size /
// Date Added / Quality = the book format from the extension). Files under a
// folder named after a monitored author are mapped (excluded); files in any
// other folder, or loose directly under the root, are unmapped.
func TestBooksUnmappedFiles(t *testing.T) {
	root := t.TempDir()
	mappedDir := filepath.Join(root, "Ursula K. Le Guin")
	if err := os.MkdirAll(mappedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mappedDir, "the-dispossessed.epub"), []byte("epub-data"), 0o644); err != nil {
		t.Fatal(err)
	}
	unmappedDir := filepath.Join(root, "Other Author")
	if err := os.MkdirAll(unmappedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unmappedDir, "novel.mobi"), []byte("mobi-data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "loose.azw3"), []byte("azw3-data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("not a book"), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := newKindsBooksRepo()
	repo.authors[1] = booksdom.Author{ID: 1, Name: "Ursula K. Le Guin", Monitored: true}
	repo.authors[2] = booksdom.Author{ID: 2, Name: "Other Author", Monitored: false}

	s, tok := newBooksUnmappedServer(t, repo, root)

	rec := doJSONReq(t, s.Handler(), http.MethodGet, "/api/v1/books/unmapped", tok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET unmapped: got %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var files []struct {
		Path      string `json:"path"`
		Size      int64  `json:"size"`
		DateAdded string `json:"date_added"`
		Quality   string `json:"quality"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &files); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("want 2 unmapped files, got %d: %+v", len(files), files)
	}

	byBase := map[string]string{}
	for _, f := range files {
		if filepath.Base(f.Path) == "the-dispossessed.epub" {
			t.Errorf("mapped file (under monitored author) leaked into unmapped list: %s", f.Path)
		}
		if filepath.Base(f.Path) == "notes.txt" {
			t.Errorf("non-book file listed: %s", f.Path)
		}
		if f.Size == 0 {
			t.Errorf("file %s: size should be non-zero", f.Path)
		}
		if f.DateAdded == "" {
			t.Errorf("file %s: date_added should be set", f.Path)
		}
		byBase[filepath.Base(f.Path)] = f.Quality
	}
	if q, ok := byBase["novel.mobi"]; !ok || q != "MOBI" {
		t.Errorf("unmapped mobi: want quality MOBI, got %q (present=%v)", q, ok)
	}
	if q, ok := byBase["loose.azw3"]; !ok || q != "AZW3" {
		t.Errorf("unmapped loose azw3: want quality AZW3, got %q (present=%v)", q, ok)
	}
}

// TestBooksUnmappedFiles_RequiresAuth is the auth contract: the unmapped
// endpoint is 401 without a bearer token.
func TestBooksUnmappedFiles_RequiresAuth(t *testing.T) {
	repo := newKindsBooksRepo()
	s, _ := newBooksUnmappedServer(t, repo, t.TempDir())
	rec := doJSONReq(t, s.Handler(), http.MethodGet, "/api/v1/books/unmapped", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET unmapped without token: got %d, want 401", rec.Code)
	}
}

// TestBooksUnmappedFiles_EmptyRoot is the unconfigured contract: with no
// media root configured the endpoint answers 200 with an empty list (the
// music L5 twin).
func TestBooksUnmappedFiles_EmptyRoot(t *testing.T) {
	repo := newKindsBooksRepo()
	s, tok := newBooksUnmappedServer(t, repo, "")
	rec := doJSONReq(t, s.Handler(), http.MethodGet, "/api/v1/books/unmapped", tok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET unmapped (empty root): got %d, want 200", rec.Code)
	}
	var files []any
	if err := json.Unmarshal(rec.Body.Bytes(), &files); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("empty root: want empty list, got %+v", files)
	}
}

// TestBooksUnmappedFiles_MissingRoot is the filesystem-problem contract: a
// configured root that does not exist maps to a clean 404 (the music L5
// twin), and a root that is a file maps to 400.
func TestBooksUnmappedFiles_MissingRoot(t *testing.T) {
	repo := newKindsBooksRepo()
	s, tok := newBooksUnmappedServer(t, repo, filepath.Join(t.TempDir(), "does-not-exist"))
	rec := doJSONReq(t, s.Handler(), http.MethodGet, "/api/v1/books/unmapped", tok, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET unmapped (missing root): got %d, want 404", rec.Code)
	}

	fileRoot := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(fileRoot, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s2, tok2 := newBooksUnmappedServer(t, newKindsBooksRepo(), fileRoot)
	rec = doJSONReq(t, s2.Handler(), http.MethodGet, "/api/v1/books/unmapped", tok2, "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("GET unmapped (root is a file): got %d, want 400", rec.Code)
	}
}
