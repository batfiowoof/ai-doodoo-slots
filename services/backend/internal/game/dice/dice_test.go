package dice

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
)

func stream(seed byte, nonce int64) *fair.Stream {
	return fair.NewPersonalStream(bytes.Repeat([]byte{seed}, fair.SeedSize), "dice-test", nonce)
}

func params(t *testing.T, direction string, target int) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(Params{Direction: direction, Target: target})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return raw
}

func TestValidateBet(t *testing.T) {
	g := New()
	for _, ok := range []int64{1, 7, 37, 10000} {
		if err := g.ValidateBet(ok); err != nil {
			t.Errorf("ValidateBet(%d): %v", ok, err)
		}
	}
	for _, bad := range []int64{0, -5, 10001} {
		if err := g.ValidateBet(bad); err == nil {
			t.Errorf("ValidateBet(%d) accepted", bad)
		}
	}
}

func TestValidateParams(t *testing.T) {
	g := New()
	for _, ok := range []struct {
		direction string
		target    int
	}{
		{"under", 2}, {"under", 50}, {"over", 98}, {"over", 3},
	} {
		if err := g.ValidateParams(params(t, ok.direction, ok.target)); err != nil {
			t.Errorf("params %s %d rejected: %v", ok.direction, ok.target, err)
		}
	}
	for _, bad := range []struct {
		direction string
		target    int
	}{
		{"sideways", 50}, {"under", 0}, {"under", 1}, {"over", 99}, {"over", 100}, {"", 50},
	} {
		if err := g.ValidateParams(params(t, bad.direction, bad.target)); err == nil {
			t.Errorf("params %q %d accepted", bad.direction, bad.target)
		}
	}
	if err := g.ValidateParams(json.RawMessage(`{`)); err == nil {
		t.Error("malformed JSON accepted")
	}
}

func TestRollDeterministic(t *testing.T) {
	a, err := New().PlayWithParams(stream(0x5A, 1), 10, params(t, "under", 50))
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	b, err := New().PlayWithParams(stream(0x5A, 1), 10, params(t, "under", 50))
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	if a.PayoutCredits != b.PayoutCredits || string(a.Payload) != string(b.Payload) {
		t.Fatalf("same seed diverged: %s vs %s", a.Payload, b.Payload)
	}
}

func TestPayoutTable(t *testing.T) {
	g := New()
	cases := []struct {
		direction string
		target    int
		bet       int64
		// Every winning payout is bet*99/chance floored; assert the multiplier
		// matches on a forced win by scanning seeds until one wins.
		wantMultNumerator int64
		wantMultDenominator int64
	}{
		{"under", 50, 10, 99, 50},  // 1.98×
		{"under", 25, 100, 99, 25}, // 3.96×
		{"over", 90, 100, 99, 10},  // 9.9×
		{"over", 98, 5, 99, 2},     // 49.5×
	}
	for _, c := range cases {
		found := false
		for nonce := int64(1); nonce <= 500 && !found; nonce++ {
			out, err := g.PlayWithParams(stream(0x33, nonce), c.bet, params(t, c.direction, c.target))
			if err != nil {
				t.Fatalf("play: %v", err)
			}
			var p struct {
				Roll  float64 `json:"roll"`
				Win   bool    `json:"win"`
				Chanc int     `json:"chance"`
			}
			if err := json.Unmarshal(out.Payload, &p); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if p.Chanc != 100-c.target && c.direction == "over" {
				t.Fatalf("over %d: chance %d", c.target, p.Chanc)
			}
			if !p.Win {
				if out.PayoutCredits != 0 {
					t.Fatalf("lost roll paid %d", out.PayoutCredits)
				}
				continue
			}
			found = true
			// payout/bet == floor(bet·99/chance)/bet; compare exactly.
			want := c.bet * c.wantMultNumerator / c.wantMultDenominator
			if out.PayoutCredits != want {
				t.Fatalf("%s %d bet %d: payout %d, want %d", c.direction, c.target, c.bet, out.PayoutCredits, want)
			}
			if p.Roll >= 100 || p.Roll < 0 {
				t.Fatalf("roll %v out of range", p.Roll)
			}
		}
		if !found {
			t.Fatalf("%s %d: no win in 500 rolls (impossible)", c.direction, c.target)
		}
	}
}

func TestRollRange(t *testing.T) {
	g := New()
	for nonce := int64(1); nonce <= 2000; nonce++ {
		out, err := g.PlayWithParams(stream(0x77, nonce), 1, params(t, "under", 98))
		if err != nil {
			t.Fatalf("play: %v", err)
		}
		var p struct {
			Roll float64 `json:"roll"`
		}
		if err := json.Unmarshal(out.Payload, &p); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if p.Roll < 0 || p.Roll >= 100 {
			t.Fatalf("roll %v outside [0, 100)", p.Roll)
		}
		// Two-decimal grid (tolerate float representation error).
		if math.Abs(p.Roll*100-math.Round(p.Roll*100)) > 1e-6 {
			t.Fatalf("roll %v not on the 2-decimal grid", p.Roll)
		}
	}
}

// TestSimulationRTP gates the long-run return against the designed 99%.
func TestSimulationRTP(t *testing.T) {
	g := New()
	configs := []struct {
		direction string
		target    int
	}{
		{"under", 50},
		{"under", 10},
		{"over", 90},
		{"over", 66},
	}
	const rolls = 200000
	const bet = int64(100)
	for _, c := range configs {
		var wagered, returned int64
		for nonce := int64(1); nonce <= rolls; nonce++ {
			// Vary the seed across chunks so the sim covers many independent
			// streams, not one seed's finite roll set.
			st := fair.NewPersonalStream(bytes.Repeat([]byte{byte(nonce/1000 + 1)}, fair.SeedSize), "dice-sim", nonce%1000+1)
			out, err := g.PlayWithParams(st, bet, params(t, c.direction, c.target))
			if err != nil {
				t.Fatalf("play: %v", err)
			}
			wagered += bet
			returned += out.PayoutCredits
		}
		rtp := float64(returned) / float64(wagered)
		// Floor quantization + sampling noise; 1.5% is generous headroom.
		if rtp < 0.975 || rtp > 1.005 {
			t.Errorf("%s %d: sim RTP %.4f outside [0.975, 1.005]", c.direction, c.target, rtp)
		}
	}
}

func TestPlayWithoutParamsLoses(t *testing.T) {
	g := New()
	if _, err := g.Play(stream(0x11, 1), 10); err == nil {
		t.Fatal("Play without params accepted")
	}
	if err := g.ValidateParams(nil); err == nil {
		t.Fatal("ValidateParams(nil) accepted")
	}
}
