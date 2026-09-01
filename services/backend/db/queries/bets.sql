-- name: CreateSettledRound :one
-- Single-player bets get a synthetic one-player round so bets.round_id is
-- always non-null. Round games write real state transitions later.
INSERT INTO rounds (game_id, state, result, opened_at, locked_at, settled_at)
VALUES ($1, 'settled', $2, now(), now(), now())
RETURNING id;

-- name: InsertBet :one
INSERT INTO bets (user_id, game_id, round_id, bet_credits, payout_credits,
                  server_seed_id, client_seed, nonce, outcome)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, created_at;

-- name: GetBetByID :one
SELECT id, user_id, game_id, round_id, bet_credits, payout_credits,
       server_seed_id, client_seed, nonce, outcome, created_at
FROM bets
WHERE id = $1;

-- name: ListBetsByUser :many
-- Ownership enforced in the query, never filtered in a handler.
SELECT id, game_id, round_id, bet_credits, payout_credits,
       client_seed, nonce, outcome, created_at
FROM bets
WHERE user_id = $1
  AND (sqlc.narg('cursor')::bigint IS NULL OR id < sqlc.narg('cursor'))
ORDER BY id DESC
LIMIT sqlc.arg('lim');

-- name: CountBetsForUser :one
SELECT COUNT(*)::bigint FROM bets WHERE user_id = $1;
