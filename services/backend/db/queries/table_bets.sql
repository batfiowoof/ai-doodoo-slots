-- name: InsertTableBet :one
-- One bets row per player per poker hand: bet = matched contribution,
-- payout = chips won from the pots. Wallet money only moves on buy-in and
-- cash-out (stacks are house-held), so this row is the hand's record.
INSERT INTO bets (user_id, game_id, round_id, bet_credits, payout_credits, action)
VALUES ($1, $2, $3, $4, $5, 'play')
RETURNING id;
