// Package providers is the metadata-provider layer (PLAN §12: providers/ —
// tmdb, tvdb, musicbrainz, openlibrary). Each adapter talks to one external
// media-metadata API and maps its response into the shared Result shape the
// add-flow search endpoints expose (issue #42).
//
// Dependency rule (mirrors indexers/ and downloads/): these are swappable
// outer adapters. Domain code never calls a provider directly; the API layer
// does, and everything is testable against in-process httptest servers with
// recorded-fixture-style JSON (PLAN §14: "adapter layer + integration tests
// against recorded fixtures; no direct calls in domain code").
//
// Auth keys and base URLs come from config (issue #42): TMDB and TVDB take an
// optional API key; MusicBrainz and Open Library are keyless. An adapter whose
// key is required but missing reports ErrNotConfigured, which the API maps to
// a 503 the UI surfaces inline.
package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// ErrNotConfigured is returned when a provider needs an API key (or base URL)
// that was not supplied. The API maps it to 503 Service Unavailable.
var ErrNotConfigured = errors.New("providers: not configured")

// ErrBadResponse is returned when the provider answered but not in a way we
// can use (non-2xx, malformed JSON). The API maps it to 502 Bad Gateway.
var ErrBadResponse = errors.New("providers: bad response")

// Result is the provider-agnostic metadata a search returns for one item.
// The add flow shows these as cards and hands the fields to the kind's
// Add* service call on selection. Extra carries kind-specific, display-only
// detail (e.g. an overview or author list) that is not persisted.
type Result struct {
	// ID is the provider's own id for the item (TMDB movie id, TVDB series
	// id, MusicBrainz artist id, Open Library work key).
	ID string
	// Title is the human title to display and to add.
	Title string
	// Year is the release year when the provider supplies one (0 = unknown).
	Year int
	// Extra carries display-only, kind-specific fields (e.g. "overview",
	// "author", "rating"). Never persisted by the service layer.
	Extra map[string]string
}

// Searcher is the surface the API needs from a provider adapter.
type Searcher interface {
	// Name identifies the provider in errors and logs ("tmdb", "tvdb", ...).
	Name() string
	// Search returns up to 25 items matching q.
	Search(ctx context.Context, q string) ([]Result, error)
}

// Client is the shared, minimal HTTP plumbing for a provider adapter.
// Each adapter embeds it with its own base URL, name, and auth header.
type Client struct {
	// Name identifies the provider in errors ("tmdb", "tvdb", ...).
	Name string
	// Base is the provider's base URL (https://api.themoviedb.org/3, ...).
	Base string
	// AuthHeader, when non-empty, is set on every request
	// (e.g. "X-API-KEY" or "Authorization: Bearer ...").
	AuthHeader string
	// HTTP is the *http.Client. Default: 10s timeout.
	HTTP *http.Client
	// UserAgent is sent on every request; several providers (MusicBrainz)
	// require a contact-bearing UA.
	UserAgent string
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// getJSON fetches base+path with the given query values and decodes the JSON
// response into out. Non-2xx responses become ErrBadResponse with the status
// in the message; a malformed body is wrapped the same way.
func (c *Client) getJSON(ctx context.Context, path string, values url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+path, nil)
	if err != nil {
		return err
	}
	if values != nil {
		req.URL.RawQuery = values.Encode()
	}
	req.Header.Set("Accept", "application/json")
	if c.AuthHeader != "" {
		req.Header.Set("X-Api-Key", c.AuthHeader)
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBadResponse, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read at most 4 MiB — a metadata search response is small; a runaway
	// body must not grow memory unbounded.
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("%w: read body: %v", ErrBadResponse, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: %s returned %d: %s", ErrBadResponse, c.Name, resp.StatusCode, snippet(data, 200))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%w: invalid json: %v", ErrBadResponse, err)
	}
	return nil
}

func snippet(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "…"
	}
	return string(b)
}
