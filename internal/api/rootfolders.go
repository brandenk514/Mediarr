package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	rfdom "github.com/brandenk514/mediarr/internal/domains/rootfolders"
	rfsvc "github.com/brandenk514/mediarr/internal/services/rootfolders"
	"github.com/brandenk514/mediarr/internal/settings"
)

// RootFolderDeps bundles what the root-folder registry endpoints need.
type RootFolderDeps struct {
	Svc *rfsvc.Service
}

// SetRootFolders wires the root-folder registry endpoints (P2 parity: the
// Add-New flow and Library Import consume the registry). Called after
// construction, mirroring SetTV.
func (s *Server) SetRootFolders(deps *RootFolderDeps) {
	s.rootFolderDeps = deps
	s.register("GET /api/v1/root-folders", s.requireAuth(s.ListRootFolders))
	s.register("POST /api/v1/root-folders", s.requireAuth(s.AddRootFolder))
	s.register("PUT /api/v1/root-folders/{id}", s.requireAuth(s.UpdateRootFolder))
	s.register("DELETE /api/v1/root-folders/{id}", s.requireAuth(s.DeleteRootFolder))
}

// rootFolderRequest is the body for POST/PUT /api/v1/root-folders.
type rootFolderRequest struct {
	Path string `json:"path"`
	Name string `json:"name"`
	// L3: per-root default profiles (names; "" = unset / keep stored on PUT).
	DefaultQualityProfile  string `json:"default_quality_profile"`
	DefaultMetadataProfile string `json:"default_metadata_profile"`
	// B5: per-root Calibre Content Server config. Omitted (nil) keeps the
	// stored block on PUT; a blank password inside keeps the stored key
	// (write-only — it is never echoed back).
	Calibre *settings.CalibreSettings `json:"calibre"`
}

// validateRootFolderRequest (L3): when the per-root default profile names are
// non-empty they must reference a seeded profile name — one of the 4 seeded
// music quality profiles (MusicProfileNames) / the 3 seeded metadata
// profiles (MetadataProfileNames). "" = unset is always allowed. (B5) a
// provided Calibre block must pass the Calibre rules (enabled ⇒ host/port/
// username).
func validateRootFolderRequest(req rootFolderRequest) string {
	if req.DefaultQualityProfile != "" && !settings.MusicProfileNames()[req.DefaultQualityProfile] {
		return fmt.Sprintf("unknown default quality profile %q", req.DefaultQualityProfile)
	}
	if req.DefaultMetadataProfile != "" && !settings.MetadataProfileNames()[req.DefaultMetadataProfile] {
		return fmt.Sprintf("unknown default metadata profile %q", req.DefaultMetadataProfile)
	}
	if req.Calibre != nil {
		if msg := req.Calibre.Validate(); msg != "" {
			return msg
		}
	}
	return ""
}

// rootFolderResponse is the wire shape of a root folder: the domain fields
// (the existing capitalized convention) plus the per-root Calibre block with
// a masked password and the computed hard-links rule (B5).
type rootFolderResponse struct {
	rfdom.RootFolder
	// HardlinksDisabled: an enabled per-root Calibre config forces hard
	// links OFF for this root (Calibre needs real files, not hard links).
	HardlinksDisabled bool `json:"HardlinksDisabled"`
}

// toRootFolderResponse builds the wire form of a root folder, masking the
// Calibre password (never echoed) and computing the hard-links rule.
func toRootFolderResponse(rf rfdom.RootFolder) rootFolderResponse {
	out := rootFolderResponse{RootFolder: rf, HardlinksDisabled: rf.HardlinksDisabled()}
	if rf.Calibre != nil {
		c := *rf.Calibre
		c.Password = "" // the stored password is write-only
		out.Calibre = &c
	}
	return out
}

// writeRootFolderError maps registry service errors to statuses.
func writeRootFolderError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, rfdom.ErrEmptyPath):
		status = http.StatusBadRequest
	case errors.Is(err, rfdom.ErrDuplicate):
		status = http.StatusConflict
	case errors.Is(err, rfdom.ErrNotFound), errors.Is(err, sql.ErrNoRows):
		status = http.StatusNotFound
	}
	writeError(w, status, err.Error())
}

// ListRootFolders handles GET /api/v1/root-folders.
func (s *Server) ListRootFolders(w http.ResponseWriter, r *http.Request) {
	all, err := s.rootFolderDeps.Svc.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if all == nil {
		all = []rfdom.RootFolder{}
	}
	// B5: mask each root's Calibre password (never echoed).
	out := make([]rootFolderResponse, 0, len(all))
	for i := range all {
		out = append(out, toRootFolderResponse(all[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

// AddRootFolder handles POST /api/v1/root-folders.
func (s *Server) AddRootFolder(w http.ResponseWriter, r *http.Request) {
	var req rootFolderRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if msg := validateRootFolderRequest(req); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	id, err := s.rootFolderDeps.Svc.Create(rfdom.RootFolder{
		Path:                   req.Path,
		Name:                   req.Name,
		DefaultQualityProfile:  req.DefaultQualityProfile,
		DefaultMetadataProfile: req.DefaultMetadataProfile,
		Calibre:                req.Calibre,
	})
	if err != nil {
		writeRootFolderError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// UpdateRootFolder handles PUT /api/v1/root-folders/{id}. Omitted path/name
// keep their stored values.
func (s *Server) UpdateRootFolder(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req rootFolderRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if msg := validateRootFolderRequest(req); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	rf, err := s.rootFolderDeps.Svc.Update(rfdom.RootFolder{
		ID:                     id,
		Path:                   req.Path,
		Name:                   req.Name,
		DefaultQualityProfile:  req.DefaultQualityProfile,
		DefaultMetadataProfile: req.DefaultMetadataProfile,
		Calibre:                req.Calibre,
	})
	if err != nil {
		writeRootFolderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toRootFolderResponse(*rf))
}

// DeleteRootFolder handles DELETE /api/v1/root-folders/{id}.
func (s *Server) DeleteRootFolder(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.rootFolderDeps.Svc.Delete(id); err != nil {
		writeRootFolderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}
