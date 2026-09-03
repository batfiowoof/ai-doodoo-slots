// Package crash implements the shared-round crash game. The round's crash
// point derives from the commit-reveal chain stream; every player's payout
// is settled server-side against that one shared outcome.
package crash

import (
	"encoding/json"
	"math"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
)

// GameID is the registry identifier.
const GameID = "crash"

const (
	// Edge is the house edge (1%): P(crash >= m) = EdgeComplement / m.
	Edge = 0.99
	// MaxMultiplier caps a round's crash point.
	MaxMultiplier = 1000.0
	// GrowthRate is the running-phase curve: multiplier = e^(rate * seconds).
	GrowthRate = 0.12
)

// Options are the per-bet options a player declares at bet time. Only the
// server evaluates them (latency fairness): there is no client-supplied
// cash-out timestamp anywhere.
type Options struct {
	// AutoCashout is the target multiplier in [1.01, MaxMultiplier]; the
	// bet pays AutoCashout x stake when the round reaches it before the
	// crash point. Required — a bet without one rides to the crash and
	// loses at phase 13 (manual cash-out arrives with live sockets).
	AutoCashout float64 `json:"autoCashout"`
}

// Crash is the RoundGame implementation.
type Crash struct{}

// New returns the crash engine.
func New() *Crash { return &Crash{} }

func (c *Crash) ID() string { return GameID }

// Phases returns the state machine in transition order. Running is
// dynamic — it ends when the displayed multiplier reaches the crash point —
// so its listed duration is a safety cap.
func (c *Crash) Phases() []game.Phase {
	maxRunning := time.Duration(math.Log(MaxMultiplier)/GrowthRate*float64(time.Second)) + time.Second
	return []game.Phase{
		{Kind: game.PhaseBettingOpen, Duration: 7 * time.Second},
		{Kind: game.PhaseLocked, Duration: 1 * time.Second},
		{Kind: game.PhaseRunning, Duration: maxRunning},
		{Kind: game.PhaseSettled, Duration: 4 * time.Second},
	}
}

// MultiplierAt is the running-phase display curve: multiplier as a function
// of elapsed seconds since the running phase began. Exported for the round
// machine's Config.
func MultiplierAt(elapsedSeconds float64) float64 {
	return math.Floor(100*math.Exp(GrowthRate*elapsedSeconds)) / 100
}

// RunningFor returns how long the display curve needs to reach the round's
// resolved multiplier (with a safety floor so instant crashes still render).
func RunningFor(r game.RoundResult) time.Duration {
	sec := math.Log(math.Max(r.Multiplier, 1.01)) / GrowthRate
	return time.Duration(sec * float64(time.Second))
}

// Resolve derives the round's crash point from the chain-derived stream.
// Pure and deterministic: the same stream always yields the same crash.
func (c *Crash) Resolve(s *fair.Stream) (game.RoundResult, error) {
	u := s.Float()
	// P(crash >= m) = Edge / m. With u uniform in [0,1): m = Edge/(1-u),
	// floored to two decimals so displayed and settled multipliers agree.
	m := math.Floor(100*Edge/(1-u)) / 100
	if m < 1.00 {
		m = 1.00 // instant crash
	}
	if m > MaxMultiplier {
		m = MaxMultiplier
	}
	payload, _ := json.Marshal(map[string]any{"crashMultiplier": m})
	return game.RoundResult{Multiplier: m, Payload: payload}, nil
}

// SettleBet pays a bet against the round result. Payout math stays in
// int64: the target is expressed in hundredths and the stake never touches
// a float — the float target only multiplies hundredths.
func (c *Crash) SettleBet(r game.RoundResult, b game.RoundBet) (int64, error) {
	var opts Options
	if len(b.Options) > 0 {
		if err := json.Unmarshal(b.Options, &opts); err != nil {
			return 0, err
		}
	}
	if opts.AutoCashout < 1.01 || opts.AutoCashout > MaxMultiplier {
		return 0, nil
	}
	// Round (not floor): 2.51*100 in float64 is 250.999…, and the user's
	// target must not silently become 2.50.
	target := int64(math.Round(opts.AutoCashout * 100)) // hundredths
	if r.Multiplier+1e-9 >= float64(target)/100 {
		// Payout = stake * target, floored at whole-credit precision.
		return b.BetCredits * target / 100, nil
	}
	return 0, nil
}

func (c *Crash) TheoreticalRTP() float64 { return Edge }
