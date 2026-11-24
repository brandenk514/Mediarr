# Design system (issue #115, epic #114)

Consistent, dark-first foundation for the unified UI. The unified shell
(#116) and every later view (#117–#123) build on the tokens and primitives
here. Existing pages are untouched; they keep their P1 CSS, which now lives
in the `app` cascade layer and re-aliases the canonical tokens.

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

Single source of truth; every token becomes a Tailwind utility.

### Color — surfaces (dark-first)

| Token                 | Value    | Utility example        | Use                              |
| --------------------- | -------- | ---------------------- | -------------------------------- |
| `--color-bg`          | `#0f1216`| `bg-bg`                | page background (charcoal)       |
| `--color-content`     | `#12161d`| `bg-content`           | content pane                     |
| `--color-surface`     | `#1b2129`| `bg-surface`           | panels, cards                    |
| `--color-surface-raised` | `#222a34` | `bg-surface-raised`  | hover rows, popovers             |
| `--color-rail` / `-hover` / `-active` | `#141920`/`#1b222c`/`#212b36` | `bg-rail`, `bg-rail-active` | sidebar (rail) |
| `--color-topbar` / `-hover` | `#115e59`/`#0f524d` | `bg-topbar`    | top bar                          |

### Color — semantic

| Token                | Value    | Utility          | Use                        |
| -------------------- | -------- | ---------------- | -------------------------- |
| `--color-text`       | `#e6e8ee`| `text-text`      | primary text               |
| `--color-text-muted` | `#8b91a0`| `text-text-muted`| secondary text             |
| `--color-border`     | `#2a313c`| `border-border`  | default border             |
| `--color-accent`     | `#14b8a6`| `bg-accent`      | actions, active, focus     |
| `--color-accent-hover` | `#0d9488`| `hover:bg-accent-hover` | accent hover         |
| `--color-success`    | `#3fb950`| `text-success`   | ok / complete / live       |
| `--color-warning`    | `#e08214`| `text-warning`   | queued / warning           |
| `--color-error`      | `#f85149`| `text-error`     | failed / missing           |
| `--color-foot` / `-text` | `#e9edf2`/`#2b313b` | `bg-foot` | poster-grid light footers |

### Typography

Families: `--font-sans` (Inter stack), `--font-mono` → `font-sans`,
`font-mono`. Scale (size / line-height): `text-2xs` (11/14), `text-xs`
(12/16), `text-sm` (14/20), `text-base` (16/24), `text-lg` (18/28),
`text-xl` (20/28), `text-2xl` (24/32), `text-3xl` (30/36). Weights
(canonical usage): regular 400, medium 500 (nav, tables), semibold 600
(headings, badges), bold 700 (brand, KPIs).

### Spacing

4px base grid (`--spacing: 0.25rem`); numeric utilities (`p-1` = 4px,
`gap-3` = 12px, ...). Canonical scale used by the primitives:
4 / 8 / 12 / 16 / 20 / 24 / 32 / 48 px.

Legacy aliases: the P1 `:root` variables (`--bg`, `--panel`, `--dim`,
`--ok`, ...) now resolve to these tokens, so old pages keep working without
duplication.

## Primitives (`ui/src/components/primitives/`)

Import from the barrel: `import { Badge, Modal, ... } from
'./components/primitives'`.

| Component                          | Purpose                                                                  |
| ---------------------------------- | ------------------------------------------------------------------------ |
| `PosterCard`                       | poster/cover art (or initials stand-in), title, meta, status badge, hover actions |
| `Badge`, `BadgeGroup`              | kind / status / genre badges (6 tones: neutral, accent, info, success, warning, error) |
| `Modal`, `ConfirmDialog`           | detail overlays + confirm dialogs; portal, Escape/overlay close, focus management |
| `Dropdown`                         | filters, sort menus, option pickers; real menu semantics, disabled options |
| `SidebarItem`                      | nav row: icon, label, active state (`aria-current`), trailing badge slot |

All primitives are UI-only (no data wiring — the shell/views wire
callbacks), use token utilities throughout, and carry unit tests
(`*.test.tsx` next to the components, vitest + Testing Library).

## Not done here (deliberate scope)

- No light theme (dark-first per #115).
- Existing pages are not migrated — the unified shell (#116) and views
  (#117–#123) adopt the primitives; the P1 components in
  `components/ui.tsx` / `poster.tsx` stay until then.
- No API contract, auth, or packaging changes (UI layer only; see #86:
  built assets ship at `/home/nonroot/ui`, served same-origin on :8080).
