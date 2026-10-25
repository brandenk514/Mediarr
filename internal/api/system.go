package api

import (
	"net/http"

	"github.com/brandenk514/mediarr/internal/system"
)

// SystemDeps bundles what the System area (P6) endpoints need. It grows
// across the P6 waves: Wave 1 adds health/tasks/disk/about, Wave 2 adds
// backup/events/updates/logs, Wave 3 adds the Sonarr migration. A field left
// nil answers 501 (not wired) for its endpoint so a partial boot still works.
type SystemDeps struct {
	// ---- Wave 1: Status foundation ----
	Health *system.Health
	Tasks  *system.TaskRegistry
	About  system.AboutDeps

	// Disk inputs (BuildDisk is pure and run per request).
	MediaRoot    string
	DownloadsDir string
	// RootFolderPaths returns the registered root-folder paths for the disk
	// report (nil = none).
	RootFolderPaths func() []string
}

// SetSystem wires the System area endpoints. Called after construction,
// mirroring the other Set* hooks, so NewServer stays usable for health-only
// boot and tests.
func (s *Server) SetSystem(deps *SystemDeps) {
	s.systemDeps = deps
	s.register("GET /api/v1/system/health", s.requireAuth(s.GetSystemHealth))
	s.register("GET /api/v1/system/tasks", s.requireAuth(s.GetSystemTasks))
	s.register("GET /api/v1/system/disk", s.requireAuth(s.GetSystemDisk))
	s.register("GET /api/v1/system/about", s.requireAuth(s.GetSystemAbout))
}

// systemDepsOr501 returns the wired deps or writes a 501 (not wired).
func (s *Server) systemDepsOr501(w http.ResponseWriter) (*SystemDeps, bool) {
	if s.systemDeps == nil {
		writeError(w, http.StatusNotImplemented, "system endpoints are not wired")
		return nil, false
	}
	return s.systemDeps, true
}

// GetSystemHealth handles GET /api/v1/system/health: the structured health
// table (System > Status). The top-level ok/warning/error counts are what the
// System nav badge reads.
func (s *Server) GetSystemHealth(w http.ResponseWriter, r *http.Request) {
	d, ok := s.systemDepsOr501(w)
	if !ok {
		return
	}
	if d.Health == nil {
		writeError(w, http.StatusNotImplemented, "health checks are not wired")
		return
	}
	writeJSON(w, http.StatusOK, d.Health.Report(r.Context()))
}

// GetSystemTasks handles GET /api/v1/system/tasks: the worker inventory
// (running + scheduled + planned tasks, PLAN.md §3).
func (s *Server) GetSystemTasks(w http.ResponseWriter, r *http.Request) {
	d, ok := s.systemDepsOr501(w)
	if !ok {
		return
	}
	if d.Tasks == nil {
		writeError(w, http.StatusNotImplemented, "task registry is not wired")
		return
	}
	writeJSON(w, http.StatusOK, d.Tasks.Snapshot())
}

// GetSystemDisk handles GET /api/v1/system/disk: usage for the configured
// media roots (media root + downloads dir + registered root folders).
func (s *Server) GetSystemDisk(w http.ResponseWriter, r *http.Request) {
	d, ok := s.systemDepsOr501(w)
	if !ok {
		return
	}
	var roots []string
	if d.RootFolderPaths != nil {
		roots = d.RootFolderPaths()
	}
	writeJSON(w, http.StatusOK, system.BuildDisk(d.MediaRoot, d.DownloadsDir, roots, nil))
}

// GetSystemAbout handles GET /api/v1/system/about: build info, mode and
// uptime (System > Status > About).
func (s *Server) GetSystemAbout(w http.ResponseWriter, r *http.Request) {
	d, ok := s.systemDepsOr501(w)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, system.BuildAbout(d.About))
}
