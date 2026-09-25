-- HighJack v0.2 board loop schema.
--
-- Adds authoritative match durability on top of the v0.1 foundation:
--   - game_configs gains an id-addressed canonical document (unchanged)
--   - games gains live turn/round/state columns
--   - game_players gains position + hashed join token (reconnect identity)
--   - match_snapshots stores the full authoritative state as JSONB
--   - match_events stores the ordered event log for recovery/audit
--
-- Design note: the board + turn state is a single JSONB snapshot rather
-- than normalized rows. Board shape is configuration-driven, so a
-- relational layout would either need a generic EAV or migration churn on
-- every new space kind. The snapshot is written in the same transaction as
-- its events, so state and event log can never diverge.

ALTER TABLE games
    ADD COLUMN seed_hex         TEXT NOT NULL DEFAULT '',
    ADD COLUMN current_seat     INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN current_round    INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN turn_phase       TEXT NOT NULL DEFAULT 'await_roll'
        CHECK (turn_phase IN ('await_roll', 'await_buy_decision', 'turn_over')),
    ADD COLUMN doubles_streak   INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN award_extra_roll BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN config_hash      TEXT NOT NULL DEFAULT '',
    ADD COLUMN cursor           BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN interrupted      BOOLEAN NOT NULL DEFAULT FALSE;

-- status gains the terminal 'interrupted' value a crashed match is marked
-- with at boot (v0.2 does not restore live matches across restarts).
ALTER TABLE games DROP CONSTRAINT games_status_check;
ALTER TABLE games ADD CONSTRAINT games_status_check
    CHECK (status IN ('lobby', 'playing', 'ended', 'interrupted'));

CREATE INDEX idx_games_playing ON games (status) WHERE status = 'playing';

ALTER TABLE game_players
    ADD COLUMN position  INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN token_hash TEXT NOT NULL DEFAULT '';

-- Reconnect tokens are unique per match so a token identifies exactly one
-- player within one match.
CREATE UNIQUE INDEX idx_game_players_token
    ON game_players (game_id, token_hash)
    WHERE token_hash <> '';

CREATE TABLE match_snapshots (
    match_id     TEXT PRIMARY KEY REFERENCES games (id) ON DELETE CASCADE,
    state        JSONB NOT NULL,
    cursor       BIGINT NOT NULL,
    written_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE match_events (
    match_id     TEXT NOT NULL REFERENCES games (id) ON DELETE CASCADE,
    tick         BIGINT NOT NULL,
    seq          BIGINT NOT NULL,
    kind         TEXT NOT NULL,
    payload      JSONB NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (match_id, tick, seq)
);

-- Recovery reads a match's ordered tail; audit reads the whole log.
CREATE INDEX idx_match_events_match ON match_events (match_id, tick, seq);
