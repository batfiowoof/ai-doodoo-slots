// Package roulette implements the shared-round roulette game: a European
// single-zero wheel (37 pockets) with the classic betting menu — straight
// numbers, dozens, columns and the even-money cells. The winning pocket
// derives from the commit-reveal chain stream; every player's spot bets
// settle server-side against that one shared outcome.
package roulette

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
)

// GameID is the registry identifier.
const GameID = "roulette"

// Phase durations: a betting window long enough to spread chips across
// spots, a no-more-bets lock, a suspense spin, and a settled display.
const (
	BettingWindow = 18 * time.Second
	LockedWindow  = 2 * time.Second
	SpinDuration  = 6 * time.Second
	SettledWindow = 6 * time.Second
)

// Payout multipliers are the total returned per credit staked, stake
// included: straight 36 = 35:1 + stake, dozen/column 3 = 2:1 + stake,
// even-money 2 = 1:1 + stake. Every spot's expected return is
// Payout × hits/37 = 36/37, so the wheel's RTP is uniform across the menu.
const (
	PayoutStraight = 36
	PayoutGroup    = 3  // dozens and columns
	PayoutEven     = 2  // red/black, odd/even, low/high
	PocketCount    = 37 // European single-zero
)

// WheelOrder is the European wheel clockwise from the zero. The ball lands
// on WheelOrder[wheelIndex]; the zero sits at index 0.
var WheelOrder = [PocketCount]int{0, 32, 15, 19, 4, 21, 2, 25, 17, 34, 6, 27, 13, 36, 11, 30, 8, 23, 10, 5, 24, 16, 33, 1, 20, 14, 31, 9, 22, 18, 29, 7, 28, 12, 35, 3, 26}

// reds are the red pockets of the European layout; the rest (minus zero)
// are black.
var reds = map[int]bool{
	1: true, 3: true, 5: true, 7: true, 9: true, 12: true, 14: true, 16: true,
	18: true, 19: true, 21: true, 23: true, 25: true, 27: true, 30: true,
	32: true, 34: true, 36: true,
}

// PocketColor returns "red", "black" or "green" (the zero).
func PocketColor(pocket int) string {
	switch {
	case pocket == 0:
		return "green"
	case reds[pocket]:
		return "red"
	default:
		return "black"
	}
}

// Spot is one betting cell: ID is the wire identifier, Payout the total
// return per credit staked, Hits the pockets it wins on.
type Spot struct {
	ID     string
	Label  string
	Payout int64
	Hits   func(pocket int) bool
}

func dozen(n int) func(int) bool {
	return func(p int) bool { return p >= (n-1)*12+1 && p <= n*12 }
}

// column n holds the numbers ≡ n (mod 3): column 1 is 1,4,…,34, column 3 is
// 3,6,…,36. The zero belongs to no column.
func column(n int) func(int) bool {
	return func(p int) bool { return p > 0 && p%3 == n%3 }
}

// Spots returns the full betting menu in board order.
func Spots() []Spot {
	out := make([]Spot, 0, PocketCount+12)
	for n := 0; n <= 36; n++ {
		n := n
		out = append(out, Spot{
			ID:     fmt.Sprintf("n%d", n),
			Label:  fmt.Sprintf("STRAIGHT %d", n),
			Payout: PayoutStraight,
			Hits:   func(pocket int) bool { return pocket == n },
		})
	}
	for d := 1; d <= 3; d++ {
		d := d
		out = append(out, Spot{
			ID:     fmt.Sprintf("d%d", d),
			Label:  fmt.Sprintf("%s DOZEN", map[int]string{1: "1ST", 2: "2ND", 3: "3RD"}[d]),
			Payout: PayoutGroup,
			Hits:   dozen(d),
		})
	}
	for c := 1; c <= 3; c++ {
		c := c
		out = append(out, Spot{
			ID:     fmt.Sprintf("c%d", c),
			Label:  fmt.Sprintf("%s COLUMN", map[int]string{1: "1ST", 2: "2ND", 3: "3RD"}[c]),
			Payout: PayoutGroup,
			Hits:   column(c),
		})
	}
	out = append(out,
		Spot{ID: "red", Label: "RED", Payout: PayoutEven, Hits: func(p int) bool { return PocketColor(p) == "red" }},
		Spot{ID: "black", Label: "BLACK", Payout: PayoutEven, Hits: func(p int) bool { return PocketColor(p) == "black" }},
		Spot{ID: "odd", Label: "ODD", Payout: PayoutEven, Hits: func(p int) bool { return p > 0 && p%2 == 1 }},
		Spot{ID: "even", Label: "EVEN", Payout: PayoutEven, Hits: func(p int) bool { return p > 0 && p%2 == 0 }},
		Spot{ID: "low", Label: "1-18", Payout: PayoutEven, Hits: func(p int) bool { return p >= 1 && p <= 18 }},
		Spot{ID: "high", Label: "19-36", Payout: PayoutEven, Hits: func(p int) bool { return p >= 19 && p <= 36 }},
	)
	return out
}

var spotIndex = func() map[string]Spot {
	m := make(map[string]Spot)
	for _, s := range Spots() {
		m[s.ID] = s
	}
	return m
}()

// ValidateSpot rejects unknown betting spots before any money moves.
func ValidateSpot(spot string) error {
	if _, ok := spotIndex[spot]; !ok {
		return fmt.Errorf("unknown spot %q", spot)
	}
	return nil
}

// ValidateSpot makes the engine satisfy the intake's spot-validator
// interface so roulette rooms accept spot bets.
func (r *Roulette) ValidateSpot(spot string) error { return ValidateSpot(spot) }

// Roulette is the RoundGame implementation.
type Roulette struct{}

// New returns the roulette engine.
func New() *Roulette { return &Roulette{} }

func (r *Roulette) ID() string { return GameID }

// Phases returns the state machine in transition order. All durations are
// fixed — the spin is pure suspense, not a curve to reach an outcome.
func (r *Roulette) Phases() []game.Phase {
	return []game.Phase{
		{Kind: game.PhaseBettingOpen, Duration: BettingWindow},
		{Kind: game.PhaseLocked, Duration: LockedWindow},
		{Kind: game.PhaseRunning, Duration: SpinDuration},
		{Kind: game.PhaseSettled, Duration: SettledWindow},
	}
}

// Curve is the running-phase display value. Roulette has no multiplier
// ladder — clients drive their own spin animation — so it reports zero.
func Curve(float64) float64 { return 0 }

// RunningFor returns the fixed suspense spin length.
func RunningFor(game.RoundResult) time.Duration { return SpinDuration }

// HistoryValue extracts the winning pocket for the recent-results history
// that powers the lobby and room strips.
func HistoryValue(res game.RoundResult) float64 {
	var p resultPayload
	if err := json.Unmarshal(res.Payload, &p); err != nil {
		return 0
	}
	return float64(p.Pocket)
}

// resultPayload is the round's client-facing outcome.
type resultPayload struct {
	Pocket     int    `json:"pocket"`
	Color      string `json:"color"`
	WheelIndex int    `json:"wheelIndex"`
}

// betOptions is what a player declares per spot bet at bet time.
type betOptions struct {
	Spot string `json:"spot"`
}

// Resolve derives the winning pocket from the chain-derived stream: one
// uniform draw over the 37 pockets. Pure and deterministic — the same
// stream always lands the same pocket.
func (r *Roulette) Resolve(s *fair.Stream) (game.RoundResult, error) {
	weights := make([]int64, PocketCount)
	for i := range weights {
		weights[i] = 1
	}
	idx := s.WeightedPick(weights)
	pocket := WheelOrder[idx]
	payload, err := json.Marshal(resultPayload{Pocket: pocket, Color: PocketColor(pocket), WheelIndex: idx})
	if err != nil {
		return game.RoundResult{}, err
	}
	return game.RoundResult{Multiplier: 0, Payload: payload}, nil
}

// SettleBet pays one spot bet against the round result. Payout stays in
// int64 (credits × small integer multiplier). Unknown spots and malformed
// payloads return an error — impossible through the validated intake, and
// the machine's settle path contains them to a zero payout so a programmer
// error can never fail a live round.
func (r *Roulette) SettleBet(res game.RoundResult, b game.RoundBet) (int64, error) {
	var rp resultPayload
	if err := json.Unmarshal(res.Payload, &rp); err != nil {
		return 0, err
	}
	var opts betOptions
	if err := json.Unmarshal(b.Options, &opts); err != nil {
		return 0, err
	}
	spot, ok := spotIndex[opts.Spot]
	if !ok {
		return 0, fmt.Errorf("unknown spot %q", opts.Spot)
	}
	if !spot.Hits(rp.Pocket) {
		return 0, nil
	}
	return b.BetCredits * spot.Payout, nil
}

func (r *Roulette) TheoreticalRTP() float64 {
	return float64(PayoutStraight) / float64(PocketCount)
}
