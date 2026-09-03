-- name: InsertBlackjackHand :one
INSERT INTO blackjack_hands (user_id, bet_id, status, bet_credits,
                             payout_credits, actions, action_keys, hand_state,
                             server_seed_id, client_seed, nonce)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING id;

-- name: GetBlackjackHandByID :one
SELECT id, user_id, bet_id, status, bet_credits, payout_credits, actions,
       action_keys, hand_state, server_seed_id, client_seed, nonce,
       created_at, updated_at, completed_at
FROM blackjack_hands
WHERE id = $1;

-- name: GetBlackjackHandByBetID :one
-- Deal idempotency replays land here: the idempotency key resolved to a
-- transaction, the transaction to its bet, the bet to its hand.
SELECT id, user_id, bet_id, status, bet_credits, payout_credits, actions,
       action_keys, hand_state, server_seed_id, client_seed, nonce,
       created_at, updated_at, completed_at
FROM blackjack_hands
WHERE bet_id = $1;

-- name: GetActiveBlackjackHandByUser :one
SELECT id, user_id, bet_id, status, bet_credits, payout_credits, actions,
       action_keys, hand_state, server_seed_id, client_seed, nonce,
       created_at, updated_at, completed_at
FROM blackjack_hands
WHERE user_id = $1 AND status = 'active';

-- name: SaveBlackjackHand :exec
-- Single writer per hand (the wallet row lock serializes the user's actions),
-- so a blind write of the full derived state is safe. completed_at is NULL
-- while the hand is active.
UPDATE blackjack_hands
SET status = $2,
    bet_credits = $3,
    payout_credits = $4,
    actions = $5,
    action_keys = $6,
    hand_state = $7,
    updated_at = now(),
    completed_at = $8
WHERE id = $1;

-- name: SetBlackjackBetSettlement :exec
-- Doubles raise the staked amount; completion fills payout and outcome.
UPDATE bets
SET bet_credits = $2,
    payout_credits = $3,
    outcome = $4
WHERE id = $1;
