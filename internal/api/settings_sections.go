package api

import (
	"context"
	"io"
	"net/http"
	"strconv"

	"github.com/brandenk514/mediarr/internal/settings"
)

// TagUsageer answers the tags section's usage view (implemented by
// postgres.TagUsageRepo; nil makes the endpoint 501).
type TagUsageer interface {
	CountByTag(ctx context.Context, label string) (series, episodes int, err error)
}

// SettingsSectionDeps bundles the P5 generic section endpoints (the
// 13-section settings shell reads every section through these) plus the
// tags usage view.
type SettingsSectionDeps struct {
	Store    settings.Store
	TagUsage TagUsageer
}

// SetSettingsSections registers the per-section get/put endpoints and the
// tag-usage endpoint. Call after NewServer; nil deps make the endpoints 501.
func (s *Server) SetSettingsSections(deps *SettingsSectionDeps) {
	s.settingsSectionDeps = deps
	s.register("GET /api/v1/settings/{section}", s.requireAuth(s.GetSettingsSection))
	s.register("PUT /api/v1/settings/{section}", s.requireAuth(s.PutSettingsSection))
	s.register("GET /api/v1/settings/tags/{id}/usage", s.requireAuth(s.GetTagUsage))
}

// GetSettingsSection returns one section document, stored values merged over
// the section defaults, with secrets masked (only *_set presence booleans).
func (s *Server) GetSettingsSection(w http.ResponseWriter, r *http.Request) {
	sec, ok := settings.ParseSection(r.PathValue("section"))
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown settings section; expected one of: "+joinSections())
		return
	}
	if s.settingsSectionDeps == nil {
		writeError(w, http.StatusNotImplemented, "settings are not available")
		return
	}
	raw, err := s.settingsSectionDeps.Store.GetSectionDocument(r.Context(), sec)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read settings section")
		return
	}
	doc, err := sec.DecodeDocument(raw)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "stored settings section is corrupt")
		return
	}
	writeJSON(w, http.StatusOK, sec.MaskDocument(doc))
}

// PutSettingsSection replaces one section document (full document in the
// body; missing fields fall back to the section defaults). A blank secret on
// write keeps the stored value; the response is the saved document, masked.
func (s *Server) PutSettingsSection(w http.ResponseWriter, r *http.Request) {
	sec, ok := settings.ParseSection(r.PathValue("section"))
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown settings section; expected one of: "+joinSections())
		return
	}
	if s.settingsSectionDeps == nil {
		writeError(w, http.StatusNotImplemented, "settings are not available")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	storedRaw, err := s.settingsSectionDeps.Store.GetSectionDocument(r.Context(), sec)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read settings section")
		return
	}
	storedDoc, err := sec.DecodeDocument(storedRaw)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "stored settings section is corrupt")
		return
	}
	incoming, err := sec.DecodeDocument(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	sec.MergeSecrets(storedDoc, incoming)
	if msg := sec.ValidateDocument(incoming); msg != "" {
		writeError(w, http.StatusBadRequest, "invalid settings: "+msg)
		return
	}
	encoded, err := sec.EncodeDocument(incoming)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode settings section")
		return
	}
	if err := s.settingsSectionDeps.Store.PutSectionDocument(r.Context(), sec, encoded); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save settings section")
		return
	}
	writeJSON(w, http.StatusOK, sec.MaskDocument(incoming))
}

// GetTagUsage returns the usage of one tag: series and episodes carrying the
// tag's label. 404 for an unknown tag id.
func (s *Server) GetTagUsage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "tag id must be a positive integer")
		return
	}
	if s.settingsSectionDeps == nil {
		writeError(w, http.StatusNotImplemented, "settings are not available")
		return
	}
	if s.settingsSectionDeps.TagUsage == nil {
		writeError(w, http.StatusNotImplemented, "tag usage is not available")
		return
	}
	stored, err := s.settingsSectionDeps.Store.GetSectionDocument(r.Context(), settings.SectionTags)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read settings section")
		return
	}
	doc, err := settings.SectionTags.DecodeDocument(stored)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "stored settings section is corrupt")
		return
	}
	tags := doc.(*settings.TagsSettings)
	var label string
	found := false
	for _, t := range tags.Tags {
		if int64(t.ID) == id {
			label, found = t.Label, true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "tag not found")
		return
	}
	series, episodes, err := s.settingsSectionDeps.TagUsage.CountByTag(r.Context(), label)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read tag usage")
		return
	}
	writeJSON(w, http.StatusOK, TagUsage{
		ID: id, Label: label, Series: series, Episodes: episodes,
	})
}

// TagUsage is the response of GET /api/v1/settings/tags/{id}/usage.
type TagUsage struct {
	ID       int64  `json:"id"`
	Label    string `json:"label"`
	Series   int    `json:"series"`
	Episodes int    `json:"episodes"`
}

func joinSections() string {
	out := ""
	for i, s := range settings.AllSections {
		if i > 0 {
			out += ", "
		}
		out += string(s)
	}
	return out
}
