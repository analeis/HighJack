-- HighJack foundational schema (v0.1).
--
-- Only entities with immediate architectural justification:
-- users, games, game_players, game_configs. Gameplay-specific tables
-- arrive with the features that need them.

CREATE TABLE users (
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 32),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE game_configs (
    id            TEXT PRIMARY KEY,
    schema_version INTEGER NOT NULL,
    document      JSONB NOT NULL,
    document_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_game_configs_hash ON game_configs (document_hash);

CREATE TABLE games (
    id          TEXT PRIMARY KEY,
    status      TEXT NOT NULL CHECK (status IN ('lobby', 'playing', 'ended')),
    config_id   TEXT NOT NULL REFERENCES game_configs (id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at    TIMESTAMPTZ
);

CREATE INDEX idx_games_status ON games (status);

CREATE TABLE game_players (
    game_id   TEXT NOT NULL REFERENCES games (id),
    user_id   TEXT NOT NULL REFERENCES users (id),
    seat      INTEGER NOT NULL CHECK (seat >= 0),
    money     BIGINT NOT NULL CHECK (money >= 0),
    status    TEXT NOT NULL CHECK (status IN ('active', 'eliminated', 'disconnected')),
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (game_id, seat)
);

-- A user participates at most once per match.
CREATE UNIQUE INDEX idx_game_players_user ON game_players (game_id, user_id);
