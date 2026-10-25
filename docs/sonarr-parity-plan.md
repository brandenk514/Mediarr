# Sonarr UI Parity — Drop-in Replacement Plan

> Audited against a live Sonarr v4.0.20.3014 instance (linuxserver.io image)
> on 2026-10-24. Goal: a Sonarr user should land in Mediarr and find the same
> pages, in the same places, with the same muscle memory.

## 1. Goal & scope

Mediarr replaces the ARR stack; **Sonarr is the parity target** because it is
the most feature-dense of the four and the one with the most complex IA
(information architecture). TV gets 1:1 parity first; the same patterns then
port to Movies / Music / Books (P7). Mediarr keeps its extras (cross-kind
dashboard, WebSocket live feeds) — parity means "Sonarr's surface exists and
feels identical", not "nothing else".

## 2. Target: Sonarr IA map (what we must match)

**App shell**
- Top bar (teal accent, ~64px): logo · borderless underline **search** ·
  heart/notifications · profile.
- Left rail (slate, ~210px): **Series** (▶) with sub-items *Add New*,
  *Library Import* · **Calendar** · **Activity** (sub: *Queue*, *History*,
  *Blocklist*) · **Wanted** (sub: *Missing*, *Cutoff Unmet*) · **Settings** ·
  **System** (with orange count badge). Active item = darker row + accent-tint
  icon/label; sub-items reveal under the active section.

**Series (home = the library itself, not a dashboard)**
- 6-column poster grid; each card = poster (2:3, optional red status ribbon)
  over a light footer with two centered lines: *Monitored* (or
  "Monitored <day>") and the *quality profile name*. A–Z quick-jump index on
  the right edge.
- Page toolbar (icon-over-label, vertical dividers): left = *Update All*,
  *RSS Sync* | *Select Series* | *Test Parsing* (context, dimmed when
  inapplicable); right = *Options* | *View ▾* (grid/list) · *Sort ▾* ·
  *Filter ▾* (e.g. Monitored/Unmonitored, status).

**Series detail**
- Toolbar: *Refresh & Scan* · *Search Monitored* · *Preview Rename* ·
  *Manage Episodes* · *History* · *Series Monitoring* (per-season
  monitored + cutoff controls).
- Header: title, badges (runtime, rating %, genre, year range, language,
  network), root path, total size, quality profile, *Monitored*/*Ended*,
  links (external). Overview paragraph + "Metadata is provided by TheTVDB".
- Season accordion: *Season N — downloaded X/Y — size*; rows = SxE, title,
  air date, size, quality, release group, download client, status; specials.

**Add New (two-pane page, not a modal)**
- Left: debounced search → result rows: poster · *Title (year)* · badges
  (rating %, language, studio, genre, season count, status) + overview +
  provider credit. Selected row highlighted.
- Right: description, then *Root Folder* (dropdown; helper "'<Title>'
  subfolder will be created automatically"), *Monitor ▾*, *Quality Profile
  ▾*, *Series Type ▾* (hint "Season and episode numbers (S01E05)"),
  *Season Folder* toggle, *Tags* chip input, checkboxes *Start search for
  missing episodes* / *Start search for cutoff unmet episodes*, green
  *Add <Title>* button.

**Calendar** — toolbar *iCal Link* · *RSS Sync* · *Search for Missing* ·
*Options* · *Filter*; views **Month / Week / Forecast / Day / Agenda**; week
strip (Sun–Sat + *Today*); day columns: show → episode title → SxE(abs) →
time range.

**Activity** — *Queue* (table; *Refresh* · *Grab Selected* · *Remove
Selected*; "Queue is empty" empty state), *History* (event feed),
*Blocklist*.

**Wanted** — tabs *Missing* | *Cutoff Unmet*; toolbar *Search All* ·
*Unmonitor Selected* · *Manual Import* · *Options* · *Filter*; columns:
checkbox · series title · episode · episode title · air date · status;
pagination ("1 / 129 · Total records: 2571").

**Settings** (left section nav, list+form per section, *Show Advanced*
toggle, unsaved-changes bar): 1. Media Management (naming, file management,
root folders) · 2. Profiles (quality/delay/release) · 3. Quality · 4. Custom
Formats · 5. Indexers · 6. Download Clients (incl. remote path mappings) ·
7. Import Lists (from another Sonarr instance / Trakt; list exclusions) ·
8. Connect (notifications, media-server connections, scripts) · 9. Metadata
(10. Metadata Source · 11. Tags · 12. General · 13. UI.

**System** — *Status* (Health table with wiki links, Disk Space, About,
Uptime), *Tasks* (running + scheduled), *Backup*, *Updates*, *Events*,
*Log Files*.

## 3. Gap analysis (Mediarr today → target)

| Area | Mediarr now | Gap |
|---|---|---|
| Shell | Top bar + horizontal nav (Dashboard/Movies/TV/Music/Books/Queue/Settings) | Sonarr shell: accent top bar + left rail, section sub-nav, context toolbars |
| Home | Custom dashboard (cards, feeds, live badge) | Home = per-kind library grid (dashboard kept as extra page) |
| Series list | Expandable table + pagination | 6-up poster grid, A–Z jump, view/sort/filter toolbar, status ribbons, monitored/quality footer |
| Series detail | Table row expansion only | Full detail page: badges header, overview, season accordion, per-episode status, series toolbar |
| Add flow | Search → select → 4 fields (title/year/profile/monitored) | Two-pane Add New: results w/ badges; Root Folder, Monitor, Quality Profile, Series Type, Season Folder, Tags, 2 search-start checkboxes |
| Library Import | — | Wizard: root folders (path/free/unmapped), scan, confirm, report |
| Calendar | — | 5 views + iCal feed |
| Activity | Queue page only; per-item history endpoint | Queue parity + global History feed + Blocklist |
| Wanted | Per-series `/wanted` only | Missing + Cutoff Unmet, Search All, Unmonitor, Manual Import, pagination |
| Settings | One page: download client, library overrides, indexers | 13-section settings IA + dirty-state save bar |
| System | /healthz, /readyz, /metrics only | Status/Health + wiki links, Tasks, Backup, Updates, Events, Log Files |
| Backend (no UI counterpart yet) | — | root folders, tags, series type, season folders, per-item profile assignment, cutoff, blocklist, manual import, rename preview, calendar data, iCal, tasks, logs, backup, notifications |

## 4. Phases

Each phase ends with: OpenAPI spec updated, UI types regenerated (M8
lockstep gate green), vitest + Playwright updated.

### P1 — Shell & design system (UI only)
- Rebuild `Layout`: Sonarr top bar (logo, global search, notifications
  placeholder, profile) + left rail with section sub-navigation and context
  toolbars per page type.
- Design tokens: dark theme, teal accent, slate rail, charcoal content,
  light card footers, red/orange status signals; icon-over-label toolbar
  component with divider groups.
- Shared components: grid/list view toggle, Sort/Filter dropdown menus,
  badge set, modal, settings section layout + unsaved-changes bar,
  A–Z jump index.
- New route tree: `/tv`, `/tv/add`, `/tv/import`, `/tv/:id`,
  `/calendar`, `/activity/{queue,history,blocklist}`,
  `/wanted/{missing,cutoff}`, `/settings/:section`, `/system/:page` (movies/
  music/books mirror later).
- Accept: a Sonarr user finds every page at the same path pattern; existing
  e2e specs updated; no backend change.

### P2 — TV (Series) parity
- Series grid (poster + monitored/quality footer, ribbons, A–Z, toolbar)
  backed by list API + client-side view state.
- Series detail page (badges header, overview + provider credit, season
  accordion with episode table, per-episode monitored, *Search Monitored*,
  series History tab).
- Add New two-pane flow (results w/ badges; Root Folder, Monitor, Quality
  Profile, Series Type, Season Folder, Tags, search-start checkboxes).
- Library Import wizard (phase 1: scan root folder → match → confirm;
  phase 2: full import report).
- **Backend deps:** root-folder registry; `series_type`, `season_folder`,
  `tags` on series; per-series quality-profile assignment; episode search
  trigger endpoint; series-level history (extend existing per-item history);
  library scan endpoint.
- Accept: add-a-series and library-browse flows are indistinguishable in
  layout from Sonarr (video comparison).

### P3 — Activity + Wanted parity
- Queue page: refresh/grab/remove, progress + client state columns.
- History: global event feed (date filter, event-type filter, media link).
- Blocklist: list + unblock + reason.
- Wanted: Missing + Cutoff Unmet tabs, Search All (fan-out to indexers),
  Unmonitor Selected, Manual Import entry, server-side pagination.
- **Backend deps:** blocklist endpoints; manual import endpoint;
  cutoff-unmet computation (needs profile cutoff config, P5); global history
  feed endpoint.

### P4 — Calendar
- Month / Week / Forecast / Day / Agenda views; week strip + Today;
  per-day episode rows (show, title, SxE, time range).
- iCal feed (`/calendar.ics`, auth via token) — importable in Google/Apple
  Calendar.
- **Backend deps:** `/api/v1/calendar?start=&end=` (upcoming monitored air
  dates from provider metadata), iCal generator.

### P5 — Settings parity (13 sections, unified-service adapted)
Ordered by value/risk:
1. **Indexers, Download Clients** — move current settings into section IA
   (list + form, per-indexer options, test, remote path mappings).
2. **Quality + Profiles** — quality levels/sizes, quality profiles, cutoff,
   (delay/release profiles when needed by wanted cutoff).
3. **Tags** — tag CRUD + usage view.
4. **Media Management** — naming templates per kind (TV parity first),
   file management options, root folders (ties to P2 add-flow).
5. **Custom Formats** — score-based rules (parity with Sonarr CF engine).
6. **Connect** — notifications (Jellyfin/Emby/Plex push, webhook),
   "heart" notifications feed in top bar finally lights up.
7. **Metadata + Metadata Source** — .nfo/subtitle file generation on
   import/refresh; per-kind source (TVDB/TMDB/MusicBrainz/OL) selection.
   - **P5.7 — Keyless fallback adapters (subtask).** The provider layer
     (`internal/providers/`) is swappable and keyless where possible
     (OpenLibrary books, MusicBrainz music). Add a per-kind source **list**
     in config (e.g. `tv: [tvdb, tvmaze]`) with a **fallback chain**: keyed
     primary → keyless fallback, and only 503 when *all* configured sources
     fail. New keyless adapters, slotting straight into the existing
     `Result` shape:
     - **TVmaze** (`api.tvmaze.com`, no auth) — search, show details with
       `?embed=episodes`, cast, air dates/schedules. Complete season/episode
       tree; weaker imagery + non-English than TMDB.
     - **Wikidata + Wikipedia** (no key) — external-ID crosswalk
       (TMDB/IMDb/TVDB/Wikidata ids interlinked, great for matching) and
       per-language titles/summaries.
     Net: a **zero-key** fresh install runs Books + Music fully and TV
     fully (metadata-complete); movies are the weak spot (no keyless
     catalog matches TMDB's ratings/posters — unofficial IMDb scraping is a
     ToS risk we don't build on). Keys become an optional quality upgrade,
     not a prerequisite — the drop-in story for someone fleeing Readarr's
     metadata outage: nothing core depends on a source that can be
     abandoned.
8. **Import Lists** — Trakt lists; **import from another Sonarr instance**
   (the drop-in bridge: pulls series/episodes/config via Sonarr's API).
9. **General + UI** — port/SSL/auth (some overlaps with existing app
   config), UI options (default views, date format).
- **Backend deps:** the largest backend chunk — settings endpoints per
  section, config document model (key/value JSON already exists as the
  `settings` table), notification dispatch, metadata file writer, CF
  scorer, and the P5.7 keyless adapters (TVmaze, Wikidata/Wikipedia) with
  per-kind source-list config + fallback chain. Phased so each section
  ships with its UI.

### P6 — System + migration
- *Status*: health table (check name, message, severity, wiki link),
  disk space (configured roots), About (version, uptime, mode).
- *Tasks*: running + scheduled jobs (worker inventory already planned in
  `PLAN.md §3` — expose state).
- *Backup*: export/import config (and optional queue/series export).
- *Updates / Events / Log Files*: changelog, event log (history source),
  streaming log viewer (SSE/WS).
- **Drop-in moment — Sonarr migration:** using the Import Lists section
  (or a dedicated System → Import flow), pull a live Sonarr instance:
  series + seasons/episodes + quality profiles + tags + indexers +
  download client → import into Mediarr with mapping report and dry-run
  mode. Postgres target, idempotent, resumable.
- **Backend deps:** structured health endpoint, task registry exposure,
  backup export/import, log streaming, Sonarr-API importer.
- Accept: `migrate from sonarr.local.uaccloud.com` reproduces the live
  library (series count, episode status, profiles) in Mediarr.

### P7 — Port the patterns to Movies / Music / Books
- Movies (Radarr-style within the same shell: grid → detail with versions/
  releases → add with quality profile + edition), Music (artist/album grid,
  lossless quality), Books (author/title/edition grid).
- Home decision: default landing = TV list (Sonarr users' home); dashboard
  reachable from the top bar / a "Dashboard" rail item (Mediarr extra).
- Accept: all four kinds share the same shell, toolbar, grid/detail,
  add-pane, settings patterns — parity is per-kind mechanical work, not
  redesign.

## 5. Sequencing & risk notes

- **P1–P2 are the visible 80%**: shell + TV make it *feel* like Sonarr.
  Everything after is depth.
- P5/P6 carry the backend weight (settings surface, importer, tasks/logs).
  Keep the M8 lockstep gate green every phase — spec first, UI second.
- Risk: settings parity sprawls. Mitigation: the 13-section IA is a
  *contract*; sections may start with fewer fields than Sonarr but the same
  layout + labels, and fill in per-section PRs.
- Non-goals: no Sonarr-compatible *API* surface (third-party tooling can use
  the OpenAPI surface; we match the UI, not the wire protocol). No
  mobile-first pass (responsive desktop-first, like Sonarr).
