-- +goose Up
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE users (
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    is_guest          BOOLEAN NOT NULL DEFAULT true,
    display_name      TEXT NOT NULL,
    email             CITEXT UNIQUE,
    password_hash     TEXT,
    email_verified_at TIMESTAMPTZ,
    role              TEXT NOT NULL DEFAULT 'player'
                      CHECK (role IN ('player', 'moderator', 'admin')),
    status            TEXT NOT NULL DEFAULT 'active'
                      CHECK (status IN ('active', 'banned', 'self_excluded')),
    status_until      TIMESTAMPTZ
);

CREATE TABLE sessions (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ip           TEXT,
    user_agent   TEXT,
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX idx_sessions_user ON sessions (user_id);

CREATE TABLE oauth_identities (
    provider         TEXT NOT NULL,
    provider_user_id TEXT NOT NULL,
    user_id          BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (provider, provider_user_id)
);

CREATE TABLE email_tokens (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL CHECK (kind IN ('verify_email', 'reset_password')),
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ
);

CREATE TABLE audit_log (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    action        TEXT NOT NULL,
    target_type   TEXT,
    target_id     BIGINT,
    metadata      JSONB NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE wallets (
    user_id         BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    balance_credits BIGINT NOT NULL DEFAULT 0 CHECK (balance_credits >= 0)
);

CREATE TABLE transactions (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    wallet_id       BIGINT NOT NULL REFERENCES wallets(user_id),
    kind            TEXT NOT NULL,
    amount_credits  BIGINT NOT NULL,
    bet_id          BIGINT,
    idempotency_key TEXT NOT NULL UNIQUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_transactions_wallet ON transactions (wallet_id, created_at);

CREATE TABLE rooms (
    id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    game_id   TEXT NOT NULL,
    slug      TEXT NOT NULL UNIQUE,
    name      TEXT NOT NULL,
    min_bet   BIGINT NOT NULL,
    max_bet   BIGINT NOT NULL,
    capacity  INT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT true
);

-- Commit-reveal hash chain for shared-round fairness. seed[i] = sha256(seed[i+1]);
-- only the hash is stored until the seed is revealed at its turn.
CREATE TABLE chain_seeds (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    index       BIGINT NOT NULL UNIQUE,
    seed_hash   TEXT NOT NULL,
    seed_plain  TEXT,
    revealed_at TIMESTAMPTZ
);

CREATE TABLE rounds (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    room_id       BIGINT REFERENCES rooms(id),
    game_id       TEXT NOT NULL,
    chain_seed_id BIGINT REFERENCES chain_seeds(id),
    salt          TEXT NOT NULL DEFAULT '',
    state         TEXT NOT NULL
                  CHECK (state IN ('betting_open', 'locked', 'running', 'settled')),
    result        JSONB,
    opened_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at     TIMESTAMPTZ,
    settled_at    TIMESTAMPTZ
);
CREATE INDEX idx_rounds_room_state ON rounds (room_id, state);

CREATE TABLE server_seeds (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    seed_hash   TEXT NOT NULL,
    seed_plain  TEXT,
    is_active   BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    revealed_at TIMESTAMPTZ
);
-- One active server seed per user.
CREATE UNIQUE INDEX idx_server_seeds_active ON server_seeds (user_id) WHERE is_active;

CREATE TABLE bets (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    game_id        TEXT NOT NULL,
    round_id       BIGINT NOT NULL REFERENCES rounds(id),
    bet_credits    BIGINT NOT NULL CHECK (bet_credits > 0),
    payout_credits BIGINT NOT NULL DEFAULT 0,
    server_seed_id BIGINT NOT NULL REFERENCES server_seeds(id),
    client_seed    TEXT NOT NULL,
    nonce          BIGINT NOT NULL,
    outcome        JSONB,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (server_seed_id, nonce)
);
CREATE INDEX idx_bets_user ON bets (user_id, created_at DESC);

CREATE TABLE themes (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    palette    JSONB NOT NULL,
    sprites    JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE themes;
DROP TABLE bets;
DROP TABLE server_seeds;
DROP INDEX IF EXISTS idx_rounds_room_state;
DROP TABLE rounds;
DROP TABLE chain_seeds;
DROP TABLE rooms;
DROP INDEX IF EXISTS idx_transactions_wallet;
DROP TABLE transactions;
DROP TABLE wallets;
DROP TABLE audit_log;
DROP TABLE email_tokens;
DROP TABLE oauth_identities;
DROP INDEX IF EXISTS idx_sessions_user;
DROP TABLE sessions;
DROP INDEX IF EXISTS idx_server_seeds_active;
DROP TABLE users;
DROP EXTENSION IF EXISTS citext;
