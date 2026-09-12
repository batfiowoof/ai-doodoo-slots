package slots

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"math/bits"
	"testing"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
)

// testSpin drives a game's spin deterministically from an index.
func testSpin(g *Game, i int64, bet int64) ([][]int, []int, []ScatterWin, int64) {
	seed := sha256.Sum256([]byte{byte(i), byte(i >> 8), byte(i >> 16), byte(i >> 24)})
	stream := fair.NewPersonalStream(seed[:], "test", i)
	return g.spin(stream, bet)
}

func TestConfigValidation(t *testing.T) {
	for _, g := range []*Game{Classic(), FruitSalad(), Treasure()} {
		if err := validate(g.cfg); err != nil {
			t.Fatalf("config %q invalid: %v", g.cfg.ID, err)
		}
	}
	// Weight sum must be 100.
	bad := Classic()
	bad.cfg.Symbols[0].Weight = 23
	if err := validate(bad.cfg); err == nil {
		t.Fatal("weight sum 101 accepted")
	}
}

func TestClassicTablesUnchanged(t *testing.T) {
	g := Classic()
	if g.cfg.Cols != 5 || g.cfg.Rows != 3 || len(g.cfg.Lines) != 9 {
		t.Fatalf("classic shape changed: %dx%d, %d lines", g.cfg.Cols, g.cfg.Rows, len(g.cfg.Lines))
	}
}

func TestValidateBetSteps(t *testing.T) {
	for _, g := range []*Game{Classic(), FruitSalad(), Treasure()} {
		for _, step := range g.cfg.BetSteps {
			if err := g.ValidateBet(step); err != nil {
				t.Fatalf("%s: bet %d rejected: %v", g.cfg.ID, step, err)
			}
		}
		// Any amount inside [MinBet, MaxBet] plays — steps are UI presets.
		if err := g.ValidateBet(7); err != nil {
			t.Fatalf("%s: bet 7 rejected: %v", g.cfg.ID, err)
		}
		if err := g.ValidateBet(g.cfg.MaxBet); err != nil {
			t.Fatalf("%s: max bet %d rejected: %v", g.cfg.ID, g.cfg.MaxBet, err)
		}
		for _, bad := range []int64{0, -5, 10001, 1000000} {
			if err := g.ValidateBet(bad); err == nil {
				t.Fatalf("%s: bet %d accepted", g.cfg.ID, bad)
			}
		}
	}
}

func TestSpinDeterministic(t *testing.T) {
	for _, g := range []*Game{Classic(), FruitSalad(), Treasure()} {
		seed := bytes.Repeat([]byte{0x5A}, fair.SeedSize)
		a := fair.NewPersonalStream(seed, "client", 1)
		b := fair.NewPersonalStream(seed, "client", 1)
		ga, wa, sa, pa := g.spin(a, 10)
		gb, wb, sb, pb := g.spin(b, 10)
		if pa != pb || !equalInts(wa, wb) || !equalScatter(sa, sb) || !equalGrid(ga, gb) {
			t.Fatalf("%s: same seed triple produced different spins", g.cfg.ID)
		}
	}
}

func TestPayloadConsistentWithPayout(t *testing.T) {
	for _, g := range []*Game{Classic(), FruitSalad(), Treasure()} {
		stream := fair.NewPersonalStream(bytes.Repeat([]byte{0x33}, fair.SeedSize), "consistency", 3)
		out, err := g.Play(stream, 25)
		if err != nil {
			t.Fatal(err)
		}
		var p payload
		if err := json.Unmarshal(out.Payload, &p); err != nil {
			t.Fatalf("payload not valid JSON: %v", err)
		}
		recomputed, lines, scatter, err := g.EvaluateGrid(p.Grid, 25)
		if err != nil {
			t.Fatalf("payload grid invalid: %v", err)
		}
		if p.Bonus == nil {
			if recomputed != out.PayoutCredits {
				t.Fatalf("%s: payload recomputes to %d, server paid %d", g.cfg.ID, recomputed, out.PayoutCredits)
			}
		} else {
			// Bonus rounds: base grid + every recorded free spin at the
			// multiplier must rebuild the exact paid amount.
			if recomputed != out.PayoutCredits-p.Bonus.Total {
				t.Fatalf("%s: base grid recomputes to %d, want paid %d minus bonus %d",
					g.cfg.ID, recomputed, out.PayoutCredits, p.Bonus.Total)
			}
			var spins int
			var total int64
			for _, bs := range p.Bonus.Spins {
				fpayout, _, _, err := g.EvaluateGrid(bs.Grid, 25)
				if err != nil {
					t.Fatalf("%s: bonus spin grid invalid: %v", g.cfg.ID, err)
				}
				if want := fpayout * p.Bonus.Multiplier; bs.Payout != want {
					t.Fatalf("%s: bonus spin pays %d, recomputes to %d*x%d", g.cfg.ID, bs.Payout, fpayout, p.Bonus.Multiplier)
				}
				total += bs.Payout
				spins++
				if bs.Retrigger {
					spins += g.cfg.Bonus.RetriggerSpins
				}
			}
			if total != p.Bonus.Total {
				t.Fatalf("%s: bonus spins sum to %d, payload total %d", g.cfg.ID, total, p.Bonus.Total)
			}
			if spins != p.Bonus.SpinsAwarded {
				t.Fatalf("%s: bonus spin count %d != spinsAwarded %d", g.cfg.ID, spins, p.Bonus.SpinsAwarded)
			}
			if out.PayoutCredits != recomputed+p.Bonus.Total {
				t.Fatalf("%s: paid %d != base %d + bonus %d", g.cfg.ID, out.PayoutCredits, recomputed, p.Bonus.Total)
			}
		}
		if !equalInts(lines, p.Lines) || !equalScatter(scatter, p.Scatter) {
			t.Fatalf("%s: win data mismatch", g.cfg.ID)
		}
	}
}

func TestAllSymbolsAppearAndAllLinesHit(t *testing.T) {
	for _, g := range []*Game{Classic(), FruitSalad(), Treasure()} {
		seen := map[int]bool{}
		hitLines := map[int]bool{}
		hitScatter := map[int]bool{}
		for i := int64(0); i < 20000; i++ {
			grid, winning, scatter, _ := testSpin(g, i, 5)
			for _, row := range grid {
				for _, sym := range row {
					seen[sym] = true
				}
			}
			for _, l := range winning {
				hitLines[l] = true
			}
			for _, sw := range scatter {
				hitScatter[sw.Symbol] = true
			}
		}
		if len(seen) != len(g.cfg.Symbols) {
			t.Fatalf("%s: only %d/%d symbols appeared", g.cfg.ID, len(seen), len(g.cfg.Symbols))
		}
		if g.mode() == "lines" && len(hitLines) != len(g.cfg.Lines) {
			t.Fatalf("%s: only %d/%d paylines hit", g.cfg.ID, len(hitLines), len(g.cfg.Lines))
		}
		if g.mode() == "scatter" && len(hitScatter) == 0 {
			t.Fatalf("%s: no scatter symbols ever paid", g.cfg.ID)
		}
	}
}

// findBonusSeed scans deterministic streams until one triggers the Treasure
// bonus, so the behavior tests below exercise a real round.
func findBonusSeed(t *testing.T) (seed []byte, nonce int64) {
	t.Helper()
	g := Treasure()
	for i := int64(0); i < 100000; i++ {
		seed := sha256.Sum256([]byte{byte(i), byte(i >> 8), byte(i >> 16), byte(i >> 24)})
		stream := fair.NewPersonalStream(seed[:], "bonus", i)
		grid, _, _, _ := g.spin(stream, 5)
		if countSymbol(grid, g.bonusIdx) >= 3 {
			return seed[:], i
		}
	}
	t.Fatal("no bonus trigger found in 100k spins")
	return nil, 0
}

func TestBonusRoundTriggersAndConsistent(t *testing.T) {
	g := Treasure()
	seed, nonce := findBonusSeed(t)
	stream := fair.NewPersonalStream(seed, "bonus", nonce)
	out, err := g.Play(stream, 10)
	if err != nil {
		t.Fatal(err)
	}
	var p payload
	if err := json.Unmarshal(out.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.Bonus == nil {
		t.Fatal("seed no longer triggers bonus")
	}
	b := p.Bonus
	if b.TriggerCount < 3 {
		t.Fatalf("trigger count %d < 3", b.TriggerCount)
	}
	wantSpins, ok := g.lookupSpins(b.TriggerCount)
	if !ok || b.SpinsAwarded != wantSpins {
		t.Fatalf("spinsAwarded %d != trigger table %v", b.SpinsAwarded, g.cfg.Bonus.TriggerSpins)
	}
	if b.Multiplier != g.cfg.Bonus.Multiplier {
		t.Fatalf("multiplier %d != config %d", b.Multiplier, g.cfg.Bonus.Multiplier)
	}
	if len(b.Spins) < b.SpinsAwarded {
		t.Fatalf("played %d spins < awarded %d", len(b.Spins), b.SpinsAwarded)
	}
	var total int64
	awarded := b.SpinsAwarded
	for i, bs := range b.Spins {
		fpayout, _, _, err := g.EvaluateGrid(bs.Grid, 10)
		if err != nil {
			t.Fatalf("spin %d grid invalid: %v", i, err)
		}
		if bs.Payout != fpayout*b.Multiplier {
			t.Fatalf("spin %d pays %d, want %d*x%d", i, bs.Payout, fpayout, b.Multiplier)
		}
		total += bs.Payout
		if bs.Retrigger {
			awarded += g.cfg.Bonus.RetriggerSpins
		}
	}
	if total != b.Total {
		t.Fatalf("spins sum %d != total %d", total, b.Total)
	}
	if len(b.Spins) != awarded {
		t.Fatalf("played %d spins, awarded %d (incl retrigger)", len(b.Spins), awarded)
	}
	if out.PayoutCredits != b.Total {
		t.Fatalf("paid %d, bonus total %d", out.PayoutCredits, b.Total)
	}
}

func TestBonusReplayIdentical(t *testing.T) {
	g := Treasure()
	seed, nonce := findBonusSeed(t)
	a := fair.NewPersonalStream(seed, "bonus", nonce)
	b := fair.NewPersonalStream(seed, "bonus", nonce)
	oa, _ := g.Play(a, 10)
	ob, _ := g.Play(b, 10)
	if string(oa.Payload) != string(ob.Payload) || oa.PayoutCredits != ob.PayoutCredits {
		t.Fatal("same fairness triple produced different bonus rounds")
	}
}

func TestBonusExcludesFlatPays(t *testing.T) {
	g := Treasure()
	// The trigger symbol must not flat-pay below KeepFlatFrom, even directly.
	grid := [][]int{
		{6, 6, 6, 0},
		{6, 0, 0, 0},
		{0, 0, 0, 0},
		{0, 0, 0, 0},
	}
	payout, _, scatter, err := g.EvaluateGrid(grid, 10)
	if err != nil {
		t.Fatal(err)
	}
	if payout != 0 || len(scatter) != 0 {
		t.Fatalf("3 bonus flat-paid %d (scatter %+v), want 0 (round replaces it)", payout, scatter)
	}
	// KeepFlatFrom+ still flat-pays.
	grid[2][0] = 6
	grid[2][1] = 6
	payout, _, scatter, err = g.EvaluateGrid(grid, 10)
	if err != nil {
		t.Fatal(err)
	}
	pay, ok := lookupPays(g.cfg.Symbols[6], 6)
	if !ok || payout != pay*10 || len(scatter) != 1 {
		t.Fatalf("6 bonus paid %d (scatter %+v), want 6-tier pay %d", payout, scatter, pay)
	}
}

func TestBonusConfigValidation(t *testing.T) {
	base := Treasure().cfg
	cases := []struct {
		name string
		mut  func(*Config)
	}{
		{"weights sum", func(c *Config) { c.Bonus.BonusWeights[0]++ }},
		{"weights length", func(c *Config) { c.Bonus.BonusWeights = c.Bonus.BonusWeights[:6] }},
		{"unknown symbol", func(c *Config) { c.Bonus.Symbol = "nope" }},
		{"trigger count low", func(c *Config) { c.Bonus.TriggerSpins = map[int]int{2: 8} }},
		{"trigger spins zero", func(c *Config) { c.Bonus.TriggerSpins = map[int]int{3: 0} }},
		{"multiplier zero", func(c *Config) { c.Bonus.Multiplier = 0 }},
		{"retrigger count one", func(c *Config) { c.Bonus.RetriggerCount = 1 }},
		{"keepFlatFrom low", func(c *Config) { c.Bonus.KeepFlatFrom = 1 }},
	}
	for _, tc := range cases {
		cfg := Treasure().cfg
		tc.mut(&cfg)
		if err := validate(cfg); err == nil {
			t.Fatalf("%s: invalid bonus config accepted", tc.name)
		}
	}
	if err := validate(base); err != nil {
		t.Fatalf("treasure bonus config rejected: %v", err)
	}
	// Bonus on a lines game is rejected.
	lines := Classic().cfg
	lines.Bonus = &BonusSpec{Symbol: "crown", TriggerSpins: map[int]int{3: 5}, KeepFlatFrom: 3, BonusWeights: []int64{1, 1, 1, 1, 1, 1, 1, 1}, Multiplier: 2, RetriggerCount: 2, RetriggerSpins: 5}
	if err := validate(lines); err == nil {
		t.Fatal("bonus on lines game accepted")
	}
}

// TestAnalyticRTPBand asserts every game lands in the target band and
// records the analytic figure.
func TestAnalyticRTPBand(t *testing.T) {
	for _, g := range []*Game{Classic(), FruitSalad(), Treasure()} {
		rtp := g.TheoreticalRTP()
		t.Logf("%s: analytic RTP = %.6f (%.2f%%)", g.cfg.ID, rtp, rtp*100)
		if rtp < 0.9 || rtp > 1.0 {
			t.Fatalf("%s: analytic RTP %v outside [0.9, 1.0]", g.cfg.ID, rtp)
		}
	}
}

// TestRTPSimulation is the RTP gate for every game: 10 million spins must
// land within 0.3% of the analytic RTP. Deterministic. Skips under -short.
func TestRTPSimulation(t *testing.T) {
	if testing.Short() {
		t.Skip("RTP simulation skipped in -short mode")
	}
	for _, g := range []*Game{Classic(), FruitSalad(), Treasure()} {
		g := g
		t.Run(g.cfg.ID, func(t *testing.T) {
			spins := 10_000_000
			const bet = int64(10)

			master, err := fair.GenerateSeed()
			if err != nil {
				t.Fatal(err)
			}
			if g.mode() == "scatter" {
				// Scatter pays have fat tails: a bigger sample keeps the
				// deviation inside the gate.
				spins = 30_000_000
			}
			bitsNeeded := bits.Len(uint(spins))

			var totalBet, totalPayout int64
			for i := 0; i < spins; i++ {
				h := sha256.New()
				h.Write(master)
				idx := uint64(i)
				for b := 0; b < bitsNeeded; b += 8 {
					h.Write([]byte{byte(idx >> uint(b))})
				}
				stream := fair.NewPersonalStream(h.Sum(nil), "rtp-sim", int64(i))
				_, payout := g.round(stream, bet)
				totalPayout += payout
				totalBet += bet
			}

			measured := float64(totalPayout) / float64(totalBet)
			theoretical := g.TheoreticalRTP()
			deviation := (measured - theoretical) / theoretical

			t.Logf("RTP SIM [%s]: measured=%.6f theoretical=%.6f deviation=%+.4f%%",
				g.cfg.ID, measured, theoretical, deviation*100)

			if deviation > 0.003 || deviation < -0.003 {
				t.Fatalf("%s: measured RTP %.6f deviates %+.4f%% from analytic %.6f (>0.3%%)",
					g.cfg.ID, measured, deviation*100, theoretical)
			}
		})
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalGrid(a, b [][]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !equalInts(a[i], b[i]) {
			return false
		}
	}
	return true
}

func equalScatter(a, b []ScatterWin) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
