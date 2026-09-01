-- name: CreateServerSeed :one
INSERT INTO server_seeds (user_id, seed_hash, seed_plain, is_active, client_seed)
VALUES ($1, $2, $3, true, $4)
RETURNING id, user_id, seed_hash, seed_plain, is_active, created_at, revealed_at, client_seed, nonce;

-- name: GetActiveServerSeed :one
SELECT id, user_id, seed_hash, seed_plain, is_active, created_at, revealed_at, client_seed, nonce
FROM server_seeds
WHERE user_id = $1 AND is_active = true;

-- name: GetServerSeedByID :one
SELECT id, user_id, seed_hash, seed_plain, is_active, created_at, revealed_at, client_seed, nonce
FROM server_seeds
WHERE id = $1;

-- name: RevealAndDeactivateServerSeed :exec
-- seed_plain is stored (the server needs it to derive outcomes) but is only
-- exposed to the player once revealed, on rotation.
UPDATE server_seeds
SET is_active = false, revealed_at = now()
WHERE id = $1;

-- name: IncrementSeedNonce :one
-- Called inside the play transaction so concurrent bets cannot share a
-- nonce; UNIQUE (server_seed_id, nonce) backstops this.
UPDATE server_seeds
SET nonce = nonce + 1
WHERE id = $1 AND is_active = true
RETURNING nonce;

-- name: UpdateClientSeed :exec
UPDATE server_seeds
SET client_seed = $2
WHERE id = $1 AND is_active = true;
