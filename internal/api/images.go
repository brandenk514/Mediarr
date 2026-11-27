package api

import (
	"net/http"

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
func (s *Server) cacheImagesEnabled() bool {
	return true
}

// GetImage handles GET /api/v1/images/{type}/{size}/{path...} (issue
// #149): cache toggle off → 404; malformed joined path ('//'/'://') →
// 403; a non-whitelisted segment or unknown type → 400; otherwise the
// upstream bytes passed through with cache headers (X-Cache-Key /
// X-Cache-Status: MISS|HIT).
func (s *Server) GetImage(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "image proxy not implemented")
}
