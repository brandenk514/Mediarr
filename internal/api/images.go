package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/brandenk514/mediarr/internal/services/imageproxy"
	"github.com/brandenk514/mediarr/internal/settings"
)

// ImagesDeps bundles the same-origin image proxy route (issue #149).
//
// Proxy is the translated Seerr image proxy (upstream allowlist, segment
// whitelist, on-disk cache, per-host rate limit). Settings, when non-nil,
// is the settings store the cache toggle is read from on every request:
// the general section's cache_images boolean (default ON — a nil store
// or a never-saved document means the cache is enabled, the fork's
// main.cacheImages default).
type ImagesDeps struct {
	Proxy    *imageproxy.Proxy
	Settings settings.Store
}

// SetImages wires the image proxy route. It is PUBLIC (registered like
// /version, without the bearer gate): #155's login backdrop consumes it
// pre-auth, and the route's safety comes from the upstream-host allowlist,
// the segment whitelist, the rate limit, and the cache cap — not from auth
// (orchestrator decision #1, work order #40).
func (s *Server) SetImages(deps *ImagesDeps) {
	s.imagesDeps = deps
	s.register("GET /api/v1/images/{type}/{size}/{path...}", s.GetImage)
}

// cacheImagesEnabled reads the general section's cache_images toggle,
// defaulting ON: a nil store, an unreadable/never-saved document, or a
// document without the field all mean enabled (the fork's default).
func (s *Server) cacheImagesEnabled(ctx context.Context) bool {
	d := s.imagesDeps
	if d == nil || d.Settings == nil {
		return true
	}
	doc, err := d.Settings.GetSectionDocument(ctx, settings.SectionGeneral)
	if err != nil || doc == nil {
		return true
	}
	var g struct {
		CacheImages *bool `json:"cache_images"`
	}
	if json.Unmarshal(doc, &g) != nil {
		return true
	}
	if g.CacheImages == nil {
		return true
	}
	return *g.CacheImages
}

// GetImage handles GET /api/v1/images/{type}/{size}/{path...} (issue
// #149): cache toggle off → 404; malformed joined path ('//'/'://') →
// 403; a non-whitelisted segment or unknown type → 400; otherwise the
// upstream bytes passed through with cache headers (X-Cache-Key /
// X-Cache-Status: MISS|HIT).
func (s *Server) GetImage(w http.ResponseWriter, r *http.Request) {
	d := s.imagesDeps
	if d == nil || d.Proxy == nil {
		writeError(w, http.StatusServiceUnavailable, "image proxy not configured")
		return
	}
	if !s.cacheImagesEnabled(r.Context()) {
		writeError(w, http.StatusNotFound, "image cache disabled")
		return
	}
	// Rebuild the joined "/{size}/{path...}" form the proxy validates.
	joined := "/" + r.PathValue("size") + "/" + r.PathValue("path")
	res, err := d.Proxy.Get(r.Context(), r.PathValue("type"), joined)
	switch {
	case errors.Is(err, imageproxy.ErrForbidden):
		writeError(w, http.StatusForbidden, "invalid url for image proxy")
		return
	case errors.Is(err, imageproxy.ErrInvalidPath), errors.Is(err, imageproxy.ErrUnsupportedType):
		writeError(w, http.StatusBadRequest, "invalid image request")
		return
	case errors.Is(err, imageproxy.ErrUpstreamNotFound):
		writeError(w, http.StatusNotFound, "upstream image not found")
		return
	case errors.Is(err, imageproxy.ErrUpstream):
		writeError(w, http.StatusBadGateway, "failed to load image")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "image proxy failed")
		return
	}
	status := "HIT"
	if res.CacheMiss {
		status = "MISS"
	}
	w.Header().Set("Content-Type", res.ContentType)
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(res.MaxAge))
	w.Header().Set("X-Cache-Key", res.CacheKey)
	w.Header().Set("X-Cache-Status", status)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(res.Data)
}
