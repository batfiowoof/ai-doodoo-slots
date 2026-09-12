// Package chicken wires the stateful chicken-run engine into the wallet
// and fairness layers. A round spans several transactions (start, then
// hop/cashout), so like mines the money moves are split: the stake debits
// at start and the payout credits once at cashout — each in one atomic
// transaction. The fatal lane derives from the fairness triple, which is
// what verification replays.
package chicken

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/admin"
	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	chickenengine "github.com/ai-doodoo-slots/services/backend/internal/game/chicken"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/ai-doodoo-slots/services/backend/internal/wallet"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// staleAfter is how long an untouched active round blocks a new start
// before the next start auto-cashes it (a closed tab should not hold the
// stake hostage — the player only ever crossed safe lanes by definition).
const staleAfter = 5 * time.Minute

var (
	ErrInvalidBet            = errors.New("invalid bet")
	ErrDifficultyInvalid     = errors.New("invalid difficulty")
	ErrIdempotencyKeyInvalid = errors.New("idempotency key must be 1-64 characters")
	ErrStatusForbidsBetting  = errors.New("account status does not permit betting")
	ErrRoundActive           = errors.New("a chicken run is already in progress")
	ErrRoundNotFound         = errors.New("chicken run not found")
	ErrRoundComplete         = errors.New("chicken run is already complete")
	ErrCannotCash            = errors.New("cross at least one lane before cashing out")

	// Re-exported for handler mapping.
	ErrInsufficientFunds   = wallet.ErrInsufficientFunds
	ErrIdempotencyConflict = wallet.ErrIdempotencyConflict
	ErrWalletNotFound      = wallet.ErrWalletNotFound
)

// View is the client-facing round state. While the round is active the
// fatal lane is withheld; the same shape (fully revealed) is the bet's
// outcome payload for history and verification.
type View struct {
	RoundID        int64   `json:"roundId"`
	BetID          int64   `json:"betId"`
	Status         string  `json:"status"`
	BetCredits     int64   `json:"betCredits"`
	Difficulty     string  `json:"difficulty"`
	Lanes          int     `json:"lanes"`
	PayoutCredits  int64   `json:"payoutCredits"`
	Crossed        int     `json:"crossed"`
	Multiplier     float64 `json:"multiplier"`
	NextMultiplier float64 `json:"nextMultiplier,omitempty"`
	Cashable       bool    `json:"cashable"`
	FatalLane      int     `json:"fatalLane,omitempty"` // only when complete
}

// StartResult is what the start endpoint returns.
type StartResult struct {
	RoundID        int64
	View           View
	BalanceCredits int64
	ServerSeedHash string
	ClientSeed     string
	Nonce          int64
	Replay         bool
}

// Service executes chicken runs.
type Service struct {
	pool *pgxpool.Pool
	clk  clock.Clock
}

func NewService(pool *pgxpool.Pool, clk clock.Clock) *Service {
	return &Service{pool: pool, clk: clk}
}

// ActiveRound returns the caller's active round view (fatal lane
// withheld), or ok=false.
func (s *Service) ActiveRound(ctx context.Context, userID int64) (View, bool, error) {
	row, err := store.New(s.pool).GetActiveChickenRoundByUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return View{}, false, nil
	}
	if err != nil {
		return View{}, false, fmt.Errorf("load active round: %w", err)
	}
	v, err := s.view(row)
	if err != nil {
		return View{}, false, err
	}
	return v, true, nil
}

// Start runs the opening transaction: lock wallet → status gate →
// idempotency → (stale active round auto-cashes) → consume nonce → draw
// the fatal lane from the personal stream → persist round + bet + debit,
// one tx.
func (s *Service) Start(ctx context.Context, userID int64, betCredits int64, difficulty, clientSeed, idempotencyKey string) (StartResult, error) {
	if len(idempotencyKey) == 0 || len(idempotencyKey) > 64 {
		return StartResult{}, ErrIdempotencyKeyInvalid
	}
	d, err := chickenengine.ByName(difficulty)
	if err != nil {
		return StartResult{}, fmt.Errorf("%w: %v", ErrDifficultyInvalid, err)
	}
	if err := chickenengine.ValidateBet(d, betCredits); err != nil {
		return StartResult{}, fmt.Errorf("%w: %v", ErrInvalidBet, err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return StartResult{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)

	lockRow, err := wallet.LockWallet(ctx, tx, userID)
	if err != nil {
		return StartResult{}, err
	}
	status, err := q.GetUserStatus(ctx, userID)
	if err != nil {
		return StartResult{}, fmt.Errorf("load status: %w", err)
	}
	if admin.StatusForbidsBetting(status) {
		return StartResult{}, ErrStatusForbidsBetting
	}

	// Idempotency: identical retry returns the original round's current
	// state; a key reused with a different bet is a conflict.
	existing, err := q.GetTransactionByIdempotencyKey(ctx, idempotencyKey)
	if err == nil {
		if existing.WalletID != userID || existing.Kind != wallet.KindBet || existing.AmountCredits != -betCredits {
			return StartResult{}, ErrIdempotencyConflict
		}
		if !existing.BetID.Valid {
			return StartResult{}, fmt.Errorf("bet transaction %d has no bet_id", existing.ID)
		}
		row, err := q.GetChickenRoundByBetID(ctx, existing.BetID.Int64)
		if err != nil {
			return StartResult{}, fmt.Errorf("load replayed round: %w", err)
		}
		seed, err := q.GetServerSeedByID(ctx, row.ServerSeedID)
		if err != nil {
			return StartResult{}, fmt.Errorf("load replayed seed: %w", err)
		}
		v, err := s.view(row)
		if err != nil {
			return StartResult{}, err
		}
		return StartResult{
			RoundID: row.ID, View: v, BalanceCredits: lockRow.BalanceCredits,
			ServerSeedHash: seed.SeedHash, ClientSeed: row.ClientSeed, Nonce: row.Nonce, Replay: true,
		}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return StartResult{}, fmt.Errorf("lookup idempotency key: %w", err)
	}

	// One active round per user; an abandoned one auto-cashes so a closed
	// tab cannot hold the stake (the unique partial index backstops races).
	if active, err := q.GetActiveChickenRoundByUser(ctx, userID); err == nil {
		if age := s.clk.Now().Sub(active.UpdatedAt); age <= staleAfter {
			return StartResult{}, ErrRoundActive
		}
		if err := s.autoCash(ctx, tx, q, active); err != nil {
			return StartResult{}, err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return StartResult{}, fmt.Errorf("load active round: %w", err)
	}

	// Seed pair: adopt a changed client seed, consume the nonce.
	seed, err := q.GetActiveServerSeed(ctx, userID)
	if err != nil {
		return StartResult{}, fmt.Errorf("load active seed: %w", err)
	}
	effectiveClientSeed := seed.ClientSeed
	if clientSeed != "" && clientSeed != seed.ClientSeed {
		if len(clientSeed) > fair.MaxClientSeedLen {
			return StartResult{}, fmt.Errorf("client seed exceeds %d bytes", fair.MaxClientSeedLen)
		}
		if err := q.UpdateClientSeed(ctx, store.UpdateClientSeedParams{ID: seed.ID, ClientSeed: clientSeed}); err != nil {
			return StartResult{}, fmt.Errorf("update client seed: %w", err)
		}
		effectiveClientSeed = clientSeed
	}
	newNonce, err := q.IncrementSeedNonce(ctx, seed.ID)
	if err != nil {
		return StartResult{}, fmt.Errorf("increment nonce: %w", err)
	}
	nonce := newNonce - 1

	// Draw the fatal lane — pure given the stream.
	plain, err := hex.DecodeString(seed.SeedPlain.String)
	if err != nil {
		return StartResult{}, fmt.Errorf("decode server seed: %w", err)
	}
	fatalLane := chickenengine.DrawFatalLane(fair.NewPersonalStream(plain, effectiveClientSeed, nonce), d)

	// Synthetic round + bet row; the outcome payload withholds the lane.
	v := View{
		Status: chickenengine.StatusActive, BetCredits: betCredits,
		Difficulty: d.Name, Lanes: d.Lanes,
		Crossed: 0, Multiplier: 1,
		NextMultiplier: chickenengine.Multiplier(d, 1),
	}
	roundID, err := q.CreateSettledRound(ctx, store.CreateSettledRoundParams{
		GameID: chickenengine.GameID,
		Result: mustJSON(v),
	})
	if err != nil {
		return StartResult{}, fmt.Errorf("create round: %w", err)
	}
	bet, err := q.InsertBet(ctx, store.InsertBetParams{
		UserID: userID, GameID: chickenengine.GameID, RoundID: roundID,
		BetCredits: betCredits, PayoutCredits: 0,
		ServerSeedID: pgtype.Int8{Int64: seed.ID, Valid: true},
		ClientSeed:   pgtype.Text{String: effectiveClientSeed, Valid: true},
		Nonce:        pgtype.Int8{Int64: nonce, Valid: true},
		Outcome:      mustJSON(v),
	})
	if err != nil {
		return StartResult{}, fmt.Errorf("insert bet: %w", err)
	}

	betID := bet.ID
	res, err := wallet.ApplyTx(ctx, tx, wallet.ApplyRequest{
		UserID: userID, Kind: wallet.KindBet, Amount: -betCredits,
		BetID: &betID, IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return StartResult{}, err
	}

	roundRowID, err := q.InsertChickenRound(ctx, store.InsertChickenRoundParams{
		UserID: userID, BetID: betID, Status: chickenengine.StatusActive,
		BetCredits: betCredits, Difficulty: d.Name, Lanes: int32(d.Lanes),
		Crossed: 0, FatalLane: int32(fatalLane), PayoutCredits: 0,
		ActionKeys:   mustJSON([]string{}),
		ServerSeedID: seed.ID, ClientSeed: effectiveClientSeed, Nonce: nonce,
	})
	if err != nil {
		return StartResult{}, fmt.Errorf("insert round: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return StartResult{}, fmt.Errorf("commit: %w", err)
	}
	v.RoundID, v.BetID = roundRowID, betID
	return StartResult{
		RoundID: roundRowID, View: v, BalanceCredits: res.Balance,
		ServerSeedHash: seed.SeedHash, ClientSeed: effectiveClientSeed, Nonce: nonce,
	}, nil
}

// Hop advances the chicken one lane on the active round: lock wallet →
// status gate → load round (owner-checked) → fatal-lane check → squash
// settle, final-lane settle, or hop persist, one tx. A retried action key
// returns the current state unchanged.
func (s *Service) Hop(ctx context.Context, userID, roundID int64, idempotencyKey string) (StartResult, error) {
	if len(idempotencyKey) == 0 || len(idempotencyKey) > 64 {
		return StartResult{}, ErrIdempotencyKeyInvalid
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return StartResult{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)

	lockRow, err := wallet.LockWallet(ctx, tx, userID)
	if err != nil {
		return StartResult{}, err
	}
	status, err := q.GetUserStatus(ctx, userID)
	if err != nil {
		return StartResult{}, fmt.Errorf("load status: %w", err)
	}
	if admin.StatusForbidsBetting(status) {
		return StartResult{}, ErrStatusForbidsBetting
	}

	row, err := q.GetChickenRoundByID(ctx, roundID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.UserID != userID) {
		return StartResult{}, ErrRoundNotFound
	}
	if err != nil {
		return StartResult{}, fmt.Errorf("load round: %w", err)
	}

	// Idempotent action retry: the key is already in the log, so the effect
	// already happened — even when that action completed the round and the
	// response was lost. Checked before the status gate.
	keys, err := parseStrings(row.ActionKeys)
	if err != nil {
		return StartResult{}, err
	}
	for _, k := range keys {
		if k == idempotencyKey {
			return s.replayView(ctx, q, row, lockRow)
		}
	}
	if row.Status != chickenengine.StatusActive {
		return StartResult{}, ErrRoundComplete
	}

	d, err := chickenengine.ByName(row.Difficulty)
	if err != nil {
		return StartResult{}, fmt.Errorf("stored difficulty %q: %w", row.Difficulty, err)
	}
	crossed := int(row.Crossed)
	if crossed >= int(row.Lanes) {
		return StartResult{}, ErrRoundComplete
	}

	next := crossed + 1
	keys = append(keys, idempotencyKey)
	if int(row.FatalLane) == next {
		// Squash: settle at zero and reveal the fatal lane.
		v, err := s.settle(ctx, tx, q, row, chickenengine.StatusSquashed, crossed, 0, keys)
		if err != nil {
			return StartResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return StartResult{}, fmt.Errorf("commit: %w", err)
		}
		return StartResult{RoundID: row.ID, View: v, BalanceCredits: lockRow.BalanceCredits}, nil
	}

	crossed = next
	if crossed == int(row.Lanes) {
		// Cleared the whole road: auto-settle at the top multiplier.
		payout := chickenengine.Payout(row.BetCredits, d, crossed)
		v, err := s.settle(ctx, tx, q, row, chickenengine.StatusCashed, crossed, payout, keys)
		if err != nil {
			return StartResult{}, err
		}
		betID := row.BetID
		res, err := wallet.ApplyTx(ctx, tx, wallet.ApplyRequest{
			UserID: userID, Kind: wallet.KindPayout, Amount: payout,
			BetID: &betID, IdempotencyKey: idempotencyKey + ":payout",
		})
		if err != nil {
			return StartResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return StartResult{}, fmt.Errorf("commit: %w", err)
		}
		return StartResult{RoundID: row.ID, View: v, BalanceCredits: res.Balance}, nil
	}

	v := View{
		RoundID: row.ID, BetID: row.BetID, Status: chickenengine.StatusActive,
		BetCredits: row.BetCredits, Difficulty: d.Name, Lanes: int(row.Lanes),
		PayoutCredits:  0,
		Crossed:        crossed,
		Multiplier:     chickenengine.Multiplier(d, crossed),
		NextMultiplier: chickenengine.Multiplier(d, crossed+1),
		Cashable:       crossed >= 1,
	}
	if err := q.SaveChickenRound(ctx, store.SaveChickenRoundParams{
		ID: row.ID, Status: chickenengine.StatusActive, PayoutCredits: 0,
		Crossed: int32(crossed), ActionKeys: mustJSON(keys), CompletedAt: nil,
	}); err != nil {
		return StartResult{}, fmt.Errorf("save round: %w", err)
	}
	if err := q.SetChickenBetSettlement(ctx, store.SetChickenBetSettlementParams{
		ID: row.BetID, PayoutCredits: 0, Outcome: mustJSON(v),
	}); err != nil {
		return StartResult{}, fmt.Errorf("save bet: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return StartResult{}, fmt.Errorf("commit: %w", err)
	}
	return StartResult{RoundID: row.ID, View: v, BalanceCredits: lockRow.BalanceCredits}, nil
}

// CashOut settles the active round at the current multiplier.
func (s *Service) CashOut(ctx context.Context, userID, roundID int64, idempotencyKey string) (StartResult, error) {
	if len(idempotencyKey) == 0 || len(idempotencyKey) > 64 {
		return StartResult{}, ErrIdempotencyKeyInvalid
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return StartResult{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)

	lockRow, err := wallet.LockWallet(ctx, tx, userID)
	if err != nil {
		return StartResult{}, err
	}
	status, err := q.GetUserStatus(ctx, userID)
	if err != nil {
		return StartResult{}, fmt.Errorf("load status: %w", err)
	}
	if admin.StatusForbidsBetting(status) {
		return StartResult{}, ErrStatusForbidsBetting
	}

	row, err := q.GetChickenRoundByID(ctx, roundID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.UserID != userID) {
		return StartResult{}, ErrRoundNotFound
	}
	if err != nil {
		return StartResult{}, fmt.Errorf("load round: %w", err)
	}

	keys, err := parseStrings(row.ActionKeys)
	if err != nil {
		return StartResult{}, err
	}
	for _, k := range keys {
		if k == idempotencyKey {
			return s.replayView(ctx, q, row, lockRow)
		}
	}
	if row.Status != chickenengine.StatusActive {
		return StartResult{}, ErrRoundComplete
	}

	if row.Crossed < 1 {
		return StartResult{}, ErrCannotCash
	}
	d, err := chickenengine.ByName(row.Difficulty)
	if err != nil {
		return StartResult{}, fmt.Errorf("stored difficulty %q: %w", row.Difficulty, err)
	}

	payout := chickenengine.Payout(row.BetCredits, d, int(row.Crossed))
	keys = append(keys, idempotencyKey)
	v, err := s.settle(ctx, tx, q, row, chickenengine.StatusCashed, int(row.Crossed), payout, keys)
	if err != nil {
		return StartResult{}, err
	}
	betID := row.BetID
	res, err := wallet.ApplyTx(ctx, tx, wallet.ApplyRequest{
		UserID: userID, Kind: wallet.KindPayout, Amount: payout,
		BetID: &betID, IdempotencyKey: idempotencyKey + ":payout",
	})
	if err != nil {
		return StartResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return StartResult{}, fmt.Errorf("commit: %w", err)
	}
	return StartResult{RoundID: row.ID, View: v, BalanceCredits: res.Balance}, nil
}

// settle marks the round complete, persists the fatal lane into the round
// and the bet outcome, and returns the finished view. Caller commits.
func (s *Service) settle(
	ctx context.Context, tx pgx.Tx, q *store.Queries,
	row store.ChickenRound, status string, crossed int, payout int64, keys []string,
) (View, error) {
	d, err := chickenengine.ByName(row.Difficulty)
	if err != nil {
		return View{}, fmt.Errorf("stored difficulty %q: %w", row.Difficulty, err)
	}
	v := View{
		RoundID: row.ID, BetID: row.BetID, Status: status,
		BetCredits: row.BetCredits, Difficulty: d.Name, Lanes: int(row.Lanes),
		PayoutCredits: payout,
		Crossed:       crossed,
		Multiplier:    chickenengine.Multiplier(d, crossed),
		FatalLane:     int(row.FatalLane),
	}
	if err := q.SaveChickenRound(ctx, store.SaveChickenRoundParams{
		ID: row.ID, Status: status, PayoutCredits: payout,
		Crossed: int32(crossed), ActionKeys: mustJSON(keys),
		CompletedAt: ptrTime(s.clk.Now()),
	}); err != nil {
		return View{}, fmt.Errorf("save round: %w", err)
	}
	if err := q.SetChickenBetSettlement(ctx, store.SetChickenBetSettlementParams{
		ID: row.BetID, PayoutCredits: payout, Outcome: mustJSON(v),
	}); err != nil {
		return View{}, fmt.Errorf("save bet: %w", err)
	}
	return v, nil
}

// autoCash cashes out a stale active round at its current multiplier using
// a derived idempotency key, so an interrupted retry can never double-pay.
func (s *Service) autoCash(ctx context.Context, tx pgx.Tx, q *store.Queries, row store.ChickenRound) error {
	keys, err := parseStrings(row.ActionKeys)
	if err != nil {
		return err
	}
	payout := int64(0)
	if row.Crossed > 0 {
		d, err := chickenengine.ByName(row.Difficulty)
		if err != nil {
			return fmt.Errorf("stored difficulty %q: %w", row.Difficulty, err)
		}
		payout = chickenengine.Payout(row.BetCredits, d, int(row.Crossed))
	}
	keys = append(keys, fmt.Sprintf("auto-cash:%d", row.BetID))
	if _, err := s.settle(ctx, tx, q, row, chickenengine.StatusCashed, int(row.Crossed), payout, keys); err != nil {
		return err
	}
	if payout == 0 {
		return nil
	}
	betID := row.BetID
	_, err = wallet.ApplyTx(ctx, tx, wallet.ApplyRequest{
		UserID: row.UserID, Kind: wallet.KindPayout, Amount: payout,
		BetID: &betID, IdempotencyKey: fmt.Sprintf("chicken-auto:%d", row.BetID),
	})
	return err
}

func (s *Service) replayView(ctx context.Context, q *store.Queries, row store.ChickenRound, lockRow store.Wallet) (StartResult, error) {
	seed, err := q.GetServerSeedByID(ctx, row.ServerSeedID)
	if err != nil {
		return StartResult{}, fmt.Errorf("load replayed seed: %w", err)
	}
	v, err := s.view(row)
	if err != nil {
		return StartResult{}, err
	}
	return StartResult{
		RoundID: row.ID, View: v, BalanceCredits: lockRow.BalanceCredits,
		ServerSeedHash: seed.SeedHash, ClientSeed: row.ClientSeed, Nonce: row.Nonce, Replay: true,
	}, nil
}

// view renders stored state; the fatal lane stays hidden while the round
// is active.
func (s *Service) view(row store.ChickenRound) (View, error) {
	d, err := chickenengine.ByName(row.Difficulty)
	if err != nil {
		return View{}, fmt.Errorf("stored difficulty %q: %w", row.Difficulty, err)
	}
	v := View{
		RoundID: row.ID, BetID: row.BetID, Status: row.Status,
		BetCredits: row.BetCredits, Difficulty: d.Name, Lanes: int(row.Lanes),
		PayoutCredits: row.PayoutCredits,
		Crossed:       int(row.Crossed),
		Multiplier:    chickenengine.Multiplier(d, int(row.Crossed)),
		Cashable:      row.Status == chickenengine.StatusActive && row.Crossed >= 1,
	}
	if row.Status == chickenengine.StatusActive {
		v.NextMultiplier = chickenengine.Multiplier(d, int(row.Crossed)+1)
	} else {
		v.FatalLane = int(row.FatalLane)
	}
	return v, nil
}

func parseStrings(raw []byte) ([]string, error) {
	var out []string
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse json list: %w", err)
	}
	return out, nil
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("null")
	}
	return b
}

func ptrTime(t time.Time) *time.Time { return &t }
