// Same-origin UI serving (issue #87).
//
// The built Vite UI (the #88 asset contract: /home/nonroot/ui in the image)
// is served by the same server as the API, with SPA fallback for
// client-side routing. The catch-all lives here so routing changes for the
// UI stay in one file.
package api

import (
	"net/http"
)

// UIDeps bundles what same-origin UI serving (#87) needs.
type UIDeps struct {
	// Dir is the asset root of the built UI (the Vite dist) —
	// /home/nonroot/ui in the image (#88). A missing (or empty) Dir makes
	// every UI path 404, which is the dev-without-a-build behaviour; API and
	// health routes are unaffected either way.
	Dir string
}

// SetUI wires the UI catch-all onto the mux. Call it after every
// API/health/metrics route is registered: the literal "/" pattern is the
// mux's lowest-priority match, so only unmatched requests (UI paths, and
// unknown /api/* paths) reach it.
//
// NOTE (TDD red state): this is the minimal hook the failing suite in
// ui_test.go needs to compile; it answers 404 to every request. The real
// static-file handler with SPA fallback, containment, and method semantics
// lands in the next commit.
func (s *Server) SetUI(deps *UIDeps) {
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
}
