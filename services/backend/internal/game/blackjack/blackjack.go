// Package blackjack is the stateful single-player card engine: deal, hit,
// stand, double. Unlike the instant games, a hand spans multiple
// transactions, so the deck order is fixed at deal time (Fisher-Yates over
// the personal fair stream) and persisted with the hand; every later draw
// is the next card of that recorded deck. The whole hand still replays
// from the seed triple, which keeps the provably-fair contract intact.
package blackjack

import (
	"errors"
	"fmt"

	"github.com/ai-doodoo-slots/services/backend/internal/cards"
	"github.com/ai-doodoo-slots/services/backend/internal/fair"
)

// GameID identifies the game in the registry and bets table.
const GameID = "blackjack"

// Hand lifecycle statuses.
const (
	StatusActive   = "active"
	StatusComplete = "complete"
)

// Settlement outcomes recorded on the hand.
const (
	OutcomeBlackjack = "blackjack" // natural 21, pays 3:2
	OutcomeWin       = "win"
	OutcomePush      = "push"
	OutcomeLose      = "lose"
	OutcomeBust      = "bust" // player over 21
)

// Engine errors surfaced by the service layer.
var (
	// ErrHandComplete means the action arrived after resolution.
	ErrHandComplete = errors.New("hand is already complete")
	// ErrInvalidAction covers illegal moves (double after a hit).
	ErrInvalidAction = errors.New("action not allowed at this point")
	// ErrBetStep means the stake is not one of the configured steps.
	ErrBetStep = errors.New("bet must be one of the configured steps")
)

// State is the full authoritative hand state, persisted as JSON between
// actions. Card lists use the compact two-character codes ("AsKd7c").
type State struct {
	BetCredits    int64    `json:"betCredits"`
	PlayerCards   string   `json:"playerCards"`
	DealerCards   string   `json:"dealerCards"`
	Deck          string   `json:"deck"` // remaining cards, index 0 is the next draw
	Actions       []string `json:"actions"`
	Status        string   `json:"status"`
	Outcome       string   `json:"outcome"`
	PayoutCredits int64    `json:"payoutCredits"`
	Doubled       bool     `json:"doubled"`
}

// Engine implements the ruleset. It is stateless; all state lives in State.
type Engine struct {
	betSteps []int64
}

// New returns an engine enforcing the given bet steps.
func New(betSteps []int64) *Engine {
	return &Engine{betSteps: betSteps}
}

// BetSteps exposes the configured steps for the games listing.
func (e *Engine) BetSteps() []int64 { return e.betSteps }

// ValidateBet enforces the step table.
func (e *Engine) ValidateBet(credits int64) error {
	for _, step := range e.betSteps {
		if credits == step {
			return nil
		}
	}
	return fmt.Errorf("%w: %d not in %v", ErrBetStep, credits, e.betSteps)
}

// TheoreticalRTP is the simulated basic-strategy return with this ruleset
// (dealer stands on all 17s, no split, double on the first two cards only,
// natural pays 3:2 rounded up). See TestSimulationRTP.
func (e *Engine) TheoreticalRTP() float64 { return 0.987 }

// Deal shuffles a full deck from the stream and deals the opening cards.
// A dealer natural resolves immediately; a player natural pays 3:2
// immediately. Everything else waits for actions.
func (e *Engine) Deal(s *fair.Stream, betCredits int64) (*State, error) {
	if err := e.ValidateBet(betCredits); err != nil {
		return nil, err
	}
	deck := cards.NewDeck()
	cards.Shuffle(deck, s)

	st := &State{
		BetCredits: betCredits,
		Status:     StatusActive,
	}
	draw := func() cards.Card {
		c := deck[0]
		deck = deck[1:]
		return c
	}

	player := []cards.Card{draw(), draw()}
	dealer := []cards.Card{draw(), draw()}

	if cards.IsBlackjack(dealer) {
		// Dealer peek: a dealer natural ends the hand before any action.
		st.PlayerCards = cards.CardsString(player)
		st.DealerCards = cards.CardsString(dealer)
		st.Deck = cards.CardsString(deck)
		if cards.IsBlackjack(player) {
			st.PayoutCredits = betCredits // push: stake returned
			st.Outcome = OutcomePush
		} else {
			st.Outcome = OutcomeLose
		}
		st.Status = StatusComplete
		return st, nil
	}
	if cards.IsBlackjack(player) {
		st.PlayerCards = cards.CardsString(player)
		st.DealerCards = cards.CardsString(dealer)
		st.Deck = cards.CardsString(deck)
		st.PayoutCredits = naturalPayout(betCredits)
		st.Outcome = OutcomeBlackjack
		st.Status = StatusComplete
		return st, nil
	}

	st.PlayerCards = cards.CardsString(player)
	st.DealerCards = cards.CardsString(dealer)
	st.Deck = cards.CardsString(deck)
	return st, nil
}

// Hit draws one card. Reaching 21 stands automatically; busting resolves
// immediately (the dealer never draws against a busted player).
func (e *Engine) Hit(st *State) error {
	if st.Status != StatusActive {
		return ErrHandComplete
	}
	player, deck, err := popCards(st.PlayerCards, st.Deck, 1)
	if err != nil {
		return err
	}
	st.PlayerCards = cards.CardsString(player)
	st.Deck = cards.CardsString(deck)
	st.Actions = append(st.Actions, "hit")

	total, _ := cards.BJTotal(player)
	switch {
	case total > 21:
		st.Outcome = OutcomeBust
		st.PayoutCredits = 0
		st.Status = StatusComplete
	case total == 21:
		return e.dealerPlays(st)
	}
	return nil
}

// Stand stops the player; the dealer draws to 17 and the hand settles.
func (e *Engine) Stand(st *State) error {
	if st.Status != StatusActive {
		return ErrHandComplete
	}
	st.Actions = append(st.Actions, "stand")
	return e.dealerPlays(st)
}

// Double doubles the stake, draws exactly one card, and stands. Legal only
// as the first action on a two-card hand.
func (e *Engine) Double(st *State) error {
	if st.Status != StatusActive {
		return ErrHandComplete
	}
	player, err := cards.ParseCards(st.PlayerCards)
	if err != nil {
		return err
	}
	if len(player) != 2 || st.Doubled {
		return ErrInvalidAction
	}
	player, deck, err := popCards(st.PlayerCards, st.Deck, 1)
	if err != nil {
		return err
	}
	st.PlayerCards = cards.CardsString(player)
	st.Deck = cards.CardsString(deck)
	st.BetCredits *= 2
	st.Doubled = true
	st.Actions = append(st.Actions, "double")

	total, _ := cards.BJTotal(player)
	if total > 21 {
		st.Outcome = OutcomeBust
		st.PayoutCredits = 0
		st.Status = StatusComplete
		return nil
	}
	return e.dealerPlays(st)
}

// dealerPlays draws for the dealer while under 17 (dealer stands on all
// 17s) and settles the hand.
func (e *Engine) dealerPlays(st *State) error {
	player, err := cards.ParseCards(st.PlayerCards)
	if err != nil {
		return err
	}
	dealer, err := cards.ParseCards(st.DealerCards)
	if err != nil {
		return err
	}
	deck, err := cards.ParseCards(st.Deck)
	if err != nil {
		return err
	}
	for {
		total, _ := cards.BJTotal(dealer)
		if total >= 17 {
			break
		}
		dealer = append(dealer, deck[0])
		deck = deck[1:]
	}
	st.DealerCards = cards.CardsString(dealer)
	st.Deck = cards.CardsString(deck)

	playerTotal, _ := cards.BJTotal(player)
	dealerTotal, _ := cards.BJTotal(dealer)
	st.Status = StatusComplete
	switch {
	case dealerTotal > 21 || playerTotal > dealerTotal:
		st.Outcome = OutcomeWin
		st.PayoutCredits = 2 * st.BetCredits
	case playerTotal == dealerTotal:
		st.Outcome = OutcomePush
		st.PayoutCredits = st.BetCredits
	default:
		st.Outcome = OutcomeLose
		st.PayoutCredits = 0
	}
	return nil
}

// PayoutCredits already includes the returned stake, matching the slots
// convention (payout is the total credited back).

// naturalPayout returns bet + 1.5*bet, with the half credit rounded up in
// the player's favor on odd stakes.
func naturalPayout(bet int64) int64 {
	return bet + (3*bet+1)/2
}

// popCards parses the player list and deck, draws n cards from the deck
// onto the player list, and returns both.
func popCards(playerStr, deckStr string, n int) ([]cards.Card, []cards.Card, error) {
	player, err := cards.ParseCards(playerStr)
	if err != nil {
		return nil, nil, err
	}
	deck, err := cards.ParseCards(deckStr)
	if err != nil {
		return nil, nil, err
	}
	if int64(len(deck)) < int64(n) {
		return nil, nil, fmt.Errorf("deck exhausted")
	}
	player = append(player, deck[:n]...)
	return player, deck[n:], nil
}
