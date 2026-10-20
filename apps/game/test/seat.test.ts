import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { forgetSeat, readStoredSeat, rememberSeat } from '../src/net/connection.ts';

/**
 * The seat store.
 *
 * A seat is a bearer credential: the server keeps only its digest, and the
 * client keeps the secret. It never expires and is never revoked, so how the
 * client stores it is a security question, not a convenience one.
 *
 * The store used to hold a single seat under one global key, and nothing ever
 * removed it. Joining a second table therefore discarded the first seat, a
 * reload always returned to whichever table was joined last — including one
 * that had already ended — and the token sat in local storage for the life of
 * the browser.
 */

/** A minimal localStorage, since the tests run in the node environment. */
class MemoryStorage {
  private map = new Map<string, string>();
  getItem(key: string): string | null {
    return this.map.get(key) ?? null;
  }
  setItem(key: string, value: string): void {
    this.map.set(key, value);
  }
  removeItem(key: string): void {
    this.map.delete(key);
  }
  get size(): number {
    return this.map.size;
  }
}

const original = (globalThis as { localStorage?: unknown }).localStorage;
let store: MemoryStorage;

beforeEach(() => {
  store = new MemoryStorage();
  (globalThis as { localStorage?: unknown }).localStorage = store;
});

afterEach(() => {
  if (original === undefined) {
    delete (globalThis as { localStorage?: unknown }).localStorage;
  } else {
    (globalThis as { localStorage?: unknown }).localStorage = original;
  }
});

describe('the stored seat', () => {
  it('round-trips a seat', () => {
    rememberSeat('m_one', 'tok_one', 'pl_one');
    const seat = readStoredSeat();
    expect(seat).toEqual({ matchId: 'm_one', token: 'tok_one', playerId: 'pl_one' });
  });

  it('keeps a seat per match rather than one seat overall', () => {
    rememberSeat('m_one', 'tok_one', 'pl_one');
    rememberSeat('m_two', 'tok_two', 'pl_two');

    expect(readStoredSeat('m_one')).toEqual({
      matchId: 'm_one',
      token: 'tok_one',
      playerId: 'pl_one',
    });
    expect(readStoredSeat('m_two')).toEqual({
      matchId: 'm_two',
      token: 'tok_two',
      playerId: 'pl_two',
    });
  });

  it('returns the most recent seat when no match is named', () => {
    rememberSeat('m_one', 'tok_one', 'pl_one');
    rememberSeat('m_two', 'tok_two', 'pl_two');
    expect(readStoredSeat()?.matchId).toBe('m_two');
  });

  it('forgets one seat without discarding the others', () => {
    rememberSeat('m_one', 'tok_one', 'pl_one');
    rememberSeat('m_two', 'tok_two', 'pl_two');

    forgetSeat('m_one');

    expect(readStoredSeat('m_one')).toBeNull();
    expect(readStoredSeat('m_two')).not.toBeNull();
  });

  it('forgets every seat when no match is named', () => {
    rememberSeat('m_one', 'tok_one', 'pl_one');
    rememberSeat('m_two', 'tok_two', 'pl_two');

    forgetSeat();

    expect(readStoredSeat()).toBeNull();
    // The entry is removed, not left behind as an empty object.
    expect(store.size).toBe(0);
  });

  it('bounds how many seats are kept', () => {
    for (let i = 0; i < 20; i++) {
      rememberSeat(`m_${i}`, `tok_${i}`, `pl_${i}`);
    }
    // The oldest is gone; the newest is not.
    expect(readStoredSeat('m_0')).toBeNull();
    expect(readStoredSeat('m_19')).not.toBeNull();
  });

  it('ignores a seat with an empty part', () => {
    rememberSeat('', 'tok', 'pl');
    rememberSeat('m_one', '', 'pl');
    rememberSeat('m_two', 'tok', '');
    expect(readStoredSeat()).toBeNull();
  });

  it('discards a corrupted store rather than throwing', () => {
    store.setItem('highjack.seat.v1', '{not json');
    expect(readStoredSeat()).toBeNull();
  });

  it('discards entries that are not seats', () => {
    store.setItem('highjack.seat.v1', JSON.stringify({ m_one: { token: 1 }, m_two: 'nope' }));
    expect(readStoredSeat()).toBeNull();
  });
});
