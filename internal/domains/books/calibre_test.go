package books

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// B5: Calibre Content Server base-URL construction (mirrors Readarr's
// HttpRequestBuilder.BuildBaseUrl).
func TestCalibre_BaseURL(t *testing.T) {
	cases := []struct {
		in   CalibreCredentials
		want string
	}{
		{CalibreCredentials{Host: "127.0.0.1", Port: 8080, UseSSL: false}, "http://127.0.0.1:8080"},
		{CalibreCredentials{Host: "localhost", Port: 8443, UseSSL: true}, "https://localhost:8443"},
		{CalibreCredentials{Host: "h", Port: 8080, URLBase: "calibre"}, "http://h:8080/calibre"},
		{CalibreCredentials{Host: "h", Port: 8080, URLBase: "/calibre/"}, "http://h:8080/calibre"},
		{CalibreCredentials{Host: "h", Port: 8080, URLBase: "  "}, "http://h:8080"},
	}
	for i, c := range cases {
		if got := c.in.BaseURL(); got != c.want {
			t.Errorf("case %d: BaseURL = %q, want %q", i, got, c.want)
		}
	}
}

func TestCalibre_ExtensionOf(t *testing.T) {
	cases := map[string]string{
		"book.epub":   ".epub",
		"/a/b/c.MOBI": ".MOBI",
		"noext":       "",
		".hidden":     "", // leading dot is not an extension
	}
	for in, want := range cases {
		if got := extensionOf(in); got != want {
			t.Errorf("extensionOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCalibre_ParseCalibreID(t *testing.T) {
	cases := []struct {
		in   string
		want int
		err  bool
	}{
		{`{"id":123,"success":true}`, 123, false},
		{`{"id" : 42 }`, 42, false},
		{`{"id":0}`, 0, false},
		{`{"success":true}`, 0, true},
	}
	for i, c := range cases {
		got, err := parseCalibreID([]byte(c.in))
		if c.err != (err != nil) {
			t.Errorf("case %d: err = %v, want err %v", i, err, c.err)
			continue
		}
		if !c.err && got != c.want {
			t.Errorf("case %d: id = %d, want %d", i, got, c.want)
		}
	}
}

// B5: the fake Content Server records every call — the seam the import
// pipeline uses in tests (no real HTTP).
type fakeContentServer struct {
	tested    bool
	testErr   error
	added     []BookFileRef
	addID     int
	addErr    error
	deleted   [][]int
	deleteErr error
}

func (f *fakeContentServer) Test(_ context.Context, _ CalibreCredentials) error {
	f.tested = true
	return f.testErr
}
func (f *fakeContentServer) AddBook(_ context.Context, _ CalibreCredentials, ref BookFileRef) (int, error) {
	f.added = append(f.added, ref)
	f.addID++
	return f.addID, f.addErr
}
func (f *fakeContentServer) DeleteBooks(_ context.Context, _ CalibreCredentials, ids []int) error {
	f.deleted = append(f.deleted, ids)
	return f.deleteErr
}

func TestCalibre_FakeContentServer(t *testing.T) {
	var cs ContentServer = &fakeContentServer{}
	ctx := context.Background()
	cred := CalibreCredentials{Host: "h", Port: 1, Library: "MyBooks"}

	if err := cs.Test(ctx, cred); err != nil {
		t.Fatalf("Test: %v", err)
	}
	id, err := cs.AddBook(ctx, cred, BookFileRef{Path: "/x/Book.epub", Title: "Book"})
	if err != nil {
		t.Fatalf("AddBook: %v", err)
	}
	if id != 1 {
		t.Errorf("AddBook id = %d, want 1", id)
	}
	if err := cs.DeleteBooks(ctx, cred, []int{id}); err != nil {
		t.Fatalf("DeleteBooks: %v", err)
	}

	f := cs.(*fakeContentServer)
	if !f.tested || len(f.added) != 1 || len(f.deleted) != 1 {
		t.Errorf("fake did not record all calls: tested=%v added=%d deleted=%d", f.tested, len(f.added), len(f.deleted))
	}
}

// helper: point a CalibreCredentials at an httptest server.
func credFor(ts *httptest.Server) CalibreCredentials {
	u, _ := url.Parse(ts.URL)
	cred := CalibreCredentials{Host: u.Hostname(), Port: portOf(u), UseSSL: false, Library: "MyBooks", Username: "u"}
	return cred
}

func portOf(u *url.URL) int {
	p := 0
	if s := u.Port(); s != "" {
		for _, r := range s {
			p = p*10 + int(r-'0')
		}
	}
	return p
}

func TestCalibre_ClientTest(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr string // empty = success
	}{
		{"calibre", `<html><head><title>calibre</title></head></html>`, ""},
		{"gui", `<html><head><title>guac-login</title></head></html>`, "remote Calibre GUI"},
		{"calibreweb", `<html><head><title>Calibre-Web</title></head></html>`, "Calibre-Web"},
		{"notcalibre", `<html><head><title>other</title></head></html>`, "not a valid Calibre content server"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				// base GET
				_, _ = w.Write([]byte(c.body))
			})
			// saved_searches probe (write access)
			mux.HandleFunc("/cdb/cmd/saved_searches", func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{}`))
			})
			ts := httptest.NewServer(mux)
			defer ts.Close()

			cl := NewContentServer(http.DefaultClient)
			err := cl.Test(context.Background(), credFor(ts))
			if c.wantErr == "" {
				if err != nil {
					t.Errorf("want success, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("want error containing %q, got %v", c.wantErr, err)
			}
		})
	}
}

func TestCalibre_ClientAddBookAndDelete(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cdb/add-book/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":77,"success":true}`))
	})
	mux.HandleFunc("/cdb/delete-books/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("delete-books must be POST, got %s", r.Method)
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	cred := credFor(ts)
	cl := NewContentServer(http.DefaultClient)

	id, err := cl.AddBook(context.Background(), cred, BookFileRef{Path: "/lib/Book.epub", Title: "Book"})
	if err != nil {
		t.Fatalf("AddBook: %v", err)
	}
	if id != 77 {
		t.Errorf("AddBook id = %d, want 77", id)
	}
	if err := cl.DeleteBooks(context.Background(), cred, []int{77}); err != nil {
		t.Fatalf("DeleteBooks: %v", err)
	}
}

func TestCalibre_ClientDeleteNoop(t *testing.T) {
	// No server needed: an empty id list is a no-op (no HTTP call).
	cl := NewContentServer(http.DefaultClient)
	if err := cl.DeleteBooks(context.Background(), CalibreCredentials{Host: "h", Port: 1}, nil); err != nil {
		t.Errorf("empty delete should be a no-op, got %v", err)
	}
}
