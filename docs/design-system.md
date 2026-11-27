# Design system (issue #115, epic #114; palette retune: issue #147)

Consistent, dark-first foundation for the unified UI. The unified shell
(#116) and every later view (#117–#123) build on the tokens and primitives
here. Existing pages are untouched; they keep their P1 CSS, which now lives
in the `app` cascade layer and re-aliases the canonical tokens.

Since issue #147 the token values are the deterministic **Seerr source
palette** (seerr fork @ 9399fe5a, MIT (c) 2020 sct): the near-black-navy
Tailwind gray scale for surfaces/text/lines, indigo for the accent, and the
Seerr status hues. Token **names** and utility mappings are unchanged, so
pages pick up the retune with zero edits. Sources (re-verified against the
fork): `src/styles/globals.css` (body `bg-gray-900`, `.sidebar` scrim
gradient, gray-700/600 lines), `src/components/Discover/constants.ts`
(colorTones, genreColorMap), `tailwind.config.js` (Inter Variable).

## Stack: Tailwind v4

`ui/src/index.css` loads Tailwind v4 through `@tailwindcss/vite`
(`@import "tailwindcss"` + `@import './styles/tokens.css'`). The cascade
layer order is:

```
theme < base (preflight) < app < components < utilities
```

- `theme` — design tokens (`src/styles/tokens.css`, `@theme`).
- `base` — Tailwind preflight (CSS reset).
- `app` — all legacy hand-written app CSS (the former index.css body).
  Sits **above** preflight, so legacy rules still win over the reset; and a
  small preflight-compatibility block at its top restores the browser UA
  defaults (unstyled headings, paragraphs, lists, link underlines) the
  legacy CSS was written against, so existing pages render unchanged.
- `utilities` — Tailwind utility classes. Sits **above** `app`, so the
  primitives' utility classes override the legacy global element rules
  (`button`, `input`, `label`, ...) — the behavior the unified shell needs.

## Tokens (`ui/src/styles/tokens.css`)

Single source of truth; every `@theme` token becomes a Tailwind utility.

### Color — surfaces (dark-first, Seerr near-black-navy family)

| Token                                     | Value                                                                                                   | Utility example        | Use                                        |
| ----------------------------------------- | ------------------------------------------------------------------------------------------------------- | ---------------------- | ------------------------------------------ |
| `--color-bg`                              | `#111827` (gray-900)                                                                                     | `bg-bg`                | page background (Seerr body)               |
| `--color-content`                         | `#1f2937` (gray-800)                                                                                     | `bg-content`           | content pane                               |
| `--color-surface`                         | `#1f2937` (gray-800)                                                                                     | `bg-surface`           | panels, cards (bordered, like the fork)    |
| `--color-surface-raised`                  | `#374151` (gray-700)                                                                                     | `bg-surface-raised`    | hover rows, popovers                       |
| `--color-rail` / `-hover` / `-active`     | `#1f2937` / `#374151` / `#4b5563` (gray-800/700/600)                                                     | `bg-rail`, `bg-rail-active` | sidebar (rail)                            |
| `--color-rail-scrim`                      | `linear-gradient(180deg, rgba(31, 41, 55, 1) 0%, #131928 100%)` (seerr `globals.css` `.sidebar`)        | `background: var(--color-rail-scrim)` | sidebar scrim (the #148 sidebar background) |

`--color-rail-scrim` and the `--tone-*` pairs below are plain `:root`
custom properties (outside `@theme`) so they always ship verbatim.

### Color — semantic

| Token                | Value                   | Utility                  | Use                        |
| -------------------- | ----------------------- | ------------------------ | -------------------------- |
| `--color-text`       | `#e5e7eb` (gray-200)    | `text-text`              | primary text               |
| `--color-text-muted` | `#9ca3af` (gray-400)    | `text-text-muted`        | secondary text             |
| `--color-border`     | `#4b5563` (gray-600)    | `border-border`          | default border             |
| `--color-accent`     | `#4f46e5` (indigo-600)  | `bg-accent`              | actions, active, focus (primary) |
| `--color-accent-hover` | `#6366f1` (indigo-500) | `hover:bg-accent-hover`  | accent hover / bright      |
| `--color-success`    | `#22c55e` (green-500)   | `text-success`           | ok / complete / live       |
| `--color-warning`    | `#eab308` (yellow-500)  | `text-warning`           | queued / warning           |
| `--color-error`      | `#ef4444` (red-500)     | `text-error`             | failed / missing           |
| `--color-foot` / `-text` | `#f3f4f6` / `#374151` (gray-100/700) | `bg-foot` | poster-grid light footers |

### Duotone tones (Seerr port, issue #147)

The 14 `colorTones` and the `genreColorMap` are ported **verbatim** from
`seerr/src/components/Discover/constants.ts` @ 9399fe5a (MIT (c) 2020 sct).
In the fork they feed the TMDB `duotone` backdrop filter for Discover genre
cards; here they are exposed twice, so issue #151 (Discover) consumes them
without re-porting:

- **TS**: `ui/src/lib/tone-map.ts` — `colorTones`, `toneNames`,
  `genreColorMap`, `toneForGenre()` (black fallback, mirroring the fork's
  `genreColorMap[genre.id] ?? genreColorMap[0]` call sites).
- **CSS**: `--tone-<name>-dark` / `--tone-<name>-light` custom properties
  in `tokens.css` (a `:root` block, always emitted).

Each tone is a `[dark, light]` hex pair. The Badge duotone pill renders the
reference style: dark-hex background, light-hex text/border.

| Tone        | `--tone-*-dark` | `--tone-*-light` |
| ----------- | --------------- | ---------------- |
| `red`       | `#991B1B`       | `#FCA5A5`        |
| `darkred`   | `#1F2937`       | `#F87171`        |
| `blue`      | `#032541`       | `#01b4e4`        |
| `lightblue` | `#1F2937`       | `#60A5FA`        |
| `darkblue`  | `#1F2937`       | `#2864d2`        |
| `orange`    | `#92400E`       | `#FCD34D`        |
| `lightgreen`| `#065F46`       | `#6EE7B7`        |
| `green`     | `#087d29`       | `#21cb51`        |
| `purple`    | `#5B21B6`       | `#C4B5FD`        |
| `yellow`    | `#777e0d`       | `#e4ed55`        |
| `darkorange`| `#552c01`       | `#d47c1d`        |
| `black`     | `#1F2937`       | `#D1D5DB`        |
| `pink`      | `#9D174D`       | `#F9A8D4`        |
| `darkpurple`| `#480c8b`       | `#a96bef`        |

`genreColorMap` (TMDB genre id → tone; verbatim from the fork):

| id    | genre              | tone        |
| ----- | ------------------ | ----------- |
| 0     | (fallback)         | `black`     |
| 28    | Action             | `red`       |
| 12    | Adventure          | `darkpurple`|
| 16    | Animation          | `blue`      |
| 35    | Comedy             | `orange`    |
| 80    | Crime              | `darkblue`  |
| 99    | Documentary        | `lightgreen`|
| 18    | Drama              | `pink`      |
| 10751 | Family             | `yellow`    |
| 14    | Fantasy            | `lightblue` |
| 36    | History            | `orange`    |
| 27    | Horror             | `black`     |
| 10402 | Music              | `blue`      |
| 9648  | Mystery            | `purple`    |
| 10749 | Romance            | `pink`      |
| 878   | Science Fiction    | `lightblue` |
| 10770 | TV Movie           | `red`       |
| 53    | Thriller           | `black`     |
| 10752 | War                | `darkred`   |
| 37    | Western            | `orange`    |
| 10759 | Action & Adventure | `darkpurple`|
| 10762 | Kids               | `blue`      |
| 10763 | News               | `black`     |
| 10764 | Reality            | `darkorange`|
| 10765 | Sci-Fi & Fantasy   | `lightblue` |
| 10766 | Soap               | `pink`      |
| 10767 | Talk               | `lightgreen`|
| 10768 | War & Politics     | `darkred`   |

### Typography

Families: `--font-sans` (Inter leading the stack, system fallbacks),
`--font-mono`. The Seerr fork ships **Inter Variable**
(`tailwind.config.js`: `fontFamily.sans = ['Inter Variable', ...]`) — the
same family we lead with; we keep the P1 stack/scale and add no font
dependency in #147. Scale (size / line-height): `text-2xs` (11/14),
`text-xs` (12/16), `text-sm` (14/20), `text-base` (16/24), `text-lg`
(18/28), `text-xl` (20/28), `text-2xl` (24/32), `text-3xl` (30/36).
Weights (canonical usage): regular 400, medium 500 (nav, tables),
semibold 600 (headings, badges), bold 700 (brand, KPIs).

### Spacing

4px base grid (`--spacing: 0.25rem`); numeric utilities (`p-1` = 4px,
`gap-3` = 12px, ...). Canonical scale used by the primitives:
4 / 8 / 12 / 16 / 20 / 24 / 32 / 48 px.

Legacy aliases: the P1 `:root` variables (`--bg`, `--panel`, `--dim`,
`--ok`, ...) now resolve to these tokens, so old pages keep working without
duplication. The `--topbar` / `--topbar-2` aliases and the
`--color-topbar` / `--color-topbar-hover` tokens were deleted outright in
#148, together with the legacy topbar layout's retirement.

## Primitives (`ui/src/components/primitives/`)

Import from the barrel: `import { Badge, Modal, ... } from
'./components/primitives'`.

| Component                          | Purpose                                                                  |
| ---------------------------------- | ------------------------------------------------------------------------ |
| `PosterCard`                       | poster/cover art (or initials stand-in), title, meta, status badge, hover actions |
| `Badge`, `BadgeGroup`              | kind / status / genre badges: 6 semantic tones (neutral, accent, info, success, warning, error) + 14 Seerr duotone genre tones (#147: dark-hex bg, light-hex text/border) |
| `Modal`, `ConfirmDialog`           | detail overlays + confirm dialogs; portal, Escape/overlay close, focus management |
| `Dropdown`                         | filters, sort menus, option pickers; real menu semantics, disabled options |
| `SidebarItem`                      | nav row: icon, label, active state (`aria-current` + solid accent pill, #147), trailing badge slot |

All primitives are UI-only (no data wiring — the shell/views wire
callbacks), use token utilities throughout, and carry unit tests
(`*.test.tsx` next to the components, vitest + Testing Library).

## Not done here (deliberate scope)

- No light theme (dark-first per #115).
- Existing pages are not migrated — the unified shell (#116) and views
  (#117–#123) adopt the primitives; the P1 components in
  `components/ui.tsx` / `poster.tsx` stay until then.
- The legacy topbar layout was retired in #148: the shell is now the Seerr
  shape (full-height sidebar with brand + nav + version pill, and a content
  header with the pill search + avatar menu); the artwork API is #149.
- No API contract, auth, or packaging changes (UI layer only; see #86:
  built assets ship at `/home/nonroot/ui`, served same-origin on :8080).
