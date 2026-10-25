# Readarr UI Parity — Drop-in Replacement Plan (Books)

> Grounded in the public surface — no local instance exists (the user has
> none, and **Readarr was retired by the Servarr team**): readarr.com (site +
> API docs, OAS 1.0.0), the Servarr wiki (user guide + settings reference,
> last edited 09/06/2025), and the archived GitHub repo (final release
> v0.4.18.2805, 2025-06-15; a v2.0.0 UI tag exists on the archived develop).
> The IA map below follows the wiki's user guide (Library / Calendar /
> Activity / Wanted / Settings / System) rendered in the shared Servarr v2
> React shell (same as Sonarr 4.x / Radarr v6 / Lidarr 3.x); exact v2 nav
> labels should be verified against the archived repo's UI source when the
> first Books phase lands.

## 1. Goal & scope

Mediarr replaces the ARR stack; **Readarr is the parity target for the Books
domain**. Two strategic facts shape this plan:

1. **Readarr is dead.** The Servarr team retired it (metadata became
   unusable; the Open Library migration stalled; no maintainer). Mediarr is
   therefore the *spiritual successor* for books. Parity means matching the
   UX Readarr users learned (IA, flows, settings) — NOT depending on Readarr
   as a metadata source. Mediarr's book metadata stays on **OpenLibrary**
   (PLAN.md §7 provider), with Google Books as an optional secondary source.
2. **People still run Readarr.** The app is retired but not gone; existing
   libraries must be importable. The drop-in moment (B6) migrates from a
   *live retired* Readarr instance via its own API — the same harness the
   other plans use.

The shared shell/design system comes from sonarr-parity-plan P1; the
Settings section IA, Activity/Wanted/Calendar/System patterns reuse P3–P6.
This plan specs only what is Books-specific, labeled **B** (Books) to
distinguish from Sonarr's **P**, Radarr's **R**, Lidarr's **L**, and
Prowlarr's **PR** phases.

## 2. Target: Readarr IA map (what we must match)

**App shell** — identical to the Sonarr plan's shell (accent top bar, left
rail, sub-nav, context toolbars). Books rail section = **Authors** (home =
the library). Books-specific rail surface:

- **Authors** (home) with sub-items: *Add New* (search by author **or**
  book; supports `goodreads:<id>`, `isbn:<…>`, `asin:<…>` lookups),
  *Unmapped Files*.
- **Calendar** — upcoming book release dates (5 shared views + iCal; feed
  path `/feed/v1/calendar/readarr.ics` → Mediarr `/feed/books.ics`).
- **Activity** (sub: *Queue*, *History*, *Blocklist*).
- **Wanted** (sub: *Missing*, *Cutoff Unmet*).
- **Settings**, **System** — shared IA (sections below).

**Library — dual view (the distinctive Readarr shape)**
- **Authors view**: author grid (cover/monitored/quality footer pattern from
  P1) with A–Z jump; toolbar: *Update All* · *Refresh & Scan* · *RSS Sync* |
  *Author Editor* (mass editor) / *Author Index* toggle · *Options* ·
  *View ▾* (Table / Posters / Overview) · *Sort ▾* · *Filter ▾*.
- **Books view** (flat index of all books across authors): same toolbar
  family with *Search All / Search Filtered / Search Selected* and
  *Book Editor* / *Book Index* toggle.
- Author detail: header (banner/poster, ratings, language, series links),
  books accordion; **each book → editions** (format EPUB/MOBI/AZW3/… +
  language per edition, per-edition monitored — the Books analogue of
  Radarr's editions), book files with media info (pages, size, format,
  language), series membership, ratings, links.

**Add New (author or book)**
- Debounced search over authors *and* books (single field, results tagged by
  kind); ID-lookup syntax in the placeholder (goodreads:id, isbn:…, asin:…).
- Result rows: cover, title/year, rating, language, format(s), series,
  overview; clicking an already-added author/book navigates to its page.
- Add modal (author): *Root Folder*, *Monitor* (All / Future / Missing /
  Existing / First / Latest / None — the Lidarr-style policy model),
  *Quality Profile*, *Metadata Profile*, *Tags*, start-search checkbox,
  *Add <Author>*. Add (single book): same fields minus the author-pull note
  (metadata profile `None` caveat applies).

**Unmapped Files** — books present in root folders but unrecognized:
info / manual-import / delete per row; *Add Missing* top action (shared
pattern with Lidarr's Unmapped Files).

**Activity / Wanted** — shared P3 IA with book-shaped columns:
Queue (Author, Book, Edition, Quality/Format, Time Left, Progress), History
(global + per-author; API has `/history/author`, `/history/since`,
`/history/failed/{id}`), Blocklist; Wanted *Missing* (monitored & absent)
and *Cutoff Unmet* (file exists but format/quality below cutoff), both with
Search All / Search Selected / per-row interactive search, Unmonitor
Selected, Manual Import.

**Settings (Readarr's 12 sections — unified-service adapted)**
1. **Media Management** — naming (book file naming incl. {Author
   [CleanName/Disambiguation]}, {Book [Title/CleanTitle/Disambiguation/
   TitleThe/Type]}, {Release Year}, {medium:NN}, {Medium Format}, {Quality
   [Full/Title]}, {MediaInfo …} tags; space/case handling), author folder
   format, folders (create/delete empty), import (skip free-space check, min
   free space, hard links, extra files), file management (ignore deleted,
   propers/repacks, watch root folders, rescan after refresh, fingerprint,
   change file date to release date, recycling bin), permissions.
   **Calibre per root folder** (the Readarr signature): Use Calibre toggle
   (ebook-only; immutable once enabled/disabled), Calibre Content Server
   host/port/urlBase/username/password/library, Convert to Format (MOBI,
   EPUB, AZW3, DOCX, FB2, …), Output Profile, Use SSL, Monitor, Quality
   Profile, Metadata Profile, Default Tags.
2. **Profiles** — Quality Profiles, **Metadata Profiles** (Minimum
   Popularity, skip books missing release date, skip no ISBN/ASIN, skip
   broken parts/sets, skip secondary series books, Allowed Languages),
   Release Profiles, Delay Profiles.
3. **Quality** — one unified ladder: **e-book qualities** (EPUB / MOBI /
   AZW3 — format-preference based) and **a-book qualities** (the lossless /
   lossy ladder) in the same definitions table; audio qualities rank above
   ebook qualities.
4. **Custom Formats** — standard CF engine (shared with P5).
5. **Indexers** — shared (P5 / Prowlarr PR1–PR2 hub).
6. **Download Clients** — shared multi-client (PR5).
7. **Import Lists** — **Goodreads** (list import), **LazyLibrarian**
   (legacy list), **another Readarr instance**; list exclusions.
8. **Connect** — connections/notifications + custom scripts (shared).
9. **Metadata** — Calibre metadata, write tags to files (book + audio tag
   types), metadata consumers.
10. **Tags** — shared.
11. **General** — host (port/urlBase/allowed hosts), security (auth, API
    key), proxy, logging, analytics, updates, backups.
12. **UI** — calendar options, date formats, style (theme/color-impaired),
    language.

**System** — shared P6 (Status/Health + Disk Space + About, Tasks, Backup,
Updates, Events, Log Files).

## 3. Gap analysis (Mediarr today → target)

| Area | Mediarr now | Gap |
|---|---|---|
| Books library | `/books` expandable author→title table + pagination | Authors grid + **flat Books index** dual view; A–Z, views (Table/Posters/Overview), mass editor, per-author toolbar |
| Book model | author → title → edition (thin: format rules from M4) | **Edition = format + language + monitored** (Radarr-edition analogue); series membership (SeriesBookLink); book files + media info (pages/size/format/lang) |
| Add flow | search authors or works → pick → quality profile + monitored | Author-or-book combined search, **ISBN/ASIN/Goodreads-ID lookups**, monitor *policy* (7 states) + metadata profile + tags |
| Unmapped Files | — | Unrecognized-files page + Add Missing / manual import per row |
| Quality | `/profiles` (global list, TV-oriented) | **e-book + a-book unified quality model** (format-preference cutoffs), metadata profiles (popularity/ISBN/language filters) |
| Bookshelves / series | — | Bookshelf grouping (Radarr-Collections analogue) + series links |
| Calendar | — | Books release-date calendar (shared P4 component, book data source) |
| Activity/Wanted | queue + per-item history; per-title wanted | Book-shaped columns; global + per-author history; Missing + Cutoff Unmet; blocklist |
| Calibre | — | **Calibre Content Server integration** (per-root, optional feature flag): CRUD via its HTTP API, format conversion, metadata write |
| Metadata | — | Tag writing (book + audio), metadata consumers |
| Import Lists | — | Goodreads + LazyLibrarian + Readarr-instance importers |
| Naming | — | Book naming template with Readarr's tag set (author/book/medium/quality/media-info) |
| System | /healthz, /readyz, /metrics | shared P6 build |
| Metadata source | OpenLibrary + (search) — good | **Deliberately NOT the retired Readarr metadata service**; Google Books optional secondary |

## 4. Phases

Each phase ends with: OpenAPI spec updated, UI types regenerated (M8
lockstep gate green), vitest + Playwright updated.

### B1 — Books library parity: dual view + author detail (UI + model)
- *Depends on / reuses:* **P1 shell** (grid, A–Z, toolbar, views, cards).
- Authors grid (home for books) + **flat Books index**; Author/Book Editor
  (mass edit) toggle; author detail with books accordion → **editions**
  (format + language + per-edition monitored) → book files (media info).
- Series membership badges; ratings/links.
- **Backend deps:** edition model enrichment (`format`, `language`,
  per-edition `monitored`); book-file table (media info: pages, size,
  format, language, added date); series links (book ↔ series); per-author
  history (shared w/ P3). The format+language+monitored edition triple is
  Books' analogue of Radarr's edition model — budget it early, it gates
  add-flow, quality cutoff, and migration.
- **Parallelizable with R1/L1** (same grid/detail/accordion components).

### B2 — Add author/book flow
- Combined author+book search (kind-tagged results), **ISBN / ASIN /
  Goodreads-ID** lookup syntax; add modal: Root Folder, Monitor (7-state
  policy), Quality Profile, Metadata Profile, Tags, start-search checkbox.
- **Backend deps:** `/search/books/lookup` extending current
  `/search/books/authors` + `/search/books/works` with id-lookups (OpenLibrary
  ISBN/ASIN endpoints; Goodreads id mapping); author monitor-policy field;
  metadata-profile assignment (L3's model, shared); root folders (shared
  w/ P2).
- **Parallelizable with R2/L2** (shared two-pane/modal Add scaffold +
  root-folder/tags backend).

### B3 — Books quality model + Unmapped Files
- Quality Definitions table: **e-book** (EPUB/MOBI/AZW3/… as
  format-preference entries) + **a-book** (lossless/lossy ladder, shared
  seed with Lidarr L3's ~40 definitions) in one table; quality profiles
  with cutoff; metadata profiles (min popularity, skip no-ISBN/ASIN, skip
  parts/sets, allowed languages).
- Unmapped Files page (Path/Size/Date Added/Quality; info / manual import /
  delete; Add Missing).
- **Backend deps:** book quality model (format-based, distinct from
  resolution-based TV/movie quality); metadata-profile filter model;
  filesystem scan of root folders for unrecognized book files (reuses the
  import-scan work from Sonarr P2's Library Import).

### B4 — Bookshelves + Series (depth, Radarr-Collections analogue)
- Bookshelves: group books (by author, series, custom) with
  missing-count/quality/monitored rollup; Series pages linking
  member books across authors.
- **Backend deps:** bookshelf grouping endpoint (client-side aggregation
  over series links + custom groups is acceptable first).
- Standalone; can run independently of B1–B3.

### B5 — Books settings sections (12, unified-service adapted)
- *Reuses P5 section layout + most shared sections* (Indexers, Download
  Clients, Custom Formats, Connect, Tags, General, UI).
- Books-specific order: (1) **Media Management** — book naming template
  (Readarr tag set), author folders, file management; (2) **Profiles** —
  book quality + metadata profiles (B3); (3) **Calibre integration** —
  per-root Content Server settings, conversion, output profiles, metadata
  write (feature-flagged; off by default; hardlinks auto-disabled when
  enabled — Calibre's requirement); (4) **Import Lists** — Goodreads,
  LazyLibrarian, Readarr-instance; (5) **Metadata** — tag writing +
  consumers.
- **Backend deps:** naming template engine (book tags), Calibre adapter
  (HTTP to Content Server; store credentials AES-256-GCM per PLAN.md §8;
  never hardlinks when Calibre-managed), Goodreads list scraper/importer,
  audio+book tag writer.
- **Parallelizable with Sonarr P5 / Lidarr L3** (shared section IA, tags,
  indexers, clients; quality tables are per-kind data).

### B6 — System + drop-in migration from live Readarr
- System pages (shared build w/ P6).
- **Drop-in moment — Readarr migration:** pull a *live (retired) Readarr
  instance* via its API (base URL + API key): authors (+monitor, metadata
  profile, path, tags), books + editions (format/language/monitored) +
  book files, series links, quality profiles + definitions, metadata
  profiles, root folders (incl. Calibre settings where enabled), tags,
  indexers + download clients → import into Mediarr with mapping report and
  dry-run. Postgres target, idempotent, resumable. ISBN/ASIN identity maps
  to OpenLibrary ids at import time (the metadata-source swap happens here,
  once, during migration).
- **Backend deps:** shared P6 System surface + **Readarr-API importer**
  (the v0.4 API surface is stable — the repo is archived, the API will not
  change; pin the importer to the v0.4.18 contract).
- Accept: a user's live Readarr instance (authors, books, editions, files,
  profiles, tags, Calibre roots) reproduces in Mediarr; books resolve to
  OpenLibrary metadata after the swap.

## 5. Metadata source subtasks (MT1–MT5)

Readarr's death was a **metadata** failure (its upstream sources became
unusable; the Open Library migration stalled), so "don't depend on the
retired service" must be turned into scoped work. **Current state
(verified in code):** Mediarr already has a swappable provider adapter
layer (`internal/providers/`): OpenLibrary for books (keyless; name-based
`SearchAuthors` + title-based `SearchWorks` over `/search.json`), TMDB
(movies) and TVDB (TV) with API keys, MusicBrainz (music). Domain code
never calls providers directly; keys come from config (503 when
unconfigured, 502 on bad responses). But the books side is thin: the OL
work key is display-only, **editions are manual data entry** (format/
ISBN/publisher/year/pages typed by the user), there is no ISBN/ASIN/
Goodreads lookup, no edition→format+language resolution, no caching, and
no source provenance stored.

- **MT1 — ISBN / ASIN / Goodreads-ID lookup** (unblocks **B2**). Extend
  the OL adapter with search filters (`isbn:`, `key:`) and a Goodreads-ID
  → OL mapping (via ISBN); new `/search/books/lookup` endpoint feeding the
  Add New ID-lookup syntax.
- **MT2 — Automatic edition resolution** (unblocks **B1**). On work
  selection, fetch work details (`/works/OL…W.json` → editions with
  formats, languages, publisher, year) and offer them as pre-filled
  editions (format + language + per-edition monitored) instead of
  typed-in. The edition triple becomes provider-fed, not manual.
- **MT3 — Cache + provenance** (foundational; do first). Persist resolved
  works (OL key, title, author, year, cover) with `source` + `source_id`
  columns (OL key now; Readarr id at B6 migration), a TTL cache, and a
  refresh command — keyless OL has no SLA, so hammering it per request is
  not viable.
- **MT4 — Metadata Profiles** (rides **B3**). Server-side filters over
  search results: minimum popularity, skip missing ISBN/ASIN, skip broken
  parts/sets, allowed languages. Cheap — it filters MT1/MT2 output.
- **MT5 — Optional secondary source: Google Books** (config-flagged,
  keyed adapter on the existing `base.go` pattern → 503 when
  unconfigured) for coverage gaps — chiefly **audiobooks**, where OL
  metadata is thin. Never primary; OL stays the source of truth.

**Sequencing:** MT3 → MT1/MT2 → (B1/B2 land on top) → MT4 → MT5. Nothing
in B4–B6 depends on MT5. (The cross-domain keyless fallback for TV/movies —
TVmaze, Wikidata/Wikipedia, per-kind source lists — lives in the Sonarr
plan's **P5.7**; books and music are already keyless via
OpenLibrary/MusicBrainz.)

## 6. Sequencing & risk notes

- **B1–B2 are the visible 80%** for books (grid + detail + add). B3 carries
  the book quality backend weight; B5 carries Calibre; B6 is the migration.
- **Parallelism with the other plans:** B1 ∥ R1/L1, B2 ∥ R2/L2, B3 ∥ L3
  (quality seed tables share format/lossless definitions), B5 ∥ P5/L3,
  B6 ∥ P6/R6/L5 (shared importer harness; the source API differs).
- **Risk: dead upstream.** Readarr will never ship the v2 UI fixes people
  expected; screenshots/docs may lag reality. Mitigation: treat §2 as the
  *contract*; verify against (a) the wiki's user guide, (b) the archived
  repo's UI source + openapi.json (readarr.com/docs/api/ OAS 1.0.0 — pin
  this as the migration contract), (c) a throwaway local Readarr v0.4.18
  docker instance if a phase needs pixel-level checks (it still runs).
- **Risk: metadata swap surprises.** OpenLibrary coverage (formats,
  languages, series) is thinner than Readarr's was. Mitigation: import maps
  Readarr's *ids* (OpenLibrary is already Mediarr's provider — the swap is
  internal to the importer), keep a `source_id` column (Readarr/OL ids) for
  provenance, and let users fall back to manual book adds when a match
  fails (the Unmapped Files + manual-import flow is the safety net).
- **Calibre is optional by design** — it is Readarr's signature but its
  Content-Server dependency makes it a hard second-class feature (no
  hardlinks, immutable toggle, auth required). Ship it feature-flagged,
  documented, and never block core book import on it.
- **Non-goals:** no Readarr-compatible *wire* API; no comics/magazines
  (Readarr never supported them); desktop-first responsive, like the other
  ARR apps.
