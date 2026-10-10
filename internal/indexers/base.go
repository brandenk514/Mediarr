package indexers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AdapterDeps bundles the shared dependencies every real indexer adapter needs
// (PLAN §7: adapters share the HTTP client, the health tracker, and the
// rate-limiter pool). A nil field means "use a built-in default", which keeps
// the constructors usable in isolation (tests) while letting main wire a single
// shared tracker/client across every adapter so per-indexer stats aggregate.
type AdapterDeps struct {
	// HTTP is the shared *http.Client. Default: 15s timeout.
	HTTP *http.Client
	// Health records per-indexer call stats. Default: a throwaway in-memory
	// tracker (only the per-adapter view, not the shared one).
	Health HealthTracker
}

// defaultHTTPClient is the fallback client when AdapterDeps.HTTP is nil.
func defaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

func (d *AdapterDeps) client() *http.Client {
	if d != nil && d.HTTP != nil {
		return d.HTTP
	}
	return defaultHTTPClient()
}

func (d *AdapterDeps) health() HealthTracker {
	if d != nil && d.Health != nil {
		return d.Health
	}
	return NewHealthTracker()
}

// httpGet performs a GET on baseURL with the given query params and returns the
// 2xx response body (caller must close it) or a typed error. It:
//   - maps non-2xx status to a typed error via ClassifyError (#31),
//   - records the outcome to the health tracker (#34),
//   - never includes credentials in the returned error string.
//
// On error the response body is already closed; on success the caller owns it.
func (d *AdapterDeps) httpGet(ctx context.Context, name, baseURL string, q url.Values) (io.ReadCloser, error) {
	start := time.Now()
	record := func(ok bool, e string) { d.health().Record(name, ok, time.Since(start), e) }

	u := baseURL
	if s := q.Encode(); s != "" {
		sep := "?"
		if strings.Contains(baseURL, "?") {
			sep = "&"
		}
		u = baseURL + sep + s
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		// A build failure means our URL was malformed. The error text can
		// embed the (credential-bearing) URL, so redact it before it reaches
		// stats or the returned error string (#34 log redaction).
		record(false, "build request: "+redactURL(err.Error()))
		return nil, newError(name+" get", ErrBadRequest, redactURL(err.Error()))
	}

	resp, err := d.client().Do(req)
	if err != nil {
		// net/http transport errors echo the request URL (which carries the
		// API key) — redact before it reaches stats or the returned error
		// string (#34 log redaction).
		rec := "transport: " + redactURL(err.Error())
		record(false, rec)
		return nil, newError(name+" get", ErrTransport, redactURL(err.Error()))
	}
	if e := ClassifyError(resp.StatusCode); e != nil {
		resp.Body.Close()
		record(false, fmt.Sprintf("status %d", resp.StatusCode))
		return nil, newError(name+" get", e, fmt.Sprintf("status %d", resp.StatusCode))
	}
	record(true, "")
	return resp.Body, nil
}

// httpStatus performs a GET and returns whether it succeeded (2xx), discarding
// the body. Used by Test() health probes (a single cheap request, PLAN §7).
func (d *AdapterDeps) httpStatus(ctx context.Context, name, baseURL string, q url.Values) error {
	body, err := d.httpGet(ctx, name, baseURL, q)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 1<<20))
	body.Close()
	return nil
}

// queryParam builds a url.Values from alternating key/value strings.
func queryParam(kvs ...string) url.Values {
	q := url.Values{}
	for i := 0; i+1 < len(kvs); i += 2 {
		q.Set(kvs[i], kvs[i+1])
	}
	return q
}

// isMagnet reports whether s is a magnet link.
func isMagnet(s string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(s)), "magnet:")
}

// protocolForURL derives a ReleaseInfo.Protocol from a download URL: magnet
// links are ProtocolTorrent, anything else (a .torrent or direct link) is
// ProtocolTorrentFile.
func protocolForURL(u string) string {
	if isMagnet(u) {
		return ProtocolTorrent
	}
	return ProtocolTorrentFile
}
