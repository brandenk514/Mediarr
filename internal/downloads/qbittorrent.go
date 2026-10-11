package downloads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// qbHTTPTimeout bounds a single Web API request. A hung qBittorrent must not
// stall the pipeline's status polling.
const qbHTTPTimeout = 15 * time.Second

// QBittorrentClient is the real torrent download client (#36). It implements
// Client (Add/Status) and Remover (drop a torrent after import). It talks to
// the qBittorrent Web API v2 over HTTP, so it is fully testable against a
// recorded/stubbed server: the HTTP client is constructed with a fixed timeout
// and every endpoint is a plain GET/POST/DELETE.
//
// Auth is either an API key (the X-Api-Key header, preferred) or a
// username/password login that yields an SID cookie. Credentials never appear
// in error strings (mirrors the indexer redaction rule).
type QBittorrentClient struct {
	name     string
	base     string
	apiKey   string
	username string
	password string
	savePath string // default save directory (its "downloads" dir)
	http     *http.Client
}

// QBittorrentConfig configures a QBittorrentClient.
type QBittorrentConfig struct {
	// Base is the Web API base URL, e.g. "http://localhost:8080".
	Base string
	// APIKey, when set, authenticates every request via the X-Api-Key header
	// and skips login. Preferred.
	APIKey string
	// Username/Password are used when APIKey is empty (cookie login).
	Username string
	Password string
	// SavePath is the default save directory; a release's PathDir overrides it.
	SavePath string
}

// NewQBittorrentClient builds a client. Base is required; exactly one of
// APIKey or Username+Password must be provided.
func NewQBittorrentClient(cfg QBittorrentConfig) (*QBittorrentClient, error) {
	if cfg.Base == "" {
		return nil, errors.New("qbittorrent: base url is required")
	}
	u, err := url.Parse(cfg.Base)
	if err != nil {
		return nil, fmt.Errorf("qbittorrent: parse base: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("qbittorrent: base must be http/https, got %q", u.Scheme)
	}
	hasKey := cfg.APIKey != ""
	hasPass := cfg.Username != "" && cfg.Password != ""
	if !hasKey && !hasPass {
		return nil, errors.New("qbittorrent: provide either APIKey or Username+Password")
	}
	c := &QBittorrentClient{
		name:     "qBittorrent",
		base:     strings.TrimRight(cfg.Base, "/"),
		apiKey:   cfg.APIKey,
		username: cfg.Username,
		password: cfg.Password,
		savePath: cfg.SavePath,
		http:     &http.Client{Timeout: qbHTTPTimeout},
	}
	if !hasKey {
		jar, err := cookiejar.New(nil)
		if err != nil {
			return nil, fmt.Errorf("qbittorrent: cookie jar: %w", err)
		}
		c.http.Jar = jar
	}
	return c, nil
}

// Name implements Client.
func (c *QBittorrentClient) Name() string { return c.name }

// qbTorrent is a qBittorrent list entry — only the fields the adapter needs.
type qbTorrent struct {
	Hash        string  `json:"hash"`
	Name        string  `json:"name"`
	Progress    float64 `json:"progress"` // 0.0-1.0
	State       string  `json:"state"`
	ContentPath string  `json:"content_path"`
}

// Add implements Client. It sends the release's URL (a magnet:? uri or a
// .torrent link) to qBittorrent. A release with no URL cannot be downloaded by
// a real client, so that is an error here (the mock accepts URL-less releases).
func (c *QBittorrentClient) Add(ctx context.Context, r Release) error {
	if r.URL == "" {
		return fmt.Errorf("qbittorrent: release %q has no download URL", r.Title)
	}
	form := url.Values{}
	form.Set("urls", r.URL)
	savePath := r.PathDir
	if savePath == "" {
		savePath = c.savePath
	}
	if savePath != "" {
		form.Set("savepath", savePath)
	}
	var out struct {
		Msg string `json:"msg"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v2/torrents/add", form, &out); err != nil {
		return fmt.Errorf("qbittorrent: add %q: %w", r.Title, err)
	}
	return nil
}

// Status implements Client. It finds the torrent by name, maps its state, and
// — when complete — resolves the downloaded file under its content path so the
// import step can move it.
func (c *QBittorrentClient) Status(ctx context.Context, title string) (Status, error) {
	var list []qbTorrent
	if err := c.do(ctx, http.MethodGet, "/api/v2/torrents/info", nil, &list); err != nil {
		return Status{}, fmt.Errorf("qbittorrent: status %q: %w", title, err)
	}
	var t *qbTorrent
	for i := range list {
		if list[i].Name == title {
			t = &list[i]
			break
		}
	}
	if t == nil {
		// Not (yet) in the queue: report in-progress, not complete.
		return Status{Complete: false, Progress: 0}, nil
	}
	// A failed/missing torrent is a hard error so the pipeline fails the queue
	// entry rather than polling forever.
	if t.State == "error" || t.State == "missingFiles" {
		return Status{Complete: false, Progress: int(t.Progress * 100)},
			fmt.Errorf("qbittorrent: torrent %q in state %q", title, t.State)
	}
	if t.Progress < 1.0 {
		return Status{Complete: false, Progress: int(t.Progress * 100)}, nil
	}
	// Complete: resolve the largest regular file under the content path.
	root := t.ContentPath
	if root == "" {
		root = c.savePath
	}
	file, err := largestFile(root)
	if err != nil {
		return Status{}, fmt.Errorf("qbittorrent: no file for %q under %q: %w", title, root, err)
	}
	return Status{Complete: true, Progress: 100, File: file}, nil
}

// Remove implements Remover. It drops the torrent by name. Unknown titles are
// a no-op (nil), per the Remover contract (already-imported / already-removed).
func (c *QBittorrentClient) Remove(ctx context.Context, title string) error {
	var list []qbTorrent
	if err := c.do(ctx, http.MethodGet, "/api/v2/torrents/info", nil, &list); err != nil {
		return fmt.Errorf("qbittorrent: remove %q: %w", title, err)
	}
	var hash string
	for _, t := range list {
		if t.Name == title {
			hash = t.Hash
			break
		}
	}
	if hash == "" {
		return nil // already gone
	}
	form := url.Values{}
	form.Set("hashes", hash)
	var out struct {
		Msg string `json:"msg"`
	}
	if err := c.do(ctx, http.MethodDelete, "/api/v2/torrents/delete", form, &out); err != nil {
		return fmt.Errorf("qbittorrent: remove %q: %w", title, err)
	}
	return nil
}

// do performs a request against the Web API, applying auth (API-key header or
// a prior cookie), encoding the form body, and decoding the JSON response.
// Auth failures and non-2xx statuses become errors that never include
// credentials.
func (c *QBittorrentClient) do(ctx context.Context, method, path string, form url.Values, out any) error {
	// Username/password auth: ensure we have logged in (cookie present) — but
	// not for the login request itself, or this recurses forever.
	if c.apiKey == "" && path != "/api/v2/auth/login" {
		if err := c.login(ctx); err != nil {
			return err
		}
	}

	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
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
		// qBittorrent errors: {"error": "..."}. Surface a credential-free message.
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		if e.Error != "" {
			return fmt.Errorf("qbittorrent: %s: %s", method+" "+path, e.Error)
		}
		return fmt.Errorf("qbittorrent: %s: HTTP %d", method+" "+path, resp.StatusCode)
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// login authenticates with username/password and stores the SID cookie in the
// client's jar. It is idempotent: if an SID cookie is already present it is a
// no-op.
func (c *QBittorrentClient) login(ctx context.Context) error {
	if c.hasSIDCookie() {
		return nil
	}
	form := url.Values{}
	form.Set("username", c.username)
	form.Set("password", c.password)
	var out struct {
		Status string `json:"status"`
		Msg    string `json:"message"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v2/auth/login", form, &out); err != nil {
		return fmt.Errorf("qbittorrent: login: %w", err)
	}
	if out.Status != "Ok." && !c.hasSIDCookie() {
		return fmt.Errorf("qbittorrent: login failed (no session cookie)")
	}
	return nil
}

func (c *QBittorrentClient) hasSIDCookie() bool {
	if c.http.Jar == nil {
		return false
	}
	u, _ := url.Parse(c.base)
	for _, ck := range c.http.Jar.Cookies(u) {
		if ck.Name == "SID" && ck.Value != "" {
			return true
		}
	}
	return false
}

// largestFile returns the path of the largest regular file under dir (walking
// recursively). It is how the client resolves the "downloaded file" for a
// completed torrent/NZB whose exact output name the tracker doesn't expose. A
// missing or empty directory is an error.
func largestFile(dir string) (string, error) {
	if dir == "" {
		return "", errors.New("empty directory")
	}
	var best string
	var bestSize int64
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries, keep walking
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Size() > bestSize {
			bestSize = info.Size()
			best = p
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if best == "" {
		return "", fmt.Errorf("no files found under %s", dir)
	}
	return best, nil
}
