// Package table drives multiplayer poker tables: one runner goroutine per
// room owns the engine state machine, the persister is the only money
// boundary (buy-in debits, cash-out credits, hand records), and the intake
// is the authorized socket path. Mirrors the round package's structure.
package table

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/ai-doodoo-slots/services/backend/internal/admin"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/ai-doodoo-slots/services/backend/internal/wallet"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SettledPlayer is one player's record for a finished hand.
type SettledPlayer struct {
	UserID      int64
	Contributed int64
	WinAmount   int64
}

// HandRecord is the full-reveal result document persisted per hand and
// broadcast at settlement — everything a verifier needs with the revealed
// chain seed.
type HandRecord struct {
	HandNo   int64               `json:"handNo"`
	Board    []string            `json:"board"`
	Seats    []SeatRecord        `json:"seats"`
	Results []PlayerResultRecord `json:"results"`
	Actions []string             `json:"actions"`
}

// SeatRecord is one seat's full reveal in a settled hand.
type SeatRecord struct {
	SeatNo      int    `json:"seatNo"`
	UserID      int64  `json:"userId"`
	DisplayName string `json:"displayName"`
	Cards       string `json:"cards"`
	Folded      bool   `json:"folded"`
	Showed      bool   `json:"showed"`
	Contributed int64  `json:"contributed"`
	WinAmount   int64  `json:"winAmount"`
	StackAfter  int64  `json:"stackAfter"`
}

// PlayerResultRecord mirrors the engine result for persistence.
type PlayerResultRecord struct {
	UserID      int64  `json:"userId"`
	DisplayName string `json:"displayName"`
	Cards       string `json:"cards"`
	HandName    string `json:"handName,omitempty"`
	WinAmount   int64  `json:"winAmount"`
	Contributed int64  `json:"contributed"`
	Net         int64  `json:"net"`
}

// Persister is the money and record boundary for a table.
type Persister interface {
	// OpenHand opens the rounds row for a hand, bound to its chain link.
	OpenHand(ctx context.Context, roomID, chainSeedID int64, gameID, salt string) (int64, error)
	// BuyIn debits chips into the table (wallet FOR UPDATE + status gate +
	// idempotent ledger entry, one transaction). replayed=true means the
	// idempotency key already paid: no chips are owed again.
	BuyIn(ctx context.Context, userID int64, gameID string, amount int64, idemKey string) (balance int64, replayed bool, err error)
	// CashOut credits chips leaving the table.
	CashOut(ctx context.Context, userID int64, amount int64, idemKey string) (balance int64, err error)
	// SettleHand records the hand atomically: round result (full reveal) and
	// one bets row per contributing player.
	SettleHand(ctx context.Context, roundID int64, gameID string, record HandRecord, settled []SettledPlayer) error
}

type pgPersister struct {
	pool *pgxpool.Pool
	w    *wallet.Wallet
}

// NewPersister returns the Postgres-backed Persister.
func NewPersister(pool *pgxpool.Pool) Persister { return &pgPersister{pool: pool, w: wallet.New(pool)} }

func (p *pgPersister) OpenHand(ctx context.Context, roomID, chainSeedID int64, gameID, salt string) (int64, error) {
	return store.New(p.pool).CreateRound(ctx, store.CreateRoundParams{
		RoomID:      pgtype.Int8{Int64: roomID, Valid: true},
		GameID:      gameID,
		ChainSeedID: pgtype.Int8{Int64: chainSeedID, Valid: true},
		Salt:        salt,
	})
}

func (p *pgPersister) BuyIn(ctx context.Context, userID int64, gameID string, amount int64, idemKey string) (int64, bool, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)

	if _, err := wallet.LockWallet(ctx, tx, userID); err != nil {
		return 0, false, err
	}
	status, err := q.GetUserStatus(ctx, userID)
	if err != nil {
		return 0, false, err
	}
	if admin.StatusForbidsBetting(status) {
		return 0, false, ErrForbiddenStatus
	}
	res, err := wallet.ApplyTx(ctx, tx, wallet.ApplyRequest{
		UserID:         userID,
		Kind:           wallet.KindBet,
		Amount:         -amount,
		IdempotencyKey: idemKey,
	})
	if err != nil {
		return 0, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, false, err
	}
	return res.Balance, res.Replayed, nil
}

func (p *pgPersister) CashOut(ctx context.Context, userID int64, amount int64, idemKey string) (int64, error) {
	if amount <= 0 {
		return 0, ErrInvalidAmount
	}
	res, err := p.w.Apply(ctx, wallet.ApplyRequest{
		UserID:         userID,
		Kind:           wallet.KindPayout,
		Amount:         amount,
		IdempotencyKey: idemKey,
	})
	if err != nil {
		return 0, err
	}
	return res.Balance, nil
}

func (p *pgPersister) SettleHand(ctx context.Context, roundID int64, gameID string, record HandRecord, settled []SettledPlayer) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)

	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err := q.SetRoundResult(ctx, store.SetRoundResultParams{ID: roundID, Result: payload}); err != nil {
		return err
	}
	for _, s := range settled {
		if s.Contributed <= 0 {
			continue // no bets row for players dealt nothing
		}
		if _, err := q.InsertTableBet(ctx, store.InsertTableBetParams{
			UserID:        s.UserID,
			GameID:        gameID,
			RoundID:       roundID,
			BetCredits:    s.Contributed,
			PayoutCredits: s.WinAmount,
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// NoopPersister is the test double.
type NoopPersister struct{}

func (NoopPersister) OpenHand(context.Context, int64, int64, string, string) (int64, error) {
	return 1, nil
}
func (NoopPersister) BuyIn(context.Context, int64, string, int64, string) (int64, bool, error) {
	return 0, false, nil
}
func (NoopPersister) CashOut(context.Context, int64, int64, string) (int64, error) {
	return 0, nil
}
func (NoopPersister) SettleHand(context.Context, int64, string, HandRecord, []SettledPlayer) error {
	return nil
}

// --- errors with wire codes ---------------------------------------------

var (
	ErrForbiddenStatus = errors.New("account status does not permit betting")
	ErrInvalidAmount   = errors.New("invalid amount")
)

type codedError struct {
	code string
	err  error
}

func (c codedError) Error() string { return c.err.Error() }
func (c codedError) Code() string  { return c.code }
func (c codedError) Unwrap() error { return c.err }

func coded(code string, err error) error { return codedError{code: code, err: err} }

// buyInKey / cashOutKey scope ledger idempotency keys to a table and user.
func buyInKey(room string, userID int64, clientKey string) string {
	return fmt.Sprintf("table:%s:%d:buyin:%s", room, userID, clientKey)
}

func cashOutKey(room string, userID int64, roundID int64) string {
	return fmt.Sprintf("table:%s:%d:cashout:%s", room, userID, strconv.FormatInt(roundID, 10))
}
