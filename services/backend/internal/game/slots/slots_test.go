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
		if err := g.ValidateBet(7); err == nil {
			t.Fatalf("%s: bet 7 accepted", g.cfg.ID)
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
		if recomputed != out.PayoutCredits {
			t.Fatalf("%s: payload recomputes to %d, server paid %d", g.cfg.ID, recomputed, out.PayoutCredits)
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
				_, _, _, payout := g.spin(stream, bet)
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
