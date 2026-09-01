// Package game defines the engine interface and registry. Adding a game
// means implementing Game, registering it, and adding a renderer on the web
// side; the wallet, fairness, history, and API surface are untouched.
package game

import (
	"encoding/json"
	"sort"
	"sync"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
)

// Outcome is what a game returns per bet. Payload is game-specific and is
// rendered by the client; the client never computes payouts.
type Outcome struct {
	PayoutCredits int64
	Payload       json.RawMessage
}

// Game is the single-player engine contract. Play must be pure and
// deterministic given the stream — no clocks, no globals, no math/rand.
// That property is what makes the RTP test and client-side verification
// possible.
type Game interface {
	ID() string
	ValidateBet(credits int64) error
	Play(s *fair.Stream, betCredits int64) (Outcome, error)
	TheoreticalRTP() float64
}

// Registry maps game IDs to implementations.
type Registry struct {
	mu    sync.RWMutex
	games map[string]Game
}

func NewRegistry() *Registry {
	return &Registry{games: make(map[string]Game)}
}

// Register adds a game; panics on duplicate IDs (programming error at boot).
func (r *Registry) Register(g Game) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.games[g.ID()]; exists {
		panic("game already registered: " + g.ID())
	}
	r.games[g.ID()] = g
}

// Get resolves a game ID.
func (r *Registry) Get(id string) (Game, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	g, ok := r.games[id]
	return g, ok
}

// List returns all games with stable (sorted) ordering.
func (r *Registry) List() []Game {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.games))
	for id := range r.games {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Game, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.games[id])
	}
	return out
}
