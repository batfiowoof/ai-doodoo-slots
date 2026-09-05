-- name: InsertRoundBet :one
INSERT INTO bets (user_id, game_id, round_id, bet_credits, server_seed_id, client_seed, nonce, action, auto_cashout_hundredths, outcome)
VALUES ($1, $2, $3, $4, NULL, NULL, NULL, $5, $6, $7)
RETURNING id;

-- name: MarkBetCashout :exec
UPDATE bets SET cashout_hundredths = $2 WHERE id = $1;

-- name: SetBetPayout :exec
UPDATE bets SET payout_credits = $2 WHERE id = $1;

-- name: MarkBetCashoutByRound :exec
UPDATE bets SET cashout_hundredths = $3
WHERE round_id = $1 AND user_id = $2 AND action = 'bet';

-- name: MarkBetsCleared :exec
UPDATE bets SET action = action || ':cleared' WHERE id = ANY($1::bigint[]);
