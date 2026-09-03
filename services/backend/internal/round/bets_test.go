package round

import (
	"testing"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
)

// seedRound starts a machine with a fixed crash point for assertions.
func seedRound(t *testing.T, crash float64) (*Machine, time.Time) {
	t.Helper()
	var seed [32]byte
	seed[0] = 0xCD
	m, err := Start("crash-1", stubGame{crash: crash}, fair.NewChainStream(seed[:], "salt"), testConfig(), testBase)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	return m, testBase
}

type stubGame struct{ crash float64 }

func (s stubGame) ID() string { return "crash" }
func (s stubGame) Resolve(*fair.Stream) (game.RoundResult, error) {
	return game.RoundResult{Multiplier: s.crash}, nil
}
func (s stubGame) Phases() []game.Phase                                    { return nil }
func (s stubGame) SettleBet(game.RoundResult, game.RoundBet) (int64, error) { return 0, nil }
func (s stubGame) TheoreticalRTP() float64                                 { return 0.99 }

var testBase = time.Unix(1800000000, 0).UTC()

func TestPlaceBetPhaseGuard(t *testing.T) {
	m, _ := seedRound(t, 2.0)
	if err := m.AddBet(1, 100, 0, "p1"); err != nil {
		t.Fatalf("bet during betting_open: %v", err)
	}
	if err := m.AddBet(1, 100, 0, "p1"); err == nil {
		t.Fatal("duplicate bet accepted")
	}
	if err := m.AddBet(2, 100, 0, "p2"); err != nil {
		t.Fatalf("second player bet: %v", err)
	}
	// Step into locked: no more bets.
	now := testBase
	for !m.Done() {
		now = now.Add(200 * time.Millisecond)
		m.Step(now)
		if m.State() == "locked" {
			if err := m.AddBet(3, 100, 0, "p3"); err == nil {
				t.Fatal("bet accepted after betting closed")
			}
			break
		}
	}
}

// TestCashOutPaysOnceAtReceiptTime is the phase-14 gate: a duplicated
// cash-out message pays exactly once, at the server receipt-time multiplier.
func TestCashOutPaysOnceAtReceiptTime(t *testing.T) {
	m, base := seedRound(t, 5.0)
	if err := m.AddBet(1, 100, 0, "p1"); err != nil {
		t.Fatalf("bet: %v", err)
	}
	// Advance into running.
	now := base
	for m.State() != "running" && !m.Done() {
		now = now.Add(200 * time.Millisecond)
		m.Step(now)
	}
	if m.State() != "running" {
		t.Fatalf("state = %q", m.State())
	}
	// Advance ~1.16s on the display curve: e^(0.12*1.16) ≈ 1.15.
	now = now.Add(1160 * time.Millisecond)
	m.Step(now)

	paid1, err := m.CashOut(1, now)
	if err != nil {
		t.Fatalf("cash out: %v", err)
	}
	paid2, err := m.CashOut(1, now.Add(time.Second))
	if err == nil {
		t.Fatal("duplicate cash-out accepted")
	}
	if paid2 != 0 {
		t.Fatalf("duplicate cash-out paid %d", paid2)
	}
	// The recorded multiplier must not move with the later receipt time.
	settled := m.Settlements()
	if len(settled) != 1 || settled[0].PayoutCredits != paid1 {
		t.Fatalf("settlement %+v, first payout %d", settled, paid1)
	}
	if settled[0].MultiplierHundredths != paid1*100/100 {
		t.Fatalf("multiplier hundredths = %d", settled[0].MultiplierHundredths)
	}
}

// TestAutoCashoutEvaluatedServerSide pins the auto-cashout semantics: the
// target is evaluated against the resolved crash point, never against the
// display curve's real-time position.
func TestAutoCashoutEvaluatedServerSide(t *testing.T) {
	m, _ := seedRound(t, 2.0)
	if err := m.AddBet(1, 100, 200, "p1"); err != nil { // target 2.00
		t.Fatalf("bet: %v", err)
	}
	if err := m.AddBet(2, 100, 250, "p2"); err != nil { // target above crash
		t.Fatalf("bet: %v", err)
	}
	now := testBase
	for !m.Done() {
		now = now.Add(200 * time.Millisecond)
		m.Step(now)
	}
	settled := m.Settlements()
	if len(settled) != 2 {
		t.Fatalf("settled = %+v", settled)
	}
	if settled[0].PayoutCredits != 200 {
		t.Fatalf("player 1 paid %d, want 200 (2.00x)", settled[0].PayoutCredits)
	}
	if settled[1].PayoutCredits != 0 {
		t.Fatalf("player 2 paid %d, want 0", settled[1].PayoutCredits)
	}
}

func TestCashOutTooLateAfterCrash(t *testing.T) {
	m, base := seedRound(t, 1.20)
	if err := m.AddBet(1, 100, 0, "p1"); err != nil {
		t.Fatalf("bet: %v", err)
	}
	now := base
	for m.State() != "running" && !m.Done() {
		now = now.Add(200 * time.Millisecond)
		m.Step(now)
	}
	// Run well past the crash point.
	for !m.Done() {
		now = now.Add(200 * time.Millisecond)
		m.Step(now)
		if m.State() == "settled" {
			break
		}
	}
	if _, err := m.CashOut(1, now); err == nil {
		t.Fatal("cash-out accepted after crash")
	}
}
