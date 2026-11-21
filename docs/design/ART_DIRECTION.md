# Art Direction

> Status: v0.2.22 — the direction below is implemented at token and logo
> level; illustration-scale artwork is future work.

## Identity thesis

**HIGH + JACK** — one word, two energies:

- **HIGH**: upward motion, rising stacks, jackpot direction, the lift of a
  card leaving the deck.
- **JACK**: the pot, the prize, and the mischievous act of taking it.

The mark expresses both in a single gesture: a card lifting off a chip,
carrying an upward-arrow cutout. Nothing else needs to be said.

## Mood targets

| We are                   | We are not               |
| ------------------------ | ------------------------ |
| Restrained, near-neutral | Neon Vegas casino floor  |
| One considered accent    | Corporate SaaS dashboard |
| Quietly confident        | Cartoonish clip-art      |
| High-contrast, readable  | Visually noisy           |

## Color language

- **Near-neutral surfaces** (`--hj-color-felt-*`): the ground. The token names
  are historical; green is now reserved for the board itself.
- **Warm ivory ink** (`--hj-color-cream-*`, `--hj-color-muted-*`): text and
  secondary text. Every step clears AA on the darkest surface.
- **One gold** (`--hj-color-gold-*`): the only accent — the primary action, the
  active navigation item and key numbers. If everything is gold, nothing is.
- **Crimson and teal**: semantic signals (risk and success), never decoration.
- **Violet**: rare, for the occasional special state.

## Typography

- **Bricolage Grotesque** (display): headings at weight 600 with tight
  tracking — confident, not chunky.
- **Space Grotesk** (body/UI): a quiet grotesque that stays readable at small
  sizes; section labels are normal-case.
- **Space Mono** (numerals): tabular money counters and code-like text only.

## Shape & material

- Quiet corners and barely-there shadows. Sections are separated by whitespace
  and a thin rule, not by walls of rounded cards; cards remain only where the
  content is genuinely card-shaped.
- Elevation is a subtle lift, never a heavy drop shadow.
- Backgrounds get atmosphere via pure-CSS radial glows (`hj-felt-bg`),
  not texture images.

## Icon language

24×24 grid, 2px strokes, open forms, rounded joins. One accent color per
icon maximum (see `assets/source/icons/`). Icons support labels; they never
replace them where meaning matters.

## Motion philosophy

- Motion communicates **cause and consequence**, never decoration: chips
  slide because money moved; a card flips because an event fired.
- Durations: 120ms feedback, 200ms transitions, 360ms reveals; spring ease
  reserved for reward moments.
- `prefers-reduced-motion` collapses animation to instant states — the game
  remains fully playable without it.

## Board / card / token philosophy

- **Board**: a ring of spaces like a table path — tactile, top-down-ish,
  generous spacing over density.
- **Cards**: cream stock, gold trim moments, one clear glyph per card.
  Text on cards is large; nobody squints at a party.
- **Tokens/pieces**: simple silhouettes distinguishable in peripheral
  vision; color-blind safe via shape+pattern, never hue alone.

## Logo usage

- Mark works at 16px (favicon) through billboard scale; minimum clear space
  equals the chip radius.
- Wordmark is typeset in Bricolage Grotesque bold ("High" cream, "Jack"
  gold) rather than baked into vectors — it stays crisp everywhere and the
  font ships with the product.
