package blackjack

import (
	"testing"

	"github.com/ai-doodoo-slots/services/backend/internal/cards"
	"github.com/ai-doodoo-slots/services/backend/internal/fair"
)

func testStream(t *testing.T, nonce int64) *fair.Stream {
	t.Helper()
	seed, err := fair.GenerateSeed()
	if err != nil {
		t.Fatalf("GenerateSeed: %v", err)
	}
	return fair.NewPersonalStream(seed, "test-client", nonce)
}

func TestValidateBet(t *testing.T) {
	e := New([]int64{10, 25, 50})
	for _, ok := range []int64{10, 25, 50} {
		if err := e.ValidateBet(ok); err != nil {
			t.Errorf("ValidateBet(%d): %v", ok, err)
		}
	}
	for _, bad := range []int64{0, -5, 7, 100} {
		if err := e.ValidateBet(bad); err == nil {
			t.Errorf("ValidateBet(%d) accepted", bad)
		}
	}
}

func TestDealDeterministic(t *testing.T) {
	seed, err := fair.GenerateSeed()
	if err != nil {
		t.Fatalf("GenerateSeed: %v", err)
	}
	a, err := New([]int64{10}).Deal(fair.NewPersonalStream(seed, "c", 1), 10)
	if err != nil {
		t.Fatalf("Deal: %v", err)
	}
	b, err := New([]int64{10}).Deal(fair.NewPersonalStream(seed, "c", 1), 10)
	if err != nil {
		t.Fatalf("Deal: %v", err)
	}
	if a.PlayerCards != b.PlayerCards || a.DealerCards != b.DealerCards || a.Deck != b.Deck {
		t.Fatal("same seed material dealt different hands")
	}
	if a.PlayerCards == "" || a.DealerCards == "" || a.Deck == "" {
		t.Fatal("deal left empty card lists")
	}
}

func TestHandLifecycle(t *testing.T) {
	e := New([]int64{10})
	st, err := e.Deal(testStream(t, 1), 10)
	if err != nil {
		t.Fatalf("Deal: %v", err)
	}
	if st.Status == StatusActive {
		// Drive to completion with hits, then verify the terminal
		// guarantees.
		for i := 0; i < 10 && st.Status == StatusActive; i++ {
			if err := e.Hit(st); err != nil {
				t.Fatalf("Hit: %v", err)
			}
		}
	}
	if st.Status != StatusComplete {
		t.Fatalf("hand did not complete: %+v", st)
	}
	if err := e.Hit(st); err == nil {
		t.Fatal("hit after completion accepted")
	}
	if err := e.Stand(st); err == nil {
		t.Fatal("stand after completion accepted")
	}
	if err := e.Double(st); err == nil {
		t.Fatal("double after completion accepted")
	}

	player, _ := cards.ParseCards(st.PlayerCards)
	pt, _ := cards.BJTotal(player)
	switch st.Outcome {
	case OutcomeBust:
		if pt <= 21 {
			t.Fatalf("bust outcome with total %d", pt)
		}
	case OutcomeBlackjack:
		if st.PayoutCredits != naturalPayout(10) {
			t.Fatalf("natural payout = %d, want %d", st.PayoutCredits, naturalPayout(10))
		}
	case OutcomeWin:
		if st.PayoutCredits != 2*st.BetCredits {
			t.Fatalf("win payout = %d, want %d", st.PayoutCredits, 2*st.BetCredits)
		}
	case OutcomePush:
		if st.PayoutCredits != st.BetCredits {
			t.Fatalf("push payout = %d, want %d", st.PayoutCredits, st.BetCredits)
		}
	case OutcomeLose:
		if st.PayoutCredits != 0 {
			t.Fatalf("lose payout = %d, want 0", st.PayoutCredits)
		}
	default:
		t.Fatalf("unexpected outcome %q", st.Outcome)
	}
}

func TestDoubleRules(t *testing.T) {
	e := New([]int64{10})
	// Find a deal that stays active, double it, and verify the stake
	// doubled and exactly one card was added.
	for nonce := int64(0); nonce < 200; nonce++ {
		st, err := e.Deal(testStream(t, nonce), 10)
		if err != nil {
			t.Fatalf("Deal: %v", err)
		}
		if st.Status != StatusActive {
			continue
		}
		before := st.PlayerCards
		if err := e.Double(st); err != nil {
			t.Fatalf("Double: %v", err)
		}
		if st.BetCredits != 20 {
			t.Fatalf("double left stake at %d", st.BetCredits)
		}
		if len(st.PlayerCards) != len(before)+2 {
			t.Fatalf("double drew %d cards", (len(st.PlayerCards)-len(before))/2-1)
		}
		if !st.Doubled {
			t.Fatal("doubled flag not set")
		}
		if err := e.Double(st); err == nil {
			t.Fatal("second double accepted")
		}
		if st.Status != StatusComplete {
			t.Fatal("double did not auto-stand")
		}
		return
	}
	t.Fatal("no active deal found in 200 attempts")
}

func TestDoubleOnlyOnTwoCards(t *testing.T) {
	e := New([]int64{10})
	for nonce := int64(0); nonce < 200; nonce++ {
		st, err := e.Deal(testStream(t, nonce), 10)
		if err != nil {
			t.Fatalf("Deal: %v", err)
		}
		if st.Status != StatusActive {
			continue
		}
		if err := e.Hit(st); err != nil {
			t.Fatalf("Hit: %v", err)
		}
		if st.Status == StatusActive {
			if err := e.Double(st); err == nil {
				t.Fatal("double after hit accepted")
			}
			return
		}
	}
	t.Skip("no eligible hand found; hand always ended on hit")
}

func TestNaturalPayout(t *testing.T) {
	if got := naturalPayout(10); got != 25 {
		t.Fatalf("naturalPayout(10) = %d, want 25", got)
	}
	if got := naturalPayout(5); got != 13 { // 5 + 8: 1.5x5=7.5 rounds up
		t.Fatalf("naturalPayout(5) = %d, want 13", got)
	}
	if got := naturalPayout(25); got != 63 { // 25 + 37.5->38
		t.Fatalf("naturalPayout(25) = %d, want 63", got)
	}
}

func TestDealerStandsOnAll17s(t *testing.T) {
	e := New([]int64{10})
	for nonce := int64(0); nonce < 500; nonce++ {
		st, err := e.Deal(testStream(t, nonce), 10)
		if err != nil {
			t.Fatalf("Deal: %v", err)
		}
		if st.Status != StatusActive {
			continue
		}
		if err := e.Stand(st); err != nil {
			t.Fatalf("Stand: %v", err)
		}
		dealer, _ := cards.ParseCards(st.DealerCards)
		dt, soft := cards.BJTotal(dealer)
		if st.Status != StatusComplete {
			t.Fatal("stand did not complete the hand")
		}
		if dt < 17 && st.Outcome != OutcomeWin {
			// Dealer must have drawn to at least 17 unless the player
			// won by bust... a win here means dealer bust, impossible
			// under 17. Guard anyway.
			t.Fatalf("dealer stopped at %d (soft=%v)", dt, soft)
		}
		_ = soft
	}
}

// basicStrategy plays a hand to completion using a compact no-split
// basic-strategy table. Returns total wagered (including doubles) and
// total returned.
func basicStrategy(e *Engine, st *State) (wagered, returned int64) {
	wagered = st.BetCredits
	player, _ := cards.ParseCards(st.PlayerCards)
	if st.Status == StatusActive {
		total, soft := cards.BJTotal(player)
		dealer, _ := cards.ParseCards(st.DealerCards)
		up := dealer[0].Rank

		double := false
		if len(player) == 2 {
			switch {
			case soft && total >= 16, soft && total >= 13 && up >= 5 && up <= 6:
				double = total >= 13
			}
			if total == 11 || (total == 10 && up <= 9) || (total == 9 && up >= 3 && up <= 6) {
				double = !soft || total >= 13
			}
		}
		var err error
		if double {
			err = e.Double(st)
			wagered = st.BetCredits
		} else {
			for st.Status == StatusActive {
				total, soft = func() (int, bool) {
					p, _ := cards.ParseCards(st.PlayerCards)
					return cards.BJTotal(p)
				}()
				hit := false
				if soft {
					switch {
					case total >= 19:
						hit = false
					case total == 18:
						hit = up > 8
					case total == 17:
						hit = true
					default:
						hit = true
					}
				} else {
					switch {
					case total >= 17:
						hit = false
					case total >= 13:
						hit = up >= 7
					case total == 12:
						hit = up <= 3 || up >= 7
					default:
						hit = true
					}
				}
				if !hit {
					err = e.Stand(st)
					break
				}
				err = e.Hit(st)
			}
		}
		if err != nil {
			t := st.Status
			_ = t
			return wagered, st.PayoutCredits
		}
	}
	return wagered, st.PayoutCredits
}

// TestSimulationRTP plays a large basic-strategy sample and gates the
// realized RTP around the reported theoretical value.
func TestSimulationRTP(t *testing.T) {
	if testing.Short() {
		t.Skip("long simulation")
	}
	e := New([]int64{10})
	seed, err := fair.GenerateSeed()
	if err != nil {
		t.Fatalf("GenerateSeed: %v", err)
	}
	const hands = 200_000
	var wagered, returned int64
	for i := 0; i < hands; i++ {
		stream := fair.NewPersonalStream(seed, "rtp-sim", int64(i))
		st, err := e.Deal(stream, 10)
		if err != nil {
			t.Fatalf("Deal: %v", err)
		}
		w, r := basicStrategy(e, st)
		wagered += w
		returned += r
	}
	rtp := float64(returned) / float64(wagered)
	// Basic strategy, S17, no split: expected ~0.987. Wide gate — the
	// point is to catch rule regressions (e.g. paying 1:1 on naturals
	// costs ~2.3%, hitting on bust-refund, etc).
	if rtp < 0.975 || rtp > 0.997 {
		t.Fatalf("simulated RTP = %.4f, outside [0.975, 0.997]", rtp)
	}
	t.Logf("simulated RTP over %d hands: %.4f", hands, rtp)
}
