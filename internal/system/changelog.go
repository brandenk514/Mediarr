package system

import "time"

// ---- P6 System > Updates: the release changelog ----
//
// The Updates page shows the release changelog with the current running
// version highlighted. The changelog is data (not derived from git at
// runtime) so it is stable and offline-friendly: each entry is a version, a
// release date, and a list of human-readable change lines.

// ChangelogEntry is one release (OpenAPI ChangelogEntry).
type ChangelogEntry struct {
	Version string   `json:"version"`
	Date    string   `json:"date"` // YYYY-MM-DD
	Entries []string `json:"entries"`
}

// SystemUpdates is the changelog response (OpenAPI SystemUpdates). Current is
// the running version (the UI highlights the matching entry).
type SystemUpdates struct {
	Current   string           `json:"current"`
	Changelog []ChangelogEntry `json:"changelog"`
}

// ChangelogService serves the release changelog.
type ChangelogService struct {
	// Version is the running build's version (highlighted by the UI).
	Version string
	// Entries is the full changelog, newest release first.
	Entries []ChangelogEntry
	// Now is the timestamp source (used only if a caller wants a relative
	// label; kept for symmetry with the other services).
	Now func() time.Time
}

// NewChangelogService builds a changelog service with the default Mediarr
// release history and the given running version.
func NewChangelogService(version string) *ChangelogService {
	return &ChangelogService{
		Version: version,
		Entries: defaultChangelog(),
	}
}

// Get returns the changelog (newest release first) with the current version.
func (c *ChangelogService) Get() SystemUpdates {
	entries := c.Entries
	if len(entries) == 0 {
		entries = []ChangelogEntry{}
	}
	return SystemUpdates{Current: c.Version, Changelog: entries}
}

// defaultChangelog is the Mediarr release history through P6, newest first.
func defaultChangelog() []ChangelogEntry {
	return []ChangelogEntry{
		{
			Version: "0.6.0",
			Date:    "2026-10-25",
			Entries: []string{
				"System area: Status (structured health, disk space, About/uptime), Tasks (worker inventory), Backup (config export/import), Updates (changelog), Events (history source), Log Files (SSE stream).",
				"Sonarr drop-in migration: pull a live Sonarr instance (series, episodes, quality profiles, tags, indexers, download client) with a mapping report, dry-run, idempotent and resumable import.",
			},
		},
		{
			Version: "0.5.0",
			Date:    "2026-10-18",
			Entries: []string{
				"Settings parity: 13 settings sections (general, UI, quality, profiles, tags, media management, custom formats, connect, metadata, metadata source, import lists, indexers, download clients).",
				"Keyless metadata fallback adapters (tvmaze, wikidata, musicbrainz, openlibrary).",
			},
		},
		{
			Version: "0.4.0",
			Date:    "2026-10-11",
			Entries: []string{
				"Calendar parity: 5 calendar views, week strip, and an iCal feed.",
			},
		},
		{
			Version: "0.3.0",
			Date:    "2026-10-04",
			Entries: []string{
				"Activity + Wanted parity: queue, history, blocklist, wanted, and manual import.",
			},
		},
		{
			Version: "0.2.0",
			Date:    "2026-09-27",
			Entries: []string{
				"TV parity: series, seasons, episodes, and the TV add flow.",
			},
		},
		{
			Version: "0.1.0",
			Date:    "2026-09-20",
			Entries: []string{
				"Foundation: auth, Postgres, Redis, dashboard, and the core add flow.",
			},
		},
	}
}
