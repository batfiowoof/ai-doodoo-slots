package round

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
)

// spotStubGame settles spot bets with fixed roulette-ish math so the
// machine-level spot path is testable without importing the engine.
type spotStubGame struct{}

func (spotStubGame) ID() string { return "roulette-stub" }
func (spotStubGame) Resolve(*fair.Stream) (game.RoundResult, error) {
	payload, _ := json.Marshal(map[string]any{"pocket": 17})
	return game.RoundResult{Multiplier: 0, Payload: payload}, nil
}
func (spotStubGame) Phases() []game.Phase { return nil }
func (spotStubGame) SettleBet(r game.RoundResult, b game.RoundBet) (int64, error) {
	var p struct {
		Pocket int `json:"pocket"`
	}
	if err := json.Unmarshal(r.Payload, &p); err != nil {
		return 0, err
	}
	var o struct {
		Spot string `json:"spot"`
	}
	if err := json.Unmarshal(b.Options, &o); err != nil {
		return 0, err
	}
	if o.Spot == "black" && p.Pocket == 17 {
		return b.BetCredits * 2, nil
	}
	if o.Spot == "n17" {
		return b.BetCredits * 36, nil
	}
	return 0, nil
}
func (spotStubGame) TheoreticalRTP() float64 { return 36.0 / 37.0 }

func spotTestConfig() Config {
	cfg := testConfig()
	cfg.SpotSettle = true
	return cfg
}

func startSpotRound(t *testing.T) (*Machine, time.Time) {
	t.Helper()
	var seed [32]byte
	seed[0] = 0xDD
	m, err := Start("roulette-1", spotStubGame{}, fair.NewChainStream(seed[:], "salt"), spotTestConfig(), testBase)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	return m, testBase
}

// TestSpotBetsMultiplePerPlayer pins the roulette bet shape: one player
// holds chips on several cells, each settles independently against the one
// shared pocket, and the same cell holds exactly one open chip.
func TestSpotBetsMultiplePerPlayer(t *testing.T) {
	m, _ := startSpotRound(t)
	red, _ := json.Marshal(map[string]string{"spot": "red"})
	straight, _ := json.Marshal(map[string]string{"spot": "n17"})
	again, _ := json.Marshal(map[string]string{"spot": "n17"})

	if err := m.AddSpotBet(1, 50, "red", red, "p1"); err != nil {
		t.Fatalf("red bet: %v", err)
	}
	if err := m.AddSpotBet(1, 10, "n17", straight, "p1"); err != nil {
		t.Fatalf("straight bet: %v", err)
	}
	if err := m.AddSpotBet(1, 10, "n17", again, "p1"); err == nil {
		t.Fatal("duplicate spot accepted")
	}
	if err := m.AddSpotBet(2, 25, "black", []byte(`{"spot":"black"}`), "p2"); err != nil {
		t.Fatalf("second player: %v", err)
	}
	if got := m.UserStakeTotal(1); got != 60 {
		t.Fatalf("player 1 total = %d, want 60", got)
	}

	// Pocket 17 (black): the straight pays 36x, red and black pay 2x.
	now := testBase
	for !m.Done() {
		now = now.Add(200 * time.Millisecond)
		m.Step(now)
	}
	settled := m.Settlements()
	if len(settled) != 3 {
		t.Fatalf("settled = %d entries, want 3", len(settled))
	}
	bySpot := make(map[string]int64, len(settled))
	for _, s := range settled {
		if s.UserID != 1 {
			continue
		}
		bySpot[s.Spot] = s.PayoutCredits
	}
	if bySpot["n17"] != 360 {
		t.Fatalf("straight paid %d, want 360", bySpot["n17"])
	}
	if bySpot["red"] != 0 {
		t.Fatalf("red paid %d, want 0 (17 is black)", bySpot["red"])
	}
	for _, s := range settled {
		if s.UserID == 2 && s.PayoutCredits != 50 {
			t.Fatalf("player 2 paid %d, want 50", s.PayoutCredits)
		}
	}
}

// TestClearBetsDropsAndRefunds pins the CLEAR semantics: every chip lifts
// off the board at once, a second clear is a no-op, and cleared spots can
// be bet again in the same round.
func TestClearBetsDropsAndRefunds(t *testing.T) {
	m, _ := startSpotRound(t)
	if err := m.AddSpotBet(1, 50, "red", []byte(`{"spot":"red"}`), "p1"); err != nil {
		t.Fatalf("bet: %v", err)
	}
	if err := m.AddSpotBet(1, 10, "n17", []byte(`{"spot":"n17"}`), "p1"); err != nil {
		t.Fatalf("bet: %v", err)
	}
	removed := m.ClearBets(1)
	if len(removed) != 2 {
		t.Fatalf("cleared %d bets, want 2", len(removed))
	}
	if m.UserStakeTotal(1) != 0 {
		t.Fatalf("stake total = %d after clear", m.UserStakeTotal(1))
	}
	if again := m.ClearBets(1); len(again) != 0 {
		t.Fatalf("second clear returned %d bets", len(again))
	}
	if err := m.AddSpotBet(1, 10, "red", []byte(`{"spot":"red"}`), "p1"); err != nil {
		t.Fatalf("re-bet after clear: %v", err)
	}
	now := testBase
	for !m.Done() {
		now = now.Add(200 * time.Millisecond)
		m.Step(now)
	}
	settled := m.Settlements()
	if len(settled) != 1 || settled[0].Spot != "red" || settled[0].PayoutCredits != 0 {
		t.Fatalf("settled = %+v, want a single losing red bet", settled)
	}
}

// TestSpotBetsRejectCrashCashOut guards the phase boundary: spot games have
// no manual cash-out — their bets ride to settlement.
func TestSpotBetsRejectCrashCashOut(t *testing.T) {
	m, base := startSpotRound(t)
	if err := m.AddSpotBet(1, 50, "red", []byte(`{"spot":"red"}`), "p1"); err != nil {
		t.Fatalf("bet: %v", err)
	}
	now := base
	for m.State() != "running" && !m.Done() {
		now = now.Add(200 * time.Millisecond)
		m.Step(now)
	}
	if _, err := m.CashOut(1, now); err == nil {
		t.Fatal("cash-out accepted on a spot game")
	}
}
