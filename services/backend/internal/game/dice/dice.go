// Package dice implements the instant over/under roll. The player picks a
// direction (UNDER or OVER) and a target from 2 to 98; the roll is uniform
// on [0, 100) at two decimals. A win pays bet·99/chance, floored — the 1%
// house edge lives in the 99. Because the payout is a pure function of the
// roll and the declared params, every play replays from the seed triple.
package dice

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
)

// GameID is the registry identifier.
const GameID = "dice"

const (
	// EdgeComplement is the payout denominator: a win pays 99/chance,
	// keeping the long-run return at 99%.
	EdgeComplement = 99
	// MinTarget / MaxTarget bound the win chance to [2%, 98%].
	MinTarget = 2
	MaxTarget = 98
	// MinBet / MaxBet are the accepted stake bounds.
	MinBet = 1
	MaxBet = 10000
)

// Params are the player's declared choices for one roll.
type Params struct {
	Direction string `json:"direction"` // "under" or "over"
	Target    int    `json:"target"`    // 2..98
}

func (p Params) validate() error {
	if p.Direction != "under" && p.Direction != "over" {
		return fmt.Errorf("direction must be \"under\" or \"over\"")
	}
	if p.Target < MinTarget || p.Target > MaxTarget {
		return fmt.Errorf("target must be %d-%d", MinTarget, MaxTarget)
	}
	return nil
}

// chance is the win probability in whole percent.
func (p Params) chance() int {
	if p.Direction == "under" {
		return p.Target
	}
	return 100 - p.Target
}

// Game is the dice engine.
type Game struct{}

func New() *Game { return &Game{} }

func (g *Game) ID() string            { return GameID }
func (g *Game) DisplayName() string   { return "Dice" }
func (g *Game) TheoreticalRTP() float64 { return 0.99 }

func (g *Game) ValidateBet(credits int64) error {
	if credits < MinBet || credits > MaxBet {
		return fmt.Errorf("bet must be between %d and %d credits", MinBet, MaxBet)
	}
	return nil
}

// BetLimits exposes the accepted stake range for the games listing.
func (g *Game) BetLimits() (int64, int64) { return MinBet, MaxBet }

func (g *Game) ValidateParams(raw json.RawMessage) error {
	var p Params
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("decode params: %w", err)
	}
	return p.validate()
}

func (g *Game) Play(s *fair.Stream, betCredits int64) (game.Outcome, error) {
	return g.PlayWithParams(s, betCredits, nil)
}

// PlayWithParams rolls and settles one bet. Play (no params) always loses —
// dice is meaningless without a declared direction/target.
func (g *Game) PlayWithParams(s *fair.Stream, betCredits int64, params json.RawMessage) (game.Outcome, error) {
	if err := g.ValidateBet(betCredits); err != nil {
		return game.Outcome{}, err
	}
	var p Params
	if len(params) == 0 {
		return game.Outcome{}, fmt.Errorf("dice requires params")
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return game.Outcome{}, fmt.Errorf("decode params: %w", err)
	}
	if err := p.validate(); err != nil {
		return game.Outcome{}, err
	}

	roll := math.Floor(s.Float()*10000) / 100
	var win bool
	if p.Direction == "under" {
		win = roll < float64(p.Target)
	} else {
		win = roll > float64(p.Target)
	}

	chance := p.chance()
	multiplier := float64(EdgeComplement) / float64(chance)
	var payout int64
	if win {
		payout = betCredits * EdgeComplement / int64(chance)
	}
	payoutMultiplier := 0.0
	if win {
		payoutMultiplier = float64(payout) / float64(betCredits)
	}

	payload, err := json.Marshal(map[string]any{
		"roll":             roll,
		"direction":        p.Direction,
		"target":           p.Target,
		"chance":           chance,
		"multiplier":       multiplier,
		"win":              win,
		"payoutMultiplier": payoutMultiplier,
	})
	if err != nil {
		return game.Outcome{}, fmt.Errorf("marshal payload: %w", err)
	}
	return game.Outcome{PayoutCredits: payout, Payload: payload}, nil
}
