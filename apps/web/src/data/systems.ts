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
      'Buy properties, pay rent to the owner and tax to the bank. If you cannot pay, your holdings go back to the bank. This is the playable part of the game today.',
    status: 'playable',
    configurable: false,
  },
  {
    id: 'trading',
    name: 'Trading & Negotiation',
    tagline: 'Trade with other players',
    description: 'Swap property and chips with other players. Designed but not playable.',
    status: 'planned',
    configurable: true,
  },
  {
    id: 'auctions',
    name: 'Auctions',
    tagline: 'Bid for contested property',
    description: 'Bid on contested property. Designed but not playable.',
    status: 'planned',
    configurable: true,
  },
  {
    id: 'gambling',
    name: 'Gambling Tables',
    tagline: 'Wager with play chips',
    description:
      'Wager play chips on poker and blackjack. Modelled in the config and protocol; no gameplay yet.',
    status: 'modeled',
    configurable: true,
  },
  {
    id: 'carnival',
    name: 'Carnival',
    tagline: 'Short skill games',
    description: 'Short skill games between turns. Designed but not playable.',
    status: 'planned',
    configurable: true,
  },
  {
    id: 'sports',
    name: 'Sports',
    tagline: 'Games between turns',
    description: 'Short competitive games between turns. Designed but not playable.',
    status: 'planned',
    configurable: true,
  },
  {
    id: 'cards',
    name: 'Treasure Cards',
    tagline: 'Draw cards that change the game',
    description:
      'Draw cards that help or hurt you. Shuffles are seeded, so every draw can be reproduced. Modelled in config only.',
    status: 'modeled',
    configurable: true,
  },
  {
    id: 'events',
    name: 'Random & Global Events',
    tagline: 'Events that affect everyone',
    description: 'Events that affect every player at set intervals. Modelled in config only.',
    status: 'modeled',
    configurable: true,
  },
  {
    id: 'victory',
    name: 'Victory Conditions',
    tagline: 'Choose how the match ends',
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
