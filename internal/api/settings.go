package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/brandenk514/mediarr/internal/settings"
)

// SettingsDeps bundles what the settings endpoints (#44) need. Store is the
// persistent settings surface (the Postgres-backed settings.Store). The
// per-library override document is the settings the UI reads and writes here;
// the (plural) download clients live on their own endpoint surface
// (download_clients.go, Prowlarr PR5).
type SettingsDeps struct {
	Store settings.Store
}

// SetSettings wires the settings endpoints (#44). Called after construction,
// mirroring SetAuth/SetIndexers, so NewServer stays usable for health-only
// boot and tests.
//
//	Routes:
//	GET  /api/v1/settings/library        all four per-library overrides
//	PUT  /api/v1/settings/library/{kind} save one library's override
func (s *Server) SetSettings(deps *SettingsDeps) {
	s.settingsDeps = deps
	s.register("GET /api/v1/settings/library", s.requireAuth(s.GetLibraryOverrides))
	s.register("PUT /api/v1/settings/library/{kind}", s.requireAuth(s.PutLibraryOverride))
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
