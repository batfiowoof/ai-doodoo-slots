package round

import (
	"encoding/json"
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
	ErrTableLimit    = errors.New("round stake limit reached")
)

// Bet is one player's stake on one spot of the current round. Everything
// here lives in the gameserver's memory; persistence happens at place and
// settle time. Crash-style rooms use the empty spot and hold one bet per
// player; spot games (roulette) key each chip by its betting cell.
type Bet struct {
	UserID         int64
	DisplayName    string
	Spot           string          // "" = the crash-style single bet
	BetCredits     int64
	AutoHundredths int64           // 0 = manual-only rider (crash)
	Cashed         bool
	CashHundredths int64           // server receipt-time multiplier when manually cashed
	BetID          int64           // set by the intake after the DB row exists
	Options        json.RawMessage // per-bet options, game-specific (e.g. {"spot": "red"})
}

// Settlement is one bet's payout at round end. PayoutCredits are whole
// credits; MultiplierHundredths records the winning multiplier (0 = lost).
type Settlement struct {
	UserID               int64
	BetID                int64
	Spot                 string
	PayoutCredits        int64
	MultiplierHundredths int64
}

// bets is the machine's stake registry, guarded by its own mutex: the runner
// steps the machine on its goroutine while socket goroutines place bets.
// Bets key by (user, spot); the empty spot keeps the crash path byte-for-byte
// compatible with its one-bet-per-player rule.
type bets struct {
	mu     sync.Mutex
	byUser map[int64]map[string]*Bet
}

func newBets() *bets { return &bets{byUser: make(map[int64]map[string]*Bet)} }

// addSpot registers a stake. Only during betting_open; one bet per spot,
// and crash-style rooms (empty spot) keep their one-bet-per-player rule.
func (b *bets) addSpot(m *Machine, userID, credits, autoHundredths int64, spot string, options json.RawMessage, displayName string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if m.StateForStakes() != game.PhaseBettingOpen {
		return ErrWrongPhase
	}
	spots := b.byUser[userID]
	if spots == nil {
		spots = make(map[string]*Bet)
		b.byUser[userID] = spots
	}
	if spot == "" {
		if len(spots) > 0 {
			return ErrAlreadyBet
		}
	} else if _, ok := spots[spot]; ok {
		return ErrAlreadyBet
	}
	spots[spot] = &Bet{
		UserID: userID, DisplayName: displayName, Spot: spot,
		BetCredits: credits, AutoHundredths: autoHundredths, Options: options,
	}
	return nil
}

// removeSpot undoes an addSpot (used when the wallet debit fails).
func (b *bets) removeSpot(userID int64, spot string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if spots := b.byUser[userID]; spots != nil {
		delete(spots, spot)
		if len(spots) == 0 {
			delete(b.byUser, userID)
		}
	}
}

// clear drops every bet a player holds on the round and returns them; the
// caller refunds the total through the persister.
func (b *bets) clear(userID int64) []*Bet {
	b.mu.Lock()
	defer b.mu.Unlock()
	spots := b.byUser[userID]
	if spots == nil {
		return nil
	}
	out := make([]*Bet, 0, len(spots))
	for _, bet := range spots {
		out = append(out, bet)
	}
	delete(b.byUser, userID)
	sort.Slice(out, func(i, j int) bool { return out[i].Spot < out[j].Spot })
	return out
}

// cashOut pays a manual cash-out at the server's receipt-time multiplier.
// The authoritative moment is when the server receives the action, never a
// client-supplied timestamp. Pays exactly once. Crash-style only: spot
// games settle at round end and hold no "" bet to cash.
func (b *bets) cashOut(userID int64, now time.Time, m *Machine) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if m.StateForStakes() != game.PhaseRunning {
		return 0, ErrNotRunning
	}
	bet, ok := b.byUser[userID][""]
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

// settle computes every payout against the resolved result. Crash rooms run
// the in-memory cashout math (auto targets were evaluated by declaration,
// manual cashouts paid-marked at receipt time); spot games defer to the
// engine's SettleBet per chip. Losers get a zero settlement so the bet rows
// still record the outcome.
func (b *bets) settle(m *Machine, result game.RoundResult) []Settlement {
	b.mu.Lock()
	defer b.mu.Unlock()
	crashHundredths := int64(result.Multiplier * 100)
	out := make([]Settlement, 0, len(b.byUser))
	for userID, spots := range b.byUser {
		for _, bet := range spots {
			switch {
			case m.cfg.SpotSettle:
				payout, err := m.game.SettleBet(result, game.RoundBet{BetCredits: bet.BetCredits, Options: bet.Options})
				if err != nil || payout < 0 {
					// Spots were validated at bet time; an error here is a
					// programmer error and must never fail the round.
					payout = 0
				}
				out = append(out, Settlement{
					UserID: userID, BetID: bet.BetID, Spot: bet.Spot,
					PayoutCredits: payout,
				})
			case bet.Cashed:
				out = append(out, Settlement{
					UserID: userID, BetID: bet.BetID, Spot: bet.Spot,
					PayoutCredits:        bet.BetCredits * bet.CashHundredths / 100,
					MultiplierHundredths: bet.CashHundredths,
				})
			case bet.AutoHundredths > 0 && bet.AutoHundredths <= crashHundredths:
				out = append(out, Settlement{
					UserID: userID, BetID: bet.BetID, Spot: bet.Spot,
					PayoutCredits:        bet.BetCredits * bet.AutoHundredths / 100,
					MultiplierHundredths: bet.AutoHundredths,
				})
			default:
				out = append(out, Settlement{UserID: userID, BetID: bet.BetID, Spot: bet.Spot})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UserID != out[j].UserID {
			return out[i].UserID < out[j].UserID
		}
		return out[i].Spot < out[j].Spot
	})
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

// totalOf sums one player's open stake across spots.
func (b *bets) totalOf(userID int64) int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	var total int64
	for _, bet := range b.byUser[userID] {
		total += bet.BetCredits
	}
	return total
}

// setBetID attaches the DB row id.
func (b *bets) setBetID(userID int64, spot string, betID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if bet, ok := b.byUser[userID][spot]; ok {
		bet.BetID = betID
	}
}

// of returns a pointer to the user's crash-style stake (same lock domain
// as add).
func (b *bets) of(userID int64) *Bet {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.byUser[userID][""]
}

// view projects stakes in stable user-id order. Spot games report the
// per-spot breakdown so reconnecting clients can restore the board.
func (b *bets) view() []StakeView {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]StakeView, 0, len(b.byUser))
	for userID, spots := range b.byUser {
		v := StakeView{UserID: userID}
		for _, bet := range spots {
			v.Credits += bet.BetCredits
			if bet.DisplayName != "" {
				v.DisplayName = bet.DisplayName
			}
			if bet.Spot != "" {
				v.Spots = append(v.Spots, SpotStakeView{Spot: bet.Spot, Credits: bet.BetCredits})
			}
			if bet.Cashed {
				v.CashedAt = float64(bet.CashHundredths) / 100
			}
		}
		sort.Slice(v.Spots, func(i, j int) bool { return v.Spots[i].Spot < v.Spots[j].Spot })
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	return out
}

// --- machine surface (locking delegated to bets) ----------------------

// AddBet registers a crash-style stake during betting_open.
func (m *Machine) AddBet(userID, credits, autoHundredths int64, displayName string) error {
	return m.stakes.addSpot(m, userID, credits, autoHundredths, "", nil, displayName)
}

// AddSpotBet registers one chip on one betting cell during betting_open.
// Options carries the game-specific declaration ({"spot": ...}).
func (m *Machine) AddSpotBet(userID, credits int64, spot string, options json.RawMessage, displayName string) error {
	return m.stakes.addSpot(m, userID, credits, 0, spot, options, displayName)
}

// RemoveBet undoes an AddBet when the wallet debit failed.
func (m *Machine) RemoveBet(userID int64) { m.stakes.removeSpot(userID, "") }

// RemoveSpotBet undoes an AddSpotBet when the wallet debit failed.
func (m *Machine) RemoveSpotBet(userID int64, spot string) { m.stakes.removeSpot(userID, spot) }

// ClearBets drops every stake the player holds on this round and returns
// them; the intake refunds the total through the persister.
func (m *Machine) ClearBets(userID int64) []*Bet { return m.stakes.clear(userID) }

// CashOut pays a manual cash-out at server receipt time. Returns the
// payout in whole credits; the credit itself lands at batch settlement.
func (m *Machine) CashOut(userID int64, now time.Time) (int64, error) {
	return m.stakes.cashOut(userID, now, m)
}

// Settlements computes the round's payouts. Valid after the result.
func (m *Machine) Settlements() []Settlement { return m.stakes.settle(m, m.result) }

// StakeUserIDs returns the sorted list of players with stakes.
func (m *Machine) StakeUserIDs() []int64 { return m.stakes.userIDs() }

// UserStakeTotal sums a player's open stake across spots; the intake
// enforces the per-round exposure cap against it.
func (m *Machine) UserStakeTotal(userID int64) int64 { return m.stakes.totalOf(userID) }

// SetBetID attaches the persisted bet row id once the debit transaction
// commits.
func (m *Machine) SetBetID(userID int64, spot string, betID int64) { m.stakes.setBetID(userID, spot, betID) }

// StakeOf returns a pointer to the user's crash-style stake, if any.
func (m *Machine) StakeOf(userID int64) *Bet { return m.stakes.of(userID) }

// StakeView is the client-safe projection of a stake.
type StakeView struct {
	UserID      int64           `json:"userId"`
	DisplayName string          `json:"displayName,omitempty"`
	Credits     int64           `json:"credits"`
	CashedAt    float64         `json:"cashedAt,omitempty"` // multiplier, 0 = riding
	Spots       []SpotStakeView `json:"spots,omitempty"`    // spot games only
}

// SpotStakeView is one cell's open stake for a player.
type SpotStakeView struct {
	Spot    string `json:"spot"`
	Credits int64  `json:"credits"`
}

// StakesView lists current stakes for snapshots and reconnects.
func (m *Machine) StakesView() []StakeView { return m.stakes.view() }

// CodedError tags intake failures with a stable wire code so the socket
// layer can relay them without importing this package.
type CodedError struct {
	Code string
	Err  error
}

func (e *CodedError) Error() string { return e.Err.Error() }
func (e *CodedError) Unwrap() error { return e.Err }

func coded(code string, err error) *CodedError { return &CodedError{Code: code, Err: err} }
