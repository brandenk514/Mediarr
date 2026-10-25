package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	hfdom "github.com/brandenk514/mediarr/internal/domains/historyfeed"
	"github.com/brandenk514/mediarr/internal/services/historyfeed"
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

	// ---- Wave 2: System depth (backup / events / updates / logs) ----
	// Backup is the config export/import service (nil = backup not wired).
	Backup *system.BackupService
	// Events is the global history feed service (the Events page source;
	// nil = events not wired).
	Events *historyfeed.Service
	// Changelog serves the Updates page (nil = updates not wired).
	Changelog *system.ChangelogService
	// Logs is the streaming log viewer source (nil = logs not wired).
	Logs *system.LogService

	// ---- Wave 3: Sonarr drop-in migration ----
	Migration *system.Migrator
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
	// Wave 2: System depth.
	s.register("GET /api/v1/system/backup", s.requireAuth(s.GetSystemBackup))
	s.register("POST /api/v1/system/backup", s.requireAuth(s.PutSystemBackup))
	s.register("GET /api/v1/system/events", s.requireAuth(s.GetSystemEvents))
	s.register("GET /api/v1/system/updates", s.requireAuth(s.GetSystemUpdates))
	s.register("GET /api/v1/system/logs", s.requireAuth(s.GetSystemLogs))
	// Wave 3: Sonarr drop-in migration.
	s.register("POST /api/v1/system/migration", s.requireAuth(s.RunMigration))
	s.register("GET /api/v1/system/migration/status", s.requireAuth(s.GetMigrationStatus))
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

// ---- Wave 2: System depth (backup / events / updates / logs) ----

// GetSystemBackup handles GET /api/v1/system/backup: the configuration export
// (System > Backup). ?include_queue / ?include_series attach optional
// snapshots.
func (s *Server) GetSystemBackup(w http.ResponseWriter, r *http.Request) {
	d, ok := s.systemDepsOr501(w)
	if !ok {
		return
	}
	if d.Backup == nil {
		writeError(w, http.StatusNotImplemented, "backup is not wired")
		return
	}
	q := r.URL.Query()
	doc, err := d.Backup.Export(r.Context(), q.Get("include_queue") == "true", q.Get("include_series") == "true")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to export backup")
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// backupImportRequest is the POST body (OpenAPI BackupImportRequest).
type backupImportRequest struct {
	Document      *system.BackupDocument `json:"document"`
	IncludeQueue  bool                   `json:"include_queue"`
	IncludeSeries bool                   `json:"include_series"`
}

// PutSystemBackup handles POST /api/v1/system/backup: the configuration
// import (System > Backup). A malformed/invalid document is a 400.
func (s *Server) PutSystemBackup(w http.ResponseWriter, r *http.Request) {
	d, ok := s.systemDepsOr501(w)
	if !ok {
		return
	}
	if d.Backup == nil {
		writeError(w, http.StatusNotImplemented, "backup is not wired")
		return
	}
	var req backupImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Document == nil {
		writeError(w, http.StatusBadRequest, "document is required")
		return
	}
	res, err := d.Backup.Import(r.Context(), req.Document, req.IncludeQueue, req.IncludeSeries)
	if err != nil {
		if errors.Is(err, system.ErrInvalidBackup) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to import backup")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// GetSystemEvents handles GET /api/v1/system/events: the event log (the
// history source) for System > Events. ?since (RFC 3339), ?event
// (case-insensitive), ?limit (default 50, clamped 1..200).
func (s *Server) GetSystemEvents(w http.ResponseWriter, r *http.Request) {
	d, ok := s.systemDepsOr501(w)
	if !ok {
		return
	}
	if d.Events == nil {
		writeError(w, http.StatusNotImplemented, "events are not wired")
		return
	}
	q := r.URL.Query()

	var since *time.Time
	if v := q.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since (want RFC 3339)")
			return
		}
		since = &t
	}
	limit := 50
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "invalid limit (want a positive integer)")
			return
		}
		limit = n
	}

	items, err := d.Events.List(r.Context(), hfdom.Filter{Since: since, Event: q.Get("event"), Limit: limit})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load events")
		return
	}
	// dom.Item's JSON shape matches the OpenAPI GlobalHistoryItem exactly;
	// wrap in the SystemEvents envelope.
	writeJSON(w, http.StatusOK, map[string]any{"events": items})
}

// GetSystemUpdates handles GET /api/v1/system/updates: the release changelog
// with the current version (System > Updates).
func (s *Server) GetSystemUpdates(w http.ResponseWriter, r *http.Request) {
	d, ok := s.systemDepsOr501(w)
	if !ok {
		return
	}
	if d.Changelog == nil {
		writeError(w, http.StatusNotImplemented, "updates are not wired")
		return
	}
	writeJSON(w, http.StatusOK, d.Changelog.Get())
}

// GetSystemLogs handles GET /api/v1/system/logs: a Server-Sent-Events stream
// of recent application log lines (System > Log Files). The first frames are
// the ring-buffer backlog, then live lines are pushed as they occur.
func (s *Server) GetSystemLogs(w http.ResponseWriter, r *http.Request) {
	d, ok := s.systemDepsOr501(w)
	if !ok {
		return
	}
	if d.Logs == nil {
		writeError(w, http.StatusNotImplemented, "log streaming is not wired")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Backlog first, then a live subscription.
	writeSSELogFrame(w, flusher, "backlog", d.Logs.Recent())
	ch, cancel := d.Logs.Subscribe()
	defer cancel()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case line, more := <-ch:
			if !more {
				return
			}
			writeSSELogFrame(w, flusher, "log", []system.LogLine{line})
		}
	}
}

// writeSSELogFrame writes one SSE frame: a named event whose data is a JSON
// array of log lines.
func writeSSELogFrame(w http.ResponseWriter, flusher http.Flusher, event string, lines []system.LogLine) {
	if lines == nil {
		lines = []system.LogLine{}
	}
	data, _ := json.Marshal(lines)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	flusher.Flush()
}
