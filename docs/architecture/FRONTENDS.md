# Frontend Architecture

> Status: v0.1.0 Foundation. The website is complete for its scope; the
> game client is a bootable shell demonstrating the layering.

## Two frontends, one boundary

| App         | Role                                                                         | Stack                                             |
| ----------- | ---------------------------------------------------------------------------- | ------------------------------------------------- |
| `apps/web`  | Product website: marketing, docs-flavored content, changelog, config preview | Astro (static) + SolidJS islands + Tailwind v4    |
| `apps/game` | Future multiplayer client                                                    | SolidJS application surface + PixiJS render stage |

Both consume `@highjack/ui` (design system) and `@highjack/protocol`
(wire contract). Neither contains domain logic — that belongs to the Go
engine.

## Website (`apps/web`)

Routes: `/`, `/game`, `/features`, `/rules`, `/about`, `/changelog`.

Principles:

- **Static-first**: every page renders to HTML at build time. Navigation
  works without JavaScript; islands hydrate progressively
  (`client:visible`).
- **Honesty constraint**: copy may only describe systems that exist in the
  repository. Availability badges (Modeled / In development / Planned) are
  driven by `src/data/systems.ts`, which mirrors `docs/game/GAME_DESIGN.md`.
- **The one interactive island with substance** is the configuration
  playground on `/features`: it builds a real GameConfig and validates it
  with `@highjack/protocol` — the identical schema the engine enforces.

## Game shell (`apps/game`)

The shell exists to prove the composition, not to fake gameplay:

```
┌───────────────────────────────────────────────┐
│ SolidJS application UI                        │
│  top bar · side panels · action dock · badges │
│                                               │
│        ↕ shell store (signals only)           │
│                                               │
│ PixiJS render stage                           │
│  board ring · pieces/cards · effects          │
│  (engine/pixi-stage.ts owns the canvas)       │
└───────────────────────────────────────────────┘
```

Rules enforced by structure:

- UI components never touch Pixi internals; the stage never touches DOM.
- All cross-layer state flows through `src/state/shell-store.ts` signals.
- The stage respects `prefers-reduced-motion` (static render instead of
  animated).
- Disabled controls are labeled as such ("concept preview", "not
  implemented yet"). Nothing pretends to be playable.

When real gameplay lands, the same seam carries authoritative snapshots and
events into the store; panels and stage react independently.

## Responsive & accessibility baselines

- Website breakpoints are intentional compositions (stacked mobile →
  two-column tablet → three-column desktop), not scaled desktop.
- Skip link, landmarks, visible focus tokens, aria-current navigation,
  reduced-motion support, no color-only status communication.
- Playwright + axe smoke tests guard these (see TESTING.md).

## What is deliberately absent

Authentication flows, lobbies, matchmaking, real-time boards, chat,
payments. The site marks them as future work rather than stubbing them.
