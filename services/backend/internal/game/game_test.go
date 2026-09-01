package game

import (
	"testing"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
)

type stubGame struct{ id string }

func (s *stubGame) ID() string                                { return s.id }
func (s *stubGame) ValidateBet(credits int64) error           { return nil }
func (s *stubGame) Play(_ *fair.Stream, _ int64) (Outcome, error) { return Outcome{}, nil }
func (s *stubGame) TheoreticalRTP() float64                   { return 0.98 }

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubGame{id: "slots"})
	r.Register(&stubGame{id: "crash"})
	r.Register(&stubGame{id: "dice"})

	got, ok := r.Get("slots")
	if !ok || got.ID() != "slots" {
		t.Fatalf("Get(slots) = %v, %v", got, ok)
	}
	if _, ok := r.Get("missing"); ok {
		t.Fatal("Get returned game for unknown id")
	}

	ids := make([]string, 0)
	for _, g := range r.List() {
		ids = append(ids, g.ID())
	}
	want := []string{"crash", "dice", "slots"}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("List order = %v, want %v", ids, want)
		}
	}
}

func TestRegistryDuplicatePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate registration did not panic")
		}
	}()
	r := NewRegistry()
	r.Register(&stubGame{id: "slots"})
	r.Register(&stubGame{id: "slots"})
}
