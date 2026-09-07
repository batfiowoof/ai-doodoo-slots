// Package plinko implements the instant peg-board drop. The player chooses
// a row count and risk; the ball takes one fair coin step per row and lands
// in a multiplier bucket. One stream float per row decides the direction,
// so the full path replays from the seed triple.
package plinko

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
)

// GameID is the registry identifier.
const GameID = "plinko"

const (
	MinBet = 1
	MaxBet = 10000
)

// RowsOptions are the supported board heights.
var RowsOptions = [3]int{8, 12, 16}

// RiskOptions are the supported risk levels.
var RiskOptions = [3]string{"low", "medium", "high"}

// baseTables[risk][rows] are the reference bucket multipliers (left to
// right), symmetric around the center.
var baseTables = map[string]map[int][]float64{
	"low": {
		8:  {5.6, 2.1, 1.1, 1, 0.5, 1, 1.1, 2.1, 5.6},
		12: {10, 3, 1.6, 1.4, 1.1, 1, 0.5, 1, 1.1, 1.4, 1.6, 3, 10},
		16: {16, 9, 2, 1.4, 1.4, 1.2, 1.1, 1, 0.5, 1, 1.1, 1.2, 1.4, 1.4, 2, 9, 16},
	},
	"medium": {
		8:  {13, 3, 1.3, 0.7, 0.4, 0.7, 1.3, 3, 13},
		12: {33, 11, 4, 2, 1.1, 0.6, 0.3, 0.6, 1.1, 2, 4, 11, 33},
		16: {110, 41, 10, 5, 3, 1.5, 1, 0.5, 0.3, 0.5, 1, 1.5, 3, 5, 10, 41, 110},
	},
	"high": {
		8:  {29, 4, 1.5, 0.3, 0.2, 0.3, 1.5, 4, 29},
		12: {170, 24, 8.1, 2, 0.7, 0.2, 0.2, 0.2, 0.7, 2, 8.1, 24, 170},
		16: {1000, 130, 26, 9, 4, 2, 0.2, 0.2, 0.2, 0.2, 0.2, 2, 4, 9, 26, 130, 1000},
	},
}

// Table returns the bucket multipliers for a board (defensive copy).
func Table(risk string, rows int) []float64 {
	base := baseTables[risk][rows]
	out := make([]float64, len(base))
	copy(out, base)
	return out
}

// exactRTP is the binomial-weighted return of a table: every path has
// probability C(rows, k)/2^rows of landing in bucket k.
func exactRTP(risk string, rows int) float64 {
	table := baseTables[risk][rows]
	n := float64(int(1) << uint(rows))
	var total float64
	for k := range table {
		total += binom(rows, k) / n * table[k]
	}
	return total
}

func binom(n, k int) float64 {
	if k < 0 || k > n {
		return 0
	}
	r := 1.0
	for i := 0; i < k; i++ {
		r = r * float64(n-i) / float64(i+1)
	}
	return r
}

// Params are the player's board choices.
type Params struct {
	Rows int    `json:"rows"`
	Risk string `json:"risk"`
}

func (p Params) validate() error {
	validRows := false
	for _, r := range RowsOptions {
		if p.Rows == r {
			validRows = true
		}
	}
	if !validRows {
		return fmt.Errorf("rows must be one of %v", RowsOptions)
	}
	if _, ok := baseTables[p.Risk]; !ok {
		return fmt.Errorf("risk must be one of %v", RiskOptions)
	}
	return nil
}

// Game is the plinko engine.
type Game struct{}

func New() *Game { return &Game{} }

func (g *Game) ID() string          { return GameID }
func (g *Game) DisplayName() string { return "Plinko" }

// TheoreticalRTP is the worst case across all boards; each board's exact
// value is within a hair of 0.99 (see TestTableRTP).
func (g *Game) TheoreticalRTP() float64 {
	worst := 1.0
	for _, risk := range RiskOptions {
		for _, rows := range RowsOptions {
			if r := exactRTP(risk, rows); r < worst {
				worst = r
			}
		}
	}
	return worst
}

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

// PlayWithParams drops one ball: rows coin flips from the stream, bucket =
// number of rightward steps, payout = floor(bet × multiplier).
func (g *Game) PlayWithParams(s *fair.Stream, betCredits int64, params json.RawMessage) (game.Outcome, error) {
	if err := g.ValidateBet(betCredits); err != nil {
		return game.Outcome{}, err
	}
	var p Params
	if len(params) == 0 {
		return game.Outcome{}, fmt.Errorf("plinko requires params")
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return game.Outcome{}, fmt.Errorf("decode params: %w", err)
	}
	if err := p.validate(); err != nil {
		return game.Outcome{}, err
	}

	table := baseTables[p.Risk][p.Rows]
	path := make([]bool, p.Rows) // true = right
	bucket := 0
	for i := 0; i < p.Rows; i++ {
		path[i] = s.Float() < 0.5
		if path[i] {
			bucket++
		}
	}
	mult := table[bucket]
	payout := int64(math.Floor(float64(betCredits) * mult))

	payload, err := json.Marshal(map[string]any{
		"rows":       p.Rows,
		"risk":       p.Risk,
		"bucket":     bucket,
		"path":       path,
		"multiplier": mult,
		"profit":     payout > betCredits,
	})
	if err != nil {
		return game.Outcome{}, fmt.Errorf("marshal payload: %w", err)
	}
	return game.Outcome{PayoutCredits: payout, Payload: payload}, nil
}
