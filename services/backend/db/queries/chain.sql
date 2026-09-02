-- name: MaxChainIndex :one
SELECT COALESCE(MAX(index), 0)::bigint FROM chain_seeds;

-- name: NextChainGroup :one
SELECT (COALESCE(MAX(chain_group), 0)::bigint + 1)::bigint FROM chain_seeds;

-- name: CountUnrevealedChainSeeds :one
SELECT count(*)::bigint FROM chain_seeds WHERE revealed_at IS NULL;

-- name: InsertChainSeed :exec
INSERT INTO chain_seeds (chain_group, index, seed_hash, seed_plain)
VALUES ($1, $2, $3, $4)
ON CONFLICT (index) DO NOTHING;

-- name: GetNextUnrevealedChainSeed :one
-- Rounds consume the chain in reverse: the next link is the highest
-- unrevealed index.
SELECT id, chain_group, index, seed_hash, seed_plain
FROM chain_seeds
WHERE revealed_at IS NULL
ORDER BY index DESC
LIMIT 1;

-- name: GetRevealedChainSeedAfter :one
-- The previously revealed link within the same chain (consumption is
-- strictly descending, so the predecessor of index N is the smallest
-- revealed index > N in this group).
SELECT id, chain_group, index, seed_hash, seed_plain
FROM chain_seeds
WHERE revealed_at IS NOT NULL AND index > $1 AND chain_group = $2
ORDER BY index ASC
LIMIT 1;

-- name: RevealChainSeed :exec
UPDATE chain_seeds SET revealed_at = now() WHERE id = $1;
