-- name: CreateWallet :exec
INSERT INTO wallets (user_id, balance_credits)
VALUES ($1, $2)
ON CONFLICT (user_id) DO NOTHING;

-- name: GetWalletBalance :one
SELECT balance_credits FROM wallets WHERE user_id = $1;

-- name: LockWalletForUpdate :one
-- The play/settlement path must always take this row lock before reading or
-- writing a balance. Two simultaneous bets on one wallet serialize here.
SELECT user_id, balance_credits FROM wallets WHERE user_id = $1 FOR UPDATE;

-- name: LockWalletsSorted :many
-- Multi-wallet settlement locks rows in ascending user_id order to make
-- deadlocks impossible. Callers pass an unordered set; SQL orders.
SELECT user_id, balance_credits FROM wallets
WHERE user_id = ANY($1::bigint[])
ORDER BY user_id
FOR UPDATE;

-- name: GetTransactionByIdempotencyKey :one
SELECT id, wallet_id, kind, amount_credits, bet_id, idempotency_key, created_at
FROM transactions
WHERE idempotency_key = $1;

-- name: InsertTransaction :one
INSERT INTO transactions (wallet_id, kind, amount_credits, bet_id, idempotency_key)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, created_at;

-- name: UpdateWalletBalance :one
-- The cached materialization is updated in the same transaction as the
-- ledger insert. Nothing may write it without a matching transaction row.
UPDATE wallets
SET balance_credits = balance_credits + $2
WHERE user_id = $1
RETURNING balance_credits;

-- name: SumTransactions :one
SELECT COALESCE(SUM(amount_credits), 0)::bigint AS total
FROM transactions
WHERE wallet_id = $1;
