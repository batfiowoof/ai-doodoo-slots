// Package mines wires the stateful mines engine into the wallet and
// fairness layers. A round spans several transactions (start, then
// reveal/cashout), so like hand.Service the money moves are split: the
// stake debits at start and the payout credits once at cashout — each in
// one atomic transaction. Mine positions derive from the fairness triple,
// which is what verification replays.
package mines

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
	minesengine "github.com/ai-doodoo-slots/services/backend/internal/game/mines"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/ai-doodoo-slots/services/backend/internal/wallet"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// staleAfter is how long an untouched active round blocks a new start
// before the next start auto-cashes it (a closed tab should not hold the
// stake hostage — the player revealed only safe tiles by definition).
const staleAfter = 5 * time.Minute

var (
	ErrInvalidBet            = errors.New("invalid bet")
	ErrMineCountInvalid      = errors.New("invalid mine count")
	ErrIdempotencyKeyInvalid = errors.New("idempotency key must be 1-64 characters")
	ErrStatusForbidsBetting  = errors.New("account status does not permit betting")
	ErrRoundActive           = errors.New("a mines round is already in progress")
	ErrRoundNotFound         = errors.New("mines round not found")
	ErrRoundComplete         = errors.New("mines round is already complete")
	ErrInvalidTile           = errors.New("tile out of range or already revealed")
	ErrCannotCash            = errors.New("reveal at least one safe tile before cashing out")

	// Re-exported for handler mapping.
	ErrInsufficientFunds   = wallet.ErrInsufficientFunds
	ErrIdempotencyConflict = wallet.ErrIdempotencyConflict
	ErrWalletNotFound      = wallet.ErrWalletNotFound
)

// View is the client-facing round state. While the round is active the
// mine positions are withheld; the same shape (fully revealed) is the bet's
// outcome payload for history and verification.
type View struct {
	RoundID        int64   `json:"roundId"`
	BetID          int64   `json:"betId"`
	Status         string  `json:"status"`
	BetCredits     int64   `json:"betCredits"`
	MineCount      int     `json:"mineCount"`
	PayoutCredits  int64   `json:"payoutCredits"`
	Revealed       []int   `json:"revealed"`
	Multiplier     float64 `json:"multiplier"`
	NextMultiplier float64 `json:"nextMultiplier"`
	Cashable       bool    `json:"cashable"`
	Mines          []int   `json:"mines,omitempty"` // only when complete
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

// Service executes mines rounds.
type Service struct {
	pool *pgxpool.Pool
	clk  clock.Clock
}

func NewService(pool *pgxpool.Pool, clk clock.Clock) *Service {
	return &Service{pool: pool, clk: clk}
}

// ActiveRound returns the caller's active round view (mines withheld), or
// ok=false.
func (s *Service) ActiveRound(ctx context.Context, userID int64) (View, bool, error) {
	row, err := store.New(s.pool).GetActiveMinesRoundByUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return View{}, false, nil
	}
	if err != nil {
		return View{}, false, fmt.Errorf("load active round: %w", err)
	}
	v, err := s.view(row, false)
	if err != nil {
		return View{}, false, err
	}
	return v, true, nil
}

// Start runs the opening transaction: lock wallet → status gate →
// idempotency → (stale active round auto-cashes) → consume nonce → draw
// mines from the personal stream → persist round + bet + debit, one tx.
func (s *Service) Start(ctx context.Context, userID int64, betCredits int64, mineCount int, clientSeed, idempotencyKey string) (StartResult, error) {
	if len(idempotencyKey) == 0 || len(idempotencyKey) > 64 {
		return StartResult{}, ErrIdempotencyKeyInvalid
	}
	if err := minesengine.ValidateBet(betCredits); err != nil {
		return StartResult{}, fmt.Errorf("%w: %v", ErrInvalidBet, err)
	}
	if err := minesengine.ValidateMineCount(mineCount); err != nil {
		return StartResult{}, fmt.Errorf("%w: %v", ErrMineCountInvalid, err)
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
		row, err := q.GetMinesRoundByBetID(ctx, existing.BetID.Int64)
		if err != nil {
			return StartResult{}, fmt.Errorf("load replayed round: %w", err)
		}
		seed, err := q.GetServerSeedByID(ctx, row.ServerSeedID)
		if err != nil {
			return StartResult{}, fmt.Errorf("load replayed seed: %w", err)
		}
		v, err := s.view(row, false)
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
	if active, err := q.GetActiveMinesRoundByUser(ctx, userID); err == nil {
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

	// Draw the mine layout — pure given the stream.
	plain, err := hex.DecodeString(seed.SeedPlain.String)
	if err != nil {
		return StartResult{}, fmt.Errorf("decode server seed: %w", err)
	}
	position := minesengine.DrawMines(fair.NewPersonalStream(plain, effectiveClientSeed, nonce), mineCount)

	// Synthetic round + bet row; the outcome payload withholds the mines.
	v := View{
		Status: minesengine.StatusActive, BetCredits: betCredits, MineCount: mineCount,
		Revealed: []int{}, Multiplier: 1,
		NextMultiplier: minesengine.Multiplier(mineCount, 1),
	}
	roundID, err := q.CreateSettledRound(ctx, store.CreateSettledRoundParams{
		GameID: minesengine.GameID,
		Result: mustJSON(v),
	})
	if err != nil {
		return StartResult{}, fmt.Errorf("create round: %w", err)
	}
	bet, err := q.InsertBet(ctx, store.InsertBetParams{
		UserID: userID, GameID: minesengine.GameID, RoundID: roundID,
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

	roundRowID, err := q.InsertMinesRound(ctx, store.InsertMinesRoundParams{
		UserID: userID, BetID: betID, Status: minesengine.StatusActive,
		BetCredits: betCredits, MineCount: int32(mineCount), PayoutCredits: 0,
		Mines: mustJSON(position), Revealed: mustJSON([]int{}),
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

// Reveal uncovers one tile on the active round: lock wallet → status gate →
// load round (owner-checked) → mine check → bust settle or reveal persist,
// one tx. A retried action key returns the current state unchanged.
func (s *Service) Reveal(ctx context.Context, userID, roundID int64, tile int, idempotencyKey string) (StartResult, error) {
	if len(idempotencyKey) == 0 || len(idempotencyKey) > 64 {
		return StartResult{}, ErrIdempotencyKeyInvalid
	}
	if tile < 0 || tile >= minesengine.Tiles {
		return StartResult{}, ErrInvalidTile
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

	row, err := q.GetMinesRoundByID(ctx, roundID)
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
	if row.Status != minesengine.StatusActive {
		return StartResult{}, ErrRoundComplete
	}

	revealed, err := parseInts(row.Revealed)
	if err != nil {
		return StartResult{}, err
	}
	for _, r := range revealed {
		if r == tile {
			return StartResult{}, ErrInvalidTile
		}
	}
	layout, err := parseInts(row.Mines)
	if err != nil {
		return StartResult{}, err
	}
	mineHit := false
	for _, m := range layout {
		if m == tile {
			mineHit = true
			break
		}
	}

	keys = append(keys, idempotencyKey)
	if mineHit {
		// Bust: settle at zero and reveal everything.
		v, err := s.settle(ctx, tx, q, row, minesengine.StatusBusted, revealed, layout, 0, keys)
		if err != nil {
			return StartResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return StartResult{}, fmt.Errorf("commit: %w", err)
		}
		return StartResult{RoundID: row.ID, View: v, BalanceCredits: lockRow.BalanceCredits}, nil
	}

	revealed = append(revealed, tile)
	v := View{
		RoundID: row.ID, BetID: row.BetID, Status: minesengine.StatusActive,
		BetCredits: row.BetCredits, MineCount: int(row.MineCount), PayoutCredits: 0,
		Revealed:       revealed,
		Multiplier:     minesengine.Multiplier(int(row.MineCount), len(revealed)),
		NextMultiplier: minesengine.Multiplier(int(row.MineCount), len(revealed)+1),
		Cashable:       len(revealed) >= 1,
	}
	if err := q.SaveMinesRound(ctx, store.SaveMinesRoundParams{
		ID: row.ID, Status: minesengine.StatusActive, PayoutCredits: 0,
		Revealed: mustJSON(revealed), ActionKeys: mustJSON(keys), CompletedAt: nil,
	}); err != nil {
		return StartResult{}, fmt.Errorf("save round: %w", err)
	}
	if err := q.SetMinesBetSettlement(ctx, store.SetMinesBetSettlementParams{
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

	row, err := q.GetMinesRoundByID(ctx, roundID)
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
	if row.Status != minesengine.StatusActive {
		return StartResult{}, ErrRoundComplete
	}

	revealed, err := parseInts(row.Revealed)
	if err != nil {
		return StartResult{}, err
	}
	if len(revealed) == 0 {
		return StartResult{}, ErrCannotCash
	}
	layout, err := parseInts(row.Mines)
	if err != nil {
		return StartResult{}, err
	}

	payout := minesengine.Payout(row.BetCredits, int(row.MineCount), len(revealed))
	keys = append(keys, idempotencyKey)
	v, err := s.settle(ctx, tx, q, row, minesengine.StatusCashed, revealed, layout, payout, keys)
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

// settle marks the round complete, persists the full reveal into the round
// and the bet outcome, and returns the finished view. Caller commits.
func (s *Service) settle(
	ctx context.Context, tx pgx.Tx, q *store.Queries,
	row store.MinesRound, status string, revealed, layout []int, payout int64, keys []string,
) (View, error) {
	mineCount := int(row.MineCount)
	v := View{
		RoundID: row.ID, BetID: row.BetID, Status: status,
		BetCredits: row.BetCredits, MineCount: mineCount, PayoutCredits: payout,
		Revealed:   revealed,
		Multiplier: minesengine.Multiplier(mineCount, len(revealed)),
		Mines:      layout,
	}
	if err := q.SaveMinesRound(ctx, store.SaveMinesRoundParams{
		ID: row.ID, Status: status, PayoutCredits: payout,
		Revealed: mustJSON(revealed), ActionKeys: mustJSON(keys),
		CompletedAt: ptrTime(s.clk.Now()),
	}); err != nil {
		return View{}, fmt.Errorf("save round: %w", err)
	}
	if err := q.SetMinesBetSettlement(ctx, store.SetMinesBetSettlementParams{
		ID: row.BetID, PayoutCredits: payout, Outcome: mustJSON(v),
	}); err != nil {
		return View{}, fmt.Errorf("save bet: %w", err)
	}
	return v, nil
}

// autoCash cashes out a stale active round at its current multiplier using
// a derived idempotency key, so an interrupted retry can never double-pay.
func (s *Service) autoCash(ctx context.Context, tx pgx.Tx, q *store.Queries, row store.MinesRound) error {
	revealed, err := parseInts(row.Revealed)
	if err != nil {
		return err
	}
	layout, err := parseInts(row.Mines)
	if err != nil {
		return err
	}
	keys, err := parseStrings(row.ActionKeys)
	if err != nil {
		return err
	}
	payout := int64(0)
	if len(revealed) > 0 {
		payout = minesengine.Payout(row.BetCredits, int(row.MineCount), len(revealed))
	}
	keys = append(keys, fmt.Sprintf("auto-cash:%d", row.BetID))
	if _, err := s.settle(ctx, tx, q, row, minesengine.StatusCashed, revealed, layout, payout, keys); err != nil {
		return err
	}
	if payout == 0 {
		return nil
	}
	betID := row.BetID
	_, err = wallet.ApplyTx(ctx, tx, wallet.ApplyRequest{
		UserID: row.UserID, Kind: wallet.KindPayout, Amount: payout,
		BetID: &betID, IdempotencyKey: fmt.Sprintf("mines-auto:%d", row.BetID),
	})
	return err
}

func (s *Service) replayView(ctx context.Context, q *store.Queries, row store.MinesRound, lockRow store.Wallet) (StartResult, error) {
	seed, err := q.GetServerSeedByID(ctx, row.ServerSeedID)
	if err != nil {
		return StartResult{}, fmt.Errorf("load replayed seed: %w", err)
	}
	v, err := s.view(row, false)
	if err != nil {
		return StartResult{}, err
	}
	return StartResult{
		RoundID: row.ID, View: v, BalanceCredits: lockRow.BalanceCredits,
		ServerSeedHash: seed.SeedHash, ClientSeed: row.ClientSeed, Nonce: row.Nonce, Replay: true,
	}, nil
}

// view renders stored state; mines stay hidden while the round is active.
func (s *Service) view(row store.MinesRound, revealMines bool) (View, error) {
	revealed, err := parseInts(row.Revealed)
	if err != nil {
		return View{}, err
	}
	mineCount := int(row.MineCount)
	v := View{
		RoundID: row.ID, BetID: row.BetID, Status: row.Status,
		BetCredits: row.BetCredits, MineCount: mineCount, PayoutCredits: row.PayoutCredits,
		Revealed:   revealed,
		Multiplier: minesengine.Multiplier(mineCount, len(revealed)),
		Cashable:   row.Status == minesengine.StatusActive && len(revealed) >= 1,
	}
	if row.Status == minesengine.StatusActive {
		v.NextMultiplier = minesengine.Multiplier(mineCount, len(revealed)+1)
	} else {
		layout, err := parseInts(row.Mines)
		if err != nil {
			return View{}, err
		}
		v.Mines = layout
	}
	return v, nil
}

func parseInts(raw []byte) ([]int, error) {
	var out []int
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse json int list: %w", err)
	}
	return out, nil
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
