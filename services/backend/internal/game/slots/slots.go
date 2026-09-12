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
//
// A Config may opt into a scatter-triggered bonus round (Bonus): landing
// enough of a symbol anywhere awards free spins drawn from a boosted weight
// table, paid at a multiplier, with retrigger. The whole round is decided
// from the same fairness stream inside one Play and recorded in the payload;
// payout = base payout + bonus total.
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

// BonusSpec opts a scatter-mode game into a free spins round. Symbol names
// the trigger symbol (resolved against Symbols at New). TriggerSpins maps a
// trigger count to free spins awarded (largest key <= count wins, like the
// pay tables). Counts below KeepFlatFrom never flat-pay the trigger symbol —
// the round replaces those pays; counts from KeepFlatFrom up still flat-pay
// on top of triggering. Free spins draw from BonusWeights (positional over
// Symbols, must sum to 100), every free-spin win pays Multiplier x, and a
// free spin landing RetriggerCount+ of the symbol adds RetriggerSpins more.
type BonusSpec struct {
	Symbol         string        `json:"symbol"`
	TriggerSpins   map[int]int   `json:"triggerSpins"` // count -> free spins
	KeepFlatFrom   int           `json:"keepFlatFrom"` // trigger symbol flat-pays from this count up
	BonusWeights   []int64       `json:"bonusWeights"` // positional over Symbols, sums to 100
	Multiplier     int64         `json:"multiplier"`
	RetriggerCount int           `json:"retriggerCount"`
	RetriggerSpins int           `json:"retriggerSpins"`
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
	BetSteps []int64 // UI presets; any amount in [MinBet, MaxBet] is accepted
	MinBet   int64
	MaxBet   int64
	Bonus    *BonusSpec // scatter-mode only; nil = no bonus round
}

// Game is a configured slot machine.
type Game struct {
	cfg       Config
	bonusIdx  int  // resolved Bonus.Symbol index
	bonusKeys []int // sorted trigger counts, descending
}

func New(cfg Config) *Game {
	if err := validate(cfg); err != nil {
		panic(fmt.Sprintf("slots: invalid config %q: %v", cfg.ID, err))
	}
	g := &Game{cfg: cfg, bonusIdx: -1}
	if cfg.Bonus != nil {
		for i, s := range cfg.Symbols {
			if s.Name == cfg.Bonus.Symbol {
				g.bonusIdx = i
				break
			}
		}
		for k := range cfg.Bonus.TriggerSpins {
			g.bonusKeys = append(g.bonusKeys, k)
		}
		sort.Sort(sort.Reverse(sort.IntSlice(g.bonusKeys)))
	}
	return g
}

func (g *Game) ID() string { return g.cfg.ID }
func (g *Game) DisplayName() string {
	if g.cfg.Name != "" {
		return g.cfg.Name
	}
	return g.cfg.ID
}

func (g *Game) ValidateBet(credits int64) error {
	if credits < g.cfg.MinBet || credits > g.cfg.MaxBet {
		return fmt.Errorf("bet must be between %d and %d credits", g.cfg.MinBet, g.cfg.MaxBet)
	}
	return nil
}

// BetLimits exposes the accepted stake range for the games listing.
func (g *Game) BetLimits() (int64, int64) { return g.cfg.MinBet, g.cfg.MaxBet }

// Paytable exposes display data; the client renders it and never computes
// payouts from it.
func (g *Game) Paytable() any {
	lines := make([][]int, len(g.cfg.Lines))
	for i, l := range g.cfg.Lines {
		rows := make([]int, len(l))
		copy(rows, l)
		lines[i] = rows
	}
	table := map[string]any{
		"symbols":  g.cfg.Symbols,
		"betSteps": g.cfg.BetSteps,
		"reels":    g.cfg.Cols,
		"rows":     g.cfg.Rows,
		"paylines": len(g.cfg.Lines),
		"lines":    lines,
		"icons":    g.cfg.Icons,
		"mode":     g.mode(),
	}
	if b := g.cfg.Bonus; b != nil {
		boost := make([]map[string]any, len(g.cfg.Symbols))
		for i, s := range g.cfg.Symbols {
			boost[i] = map[string]any{"name": s.Name, "from": s.Weight, "to": b.BonusWeights[i]}
		}
		table["bonus"] = map[string]any{
			"symbol":         b.Symbol,
			"triggerSpins":   b.TriggerSpins,
			"multiplier":     b.Multiplier,
			"retriggerCount": b.RetriggerCount,
			"retriggerSpins": b.RetriggerSpins,
			"boost":          boost,
		}
	}
	return table
}

func (g *Game) mode() string {
	if len(g.cfg.Lines) == 0 {
		return "scatter"
	}
	return "lines"
}

// lookupPays returns the pay for a match of the given count: the pays entry
// with the largest key ≤ count.
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

// lookupSpins mirrors lookupPays for the bonus trigger table.
func (g *Game) lookupSpins(count int) (int, bool) {
	if g.cfg.Bonus == nil {
		return 0, false
	}
	for _, k := range g.bonusKeys {
		if count >= k {
			return g.cfg.Bonus.TriggerSpins[k], true
		}
	}
	return 0, false
}

// flatPayExcluded reports whether the bonus trigger symbol must skip its
// flat pay at this count (the round replaces those tiers).
func (g *Game) flatPayExcluded(symIdx, count int) bool {
	b := g.cfg.Bonus
	if b == nil || g.bonusIdx != symIdx || count < 3 {
		return false
	}
	return count < b.KeepFlatFrom
}

type payload struct {
	Grid    [][]int       `json:"grid"`
	Lines   []int         `json:"winningLines"`
	Scatter []ScatterWin  `json:"scatterWins,omitempty"`
	Bonus   *BonusPayload `json:"bonus,omitempty"`
}

// ScatterWin reports a scatter symbol that paid: symbol index, count found,
// and the pay multiplier applied.
type ScatterWin struct {
	Symbol int   `json:"symbol"`
	Count  int   `json:"count"`
	Pay    int64 `json:"pay"`
}

// BonusPayload records the entire pre-decided free spins round.
type BonusPayload struct {
	TriggerCount int         `json:"triggerCount"`
	SpinsAwarded int         `json:"spinsAwarded"`
	Multiplier   int64       `json:"multiplier"`
	Spins        []BonusSpin `json:"spins"`
	Total        int64       `json:"total"`
}

// BonusSpin is one free spin; Payout is post-multiplier.
type BonusSpin struct {
	Grid    [][]int      `json:"grid"`
	Lines   []int        `json:"winningLines,omitempty"`
	Scatter []ScatterWin `json:"scatterWins,omitempty"`
	Payout  int64        `json:"payout"`
	Retrigger bool       `json:"retrigger"`
}

// maxBonusSpins bounds a single bonus round. The retrigger tail is
// geometric, so sane configs never approach this; it only caps a
// pathological config.
const maxBonusSpins = 1000

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

func (g *Game) bonusWeights() []int64 {
	return append([]int64(nil), g.cfg.Bonus.BonusWeights...)
}

// fill draws a full grid row-major, one weighted pick per cell.
func fill(s *fair.Stream, weights []int64, grid [][]int) {
	for r := range grid {
		for c := range grid[r] {
			grid[r][c] = s.WeightedPick(weights)
		}
	}
}

func countSymbol(grid [][]int, symIdx int) int {
	n := 0
	for _, row := range grid {
		for _, sym := range row {
			if sym == symIdx {
				n++
			}
		}
	}
	return n
}

// evalGrid computes line/scatter wins for an existing grid. Shared by the
// live spin and the audit path (EvaluateGrid) so payload and payout can
// never drift.
func (g *Game) evalGrid(grid [][]int, betCredits int64) (int64, []int, []ScatterWin) {
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
		return payout, winning, scatterWins
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
		if g.flatPayExcluded(si, count) {
			continue
		}
		pay, ok := lookupPays(sym, count)
		if !ok {
			continue
		}
		scatterWins = append(scatterWins, ScatterWin{Symbol: si, Count: count, Pay: pay})
		payout += pay * betCredits
	}
	return payout, winning, scatterWins
}

// spin is the pure base-game core: draw the grid, evaluate it.
func (g *Game) spin(s *fair.Stream, betCredits int64) ([][]int, []int, []ScatterWin, int64) {
	grid := g.emptyGrid()
	fill(s, g.weights(), grid)
	payout, winning, scatterWins := g.evalGrid(grid, betCredits)
	return grid, winning, scatterWins, payout
}

// round plays the base spin plus any bonus round the base grid triggers,
// all from the same fairness stream. The full sequence is recorded in the
// returned payload; total = base payout + bonus total.
func (g *Game) round(s *fair.Stream, betCredits int64) (payload, int64) {
	grid, winning, scatter, basePayout := g.spin(s, betCredits)
	p := payload{Grid: grid, Lines: winning, Scatter: scatter}
	if g.cfg.Bonus == nil {
		return p, basePayout
	}

	count := countSymbol(grid, g.bonusIdx)
	spins, ok := g.lookupSpins(count)
	if !ok {
		return p, basePayout
	}
	b := g.cfg.Bonus
	bp := &BonusPayload{TriggerCount: count, SpinsAwarded: spins, Multiplier: b.Multiplier}
	weights := g.bonusWeights()
	played := 0
	for remaining := spins; remaining > 0; remaining-- {
		played++
		if played > maxBonusSpins {
			break // unreachable for sane configs; guards a pathological retrigger loop
		}
		fgrid := g.emptyGrid()
		fill(s, weights, fgrid)
		fpayout, fwinning, fscatter := g.evalGrid(fgrid, betCredits)
		bs := BonusSpin{
			Grid:    fgrid,
			Lines:   fwinning,
			Scatter: fscatter,
			Payout:  fpayout * b.Multiplier,
		}
		if b.RetriggerSpins > 0 && countSymbol(fgrid, g.bonusIdx) >= b.RetriggerCount {
			bs.Retrigger = true
			remaining += b.RetriggerSpins
		}
		bp.Spins = append(bp.Spins, bs)
		bp.Total += bs.Payout
	}
	p.Bonus = bp
	return p, basePayout + bp.Total
}

func (g *Game) Play(stream *fair.Stream, betCredits int64) (game.Outcome, error) {
	if err := g.ValidateBet(betCredits); err != nil {
		return game.Outcome{}, err
	}
	p, total := g.round(stream, betCredits)
	raw, err := json.Marshal(p)
	if err != nil {
		return game.Outcome{}, fmt.Errorf("marshal payload: %w", err)
	}
	return game.Outcome{PayoutCredits: total, Payload: raw}, nil
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
	payout, winning, scatterWins := g.evalGrid(grid, betCredits)
	return payout, winning, scatterWins, nil
}

// TheoreticalRTP computes the exact analytic RTP from the config.
//
// Line games: per line, P(exactly n of a kind from the left) is p^n·(1-p)
// for n < cols and p^cols for a full line.
//
// Scatter games: per symbol, P(exactly m anywhere) is the binomial
// C(cells, m)·p^m·(1-p)^(cells-m), and each exact count maps through the
// pay table. Counts that trigger the bonus round pay nothing flat; the
// exact bonus EV contribution (trigger probability · spins/(1-retrigger) ·
// multiplier · per-spin EV under the boosted weights — the retrigger
// extension is a geometric series over i.i.d. free spins) is added once.
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

	for si, sym := range g.cfg.Symbols {
		p := float64(sym.Weight) / float64(weightSumOf(g.cfg.Symbols))
		q := 1 - p
		for m := 3; m <= cells; m++ {
			if g.flatPayExcluded(si, m) {
				// These counts trigger the bonus round instead of paying.
				continue
			}
			if pay, ok := lookupPays(sym, m); ok {
				total += binom(cells, m) * powF(p, m) * powF(q, cells-m) * float64(pay)
			}
		}
	}
	return total + g.bonusRoundEV()
}

// bonusRoundEV is the exact RTP contribution of the bonus round in bet
// multipliers: Sigma over trigger counts of P(count) · spins/(1-r·k) ·
// multiplier · per-spin EV under the boosted weights, where each free spin
// spawns k more with probability r (branching process; validate rejects
// offspring means >= 1).
func (g *Game) bonusRoundEV() float64 {
	b := g.cfg.Bonus
	if b == nil {
		return 0
	}
	cells := g.cfg.Cols * g.cfg.Rows
	sum := weightSumOf(g.cfg.Symbols)
	bwSum := weightSumOf64(b.BonusWeights)

	// Per-spin EV under the boosted weights, same exclusion as evalGrid.
	var evPerSpin float64
	for si, sym := range g.cfg.Symbols {
		p := float64(b.BonusWeights[si]) / float64(bwSum)
		q := 1 - p
		for m := 3; m <= cells; m++ {
			if g.flatPayExcluded(si, m) {
				continue
			}
			if pay, ok := lookupPays(sym, m); ok {
				evPerSpin += binom(cells, m) * powF(p, m) * powF(q, cells-m) * float64(pay)
			}
		}
	}

	// Retrigger probability: r+ of the trigger symbol in a free-spin grid.
	var retrigProb float64
	pb := float64(b.BonusWeights[g.bonusIdx]) / float64(bwSum)
	for m := b.RetriggerCount; m <= cells; m++ {
		retrigProb += binom(cells, m) * powF(pb, m) * powF(1-pb, cells-m)
	}
	// Each spin spawns RetriggerSpins more with probability retrigProb, so
	// expected spins follow a branching process: s/(1 - retrigProb·spins).
	// validate() rejects configs where the offspring mean reaches 1.
	extension := 1.0
	if b.RetriggerSpins > 0 {
		extension = 1 / (1 - retrigProb*float64(b.RetriggerSpins))
	}

	// Weighted by trigger probability over exact base-grid counts.
	var ev float64
	pTrigger := float64(g.cfg.Symbols[g.bonusIdx].Weight) / float64(sum)
	qTrigger := 1 - pTrigger
	for m := 3; m <= cells; m++ {
		spins, ok := g.lookupSpins(m)
		if !ok {
			continue
		}
		prob := binom(cells, m) * powF(pTrigger, m) * powF(qTrigger, cells-m)
		ev += prob * float64(spins) * extension * float64(b.Multiplier) * evPerSpin
	}
	return ev
}

// retriggerProb is P(RetriggerCount+ of the bonus symbol in one free-spin
// grid) under the boosted weights.
func retriggerProb(cfg Config, b *BonusSpec) float64 {
	cells := cfg.Cols * cfg.Rows
	var bwSum int64
	for _, w := range b.BonusWeights {
		bwSum += w
	}
	idx := -1
	for i, s := range cfg.Symbols {
		if s.Name == b.Symbol {
			idx = i
			break
		}
	}
	pb := float64(b.BonusWeights[idx]) / float64(bwSum)
	var r float64
	for m := b.RetriggerCount; m <= cells; m++ {
		r += binom(cells, m) * powF(pb, m) * powF(1-pb, cells-m)
	}
	return r
}

func weightSumOf(symbols []SymbolCfg) int64 {
	var sum int64
	for _, s := range symbols {
		sum += s.Weight
	}
	return sum
}

func weightSumOf64(weights []int64) int64 {
	var sum int64
	for _, w := range weights {
		sum += w
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
