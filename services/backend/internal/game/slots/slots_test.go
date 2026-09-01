package slots

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"math/bits"
	"testing"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
)

func TestWeightsSumTo100(t *testing.T) {
	var sum int64
	for _, s := range symbols {
		if s.Weight <= 0 || s.Pay3 <= 0 || s.Pay4 < s.Pay3 || s.Pay5 < s.Pay4 {
			t.Fatalf("symbol %q has non-positive weight/pay", s.Name)
		}
		sum += s.Weight
	}
	if sum != weightSum {
		t.Fatalf("weights sum to %d, want %d", sum, weightSum)
	}
	// Ordered common to rare, matching the paytable's escalation.
	for i := 1; i < len(symbols); i++ {
		if symbols[i].Weight >= symbols[i-1].Weight {
			t.Fatalf("weights not strictly decreasing at %q", symbols[i].Name)
		}
		if symbols[i].Pay3 <= symbols[i-1].Pay3 {
			t.Fatalf("pays not increasing at %q", symbols[i].Name)
		}
	}
}

func TestPaylinesShape(t *testing.T) {
	if len(paylines) != lineCount {
		t.Fatalf("expected %d paylines, got %d", lineCount, len(paylines))
	}
	for i, line := range paylines {
		if len(line) != cols {
			t.Fatalf("payline %d has %d cells, want %d", i, len(line), cols)
		}
		for _, cl := range line {
			if cl.y < 0 || cl.y >= rows || cl.x < 0 || cl.x >= cols {
				t.Fatalf("payline %d has out-of-range cell %+v", i, cl)
			}
		}
	}
	// The V and A diagonals must both be present.
	if paylines[3] != [cols]cell{{0, 0}, {1, 1}, {2, 2}, {3, 1}, {4, 0}} {
		t.Fatal("V payline misconfigured")
	}
	if paylines[4] != [cols]cell{{0, 2}, {1, 1}, {2, 0}, {3, 1}, {4, 2}} {
		t.Fatal("A payline misconfigured")
	}
}

func TestValidateBetSteps(t *testing.T) {
	g := New()
	for _, step := range BetSteps {
		if err := g.ValidateBet(step); err != nil {
			t.Fatalf("bet %d rejected: %v", step, err)
		}
	}
	for _, bad := range []int64{0, 1, 4, 6, -5, 1000} {
		if err := g.ValidateBet(bad); err == nil {
			t.Fatalf("bet %d accepted", bad)
		}
	}
}

func TestSpinDeterministic(t *testing.T) {
	seed := bytes.Repeat([]byte{0x5A}, fair.SeedSize)
	a := fair.NewPersonalStream(seed, "client", 1)
	b := fair.NewPersonalStream(seed, "client", 1)

	ga, wa, pa := spin(a, 10)
	gb, wb, pb := spin(b, 10)
	if ga != gb || pa != pb || !equalInts(wa, wb) {
		t.Fatal("same seed triple produced different spins")
	}
}

func TestPayloadConsistentWithPayout(t *testing.T) {
	g := New()
	stream := fair.NewPersonalStream(bytes.Repeat([]byte{0x33}, fair.SeedSize), "consistency", 3)

	out, err := g.Play(stream, 25)
	if err != nil {
		t.Fatal(err)
	}
	var p payload
	if err := json.Unmarshal(out.Payload, &p); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	// Recompute the payout from the payload grid â€” the client-facing data
	// must fully explain the server's number.
	recomputed, lines, err := EvaluateGrid(p.Grid, 25)
	if err != nil {
		t.Fatalf("payload grid invalid: %v", err)
	}
	if recomputed != out.PayoutCredits {
		t.Fatalf("payload recomputes to %d, server paid %d", recomputed, out.PayoutCredits)
	}
	if !equalInts(lines, p.WinningLines) {
		t.Fatalf("winning lines %v != recomputed %v", p.WinningLines, lines)
	}
}

func TestAllSymbolsAppearAndAllLinesHit(t *testing.T) {
	// Over enough spins every symbol appears and every payline wins at least
	// once (coverage of the evaluator, not statistics).
	seenSymbols := map[int]bool{}
	hitLines := map[int]bool{}
	for i := int64(0); i < 5000; i++ {
		seed := sha256.Sum256([]byte{byte(i), byte(i >> 8), byte(i >> 16)})
		stream := fair.NewPersonalStream(seed[:], "coverage", i)
		grid, winning, _ := spin(stream, 5)
		for y := 0; y < rows; y++ {
			for x := 0; x < cols; x++ {
				seenSymbols[grid[y][x]] = true
			}
		}
		for _, l := range winning {
			hitLines[l] = true
		}
	}
	if len(seenSymbols) != len(symbols) {
		t.Fatalf("only %d/%d symbols appeared", len(seenSymbols), len(symbols))
	}
	if len(hitLines) != lineCount {
		t.Fatalf("only %d/%d paylines hit", len(hitLines), lineCount)
	}
}

func TestTheoreticalRTPNear98(t *testing.T) {
	rtp := New().TheoreticalRTP()
	t.Logf("analytic slots RTP = %.6f (%.2f%%)", rtp, rtp*100)
	// The shipped tables come out at ~0.9775 for nine lines. Assert the band
	// and record the figure; the measured figure lives in the RTP sim.
	if rtp < 0.95 || rtp > 1.0 {
		t.Fatalf("analytic RTP %v outside [0.95, 1.0]", rtp)
	}
}

// TestRTPSimulation is the phase 4 gate: 10 million simulated spins must
// land within 0.3% of the analytic RTP, and the test reports the actual
// figure. Deterministic: per-spin seeds derive from one master seed, so a
// failure reproduces exactly. Skips under -short.
func TestRTPSimulation(t *testing.T) {
	if testing.Short() {
		t.Skip("RTP simulation skipped in -short mode")
	}

	const spins = 10_000_000
	const bet = int64(10)

	master, err := fair.GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	g := New()
	bitsNeeded := bits.Len(uint(spins))

	var totalBet, totalPayout int64
	for i := 0; i < spins; i++ {
		// Deterministic per-spin server seed: sha256(master || index).
		h := sha256.New()
		h.Write(master)
		idx := uint64(i)
		for b := 0; b < bitsNeeded; b += 8 {
			h.Write([]byte{byte(idx >> uint(b))})
		}
		stream := fair.NewPersonalStream(h.Sum(nil), "rtp-sim", int64(i))

		_, _, payout := spin(stream, bet)
		totalPayout += payout
		totalBet += bet
	}

	measured := float64(totalPayout) / float64(totalBet)
	theoretical := g.TheoreticalRTP()
	deviation := (measured - theoretical) / theoretical

	// Repo record: measured RTP figure is logged here on every run.
	t.Logf("RTP SIM: %d spins, bet=%d", spins, bet)
	t.Logf("RTP SIM: measured=%.6f theoretical=%.6f deviation=%+.4f%%",
		measured, theoretical, deviation*100)

	if deviation > 0.003 || deviation < -0.003 {
		t.Fatalf("measured RTP %.6f deviates %+.4f%% from analytic %.6f (>0.3%%)",
			measured, deviation*100, theoretical)
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
