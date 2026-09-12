-- name: InsertChickenRound :one
INSERT INTO chicken_rounds (user_id, bet_id, status, bet_credits, difficulty,
                            lanes, crossed, fatal_lane, payout_credits, action_keys,
                            server_seed_id, client_seed, nonce)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING id;

-- name: GetChickenRoundByID :one
SELECT id, user_id, bet_id, status, bet_credits, difficulty, lanes, crossed,
       fatal_lane, payout_credits, action_keys, server_seed_id, client_seed, nonce,
       created_at, updated_at, completed_at
FROM chicken_rounds
WHERE id = $1;

-- name: GetChickenRoundByBetID :one
-- Start idempotency replays land here: the idempotency key resolved to a
-- transaction, the transaction to its bet, the bet to its round.
SELECT id, user_id, bet_id, status, bet_credits, difficulty, lanes, crossed,
       fatal_lane, payout_credits, action_keys, server_seed_id, client_seed, nonce,
       created_at, updated_at, completed_at
FROM chicken_rounds
WHERE bet_id = $1;

-- name: GetActiveChickenRoundByUser :one
SELECT id, user_id, bet_id, status, bet_credits, difficulty, lanes, crossed,
       fatal_lane, payout_credits, action_keys, server_seed_id, client_seed, nonce,
       created_at, updated_at, completed_at
FROM chicken_rounds
WHERE user_id = $1 AND status = 'active';

-- name: SaveChickenRound :exec
-- Single writer per round (the wallet row lock serializes the user's
-- actions), so a blind write of the full derived state is safe.
-- completed_at is NULL while the round is active.
UPDATE chicken_rounds
SET status = $2,
    payout_credits = $3,
    crossed = $4,
    action_keys = $5,
    updated_at = now(),
    completed_at = $6
WHERE id = $1;

-- name: SetChickenBetSettlement :exec
-- Completion (cashout or squash) fills payout and the fully-revealed outcome.
UPDATE bets
SET payout_credits = $2,
    outcome = $3
WHERE id = $1;
