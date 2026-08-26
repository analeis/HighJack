# Art Direction

> Status: v0.1.0 Foundation — the direction below is implemented at token
> and logo level; illustration-scale artwork is future work.

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
| Tactile card-table night | Neon Vegas casino floor  |
| Premium felt & brass     | Corporate SaaS dashboard |
| Playfully mischievous    | Cartoonish clip-art      |
| High-contrast, readable  | Visually noisy           |

## Color language

- **Felt greens-black** (`--hj-color-felt-*`): the world's ground. Deep,
  calm, premium.
- **Cream ink** (`--hj-color-cream-*`): warm paper-white text. Pure white
  feels sterile against felt.
- **Jackpot gold** (`--hj-color-gold-*`): THE accent. Reward energy, used
  for primary actions, wins, and focus. If everything is gold, nothing is.
- **Risk crimson**: stakes and elimination only.
- **Fortune teal**: gains, success, live states.
- **Chaos violet**: random events and wildcard moments only. Rare by
  design — scarcity keeps it special.

## Typography

- **Bricolage Grotesque** (display): chunky personality for headings;
  slightly irreverent without losing legibility.
- **Space Grotesk** (body/UI): characterful grotesque that stays quiet at
  small sizes.
- **Space Mono** (numerals): tabular money counters and tickers; exactness
  you can feel.

## Shape & material

- Radii from tokens (6–24px); cards read as _cards_: rounded rectangles on
  felt with warm shadows, never gray drop-shadows.
- Elevation = physical lift off the table (two-layer warm shadow).
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
