package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/brandenk514/mediarr/internal/downloadclients"
)

// downloadClientProbeTimeout bounds a single test-probe request. A hung
// client must not stall the settings page (like the proxy probe's 5s).
const downloadClientProbeTimeout = 5 * time.Second

// DownloadClientsDeps bundles the saved download-client store (Prowlarr PR5
// "Download Clients"). Repo is the vault-aware client repository
// (postgres.DownloadClientsRepo implements downloadclients.Repo); it is the
// only layer that sees key ciphertext. HTTPClient overrides the probe client
// for the test endpoints (nil = a downloadClientProbeTimeout client).
type DownloadClientsDeps struct {
	Repo       downloadclients.Repo
	HTTPClient *http.Client
}

// SetDownloadClients registers the download-client endpoints. Call after
// NewServer; nil deps make the endpoints 501.
func (s *Server) SetDownloadClients(deps *DownloadClientsDeps) {
	s.downloadClientsDeps = deps
	s.register("GET /api/v1/settings/download-clients", s.requireAuth(s.ListDownloadClients))
	s.register("POST /api/v1/settings/download-clients", s.requireAuth(s.CreateDownloadClient))
	s.register("PUT /api/v1/settings/download-clients/{id}", s.requireAuth(s.UpdateDownloadClient))
	s.register("DELETE /api/v1/settings/download-clients/{id}", s.requireAuth(s.DeleteDownloadClient))
	s.register("POST /api/v1/settings/download-clients/{id}/test", s.requireAuth(s.TestDownloadClient))
	s.register("POST /api/v1/settings/download-clients/test-all", s.requireAuth(s.TestAllDownloadClients))
}

// downloadClientJSON is the read shape of a saved client: api_key is always
// empty (masked) and api_key_set reports its presence.
type downloadClientJSON struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	Enabled     bool      `json:"enabled"`
	Position    int       `json:"position"`
	BaseURL     string    `json:"base_url"`
	Username    string    `json:"username"`
	APIKey      string    `json:"api_key"`
	APIKeySet   bool      `json:"api_key_set"`
	CompleteDir string    `json:"complete_dir"`
	CreatedAt   time.Time `json:"created_at"`
}

func clientToJSON(rec downloadclients.Record) downloadClientJSON {
	return downloadClientJSON{
		ID:          rec.ID,
		Name:        rec.Name,
		Kind:        string(rec.Kind),
		Enabled:     rec.Enabled,
		Position:    rec.Position,
		BaseURL:     rec.BaseURL,
		Username:    rec.Username,
		APIKey:      "",
		APIKeySet:   rec.APIKey != "",
		CompleteDir: rec.CompleteDir,
		CreatedAt:   rec.CreatedAt,
	}
}

// downloadClientCreate is the body for POST /api/v1/settings/download-clients.
type downloadClientCreate struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	BaseURL     string `json:"base_url"`
	Username    string `json:"username"`
	APIKey      string `json:"api_key"`
	CompleteDir string `json:"complete_dir"`
	// Position, when omitted, appends after the last client.
	Position *int `json:"position"`
	// Enabled, when omitted, defaults to true.
	Enabled *bool `json:"enabled"`
}

// downloadClientUpdate is the body for PUT /api/v1/settings/download-clients
// /{id}. Every field is optional; absent/empty fields keep the stored value
// (the masked-edit round-trip: a client re-PUT with api_key "" keeps its
// key).
type downloadClientUpdate struct {
	Name        *string `json:"name"`
	Kind        *string `json:"kind"`
	BaseURL     *string `json:"base_url"`
	Username    *string `json:"username"`
	APIKey      *string `json:"api_key"`
	CompleteDir *string `json:"complete_dir"`
	Position    *int    `json:"position"`
	Enabled     *bool   `json:"enabled"`
}

// ListDownloadClients is GET /api/v1/settings/download-clients — every saved
// client in display order (position, then id), keys masked.
func (s *Server) ListDownloadClients(w http.ResponseWriter, r *http.Request) {
	if s.downloadClientsDeps == nil {
		writeError(w, http.StatusNotImplemented, "download clients are not available")
		return
	}
	recs, err := s.downloadClientsDeps.Repo.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list download clients")
		return
	}
	out := make([]downloadClientJSON, 0, len(recs))
	for _, rec := range recs {
		out = append(out, clientToJSON(rec))
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateDownloadClient is POST /api/v1/settings/download-clients.
func (s *Server) CreateDownloadClient(w http.ResponseWriter, r *http.Request) {
	if s.downloadClientsDeps == nil {
		writeError(w, http.StatusNotImplemented, "download clients are not available")
		return
	}
	var req downloadClientCreate
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if !downloadclients.Kind(req.Kind).Valid() {
		writeError(w, http.StatusBadRequest, "kind must be one of: qbittorrent, sabnzbd, transmission")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	var position int
	if req.Position != nil {
		position = *req.Position
	} else {
		p, err := s.nextClientPosition(r)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to assign a client position")
			return
		}
		position = p
	}
	id, _, err := s.downloadClientsDeps.Repo.Create(r.Context(), downloadclients.Record{
		Name:        req.Name,
		Kind:        downloadclients.Kind(req.Kind),
		Enabled:     enabled,
		Position:    position,
		BaseURL:     req.BaseURL,
		Username:    req.Username,
		APIKey:      req.APIKey,
		CompleteDir: req.CompleteDir,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create download client")
		return
	}
	rec, err := s.downloadClientsDeps.Repo.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create download client")
		return
	}
	writeJSON(w, http.StatusCreated, clientToJSON(*rec))
}

// nextClientPosition returns the position a new client gets (max position +
// 1, or 1 for an empty list).
func (s *Server) nextClientPosition(r *http.Request) (int, error) {
	recs, err := s.downloadClientsDeps.Repo.List(r.Context())
	if err != nil {
		return 0, err
	}
	max := 0
	for _, rec := range recs {
		if rec.Position > max {
			max = rec.Position
		}
	}
	return max + 1, nil
}

// UpdateDownloadClient is PUT /api/v1/settings/download-clients/{id}. An
// absent/empty field keeps the stored value — in particular an empty api_key
// keeps the stored key, which is what makes the masked-edit round-trip safe.
func (s *Server) UpdateDownloadClient(w http.ResponseWriter, r *http.Request) {
	if s.downloadClientsDeps == nil {
		writeError(w, http.StatusNotImplemented, "download clients are not available")
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	rec, err := s.downloadClientsDeps.Repo.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "unknown download client")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read download client")
		return
	}
	var req downloadClientUpdate
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name must not be empty")
		return
	}
	if req.Kind != nil && *req.Kind != "" && !downloadclients.Kind(*req.Kind).Valid() {
		writeError(w, http.StatusBadRequest, "kind must be one of: qbittorrent, sabnzbd, transmission")
		return
	}
	if req.Name != nil {
		rec.Name = *req.Name
	}
	if req.Kind != nil && *req.Kind != "" {
		rec.Kind = downloadclients.Kind(*req.Kind)
	}
	if req.BaseURL != nil {
		rec.BaseURL = *req.BaseURL
	}
	if req.Username != nil {
		rec.Username = *req.Username
	}
	if req.APIKey != nil && *req.APIKey != "" {
		rec.APIKey = *req.APIKey
	}
	if req.CompleteDir != nil {
		rec.CompleteDir = *req.CompleteDir
	}
	if req.Position != nil {
		rec.Position = *req.Position
	}
	if req.Enabled != nil {
		rec.Enabled = *req.Enabled
	}
	if err := s.downloadClientsDeps.Repo.Update(r.Context(), *rec); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update download client")
		return
	}
	writeJSON(w, http.StatusOK, clientToJSON(*rec))
}

// DeleteDownloadClient is DELETE /api/v1/settings/download-clients/{id}.
func (s *Server) DeleteDownloadClient(w http.ResponseWriter, r *http.Request) {
	if s.downloadClientsDeps == nil {
		writeError(w, http.StatusNotImplemented, "download clients are not available")
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := s.downloadClientsDeps.Repo.Get(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "unknown download client")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read download client")
		return
	}
	if err := s.downloadClientsDeps.Repo.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete download client")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// downloadClientTestResult is the wire shape of one probe outcome. A failed
// probe (config or network) is 200 {ok: false, message} — like the PR4
// notification test probe — never a 5xx.
type downloadClientTestResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// TestDownloadClient is POST /api/v1/settings/download-clients/{id}/test.
// Unknown id -> 404; otherwise the per-kind probe runs and the outcome is
// always 200 with {ok, message}.
func (s *Server) TestDownloadClient(w http.ResponseWriter, r *http.Request) {
	if s.downloadClientsDeps == nil {
		writeError(w, http.StatusNotImplemented, "download clients are not available")
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	rec, err := s.downloadClientsDeps.Repo.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "unknown download client")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read download client")
		return
	}
	okProbe, message := downloadClientProbe(r.Context(), *rec, s.downloadClientsDeps.HTTPClient)
	writeJSON(w, http.StatusOK, downloadClientTestResult{OK: okProbe, Message: message})
}

// TestAllDownloadClients is POST /api/v1/settings/download-clients/test-all —
// probes every saved client and reports each outcome (200 with an array).
func (s *Server) TestAllDownloadClients(w http.ResponseWriter, r *http.Request) {
	if s.downloadClientsDeps == nil {
		writeError(w, http.StatusNotImplemented, "download clients are not available")
		return
	}
	recs, err := s.downloadClientsDeps.Repo.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list download clients")
		return
	}
	type result struct {
		ID      int64  `json:"id"`
		Name    string `json:"name"`
		OK      bool   `json:"ok"`
		Message string `json:"message,omitempty"`
	}
	out := make([]result, 0, len(recs))
	for _, rec := range recs {
		okProbe, message := downloadClientProbe(r.Context(), rec, s.downloadClientsDeps.HTTPClient)
		out = append(out, result{ID: rec.ID, Name: rec.Name, OK: okProbe, Message: message})
	}
	writeJSON(w, http.StatusOK, out)
}

// downloadClientProbe runs the per-kind connectivity probe for c and returns
// (ok, message). Every failure — a config gap or an unreachable/wrong
// endpoint — comes back as ok=false; the message never includes credentials.
func downloadClientProbe(ctx context.Context, c downloadclients.Record, httpc *http.Client) (bool, string) {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		return false, "client has no base_url configured"
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false, "base_url must be an http(s) URL"
	}
	if httpc == nil {
		httpc = &http.Client{Timeout: downloadClientProbeTimeout}
	}
	switch c.Kind {
	case downloadclients.KindQB:
		return probeQBittorrent(ctx, c, base, httpc)
	case downloadclients.KindSAB:
		return probeSABnzbd(ctx, c, base, httpc)
	case downloadclients.KindTransmission:
		return probeTransmission(ctx, c, base, httpc)
	default:
		return false, "unknown client kind " + string(c.Kind)
	}
}

// probeQBittorrent mirrors internal/downloads' auth check: the app/version
// endpoint requires authentication, so a 200 means the API key (the
// X-Api-Key header) is valid.
func probeQBittorrent(ctx context.Context, c downloadclients.Record, base string, httpc *http.Client) (bool, string) {
	if c.APIKey == "" {
		return false, "qBittorrent requires an API key"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v2/app/version", nil)
	if err != nil {
		return false, err.Error()
	}
	req.Header.Set("X-Api-Key", c.APIKey)
	resp, err := httpc.Do(req)
	if err != nil {
		return false, "qBittorrent unreachable: " + err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return false, "qBittorrent rejected the credentials (HTTP " + itoa(resp.StatusCode) + ")"
	}
	return true, ""
}

// probeSABnzbd pings the JSON API with the stored key: GET
// /api?mode=version&output=json&apikey=*** A 200 with a version and no
// "error" key means the key is valid (SABnzbd answers auth failures with an
// "error" body or a non-2xx).
func probeSABnzbd(ctx context.Context, c downloadclients.Record, base string, httpc *http.Client) (bool, string) {
	if c.APIKey == "" {
		return false, "SABnzbd requires an API key"
	}
	q := url.Values{}
	q.Set("mode", "version")
	q.Set("output", "json")
	q.Set("apikey", c.APIKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api?"+q.Encode(), nil)
	if err != nil {
		return false, err.Error()
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return false, "SABnzbd unreachable: " + err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false, "SABnzbd unreachable: " + err.Error()
	}
	var out struct {
		Error   string `json:"error"`
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &out) == nil && out.Error != "" {
		return false, "SABnzbd rejected the API key: " + out.Error
	}
	if resp.StatusCode != 200 {
		return false, "SABnzbd unreachable (HTTP " + itoa(resp.StatusCode) + ")"
	}
	if out.Version == "" {
		return false, "SABnzbd did not report a version"
	}
	return true, ""
}

// probeTransmission sends the JSON-RPC session.get ping to the RPC endpoint;
// a 200 whose result carries a session-id means the endpoint is reachable.
func probeTransmission(ctx context.Context, c downloadclients.Record, base string, httpc *http.Client) (bool, string) {
	body, _ := json.Marshal(map[string]any{"method": "session.get"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/transmission/rpc", strings.NewReader(string(body)))
	if err != nil {
		return false, err.Error()
	}
	req.Header.Set("Content-Type", "application/x-json-rpc")
	resp, err := httpc.Do(req)
	if err != nil {
		return false, "Transmission unreachable: " + err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false, "Transmission unreachable: " + err.Error()
	}
	var out struct {
		Result struct {
			SessionID string `json:"session-id"`
		} `json:"result"`
	}
	if resp.StatusCode != 200 || json.Unmarshal(data, &out) != nil || out.Result.SessionID == "" {
		return false, "Transmission did not answer the session probe (HTTP " + itoa(resp.StatusCode) + ")"
	}
	return true, ""
}
