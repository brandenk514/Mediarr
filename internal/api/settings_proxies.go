package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/brandenk514/mediarr/internal/indexers"
)

// proxyProbeTimeout is the short client timeout for the FlareSolverr
// reachability probe (spec: ~5s).
const proxyProbeTimeout = 5 * time.Second

// IndexerProxiesDeps bundles the saved indexer-proxy store (Prowlarr PR4
// "Indexer Proxies" — FlareSolverr instances). Repo is the vault-aware
// proxy repository (postgres.IndexerProxiesRepo implements
// indexers.ProxyRepo); it is the only layer that sees password ciphertext.
// HTTPClient overrides the probe client for the test endpoint (nil = a
// proxyProbeTimeout client).
type IndexerProxiesDeps struct {
	Repo       indexers.ProxyRepo
	HTTPClient *http.Client
}

// SetIndexerProxies registers the indexer-proxy endpoints. Call after
// NewServer; nil deps make the endpoints 501.
func (s *Server) SetIndexerProxies(deps *IndexerProxiesDeps) {
	s.indexerProxiesDeps = deps
	s.register("GET /api/v1/settings/proxies", s.requireAuth(s.ListIndexerProxies))
	s.register("POST /api/v1/settings/proxies", s.requireAuth(s.CreateIndexerProxy))
	s.register("PUT /api/v1/settings/proxies/{id}", s.requireAuth(s.UpdateIndexerProxy))
	s.register("DELETE /api/v1/settings/proxies/{id}", s.requireAuth(s.DeleteIndexerProxy))
	s.register("POST /api/v1/settings/proxies/{id}/test", s.requireAuth(s.TestIndexerProxy))
}

// indexerProxyJSON is the wire shape of a saved proxy: password is always
// empty (masked) and password_set reports its presence; tags is never null.
type indexerProxyJSON struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	Username    string    `json:"username"`
	Password    string    `json:"password"`
	PasswordSet bool      `json:"password_set"`
	Tags        []string  `json:"tags"`
	CreatedAt   time.Time `json:"created_at"`
}

func proxyToJSON(p indexers.Proxy) indexerProxyJSON {
	tags := p.Tags
	if tags == nil {
		tags = []string{}
	}
	return indexerProxyJSON{
		ID:          p.ID,
		Name:        p.Name,
		URL:         p.URL,
		Username:    p.Username,
		Password:    "",
		PasswordSet: p.Password != "",
		Tags:        tags,
		CreatedAt:   p.CreatedAt,
	}
}

// indexerProxyCreate is the body for POST /api/v1/settings/proxies. Tags
// omitted = []; password is plaintext in the request but never echoed back.
type indexerProxyCreate struct {
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	Username string   `json:"username"`
	Password string   `json:"password"`
	Tags     []string `json:"tags"`
}

// indexerProxyUpdate is the body for PUT /api/v1/settings/proxies/{id}.
// An empty password keeps the stored password (masked-edit round-trip);
// omitted tags keep the stored tags.
type indexerProxyUpdate struct {
	Name     string    `json:"name"`
	URL      string    `json:"url"`
	Username string    `json:"username"`
	Password string    `json:"password"`
	Tags     *[]string `json:"tags"`
}

// ListIndexerProxies returns every saved proxy in creation order, masked.
func (s *Server) ListIndexerProxies(w http.ResponseWriter, r *http.Request) {
	if s.indexerProxiesDeps == nil {
		writeError(w, http.StatusNotImplemented, "indexer proxies are not available")
		return
	}
	proxies, err := s.indexerProxiesDeps.Repo.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list indexer proxies")
		return
	}
	out := make([]indexerProxyJSON, 0, len(proxies))
	for _, p := range proxies {
		out = append(out, proxyToJSON(p))
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateIndexerProxy stores a new proxy and returns it masked (201). Name
// and url are required.
func (s *Server) CreateIndexerProxy(w http.ResponseWriter, r *http.Request) {
	if s.indexerProxiesDeps == nil {
		writeError(w, http.StatusNotImplemented, "indexer proxies are not available")
		return
	}
	var in indexerProxyCreate
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if in.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	tags := in.Tags
	if tags == nil {
		tags = []string{}
	}
	p := indexers.Proxy{
		Name:     in.Name,
		URL:      in.URL,
		Username: in.Username,
		Password: in.Password,
		Tags:     tags,
	}
	id, createdAt, err := s.indexerProxiesDeps.Repo.Create(r.Context(), p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create indexer proxy")
		return
	}
	p.ID = id
	p.CreatedAt = createdAt
	writeJSON(w, http.StatusCreated, proxyToJSON(p))
}

// UpdateIndexerProxy fully replaces a proxy's fields (200, masked). An empty
// password keeps the stored password; an unknown id is a 404.
func (s *Server) UpdateIndexerProxy(w http.ResponseWriter, r *http.Request) {
	if s.indexerProxiesDeps == nil {
		writeError(w, http.StatusNotImplemented, "indexer proxies are not available")
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in indexerProxyUpdate
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if in.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	existing, err := s.indexerProxiesDeps.Repo.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "unknown indexer proxy")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read indexer proxy")
		return
	}
	password := in.Password
	if password == "" {
		password = existing.Password // masked-edit round-trip: keep the stored secret
	}
	tags := existing.Tags
	if in.Tags != nil {
		tags = *in.Tags
	}
	if tags == nil {
		tags = []string{}
	}
	p := indexers.Proxy{
		ID:        id,
		Name:      in.Name,
		URL:       in.URL,
		Username:  in.Username,
		Password:  password,
		Tags:      tags,
		CreatedAt: existing.CreatedAt,
	}
	if err := s.indexerProxiesDeps.Repo.Update(r.Context(), p); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "unknown indexer proxy")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update indexer proxy")
		return
	}
	writeJSON(w, http.StatusOK, proxyToJSON(p))
}

// DeleteIndexerProxy removes a proxy (204). An unknown id is a 404.
func (s *Server) DeleteIndexerProxy(w http.ResponseWriter, r *http.Request) {
	if s.indexerProxiesDeps == nil {
		writeError(w, http.StatusNotImplemented, "indexer proxies are not available")
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := s.indexerProxiesDeps.Repo.Get(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "unknown indexer proxy")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read indexer proxy")
		return
	}
	if err := s.indexerProxiesDeps.Repo.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete indexer proxy")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// flareRoot is the subset of the FlareSolverr root JSON the probe reads.
type flareRoot struct {
	Version string `json:"version"`
}

// TestIndexerProxy GETs the instance root with a short timeout (a
// FlareSolverr instance serves its version as JSON at the root). A
// reachable instance is 200 with {ok: true, version}; an unreachable or
// non-2xx instance is a 502. An unknown id is a 404.
func (s *Server) TestIndexerProxy(w http.ResponseWriter, r *http.Request) {
	if s.indexerProxiesDeps == nil {
		writeError(w, http.StatusNotImplemented, "indexer proxies are not available")
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := s.indexerProxiesDeps.Repo.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "unknown indexer proxy")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read indexer proxy")
		return
	}
	client := s.indexerProxiesDeps.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: proxyProbeTimeout}
	}
	ctx, cancel := context.WithTimeout(r.Context(), proxyProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil) //nolint:gosec // G704: p.URL is an admin-configured FlareSolverr instance; the route is auth-gated and the probe is the feature
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid proxy url: "+err.Error())
		return
	}
	if p.Username != "" {
		req.SetBasicAuth(p.Username, p.Password)
	}
	resp, err := client.Do(req) //nolint:gosec // G704: see above
	if err != nil {
		writeError(w, http.StatusBadGateway, "flaresolverr instance unreachable: "+err.Error())
		return
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadGateway, "flaresolverr instance unreachable: "+err.Error())
		return
	}
	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, "flaresolverr instance returned "+strconv.Itoa(resp.StatusCode))
		return
	}
	var root flareRoot
	_ = json.Unmarshal(body, &root) // version is best-effort
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": root.Version})
}
