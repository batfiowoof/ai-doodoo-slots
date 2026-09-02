-- +goose Up
-- Identity for each generated chain: verification of a link's predecessor
-- is scoped to the same group, since a new chain's terminal seed is fresh
-- and does not hash into the previous chain.
ALTER TABLE chain_seeds ADD COLUMN chain_group BIGINT NOT NULL DEFAULT 0;

-- Backfill: every full run of 1000 links is one generated chain.
WITH runs AS (
    SELECT id, ((index - 1) / 1000)::bigint AS g
    FROM chain_seeds
)
UPDATE chain_seeds SET chain_group = runs.g
FROM runs WHERE chain_seeds.id = runs.id;

-- +goose Down
ALTER TABLE chain_seeds DROP COLUMN chain_group;
