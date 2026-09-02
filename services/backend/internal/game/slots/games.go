package slots

import "fmt"

// Classic is the shipped 5x3 nine-line game. Tables are the shipped
// economy: analytic RTP ~0.9817, verified by the 10M-spin sim.
func Classic() *Game {
	pays := func(p3, p4, p5 int64) map[int]int64 {
		return map[int]int64{3: p3, 4: p4, 5: p5}
	}
	return New(Config{
		ID:   "slots",
		Name: "Classic",
		Cols: 5, Rows: 3,
		Lines: [][]int{
			{1, 1, 1, 1, 1},
			{0, 0, 0, 0, 0},
			{2, 2, 2, 2, 2},
			{0, 1, 2, 1, 0},
			{2, 1, 0, 1, 2},
			{1, 0, 1, 0, 1},
			{1, 2, 1, 2, 1},
			{0, 0, 1, 0, 0},
			{2, 2, 1, 2, 2},
		},
		Symbols: []SymbolCfg{
			{Name: "plum", Weight: 22, Pays: pays(1, 3, 7)},
			{Name: "cherry", Weight: 20, Pays: pays(2, 6, 14)},
			{Name: "bell", Weight: 16, Pays: pays(4, 10, 28)},
			{Name: "clover", Weight: 14, Pays: pays(5, 15, 35)},
			{Name: "star", Weight: 11, Pays: pays(6, 18, 42)},
			{Name: "diamond", Weight: 8, Pays: pays(12, 36, 84)},
			{Name: "seven", Weight: 6, Pays: pays(24, 72, 168)},
			{Name: "crown", Weight: 3, Pays: pays(90, 270, 630)},
		},
		Icons:    []string{"plum", "cherries", "bell", "clover", "star", "diamond-blue", "seven", "crown"},
		BetSteps: []int64{5, 10, 25, 50, 100},
	})
}

// FruitSalad is the 3x3 five-line fruit machine. Three of a kind on a line
// is the only win (a 3x3 line has exactly three cells).
func FruitSalad() *Game {
	pay := func(p3 int64) map[int]int64 { return map[int]int64{3: p3} }
	return New(Config{
		ID:   "fruits",
		Name: "Fruit Salad",
		Cols: 3, Rows: 3,
		Lines: [][]int{
			{1, 1, 1},
			{0, 0, 0},
			{2, 2, 2},
			{0, 1, 2},
			{2, 1, 0},
		},
		Symbols: []SymbolCfg{
			{Name: "lemon", Weight: 30, Pays: pay(2)},
			{Name: "orange", Weight: 25, Pays: pay(4)},
			{Name: "watermelon", Weight: 18, Pays: pay(5)},
			{Name: "grapes", Weight: 13, Pays: pay(10)},
			{Name: "strawberry", Weight: 9, Pays: pay(18)},
			{Name: "blueberries", Weight: 5, Pays: pay(75)},
		},
		Icons:    []string{"lemon", "orange", "watermelon", "grapes", "strawberry", "blueberries"},
		BetSteps: []int64{5, 10, 25, 50, 100},
	})
}

// Treasure is the 4x4 scatter game: no paylines at all. Bar, coin stacks,
// money bags, and bonus symbols pay anywhere on the grid when enough land.
func Treasure() *Game {
	return New(Config{
		ID:   "treasure",
		Name: "Treasure Scatter",
		Cols: 4, Rows: 4,
		Lines: nil, // scatter mode
		Symbols: []SymbolCfg{
			{Name: "spade", Weight: 28, Pays: nil},
			{Name: "club", Weight: 22, Pays: nil},
			{Name: "heart-card", Weight: 16, Pays: nil},
			{Name: "bar", Weight: 14, Pays: map[int]int64{4: 1, 5: 2, 6: 3}},
			{Name: "coin-stack", Weight: 10, Pays: map[int]int64{3: 1, 4: 2}},
			{Name: "money-bag", Weight: 7, Pays: map[int]int64{3: 1, 4: 4, 5: 20, 6: 100, 7: 500}},
			{Name: "bonus", Weight: 3, Pays: map[int]int64{3: 10, 4: 40, 5: 200, 6: 1000, 7: 5000}},
		},
		Icons:    []string{"spade", "club", "heart-card", "bar", "coin-stack", "money-bag", "bonus"},
		BetSteps: []int64{5, 10, 25, 50, 100},
	})
}

// validate asserts the structural invariants every config must hold.
func validate(cfg Config) error {
	var sum int64
	for _, s := range cfg.Symbols {
		if s.Weight <= 0 {
			return fmt.Errorf("symbol %q has non-positive weight", s.Name)
		}
		sum += s.Weight
	}
	if sum != 100 {
		return fmt.Errorf("weights sum to %d, want 100", sum)
	}
	if cfg.Cols < 3 || cfg.Rows < 3 {
		return fmt.Errorf("grid must be at least 3x3")
	}
	if len(cfg.Icons) != len(cfg.Symbols) {
		return fmt.Errorf("icons count %d != symbols count %d", len(cfg.Icons), len(cfg.Symbols))
	}
	for i, line := range cfg.Lines {
		if len(line) != cfg.Cols {
			return fmt.Errorf("line %d has %d cells, want %d", i, len(line), cfg.Cols)
		}
		for _, r := range line {
			if r < 0 || r >= cfg.Rows {
				return fmt.Errorf("line %d has out-of-range row %d", i, r)
			}
		}
	}
	for _, s := range cfg.Symbols {
		for count, pay := range s.Pays {
			if count < 3 || count > cfg.Cols*cfg.Rows {
				return fmt.Errorf("symbol %q pay count %d out of range", s.Name, count)
			}
			if pay <= 0 {
				return fmt.Errorf("symbol %q pay %d is non-positive", s.Name, pay)
			}
		}
	}
	return nil
}
