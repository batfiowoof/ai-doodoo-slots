// Package slots implements the 5x3, nine-payline slot machine.
//
// The grid is filled row-major from the fairness stream, one weighted draw
// per cell (row 0 left to right, then row 1, then row 2). A win is three or
// more matching symbols counting from the LEFT reel of a payline; 3, 4, and
// 5 of a kind each have their own pay (a multiple of the total bet), and
// multiple winning lines stack. With the shipped tables the analytic RTP is
// ~0.985.
package slots

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
)

const (
	id        = "slots"
	cols      = 5
	rows      = 3
	lineCount = 9
	weightSum = 100 // weights must total this; asserted in tests
)

// BetSteps are the only accepted bet sizes, server-enforced.
var BetSteps = []int64{5, 10, 25, 50, 100}

// Symbol is one reel symbol. Weights sum to 100, ordered common to rare.
// Pay3/Pay4/Pay5 are multiples of the TOTAL bet for 3, 4, and 5 of a kind
// counting from the left. These figures are the shipped economy; change
// them together with TheoreticalRTP and the RTP sim.
type Symbol struct {
	Name   string `json:"name"`
	Weight int64  `json:"weight"`
	Pay3   int64  `json:"pay3"`
	Pay4   int64  `json:"pay4"`
	Pay5   int64  `json:"pay5"`
}

var symbols = []Symbol{
	{Name: "plum", Weight: 22, Pay3: 1, Pay4: 3, Pay5: 7},
	{Name: "cherry", Weight: 20, Pay3: 2, Pay4: 6, Pay5: 14},
	{Name: "bell", Weight: 16, Pay3: 4, Pay4: 10, Pay5: 28},
	{Name: "clover", Weight: 14, Pay3: 5, Pay4: 15, Pay5: 35},
	{Name: "star", Weight: 11, Pay3: 6, Pay4: 18, Pay5: 42},
	{Name: "diamond", Weight: 8, Pay3: 12, Pay4: 36, Pay5: 84},
	{Name: "seven", Weight: 6, Pay3: 24, Pay4: 72, Pay5: 168},
	{Name: "crown", Weight: 3, Pay3: 90, Pay4: 270, Pay5: 630},
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

// paylines: three rows, V and A diagonals, two zigzags, two bumps.
var paylines = [lineCount][cols]cell{
	{{0, 1}, {1, 1}, {2, 1}, {3, 1}, {4, 1}}, // middle row
	{{0, 0}, {1, 0}, {2, 0}, {3, 0}, {4, 0}}, // top row
	{{0, 2}, {1, 2}, {2, 2}, {3, 2}, {4, 2}}, // bottom row
	{{0, 0}, {1, 1}, {2, 2}, {3, 1}, {4, 0}}, // V
	{{0, 2}, {1, 1}, {2, 0}, {3, 1}, {4, 2}}, // A
	{{0, 1}, {1, 0}, {2, 1}, {3, 0}, {4, 1}}, // zigzag upper
	{{0, 1}, {1, 2}, {2, 1}, {3, 2}, {4, 1}}, // zigzag lower
	{{0, 0}, {1, 0}, {2, 1}, {3, 0}, {4, 0}}, // top bump
	{{0, 2}, {1, 2}, {2, 1}, {3, 2}, {4, 2}}, // bottom bump
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
	lines := make([][]int, lineCount)
	for i, l := range paylines {
		lr := make([]int, cols)
		for c, cl := range l {
			lr[c] = cl.y
		}
		lines[i] = lr
	}
	return map[string]any{
		"symbols":  symbols,
		"betSteps": BetSteps,
		"reels":    cols,
		"rows":     rows,
		"paylines": lineCount,
		"lines":    lines,
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

// TheoreticalRTP computes the analytic RTP from the tables: nine lines;
// per line, P(exactly 3 from left) = p^3(1-p), etc.
func (s *Slots) TheoreticalRTP() float64 {
	var perLine float64
	for _, sym := range symbols {
		p := float64(sym.Weight) / weightSum
		q := 1 - p
		perLine += p*p*p*q*float64(sym.Pay3) +
			p*p*p*p*q*float64(sym.Pay4) +
			p*p*p*p*p*float64(sym.Pay5)
	}
	return float64(lineCount) * perLine
}

type payload struct {
	// Grid is [row][col] of symbol indices, ordered common to rare.
	Grid         [rows][cols]int `json:"grid"`
	WinningLines []int           `json:"winningLines"`
}

// lineWin evaluates one payline: count consecutive matching symbols from
// the left; 3+ pays pay3/pay4/pay5 of the matched symbol.
func lineWin(grid [rows][cols]int, line [cols]cell) (int64, bool) {
	first := grid[line[0].y][line[0].x]
	count := 1
	for _, cl := range line[1:] {
		if grid[cl.y][cl.x] != first {
			break
		}
		count++
	}
	if count < 3 {
		return 0, false
	}
	sym := symbols[first]
	switch count {
	case 3:
		return sym.Pay3, true
	case 4:
		return sym.Pay4, true
	default:
		return sym.Pay5, true
	}
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
		if pay, win := lineWin(grid, line); win {
			winning = append(winning, i)
			payout += pay * betCredits
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
		if pay, win := lineWin(grid, line); win {
			winning = append(winning, i)
			payout += pay * betCredits
		}
	}
	return payout, winning, nil
}
