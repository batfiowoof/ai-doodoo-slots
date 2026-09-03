// Package slots implements config-driven slot machines. A game is a Config:
// grid size, weighted symbols, paylines (or scatter-pays mode), and per-count
// pay tables. The classic 5x3 game ships alongside a 3x3 fruit game and a
// 4x4 scatter game; all share the fairness stream, wallet, and API surface.
//
// Grids are filled row-major from the fairness stream, one weighted draw per
// cell. Line games pay for matches counting from the LEFT reel of a payline
// (3+ by count tier). Scatter games pay per symbol for N-or-more anywhere on
// the grid. TheoreticalRTP is exact, and the RTP simulations gate every game
// against it.
package slots

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
)

// SymbolCfg is one reel symbol: weight and pays by match count.
type SymbolCfg struct {
	Name   string        `json:"name"`
	Weight int64         `json:"weight"`
	Pays   map[int]int64 `json:"pays"` // match count -> bet multiplier
}

// Config fully describes a slot game.
type Config struct {
	ID       string
	Name     string
	Cols     int
	Rows     int
	Symbols  []SymbolCfg
	Lines    [][]int // row index per column; empty = scatter-pays mode
	Icons    []string
	BetSteps []int64
}

// Game is a configured slot machine.
type Game struct {
	cfg Config
}

func New(cfg Config) *Game {
	if err := validate(cfg); err != nil {
		panic(fmt.Sprintf("slots: invalid config %q: %v", cfg.ID, err))
	}
	return &Game{cfg: cfg}
}

func (g *Game) ID() string { return g.cfg.ID }
func (g *Game) DisplayName() string {
	if g.cfg.Name != "" {
		return g.cfg.Name
	}
	return g.cfg.ID
}

func (g *Game) ValidateBet(credits int64) error {
	for _, step := range g.cfg.BetSteps {
		if credits == step {
			return nil
		}
	}
	return fmt.Errorf("bet must be one of %v credits", g.cfg.BetSteps)
}

// Paytable exposes display data; the client renders it and never computes
// payouts from it.
func (g *Game) Paytable() any {
	lines := make([][]int, len(g.cfg.Lines))
	for i, l := range g.cfg.Lines {
		rows := make([]int, len(l))
		copy(rows, l)
		lines[i] = rows
	}
	return map[string]any{
		"symbols":  g.cfg.Symbols,
		"betSteps": g.cfg.BetSteps,
		"reels":    g.cfg.Cols,
		"rows":     g.cfg.Rows,
		"paylines": len(g.cfg.Lines),
		"lines":    lines,
		"icons":    g.cfg.Icons,
		"mode":     g.mode(),
	}
}

func (g *Game) mode() string {
	if len(g.cfg.Lines) == 0 {
		return "scatter"
	}
	return "lines"
}

// lookupPays returns the pay for a match of the given count: the pays entry
// with the largest key â‰¤ count.
func lookupPays(sym SymbolCfg, count int) (int64, bool) {
	keys := make([]int, 0, len(sym.Pays))
	for k := range sym.Pays {
		keys = append(keys, k)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(keys)))
	for _, k := range keys {
		if count >= k {
			return sym.Pays[k], true
		}
	}
	return 0, false
}

type payload struct {
	Grid    [][]int      `json:"grid"`
	Lines   []int        `json:"winningLines"`
	Scatter []ScatterWin `json:"scatterWins,omitempty"`
}

// ScatterWin reports a scatter symbol that paid: symbol index, count found,
// and the pay multiplier applied.
type ScatterWin struct {
	Symbol int   `json:"symbol"`
	Count  int   `json:"count"`
	Pay    int64 `json:"pay"`
}

func (g *Game) emptyGrid() [][]int {
	grid := make([][]int, g.cfg.Rows)
	for r := range grid {
		grid[r] = make([]int, g.cfg.Cols)
	}
	return grid
}

func (g *Game) weights() []int64 {
	w := make([]int64, len(g.cfg.Symbols))
	for i, s := range g.cfg.Symbols {
		w[i] = s.Weight
	}
	return w
}

// spin is the pure evaluation core; Play wraps it in JSON.
func (g *Game) spin(s *fair.Stream, betCredits int64) ([][]int, []int, []ScatterWin, int64) {
	grid := g.emptyGrid()
	weights := g.weights()
	for r := 0; r < g.cfg.Rows; r++ {
		for c := 0; c < g.cfg.Cols; c++ {
			grid[r][c] = s.WeightedPick(weights)
		}
	}

	var winning []int
	var scatterWins []ScatterWin
	var payout int64

	if g.mode() == "lines" {
		for i, line := range g.cfg.Lines {
			first := grid[line[0]][0]
			count := 1
			for c := 1; c < g.cfg.Cols && c < len(line); c++ {
				if grid[line[c]][c] != first {
					break
				}
				count++
			}
			if count < 3 {
				continue
			}
			pay, ok := lookupPays(g.cfg.Symbols[first], count)
			if !ok {
				continue
			}
			winning = append(winning, i)
			payout += pay * betCredits
		}
		return grid, winning, scatterWins, payout
	}

	// Scatter mode: count occurrences anywhere.
	counts := make(map[int]int)
	for _, row := range grid {
		for _, sym := range row {
			counts[sym]++
		}
	}
	for si, sym := range g.cfg.Symbols {
		count := counts[si]
		if count < 3 {
			continue
		}
		pay, ok := lookupPays(sym, count)
		if !ok {
			continue
		}
		scatterWins = append(scatterWins, ScatterWin{Symbol: si, Count: count, Pay: pay})
		payout += pay * betCredits
	}
	return grid, winning, scatterWins, payout
}

func (g *Game) Play(stream *fair.Stream, betCredits int64) (game.Outcome, error) {
	if err := g.ValidateBet(betCredits); err != nil {
		return game.Outcome{}, err
	}
	grid, winning, scatter, payout := g.spin(stream, betCredits)
	raw, err := json.Marshal(payload{Grid: grid, Lines: winning, Scatter: scatter})
	if err != nil {
		return game.Outcome{}, fmt.Errorf("marshal payload: %w", err)
	}
	return game.Outcome{PayoutCredits: payout, Payload: raw}, nil
}

// ErrUnknownSymbol guards payload decoding.
var ErrUnknownSymbol = errors.New("symbol index out of range")

// EvaluateGrid recomputes the payout for a grid â€” used by tests and any
// auditing path that wants to replay an outcome from its payload.
func (g *Game) EvaluateGrid(grid [][]int, betCredits int64) (int64, []int, []ScatterWin, error) {
	if len(grid) != g.cfg.Rows {
		return 0, nil, nil, ErrUnknownSymbol
	}
	for _, row := range grid {
		if len(row) != g.cfg.Cols {
			return 0, nil, nil, ErrUnknownSymbol
		}
		for _, sym := range row {
			if sym < 0 || sym >= len(g.cfg.Symbols) {
				return 0, nil, nil, ErrUnknownSymbol
			}
		}
	}

	var winning []int
	var scatterWins []ScatterWin
	var payout int64

	if g.mode() == "lines" {
		for i, line := range g.cfg.Lines {
			first := grid[line[0]][0]
			count := 1
			for c := 1; c < g.cfg.Cols && c < len(line); c++ {
				if grid[line[c]][c] != first {
					break
				}
				count++
			}
			if count < 3 {
				continue
			}
			pay, ok := lookupPays(g.cfg.Symbols[first], count)
			if !ok {
				continue
			}
			winning = append(winning, i)
			payout += pay * betCredits
		}
		return payout, winning, scatterWins, nil
	}

	counts := make(map[int]int)
	for _, row := range grid {
		for _, sym := range row {
			counts[sym]++
		}
	}
	for si, sym := range g.cfg.Symbols {
		count := counts[si]
		if count < 3 {
			continue
		}
		pay, ok := lookupPays(sym, count)
		if !ok {
			continue
		}
		scatterWins = append(scatterWins, ScatterWin{Symbol: si, Count: count, Pay: pay})
		payout += pay * betCredits
	}
	return payout, winning, scatterWins, nil
}

// TheoreticalRTP computes the exact analytic RTP from the config.
//
// Line games: per line, P(exactly n of a kind from the left) is p^nÂ·(1-p)
// for n < cols and p^cols for a full line.
//
// Scatter games: per symbol, P(exactly m anywhere) is the binomial
// C(cells, m)Â·p^mÂ·(1-p)^(cells-m), and each exact count maps through the
// pay table.
func (g *Game) TheoreticalRTP() float64 {
	cells := g.cfg.Cols * g.cfg.Rows
	var total float64

	if g.mode() == "lines" {
		for range g.cfg.Lines {
			for _, sym := range g.cfg.Symbols {
				p := float64(sym.Weight) / float64(weightSumOf(g.cfg.Symbols))
				for n := 3; n <= g.cfg.Cols; n++ {
					prob := powF(p, n)
					if n < g.cfg.Cols {
						prob *= 1 - p
					}
					if pay, ok := lookupPays(sym, n); ok {
						total += prob * float64(pay)
					}
				}
			}
		}
		return total
	}

	for _, sym := range g.cfg.Symbols {
		p := float64(sym.Weight) / float64(weightSumOf(g.cfg.Symbols))
		q := 1 - p
		for m := 3; m <= cells; m++ {
			prob := binom(cells, m) * powF(p, m) * powF(q, cells-m)
			if pay, ok := lookupPays(sym, m); ok {
				total += prob * float64(pay)
			}
		}
	}
	return total
}

func weightSumOf(symbols []SymbolCfg) int64 {
	var sum int64
	for _, s := range symbols {
		sum += s.Weight
	}
	return sum
}

func powF(base float64, exp int) float64 {
	result := 1.0
	for i := 0; i < exp; i++ {
		result *= base
	}
	return result
}

// binom computes C(n, k) exactly.
func binom(n, k int) float64 {
	if k < 0 || k > n {
		return 0
	}
	result := 1.0
	for i := 0; i < k; i++ {
		result = result * float64(n-i) / float64(i+1)
	}
	return result
}
