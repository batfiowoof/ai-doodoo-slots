// Package chicken implements the stateful single-player chicken run game:
// the chicken crosses L road lanes one hop at a time; every lane passed
// raises the multiplier and the player cashes out before hopping into the
// one fatal lane. The fatal lane is drawn once from the personal fair
// stream at start, so a finished round replays exactly from the fairness
// triple plus the hop log.
package chicken

import (
	"fmt"
	"math"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
)

// GameID is the registry and routes identifier.
const GameID = "chicken"

const (
	// MinBet bounds the stake from below.
	MinBet = 1
	// MaxBet is the global stake ceiling; risky roads lower it (MaxBetFor).
	MaxBet = 10000
	// EdgeComplement scales every multiplier: the return is 99% at any
	// cashout point.
	EdgeComplement = 0.99
	// MaxWinCredits caps a single round's payout; risky roads shrink their
	// max bet so a top-lane cashout cannot exceed it.
	MaxWinCredits = 1_000_000
)

// Round lifecycle statuses.
const (
	StatusActive   = "active"
	StatusCashed   = "cashed"
	StatusSquashed = "squashed"
)

// Difficulty is a road preset: lane count and the per-lane survival
// probability. Multiplier(lane) = EdgeComplement·Survival^−lane, so cashing
// out at any lane returns EdgeComplement in the long run.
type Difficulty struct {
	Name     string
	Lanes    int
	Survival float64
}

// Difficulties are the selectable roads, easiest first.
var Difficulties = []Difficulty{
	{Name: "easy", Lanes: 20, Survival: 0.92},
	{Name: "medium", Lanes: 18, Survival: 0.82},
	{Name: "hard", Lanes: 15, Survival: 0.70},
	{Name: "hardcore", Lanes: 12, Survival: 0.55},
}

// ByName resolves a difficulty preset.
func ByName(name string) (Difficulty, error) {
	for _, d := range Difficulties {
		if d.Name == name {
			return d, nil
		}
	}
	return Difficulty{}, fmt.Errorf("difficulty must be one of easy, medium, hard, hardcore")
}

// TopMultiplier is the ladder's last step.
func (d Difficulty) TopMultiplier() float64 { return Multiplier(d, d.Lanes) }

// MaxBetFor shrinks the stake ceiling on risky roads so a top-lane cashout
// stays under MaxWinCredits.
func MaxBetFor(d Difficulty) int64 {
	capped := int64(float64(MaxWinCredits) / d.TopMultiplier())
	if capped > MaxBet {
		return MaxBet
	}
	if capped < MinBet {
		return MinBet
	}
	return capped
}

// ValidateBet bounds the stake for the given road.
func ValidateBet(d Difficulty, credits int64) error {
	max := MaxBetFor(d)
	if credits < MinBet || credits > max {
		return fmt.Errorf("bet must be between %d and %d credits", MinBet, max)
	}
	return nil
}

// BetLimits exposes the widest accepted stake range for the games listing.
func BetLimits() (int64, int64) { return MinBet, MaxBet }

// DrawFatalLane draws the lane the chicken must not enter: 1..Lanes, or
// Lanes+1 when the roll clears the whole road. Per-lane survival is
// Survival, so P(fatal = m) = (1−Survival)·Survival^(m−1) and
// P(clear) = Survival^Lanes; inverting one uniform float samples it.
func DrawFatalLane(s *fair.Stream, d Difficulty) int {
	u := s.Float()
	if u <= 0 || u < math.Pow(d.Survival, float64(d.Lanes)) {
		return d.Lanes + 1
	}
	lane := int(math.Ceil(math.Log(u) / math.Log(d.Survival)))
	if lane < 1 {
		lane = 1
	}
	if lane > d.Lanes {
		lane = d.Lanes
	}
	return lane
}

// Multiplier is the cashout value after lane lanes crossed:
// EdgeComplement·Survival^−lane, floored to two decimals — the inverse of
// the probability of reaching that lane, so the expected return is
// EdgeComplement wherever the player stops.
func Multiplier(d Difficulty, lane int) float64 {
	if lane <= 0 {
		return 1
	}
	m := EdgeComplement / math.Pow(d.Survival, float64(lane))
	return math.Floor(m*100) / 100
}

// Payout is the credited amount for a cashout after lane crossings.
func Payout(betCredits int64, d Difficulty, lane int) int64 {
	return int64(math.Floor(float64(betCredits) * Multiplier(d, lane)))
}

// TheoreticalRTP is the long-run return of any fixed-lane cashout strategy.
func TheoreticalRTP() float64 { return EdgeComplement }
