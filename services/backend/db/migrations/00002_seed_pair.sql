-- +goose Up
-- The client seed lives on the active server seed row: rotation replaces
-- both halves of the pair. nonce counts bets consumed from this pair, so
-- /fair/current advertises it as the next nonce (0-based).
ALTER TABLE server_seeds ADD COLUMN client_seed TEXT NOT NULL DEFAULT '';
ALTER TABLE server_seeds ADD COLUMN nonce BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE server_seeds DROP COLUMN client_seed;
ALTER TABLE server_seeds DROP COLUMN nonce;
