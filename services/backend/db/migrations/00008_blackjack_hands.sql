-- +goose Up
-- Stateful single-player blackjack: one hand spans several requests
-- (deal, then hit/stand/double), so the authoritative hand state persists
-- between them. The shuffled deck itself is NOT stored — it replays exactly
-- from the fairness triple (server seed, client seed, nonce) plus the action
-- log, which is what makes every hand independently verifiable.
CREATE TABLE blackjack_hands (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bet_id         BIGINT NOT NULL UNIQUE REFERENCES bets(id),
    status         TEXT NOT NULL DEFAULT 'active'
                   CHECK (status IN ('active', 'complete')),
    bet_credits    BIGINT NOT NULL CHECK (bet_credits > 0),
    payout_credits BIGINT NOT NULL DEFAULT 0,
    -- Ordered action log ("hit"/"stand"/"double") plus the client
    -- idempotency key of each action, so network retries never draw twice.
    actions        JSONB NOT NULL DEFAULT '[]'::jsonb,
    action_keys    JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- Materialized engine state minus the deck (a cache; the source of
    -- truth is the triple + actions).
    hand_state     JSONB NOT NULL,
    server_seed_id BIGINT NOT NULL REFERENCES server_seeds(id),
    client_seed    TEXT NOT NULL,
    nonce          BIGINT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at   TIMESTAMPTZ
);
-- One active hand per user at a time.
CREATE UNIQUE INDEX uq_blackjack_hands_one_active
    ON blackjack_hands (user_id) WHERE status = 'active';
CREATE INDEX idx_blackjack_hands_user ON blackjack_hands (user_id, id DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_blackjack_hands_user;
DROP INDEX IF EXISTS uq_blackjack_hands_one_active;
DROP TABLE IF EXISTS blackjack_hands;
