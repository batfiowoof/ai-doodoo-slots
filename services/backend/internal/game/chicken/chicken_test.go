package chicken

import (
	"bytes"
	"math"
	"testing"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
)

func stream(seed byte, nonce int64) *fair.Stream {
	return fair.NewPersonalStream(bytes.Repeat([]byte{seed}, fair.SeedSize), "chicken-test", nonce)
}

func TestDrawFatalLaneRange(t *testing.T) {
	for nonce := int64(1); nonce <= 2000; nonce++ {
		for _, d := range Difficulties {
			f := DrawFatalLane(stream(0x21, nonce), d)
			if f < 1 || f > d.Lanes+1 {
				t.Fatalf("%s: fatal lane %d out of range 1..%d", d.Name, f, d.Lanes+1)
			}
		}
	}
}

func TestDrawFatalLaneDeterministic(t *testing.T) {
	d, _ := ByName("medium")
	for nonce := int64(1); nonce <= 100; nonce++ {
		a := DrawFatalLane(stream(0x5A, nonce), d)
		b := DrawFatalLane(stream(0x5A, nonce), d)
		if a != b {
			t.Fatalf("same seed diverged at nonce %d: %d vs %d", nonce, a, b)
		}
	}
}

func TestDrawFatalLaneDistribution(t *testing.T) {
	// Easy rolls: P(clear) = 0.92^20 ≈ 0.189, P(fatal = 1) = 0.08. At 200k
	// draws σ ≈ 0.0009 and 0.0006, so 0.01 is comfortably > 3σ.
	const rounds = 200000
	d, _ := ByName("easy")
	cleared, first := 0, 0
	for nonce := int64(1); nonce <= rounds; nonce++ {
		switch f := DrawFatalLane(stream(byte(nonce/1000+1), nonce%1000+1), d); f {
		case d.Lanes + 1:
			cleared++
		case 1:
			first++
		}
	}
	if p := float64(cleared) / rounds; math.Abs(p-math.Pow(d.Survival, float64(d.Lanes))) > 0.01 {
		t.Errorf("clear rate %.4f, want ~%.4f", p, math.Pow(d.Survival, float64(d.Lanes)))
	}
	if p := float64(first) / rounds; math.Abs(p-(1-d.Survival)) > 0.01 {
		t.Errorf("first-lane rate %.4f, want ~%.4f", p, 1-d.Survival)
	}
}

func TestMultiplierLadder(t *testing.T) {
	medium, _ := ByName("medium")
	if got := Multiplier(medium, 0); got != 1 {
		t.Fatalf("0 lanes multiplier %v, want 1", got)
	}
	// First lane: 0.99 / 0.82 = 1.2073 → 1.20.
	if got := Multiplier(medium, 1); got != 1.20 {
		t.Fatalf("medium lane 1: %v, want 1.20", got)
	}
	// Monotonic in lanes for every road.
	for _, d := range Difficulties {
		prev := 1.0
		for lane := 1; lane <= d.Lanes; lane++ {
			got := Multiplier(d, lane)
			if got <= prev {
				t.Fatalf("%s lane %d: %v not above previous %v", d.Name, lane, got, prev)
			}
			prev = got
		}
	}
}

func TestTopMultipliers(t *testing.T) {
	// Ladder tops land in the designed bands.
	for _, tc := range []struct {
		name     string
		min, max float64
	}{
		{"easy", 4, 6},
		{"medium", 30, 40},
		{"hard", 180, 230},
		{"hardcore", 1200, 1400},
	} {
		d, err := ByName(tc.name)
		if err != nil {
			t.Fatalf("preset %s missing", tc.name)
		}
		if got := d.TopMultiplier(); got < tc.min || got > tc.max {
			t.Errorf("%s top multiplier %.2f outside [%v, %v]", tc.name, got, tc.min, tc.max)
		}
	}
}

func TestMaxBetFor(t *testing.T) {
	for _, d := range Difficulties {
		max := MaxBetFor(d)
		if max < MinBet {
			t.Errorf("%s max bet %d below min", d.Name, max)
		}
		// A top-lane cashout at max bet never breaches the win cap (the
		// payout floors down, so the check is ≤ with a credit of slack).
		if win := int64(float64(max) * d.TopMultiplier()); win > MaxWinCredits {
			t.Errorf("%s max-bet top win %d exceeds cap %d", d.Name, win, MaxWinCredits)
		}
	}
}

func TestValidate(t *testing.T) {
	d, _ := ByName("easy")
	for _, ok := range []int64{1, 7, MaxBetFor(d)} {
		if err := ValidateBet(d, ok); err != nil {
			t.Errorf("bet %d rejected: %v", ok, err)
		}
	}
	for _, bad := range []int64{0, -5, MaxBetFor(d) + 1} {
		if err := ValidateBet(d, bad); err == nil {
			t.Errorf("bet %d accepted", bad)
		}
	}
	if _, err := ByName("nightmare"); err == nil {
		t.Error("unknown difficulty accepted")
	}
}

// TestSimulationRTP verifies the designed property: cashing out after any
// fixed number of lanes returns 99% in the long run.
func TestSimulationRTP(t *testing.T) {
	const rounds = 200000
	const bet = int64(100)
	for _, tc := range []struct {
		name string
		stop int
	}{
		{"easy", 10}, {"medium", 9}, {"hard", 8}, {"hardcore", 6},
	} {
		d, _ := ByName(tc.name)
		var wagered, returned int64
		for nonce := int64(1); nonce <= rounds; nonce++ {
			st := fair.NewPersonalStream(bytes.Repeat([]byte{byte(nonce/1000 + 1)}, fair.SeedSize), "chicken-sim", nonce%1000+1)
			fatal := DrawFatalLane(st, d)
			wagered += bet
			if fatal > tc.stop {
				returned += Payout(bet, d, tc.stop)
			}
		}
		rtp := float64(returned) / float64(wagered)
		// Long-lane cashouts on risky roads pay big on rare wins; σ of the
		// RTP estimate stays ≈1.3 points at these stops, so 5 is ~4σ.
		if math.Abs(rtp-0.99) > 0.05 {
			t.Errorf("%s stop %d: sim RTP %.4f vs 0.99", tc.name, tc.stop, rtp)
		}
	}
}
