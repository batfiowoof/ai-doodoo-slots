-- name: InsertMinesRound :one
INSERT INTO mines_rounds (user_id, bet_id, status, bet_credits, mine_count,
                          payout_credits, mines, revealed, action_keys,
                          server_seed_id, client_seed, nonce)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING id;

-- name: GetMinesRoundByID :one
SELECT id, user_id, bet_id, status, bet_credits, mine_count, payout_credits,
       mines, revealed, action_keys, server_seed_id, client_seed, nonce,
       created_at, updated_at, completed_at
FROM mines_rounds
WHERE id = $1;

-- name: GetMinesRoundByBetID :one
-- Start idempotency replays land here: the idempotency key resolved to a
-- transaction, the transaction to its bet, the bet to its round.
SELECT id, user_id, bet_id, status, bet_credits, mine_count, payout_credits,
       mines, revealed, action_keys, server_seed_id, client_seed, nonce,
       created_at, updated_at, completed_at
FROM mines_rounds
WHERE bet_id = $1;

-- name: GetActiveMinesRoundByUser :one
SELECT id, user_id, bet_id, status, bet_credits, mine_count, payout_credits,
       mines, revealed, action_keys, server_seed_id, client_seed, nonce,
       created_at, updated_at, completed_at
FROM mines_rounds
WHERE user_id = $1 AND status = 'active';

-- name: SaveMinesRound :exec
-- Single writer per round (the wallet row lock serializes the user's
-- actions), so a blind write of the full derived state is safe.
-- completed_at is NULL while the round is active.
UPDATE mines_rounds
SET status = $2,
    payout_credits = $3,
    revealed = $4,
    action_keys = $5,
    updated_at = now(),
    completed_at = $6
WHERE id = $1;

-- name: SetMinesBetSettlement :exec
-- Completion (cashout or bust) fills payout and the fully-revealed outcome.
UPDATE bets
SET payout_credits = $2,
    outcome = $3
WHERE id = $1;
