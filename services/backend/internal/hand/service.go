// Package hand wires the stateful blackjack engine into the wallet and
// fairness layers. A hand spans several transactions (deal, then
// hit/stand/double), so unlike play.Service the money moves are split: the
// stake debits at deal, a double debits its extra stake at the double, and
// the payout credits once at completion — each in one atomic transaction.
//
// The deck is never trusted from storage. Every action replays the hand from
// the fairness triple (server seed, client seed, nonce) plus the persisted
// action log, so the stored state is a cache and the source of truth is
// exactly what verification recomputes.
package hand

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/admin"
	"github.com/ai-doodoo-slots/services/backend/internal/cards"
	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game/blackjack"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/ai-doodoo-slots/services/backend/internal/wallet"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// staleAfter is how long an untouched active hand blocks a new deal before
// the next deal auto-stands it (a closed tab should not hold the seat).
const staleAfter = 5 * time.Minute

var (
	// ErrUnknownGame, ErrInvalidBet, ErrIdempotencyKeyInvalid mirror play.
	ErrInvalidBet            = errors.New("invalid bet")
	ErrIdempotencyKeyInvalid = errors.New("idempotency key must be 1-64 characters")
	// ErrStatusForbidsBetting is returned for banned/self-excluded accounts.
	ErrStatusForbidsBetting = errors.New("account status does not permit betting")
	// ErrHandActive means a deal arrived while another hand is in progress.
	ErrHandActive = errors.New("a hand is already in progress")
	// ErrHandNotFound means no such hand for this user.
	ErrHandNotFound = errors.New("hand not found")
	// ErrHandComplete means the action arrived after resolution.
	ErrHandComplete = blackjack.ErrHandComplete
	// ErrInvalidAction covers illegal moves (double after a hit).
	ErrInvalidAction = blackjack.ErrInvalidAction
	// ErrInvalidActionName covers unknown action verbs.
	ErrInvalidActionName = errors.New("action must be hit, stand, or double")

	// Re-exported for handler mapping.
	ErrInsufficientFunds   = wallet.ErrInsufficientFunds
	ErrIdempotencyConflict = wallet.ErrIdempotencyConflict
	ErrWalletNotFound      = wallet.ErrWalletNotFound
)

// HandView is the client-facing hand state. While the hand is active the
// dealer's hole card is withheld: dealerCards carries only the up card and
// dealerTotal is null. The same shape (fully revealed) is the bet's outcome
// payload, which is what the history table and verifier consume.
type HandView struct {
	HandID        int64    `json:"handId"`
	BetID         int64    `json:"betId"`
	Status        string   `json:"status"`
	BetCredits    int64    `json:"betCredits"`
	PayoutCredits int64    `json:"payoutCredits"`
	Outcome       string   `json:"outcome,omitempty"`
	PlayerCards   string   `json:"playerCards"`
	DealerCards   string   `json:"dealerCards"`
	PlayerTotal   int      `json:"playerTotal"`
	DealerTotal   *int     `json:"dealerTotal"`
	Doubled       bool     `json:"doubled"`
	CanDouble     bool     `json:"canDouble"`
	Actions       []string `json:"actions"`
}

// DealResult is what the deal and action endpoints return.
type DealResult struct {
	HandID         int64
	View           HandView
	BalanceCredits int64
	ServerSeedHash string
	ClientSeed     string
	Nonce          int64
	Replay         bool
}

// snapshot is the persisted hand_state document. DealBetCredits is frozen at
// deal because Double mutates the engine's stake; replay must start from the
// original stake or a doubled hand would double again.
type snapshot struct {
	DealBetCredits int64           `json:"dealBetCredits"`
	State          blackjack.State `json:"state"`
}

// Service executes blackjack deals and actions.
type Service struct {
	pool   *pgxpool.Pool
	engine *blackjack.Engine
	clk    clock.Clock
}

func NewService(pool *pgxpool.Pool, engine *blackjack.Engine, clk clock.Clock) *Service {
	return &Service{pool: pool, engine: engine, clk: clk}
}

// ActiveHand returns the caller's active hand view (masked), or ok=false.
func (s *Service) ActiveHand(ctx context.Context, userID int64) (HandView, bool, error) {
	row, err := store.New(s.pool).GetActiveBlackjackHandByUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return HandView{}, false, nil
	}
	if err != nil {
		return HandView{}, false, fmt.Errorf("load active hand: %w", err)
	}
	snap, err := parseSnapshot(row.HandState)
	if err != nil {
		return HandView{}, false, err
	}
	return s.view(row.ID, row.BetID, &snap.State), true, nil
}

// Deal runs the opening transaction:
//
//	lock wallet → status gate → idempotency → (stale active hand auto-stands)
//	→ consume nonce → shuffle from personal stream → persist hand + bet +
//	debit, one tx. Naturals resolve and pay inside the same tx.
func (s *Service) Deal(ctx context.Context, userID int64, betCredits int64, clientSeed, idempotencyKey string) (DealResult, error) {
	if len(idempotencyKey) == 0 || len(idempotencyKey) > 64 {
		return DealResult{}, ErrIdempotencyKeyInvalid
	}
	if err := s.engine.ValidateBet(betCredits); err != nil {
		return DealResult{}, fmt.Errorf("%w: %v", ErrInvalidBet, err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return DealResult{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)

	// 1-2. Serialize on the wallet, then gate on account status.
	lockRow, err := wallet.LockWallet(ctx, tx, userID)
	if err != nil {
		return DealResult{}, err
	}
	if err := s.gateStatus(ctx, q, userID); err != nil {
		return DealResult{}, err
	}

	// 3. Idempotency: identical retry returns the original hand's current
	// state; a key reused with a different bet is a conflict.
	existing, err := q.GetTransactionByIdempotencyKey(ctx, idempotencyKey)
	if err == nil {
		if existing.WalletID != userID || existing.Kind != wallet.KindBet || existing.AmountCredits != -betCredits {
			return DealResult{}, ErrIdempotencyConflict
		}
		if !existing.BetID.Valid {
			return DealResult{}, fmt.Errorf("bet transaction %d has no bet_id", existing.ID)
		}
		row, err := q.GetBlackjackHandByBetID(ctx, existing.BetID.Int64)
		if err != nil {
			return DealResult{}, fmt.Errorf("load replayed hand: %w", err)
		}
		snap, err := parseSnapshot(row.HandState)
		if err != nil {
			return DealResult{}, err
		}
		seed, err := q.GetServerSeedByID(ctx, row.ServerSeedID)
		if err != nil {
			return DealResult{}, fmt.Errorf("load replayed seed: %w", err)
		}
		return DealResult{
			HandID:         row.ID,
			View:           s.view(row.ID, row.BetID, &snap.State),
			BalanceCredits: lockRow.BalanceCredits,
			ServerSeedHash: seed.SeedHash,
			ClientSeed:     row.ClientSeed,
			Nonce:          row.Nonce,
			Replay:         true,
		}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return DealResult{}, fmt.Errorf("lookup idempotency key: %w", err)
	}

	// 4. One active hand per user; an abandoned one auto-stands so a closed
	// tab cannot hold the seat (the unique partial index backstops races).
	if active, err := q.GetActiveBlackjackHandByUser(ctx, userID); err == nil {
		if age := s.clk.Now().Sub(active.UpdatedAt); age <= staleAfter {
			return DealResult{}, ErrHandActive
		}
		if _, err := s.settleStale(ctx, tx, q, active, userID); err != nil {
			return DealResult{}, err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return DealResult{}, fmt.Errorf("load active hand: %w", err)
	}

	// 5. Seed pair: adopt a changed client seed, consume the nonce.
	seed, err := q.GetActiveServerSeed(ctx, userID)
	if err != nil {
		return DealResult{}, fmt.Errorf("load active seed: %w", err)
	}
	effectiveClientSeed := seed.ClientSeed
	if clientSeed != "" && clientSeed != seed.ClientSeed {
		if len(clientSeed) > fair.MaxClientSeedLen {
			return DealResult{}, fmt.Errorf("client seed exceeds %d bytes", fair.MaxClientSeedLen)
		}
		if err := q.UpdateClientSeed(ctx, store.UpdateClientSeedParams{ID: seed.ID, ClientSeed: clientSeed}); err != nil {
			return DealResult{}, fmt.Errorf("update client seed: %w", err)
		}
		effectiveClientSeed = clientSeed
	}
	newNonce, err := q.IncrementSeedNonce(ctx, seed.ID)
	if err != nil {
		return DealResult{}, fmt.Errorf("increment nonce: %w", err)
	}
	nonce := newNonce - 1

	// 6. Derive the hand — pure given the stream.
	plain, err := hex.DecodeString(seed.SeedPlain.String)
	if err != nil {
		return DealResult{}, fmt.Errorf("decode server seed: %w", err)
	}
	st, err := s.engine.Deal(fair.NewPersonalStream(plain, effectiveClientSeed, nonce), betCredits)
	if err != nil {
		return DealResult{}, err
	}

	// 7-8. Synthetic round + bet row. The outcome payload masks the hole
	// card while the hand can still act; completion rewrites it in full.
	roundID, err := q.CreateSettledRound(ctx, store.CreateSettledRoundParams{
		GameID: blackjack.GameID,
		Result: s.outcomePayload(st),
	})
	if err != nil {
		return DealResult{}, fmt.Errorf("create round: %w", err)
	}
	bet, err := q.InsertBet(ctx, store.InsertBetParams{
		UserID:        userID,
		GameID:        blackjack.GameID,
		RoundID:       roundID,
		BetCredits:    betCredits,
		PayoutCredits: st.PayoutCredits,
		ServerSeedID:  pgtypeInt8(seed.ID),
		ClientSeed:    pgtypeText(effectiveClientSeed),
		Nonce:         pgtypeInt8(nonce),
		Outcome:       s.outcomePayload(st),
	})
	if err != nil {
		return DealResult{}, fmt.Errorf("insert bet: %w", err)
	}

	// 9. Debit the stake; credit an instant natural's payout.
	betID := bet.ID
	res, err := wallet.ApplyTx(ctx, tx, wallet.ApplyRequest{
		UserID:         userID,
		Kind:           wallet.KindBet,
		Amount:         -betCredits,
		BetID:          &betID,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return DealResult{}, err
	}
	balance := res.Balance
	if st.Status == blackjack.StatusComplete && st.PayoutCredits > 0 {
		res, err = wallet.ApplyTx(ctx, tx, wallet.ApplyRequest{
			UserID:         userID,
			Kind:           wallet.KindPayout,
			Amount:         st.PayoutCredits,
			BetID:          &betID,
			IdempotencyKey: idempotencyKey + ":payout",
		})
		if err != nil {
			return DealResult{}, err
		}
		balance = res.Balance
	}

	// 10. Persist the hand.
	handID, err := q.InsertBlackjackHand(ctx, store.InsertBlackjackHandParams{
		UserID:        userID,
		BetID:         betID,
		Status:        st.Status,
		BetCredits:    st.BetCredits,
		PayoutCredits: st.PayoutCredits,
		Actions:       mustJSON(st.Actions),
		ActionKeys:    mustJSON([]string{}),
		HandState:     mustJSON(snapshot{DealBetCredits: betCredits, State: clearDeck(st)}),
		ServerSeedID:  seed.ID,
		ClientSeed:    effectiveClientSeed,
		Nonce:         nonce,
	})
	if err != nil {
		return DealResult{}, fmt.Errorf("insert hand: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return DealResult{}, fmt.Errorf("commit: %w", err)
	}

	return DealResult{
		HandID:         handID,
		View:           s.view(handID, betID, st),
		BalanceCredits: balance,
		ServerSeedHash: seed.SeedHash,
		ClientSeed:     effectiveClientSeed,
		Nonce:          nonce,
	}, nil
}

// Action applies hit/stand/double to the active hand:
//
//	lock wallet → status gate → load hand (owner-checked) → replay from the
//	seed triple → apply the action → double debit / completion payout →
//	persist, one tx. Retried action keys return the current state unchanged.
func (s *Service) Action(ctx context.Context, userID, handID int64, action, idempotencyKey string) (DealResult, error) {
	if len(idempotencyKey) == 0 || len(idempotencyKey) > 64 {
		return DealResult{}, ErrIdempotencyKeyInvalid
	}
	if action != "hit" && action != "stand" && action != "double" {
		return DealResult{}, ErrInvalidActionName
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return DealResult{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)

	lockRow, err := wallet.LockWallet(ctx, tx, userID)
	if err != nil {
		return DealResult{}, err
	}
	if err := s.gateStatus(ctx, q, userID); err != nil {
		return DealResult{}, err
	}

	row, err := q.GetBlackjackHandByID(ctx, handID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.UserID != userID) {
		return DealResult{}, ErrHandNotFound
	}
	if err != nil {
		return DealResult{}, fmt.Errorf("load hand: %w", err)
	}

	// Idempotent action retry: the key is already in the log, so the effect
	// already happened — even when that action completed the hand and the
	// completion response was lost. Checked before the status gate.
	keys, err := parseStrings(row.ActionKeys)
	if err != nil {
		return DealResult{}, err
	}
	for _, k := range keys {
		if k == idempotencyKey {
			snap, err := parseSnapshot(row.HandState)
			if err != nil {
				return DealResult{}, err
			}
			seed, err := q.GetServerSeedByID(ctx, row.ServerSeedID)
			if err != nil {
				return DealResult{}, fmt.Errorf("load replayed seed: %w", err)
			}
			return DealResult{
				HandID:         row.ID,
				View:           s.view(row.ID, row.BetID, &snap.State),
				BalanceCredits: lockRow.BalanceCredits,
				ServerSeedHash: seed.SeedHash,
				ClientSeed:     row.ClientSeed,
				Nonce:          row.Nonce,
				Replay:         true,
			}, nil
		}
	}

	if row.Status != blackjack.StatusActive {
		return DealResult{}, ErrHandComplete
	}

	// Replay the hand from the fairness triple + action log; storage's copy
	// of the cards is never trusted as an input.
	snap, err := parseSnapshot(row.HandState)
	if err != nil {
		return DealResult{}, err
	}
	st, seed, err := s.replay(ctx, q, row, snap.DealBetCredits)
	if err != nil {
		return DealResult{}, err
	}

	prevStake := st.BetCredits
	switch action {
	case "hit":
		err = s.engine.Hit(st)
	case "stand":
		err = s.engine.Stand(st)
	case "double":
		err = s.engine.Double(st)
	}
	if err != nil {
		return DealResult{}, err
	}

	balance := lockRow.BalanceCredits
	betID := row.BetID
	if action == "double" {
		// The extra stake is a second ledger entry with its own key.
		res, err := wallet.ApplyTx(ctx, tx, wallet.ApplyRequest{
			UserID:         userID,
			Kind:           wallet.KindBet,
			Amount:         -(st.BetCredits - prevStake),
			BetID:          &betID,
			IdempotencyKey: idempotencyKey,
		})
		if err != nil {
			return DealResult{}, err
		}
		balance = res.Balance
	}

	completed := st.Status == blackjack.StatusComplete
	if completed {
		if st.PayoutCredits > 0 {
			res, err := wallet.ApplyTx(ctx, tx, wallet.ApplyRequest{
				UserID:         userID,
				Kind:           wallet.KindPayout,
				Amount:         st.PayoutCredits,
				BetID:          &betID,
				IdempotencyKey: idempotencyKey + ":payout",
			})
			if err != nil {
				return DealResult{}, err
			}
			balance = res.Balance
		}
		// The bet row carries the round id; the settled round's result is
		// rewritten to the full reveal.
		bet, err := q.GetBetByID(ctx, betID)
		if err != nil {
			return DealResult{}, fmt.Errorf("load bet: %w", err)
		}
		if err := q.SetBlackjackBetSettlement(ctx, store.SetBlackjackBetSettlementParams{
			ID:            betID,
			BetCredits:    st.BetCredits,
			PayoutCredits: st.PayoutCredits,
			Outcome:       s.outcomePayload(st),
		}); err != nil {
			return DealResult{}, fmt.Errorf("settle bet: %w", err)
		}
		if err := q.SetRoundResult(ctx, store.SetRoundResultParams{
			ID:     bet.RoundID,
			Result: s.outcomePayload(st),
		}); err != nil {
			return DealResult{}, fmt.Errorf("set round result: %w", err)
		}
	}

	// The engine appended the action to st.Actions itself.
	var completedAt *time.Time
	if completed {
		t := s.clk.Now()
		completedAt = &t
	}
	if err := q.SaveBlackjackHand(ctx, store.SaveBlackjackHandParams{
		ID:            row.ID,
		Status:        st.Status,
		BetCredits:    st.BetCredits,
		PayoutCredits: st.PayoutCredits,
		Actions:       mustJSON(st.Actions),
		ActionKeys:    mustJSON(append(keys, idempotencyKey)),
		HandState:     mustJSON(snapshot{DealBetCredits: snap.DealBetCredits, State: clearDeck(st)}),
		CompletedAt:   completedAt,
	}); err != nil {
		return DealResult{}, fmt.Errorf("save hand: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return DealResult{}, fmt.Errorf("commit: %w", err)
	}

	return DealResult{
		HandID:         row.ID,
		View:           s.view(row.ID, row.BetID, st),
		BalanceCredits: balance,
		ServerSeedHash: seed.SeedHash,
		ClientSeed:     row.ClientSeed,
		Nonce:          row.Nonce,
	}, nil
}

// settleStale auto-stands an abandoned active hand inside the caller's
// transaction, paying out whatever it was worth.
func (s *Service) settleStale(ctx context.Context, tx pgx.Tx, q *store.Queries, row store.BlackjackHand, userID int64) (int64, error) {
	snap, err := parseSnapshot(row.HandState)
	if err != nil {
		return 0, err
	}
	st, _, err := s.replay(ctx, q, row, snap.DealBetCredits)
	if err != nil {
		return 0, err
	}
	if err := s.engine.Stand(st); err != nil {
		return 0, err
	}
	betID := row.BetID
	if st.PayoutCredits > 0 {
		if _, err := wallet.ApplyTx(ctx, tx, wallet.ApplyRequest{
			UserID:         userID,
			Kind:           wallet.KindPayout,
			Amount:         st.PayoutCredits,
			BetID:          &betID,
			IdempotencyKey: fmt.Sprintf("stale-stand:%d", row.ID),
		}); err != nil {
			return 0, err
		}
	}
	if err := q.SetBlackjackBetSettlement(ctx, store.SetBlackjackBetSettlementParams{
		ID:            betID,
		BetCredits:    st.BetCredits,
		PayoutCredits: st.PayoutCredits,
		Outcome:       s.outcomePayload(st),
	}); err != nil {
		return 0, fmt.Errorf("settle stale bet: %w", err)
	}
	var completed time.Time = s.clk.Now()
	if err := q.SaveBlackjackHand(ctx, store.SaveBlackjackHandParams{
		ID:            row.ID,
		Status:        blackjack.StatusComplete,
		BetCredits:    st.BetCredits,
		PayoutCredits: st.PayoutCredits,
		Actions:       mustJSON(st.Actions),
		ActionKeys:    row.ActionKeys,
		HandState:     mustJSON(snapshot{DealBetCredits: snap.DealBetCredits, State: clearDeck(st)}),
		CompletedAt:   &completed,
	}); err != nil {
		return 0, fmt.Errorf("save stale hand: %w", err)
	}
	return betID, nil
}

// replay rebuilds the authoritative hand state from the seed triple and the
// stored action log. This is the same derivation a verifier runs.
func (s *Service) replay(ctx context.Context, q *store.Queries, row store.BlackjackHand, dealBet int64) (*blackjack.State, store.ServerSeed, error) {
	seed, err := q.GetServerSeedByID(ctx, row.ServerSeedID)
	if err != nil {
		return nil, store.ServerSeed{}, fmt.Errorf("load hand seed: %w", err)
	}
	plain, err := hex.DecodeString(seed.SeedPlain.String)
	if err != nil {
		return nil, store.ServerSeed{}, fmt.Errorf("decode server seed: %w", err)
	}
	actions, err := parseStrings(row.Actions)
	if err != nil {
		return nil, store.ServerSeed{}, err
	}
	st, err := s.engine.Deal(fair.NewPersonalStream(plain, row.ClientSeed, row.Nonce), dealBet)
	if err != nil {
		return nil, store.ServerSeed{}, err
	}
	for _, a := range actions {
		var err error
		switch a {
		case "hit":
			err = s.engine.Hit(st)
		case "stand":
			err = s.engine.Stand(st)
		case "double":
			err = s.engine.Double(st)
		default:
			err = fmt.Errorf("stored action %q not recognized", a)
		}
		if err != nil {
			return nil, store.ServerSeed{}, fmt.Errorf("replay action %q: %w", a, err)
		}
	}
	return st, seed, nil
}

func (s *Service) gateStatus(ctx context.Context, q *store.Queries, userID int64) error {
	status, err := q.GetUserStatus(ctx, userID)
	if err != nil {
		return fmt.Errorf("load status: %w", err)
	}
	if admin.StatusForbidsBetting(status) {
		return ErrStatusForbidsBetting
	}
	return nil
}

// view renders the client-facing state, withholding the dealer's hole card
// while the hand is active.
func (s *Service) view(handID, betID int64, st *blackjack.State) HandView {
	player, _ := cards.ParseCards(st.PlayerCards)
	playerTotal, _ := cards.BJTotal(player)
	v := HandView{
		HandID:        handID,
		BetID:         betID,
		Status:        st.Status,
		BetCredits:    st.BetCredits,
		PayoutCredits: st.PayoutCredits,
		Outcome:       st.Outcome,
		PlayerCards:   st.PlayerCards,
		PlayerTotal:   playerTotal,
		Doubled:       st.Doubled,
		Actions:       st.Actions,
		CanDouble:     st.Status == blackjack.StatusActive && len(player) == 2 && !st.Doubled,
	}
	if st.Status == blackjack.StatusActive {
		v.DealerCards = st.DealerCards[:2] // up card only
		if v.DealerCards == "" {
			v.DealerCards = st.DealerCards
		}
	} else {
		v.DealerCards = st.DealerCards
		dealer, _ := cards.ParseCards(st.DealerCards)
		dealerTotal, _ := cards.BJTotal(dealer)
		v.DealerTotal = &dealerTotal
	}
	if v.Actions == nil {
		v.Actions = []string{}
	}
	return v
}

// outcomePayload is the bet-row/round result document: the client-facing
// view without the row ids (the bets table carries those). While the hand
// can still act the dealer's hole card stays hidden.
func (s *Service) outcomePayload(st *blackjack.State) []byte {
	v := s.view(0, 0, st)
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// clearDeck strips the remaining deck from the persisted snapshot: it is
// derivable from the triple and must not leak future cards.
func clearDeck(st *blackjack.State) blackjack.State {
	c := *st
	c.Deck = ""
	return c
}

func parseSnapshot(raw []byte) (snapshot, error) {
	var snap snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return snap, fmt.Errorf("parse hand state: %w", err)
	}
	return snap, nil
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

func pgtypeInt8(v int64) pgtype.Int8 {
	return pgtype.Int8{Int64: v, Valid: true}
}

func pgtypeText(v string) pgtype.Text {
	return pgtype.Text{String: v, Valid: true}
}
