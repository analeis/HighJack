# Frontend Architecture

> Status: v0.2.23. The website is complete for its scope; the game client
> is a playable board loop over the authoritative backend.

## Two frontends, one boundary

| App         | Role                                                                         | Stack                                             |
| ----------- | ---------------------------------------------------------------------------- | ------------------------------------------------- |
| `apps/web`  | Product website: marketing, docs-flavored content, changelog, config preview | Astro (static) + SolidJS islands + Tailwind v4    |
| `apps/game` | Playable multiplayer client                                                  | SolidJS application surface + PixiJS render stage |

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

## Game client (`apps/game`)

The client renders authoritative snapshots and sends player choices:

```
┌───────────────────────────────────────────────┐
│ SolidJS application UI                        │
│  top bar · side panel · action dock · badges  │
│                                               │
│        ↕ game store (signals only)            │
│                                               │
│ PixiJS render stage                           │
│  board ring · pieces/cards · effects          │
│  (engine/board-stage.ts owns the canvas)      │
└───────────────────────────────────────────────┘
```

Rules enforced by structure:

- UI components never touch Pixi internals; the stage never touches DOM.
- All cross-layer state flows through `src/state/game-store.ts` signals.
- The stage respects `prefers-reduced-motion` (static render instead of
  animated).
- The dock renders the engine's per-recipient legal-action list. It does not
  re-derive affordability, turn ownership or start eligibility, so a control
  is enabled exactly when the server would accept it.

Snapshots and transitions arrive over the socket into the store; panels and
stage react independently to the same view.

## Responsive & accessibility baselines

- Website breakpoints are intentional compositions (stacked mobile →
  two-column tablet → three-column desktop), not scaled desktop.
- Skip link, landmarks, visible focus tokens, aria-current navigation,
  reduced-motion support, no color-only status communication.
- Playwright + axe smoke tests guard these (see TESTING.md).

## What is deliberately absent

Accounts/authentication, public matchmaking and lobby listings, chat,
payments, trading, auctions and the other unbuilt systems in GAME_DESIGN.md.
Private lobbies, real-time boards and reconnect are present. The site marks
the rest as future work rather than stubbing them.
