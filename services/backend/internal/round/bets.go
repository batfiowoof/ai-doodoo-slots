package round

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/game"
)

// Errors returned by the bet surface. The intake layer maps them to socket
// error messages; every one of them is also re-checked server-side at
// settlement, never trusted from the client.
var (
	ErrWrongPhase    = errors.New("bets only during betting_open")
	ErrAlreadyBet    = errors.New("already bet this round")
	ErrUnknownRound  = errors.New("bet for a past round")
	ErrNotRunning    = errors.New("cash-out only during running")
	ErrAlreadyCashed = errors.New("already cashed out")
	ErrNoBet         = errors.New("no bet this round")
	ErrTooLate       = errors.New("round already crashed")
)

// Bet is one player's stake in the current round. Everything here lives in
// the gameserver's memory; persistence happens at place and settle time.
type Bet struct {
	UserID          int64
	BetCredits      int64
	AutoHundredths  int64 // 0 = manual-only rider
	Cashed          bool
	CashHundredths  int64 // server receipt-time multiplier when manually cashed
	BetID           int64 // set by the intake after the DB row exists
}

// Settlement is one player's payout at round end. PayoutCredits are whole
// credits; MultiplierHundredths records the winning multiplier (0 = lost).
type Settlement struct {
	UserID              int64
	BetID               int64
	PayoutCredits       int64
	MultiplierHundredths int64
}

// bets is the machine's stake registry, guarded by its own mutex: the runner
// steps the machine on its goroutine while socket goroutines place bets.
type bets struct {
	mu   sync.Mutex
	byUser map[int64]*Bet
}

func newBets() *bets { return &bets{byUser: make(map[int64]*Bet)} }

// add registers a stake. Only during betting_open, one per user per round.
func (b *bets) add(m *Machine, userID, credits, autoHundredths int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if m.StateForStakes() != game.PhaseBettingOpen {
		return ErrWrongPhase
	}
	if _, ok := b.byUser[userID]; ok {
		return ErrAlreadyBet
	}
	b.byUser[userID] = &Bet{UserID: userID, BetCredits: credits, AutoHundredths: autoHundredths}
	return nil
}

// remove undoes an add (used when the wallet debit fails).
func (b *bets) remove(userID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.byUser, userID)
}

// cashOut pays a manual cash-out at the server's receipt-time multiplier.
// The authoritative moment is when the server receives the action, never a
// client-supplied timestamp. Pays exactly once.
func (b *bets) cashOut(userID int64, now time.Time, m *Machine) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if m.StateForStakes() != game.PhaseRunning {
		return 0, ErrNotRunning
	}
	bet, ok := b.byUser[userID]
	if !ok {
		return 0, ErrNoBet
	}
	if bet.Cashed {
		return 0, ErrAlreadyCashed
	}
	// The displayed multiplier at receipt, clamped to the crash point. If
	// the crash point is already reached the bet loses.
	mult := m.MultiplierAt(now)
	if mult >= m.result.Multiplier {
		return 0, ErrTooLate
	}
	hundredths := int64(mult * 100)
	bet.Cashed = true
	bet.CashHundredths = hundredths
	return bet.BetCredits * hundredths / 100, nil
}

// settle computes every payout against the resolved result. Auto-cashouts
// were evaluated server-side by declaration: target <= crash wins the
// target; manual cashouts were paid-marked at receipt time. Losers get a
// zero settlement so the bet rows still record the outcome.
func (b *bets) settle(result game.RoundResult) []Settlement {
	b.mu.Lock()
	defer b.mu.Unlock()
	crashHundredths := int64(result.Multiplier * 100)
	out := make([]Settlement, 0, len(b.byUser))
	for _, bet := range b.byUser {
		switch {
		case bet.Cashed:
			out = append(out, Settlement{
				UserID: bet.UserID, BetID: bet.BetID,
				PayoutCredits:        bet.BetCredits * bet.CashHundredths / 100,
				MultiplierHundredths: bet.CashHundredths,
			})
		case bet.AutoHundredths > 0 && bet.AutoHundredths <= crashHundredths:
			out = append(out, Settlement{
				UserID: bet.UserID, BetID: bet.BetID,
				PayoutCredits:        bet.BetCredits * bet.AutoHundredths / 100,
				MultiplierHundredths: bet.AutoHundredths,
			})
		default:
			out = append(out, Settlement{UserID: bet.UserID, BetID: bet.BetID})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	return out
}

// userIDs returns the sorted stake holders (wallet locks are always
// acquired in sorted ID order).
func (b *bets) userIDs() []int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	ids := make([]int64, 0, len(b.byUser))
	for id := range b.byUser {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// setBetID attaches the DB row id.
func (b *bets) setBetID(userID, betID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if bet, ok := b.byUser[userID]; ok {
		bet.BetID = betID
	}
}

// of returns a pointer to the user's stake (same lock domain as add).
func (b *bets) of(userID int64) *Bet {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.byUser[userID]
}

// --- machine surface (locking delegated to bets) ----------------------

// AddBet registers a stake during betting_open.
func (m *Machine) AddBet(userID, credits, autoHundredths int64) error {
	return m.stakes.add(m, userID, credits, autoHundredths)
}

// RemoveBet undoes an AddBet when the wallet debit failed.
func (m *Machine) RemoveBet(userID int64) { m.stakes.remove(userID) }

// CashOut pays a manual cash-out at server receipt time. Returns the
// payout in whole credits; the credit itself lands at batch settlement.
func (m *Machine) CashOut(userID int64, now time.Time) (int64, error) {
	return m.stakes.cashOut(userID, now, m)
}

// Settlements computes the round's payouts. Valid after the result.
func (m *Machine) Settlements() []Settlement { return m.stakes.settle(m.result) }

// StakeUserIDs returns the sorted list of players with stakes.
func (m *Machine) StakeUserIDs() []int64 { return m.stakes.userIDs() }

// SetBetID attaches the persisted bet row id once the debit transaction
// commits.
func (m *Machine) SetBetID(userID, betID int64) { m.stakes.setBetID(userID, betID) }

// StakeOf returns a copy of the user's stake, if any.
func (m *Machine) StakeOf(userID int64) *Bet { return m.stakes.of(userID) }

// CodedError tags intake failures with a stable wire code so the socket
// layer can relay them without importing this package.
type CodedError struct {
	Code string
	Err  error
}

func (e *CodedError) Error() string { return e.Err.Error() }
func (e *CodedError) Unwrap() error { return e.Err }

func coded(code string, err error) *CodedError { return &CodedError{Code: code, Err: err} }
