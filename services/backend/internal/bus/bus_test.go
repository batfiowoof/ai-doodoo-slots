package bus

import (
	"testing"
	"time"
)

func TestSubscribeReceivesMatchingTopicsOnly(t *testing.T) {
	b := NewMemoryBus()
	room := b.Subscribe("room:crash-1")
	lobby := b.Subscribe("lobby")

	b.Publish(Event{Topic: "room:crash-1", Type: "round_tick", Payload: []byte(`{}`)})
	b.Publish(Event{Topic: "lobby", Type: "lobby_summary", Payload: []byte(`{}`)})
	b.Publish(Event{Topic: "room:other", Type: "round_tick", Payload: []byte(`{}`)})

	got := 0
	deadline := time.After(time.Second)
	for got < 2 {
		select {
		case ev := <-room.Chan():
			if ev.Topic != "room:crash-1" {
				t.Errorf("room sub got topic %q", ev.Topic)
			}
			got++
		case ev := <-lobby.Chan():
			if ev.Topic != "lobby" {
				t.Errorf("lobby sub got topic %q", ev.Topic)
			}
			got++
		case <-deadline:
			t.Fatalf("timed out with %d events", got)
		}
	}
	// The other-room event must not have been delivered to either.
	select {
	case ev := <-room.Chan():
		t.Errorf("unexpected event %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestCloseStopsDelivery(t *testing.T) {
	b := NewMemoryBus()
	s := b.Subscribe("lobby")
	s.Close()

	b.Publish(Event{Topic: "lobby", Type: "x"})
	select {
	case ev := <-s.Chan():
		t.Errorf("closed subscription delivered %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSlowSubscriberDoesNotBlockPublisher(t *testing.T) {
	b := NewMemoryBus()
	_ = b.Subscribe("room:x") // never drained

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < subBuffer*3; i++ {
			b.Publish(Event{Topic: "room:x", Type: "tick"})
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publish blocked on a full subscriber")
	}
}
