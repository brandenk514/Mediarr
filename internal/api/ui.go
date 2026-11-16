// Same-origin UI serving (issue #87).
//
// The built Vite UI (the #88 asset contract: /home/nonroot/ui in the image)
// is served by the same server as the API, with SPA fallback for
// client-side routing. The catch-all lives here so routing changes for the
// UI stay in one file.
package api

import (
	"net/http"
	"os"
	"path"
	"strings"
)

// reservedUIPrefixes are the route families the UI catch-all must never
// absorb. Membership is checked at path-SEGMENT boundaries: "/apiary" is a
// UI path, not part of /api, and "/apiv2" is one too — only "/api" itself
// and "/api/..." are reserved.
var reservedUIPrefixes = []string{"/api", "/healthz", "/readyz", "/version", "/metrics"}

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
func (s *Server) SetUI(deps *UIDeps) {
	s.mux.HandleFunc("/", s.serveUI(deps.Dir))
}

// isReservedUIPath reports whether p is at or under one of the reserved
// route families, at a path-segment boundary.
func isReservedUIPath(p string) bool {
	for _, prefix := range reservedUIPrefixes {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

// hasFileExtension reports whether the final path segment carries an
// extension (a dot not at the segment's first position). That marks the
// request as an asset reference: a missing asset is a 404, whereas a
// missing non-file-like path is a client-routing deep link that falls back
// to index.html.
func hasFileExtension(seg string) bool {
	return strings.IndexByte(seg, '.') > 0
}

// serveUI builds the catch-all handler for asset root dir.
//
// Contract (issue #87):
//   - reserved prefixes (/api/*, /healthz, /readyz, /version, /metrics —
//     segment-boundary) never fall through: unknown /api paths answer a
//     non-HTML JSON 404, never index.html;
//   - GET/HEAD only — anything else is 405 (the SPA is never returned for
//     non-file methods);
//   - existing regular files are served with their MIME type (HEAD mirrors
//     GET minus the body); directories are 404 — never listed;
//   - a trailing slash never serves file content and never lists:
//     /assets/ (an existing directory) and /assets/app.js/ (an existing
//     file) are 404, but /movies/ is a client-routing deep link that
//     falls back to index.html;
//   - a missing file-like path (extension in the last non-empty segment)
//     is 404, not the SPA.
//   - containment: every file open goes through os.OpenInRoot, and the
//     request path is validated before the open (no backslash, no NUL, no
//     "."/".." segments) so traversal, encoded traversal (%2e%2e, %252f),
//     and symlink escapes can never leave dir.
//
// Path canonicalisation (// double slashes, /a/../b) is owned by
// net/http.ServeMux — it issues the canonical redirect; this handler never
// redirects. (Encoded dot-dot, %2e%2e, is NOT canonicalised by the mux and
// reaches the handler as literal ".." segments: the segment check above is
// what keeps those from being opened.)
func (s *Server) serveUI(dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if dir == "" {
			// An empty root would make OpenInRoot a plain CWD-relative
			// open; refuse the whole UI surface instead.
			http.NotFound(w, r)
			return
		}
		p := r.URL.Path // decoded; the query is already split by net/url

		// 1. Reserved families: unknown API/health/metrics paths are API
		//    errors, never the SPA.
		if isReservedUIPath(p) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}

		// 2. Method semantics: the SPA fallback (and file serving) is
		//    GET/HEAD only.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// 3. Reject shapes the mux's canonicalisation does not cover. A
		//    backslash is not a legal path character (RFC 9110 pchar):
		//    browsers normalise it to "/", raw clients don't, and /api\...
		//    would otherwise slip past the segment-boundary check above.
		//    NUL is never a filename. Encoded dot-dot (%2e%2e) decodes to
		//    literal ".." segments that must 404, not be opened.
		if strings.ContainsAny(p, "\\\x00") {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		for _, seg := range strings.Split(p, "/") {
			if seg == ".." || seg == "." {
				http.NotFound(w, r)
				return
			}
		}

		// 4. Serve the file if it exists (directories are 404: no
		//    listing). A trailing slash names a directory, so the open
		//    probes the trimmed path: an existing directory is never
		//    listed, and a regular file is not addressable with a
		//    trailing slash either — both 404.
		rel := strings.TrimPrefix(p, "/")
		if rel != "" {
			probe := rel
			if strings.HasSuffix(rel, "/") {
				probe = strings.TrimSuffix(rel, "/")
			}
			if f, err := os.OpenInRoot(dir, probe); err == nil {
				defer f.Close()
				if st, serr := f.Stat(); serr == nil {
					if st.Mode().IsRegular() && !strings.HasSuffix(rel, "/") {
						// ServeContent handles HEAD (headers only),
						// conditional requests, and the Content-Type
						// from the file extension.
						http.ServeContent(w, r, st.Name(), st.ModTime(), f)
						return
					}
				}
				// A directory (never listed), a stat failure, or a
				// regular file asked with a trailing slash.
				http.NotFound(w, r)
				return
			}
			// Missing (or unopenable, e.g. a rejected escape): a
			// file-like name is a dead asset reference — 404, not the
			// SPA. path.Base ignores trailing slashes, so
			// /assets/app.js/ is file-like ("app.js") while /movies/ is
			// a client-routing deep link that falls through.
			if hasFileExtension(path.Base(rel)) {
				http.NotFound(w, r)
				return
			}
		}

		// 5. SPA fallback: the entry point, or 404 when the asset root (and
		//    its index.html) is missing — the dev-without-a-build case.
		f, err := os.OpenInRoot(dir, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		st, serr := f.Stat()
		if serr != nil || !st.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, "index.html", st.ModTime(), f)
	}
}
