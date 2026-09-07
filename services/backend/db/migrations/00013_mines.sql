-- +goose Up
-- Stateful single-player mines: one round spans several requests (start,
-- then reveal/cashout), so the authoritative round state persists between
-- them. Mine positions are stored server-side and withheld while the round
-- is active; they derive from the fairness triple, which is what makes the
-- finished round independently verifiable.
CREATE TABLE mines_rounds (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bet_id         BIGINT NOT NULL UNIQUE REFERENCES bets(id),
    status         TEXT NOT NULL DEFAULT 'active'
                   CHECK (status IN ('active', 'cashed', 'busted')),
    bet_credits    BIGINT NOT NULL CHECK (bet_credits > 0),
    mine_count     INT NOT NULL CHECK (mine_count BETWEEN 1 AND 24),
    payout_credits BIGINT NOT NULL DEFAULT 0,
    -- Mine positions (tile indexes 0-24) and revealed tiles, in reveal order.
    mines          JSONB NOT NULL,
    revealed       JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- Client idempotency keys of each action, so network retries never
    -- reveal twice.
    action_keys    JSONB NOT NULL DEFAULT '[]'::jsonb,
    server_seed_id BIGINT NOT NULL REFERENCES server_seeds(id),
    client_seed    TEXT NOT NULL,
    nonce          BIGINT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at   TIMESTAMPTZ
);
-- One active round per user at a time.
CREATE UNIQUE INDEX uq_mines_rounds_one_active
    ON mines_rounds (user_id) WHERE status = 'active';
CREATE INDEX idx_mines_rounds_user ON mines_rounds (user_id, id DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_mines_rounds_user;
DROP INDEX IF EXISTS uq_mines_rounds_one_active;
DROP TABLE IF EXISTS mines_rounds;
