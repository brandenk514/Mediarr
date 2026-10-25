# Lidarr UI Parity — Drop-in Replacement Plan (Music)

> Audited against a live Lidarr v3.1.0.4875 instance (linuxserver.io image
> `3.1.0.4875-ls41`, .NET 8, Sqlite 3.50.4) on 2026-10-24. Goal: a Lidarr user
> should land in Mediarr and find the same pages, in the same places, with the
> same muscle memory. The shared shell/design system already comes from the
> Sonarr plan (**reuses sonarr-parity-plan P1 shell**) — this plan sizes
> Lidarr-specific work only.

## 1. Goal & scope

Mediarr replaces the ARR stack; **Lidarr is the parity target for Music**
(artist/album library, lossless quality, album-scoped monitoring). The shell,
toolbar components, Activity/Wanted IA, calendar, settings section layout, and
System pages are built once in the Sonarr plan (P1–P6); music reuses them and
only fills in the music-specific layers. Phases below (L1–L5) are sized for
Lidarr-specific work and can run in **parallel with Sonarr phases P3–P6**
wherever they share a backend dependency (blocklist, calendar, settings
sections, importer, tasks/logs) — each shared dependency is listed once in
both plans. Mediarr keeps its 4-kind dashboard as an extra (top bar / rail
item); parity means "Lidarr's surface exists and feels identical", not
"nothing else".

Non-goals: no Lidarr-compatible wire API (OpenAPI surface is the contract);
desktop-first responsive (like Lidarr).

## 2. Target: Lidarr IA map (what we must match)

**App shell** (reuses sonarr-parity-plan P1 shell; notes are music deltas)
- Top bar (Lidarr brand green in this install; accent color per instance):
  logo · borderless underline **search** · heart/notifications · profile.
- Left rail: **Library** (▶) with sub-items *Add New*, *Unmapped Files* ·
  **Calendar** · **Activity** (sub: *Queue*, *History*, *Blocklist*, orange
  count badge) · **Wanted** (sub: *Missing*, *Cutoff Unmet*) · **Settings** ·
  **System** (orange count badge). Active item = darker row + accent icon/
  label + accent edge bar; sub-items reveal under the active section.

**Artists (home = the library itself, not a dashboard)**
- 6-column grid; card = square cover (146px) over a thin **colored status
  ribbon** + light footer panel with three centered lines: *artist name*
  (bold), *Monitored*, *quality profile name* (e.g. "Lossless Best Effort").
  Red corner fold ribbon on some covers (status flag).
- Ribbon semantics (library footer legend): *Continuing (all tracks
  downloaded)*, *Ended (all tracks downloaded)*, *Missing Tracks (artist
  monitored)*, *Missing Tracks (artist not monitored)*, *Downloading*.
- Page footer **status bar**: counts — Artist, Inactive, Continuing,
  Monitored, Unmonitored, Tracks, Files, Total File Size (Lidarr-specific;
  Sonarr has no equivalent — keep it for music).
- A–Z quick-jump index on the right edge (PageJumpBar, only letters with
  entries).
- Page toolbar (icon-over-label, vertical dividers): left = *Update All*,
  *RSS Sync* | *Select Artists* |; right = *Options* | *View ▾* (grid/list) ·
  *Sort ▾* · *Filter ▾* (monitored/unmonitored, status groups with live
  counts).

**Artist detail**
- Toolbar: *Refresh & Scan* · *Search Monitored* · *Interactive Search* |
  *Preview Rename* · *Preview Retag* · *Manage Tracks* · *History* · *Manual
  Import* | *Artist Monitoring* · *Edit* · *Delete*; far right *Expand All*.
  (vs Sonarr: *Interactive Search*, *Manage Tracks*, *Preview Retag* are
  music-only; *Artist Monitoring* ≙ Sonarr's *Series Monitoring*.)
- Header: banner background (artist photo), artist name, *♥ 89%* + *genre*,
  green pill badges: root path · total size · quality profile · *Monitored* ·
  *Continuing* · *Links* (external); circular prev/up/next artist paging
  arrows at banner right edge.
- Overview paragraph (MusicBrainz bio).
- **Albums accordion grouped by album type** — e.g. *Album — 3/9 — 444.7
  MiB* (red count badge = downloaded/total, size). Expanding a group reveals
  album rows: *Title · Release Date · Rating · Status*. Expanding an album
  reveals the album sub-header (duration, rating, genre, release date, size,
  Monitored, Links) → **disc sections** (*CD 1 — 12/12*) → **track table**:
  *Track · Title · Duration · Audio Info · Status* (Audio Info = "FLAC - 2.0 -
  994 kbps - 44.1kHz - 16bit"; status = quality or empty for missing).
- Helper line: "Missing Albums, Singles, or Other Types? Modify or create a
  new **Metadata Profile** or manually **Search** for new items!"

**Add New Artist** (`/add/search` page + modal, NOT Sonarr's persistent
right pane — Lidarr 3.x)
- Search page: placeholder "eg. Breaking Benjamin,
  lidarr:<musicbrainz-id>"; helper text explains MusicBrainz-ID lookup.
  Debounced results: **artist rows** (artwork · name · *♥ %* · *Group* badge ·
  overview) and **release-group rows** (artwork · title · *♥ %* · year ·
  *N releases* · type chips: Album/Single/EP/Compilation/Live/Remix/Broadcast/
  Demo/Other/DJ-mix). Clicking a result already in the library jumps to its
  artist page.
- Selecting a new artist opens an **"Add New Artist" modal** (close ×;
  scrollable): left = artist photo; right = name, then in order:
  1. *Root Folder* — "[Music Library] /music" → /Khruangbin · "13 TiB Free" ·
     helper "'Khruangbin' subfolder will be created automatically"
  2. *Monitor ⓘ* — All Albums / Future Albums / Missing Albums / Existing
     Albums / First Album / Lastest Album / None — helper "Which albums should
     be monitored after the artist is added (one-time adjustment)"
  3. *Monitor New Albums ⓘ* — All Albums — helper "Which new albums should be
     monitored"
  4. *Quality Profile* — (e.g. Lossless Best Effort)
  5. *Tags* — chip input
  6. footer: ☐ *Start search for missing albums* · green **Add <Artist>**
- **Edit Artist** modal (from detail toolbar): ☐ *Monitored* ("Download
  monitored albums from this artist") · *Monitor New Albums* · *Quality
  Profile* · *Metadata Profile* (helper "Metadata Profile list items should
  be added with") · *Path* · *Tags* · [Delete | Cancel · Save].

**Unmapped Files** (music's import path; there is no separate Sonarr-style
"Library Import" wizard in Lidarr)
- Toolbar: *Add Missing* · *Delete Selected* · *Options*.
- Columns: *Path · Size · Date Added · Quality* (Quality shows per-file
  lossy/lossless string, e.g. MP3-160, AAC-256, FLAC).

**Calendar** — **Lidarr DOES have one** (upcoming release dates of monitored
artists; music has no air dates, but Lidarr ships the widget). Same IA as
Sonarr: toolbar *iCal Link* · *RSS Sync* · *Search for Missing* · *Options* ·
*Filter*; views **Month / Week / Forecast / Day / Agenda**; week strip
(Sun–Sat + *Today*).

**Activity** — *Queue*: toolbar *Refresh* · *Grab Selected* · *Remove
Selected* · *Options* · *Filter*; columns (checkbox) · *Artist* · *Album
Title* · *Quality* · *Formats* · *Time Left* · *Progress*; pagination ("1 / 4
· Total records: 70"). *History*: toolbar *Refresh* · *Options* · *Filter*;
columns *Artist · Album Title · Track Title · Quality · Formats · Date*;
relative dates ("Yesterday"); pagination. *Blocklist*: *Remove Selected* ·
*Clear* · *Options*; empty state "No history blocklist".

**Wanted** — tabs *Missing* | *Cutoff Unmet*; toolbar *Search Selected* ·
*Unmonitor Selected* · *Search All* · *Manual Import* (Missing only) ·
*Options* · *Filter*; columns: checkbox · *Artist Name* · *Album Title* ·
*Album Type* (Album/EP/Single/…) · *Release Date*; pagination ("1 / 156 ·
Total records: 3105").

**Settings** (left section nav; status bar shows current background task
e.g. "Identifying album 352/530"; *Show Advanced* toggle; unsaved-changes
bar "No Changes"/dirty). 12 sections, exact names + subtitles:
1. **Media Management** — "Naming, file management settings and root
   folders" (Root Folders with **per-root default Quality Profile + Metadata
   Profile**; Track Naming: rename toggle, illegal-char replacement, colon
   replacement, Standard/Multi-Disc track format; File Management).
2. **Profiles** — "Quality, Metadata, Delay, and Release profiles" (Quality
   Profiles list — live instance: *Lossless Best Effort, High Quality
   Lossy, Lossless, WAV*; Metadata Profiles — *Standard, Album, Studio*;
   Delay Profiles — Preferred Protocol / Usenet Delay / Torrent Delay + Tags;
   Release Profiles).
3. **Quality** — "Quality sizes and naming" (**Quality Definitions** table:
   Quality · Title · Size Limit, ~40 rows — MP3-8…MP3-320, OGG Vorbis
   Q5–Q10, AAC-192/256/320/VBR, WMA, MP3-VBR-V2/V0, **FLAC, ALAC, APE,
   WavPack, FLAC 24bit, ALAC 24bit, WAV** (all "Unlimited"); "Limits are
   automatically adjusted for the album duration"; *Reset Definitions*).
4. **Custom Formats** — "Custom Formats and Settings".
5. **Indexers** — "Indexers and indexer options".
6. **Download Clients** — "Download clients, download handling and remote
   path mappings".
7. **Import Lists** — "Import from another Lidarr instance or Trakt lists
   and manage list exclusions".
8. **Connect** — "Notifications, connections to media servers/players, and
   custom scripts".
9. **Metadata** — "Create metadata files when tracks are imported or artist
   are refreshed" (Write Metadata to Audio Files / Tag Audio Files with
   Metadata, *Scrub Existing Tags*, **Metadata Consumers**: Kodi (XBMC)/Emby,
   Roksbox, WDTV).
10. **Tags** — "See all tags and how they are used. Unused tags can be
    removed".
11. **General** — "Port, SSL, username/password, proxy, analytics and
    updates".
12. **UI** — "Calendar, date and color impaired options" (First Day of Week,
    Week Column Header, Short/Long Date + Time formats, relative dates,
    Theme (Auto), Color-Impaired Mode, **Expand Items by Default: Albums /
    EPs / Singles / Broadcast / Other**, UI Language).
- vs Sonarr: 12 sections, and **no standalone "Metadata Source" section** —
  music sources are *Metadata Profiles* inside Profiles.

**System** — *Status* (Health table with wiki links; Disk Space per root:
  Location/Free/Total + % bar; About: Version **3.1.0.4875**, Package
  Version, .NET, Docker, Database, Database Migration, AppData Directory,
  Startup Directory, Mode, Uptime, More Info links), *Tasks* (scheduled: Check
  Health, Housekeeping, Import List Sync, Messaging Cleanup, Refresh Artist,
  Refresh Monitored Downloads, Rescan Folders, Rss Sync; + running queue
  table Name/Queued/Started/Ended/Duration), *Backup* (export + zip list),
  *Updates* (versioned changelog, New/Fixed), *Events* (live event stream:
  time · event type · message, e.g. "ImportApprovedTracks — Importing album
  95/96"), *Log Files* (file list — empty on this instance).

## 3. Gap analysis (Mediarr today → target)

| Area | Mediarr now | Gap |
|---|---|---|
| Shell | Top bar + horizontal nav (Dashboard/Movies/TV/Music/Books/Queue/Settings) | P1 shell (shared w/ Sonarr) + music sub-nav (Library → Add New, Unmapped Files) |
| Music list | `Music.tsx`: server-filtered, paginated artist **table** w/ 2-level lazy expansion | 6-up square-cover grid, status ribbons + legend, 3-line footer (name/monitored/profile), A–Z jump, library status-bar counts, view/sort/filter toolbar |
| Artist detail | Table row expansion only | Banner header (rating/genre pills, root/size/profile/monitored/continuing/links, prev-next paging), albums accordion **grouped by album type**, album rows (Title/Release Date/Rating/Status), disc sections, track table (Track/Title/Duration/Audio Info/Status), full artist toolbar incl. Interactive Search, Manage Tracks, Preview Retag |
| Add flow | `ArtistAdd.tsx`: search → pick → quality profile + monitored | Lidarr search page (artist + release-group rows w/ badges) + "Add New Artist" **modal**: Root Folder, Monitor (7 options), Monitor New Albums, Quality Profile, Tags, Start-search checkbox |
| Unmapped Files | — | Page: Path/Size/Date Added/Quality, Add Missing / Delete Selected |
| Calendar | — | Inherited from Sonarr P4; music data = upcoming release dates of monitored artists (no air dates) |
| Activity | Queue page (all kinds); per-item history endpoint | Queue/History/Blocklist music columns (Artist/Album/Quality/Formats/…) + blocklist parity — shared with Sonarr P3 |
| Wanted | Per-album `/wanted` only | Missing + Cutoff Unmet, Search All, Unmonitor Selected, Manual Import entry — shared with Sonarr P3, music row model (Album Type + Release Date) |
| Settings | One page: download client, library overrides, indexers CRUD | 12-section IA; music-specific: Quality **Definitions** table (lossless levels), Profiles (quality + **metadata** + delay + release), per-root default profiles, Metadata consumers, UI per-release-type expand defaults |
| System | /healthz, /readyz, /metrics only | Status/Health, Tasks, Backup, Updates, Events, Log Files — shared with Sonarr P6 |
| Backend (no endpoint yet) | artists/albums/tracks CRUD, per-album wanted, search/artists, profiles (untyped) | lossless quality model (definitions + profiles), metadata profiles, album-type model (Album/EP/Single/Broadcast/Other/Demo/…), per-album + per-artist monitoring, artist status (continuing/ended/inactive), unmapped files, interactive search, manage tracks, retag, blocklist, tags, root folders (w/ default profiles), calendar release dates, iCal, import from Lidarr |

## 4. Phases

Each phase ends with: OpenAPI spec updated, UI types regenerated (M8 lockstep
gate green), vitest + Playwright updated. All phases assume the P1 shell
exists (or land on its route tree).

### L1 — Artist library grid + detail parity (UI-heavy, reuses P1 components)
- Artist grid: 6-up square-cover cards (name/monitored/quality footer,
  status ribbon + legend, red corner flag), A–Z jump, view/sort/filter
  toolbar, library footer status bar (counts).
- Artist detail: banner header with pills + prev/next paging, albums
  accordion grouped by album type (count badge + size), album rows, disc
  sections, track table with Audio Info; artist toolbar (Refresh & Scan,
  Search Monitored, Interactive Search, Preview Rename, Preview Retag,
  Manage Tracks, History, Manual Import, Artist Monitoring, Edit, Delete,
  Expand All); Edit Artist modal.
- **Backend deps:** artist status field (continuing/ended/inactive +
  monitored), per-album `album_type` + monitoring, per-track audio-info
  normalization (codec/channels/kbps/sample rate/bit depth), per-artist
  history (shared w/ Sonarr P3), interactive-search + manage-tracks + retag
  endpoints (music-only; can be deferred behind the toolbar buttons).
- **Runs parallel to Sonarr P2** (same grid/detail components, different row
  model). Accept: add/browse flows indistinguishable in layout from Lidarr
  (video comparison).

### L2 — Add Artist flow (Lidarr modal, music monitoring model)
- `/music/add` search page: debounced results with artist rows (♥ %, Group,
  overview) and release-group rows (year, N releases, type chips);
  MusicBrainz-ID lookup support; clicking an already-added artist jumps to
  its page.
- "Add New Artist" modal in the exact field order (Root Folder, Monitor,
  Monitor New Albums, Quality Profile, Tags, Start search for missing
  albums); Edit Artist modal (Monitored, Monitor New Albums, Quality
  Profile, Metadata Profile, Path, Tags).
- **Backend deps:** root-folder registry (shared w/ Sonarr P2); monitor +
  monitor-new-albums policy fields (music monitoring model); tags (shared
  w/ Sonarr P5.3); per-artist profile assignment.
- **Runs parallel to Sonarr P2 add flow** (shared root-folder/tags backend).
  Accept: a Lidarr user adds an artist with zero relearning.

### L3 — Music settings sections + lossless quality model
Ordered by value (section IA itself comes from the P1 settings layout;
shared with Sonarr P5):
1. **Quality** — Quality Definitions table: the full lossy + lossless
   ladder with per-level size limits ("Limits are automatically adjusted for
   the album duration"), *Reset Definitions*; this is the **lossless quality
   profile model** Mediarr lacks (no FLAC/MP3/ALAC/APE/WavPack/24-bit types
   today).
2. **Profiles** — quality profiles (default set: Lossless Best Effort,
   High Quality Lossy, Lossless, WAV) + **metadata profiles** (Standard /
   Album / Studio) + delay + release profiles (delay/release shared w/
   Sonarr P5.2).
3. **Media Management** — track naming (single + multi-disc formats,
   illegal-char/colon handling) + **root folders with per-root default
   Quality + Metadata profile** (ties to L2).
4. **Metadata** — tag audio files, scrub existing tags, metadata consumers
   (Kodi/Emby, Roksbox, WDTV).
5. **UI (music bits)** — Expand Items by Default per release type, theme /
   color-impaired.
- Remaining sections (Custom Formats, Indexers, Download Clients, Import
  Lists, Connect, Tags, General, UI) are built once in Sonarr P5 — music
  inherits; only verify music-specific option sets.
- **Backend deps:** quality definitions + typed quality profiles (the
  biggest music-specific backend chunk), metadata-profile model, naming
  templates for music (artist/album/disc/track), per-root defaults.
- **Runs parallel to Sonarr P5** (settings section layout, tags,
  indexers, clients are shared). Accept: a Lidarr user can rebuild the live
  instance's quality/profile config 1:1.

### L4 — Activity/Wanted/Calendar music wiring (mostly DRY)
- Queue/History/Blocklist music columns (Artist/Album Title/Quality/Formats/
  Track Title/Date) + Cutoff Unmet for albums (cutoff from the quality
  profile, L3).
- Wanted Missing + Cutoff Unmet (Album Type + Release Date columns),
  Search All, Unmonitor Selected, Manual Import entry.
- Calendar: music data source = upcoming **release dates** of monitored
  artists (Lidarr's calendar shows these; it is not an air-date calendar).
  **Honest note:** the working assumption "Lidarr has no calendar" was
  checked against the live 3.1 instance — it does, with the same 5 views +
  iCal as Sonarr, so music calendar parity is in scope (data source is
  different).
- **Backend deps:** shared with Sonarr P3/P4 (blocklist, global history,
  manual import, calendar endpoint + iCal) — no new music-only backend
  beyond release-date availability from the artist/album catalog.
- Accept: every Activity/Wanted/Calendar page renders music rows in the
  Lidarr column set.

### L5 — System + drop-in migration from live Lidarr
- System pages (shared build w/ Sonarr P6): Status/Health + disk space per
  root + About, Tasks, Backup, Updates, Events (event stream already exists
  as /api/v1/events WS — expose in UI), Log Files.
- **Unmapped Files** page (Path/Size/Date Added/Quality + Add Missing /
  Delete Selected) — music's import path.
- **Drop-in moment — Lidarr migration** (mirrors Sonarr P6's approach):
  via Import Lists section ("Import from another Lidarr instance"), pull a
  live Lidarr 3.x instance: **artists + albums (type, monitoring, tracks) +
  quality profiles + quality definitions + metadata profiles + tags +
  root folders (with default profiles) + indexers + download client** →
  import into Mediarr with mapping report and dry-run mode. Postgres target,
  idempotent, resumable. (Lidarr's own Import Lists UI is the user's mental
  model for this.)
- **Backend deps:** structured health endpoint, task registry exposure,
  backup export/import, log streaming (all shared w/ Sonarr P6), plus a
  **Lidarr-API importer** (artists/albums/tracks/profiles/tags/roots).
- Accept: `migrate from lidarr.local.uaccloud.com` reproduces the live
  library (502 artists, 78,596 tracks, 36,106 files, 1.5 TiB, profiles,
  tags) in Mediarr.

## 5. Sequencing & risk notes

- **L1–L2 are the visible 80%** for music; L3 carries the backend weight
  (lossless quality model is the one genuinely new data model). L4 is mostly
  DRY reuse of Sonarr P3/P4; L5's System pages are DRY reuse of Sonarr P6.
- **Parallelism with Sonarr plan:** L1 ∥ P2, L2 ∥ P2, L3 ∥ P5, L4 ∥ P3/P4,
  L5 ∥ P6. Shared dependencies to coordinate in one place (root folders,
  tags, blocklist, calendar, settings IA, importer, tasks/logs) — spec each
  once, reference from both plans.
- Risk: **quality model sprawl** (Lidarr has ~40 quality definitions).
  Mitigation: ship the definitions table as *data* (seed table), not
  compiled types; per-artist/profile assignment is the only model work.
- Risk: 4-level drill (artist → album type group → album/disc → track) is
  deeper than Sonarr's 3; reuse the accordion component from P2 and keep
  lazy loading per level (existing `Music.tsx` 2-level lazy expansion is the
  starting point).
- Honest deltas found during audit (keep DRY, don't duplicate Sonarr work):
  the add flow is a **modal** (Lidarr 3.x), not Sonarr's persistent two-pane;
  the library page has a **footer status bar + ribbon legend** Sonarr lacks
  (music-only); artist cards show the **name** on the card (Sonarr grid
  cards don't); Lidarr settings are **12 sections** with metadata source
  folded into Profiles; **Lidarr has a calendar** despite the brief's
  assumption (upcoming release dates).
- Non-goals: no Lidarr-compatible wire API; no mobile-first pass (responsive
  desktop-first, like Lidarr).
