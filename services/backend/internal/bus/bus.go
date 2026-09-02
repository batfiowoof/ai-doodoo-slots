// Package bus publishes events between components that must not know each
// other: the gameserver publishes round transitions, the auth path publishes
// session revocations, and sockets/lobbies subscribe. This in-process
// implementation is the phase-12 shape; the Redis swap when there is more
// than one gameserver replaces the constructor, not the callers.
package bus

import (
	"sync"
)

// Event is a message on the bus. Topic selects the audience
// ("room:<slug>", "lobby", "session", "user"); Payload is JSON.
type Event struct {
	Topic   string
	Type    string
	Payload []byte
}

// Subscription receives matching events. Close stops delivery.
type Subscription struct {
	ch     <-chan Event
	cancel func()
}

// Chan exposes the event channel. Events are dropped, never blocked on:
// a slow subscriber must not stall a publisher.
func (s *Subscription) Chan() <-chan Event { return s.ch }

// Close stops the subscription and drains its slot.
func (s *Subscription) Close() { s.cancel() }

// Bus is the eventPublisher contract.
type Bus interface {
	Publish(ev Event)
	Subscribe(topics ...string) *Subscription
}

// MemoryBus is the in-process implementation. Each subscriber owns a
// bounded channel; publishes never block and overflow drops events for
// that subscriber only.
type MemoryBus struct {
	mu   sync.RWMutex
	next int
	subs map[int]*memSub
}

type memSub struct {
	topics map[string]bool
	ch     chan Event
}

func NewMemoryBus() *MemoryBus {
	return &MemoryBus{subs: make(map[int]*memSub)}
}

const subBuffer = 128

func (b *MemoryBus) Subscribe(topics ...string) *Subscription {
	ch := make(chan Event, subBuffer)
	s := &memSub{topics: make(map[string]bool, len(topics)), ch: ch}
	for _, t := range topics {
		s.topics[t] = true
	}

	b.mu.Lock()
	id := b.next
	b.next++
	b.subs[id] = s
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		delete(b.subs, id)
		b.mu.Unlock()
	}
	return &Subscription{ch: ch, cancel: cancel}
}

func (b *MemoryBus) Publish(ev Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, s := range b.subs {
		if !s.topics[ev.Topic] {
			continue
		}
		select {
		case s.ch <- ev:
		default:
			// Slow subscriber: drop rather than block the publisher.
		}
	}
}
