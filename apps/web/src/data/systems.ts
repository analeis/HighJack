/**
 * The HighJack system catalog as it actually stands: what is modeled,
 * what is in development, and what is planned. The website must never
 * claim more than exists — statuses here drive badges across pages and
 * are mirrored by docs/game/GAME_DESIGN.md.
 */

export type SystemStatus =
  | 'playable' // fully implemented and playable end to end today
  | 'modeled' // exists in config/protocol/engine foundations, but no gameplay yet
  | 'in-development' // actively being built next
  | 'planned'; // designed-for, not started

export interface GameSystem {
  id: string;
  name: string;
  tagline: string;
  description: string;
  status: SystemStatus;
  /** Systems exposed as toggles in the interactive configuration preview. */
  configurable: boolean;
}

export const SYSTEMS: readonly GameSystem[] = [
  {
    id: 'economy',
    name: 'Property & Economy',
    tagline: 'Buy, hold, collect rent',
    description:
      'The playable economic backbone: buy properties, pay rent to the owner, pay tax to the bank. No debt — miss a payment and your holdings revert to the bank. Development levels are reserved for a later release.',
    status: 'playable',
    configurable: false,
  },
  {
    id: 'trading',
    name: 'Trading & Negotiation',
    tagline: 'Every deal is personal',
    description:
      'Player-to-player trades with offers, counteroffers, and escrowed swaps. Negotiation is where tables are won — or flipped.',
    status: 'planned',
    configurable: true,
  },
  {
    id: 'auctions',
    name: 'Auctions',
    tagline: 'Going once, going twice',
    description:
      'Contested assets go under the hammer. Open or sealed-bid formats, configurable reserve rules.',
    status: 'planned',
    configurable: true,
  },
  {
    id: 'gambling',
    name: 'Gambling Tables',
    tagline: 'Stakes on stakes',
    description:
      'Optional wager-based subsystems: poker nights, blackjack pits, and a casino floor of quick-play luck games. Play-money only — always.',
    status: 'modeled',
    configurable: true,
  },
  {
    id: 'carnival',
    name: 'Carnival',
    tagline: 'Skill-ish sideshows',
    description:
      'Skeeball physics, ring tosses, and prize booths. Light, loud, and completely optional.',
    status: 'planned',
    configurable: true,
  },
  {
    id: 'sports',
    name: 'Sports',
    tagline: 'Halftime at the table',
    description:
      'Quick competitive minigames between turns — penalties, shootouts, sprints. Losers pay up.',
    status: 'planned',
    configurable: true,
  },
  {
    id: 'cards',
    name: 'Treasure Cards',
    tagline: 'Fate, dealt',
    description:
      'Draw decks with fortune and misfortune effects. Seeded shuffles keep every draw reproducible.',
    status: 'modeled',
    configurable: true,
  },
  {
    id: 'events',
    name: 'Random & Global Events',
    tagline: 'The table turns',
    description:
      'Scheduled chaos: market crashes, jackpot rain, rule twists that hit everyone at once. Interval configurable per match.',
    status: 'modeled',
    configurable: true,
  },
  {
    id: 'victory',
    name: 'Victory Conditions',
    tagline: 'Decide how it ends',
    description:
      'Last player standing, first to target wealth, or best net worth when the round limit hits. Evaluated by the server after every move. Chosen per match.',
    status: 'playable',
    configurable: false,
  },
];

export function statusLabel(status: SystemStatus): string {
  switch (status) {
    case 'playable':
      return 'Playable';
    case 'modeled':
      return 'Modeled';
    case 'in-development':
      return 'In development';
    case 'planned':
      return 'Planned';
  }
}
