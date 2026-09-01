package clock

import (
	"testing"
	"time"
)

func TestFakeClockAdvance(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := NewFake(start)

	if got := f.Now(); !got.Equal(start) {
		t.Fatalf("Now() = %v, want %v", got, start)
	}

	f.Advance(30 * time.Second)
	if got := f.Now(); !got.Equal(start.Add(30 * time.Second)) {
		t.Fatalf("after Advance: Now() = %v, want %v", got, start.Add(30*time.Second))
	}

	f.Set(start.Add(time.Hour))
	if got := f.Now(); !got.Equal(start.Add(time.Hour)) {
		t.Fatalf("after Set: Now() = %v, want %v", got, start.Add(time.Hour))
	}
}

func TestRealClockMonotonic(t *testing.T) {
	var c Clock = Real{}
	first := c.Now()
	time.Sleep(2 * time.Millisecond)
	if !c.Now().After(first) {
		t.Fatal("Real clock did not advance")
	}
}
