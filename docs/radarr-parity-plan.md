# Radarr UI Parity — Drop-in Replacement Plan

> Audited against a live Radarr v6.4.4.10685 instance (linuxserver.io image,
> `.10685-ls317`, .NET 8, SQLite) on 2026-10-24. Library at audit time:
> 650 movies / 640 files / 8.7 TiB, 44 queued, 10 missing. Goal: a Radarr user
> should land in Mediarr and find the same pages, in the same places, with the
> same muscle memory.
>
> **Note:** this is Radarr **v6** — a rebuilt React UI, not the classic v5
> look. It carries new surface (status filter chips, a stats bar, per-card
> hover controls, Discover/Collections as first-class pages) that the older
> Radarr screenshots do not. Parity targets v6.

## 1. Goal & scope

Mediarr replaces the ARR stack. The **Sonarr plan is the shell/design-system
owner** — this plan *reuses sonarr-parity-plan P1 shell* (top bar, left rail,
icon-over-label toolbar groups, badge set, view/sort/filter menus, A–Z jump,
settings section layout + unsaved-changes bar, modal). It does **not** re-spec
the shell. This plan is sized for **Radarr-specific work only**: movie grid +
detail parity (with the multi-edition model), the Add Movie two-pane flow with
edition/quality-profile + minimum-availability, movie calendar, Radarr's 12
settings sections, Collections/Discover, and import-from-Radarr migration.
Where a Radarr page is just the shared Sonarr IA (Activity, Wanted, Calendar,
System, settings pattern), this plan says so and defers to that phase rather
than duplicating it. Mediarr keeps its **4-kind dashboard** (Movies/TV/Music/
Books) as an extra, reachable from the top bar / a rail item.

## 2. Target: Radarr IA map (what we must match)

**App shell** — *reuses sonarr-parity-plan P1 shell* (accent top bar, left
rail, section sub-nav, context toolbars). Radarr specifics that ride on that
shell: top bar = logo · underline **search** · notifications (heart) ·
language/locale glyph · profile; left-rail active item = accent bar + tinted
icon; orange count badges on Activity (44) and System (1).

**Left rail (top-level + sub-items)**
- **Movies** (▶) — home = the library itself
  - *Add New* · *Library Import* · *Collections* · *Discover*
- **Calendar**
- **Activity** (count badge) — sub: *Queue*, *History*, *Blocklist*
- **Wanted** — sub: *Missing*, *Cutoff Unmet*
- **Settings**
- **System** (count badge) — sub: *Status*, *Tasks*, *Backup*, *Updates*,
  *Events*, *Log Files*

**Movies (home = the library itself, not a dashboard)**
- 6-column poster grid. Each card = poster (2:3) + **thin status ribbon**
  (green = monitored/downloaded; red/orange for missing/error) over a light
  footer with two centered lines: *Monitored* and the *quality profile name*
  (e.g. "4K Best Effort"). Titles are read off the poster art (no separate
  title line). **Per-card hover controls** (top-left of poster): *Refresh
  movie* + *Edit Movie* icon buttons. A–Z quick-jump index on the right edge
  (only letters present; # for numerals).
- **Status filter chips** (radarr v6 new): *Downloaded (Monitored)*,
  *Downloaded (Unmonitored)*, *Missing (Monitored)*, *Missing (Unmonitored)*,
  *Queued*, *Unreleased*.
- **Stats bar** (radarr v6 new): *Movies 650 · Movie Files 640 · Monitored
  650 · Unmonitored 0 · Total File Size 8.7 TiB*.
- Page toolbar (icon-over-label, vertical dividers): left = *Update All*,
  *RSS Sync* | *Search All*, *Manual Import* | *Edit Movies* (bulk); right =
  *Options* | *View ▾* (grid/list) · *Sort ▾* · *Filter ▾*.

**Movie detail** (single scrolling page, header card → sections)
- Toolbar: *Refresh & Scan* · *Search Movie* · *Interactive Search* |
  *Preview Rename* · *Manage Files* · *History* | *Edit* · *Delete*.
- Header card: poster + title + prev/next-movie nav arrows; badge row
  (*rating* e.g. PG-13, *year*, *runtime*, external-link); **score badges**
  (TMDB %, IMDb score, Rotten Tomatoes %, Metacritic %); two-row **field
  grid**: *Path*, *Status* (with status bar), *Quality Profile*, *Size* |
  *Collection*, *Original Language*, *Studio*, *Genres*; overview paragraph.
- **Editions** (radarr v6 — *conditional*: renders only when a movie has >1
  edition). Each edition row = edition label, **per-edition monitored toggle**
  ("Monitored, click to unmonitor" / "Unmonitored, click to monitor"),
  quality, size. This is Radarr's multi-release model and its biggest
  structural difference from TV.
- **Files** table: *Relative Path · Video Codec · Audio Info · Size ·
  Languages · Quality · Release Group · Formats* (per-file edit/delete;
  subtitle rows under the same header with *Extension · Type*).
- **Metadata files** table: *Relative Path · Extension · Type* (backdrop,
  folder, landscape, logo, .nfo, .srt).
- **Cast** (name → role), **Crew** (name → job), **Titles** (alternative
  titles, *Type* = Tmdb).

**Add New (two-pane page, not a modal)**
- Left: debounced search (supports `tmdb:<id>` lookup) → result rows: poster ·
  *Title (year)* · badges (rating, runtime, TMDB %, IMDb, language, studio,
  genre, **Links**) + overview. Selected row opens the right pane.
- Right: *Root Folder* (dropdown; helper "'<Title> (year)' subfolder will be
  created automatically" + free space), *Monitor ▾* (**All / Movie Only /
  Unmonitored / All except 3D** — edition-aware), *Minimum Availability ▾*
  (**Announced / Posters / In Production / Released**), *Quality Profile ▾*,
  *Tags* chip input, checkbox *Start search for missing movie*, green *Add
  Movie* button, *More Info* link.

**Library Import** — wizard (root folder → scan → match → confirm → report),
same shape as Sonarr's import wizard (reuse).

**Collections** (radarr v6 first-class page)
- Grid of collection cards: name, "*N missing movie(s) from library*", quality
  profile, root path, genres; each card lists its member movies (with
  monitored/unmonitored variants).

**Discover** (radarr v6 first-class page)
- TMDB-powered recommendations: title, overview, year, studio, genre.

**Calendar** — *reuses sonarr-parity-plan P4 calendar* (shared component):
toolbar *iCal Link* · *RSS Sync* · *Search for Missing* | *Options* ·
*Filter*; views **Today / Month / Week / Forecast / Day / Agenda**; status
filter chips. Data source = movie **release dates** (not TV air dates).

**Activity** — *reuses sonarr-parity-plan P3 IA* (shared toolbar + table):
*Queue* (table *Movie · Year · Languages · Quality · Formats · Time Left ·
Progress*; pagination "1 / 3 · Total records: 44"; empty state), *History*
(event feed *Movie · Languages · Quality · Formats · Date*), *Blocklist*
(empty state "No blocklist items").

**Wanted** — *reuses sonarr-parity-plan P3 IA*: tabs *Missing* | *Cutoff
Unmet*; table *Movie Title · Year · Status*; toolbar *Search All* · *Unmonitor
Selected* · *Manual Import* · *Options* · *Filter*; server-side pagination.

**Settings** (left section nav, list+form per section, *Show Advanced*
toggle, unsaved-changes bar — *reuses sonarr-parity-plan P5 layout*; the 12
sections + subtitles are Radarr-specific):
1. **Media Management** — "Naming and file management settings" (Movie Naming:
   Rename Movies, Replace Illegal Characters, Colon Replacement, Standard
   Movie Format "Movie: Title (2010) Bluray-1080p Proper"; File Management:
   Unmonitor Deleted Movies, …; Root Folders).
2. **Profiles** — "Quality, Language, Delay and Release profiles" (Quality
   Profiles: 4K Best Effort, HDTV/WEB/Bluray/Remux 720p/1080p/2160p, BR-DISK,
   Raw-HD; Delay Profiles; Release Profiles).
3. **Quality** — "Quality sizes and naming" (Quality Definitions table:
   Quality · Title · Size Limit across all levels).
4. **Custom Formats** — "Custom Formats and Settings".
5. **Indexers** — "Indexers and release restrictions" (per-indexer toggles
   RSS / Automatic Search / Interactive Search; Options).
6. **Download Clients** — "Download clients, download handling and remote
   path mappings".
7. **Import Lists** — "Import from another Radarr instance or Trakt lists and
   manage list exclusions" (the drop-in bridge lives here).
8. **Connect** — "Notifications, connections to media servers/players, and
   custom scripts".
9. **Metadata** — "Create metadata files when movies are imported or
   refreshed".
10. **Tags** — "See all tags and how they are used. Unused tags can be
    removed".
11. **General** — "Port, SSL, username/password, proxy, analytics and updates"
    (Host: Port Number, URL Base, Allowed Hosts; SSL; Auth; Proxy; Analytics;
    Updates).
12. **UI** — "Calendar, date and color impaired options".

**System** — *reuses sonarr-parity-plan P6*: *Status* (Health table with wiki
links, Disk Space per root, About: version 6.4.4.10685 / package / .NET /
Docker / DB / migration / appdata / mode / uptime, More Info links), *Tasks*
(scheduled table: Name · Interval · Last Execution · Last Duration · Next
Execution — Application Check Update, Backup, Check Health, Clean Up Recycle
Bin, Housekeeping, Import List Update, … + running), *Backup* (file list:
Name · Size · Time), *Updates* (latest-installed notice + changelog:
Currently Installed / New / Fixed), *Events* (table: Time · Component ·
Message), *Log Files* (file list: Filename · Last Write Time · Download).

## 3. Gap analysis (Mediarr today → target)

| Area | Mediarr now | Gap |
|---|---|---|
| Shell | Top bar + horizontal nav (Dashboard/Movies/TV/Music/Books/Queue/Settings) | *Reuse P1 shell* (Sonarr plan) — no re-spec; Radarr pages slot into it |
| Home | Custom 4-kind dashboard | Home = Movies library grid (dashboard kept as extra page) |
| Movie list | Expandable table + pagination | 6-up poster grid, status ribbons, per-card hover controls, status filter chips, stats bar, A–Z jump, movie toolbar (Update All/RSS Sync/Search All/Manual Import/Edit Movies/Options/View/Sort/Filter) |
| Movie detail | Table row expansion only | Full detail page: badge header + score badges + field grid, **Editions (multi-release) section with per-edition monitor toggles**, Files table with media-info columns (codec/audio/release group/formats), metadata files, cast, crew, alternative titles; detail toolbar (Refresh & Scan/Search Movie/Interactive Search/Preview Rename/Manage Files/History/Edit/Delete) |
| Add flow | Search → select → 4 fields | Two-pane Add: results w/ badges; Root Folder, **Monitor (All/Movie Only/Unmonitored/All except 3D)**, **Minimum Availability (Announced/Posters/In Production/Released)**, Quality Profile, Tags, start-search checkbox |
| Editions/releases model | No edition model for movies (API) | Movie → N editions, each with monitored + quality + files; conditional Editions UI |
| Library Import | — | Wizard (reuse Sonarr import pattern) |
| Collections | — | Collection cards (missing-movie count, quality, root, genres, members) |
| Discover | — | TMDB recommendations feed |
| Calendar | — | *Reuse P4* (5 views + iCal) fed by movie release dates |
| Activity | Queue page only; per-item history endpoint | *Reuse P3* Queue parity + global History feed + Blocklist |
| Wanted | Per-movie `/wanted` only | *Reuse P3* Missing + Cutoff Unmet, Search All, Unmonitor, Manual Import, pagination |
| Settings | One page: download client, library overrides, indexers | *Reuse P5* 12-section IA + dirty-state save bar (sections/fields are Radarr-specific) |
| System | /healthz, /readyz, /metrics only | *Reuse P6* Status/Health + wiki links, Tasks, Backup, Updates, Events, Log Files |
| Backend (no UI counterpart yet) | — | edition model, file media info (codec/audio), per-edition monitor, minimum availability, collections, discover feed, movie calendar (release dates), movie-specific quality profiles, tasks/logs/backup, Radarr-API importer |

## 4. Phases

Each phase ends with: OpenAPI spec updated, UI types regenerated (M8
lockstep gate green), vitest + Playwright updated. Phases labelled **R**
(Radarr) to distinguish from the Sonarr plan's **P**; parallelizable phases
are marked.

### R1 — Movie shell parity: library grid + detail (UI + edition model)
- *Depends on / reuses:* **P1 shell** (sonarr-parity-plan) — top bar, left
  rail, toolbar, A–Z, badges, view/sort/filter, card grid scaffold.
- Movies library: 6-up poster grid, status ribbons, per-card hover controls
  (Refresh/Edit), status filter chips, stats bar, movie toolbar.
- Movie detail: badge header + score badges + field grid, overview,
  **conditional Editions section** (per-edition monitored toggle, quality,
  size), Files table (media-info columns), metadata files, cast/crew/
  alternative titles, detail toolbar.
- **Backend deps:** **edition model** (movie → N editions, each with
  `monitored`, `quality`, files) — the single biggest Radarr-only backend
  chunk; file media info (video codec, audio info, release group, formats);
  per-edition monitor toggle endpoint; media enrichment (score badges,
  cast/crew, alt titles).
- Accept: a Radarr user browses the grid and opens a detail page
  indistinguishable from Radarr (video comparison).
- **Parallelizable with Sonarr P2** (TV detail) — shared detail-page scaffold,
  section components, toolbar.

### R2 — Add Movie two-pane (edition + quality profile)
- Left: debounced search (`tmdb:<id>` aware) → result rows with badges.
  Right: Root Folder (auto-subfolder + free space), **Monitor (All / Movie
  Only / Unmonitored / All except 3D)**, **Minimum Availability (Announced /
  Posters / In Production / Released)**, Quality Profile, Tags, start-search
  checkbox, Add Movie.
- **Backend deps:** add-movie endpoint accepting `monitor` (edition-aware),
  `minimum_availability`, `quality_profile_id`, `tags`; TMDB search
  (extends existing `/search/movies`); root folders.
- **Parallelizable with Sonarr P2** (shared two-pane Add component; the
  field set differs — edition/availability vs series type/season folder).

### R3 — Collections + Discover (Radarr-specific)
- Collections: collection cards (name, missing-movie count, quality profile,
  root path, genres, member list with monitored variants).
- Discover: TMDB recommendations (title, overview, year, studio, genre).
- **Backend deps:** collection grouping (TMDB collection relation), discover /
  recommendation feed endpoint.
- **No Sonarr equivalent** — standalone; can run independently of P2–P6.

### R4 — Movie Calendar
- *Depends on / reuses:* **P4 calendar** (shared 5-view + iCal component).
  Movie variant fed by **release dates** (not air dates); status chips.
- **Backend deps:** `/api/v1/calendar?kind=movie&start=&end=` (upcoming
  monitored release dates), iCal generator.
- **Parallelizable with Sonarr P4** (same component, different data source).

### R5 — Radarr Settings (12 sections, unified-service adapted)
- *Depends on / reuses:* **P5 settings IA** (list+form per section, Show
  Advanced, unsaved-changes bar). The 12 Radarr sections + subtitles as in §2.
  Ordered by value/risk: (1) Indexers + Download Clients — move current
  settings in, add per-indexer RSS/Auto/Interactive toggles + remote path
  mappings; (2) Quality + Profiles — 14 movie quality profiles, quality
  definitions, delay/release profiles; (3) Tags; (4) Media Management —
  movie naming template ("Movie: Title (2010) Bluray-1080p Proper"), file
  management, root folders; (5) Custom Formats; (6) Connect; (7) Metadata;
  (8) Import Lists (Trakt + **import from Radarr**); (9) General + UI.
- **Backend deps:** settings endpoints per section (many kind-agnostic and
  shared with P5: quality, CF, indexers, download clients, tags, metadata,
  general, UI); per-kind naming + quality-profile store.
- **Parallelizable with Sonarr P5** (shared section layout + most sections;
  per-kind naming/quality differ).

### R6 — System + Radarr migration
- *Depends on / reuses:* **P6 System** (Status/Tasks/Backup/Updates/Events/
  Log Files — shared).
- **Drop-in moment — Radarr migration:** using Import Lists (or a dedicated
  System → Import flow), pull a live Radarr instance: **movies + editions +
  quality profiles + tags + indexers + download client + settings** → import
  into Mediarr with a mapping report and dry-run mode. Postgres target,
  idempotent, resumable.
- **Backend deps:** structured health endpoint, task registry exposure, backup
  export/import, log streaming, **Radarr-API importer** (movies with editions,
  quality profiles, tags, settings).
- Accept: `migrate from radarr.local.uaccloud.com` reproduces the live library
  (650 movies, editions, 8.7 TiB, 14 quality profiles, tags) in Mediarr.
- **Parallelizable with Sonarr P6** (shared System + importer harness; the
  importer's source API differs — Radarr vs Sonarr).

### R7 — 4-kind dashboard retained (Mediarr extra)
- Not part of Radarr parity. Keep the cross-kind dashboard (Movies/TV/Music/
  Books cards, live feeds) reachable from the top bar / a rail item; default
  landing = Movies list (Radarr users' home).

## 5. Sequencing & risk notes

- **R1–R2 are the visible 80%**: movie grid + detail + Add make it *feel*
  like Radarr. Everything after is depth.
- **The edition model is the one true Radarr-specific backend chunk** (R1).
  TV has no equivalent (seasons/episodes), so it cannot ride on Sonarr's
  backend. Budget it early; everything else (Activity/Wanted/Calendar/System/
  Settings IA) reuses Sonarr phases P3–P6.
- **Maximize parallelism with the Sonarr plan:** R1∥P2, R2∥P2, R4∥P4, R5∥P5,
  R6∥P6. Only R3 (Collections/Discover) and the edition model are Radarr-only
  net-new work.
- Keep the **M8 lockstep gate green every phase** — spec first, UI second.
- Risk: Radarr **v6** surface (status chips, stats bar, per-card controls,
  Discover/Collections) is newer than most reference material. Mitigation:
  treat §2's IA map as the *contract*; verify each page against the live
  instance, not stale screenshots.
- Risk: settings parity sprawls. Mitigation: the 12-section IA is a
  *contract*; sections may start with fewer fields than Radarr but the same
  layout + labels, filled in per-section PRs.
- **Non-goals:** no Radarr-compatible *wire* API (third-party tooling uses
  the OpenAPI surface; we match the UI, not the wire protocol). No
  mobile-first pass (responsive desktop-first, like Radarr).
