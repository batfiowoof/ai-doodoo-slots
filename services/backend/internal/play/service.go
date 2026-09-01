// Package play wires the engine, wallet, and fairness layers into the
// single transaction that every bet runs inside. The client sends a bet and
// receives a result; it never rolls, never computes a payout, never adjusts
// a balance.
package play

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/ai-doodoo-slots/services/backend/internal/wallet"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrUnknownGame means the game ID is not in the registry.
	ErrUnknownGame = errors.New("unknown game")
	// ErrInvalidBet wraps the engine's bet validation failure.
	ErrInvalidBet = errors.New("invalid bet")
	// ErrIdempotencyKeyInvalid covers missing or oversized keys.
	ErrIdempotencyKeyInvalid = errors.New("idempotency key must be 1-64 characters")
	// Re-exported for handler mapping.
	ErrInsufficientFunds     = wallet.ErrInsufficientFunds
	ErrIdempotencyConflict   = wallet.ErrIdempotencyConflict
	ErrWalletNotFound        = wallet.ErrWalletNotFound
)

// Result is what the play endpoint returns.
type Result struct {
	BetID          int64
	GameID         string
	PayoutCredits  int64
	BalanceCredits int64
	Outcome        json.RawMessage
	ServerSeedHash string
	ClientSeed     string
	Nonce          int64
	Replay         bool
}

// Service executes bets.
type Service struct {
	pool     *pgxpool.Pool
	registry *game.Registry
}

func NewService(pool *pgxpool.Pool, registry *game.Registry) *Service {
	return &Service{pool: pool, registry: registry}
}

// Play runs one bet atomically:
//
//	lock wallet (FOR UPDATE) → idempotency check → lock seed pair, consume
//	nonce → derive stream → engine outcome → synthetic round + bet row →
//	debit and payout ledger entries → balance materialization → commit.
//
// A replayed idempotency key returns the original result and creates no
// second transaction. Lock order is wallet then seed everywhere, so
// concurrent plays serialize without deadlock.
func (s *Service) Play(ctx context.Context, userID int64, gameID string, betCredits int64, clientSeed, idempotencyKey string) (Result, error) {
	if len(idempotencyKey) == 0 || len(idempotencyKey) > 64 {
		return Result{}, ErrIdempotencyKeyInvalid
	}
	g, ok := s.registry.Get(gameID)
	if !ok {
		return Result{}, ErrUnknownGame
	}
	if err := g.ValidateBet(betCredits); err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrInvalidBet, err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)

	// 1. Serialize on the wallet row before anything else.
	lockRow, err := wallet.LockWallet(ctx, tx, userID)
	if err != nil {
		return Result{}, err
	}

	// 2. Idempotency: identical retry replays the original result; the same
	// key with a different bet is a conflict.
	existing, err := q.GetTransactionByIdempotencyKey(ctx, idempotencyKey)
	if err == nil {
		if existing.WalletID != userID || existing.Kind != wallet.KindBet || existing.AmountCredits != -betCredits {
			return Result{}, ErrIdempotencyConflict
		}
		if !existing.BetID.Valid {
			return Result{}, fmt.Errorf("bet transaction %d has no bet_id", existing.ID)
		}
		bet, err := q.GetBetByID(ctx, existing.BetID.Int64)
		if err != nil {
			return Result{}, fmt.Errorf("load replayed bet: %w", err)
		}
		seed, err := q.GetServerSeedByID(ctx, bet.ServerSeedID)
		if err != nil {
			return Result{}, fmt.Errorf("load replayed seed: %w", err)
		}
		return Result{
			BetID:          bet.ID,
			GameID:         bet.GameID,
			PayoutCredits:  bet.PayoutCredits,
			BalanceCredits: lockRow.BalanceCredits,
			Outcome:        bet.Outcome,
			ServerSeedHash: seed.SeedHash,
			ClientSeed:     bet.ClientSeed,
			Nonce:          bet.Nonce,
			Replay:         true,
		}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Result{}, fmt.Errorf("lookup idempotency key: %w", err)
	}

	// 3. Active seed pair; adopt a newly supplied client seed if changed.
	seed, err := q.GetActiveServerSeed(ctx, userID)
	if err != nil {
		return Result{}, fmt.Errorf("load active seed: %w", err)
	}
	effectiveClientSeed := seed.ClientSeed
	if clientSeed != "" && clientSeed != seed.ClientSeed {
		if len(clientSeed) > fair.MaxClientSeedLen {
			return Result{}, fmt.Errorf("client seed exceeds %d bytes", fair.MaxClientSeedLen)
		}
		if err := q.UpdateClientSeed(ctx, store.UpdateClientSeedParams{ID: seed.ID, ClientSeed: clientSeed}); err != nil {
			return Result{}, fmt.Errorf("update client seed: %w", err)
		}
		effectiveClientSeed = clientSeed
	}

	// 4. Consume the nonce inside the transaction; the seed row lock
	// serializes concurrent plays on the same pair.
	newNonce, err := q.IncrementSeedNonce(ctx, seed.ID)
	if err != nil {
		return Result{}, fmt.Errorf("increment nonce: %w", err)
	}
	nonce := newNonce - 1 // stored nonce counts consumed; this bet used the old value

	// 5. Derive the outcome — pure given the stream.
	plain, err := hex.DecodeString(seed.SeedPlain.String)
	if err != nil {
		return Result{}, fmt.Errorf("decode server seed: %w", err)
	}
	stream := fair.NewPersonalStream(plain, effectiveClientSeed, nonce)
	outcome, err := g.Play(stream, betCredits)
	if err != nil {
		return Result{}, fmt.Errorf("play: %w", err)
	}

	// 6. Synthetic one-player round, so bets.round_id is always non-null.
	roundID, err := q.CreateSettledRound(ctx, store.CreateSettledRoundParams{GameID: gameID, Result: outcome.Payload})
	if err != nil {
		return Result{}, fmt.Errorf("create round: %w", err)
	}

	// 7. Bet row (UNIQUE (server_seed_id, nonce) backstops replay).
	bet, err := q.InsertBet(ctx, store.InsertBetParams{
		UserID:        userID,
		GameID:        gameID,
		RoundID:       roundID,
		BetCredits:    betCredits,
		PayoutCredits: outcome.PayoutCredits,
		ServerSeedID:  seed.ID,
		ClientSeed:    effectiveClientSeed,
		Nonce:         nonce,
		Outcome:       outcome.Payload,
	})
	if err != nil {
		return Result{}, fmt.Errorf("insert bet: %w", err)
	}

	// 8. Ledger: debit the bet, credit any payout, materialize the balance.
	betID := bet.ID
	res, err := wallet.ApplyTx(ctx, tx, wallet.ApplyRequest{
		UserID:         userID,
		Kind:           wallet.KindBet,
		Amount:         -betCredits,
		BetID:          &betID,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return Result{}, err
	}
	balance := res.Balance
	if outcome.PayoutCredits > 0 {
		res, err = wallet.ApplyTx(ctx, tx, wallet.ApplyRequest{
			UserID:         userID,
			Kind:           wallet.KindPayout,
			Amount:         outcome.PayoutCredits,
			BetID:          &betID,
			IdempotencyKey: idempotencyKey + ":payout",
		})
		if err != nil {
			return Result{}, err
		}
		balance = res.Balance
	}

	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit: %w", err)
	}

	return Result{
		BetID:          bet.ID,
		GameID:         gameID,
		PayoutCredits:  outcome.PayoutCredits,
		BalanceCredits: balance,
		Outcome:        outcome.Payload,
		ServerSeedHash: seed.SeedHash,
		ClientSeed:     effectiveClientSeed,
		Nonce:          nonce,
	}, nil
}
