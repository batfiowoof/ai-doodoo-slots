// Package wallet implements the append-only credit ledger and the cached
// balance materialization. Balance is derived from transactions; it is only
// ever updated in the same database transaction as a ledger insert, behind a
// row lock. Nothing may write wallets.balance_credits without a matching
// transaction row.
package wallet

import (
	"context"
	"errors"
	"fmt"

	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// int8Ptr converts an optional int64 to pgtype.Int8.
func int8Ptr(v *int64) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *v, Valid: true}
}

// Transaction kinds. Signed amounts: debits negative, credits positive.
const (
	KindSignupBonus = "signup_bonus"
	KindDailyTopup  = "daily_topup"
	KindBet         = "bet"
	KindPayout      = "payout"
	KindAdminAdjust = "admin_adjust"
)

var (
	// ErrInsufficientFunds means the debit would take the balance negative.
	ErrInsufficientFunds = errors.New("insufficient funds")
	// ErrIdempotencyConflict means the idempotency key exists with a
	// differing payload. Identical retries return the original result.
	ErrIdempotencyConflict = errors.New("idempotency key reuse with differing payload")
	// ErrWalletNotFound means no wallet row exists for the user.
	ErrWalletNotFound = errors.New("wallet not found")
)

// ApplyRequest is a single ledger mutation.
type ApplyRequest struct {
	UserID         int64
	Kind           string
	Amount         int64 // signed: negative debits, positive credits
	BetID          *int64
	IdempotencyKey string
}

// ApplyResult reports the transaction and post-application balance.
type ApplyResult struct {
	TransactionID int64
	Balance       int64
	// Replayed is true when an identical retry returned the original result
	// without a second transaction.
	Replayed bool
}

// Wallet owns ledger mutations.
type Wallet struct {
	pool *pgxpool.Pool
	q    *store.Queries
}

func New(pool *pgxpool.Pool) *Wallet {
	return &Wallet{pool: pool, q: store.New(pool)}
}

// Apply runs one ledger mutation atomically:
//
//  1. lock the wallet row (SELECT ... FOR UPDATE) — concurrent bets serialize
//     here, in ascending wallet order for multi-wallet settlement
//  2. if the idempotency key exists: return the original result for an
//     identical payload, ErrIdempotencyConflict otherwise
//  3. insert the transaction
//  4. update the cached balance in the same transaction
func (w *Wallet) Apply(ctx context.Context, req ApplyRequest) (ApplyResult, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	res, err := ApplyTx(ctx, tx, req)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ApplyResult{}, fmt.Errorf("commit: %w", err)
	}
	return res, nil
}

// LockWallet takes the FOR UPDATE row lock on a wallet inside an open
// transaction. The play path locks wallet first, then the seed row, always
// in that order; multi-wallet settlement uses LockWalletsSorted.
func LockWallet(ctx context.Context, tx pgx.Tx, userID int64) (store.Wallet, error) {
	row, err := store.New(tx).LockWalletForUpdate(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Wallet{}, ErrWalletNotFound
	}
	return row, err
}

// ApplyTx is the ledger mutation body run on an open transaction, shared
// with multi-step flows (the play path, guest signup) that must include
// ledger writes in a larger atomic unit.
func ApplyTx(ctx context.Context, tx pgx.Tx, req ApplyRequest) (ApplyResult, error) {
	q := store.New(tx)

	// 1. Serialize on the wallet row before any read.
	lockRow, err := q.LockWalletForUpdate(ctx, req.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ApplyResult{}, ErrWalletNotFound
		}
		return ApplyResult{}, fmt.Errorf("lock wallet: %w", err)
	}

	// 2. Idempotency: replay identical, conflict on difference.
	existing, err := q.GetTransactionByIdempotencyKey(ctx, req.IdempotencyKey)
	if err == nil {
		if existing.WalletID != req.UserID || existing.Kind != req.Kind || existing.AmountCredits != req.Amount {
			return ApplyResult{}, ErrIdempotencyConflict
		}
		return ApplyResult{TransactionID: existing.ID, Balance: lockRow.BalanceCredits, Replayed: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ApplyResult{}, fmt.Errorf("lookup idempotency key: %w", err)
	}

	// 3. Debits must not take the balance negative.
	if req.Amount < 0 && lockRow.BalanceCredits+req.Amount < 0 {
		return ApplyResult{}, ErrInsufficientFunds
	}

	// 4. Ledger insert and balance materialization, same transaction.
	inserted, err := q.InsertTransaction(ctx, store.InsertTransactionParams{
		WalletID:       req.UserID,
		Kind:           req.Kind,
		AmountCredits:  req.Amount,
		BetID:          int8Ptr(req.BetID),
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return ApplyResult{}, fmt.Errorf("insert transaction: %w", err)
	}
	balance, err := q.UpdateWalletBalance(ctx, store.UpdateWalletBalanceParams{
		UserID:         req.UserID,
		BalanceCredits: req.Amount,
	})
	if err != nil {
		return ApplyResult{}, fmt.Errorf("update balance: %w", err)
	}

	return ApplyResult{TransactionID: inserted.ID, Balance: balance}, nil
}

// EnsureSignup idempotently provisions a wallet with the starting credits.
// Safe to retry: the signup bonus carries the idempotency key signup:{id}.
func (w *Wallet) EnsureSignup(ctx context.Context, userID int64, startingCredits int64) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	q := store.New(tx)
	if err := q.CreateWallet(ctx, store.CreateWalletParams{UserID: userID, BalanceCredits: 0}); err != nil {
		return fmt.Errorf("create wallet: %w", err)
	}
	if _, err := ApplyTx(ctx, tx, ApplyRequest{
		UserID:         userID,
		Kind:           KindSignupBonus,
		Amount:         startingCredits,
		IdempotencyKey: fmt.Sprintf("signup:%d", userID),
	}); err != nil {
		return fmt.Errorf("apply signup bonus: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Balance returns the cached balance. The ledger sum is authoritative; the
// reconciliation job and tests assert the two agree.
func (w *Wallet) Balance(ctx context.Context, userID int64) (int64, error) {
	balance, err := w.q.GetWalletBalance(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrWalletNotFound
		}
		return 0, err
	}
	return balance, nil
}

// LedgerSum returns SUM(transactions) for a wallet. Tests assert it equals
// the cached balance.
func (w *Wallet) LedgerSum(ctx context.Context, userID int64) (int64, error) {
	return w.q.SumTransactions(ctx, userID)
}

// LockWalletsSorted takes FOR UPDATE row locks on the given wallets in
// ascending user_id order, making multi-wallet settlement deadlock-free.
// It must be called on an open transaction; the locks live until commit.
func LockWalletsSorted(ctx context.Context, tx pgx.Tx, userIDs []int64) error {
	_, err := store.New(tx).LockWalletsSorted(ctx, userIDs)
	return err
}
