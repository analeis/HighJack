# Design System Reference

> Status: v0.2.16 — implemented in `packages/ui` and consumed by both apps.

## Architecture

```
packages/ui/
├── src/styles/tokens.css   raw CSS custom properties (--hj-*) — SOURCE OF TRUTH
├── src/styles/index.css    Tailwind v4 wiring (@theme inline) + base layer
└── src/components/*.tsx    SolidJS primitives
```

Values are declared **once** as `--hj-*` custom properties; Tailwind's
`@theme inline` maps them to utility namespaces (`bg-gold-500`,
`font-display`, `shadow-card`, …). Apps import a single stylesheet:

```css
@import '@highjack/ui/styles/index.css';
```

## Token groups

| Group     | Examples                                                 | Notes                                                                                        |
| --------- | -------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| Surfaces  | `--hj-color-felt-950…500`, `surface-base/raised/overlay` | Near-neutral darks. The felt names persist; green is reserved for the board itself.          |
| Ink       | `cream-100/300`, `muted-400/500`                         | Warm ivory and warm grey. Every step clears AA on the darkest surface.                       |
| Accents   | `gold-*`, `crimson-*`, `teal-*`, `violet-*`              | One gold accent: primary action and key numbers. Crimson/teal are semantic; violet rare.     |
| Lines     | `line-subtle`, `line-strong`                             | Translucent ivory, so the surface shows through.                                             |
| Type      | `--hj-font-display/body/mono`, scale `--hj-text-xs…5xl`  | Display at weight 600 with tight tracking; labels normal-case; mono only for code-like text. |
| Space     | 4px base, `--hj-space-1…24`                              |                                                                                              |
| Radii     | sm 6 · md 10 · lg 16 · xl 24 · pill                      |                                                                                              |
| Elevation | `shadow-card`, `shadow-raised`, `shadow-gold-glow`       | Warm-tinted two-layer shadows.                                                               |
| Motion    | durations fast/normal/slow, ease-out & spring            | Reduced-motion collapses all to instant (base layer).                                        |
| Focus     | `--hj-focus-ring`                                        | Double-ring gold; applied globally via `:focus-visible`.                                     |

## Components (`@highjack/ui`)

| Component                       | Purpose                                         | Accessibility contract                                                                                                |
| ------------------------------- | ----------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `Button`                        | primary / secondary / ghost / danger × sm/md/lg | Real `<button>`; disabled is genuinely inert; state changes never rely on color alone.                                |
| `Badge` + `StatusDot` + `Money` | labels & status                                 | StatusDot requires a text label (`role="img"`, no color-only status); Money renders safe integers only, tabular mono. |
| `Card` family                   | surface container + header/title/body           | Semantic headings via `CardTitle`.                                                                                    |
| `Switch`                        | boolean toggle                                  | `role="switch"` + `aria-checked` + required label prop.                                                               |
| `Tabs` + `TabPanel`             | tabbed panels                                   | WAI-ARIA tabs pattern: roving tabindex, Arrow/Home/End keys, `aria-selected`, labelled tablist.                       |
| `Dialog`                        | modal                                           | Native `<dialog>`: focus trap, Escape, inert background for free; backdrop click closes; always titled.               |

Utility classes: `.hj-skip-link`, `.hj-numerals`, `.hj-felt-bg`.

## Conventions

1. Never hard-code a color/size in an app when a token exists.
2. New primitives land in `packages/ui` with tests **before** any app uses
   them ad hoc.
3. Component styling uses Tailwind utilities composed from token mappings;
   the base layer holds only element defaults and semantic patterns.
4. PixiJS rendering never enters this package — the board has its own literal
   mirror of tokens in `apps/game/src/engine/board-stage.ts`. `repo sanity`
   holds that mirror to this file, so a palette change cannot leave the canvas
   in the old colours.

## Testing

Primitives carry jsdom tests (`packages/ui/test/`) asserting behavior and
accessibility contracts (roles, aria states, keyboard handling). See
[../development/TESTING.md](../development/TESTING.md).

Related reading: [ART_DIRECTION.md](./ART_DIRECTION.md),
[UI_PRINCIPLES.md](./UI_PRINCIPLES.md).
