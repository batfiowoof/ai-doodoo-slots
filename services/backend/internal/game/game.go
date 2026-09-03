// Package game defines the engine interface and registry. Adding a game
// means implementing Game, registering it, and adding a renderer on the web
// side; the wallet, fairness, history, and API surface are untouched.
package game

import (
	"encoding/json"
	"sort"
	"sync"
	"time"

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
	// listings are metadata-only entries for games that are not single-call
	// instant engines (blackjack deals and takes actions over its own
	// endpoints). They appear in GET /api/v1/games but never in Get().
	listings map[string]Listing
}

// Listing is one entry of the games listing, unified across engine kinds.
type Listing struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	TheoreticalRTP float64 `json:"theoreticalRtp"`
	Paytable       any     `json:"paytable,omitempty"`
	BetSteps       []int64 `json:"betSteps,omitempty"`
	Kind           string  `json:"kind"` // "instant" or "stateful"
}

// PhaseKind is one state of a shared round.
type PhaseKind string

const (
	PhaseBettingOpen PhaseKind = "betting_open"
	PhaseLocked      PhaseKind = "locked"
	PhaseRunning     PhaseKind = "running"
	PhaseSettled     PhaseKind = "settled"
)

// Phase is one state of the round state machine and how long it lasts.
type Phase struct {
	Kind     PhaseKind
	Duration time.Duration
}

// RoundResult is what a shared round resolved to. Payload is game-specific
// and rendered by the client.
type RoundResult struct {
	// Multiplier is the round's canonical outcome (1.0+ for crash-style
	// games); games that do not use multipliers report 0.
	Multiplier float64
	Payload    json.RawMessage
}

// RoundBet is a player's stake in a shared round plus their declared
// per-round options (e.g. a crash auto-cashout target), set at bet time.
type RoundBet struct {
	BetCredits int64
	Options    json.RawMessage
}

// RoundGame is the shared-round engine contract. Resolve and SettleBet must
// be pure and deterministic given the stream/result, so a whole round can be
// replayed from its seed and inputs for auditing or dispute.
type RoundGame interface {
	ID() string
	// Phases returns the state machine in transition order with durations.
	Phases() []Phase
	// Resolve derives the round outcome from the chain-derived stream.
	Resolve(s *fair.Stream) (RoundResult, error)
	// SettleBet computes a player's payout (0 when the bet loses) from the
	// round result.
	SettleBet(r RoundResult, b RoundBet) (int64, error)
	TheoreticalRTP() float64
}

func NewRegistry() *Registry {
	return &Registry{games: make(map[string]Game), listings: make(map[string]Listing)}
}

// Register adds a game; panics on duplicate IDs (programming error at boot).
func (r *Registry) Register(g Game) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.games[g.ID()]; exists {
		panic("game already registered: " + g.ID())
	}
	if _, exists := r.listings[g.ID()]; exists {
		panic("game already registered as listing: " + g.ID())
	}
	r.games[g.ID()] = g
}

// RegisterListing adds a metadata-only entry for a game whose flow has its
// own endpoints (stateful card games); panics on duplicate IDs at boot.
func (r *Registry) RegisterListing(l Listing) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.listings[l.ID]; exists {
		panic("listing already registered: " + l.ID)
	}
	if _, exists := r.games[l.ID]; exists {
		panic("game already registered: " + l.ID)
	}
	r.listings[l.ID] = l
}

// Get resolves a game ID.
func (r *Registry) Get(id string) (Game, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	g, ok := r.games[id]
	return g, ok
}

// List returns all instant games with stable (sorted) ordering.
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

// Listings returns every game — instant engines and metadata-only entries —
// in one sorted list for the games endpoint and the arcade floor.
func (r *Registry) Listings() []Listing {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Listing, 0, len(r.games)+len(r.listings))
	for id, g := range r.games {
		name := id
		if d, ok := g.(interface{ DisplayName() string }); ok {
			name = d.DisplayName()
		}
		l := Listing{ID: id, Name: name, TheoreticalRTP: g.TheoreticalRTP(), Kind: "instant"}
		if p, ok := g.(interface{ Paytable() any }); ok {
			l.Paytable = p.Paytable()
			if m, ok := l.Paytable.(map[string]any); ok {
				if bs, ok := m["betSteps"].([]int64); ok {
					l.BetSteps = bs
				}
			}
		}
		out = append(out, l)
	}
	for _, l := range r.listings {
		if l.Kind == "" {
			l.Kind = "stateful"
		}
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
