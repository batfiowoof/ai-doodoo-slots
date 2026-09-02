-- +goose Up
-- Round bets (crash) ride the shared chain, not a per-user server seed, so
-- the personal-game fairness columns become optional.
ALTER TABLE bets ALTER COLUMN server_seed_id DROP NOT NULL;
ALTER TABLE bets ALTER COLUMN client_seed DROP NOT NULL;
ALTER TABLE bets ALTER COLUMN nonce DROP NOT NULL;

-- Per-round action idempotency: (round_id, user_id, action) makes replayed
-- place_bet / cash_out structurally impossible to pay twice.
ALTER TABLE bets ADD COLUMN action TEXT NOT NULL DEFAULT 'play';
ALTER TABLE bets ADD COLUMN auto_cashout_hundredths BIGINT NOT NULL DEFAULT 0;
ALTER TABLE bets ADD COLUMN cashout_hundredths BIGINT NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX uq_bets_round_action ON bets (round_id, user_id, action);

-- +goose Down
DROP INDEX IF EXISTS uq_bets_round_action;
ALTER TABLE bets DROP COLUMN auto_cashout_hundredths;
ALTER TABLE bets DROP COLUMN cashout_hundredths;
ALTER TABLE bets DROP COLUMN action;
ALTER TABLE bets ALTER COLUMN nonce SET NOT NULL;
ALTER TABLE bets ALTER COLUMN client_seed SET NOT NULL;
ALTER TABLE bets ALTER COLUMN server_seed_id SET NOT NULL;
