package round

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/ai-doodoo-slots/services/backend/internal/bus"
	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/ai-doodoo-slots/services/backend/internal/wallet"
	"github.com/ai-doodoo-slots/services/backend/internal/ws"
	"github.com/jackc/pgx/v5/pgtype"
)

// Bet intake errors — the socket layer relays them as error messages.
var (
	ErrForbiddenStatus = errors.New("account status does not permit betting")
	ErrInvalidAmount   = errors.New("invalid bet amount")
	ErrStaleRound      = errors.New("stale round")
)

// SettledBet carries one bet's outcome to the persister.
type SettledBet struct {
	BetID                int64
	UserID               int64
	PayoutCredits        int64
	MultiplierHundredths int64
}

// Intake is the authorized path for socket bet messages. It is the only
// component allowed to move money on behalf of a round; every message runs
// the same authorization gate as an HTTP request (identity, status, phase).
type Intake struct {
	runner  *Runner
	persist Persister
	clk     clock.Clock
	bus     bus.Bus
	logger  *slog.Logger
}

func NewIntake(r *Runner, persist Persister, clk clock.Clock, b bus.Bus, logger *slog.Logger) *Intake {
	return &Intake{runner: r, persist: persist, clk: clk, bus: b, logger: logger}
}

// HandleGameAction adapts socket bet messages to room-handler routing: the
// hub injects the wire verb ("place_bet"/"cash_out") as the action, so each
// room's intake enforces its own limits and rounds.
func (i *Intake) HandleGameAction(id ws.Identity, payload json.RawMessage) (map[string]any, error) {
	var p struct {
		Action         string  `json:"action"`
		Credits        int64   `json:"credits"`
		AutoCashout    float64 `json:"autoCashout"`
		IdempotencyKey string  `json:"idempotencyKey"`
	}
	if err := json.Unmarshal(payload, &p); err != nil || p.Action == "" {
		return nil, coded("bad_request", fmt.Errorf("malformed bet message"))
	}
	if id.Status != "active" {
		return nil, codedBet(ErrForbiddenStatus)
	}
	switch p.Action {
	case "place_bet":
		hundredths := int64(p.AutoCashout * 100)
		return i.PlaceBet(id, p.Credits, hundredths, p.IdempotencyKey)
	case "cash_out":
		return i.CashOut(id)
	default:
		return nil, coded("bad_request", fmt.Errorf("unknown action %q", p.Action))
	}
}

var _ ws.RoomHandler = (*Intake)(nil)

// PlaceBet validates and debits a stake during betting_open. The wallet
// debit and the bet row land in one transaction; a failure there removes
// the in-memory reservation.
func (i *Intake) PlaceBet(id ws.Identity, credits, autoHundredths int64, idemKey string) (map[string]any, error) {
	// Authorization: banned and self-excluded accounts may read but never bet.
	if id.Status != "active" {
		return nil, codedBet(ErrForbiddenStatus)
	}
	if credits <= 0 || credits > 1_000_000 || idemKey == "" {
		return nil, codedBet(ErrInvalidAmount)
	}
	// Room stake tier: enforced server-side from the rooms table.
	if min, max := i.runner.Limits(); credits < min || credits > max {
		return nil, coded("out_of_tier", fmt.Errorf("bet must be between %d and %d", min, max))
	}
	roundID, m := i.runner.Live()
	if m == nil || roundID == 0 {
		return nil, codedBet(ErrUnknownRound)
	}
	if err := m.AddBet(id.UserID, credits, autoHundredths, id.DisplayName); err != nil {
		return nil, codedBet(err)
	}
	betID, balance, err := i.persist.PlaceBet(context.Background(), id.UserID, roundID, credits, autoHundredths, idemKey)
	if err != nil {
		m.RemoveBet(id.UserID)
		if errors.Is(err, wallet.ErrInsufficientFunds) {
			return nil, codedBet(wallet.ErrInsufficientFunds)
		}
		return nil, codedBet(err)
	}
	m.SetBetID(id.UserID, betID)
	i.publishRoom("bet_placed", map[string]any{
		"userId":      id.UserID,
		"displayName": id.DisplayName,
		"credits":     credits,
	})
	return map[string]any{
		"roundId":        roundID,
		"betCredits":     credits,
		"balanceCredits": balance,
	}, nil
}

// CashOut marks a manual cash-out at the server's receipt time. The credit
// itself lands in the round's atomic batch settlement.
func (i *Intake) CashOut(id ws.Identity) (map[string]any, error) {
	if id.Status != "active" {
		return nil, codedBet(ErrForbiddenStatus)
	}
	roundID, m := i.runner.Live()
	if m == nil || roundID == 0 {
		return nil, codedBet(ErrUnknownRound)
	}
	// Server receipt time is the authoritative moment — never a client
	// timestamp.
	payout, err := m.CashOut(id.UserID, i.clk.Now())
	if err != nil {
		return nil, codedBet(err)
	}
	hundredths := int64(0)
	if bet := m.StakeOf(id.UserID); bet != nil && bet.Cashed {
		hundredths = bet.CashHundredths
	}
	i.publishRoom("bet_cashout", map[string]any{
		"userId":      id.UserID,
		"displayName": id.DisplayName,
		"multiplier":  float64(hundredths) / 100,
	})
	if err := i.persist.MarkCashout(context.Background(), roundID, id.UserID, hundredths); err != nil {
		// The machine remains the source of truth for settlement; a failed
		// display update must not roll back the authoritative state.
		i.logger.Warn("mark cashout", "err", err)
	}
	return map[string]any{
		"roundId":       roundID,
		"payoutCredits": payout,
		"paidAtReceipt": true,
	}, nil
}

// --- persister: place/mark/settle -------------------------------------

// pgPersister additions live here so the Persister interface stays the one
// money boundary.

func (p *pgPersister) PlaceBet(ctx context.Context, userID, roundID, credits, autoHundredths int64, idemKey string) (int64, int64, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	// Debit first: wallet row FOR UPDATE inside the same transaction as the
	// ledger insert and the bet row.
	res, err := wallet.ApplyTx(ctx, tx, wallet.ApplyRequest{
		UserID:         userID,
		Kind:           wallet.KindBet,
		Amount:         -credits,
		IdempotencyKey: "round:" + strconv.FormatInt(roundID, 10) + ":" + strconv.FormatInt(userID, 10) + ":bet:" + idemKey,
	})
	if err != nil {
		return 0, 0, err
	}
	betID, err := store.New(tx).InsertRoundBet(ctx, store.InsertRoundBetParams{
		UserID:               userID,
		RoundID:              roundID,
		BetCredits:           credits,
		AutoCashoutHundredths: autoHundredths,
	})
	if err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return betID, res.Balance, nil
}

func (p *pgPersister) MarkCashout(ctx context.Context, roundID, userID, hundredths int64) error {
	// The bet id is resolved from the round+user; one bet per round per user.
	rows, err := p.pool.Query(ctx, `SELECT id FROM bets WHERE round_id = $1 AND user_id = $2 AND action = 'bet'`, roundID, userID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := store.New(p.pool).MarkBetCashout(ctx, store.MarkBetCashoutParams{ID: id, CashoutHundredths: hundredths}); err != nil {
			return err
		}
	}
	return nil
}

func settleKey(roundID, userID int64) string {
	return "round:" + strconv.FormatInt(roundID, 10) + ":" + strconv.FormatInt(userID, 10) + ":win"
}

func settledUserIDs(settled []SettledBet) []int64 {
	ids := make([]int64, 0, len(settled))
	for _, s := range settled {
		ids = append(ids, s.UserID)
	}
	return ids
}
// SettleRound settles the round atomically: wallets lock in ascending
// user_id order (deadlocks structurally impossible), ledger inserts and
// balance updates ride one transaction, and every bet row records its
// outcome. A round is all-paid or none.
func (p *pgPersister) SettleRound(ctx context.Context, roundID int64, settled []SettledBet) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)

	// Lock every stake holder's wallet in ascending user_id order.
	wallets, err := q.LockWalletsSorted(ctx, settledUserIDs(settled))
	if err != nil {
		return err
	}
	_ = wallets

	for _, s := range settled {
		if err := q.SetBetPayout(ctx, store.SetBetPayoutParams{ID: s.BetID, PayoutCredits: s.PayoutCredits}); err != nil {
			return err
		}
		if s.PayoutCredits <= 0 {
			continue
		}
		if _, err := q.InsertTransaction(ctx, store.InsertTransactionParams{
			WalletID:       s.UserID,
			Kind:           wallet.KindPayout,
			AmountCredits:  s.PayoutCredits,
			BetID:          pgtype.Int8{Int64: s.BetID, Valid: true},
			IdempotencyKey: settleKey(roundID, s.UserID),
		}); err != nil {
			return err
		}
		// Delta update of the cached materialization, same transaction as
		// the ledger insert.
		if _, err := q.UpdateWalletBalance(ctx, store.UpdateWalletBalanceParams{
			UserID: s.UserID, BalanceCredits: s.PayoutCredits,
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// codedBet tags intake failures with their wire code.
func codedBet(err error) error {
	switch err {
	case ErrForbiddenStatus:
		return coded("status_forbids_betting", err)
	case ErrInvalidAmount:
		return coded("invalid_amount", err)
	case ErrUnknownRound:
		return coded("unknown_round", err)
	case ErrWrongPhase:
		return coded("wrong_phase", err)
	case ErrAlreadyBet:
		return coded("already_bet", err)
	case ErrAlreadyCashed:
		return coded("already_cashed", err)
	case ErrNotRunning:
		return coded("not_running", err)
	case ErrTooLate:
		return coded("too_late", err)
	case ErrNoBet:
		return coded("no_bet", err)
	case wallet.ErrInsufficientFunds:
		return coded("insufficient_funds", err)
	}
	return err
}

// publishRoom fans a room event out via the bus (the hub relays it to the
// room's sockets; the lobby never sees it).
func (i *Intake) publishRoom(evType string, payload map[string]any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	i.bus.Publish(bus.Event{Topic: "rooms", Room: i.runner.room, Type: evType, Payload: raw})
}


