package roulette

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
)

// resolvedFor drives Resolve against a fixed seed and returns the payload.
func resolvedFor(t *testing.T, seedByte byte) (game.RoundResult, resultPayload) {
	t.Helper()
	r := New()
	var seed [32]byte
	seed[0] = seedByte
	res, err := r.Resolve(fair.NewChainStream(seed[:], "salt"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	var p resultPayload
	if err := json.Unmarshal(res.Payload, &p); err != nil {
		t.Fatalf("payload: %v", err)
	}
	return res, p
}

func TestResolveDeterministicAndBounded(t *testing.T) {
	res1, p1 := resolvedFor(t, 0x42)
	res2, p2 := resolvedFor(t, 0x42)
	if !bytes.Equal(res1.Payload, res2.Payload) {
		t.Fatal("same stream resolved different pockets")
	}
	if p1.Pocket != p2.Pocket || p1.WheelIndex != p2.WheelIndex {
		t.Fatalf("payloads diverge: %+v vs %+v", p1, p2)
	}
	if p1.Pocket < 0 || p1.Pocket > 36 {
		t.Fatalf("pocket out of range: %d", p1.Pocket)
	}
	if WheelOrder[p1.WheelIndex] != p1.Pocket {
		t.Fatalf("wheel index %d holds %d, payload says %d", p1.WheelIndex, WheelOrder[p1.WheelIndex], p1.Pocket)
	}
	if p1.Color != PocketColor(p1.Pocket) {
		t.Fatalf("color %q disagrees with pocket %d", p1.Color, p1.Pocket)
	}
}

// TestResolveCoversAllPockets draws a whole chain-worth of rounds and
// expects every pocket to land at least once — a stuck wheel would be
// caught here long before players notice.
func TestResolveCoversAllPockets(t *testing.T) {
	r := New()
	seed := make([]byte, 32)
	seen := make(map[int]bool)
	for round := 0; round < 2000; round++ {
		seed[1] = byte(round)
		seed[2] = byte(round >> 8)
		res, err := r.Resolve(fair.NewChainStream(seed, "coverage"))
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		var p resultPayload
		if err := json.Unmarshal(res.Payload, &p); err != nil {
			t.Fatalf("payload: %v", err)
		}
		seen[p.Pocket] = true
	}
	if len(seen) != PocketCount {
		t.Fatalf("covered %d of %d pockets in 2000 spins", len(seen), PocketCount)
	}
}

func spotBet(spot string, credits int64) game.RoundBet {
	raw, _ := json.Marshal(betOptions{Spot: spot})
	return game.RoundBet{BetCredits: credits, Options: raw}
}

func resultWithPocket(t *testing.T, pocket int) game.RoundResult {
	t.Helper()
	payload, err := json.Marshal(resultPayload{Pocket: pocket, Color: PocketColor(pocket)})
	if err != nil {
		t.Fatalf("payload: %v", err)
	}
	return game.RoundResult{Multiplier: 0, Payload: payload}
}

// TestSettleBetAcrossSpots pins the payout table on pocket 17 (black, odd,
// 1-18, 2nd dozen, 2nd column) and on the zero, which kills every outside
// bet while paying its own straight.
func TestSettleBetAcrossSpots(t *testing.T) {
	r := New()
	cases := []struct {
		pocket int
		spot   string
		stake  int64
		want   int64
	}{
		{17, "n17", 10, 360},  // straight hit: 35:1 + stake
		{17, "n16", 10, 0},    // straight miss
		{17, "black", 25, 50}, // even-money hit
		{17, "red", 25, 0},
		{17, "odd", 25, 50},
		{17, "even", 25, 0},
		{17, "low", 25, 50},
		{17, "high", 25, 0},
		{17, "d2", 10, 30}, // dozen hit: 2:1 + stake
		{17, "d1", 10, 0},
		{17, "d3", 10, 0},
		{17, "c2", 10, 30}, // column hit
		{17, "c1", 10, 0},
		{17, "c3", 10, 0},
		{0, "n0", 10, 360},    // zero straight
		{0, "red", 25, 0},     // zero kills every outside bet
		{0, "black", 25, 0},   // even 0 is green
		{0, "even", 25, 0},    // 0 is neither even…
		{0, "odd", 25, 0},     // …nor odd
		{0, "low", 25, 0},     // …nor in 1-18…
		{0, "high", 25, 0},    // …nor 19-36
		{0, "d1", 10, 0},      // nor in any dozen/column
		{0, "c1", 10, 0},      // (0 % 3 == 0, but the zero belongs
		{36, "high", 25, 50},  // boundary: high wins
		{36, "low", 25, 0},    // low loses
		{36, "c3", 10, 30},    // 36 sits in the 3rd column
		{1, "low", 25, 50},    // boundary: low wins
		{1, "d1", 10, 30},     // 1st dozen opens at 1
		{18, "low", 25, 50},   // low closes at 18
		{19, "high", 25, 50},  // high opens at 19
		{12, "d1", 10, 30},    // 1st dozen closes at 12
		{13, "d2", 10, 30},    // 2nd dozen opens at 13
		{34, "red", 25, 50},   // highest red
		{36, "even", 25, 50},  // highest even
	}
	for _, tc := range cases {
		name := fmt.Sprintf("pocket %d/%s", tc.pocket, tc.spot)
		t.Run(name, func(t *testing.T) {
			got, err := r.SettleBet(resultWithPocket(t, tc.pocket), spotBet(tc.spot, tc.stake))
			if err != nil {
				t.Fatalf("settle: %v", err)
			}
			if got != tc.want {
				t.Fatalf("payout = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestSettleBetRejectsBadSpots(t *testing.T) {
	r := New()
	res := resultWithPocket(t, 17)
	if _, err := r.SettleBet(res, game.RoundBet{BetCredits: 10, Options: []byte(`{"spot":"n99"}`)}); err == nil {
		t.Fatal("unknown spot accepted")
	}
	if _, err := r.SettleBet(res, game.RoundBet{BetCredits: 10, Options: []byte(`{}`)}); err == nil {
		t.Fatal("empty spot accepted")
	}
}

func TestValidateSpot(t *testing.T) {
	for _, s := range Spots() {
		if err := ValidateSpot(s.ID); err != nil {
			t.Fatalf("spot %q rejected: %v", s.ID, err)
		}
	}
	for _, bad := range []string{"", "n37", "d0", "d4", "c4", "RED", "straight", "n00"} {
		if err := ValidateSpot(bad); err == nil {
			t.Fatalf("invalid spot %q accepted", bad)
		}
	}
}

// TestEverySpotPaysExpectedRTP pins the uniform house edge: each spot's
// expected return is Payout × hits / 37 = 36/37.
func TestEverySpotPaysExpectedRTP(t *testing.T) {
	want := 36.0 / 37.0
	if got := New().TheoreticalRTP(); got != want {
		t.Fatalf("engine RTP = %v, want %v", got, want)
	}
	for _, s := range Spots() {
		hits := 0
		for p := 0; p <= 36; p++ {
			if s.Hits(p) {
				hits++
			}
		}
		rtp := float64(s.Payout) * float64(hits) / 37.0
		if rtp != want {
			t.Fatalf("spot %q RTP = %v, want %v", s.ID, rtp, want)
		}
	}
}

func TestHistoryValueExtractsPocket(t *testing.T) {
	res, p := resolvedFor(t, 0x7E)
	if got := HistoryValue(res); got != float64(p.Pocket) {
		t.Fatalf("history value = %v, want %v", got, p.Pocket)
	}
}
