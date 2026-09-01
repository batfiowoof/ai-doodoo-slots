// Package slots implements the 3x3, five-payline slot machine.
//
// The grid is filled row-major from the fairness stream, one weighted draw
// per cell (row 0 left to right, then row 1, then row 2). A win is three
// matching symbols on a payline; multiple winning lines stack. Payout is
// symbol pay x bet per winning line.
package slots

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
)

const (
	id         = "slots"
	cols       = 3
	rows       = 3
	lineCount  = 5
	weightSum  = 100 // weights must total this; asserted in tests
)

// BetSteps are the only accepted bet sizes, server-enforced.
var BetSteps = []int64{5, 10, 25, 50, 100}

// Symbol is one reel symbol. Weights sum to 100, ordered common to rare.
// These figures are the shipped economy; do not tune.
type Symbol struct {
	Name   string `json:"name"`
	Weight int64  `json:"weight"`
	Pay    int64  `json:"pay"`
}

var symbols = []Symbol{
	{Name: "plum", Weight: 22, Pay: 4},
	{Name: "cherry", Weight: 20, Pay: 5},
	{Name: "bell", Weight: 16, Pay: 8},
	{Name: "clover", Weight: 14, Pay: 11},
	{Name: "star", Weight: 11, Pay: 16},
	{Name: "diamond", Weight: 8, Pay: 26},
	{Name: "seven", Weight: 6, Pay: 52},
	{Name: "crown", Weight: 3, Pay: 200},
}

// Weights returns the weight table, ordered common to rare, for the
// fairness stream's WeightedPick.
func Weights() []int64 {
	w := make([]int64, len(symbols))
	for i, s := range symbols {
		w[i] = s.Weight
	}
	return w
}

// cell is a grid position: column x, row y, zero-based.
type cell struct{ x, y int }

// paylines: three horizontals plus both diagonals.
var paylines = [lineCount][3]cell{
	{{0, 0}, {1, 0}, {2, 0}}, // top row
	{{0, 1}, {1, 1}, {2, 1}}, // middle row
	{{0, 2}, {1, 2}, {2, 2}}, // bottom row
	{{0, 0}, {1, 1}, {2, 2}}, // diagonal down
	{{0, 2}, {1, 1}, {2, 0}}, // diagonal up
}

// Slots is the slot machine engine.
type Slots struct{}

func New() *Slots { return &Slots{} }

func (s *Slots) ID() string { return id }

// DisplayName is the human-facing name shown in the lobby and paytable.
func (s *Slots) DisplayName() string { return "Slots" }

// Paytable exposes display data; the client renders it and never computes
// payouts from it.
func (s *Slots) Paytable() any {
	return map[string]any{
		"symbols":  symbols,
		"betSteps": BetSteps,
		"reels":    cols,
		"rows":     rows,
		"paylines": lineCount,
	}
}

func (s *Slots) ValidateBet(credits int64) error {
	for _, step := range BetSteps {
		if credits == step {
			return nil
		}
	}
	return fmt.Errorf("bet must be one of %v credits", BetSteps)
}

// TheoreticalRTP computes the analytic RTP from the tables: five independent
// lines, each paying pay_k with probability (w_k/100)^3. Expectation is
// linear, so per-line correlation does not change the figure.
func (s *Slots) TheoreticalRTP() float64 {
	var perLine float64
	for _, sym := range symbols {
		p := float64(sym.Weight) / weightSum
		perLine += p * p * p * float64(sym.Pay)
	}
	return float64(lineCount) * perLine
}

type payload struct {
	// Grid is [row][col] of symbol indices, ordered common to rare.
	Grid         [rows][cols]int `json:"grid"`
	WinningLines []int           `json:"winningLines"`
}

// spin is the pure evaluation core; Play wraps it in JSON.
func spin(s *fair.Stream, betCredits int64) ([rows][cols]int, []int, int64) {
	var grid [rows][cols]int
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			grid[y][x] = s.WeightedPick(Weights())
		}
	}

	var winning []int
	var payout int64
	for i, line := range paylines {
		first := grid[line[0].y][line[0].x]
		if grid[line[1].y][line[1].x] == first && grid[line[2].y][line[2].x] == first {
			winning = append(winning, i)
			payout += symbols[first].Pay * betCredits
		}
	}
	return grid, winning, payout
}

func (s *Slots) Play(stream *fair.Stream, betCredits int64) (game.Outcome, error) {
	if err := s.ValidateBet(betCredits); err != nil {
		return game.Outcome{}, err
	}
	grid, winning, payout := spin(stream, betCredits)
	raw, err := json.Marshal(payload{Grid: grid, WinningLines: winning})
	if err != nil {
		return game.Outcome{}, fmt.Errorf("marshal payload: %w", err)
	}
	return game.Outcome{PayoutCredits: payout, Payload: raw}, nil
}

// ErrUnknownSymbol guards payload decoding.
var ErrUnknownSymbol = errors.New("symbol index out of range")

// EvaluateGrid recomputes the payout for a grid — used by tests and any
// auditing path that wants to replay an outcome from its payload.
func EvaluateGrid(grid [rows][cols]int, betCredits int64) (int64, []int, error) {
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			if grid[y][x] < 0 || grid[y][x] >= len(symbols) {
				return 0, nil, ErrUnknownSymbol
			}
		}
	}
	var winning []int
	var payout int64
	for i, line := range paylines {
		first := grid[line[0].y][line[0].x]
		if grid[line[1].y][line[1].x] == first && grid[line[2].y][line[2].x] == first {
			winning = append(winning, i)
			payout += symbols[first].Pay * betCredits
		}
	}
	return payout, winning, nil
}
