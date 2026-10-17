package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/brandenk514/mediarr/internal/settings"
)

// SettingsDeps bundles what the settings endpoints (#44) need. Store is the
// persistent settings surface (the Postgres-backed settings.Store). The two
// documents it holds — the download-client config and the per-library
// overrides — are exactly the settings the UI reads and writes.
type SettingsDeps struct {
	Store settings.Store
}

// SetSettings wires the settings endpoints (#44). Called after construction,
// mirroring SetAuth/SetIndexers, so NewServer stays usable for health-only
// boot and tests.
//
//	Routes:
//	GET  /api/v1/settings/download-client          masked client config
//	PUT  /api/v1/settings/download-client          save client config
//	GET  /api/v1/settings/library                  all four per-library overrides
//	PUT  /api/v1/settings/library/{kind}           save one library's override
func (s *Server) SetSettings(deps *SettingsDeps) {
	s.settingsDeps = deps
	s.register("GET /api/v1/settings/download-client", s.requireAuth(s.GetDownloadClient))
	s.register("PUT /api/v1/settings/download-client", s.requireAuth(s.PutDownloadClient))
	s.register("GET /api/v1/settings/library", s.requireAuth(s.GetLibraryOverrides))
	s.register("PUT /api/v1/settings/library/{kind}", s.requireAuth(s.PutLibraryOverride))
}

// ---- download client ----

// downloadClientJSON is the *masked* read shape of the download-client config.
// The secrets (qb API key / password, sab API key) are never returned — only
// presence booleans (PLAN §8: "keys never returned, only masked presence"),
// the same contract as the indexer list endpoint. The non-secret fields are
// returned as-is.
type downloadClientJSON struct {
	Kind           string `json:"kind"`
	QBBase         string `json:"qb_base"`
	QBUsername     string `json:"qb_username"`
	QBAPIKeySet    bool   `json:"qb_api_key_set"`
	QBPasswordSet  bool   `json:"qb_password_set"`
	SABBase        string `json:"sab_base"`
	SABAPIKeySet   bool   `json:"sab_api_key_set"`
	SABCompleteDir string `json:"sab_complete_dir"`
}

func dcToJSON(dc *settings.DownloadClient) downloadClientJSON {
	if dc == nil {
		dc = &settings.DownloadClient{}
	}
	return downloadClientJSON{
		Kind:           dc.Kind,
		QBBase:         dc.QBBase,
		QBUsername:     dc.QBUsername,
		QBAPIKeySet:    dc.QBAPIKey != "",
		QBPasswordSet:  dc.QBPassword != "",
		SABBase:        dc.SABBase,
		SABAPIKeySet:   dc.SABAPIKey != "",
		SABCompleteDir: dc.SABCompleteDir,
	}
}

// downloadClientRequest is the write shape. A secret field that is an empty
// string means "keep the existing value" — the standard masked-password
// round-trip, so the UI can edit the non-secret fields (and re-encrypt nothing)
// without clobbering a stored credential. A non-empty value replaces it.
type downloadClientRequest struct {
	Kind           string `json:"kind"`
	QBBase         string `json:"qb_base"`
	QBUsername     string `json:"qb_username"`
	QBAPIKey       string `json:"qb_api_key"`
	QBPassword     string `json:"qb_password"`
	SABBase        string `json:"sab_base"`
	SABAPIKey      string `json:"sab_api_key"`
	SABCompleteDir string `json:"sab_complete_dir"`
}

// GetDownloadClient handles GET /api/v1/settings/download-client.
func (s *Server) GetDownloadClient(w http.ResponseWriter, r *http.Request) {
	dc, err := s.settingsDeps.Store.GetDownloadClient(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dcToJSON(dc))
}

// PutDownloadClient handles PUT /api/v1/settings/download-client. It
// server-side-validates the client kind + per-client required fields (mirroring
// the downloads factory, so a saved config actually builds at boot), merges an
// empty secret against the existing value ("empty = keep"), and persists it.
func (s *Server) PutDownloadClient(w http.ResponseWriter, r *http.Request) {
	var req downloadClientRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Kind == "" {
		req.Kind = "mock"
	}
	if !settings.IsValidClientKind(req.Kind) {
		writeError(w, http.StatusBadRequest, "unknown client kind "+strconv.Quote(req.Kind))
		return
	}

	// Load the existing document so an empty secret can mean "keep".
	existing, err := s.settingsDeps.Store.GetDownloadClient(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	dc := &settings.DownloadClient{
		Kind:           req.Kind,
		QBBase:         req.QBBase,
		QBUsername:     req.QBUsername,
		SABBase:        req.SABBase,
		SABCompleteDir: req.SABCompleteDir,
		// Secrets: empty request value preserves the stored one.
		QBAPIKey:   keepOr(existing.QBAPIKey, req.QBAPIKey),
		QBPassword: keepOr(existing.QBPassword, req.QBPassword),
		SABAPIKey:  keepOr(existing.SABAPIKey, req.SABAPIKey),
	}

	if msg := validateDownloadClient(dc); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	if err := s.settingsDeps.Store.PutDownloadClient(r.Context(), dc); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dcToJSON(dc))
}

// validateDownloadClient returns a human-readable error if the config is
// invalid, else "". It mirrors the downloads factory's per-client rules so a
// saved configuration is guaranteed to build.
func validateDownloadClient(dc *settings.DownloadClient) string {
	switch dc.Kind {
	case "qbittorrent":
		if !isHTTPURL(dc.QBBase) {
			return "qbittorrent: base url is required (http/https)"
		}
		hasKey := dc.QBAPIKey != ""
		hasPass := dc.QBUsername != "" && dc.QBPassword != ""
		if !hasKey && !hasPass {
			return "qbittorrent: provide either an API key or a username + password"
		}
	case "sabnzbd":
		if !isHTTPURL(dc.SABBase) {
			return "sabnzbd: base url is required (http/https)"
		}
		if dc.SABAPIKey == "" {
			return "sabnzbd: an API key is required"
		}
	}
	return ""
}

func isHTTPURL(s string) bool {
	if s == "" {
		return false
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// keepOr returns in when in is non-empty (a replacement), else the stored
// existing value (a masked-field no-op).
func keepOr(existing, in string) string {
	if in != "" {
		return in
	}
	return existing
}

// ---- per-library overrides ----

// GetLibraryOverrides handles GET /api/v1/settings/library: all four libraries'
// overrides in one document, keyed by kind, so the UI can render a column per
// library without four round-trips.
func (s *Server) GetLibraryOverrides(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := map[string]settings.LibraryOverride{}
	for _, k := range settings.LibraryKinds {
		o, err := s.settingsDeps.Store.GetLibrary(ctx, k)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if o == nil {
			o = &settings.LibraryOverride{}
		}
		out[k.String()] = *o
	}
	writeJSON(w, http.StatusOK, out)
}

// PutLibraryOverride handles PUT /api/v1/settings/library/{kind}: save one
// library's override. A body of all-empty values clears the override (the
// store deletes the row so a later read yields a fresh default).
func (s *Server) PutLibraryOverride(w http.ResponseWriter, r *http.Request) {
	kind, err := parseLibraryKind(r.PathValue("kind"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var o settings.LibraryOverride
	if err := decodeJSON(w, r, &o); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.settingsDeps.Store.PutLibrary(r.Context(), kind, &o); err != nil {
		if errors.Is(err, settings.ErrInvalidLibraryKind) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func parseLibraryKind(s string) (settings.LibraryKind, error) {
	k := settings.LibraryKind(s)
	if !k.IsValid() {
		return "", errors.New("unknown library kind " + strconv.Quote(s) + " (want movie, tv, music, or book)")
	}
	return k, nil
}
