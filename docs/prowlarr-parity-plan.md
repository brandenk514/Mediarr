# Prowlarr UI Parity — Drop-in Replacement Plan

> Audited against a live Prowlarr v2.6.5.5623 instance (linuxserver.io image,
> `.5623-ls161`, .NET 8.0.27, SQLite 3.53.4, DB migration 44) on 2026-10-24.
> State at audit time: 7 indexers (all torrent, public, all enabled; one
> FlareSolverr proxy `cf-fs` tagged onto 1337x + LimeTorrents), 1 download
> client (Transmission), 4 connected apps (LazyLibrarian, Lidarr, Radarr,
> Sonarr — all "Full Sync"), 1 tag, 654 definitions in the built-in
> community library. Goal: a Prowlarr user should land in Mediarr and find
> the same pages, in the same places, with the same muscle memory.

## 1. Goal & scope

Prowlarr is **not** a media app — it is the **shared indexer hub**: indexer
definitions, testing, stats, manual search, query history, download clients
for in-app search, tags, and the app-sync that feeds Radarr/Lidarr/Sonarr/
LazyLibrarian. Mediarr's equivalent is the **unified indexer layer**
(`PLAN.md §7`): indexer definitions as code + user config, one provider
interface with per-protocol adapters (TorrentRSS, Torznab, usenet NZB,
PTP/Internet Archive), a built-in community definition library, periodic
indexer health + per-indexer stats, search fan-out, and AES-256-GCM
encryption of indexer keys at rest.

This plan is the **backend + UI spec for that layer, and the contract that
sonarr-parity-plan P5 sections 'Indexers' and 'Download Clients' consume** —
say it once here, build it once there. The section IA, labels, and field
lists below are the DRY source of truth for those P5 sections; the per-kind
settings pages reference this plan instead of re-specifying.

**One surface serves all four kinds.** In the ARR stack Radarr/Lidarr/Sonarr
(and LazyLibrarian) import their indexers *from* Prowlarr via per-app
connection settings (`prowlarrUrl` + `apiKey` + `syncCategories`). In
Mediarr there is no external hub: the TV/Movies/Music/Books settings pages
all bind to the **same** indexer records, with per-kind category filters and
sync profiles standing in for Prowlarr's app-sync. Building the indexer
surface here is parity work for four apps at once.

The **shared shell/design system comes from sonarr-parity-plan P1** (accent
top bar, left rail with section sub-nav, icon-over-label toolbars, badge set,
view/sort/filter menus, A–Z jump, settings section layout + unsaved-changes
bar, modal component). This plan does **not** re-spec the shell; it adds the
*Indexers* hub section to the rail. Note Prowlarr's own accent is **orange**
(top bar + active rail item) versus Sonarr's teal — the P1 token system
should support a per-section accent so the Indexers hub reads "Prowlarr" to
migrating users. Mediarr keeps its extras (cross-kind dashboard, WebSocket
live feeds) — parity means "Prowlarr's surface exists and feels identical",
not "nothing else".

Non-goals: no Prowlarr-compatible *wire* API (third-party tooling can use
the OpenAPI surface; we match the UI, not the protocol); no 1:1 "Apps
sync" page (replaced by per-kind settings, §2); desktop-first responsive.

## 2. Target: Prowlarr IA map (what we must match)

**App shell** — orange top bar: Prowlarr logo · borderless underline
**search** (id `movieSearch`) · heart (donate) · language glyph (translation
link) · user pill ("A" + person). Left rail (dark charcoal, ~210px); active
item = orange left bar + orange icon/text; sub-items reveal under the active
section.

**Left rail (top-level + sub-items)**
- **Indexers** (▶) — home = the indexer list itself
  - *Stats* (sub: `/indexers/stats`)
- **Search** — manual multi-indexer search
- **History** — query history
- **Settings** — sub: *Indexers* (proxies) · *Apps* · *Download Clients* ·
  *Notifications* · *Tags* · *General* · *UI*
- **System** — sub: *Status* · *Tasks* · *Backup* · *Updates* · *Events* ·
  *Log Files*

**Indexers (home = the list, not a dashboard)**
- Page toolbar (icon-over-label, dividers): left = *Add Indexer* · *Sync App
  Indexers* · *Test All Indexers* · *Select Indexers* | right = *Options*
  (column visibility) · *Sort ▾* · *Filter ▾*.
- Table columns: *(status icon)* · *Indexer Name* (sorted) · *Protocol*
  (green pill: torrent/usenet) · *Privacy* (red pill: Public/Private/
  Semi-Private) · *Priority* (25) · *Sync Profile* (Standard) · *Added*
  (YYYY-MM-DD) · *Categories* (stack of dark pill badges: Console, Movies,
  Audio, PC, TV, XXX, Books, Other). Per-row kebab: *Edit Indexer* (opens
  the same modal as add, prefilled).
- Right rail: **A–Z quick-jump** (`#` + first letters present, e.g.
  B·I·L·P·T).
- Bottom **summary panel**: color legend (green = Enabled, blue =
  Enabled+Redirected, gray = Disabled, red = Error) + stat counters
  (*Indexers 7 · Enabled 7 · Torrent 7 · Usenet 0*).

**Add Indexer flow (the signature feature — two-stage modal, not a page)**
1. *Add Indexer* opens a **large modal picker** over the list: intro line
   ("Prowlarr supports many indexers in addition to any indexer that uses
   the Newznab/Torznab standard using 'Generic Newznab' (for usenet) or
   'Generic Torznab' (for torrents). Search & Select your indexer from
   below."), a *Filters* button (Protocol / Language / Privacy /
   Categories) + text filter, and a 654-row virtualized table: *Protocol ·
   Name · Language (locale code) · Description · Privacy · Categories*.
   "(API)" suffixed names mark alternate implementations of the same
   tracker.
2. Clicking a row opens a **medium (720px) form modal** layered over the
   picker, generic fields first, then definition-specific fields:
   - *Name* · *Enable* (toggle) · *Redirect* (toggle; helper: "Redirect
     incoming download request for indexer and pass the grab directly
     instead of proxying the request via Prowlarr") · *Sync Profile ▾*
     (Standard; helper: app profiles control RSS / Automatic Search /
     Interactive Search settings on application sync)
   - Definition fields per config contract (private-tracker example,
     AlphaRatio): *Base Url ▾* ("Select which base url Prowlarr will use
     for requests to the site"), *Username* ("Site Username"), *Password*
     ("Site Password"), *Use Freeleech Tokens ▾* (Never/Always/…; "When to
     use freeleech tokens"), *Freeleech Only* ("Search freeleech releases
     only"), *Exclude SCENE*, *Seed Ratio* ("at least 1.0 and follow the
     indexers rules") — plus *Show Advanced* (baseSettings.queryLimit,
     grabLimit, limitsUnit, appMinimumSeeders, seedTime, packSeedTime,
     preferMagnetUrl…).
   - *Tags* (chip input; helper: "Use tags to specify Indexer Proxies or
     which apps the indexer is synced to… An indexer with a tag will only
     sync to apps with the same tag.") · footer *Cancel* / *Add Indexer*.

**Stats** — toolbar *Refresh · Filter*; four KPI cards (*Active Indexers 5 ·
Total Queries 49.4K · Total Grabs 577 · Active Apps 3*) above two large
time-series charts (queries, grabs) + one small chart per active indexer.

**Search** — toolbar *Options · Sort · Filter*; form: *Query* (text) ·
*Indexers ▾* (multi) · *Categories ▾* (multi) · *Search all indexers*
(checkbox) · *Search* button; results table of releases with per-release
grab action (routed to a download client).

**History** — toolbar *Refresh · Clear · Options · Filter*; table:
*(icon)* · *Indexer* · *Query* · *Parameters* · *Categories* · *Date* ·
*Elapsed Time* (ms — a slow-indexer signal); server-side pagination
("1 / 2500 · Total records: 49986").

**Settings** (left section nav, list+form per section, *Show Advanced*
toggle, "No Changes" unsaved bar):
1. **Indexers** — *Indexer Proxies*: FlareSolverr instances (e.g. `cf-fs`)
   each showing the indexers they serve via tag.
2. **Apps** — toolbar *Sync App Indexers · Test All Apps · Manage
   Applications*; app rows (LazyLibrarian, Lidarr, Radarr, Sonarr) each with
   a *Full Sync* status; *Sync Profiles*: Standard = {RSS, Automatic
   Search, Interactive Search}. Per-app config: `prowlarrUrl`, `baseUrl`,
   `apiKey`, `authUsername`, `authPassword`, `syncCategories`
   (+ Sonarr: `animeSyncCategories`, `syncAnimeStandardFormatSearch`;
   Radarr/Lidarr/Sonarr: `syncRejectBlocklistedTorrentHashesWhileGrabbing`).
3. **Download Clients** — toolbar *Test All Clients · Manage Clients*;
   explainer ("If you intend to do searches directly within Prowlarr, you
   need to add Download Clients… For searches from your Apps, the download
   clients configured there are used instead. Download clients are for
   Prowlarr in-app searches only and do not sync to apps."); client rows
   (Transmission — *Enabled*). Config fields (Transmission): *Host · Port ·
   Use SSL · URL Base · Username · Password · Category · Directory ·
   Priority · Add Paused*.
4. **Notifications** — *Connections* list (notifications + custom scripts;
   empty state here).
5. **Tags** — tag list with usage counts ("cf-fs — 2 Indexers, 1 Indexer
   Proxy"), add/remove.
6. **General** — *Host*: Port (9696, "Requires restart to take effect"),
   URL Base ("For reverse proxy support"), Allowed Hosts ("prowlarr,
   prowlarr.local.uaccloud.com"); *Security*: Authentication (Forms — Login
   Page), Require Username and Password, Authentication Required, Disabled
   for Local Addresses, Username, Password (+ confirmation), **API Key**
   (plain-text input, restart-required), Certificate Validation (Enabled),
   Trusted Networks (CIDR list for reverse proxies); *Proxy*: Use Proxy;
   *Logging*: Log Level (Info); *Analytics*: Send Anonymous Usage Data.
7. **UI** — *Dates*: Short/Long Date Format (YYYY-MM-DD), Time Format,
   Show Relative Dates; *Style*: Theme (Auto — "Inspired by Theme.Park"),
   Enable Color-Impaired Mode; *Language*: UI Language (English; "Browser
   Reload Required").

**System**
- **Status** — *Health* ("No issues with your configuration" / health table)
  + *About*: Version (2.6.5.5623), Package Version
  (2.6.5.5623-ls161 by linuxserver.io), .NET (8.0.27), Docker (Yes),
  Database (Sqlite 3.53.4), Database Migration (44), AppData Directory
  (/config), Startup Directory (/app/prowlarr/bin), Mode (Console), Uptime;
  *More Info* links (Home Page, Wiki, Reddit, Discord, Source, Feature
  Requests, Donations).
- **Tasks** — *Scheduled* table (Name · Interval · Last Execution · Last
  Duration · Next Execution): Application Indexer Sync (6h), Application
  Update Check (6h), Backup (7d), Check Health (6h), Clean Up History (1d),
  Housekeeping (1d), Indexer Definition Update (1d), Messaging Cleanup
  (5m); *Queue* table (Name · Queued · Started · Ended · Duration).
- **Backup** — *Backup Now · Restore Backup* + zip list (Name · Size ·
  Time; weekly `prowlarr_backup_v<ver>_<date>.zip`).
- **Updates** — "The latest version of Prowlarr is already installed" /
  upgrade banner + release notes (*New* / *Fixed* bullets).
- **Events** — toolbar *Refresh · Clear · Options · Filter*; table *Time ·
  Component · Message* (live feed; e.g. `ReleaseSearchService — Searching
  indexer(s): [The Pirate Bay] for Term: [], Offset: 0, Limit: 100,
  Categories: [3000, 3030, 3040, 3050]`).
- **Log Files** — toolbar *Refresh · Delete*; "Log files are located in:
  /config/logs. The log level defaults to 'Debug' and can be changed in
  General Settings"; table *Filename · Last Write Time · Download* (rotated
  `prowlarr[.N].txt`).

## 3. Gap analysis (Mediarr today → target)

| Area | Mediarr now | Gap |
|---|---|---|
| Shell | Top bar + horizontal nav (Dashboard/Movies/TV/Music/Books/Queue/Settings) | P1 Sonarr shell (reused) + new **Indexers** rail section with *Stats* sub-item; per-section orange accent for the hub |
| Indexer list | Section inside the single Settings page: table of indexers (kinds fake/torznab/torrent-rss/nzb), CRUD, enable toggle, on-demand test, masked secrets | First-class home page: status icon, Protocol/Privacy pills, Priority, Sync Profile, Added, category badges, per-row Edit, A–Z jump, summary panel (legend + counts), Options/Sort/Filter, *Test All Indexers*, *Select Indexers* |
| Add flow | Inline "new indexer" form with 4 fixed kinds | Two-stage modal: 654-entry **community definition library** picker (Protocol/Language/Privacy/Categories filters, (API) variants) → config-contract form (Base Url select, credentials, freeleech/seed-ratio per-protocol fields, Sync Profile, Tags, Show Advanced) |
| Indexer health | On-demand test + per-indexer stats endpoint | Periodic test task + error tracking + **Stats page** (KPI cards: active indexers/total queries/total grabs/active apps; time-series + per-indexer charts) + query **History** (indexer, query, categories, date, elapsed ms, Clear, pagination) |
| Manual search | — | **Search page**: query + indexer multi-select + categories + "search all" + grab-to-client |
| Categories | None (kinds only) | Torrent (8) / usenet category sets per definition, per-indexer selection, used by per-kind search fan-out |
| Tags | None | Tag CRUD + usage view (indexer + proxy usage) + tag-based proxy/sync routing |
| Indexer proxies | None | FlareSolverr (and generic HTTP proxy) instances, assignment via tags |
| Download clients | Single client in Settings (`settings/download-client`) | **Multi-client** list: add/edit/test/delete, type-specific fields (Transmission: host/port/ssl/urlBase/user/pass/category/directory/priority/addPaused), *Test All Clients*, in-app-search scoping explainer |
| App sync | — | No 1:1 Apps page — replaced by per-kind settings binding to shared indexers (syncCategories → per-kind category filters; "Sync App Indexers" button retired); document the mapping in the UI |
| Notifications | — | Connections section (webhook/notification list, empty state) |
| General | App config via env/compose | In-UI General: host/port/urlBase/allowedHosts, auth (forms, local-address exception), **API key display + rotate**, certificate validation, trusted networks, proxy, log level, analytics opt-out |
| UI settings | Theme via app | Date formats, relative dates, theme (auto), color-impaired mode, UI language |
| System | /healthz, /readyz, /metrics only | Status (Health + About + More Info links), Tasks (scheduled + queue), Backup (create/restore + list), Updates (version check + changelog), Events (live feed), Log Files (rotated list + download) |
| Backend (no UI counterpart yet) | — | definition library (code + seeded JSON), config-contract field engine, per-indexer history, stats aggregation (queries/grabs/time series), multi-download-client model, tags, notifications, FlareSolverr proxy support, app-sync-equivalent (per-kind indexer binding), backup export/import, task registry exposure, log streaming, Prowlarr-API importer |

## 4. Phases

Each phase ends with: OpenAPI spec updated, UI types regenerated (M8
lockstep gate green), vitest + Playwright updated. Prerequisite:
**sonarr-parity-plan P1 shell** (or at least its rail + toolbar + modal +
settings-section components).

### PR1 — Indexer hub IA (list page parity; UI-heavy)
- New rail section **Indexers** (home = the list, replacing the current
  settings-section placement) with *Stats* sub-item; *Search* and
  *History* rail items land in PR3.
- List parity: status icon (Enabled / Enabled+Redirected / Disabled /
  Error), Protocol + Privacy pills, Priority, Sync Profile, Added,
  category badges, per-row *Edit Indexer*, A–Z jump, bottom summary panel
  (legend + Indexers/Enabled/Torrent/Usenet counters), toolbar (*Add
  Indexer · Test All Indexers · Select Indexers · Options · Sort ·
  Filter*; *Sync App Indexers* stubbed → replaced by per-kind sync note).
- Move existing CRUD/enable/test/stats into the hub; keep current API
  endpoints (paths may normalize to Prowlarr-like `/api/v1/indexer`).
- **Backend deps:** none beyond existing indexer CRUD + test + stats
  (add `privacy`, `priority`, `sync_profile`, `added` to the indexer
  record if missing).
- Accept: a Prowlarr user opens `/indexers` and finds the same table,
  toolbar, summary, and row actions.

### PR2 — Definition library + add/edit flow (the signature feature)
- **Built-in definition library** per `PLAN.md §7`: definitions as code
  (JSON config contracts, same idea as Prowlarr) seeded at build/migrate
  time — target the Prowlarr community set (654 at audit: 2 protocols
  torrent/usenet; 3 privacy classes; per-definition `fields` = the config
  contract; `indexerUrls`/legacyUrls; `presets`; "Generic Newznab" /
  "Generic Torznab" catch-all definitions included). Daily *Indexer
  Definition Update* task refreshes from upstream (PR6).
- *Add Indexer* = two-stage modal over the list (picker → 720px form
  modal), exactly per §2: filters (Protocol/Language/Privacy/Categories),
  search box, (API) variant naming, then generic fields (Name, Enable,
  Redirect, Sync Profile, Tags) + definition fields rendered from the
  contract (Base Url select, credentials, per-protocol extras, Show
  Advanced), Cancel/Add footer. *Edit Indexer* reuses the modal prefilled.
- **Backend deps:** definition store + `/api/v1/indexer/schema`-equivalent
  (list definitions incl. contract); config-contract field engine
  (typed fields: textbox/password/number/checkbox/select; advanced
  flag; default values); encrypted storage of secret fields
  (AES-256-GCM per PLAN.md §8 — masked in UI, never logged).
- Accept: "add 1337x" and "add a private tracker via Generic Torznab" are
  indistinguishable in flow from Prowlarr.

### PR3 — Stats, Search, History (the hub's depth)
- **Stats**: 4 KPI cards (Active Indexers, Total Queries, Total Grabs,
  Active Apps) + query/grab time-series + per-active-indexer charts
  (client-side rendering from aggregated endpoints).
- **Search** (manual): query + indexer multi-select + category multi-
  select + "Search all indexers" + results table with per-release grab →
  download client (ties to PR5).
- **History**: query log table (indexer, query, parameters, categories,
  date, elapsed ms), Clear, filters, server-side pagination.
- **Backend deps:** query history table + retention (mirror Prowlarr's
  Clean Up History task), stats aggregation endpoints (totals +
  time-bucketed series), search fan-out endpoint (already planned in
  PLAN.md §6/§7) exposed for manual search, grab-to-client action.

### PR4 — Settings parity (7 sections, unified-service adapted)
Ordered by value/risk; this is the content of sonarr-plan P5 sections
*Indexers* and *Download Clients* plus the Prowlarr-only extras:
1. **Tags** — CRUD + usage view (indexers/proxies using each tag).
2. **Indexers (proxies)** — FlareSolverr instances, tag-based assignment
   to indexers.
3. **Notifications** — connections list (webhook/push), empty state.
4. **Apps** — *do not build 1:1*: a read-only "connected apps (legacy)"
   note or omit; per-kind category/sync behavior lives in the TV/Movies/
   Music/Books settings (one surface, four kinds).
5. **General** — host (port/urlBase/allowedHosts), security (forms auth,
   local-address exception, **API key display + rotate**, certificate
   validation, trusted networks), proxy, log level, analytics opt-out;
   "requires restart" hints where applicable.
6. **UI** — date formats, relative dates, theme (auto), color-impaired,
   language.
- **Backend deps:** settings document expansion (key/value JSON `settings`
  table already exists), API key management (issue/rotate/list),
  notification dispatch, proxy (FlareSolverr) health.

### PR5 — Download Clients (multi-client)
- Replace the single `settings/download-client` with a client **list**:
  add/edit/test/delete, *Test All Clients*, type-specific field sets
  (start with Transmission + qBittorrent + SABnzbd — matches existing
  e2e tests; extend to Prowlarr's full type set over time), the in-app-
  search-only explainer adapted to Mediarr (clients serve the Search page
  + per-kind grabs).
- Per-indexer *downloadClientId* binding (Prowlarr indexer DTO carries it).
- **Backend deps:** plural download-client model (replaces the single
  settings key — migrate existing config), per-type test adapters
  (existing qBittorrent/SABnzbd e2e coverage extends to UI flows).

### PR6 — System + migration (drop-in moment)
- **Status** (Health table + About: version, image, Go runtime equivalent
  of .NET, DB engine/version/migration, appdata dir, startup dir, mode,
  uptime; More Info links), **Tasks** (scheduled: definition update,
  indexer health check, housekeeping, backup, messaging cleanup; queue:
  running jobs — expose the planned worker inventory from PLAN.md §3),
  **Backup** (config export/import + list), **Updates** (version check +
  changelog), **Events** (live feed: Time · Component · Message, WS-backed
  — the existing events WS already feeds this), **Log Files** (rotated
  list + download + streaming viewer).
- **Drop-in moment — Prowlarr migration:** using the Prowlarr instance's
  own API (base URL + API key, from its General > Security), pull:
  indexers (name, definitionName, baseUrl, enable, redirect, priority,
  tags, configFields with encrypted credentials), download clients,
  tags, indexer proxies (FlareSolverr endpoints), and the sync-profile /
  app category settings for reference. Import into Mediarr with:
  credential re-encryption (Prowlarr plaintext → AES-256-GCM at rest;
  never echoed to logs), mapping report, **dry-run mode**, idempotent +
  resumable, Postgres target. Definition names not in the built-in
  library fall back to Generic Torznab/Newznab with the stored fields.
  Apps section is *not* imported (replaced by per-kind settings) — the
  report states this explicitly.
- **Backend deps:** structured health endpoint, task registry exposure,
  backup export/import, log streaming, Prowlarr-API importer.
- Accept: `migrate from prowlarr.local.uaccloud.com` reproduces the live
  state (7 indexers with working keys, Transmission client, tags, proxy)
  in Mediarr; a search in Mediarr hits the same indexers.

## 5. Sequencing & risk notes

- **PR1–PR2 are the visible 80%**: the hub list + the add/indexer flow is
  what Prowlarr users actually touch. Stats/Search/History (PR3) and
  Settings (PR4) are depth.
- PR2's definition library is the long pole: contract field engine +
  654-seed + upstream update task. Seed the library from Prowlarr's
  community JSON (same source Prowlarr pulls), keep definitions as
  versioned code so the update task is a data refresh, not a recompile.
- PR4/PR6 carry the backend weight (settings surface, importer,
  tasks/logs). Keep the M8 lockstep gate green every phase — spec first,
  UI second.
- **Risk: settings parity sprawls.** Mitigation: the 7-section IA is a
  *contract*; sections may start with fewer fields than Prowlarr but the
  same layout + labels, filled in per-section PRs.
- **Risk: secret handling.** Prowlarr stores indexer credentials in
  plaintext (SQLite) and shows the API key in General. Mediarr's
  encryption at rest is a feature, not a gap — but the importer must
  decrypt-in-memory and re-encrypt without logging, and the UI must keep
  the masked-secrets pattern already in Settings.tsx.
- **Risk: "Apps" is a Prowlarr-only concept.** Don't build a fake sync
  page; document the replacement (per-kind indexer binding) in the UI
  copy where Prowlarr shows *Sync App Indexers*.
- Ordering vs. the other plans: this plan's PR4 lands *into* sonarr-plan
  P5 (it is the 'Indexers' + 'Download Clients' spec); PR1–PR3 can ship
  independently of TV/Movies parity (radarr/sonarr plans) because the
  indexer layer is shared infra. The hub's home page is the *Indexers*
  section, not a kind — the cross-kind dashboard stays reachable from the
  top bar (Mediarr extra).
