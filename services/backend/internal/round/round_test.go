package round

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
	"github.com/ai-doodoo-slots/services/backend/internal/game/crash"
)

func testConfig() Config {
	return Config{
		BettingOpen: 7 * time.Second,
		Locked:      1 * time.Second,
		Settled:     4 * time.Second,
		Tick:        100 * time.Millisecond,
		MaxRunning:  90 * time.Second,
		Curve:       crash.MultiplierAt,
		RunningFor:  crash.RunningFor,
	}
}

func startRound(t *testing.T, room string, now time.Time) *Machine {
	t.Helper()
	var seed [32]byte
	seed[0] = 0xAB
	m, err := Start(room, crash.New(), fair.NewChainStream(seed[:], "salt"), testConfig(), now)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	return m
}

func runRound(t *testing.T, m *Machine, start time.Time, step time.Duration) []Event {
	t.Helper()
	var events []Event
	now := start
	for !m.Done() {
		now = now.Add(step)
		events = append(events, m.Step(now)...)
		if time.Duration(now.Sub(start)/time.Hour) > 24*time.Hour {
			t.Fatal("round never finished")
		}
	}
	return events
}

// TestFullRoundDeterministicUnderFakeClock is the phase-13 gate: a full
// round simulated under a fake clock is deterministic and completes in
// under a millisecond.
func TestFullRoundDeterministicUnderFakeClock(t *testing.T) {
	start := (clock.Real{}).Now()
	step := 50 * time.Millisecond

	// Two runs with identical inputs must produce identical event streams.
	m1 := startRound(t, "crash-1", start)
	ev1 := runRound(t, m1, start, step)
	m2 := startRound(t, "crash-1", start)
	ev2 := runRound(t, m2, start, step)

	raw1, _ := json.Marshal(ev1)
	raw2, _ := json.Marshal(ev2)
	if !bytes.Equal(raw1, raw2) {
		t.Fatal("event streams diverged for identical inputs")
	}

	// Phase sequence: betting_open → locked → running → settled.
	var states []string
	for _, ev := range ev1 {
		if ev.Type == EventStateChanged {
			var p struct {
				State string `json:"state"`
			}
			if json.Unmarshal(ev.Payload, &p) == nil {
				states = append(states, p.State)
			}
		}
	}
	want := []string{"locked", "running", "settled"}
	if len(states) < len(want) {
		t.Fatalf("states = %v", states)
	}
	for i, w := range want {
		if states[i] != w {
			t.Fatalf("state[%d] = %q, want %q (all: %v)", i, states[i], w, states)
		}
	}

	// Result event carries the resolved crash multiplier.
	var lastResult game.RoundResult
	found := false
	for _, ev := range ev1 {
		if ev.Type == EventResult {
			lastResult = m1.Result()
			found = true
		}
	}
	if !found {
		t.Fatal("no result event")
	}
	if lastResult.Multiplier < 1.00 {
		t.Fatalf("multiplier = %v", lastResult.Multiplier)
	}

	// Ticks are monotonic during running.
	prev := 0.0
	for _, ev := range ev1 {
		if ev.Type != EventTick {
			continue
		}
		var p struct {
			Multiplier float64 `json:"multiplier"`
		}
		if json.Unmarshal(ev.Payload, &p) == nil {
			if p.Multiplier < prev {
				t.Fatalf("ticks not monotonic: %v after %v", p.Multiplier, prev)
			}
			prev = p.Multiplier
		}
	}
}

func TestFullRoundSimulatesInUnderMillisecond(t *testing.T) {
	start := (clock.Real{}).Now()
	const n = 200
	for i := 0; i < n; i++ {
		m := startRound(t, "crash-1", start)
		runRound(t, m, start, 50*time.Millisecond)
	}
	elapsed := (clock.Real{}).Now().Sub(start)
	perRound := elapsed / n
	if perRound > time.Millisecond {
		t.Fatalf("average full round sim = %v, want < 1ms", perRound)
	}
}
