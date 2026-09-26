-- HighJack v0.2.0 fix: allow the pre-match (empty) turn phase.
--
-- games.turn_phase was constrained to the three in-play turn phases, but
-- a match row is created before the match starts and therefore has no turn
-- yet. The engine models that as the zero value, so inserts were rejected.
-- Existing rows are unaffected: the constraint is only relaxed.
--
-- Forward-only: DROP + ADD CONSTRAINT in one transaction.

ALTER TABLE games DROP CONSTRAINT games_turn_phase_check;

ALTER TABLE games ADD CONSTRAINT games_turn_phase_check
    CHECK (
        turn_phase IN ('', 'await_roll', 'await_buy_decision', 'turn_over')
    );
