# UI Principles

How we make interface decisions across the website and (future) game
client. When two options are equally easy, these break ties.

## 1. The table is honest

The UI never implies capability that does not exist. Unimplemented
controls are visibly disabled with a hint; unreleased systems carry
status badges; the changelog tells the truth. Trust is a feature.

## 2. Readable first, fancy second

- Contrast meets WCAG AA at minimum on every surface pair we ship.
- Body text ≥ 16px equivalent; game-critical numbers use tabular mono so
  columns don't jitter.
- Text wraps (`text-wrap: balance/pretty`) instead of orphaning.

## 3. Keyboard is a first-class player

Every interactive element is reachable and operable by keyboard, focus is
always visible (`--hj-focus-ring`), and composite widgets follow their
WAI-ARIA pattern exactly (tabs: roving tabindex + arrows; dialogs: native
`<dialog>` semantics).

## 4. Status is never color-only

Success/danger/pending always pair color with text, icon, or shape
(`StatusDot` requires a label). Money never relies on sign or hue alone —
it's formatted.

## 5. Motion explains, or it disappears

Animation must communicate causality (what changed, where it went).
Decorative loops don't ship. `prefers-reduced-motion` yields an instant,
complete experience — verified in tests, not just respected in CSS.

## 6. Progressive enhancement over hydration religion

Static HTML that works without JavaScript beats a client-only shell.
Islands hydrate when scrolled into view (`client:visible`) unless they are
the page's primary interaction.

## 7. Layers stay separated

Application UI (Solid), render surface (Pixi), and domain logic (Go
engine) never reach into each other. Shared state crosses boundaries only
through defined stores/protocol. If a change needs to violate this, the
boundary is wrong or the change is — fix the boundary first.

## 8. Density where decisions happen

Game surfaces prioritize decision-relevant information (money, stakes,
whose turn) at high contrast near the action; flavor lives further out.
Whitespace is not wasted space on a table — it's felt.
