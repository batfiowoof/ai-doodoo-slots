package crash

import (
	"crypto/sha256"
	"encoding/json"
	"testing"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
)

// TestCrashRTP is the phase-13 gate: 10 million simulated rounds with a
// fixed 2.00x auto-cashout land on the intended 99% RTP. The figure is
// recorded here for the review checklist: measured RTP printed on failure.
func TestCrashRTP(t *testing.T) {
	if testing.Short() {
		t.Skip("long")
	}
	g := New()
	const rounds = 10_000_000
	const bet = int64(100)

	var totalBet, totalPaid int64
	var idx [8]byte
	for i := 0; i < rounds; i++ {
		// The outcome distribution must hold for any deterministic sequence
		// of streams, so derive each round's chain seed from the round
		// index with a uniform hash.
		idx[0] = byte(i)
		idx[1] = byte(i >> 8)
		idx[2] = byte(i >> 16)
		idx[3] = byte(i >> 24)
		seed := sha256.Sum256(idx[:])
		stream := fair.NewChainStream(seed[:], "rtp")
		res, err := g.Resolve(stream)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		opts, _ := json.Marshal(Options{AutoCashout: 2.0})
		paid, err := g.SettleBet(res, game.RoundBet{BetCredits: bet, Options: opts})
		if err != nil {
			t.Fatalf("settle: %v", err)
		}
		totalBet += bet
		totalPaid += paid
	}

	rtp := float64(totalPaid) / float64(totalBet)
	// Target 0.99; with n=10M and p≈0.495, sigma ≈ 0.00016 — 0.005 slack is
	// generous yet meaningful.
	if rtp < 0.985 || rtp > 0.995 {
		t.Fatalf("measured RTP %.4f outside [0.985, 0.995] (target 0.99)", rtp)
	}
}

func TestResolveBoundsAndDeterminism(t *testing.T) {
	g := New()
	var seed [32]byte
	for b := range seed {
		seed[b] = byte(0xA5 ^ b)
	}
	stream := fair.NewChainStream(seed[:], "det")
	r1, err := g.Resolve(stream)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	stream2 := fair.NewChainStream(seed[:], "det")
	r2, _ := g.Resolve(stream2)
	if r1.Multiplier != r2.Multiplier {
		t.Fatal("resolve not deterministic for identical inputs")
	}
	if r1.Multiplier < 1.00 || r1.Multiplier > MaxMultiplier {
		t.Fatalf("multiplier %v out of bounds", r1.Multiplier)
	}
}

func TestSettleBetIntegerMath(t *testing.T) {
	g := New()
	res := game.RoundResult{Multiplier: 2.5, Payload: json.RawMessage(`{}`)}

	// Win at exactly the crash multiplier.
	paid, err := g.SettleBet(res, game.RoundBet{BetCredits: 33, Options: mustOpts(t, 2.5)})
	if err != nil || paid != 82 { // 33 * 2.50 = 82.5 → floored to 82 whole credits
		t.Fatalf("paid = %d, err = %v; want 82", paid, err)
	}

	// Crash before the target loses.
	paid, _ = g.SettleBet(res, game.RoundBet{BetCredits: 33, Options: mustOpts(t, 2.51)})
	if paid != 0 {
		t.Fatalf("paid = %d, want 0", paid)
	}

	// Instant crash (1.00) pays nothing above 1.01 targets.
	instant := game.RoundResult{Multiplier: 1.00}
	paid, _ = g.SettleBet(instant, game.RoundBet{BetCredits: 33, Options: mustOpts(t, 2.0)})
	if paid != 0 {
		t.Fatalf("paid = %d, want 0 on instant crash", paid)
	}

	// No options: loses (no server-side guess).
	paid, _ = g.SettleBet(res, game.RoundBet{BetCredits: 33})
	if paid != 0 {
		t.Fatalf("paid = %d, want 0 without options", paid)
	}
}

func mustOpts(t *testing.T, target float64) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(Options{AutoCashout: target})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}
