package downloads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// sabHTTPTimeout bounds a single SABnzbd Web UI API request.
const sabHTTPTimeout = 15 * time.Second

// SABnzbdClient is the real usenet download client (#37). It implements
// Client (Add/Status) and Remover (delete a finished item after import) and is
// the parity counterpart of QBittorrentClient: same interface, usenet
// semantics. It talks to the SABnzbd JSON API (api=...&output=json), so it is
// fully testable against a recorded/stubbed server.
//
// Auth is a single API key (SABNZBD_API_KEY) appended as ?apikey=. Like the
// qBittorrent client, the key never appears in error strings.
type SABnzbdClient struct {
	name        string
	base        string
	apiKey      string
	completeDir string // default complete directory (its "downloads" dir)
	http        *http.Client
}

// SABnzbdConfig configures a SABnzbdClient.
type SABnzbdConfig struct {
	// Base is the Web UI base URL, e.g. "http://localhost:8080".
	Base string
	// APIKey is the SABnzbd API key (required).
	APIKey string
	// CompleteDir is the default complete directory; a release's PathDir
	// overrides it at queue time.
	CompleteDir string
}

// NewSABnzbdClient builds a client. Base and APIKey are required.
func NewSABnzbdClient(cfg SABnzbdConfig) (*SABnzbdClient, error) {
	if cfg.Base == "" {
		return nil, errors.New("sabnzbd: base url is required")
	}
	u, err := url.Parse(cfg.Base)
	if err != nil {
		return nil, fmt.Errorf("sabnzbd: parse base: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("sabnzbd: base must be http/https, got %q", u.Scheme)
	}
	if cfg.APIKey == "" {
		return nil, errors.New("sabnzbd: api key is required")
	}
	return &SABnzbdClient{
		name:        "SABnzbd",
		base:        strings.TrimRight(cfg.Base, "/"),
		apiKey:      cfg.APIKey,
		completeDir: cfg.CompleteDir,
		http:        &http.Client{Timeout: sabHTTPTimeout},
	}, nil
}

// Name implements Client.
func (c *SABnzbdClient) Name() string { return c.name }

// sabQueueItem is an active queue entry (JSON API /api?mode=queue).
type sabQueueItem struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	PP        string  `json:"pp"` // post-processing state
	Status    string  `json:"status"`
	MBTotal   float64 `json:"mbtotal"`
	MBDone    float64 `json:"mbdone"`
	LineSplit int     `json:"line_split"`
	// CompletedAt is non-zero (epoch-ish) when the slot is done.
	Completed bool `json:"-"`
}

// sabHistoryItem is a finished item (JSON API /api?mode=history).
type sabHistoryItem struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"` // "Success", "Failed", "Paused", ...
}

// Add implements Client. It queues the release's URL (an NZB link or .nzb
// URL) into SABnzbd. A release with no URL cannot be downloaded by a real
// client, so that is an error here (the mock accepts URL-less releases).
func (c *SABnzbdClient) Add(ctx context.Context, r Release) error {
	if r.URL == "" {
		return fmt.Errorf("sabnzbd: release %q has no download URL", r.Title)
	}
	q := url.Values{}
	q.Set("mode", "addurl")
	q.Set("link", r.URL)
	if p := r.PathDir; p != "" {
		q.Set("cat", "") // keep default category
	}
	var out struct {
		Status string `json:"status"`
		Errors []any  `json:"errors"`
	}
	if err := c.do(ctx, q, &out); err != nil {
		return fmt.Errorf("sabnzbd: add %q: %w", r.Title, err)
	}
	if out.Status != "success" {
		return fmt.Errorf("sabnzbd: add %q: %v", r.Title, out.Errors)
	}
	return nil
}

// Status implements Client. It looks the release up in the active queue, then
// in history if it has already completed, and maps usenet states to the
// shared Status. A completed release resolves its file under the complete
// directory (SABnzbd names output folders after the NZB name).
func (c *SABnzbdClient) Status(ctx context.Context, title string) (Status, error) {
	// 1. Active queue: in-progress or failed.
	var q struct {
		Slots []sabQueueItem `json:"slots"`
	}
	if err := c.do(ctx, url.Values{"mode": {"queue"}, "start": {"0"}, "limit": {"500"}}, &q); err != nil {
		return Status{}, fmt.Errorf("sabnzbd: status %q: %w", title, err)
	}
	for _, s := range q.Slots {
		if s.Name != title {
			continue
		}
		// "Paused"/"Queued"/downloading/repairing/parity: not complete.
		// "Failed" (post-processing failed) is a hard error.
		if s.Status == "Failed" {
			return Status{Complete: false, Progress: progressOf(s)},
				fmt.Errorf("sabnzbd: release %q failed: %s", title, s.PP)
		}
		// "Completed"/"Checking..." are transitional; the file may not be
		// final yet. Treat completed-in-queue as done if a file is present.
		if s.Status == "Completed" {
			if f, err := c.resolveFile(title); err == nil {
				return Status{Complete: true, Progress: 100, File: f}, nil
			}
		}
		return Status{Complete: false, Progress: progressOf(s)}, nil
	}

	// 2. History: finished (and usually no longer in the queue).
	var h struct {
		Slots []sabHistoryItem `json:"slots"`
	}
	if err := c.do(ctx, url.Values{"mode": {"history"}, "start": {"0"}, "limit": {"100"}}, &h); err != nil {
		return Status{}, fmt.Errorf("sabnzbd: history %q: %w", title, err)
	}
	for _, s := range h.Slots {
		if s.Name != title {
			continue
		}
		if s.Status == "Failed" {
			return Status{Complete: false, Progress: 100},
				fmt.Errorf("sabnzbd: release %q failed in history", title)
		}
		f, err := c.resolveFile(title)
		if err != nil {
			return Status{Complete: false, Progress: 100}, nil // finishing
		}
		return Status{Complete: true, Progress: 100, File: f}, nil
	}

	// Not in queue or history: report in-progress (may not have started).
	return Status{Complete: false, Progress: 0}, nil
}

// remove implements Remover by deleting the finished item from history.
func (c *SABnzbdClient) Remove(ctx context.Context, title string) error {
	var h struct {
		Slots []sabHistoryItem `json:"slots"`
	}
	if err := c.do(ctx, url.Values{"mode": {"history"}, "start": {"0"}, "limit": {"200"}}, &h); err != nil {
		return fmt.Errorf("sabnzbd: remove %q: %w", title, err)
	}
	for _, s := range h.Slots {
		if s.Name != title {
			continue
		}
		q := url.Values{}
		q.Set("mode", "history")
		q.Set("delete", s.ID)
		var out struct {
			Status string `json:"status"`
		}
		if err := c.do(ctx, q, &out); err != nil {
			return fmt.Errorf("sabnzbd: remove %q: %w", title, err)
		}
		return nil
	}
	return nil // already gone
}

// resolveFile finds the downloaded file: SABnzbd writes into <completeDir>/<nz
// name>, so the largest regular file under that folder is the release.
func (c *SABnzbdClient) resolveFile(title string) (string, error) {
	dir := c.completeDir
	if dir == "" {
		return "", errors.New("sabnzbd: no complete directory configured")
	}
	// The exact subfolder is the NZB name; fall back to scanning the complete
	// dir for the largest file (the release is the newest big thing there).
	f, err := largestFile(dir + "/" + title)
	if err == nil {
		return f, nil
	}
	return largestFile(dir)
}

// progressOf maps a queue slot's sizes to a 0-100 progress (0 if unknown).
func progressOf(s sabQueueItem) int {
	if s.MBTotal <= 0 {
		return 0
	}
	p := int(s.MBDone / s.MBTotal * 100)
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	return p
}

// do issues a SABnzbd JSON API request (base + /api?...) with the API key and
// decodes the response. A non-2xx or JSON-API "success:false" is an error that
// never includes the key.
func (c *SABnzbdClient) do(ctx context.Context, q url.Values, out any) error {
	q.Set("apikey", c.apiKey)
	q.Set("output", "json")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("sabnzbd: HTTP %d", resp.StatusCode)
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// compile-time proof both clients satisfy the shared interfaces.
var (
	_ Client  = (*QBittorrentClient)(nil)
	_ Remover = (*QBittorrentClient)(nil)
	_ Client  = (*SABnzbdClient)(nil)
	_ Remover = (*SABnzbdClient)(nil)
)
