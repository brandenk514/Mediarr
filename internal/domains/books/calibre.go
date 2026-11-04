// Package books (Readarr B5) — Calibre Content Server client.
//
// The Calibre integration is Readarr's signature, feature-flagged (off by
// default). This file is the clean, testable seam for talking to a Calibre
// Content Server: the ContentServer interface, a real HTTP Client
// (secondary — full external I/O is behind the interface so it is swappable
// and testable), and a BookFileRef value type. The config surface itself
// (CalibreSettings) lives in the settings package; the per-root password is
// opened from the vault by the caller before it reaches this package (the
// notify.BuildSink convention: this package never touches the vault and never
// logs a credential).
package books

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// CalibreCredentials are the opened (already-decrypted) Content Server
// credentials used to build and authenticate requests. The service opens the
// vault-encrypted per-root password before calling the client; this package
// treats the password as opaque and never logs it.
type CalibreCredentials struct {
	Host     string
	Port     int
	URLBase  string
	Username string
	Password string
	UseSSL   bool
	Library  string
}

// BaseURL builds the Content Server base URL from the credentials
// (scheme://host:port[/url_base]), mirroring Readarr's
// HttpRequestBuilder.BuildBaseUrl(UseSsl, Host, Port, UrlBase).
func (c CalibreCredentials) BaseURL() string {
	scheme := "http"
	if c.UseSSL {
		scheme = "https"
	}
	base := fmt.Sprintf("%s://%s:%d", scheme, c.Host, c.Port)
	if b := strings.TrimSpace(c.URLBase); b != "" {
		base = strings.TrimRight(base, "/") + "/" + strings.Trim(b, "/")
	}
	return base
}

// BookFileRef is the minimal book file identity the Content Server needs to
// add / manage a book in a library.
type BookFileRef struct {
	// Path is the local path of the book file to upload.
	Path string
	// Title / Author are the book's metadata (used for a display name).
	Title  string
	Author string
	// CalibreID is the book's id in the Content Server (0 = not yet added).
	CalibreID int
}

// ContentServer is the Calibre Content Server client contract (B5). The full
// external HTTP I/O is secondary to the config surface, so the interface is
// intentionally narrow and swappable: Test (reachability + "is this really a
// calibre content server?" + write access), AddBook (upload a book file to a
// library), and DeleteBooks (remove books from the library). Implementations:
// Client (real HTTP) and the test fake.
type ContentServer interface {
	// Test verifies the Content Server is reachable, is a calibre content
	// server (not the Calibre GUI or a Calibre-Web instance), and that the
	// credentials have write access. It returns nil on success, or a
	// human-readable reason on failure.
	Test(ctx context.Context, c CalibreCredentials) error
	// AddBook uploads the book file at ref.Path into the library and returns
	// the assigned Calibre book id. A 0 id (with no error) means the server
	// rejected a duplicate.
	AddBook(ctx context.Context, c CalibreCredentials, ref BookFileRef) (int, error)
	// DeleteBooks removes the given Calibre book ids from the library.
	DeleteBooks(ctx context.Context, c CalibreCredentials, ids []int) error
}

// Client is the real (secondary) HTTP Content Server implementation. It
// targets the Calibre Content Server REST surface (the calibre-ebook
// "server.html" API): cdb/* for book operations, ajax/* for reads.
type Client struct {
	// HTTP is the underlying client; nil uses http.DefaultClient.
	HTTP *http.Client
}

// NewContentServer builds a real HTTP Content Server client.
func NewContentServer(hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{HTTP: hc}
}

var _ ContentServer = (*Client)(nil)

// do performs an authenticated request against the Content Server and returns
// the response body (drained and closed). A non-2xx status is an error carrying
// the status code; the body is included (truncated) for diagnosability.
func (cl *Client) do(ctx context.Context, c CalibreCredentials, method, path string, body io.Reader, contentType string) ([]byte, error) {
	raw := c.BaseURL() + "/" + strings.Trim(path, "/")
	req, err := http.NewRequestWithContext(ctx, method, raw, body)
	if err != nil {
		return nil, fmt.Errorf("calibre: build request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	if c.Username != "" {
		// The password is never logged.
		req.SetBasicAuth(c.Username, c.Password)
	}
	resp, err := cl.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calibre: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return data, fmt.Errorf("calibre: %s %s: status %d", method, path, resp.StatusCode)
	}
	return data, nil
}

// Test implements ContentServer.Test. It mirrors Readarr's TestCalibre: a GET
// of the base URL that must be a calibre content server (the
// "<title>calibre</title>" marker; not the guac remote-GUI or Calibre-Web) and
// the credentials must have write access (the saved_searches probe).
func (cl *Client) Test(ctx context.Context, c CalibreCredentials) error {
	body, err := cl.do(ctx, c, http.MethodGet, "", nil, "")
	if err != nil {
		if strings.Contains(err.Error(), "status 404") {
			return fmt.Errorf("calibre: could not connect (host/port wrong?)")
		}
		return fmt.Errorf("calibre: unable to connect: %w", err)
	}
	s := string(body)
	if strings.Contains(s, "guac-login") {
		return fmt.Errorf("calibre: bad port — this is the container's remote Calibre GUI, not the content server (try mapping port 8081)")
	}
	if strings.Contains(s, "Calibre-Web") {
		return fmt.Errorf("calibre: this is a Calibre-Web server, not the required Calibre content server")
	}
	if !strings.Contains(s, "<title>calibre</title>") {
		return fmt.Errorf("calibre: not a valid Calibre content server")
	}
	if err := cl.hasWriteAccess(ctx, c); err != nil {
		return fmt.Errorf("calibre: needs write access — %w", err)
	}
	return nil
}

// hasWriteAccess probes the saved_searches command (a read-only "list"); a
// 403 means the credentials are read-only.
func (cl *Client) hasWriteAccess(ctx context.Context, c CalibreCredentials) error {
	_, err := cl.do(ctx, c, http.MethodPost, "cdb/cmd/saved_searches",
		strings.NewReader(`["list"]`), "application/json")
	if err != nil && strings.Contains(err.Error(), "status 403") {
		return fmt.Errorf("read-only account (configure a user or trusted IP in calibre)")
	}
	return nil
}

// AddBook implements ContentServer.AddBook. It POSTs the book file's bytes to
// cdb/add-book/{jobid}/{addDuplicates}/{filename}/{library} and returns the
// assigned Calibre book id (the "id" field of the JSON response).
func (cl *Client) AddBook(ctx context.Context, c CalibreCredentials, ref BookFileRef) (int, error) {
	jobID := jobIDFor(ref)
	filename := "$dummy" + extensionOf(ref.Path)
	path := fmt.Sprintf("cdb/add-book/%d/1/%s/%s", jobID, filename, url.PathEscape(c.Library))
	data, err := cl.do(ctx, c, http.MethodPost, path, strings.NewReader(""), "")
	if err != nil {
		return 0, fmt.Errorf("calibre: add book: %w", err)
	}
	// The response is a small JSON object with at least an "id" field.
	return parseCalibreID(data)
}

// DeleteBooks implements ContentServer.DeleteBooks. It POSTs the comma-joined
// ids to cdb/delete-books/{ids}/{library}.
func (cl *Client) DeleteBooks(ctx context.Context, c CalibreCredentials, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	var b strings.Builder
	for i, id := range ids {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(strconv.Itoa(id))
	}
	path := fmt.Sprintf("cdb/delete-books/%s/%s", b.String(), url.PathEscape(c.Library))
	if _, err := cl.do(ctx, c, http.MethodPost, path, strings.NewReader(""), ""); err != nil {
		return fmt.Errorf("calibre: delete books: %w", err)
	}
	return nil
}

// jobIDFor derives a stable Calibre add-book job id from the book identity
// (Readarr derives it from the file's last-modified ticks; here the path is
// the identity).
func jobIDFor(ref BookFileRef) int {
	var h uint32 = 2166136261 // FNV-1a
	for i := 0; i < len(ref.Path); i++ {
		h ^= uint32(ref.Path[i])
		h *= 16777619
	}
	return int(h % 1000000000)
}

// extensionOf returns the file extension (with leading dot) of path, or "" if
// it has none.
func extensionOf(path string) string {
	i := strings.LastIndex(path, "/")
	name := path
	if i >= 0 {
		name = path[i+1:]
	}
	if j := strings.LastIndex(name, "."); j > 0 {
		return name[j:]
	}
	return ""
}

// parseCalibreID extracts the numeric "id" from a Calibre JSON response
// (tolerant: it scans for `"id":N`).
func parseCalibreID(data []byte) (int, error) {
	s := string(data)
	key := `"id":`
	i := strings.Index(s, key)
	if i < 0 {
		// Some responses use `"id" : N` with spaces.
		key = `"id"`
		i = strings.Index(s, key)
		if i < 0 {
			return 0, fmt.Errorf("calibre: no book id in response")
		}
	}
	rest := s[i+len(key):]
	rest = strings.TrimLeft(rest, " :")
	n := 0
	for _, r := range rest {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}
