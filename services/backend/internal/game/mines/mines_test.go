package mines

import (
	"bytes"
	"math"
	"testing"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
)

func stream(seed byte, nonce int64) *fair.Stream {
	return fair.NewPersonalStream(bytes.Repeat([]byte{seed}, fair.SeedSize), "mines-test", nonce)
}

func TestDrawMinesDistinct(t *testing.T) {
	for nonce := int64(1); nonce <= 500; nonce++ {
		for _, m := range []int{1, 3, 5, 10, 24} {
			mines := DrawMines(stream(0x11, nonce), m)
			if len(mines) != m {
				t.Fatalf("m=%d: got %d mines", m, len(mines))
			}
			seen := make(map[int]bool, m)
			for _, p := range mines {
				if p < 0 || p >= Tiles {
					t.Fatalf("position %d out of range", p)
				}
				if seen[p] {
					t.Fatalf("duplicate mine at %d", p)
				}
				seen[p] = true
			}
		}
	}
}

func TestDrawMinesDeterministic(t *testing.T) {
	a := DrawMines(stream(0x5A, 7), 5)
	b := DrawMines(stream(0x5A, 7), 5)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("same seed diverged at %d: %v vs %v", i, a, b)
		}
	}
}

func TestMultiplierLadder(t *testing.T) {
	// No reveals: nothing to cash.
	if got := Multiplier(3, 0); got != 1 {
		t.Fatalf("0 reveals multiplier %v, want 1", got)
	}
	// 3 mines, first safe pick: 0.99·25/22 = 1.125 → 1.12.
	if got := Multiplier(3, 1); got != 1.12 {
		t.Fatalf("3 mines 1 reveal: %v, want 1.12", got)
	}
	// More mines pay faster: 1 mine, 1 reveal → 0.99·25/24 = 1.03125 → 1.03.
	if got := Multiplier(1, 1); got != 1.03 {
		t.Fatalf("1 mine 1 reveal: %v, want 1.03", got)
	}
	// Monotonic in reveals and in mines.
	for m := 1; m <= 5; m++ {
		prev := 1.0
		for k := 1; k <= 8; k++ {
			got := Multiplier(m, k)
			if got <= prev {
				t.Fatalf("m=%d k=%d: %v not above previous %v", m, k, got, prev)
			}
			prev = got
		}
	}
}

func TestMultiplierAllSafe(t *testing.T) {
	// Reveal every safe tile on a 1-mine board: 0.99·C(25,24)/C(24,24) = 24.75.
	if got := Multiplier(1, 24); math.Abs(got-24.75) > 1e-9 {
		t.Fatalf("1 mine 24 reveals: %v, want 24.75", got)
	}
}

func TestValidate(t *testing.T) {
	for _, ok := range []int64{1, 7, 10000} {
		if err := ValidateBet(ok); err != nil {
			t.Errorf("bet %d rejected: %v", ok, err)
		}
	}
	for _, bad := range []int64{0, -5, 10001} {
		if err := ValidateBet(bad); err == nil {
			t.Errorf("bet %d accepted", bad)
		}
	}
	for _, m := range []int{1, 12, 24} {
		if err := ValidateMineCount(m); err != nil {
			t.Errorf("mines %d rejected: %v", m, err)
		}
	}
	for _, m := range []int{0, -1, 25} {
		if err := ValidateMineCount(m); err == nil {
			t.Errorf("mines %d accepted", m)
		}
	}
}

// TestSimulationRTP verifies the designed property: cashing out at any k
// returns 99% in the long run. Simulate a fixed k-reveal cashout strategy.
func TestSimulationRTP(t *testing.T) {
	const rounds = 200000
	const bet = int64(100)
	for _, tc := range []struct {
		mines, stop int
	}{
		{3, 1}, {3, 5}, {5, 3}, {10, 2}, {1, 10}, {24, 1},
	} {
		var wagered, returned int64
		for nonce := int64(1); nonce <= rounds; nonce++ {
			st := fair.NewPersonalStream(bytes.Repeat([]byte{byte(nonce/1000 + 1)}, fair.SeedSize), "mines-sim", nonce%1000+1)
			layout := make(map[int]bool)
			for _, p := range DrawMines(st, tc.mines) {
				layout[p] = true
			}
			wagered += bet
			// Reveal tiles in order 0,1,2… skipping mines; the strategy
			// stops after tc.stop safe reveals or busts.
			var revealed int
			busted := false
			for tile := 0; tile < Tiles && revealed < tc.stop; tile++ {
				if layout[tile] {
					busted = true
					break
				}
				revealed++
			}
			if !busted {
				returned += Payout(bet, tc.mines, revealed)
			}
		}
		rtp := float64(returned) / float64(wagered)
		// High-mine boards pay ~25× on ~4% wins: σ of the RTP estimate is
		// over a percentage point at 200k rounds, so 3.5 points is ~3σ.
		if math.Abs(rtp-0.99) > 0.035 {
			t.Errorf("mines %d stop %d: sim RTP %.4f vs 0.99", tc.mines, tc.stop, rtp)
		}
	}
}
