-- +goose Up
-- Stateful single-player chicken run: one round spans several requests
-- (start, then hop/cashout), so the authoritative round state persists
-- between them. The fatal lane is stored server-side and withheld while
-- the round is active; it derives from the fairness triple, which is what
-- makes the finished round independently verifiable.
CREATE TABLE chicken_rounds (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bet_id         BIGINT NOT NULL UNIQUE REFERENCES bets(id),
    status         TEXT NOT NULL DEFAULT 'active'
                   CHECK (status IN ('active', 'cashed', 'squashed')),
    bet_credits    BIGINT NOT NULL CHECK (bet_credits > 0),
    difficulty     TEXT NOT NULL,
    lanes          INT NOT NULL CHECK (lanes BETWEEN 1 AND 64),
    crossed        INT NOT NULL DEFAULT 0 CHECK (crossed >= 0),
    fatal_lane     INT NOT NULL CHECK (fatal_lane BETWEEN 1 AND 65),
    payout_credits BIGINT NOT NULL DEFAULT 0,
    -- Client idempotency keys of each action, so network retries never hop
    -- twice.
    action_keys    JSONB NOT NULL DEFAULT '[]'::jsonb,
    server_seed_id BIGINT NOT NULL REFERENCES server_seeds(id),
    client_seed    TEXT NOT NULL,
    nonce          BIGINT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at   TIMESTAMPTZ
);
-- One active round per user at a time.
CREATE UNIQUE INDEX uq_chicken_rounds_one_active
    ON chicken_rounds (user_id) WHERE status = 'active';
CREATE INDEX idx_chicken_rounds_user ON chicken_rounds (user_id, id DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_chicken_rounds_user;
DROP INDEX IF EXISTS uq_chicken_rounds_one_active;
DROP TABLE IF EXISTS chicken_rounds;
