// Package round owns the shared-round lifecycle: betting_open → locked →
// running → settled. The machine is a pure stepper — no goroutines, no
// sleeps, no I/O — advanced by Step(now) with an injectable Clock, so a
// full round replays deterministically under a fake clock in microseconds
// and a driver (gameserver loop or test) decides when time moves.
package round

import (
	"encoding/json"
	"math"
	"sync"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
)

// EventType strings emitted on the bus and to sockets.
const (
	EventStateChanged = "round_state"
	EventTick         = "round_tick"
	EventResult       = "round_result"
)

// Event is one state-machine emission.
type Event struct {
	Room    string
	Type    string
	Payload json.RawMessage
}

// Config holds the phase durations (from the game) and the tick cadence.
// Curve and RunningFor come from the game: the running-phase display curve
// and the duration that curve needs to reach a resolved multiplier.
type Config struct {
	BettingOpen time.Duration
	Locked      time.Duration
	Settled     time.Duration
	Tick        time.Duration
	MaxRunning  time.Duration // safety cap on the running phase
	Curve       func(elapsedSeconds float64) float64
	RunningFor  func(result game.RoundResult) time.Duration
}

// Machine is one round of a RoundGame in a room.
type Machine struct {
	room   string
	game   game.RoundGame
	result game.RoundResult
	cfg    Config

	stateMu    sync.RWMutex // guards state; runner writes, bet intake reads
	state      game.PhaseKind
	phaseStart time.Time
	runningDur time.Duration // running-phase length implied by the crash point
	tickSeq    uint64
	done       bool

	stakes *bets
}

// Start resolves the round (from the chain-derived stream) and opens
// betting at now.
func Start(room string, g game.RoundGame, stream *fair.Stream, cfg Config, now time.Time) (*Machine, error) {
	result, err := g.Resolve(stream)
	if err != nil {
		return nil, err
	}
	running := cfg.RunningFor(result)
	if running > cfg.MaxRunning {
		running = cfg.MaxRunning
	}
	return &Machine{
		room:       room,
		game:       g,
		result:     result,
		cfg:        cfg,
		state:      game.PhaseBettingOpen,
		phaseStart: now,
		runningDur: running,
		stakes:     newBets(),
	}, nil
}

// State returns the current phase.
func (m *Machine) State() game.PhaseKind {
	m.stateMu.RLock()
	defer m.stateMu.RUnlock()
	return m.state
}

// StateForStakes exposes the phase to the bet registry under its own lock.
func (m *Machine) StateForStakes() game.PhaseKind { return m.State() }

// Result returns the resolved round result.
func (m *Machine) Result() game.RoundResult { return m.result }

// Done reports whether the round has completed its settled phase.
func (m *Machine) Done() bool { return m.done }

// MultiplierAt returns the displayed multiplier during the running phase,
// clamped to the resolved crash point.
func (m *Machine) MultiplierAt(now time.Time) float64 {
	elapsed := now.Sub(m.phaseStart).Seconds()
	v := m.cfg.Curve(elapsed)
	return math.Min(v, m.result.Multiplier)
}

// Step advances the machine to now, emitting events for state transitions
// and (during running) multiplier ticks. Safe to call at any cadence; the
// machine never sleeps. Step is the single writer of state (runner
// goroutine); bet intake reads it concurrently.
func (m *Machine) Step(now time.Time) []Event {
	var events []Event
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	if m.done {
		return events
	}
	// Emit ticks while running (bounded by elapsed time, not call count).
	if m.state == game.PhaseRunning {
		events = append(events, m.emitTicks(now)...)
	}

	elapsed := now.Sub(m.phaseStart)
	var limit time.Duration
	switch m.state {
	case game.PhaseBettingOpen:
		limit = m.cfg.BettingOpen
	case game.PhaseLocked:
		limit = m.cfg.Locked
	case game.PhaseRunning:
		limit = m.runningDur
		if limit > m.cfg.MaxRunning {
			limit = m.cfg.MaxRunning
		}
	default: // settled
		limit = m.cfg.Settled
	}

	if elapsed < limit {
		return events
	}

	// Phase over: transition.
	next, ok := m.nextState()
	if !ok {
		m.done = true
		return events
	}
	m.state = next
	m.phaseStart = m.phaseStart.Add(limit)
	events = append(events, m.stateEvent(now))

	switch next {
	case game.PhaseRunning:
		// Ticks between phaseStart and now for the freshly-opened running
		// phase, then the result when it ends.
		events = append(events, m.emitTicks(now)...)
		if m.runningDur == 0 {
			events = append(events, m.resultEvent())
		}
	case game.PhaseSettled:
		events = append(events, m.resultEvent())
	}
	return events
}

func (m *Machine) nextState() (game.PhaseKind, bool) {
	switch m.state {
	case game.PhaseBettingOpen:
		return game.PhaseLocked, true
	case game.PhaseLocked:
		return game.PhaseRunning, true
	case game.PhaseRunning:
		return game.PhaseSettled, true
	case game.PhaseSettled:
		return "", false
	}
	return "", false
}

// emitTicks catches the tick sequence up to now.
func (m *Machine) emitTicks(now time.Time) []Event {
	var events []Event
	tick := m.cfg.Tick
	if tick <= 0 {
		tick = 100 * time.Millisecond
	}
	nextTick := m.phaseStart.Add(time.Duration(m.tickSeq+1) * tick)
	for !nextTick.After(now) && nextTick.Sub(m.phaseStart) < m.runningDur {
		m.tickSeq++
		payload, _ := json.Marshal(map[string]any{
			"multiplier": m.MultiplierAt(nextTick),
			"seq":        m.tickSeq,
		})
		events = append(events, Event{Room: m.room, Type: EventTick, Payload: payload})
		nextTick = m.phaseStart.Add(time.Duration(m.tickSeq+1) * tick)
	}
	return events
}

func (m *Machine) stateEvent(now time.Time) Event {
	// Caller holds stateMu (write in Step, read in InitialEvent).
	payload, _ := json.Marshal(map[string]any{
		"state":  string(m.state),
		"msLeft": m.phaseMsLeftLocked(now),
	})
	return Event{Room: m.room, Type: EventStateChanged, Payload: payload}
}

// InitialEvent returns the state event for the machine's opening phase.
// Step only broadcasts transitions, so without this the betting_open phase
// is invisible to connected clients until the switch to locked.
func (m *Machine) InitialEvent(now time.Time) Event {
	m.stateMu.RLock()
	defer m.stateMu.RUnlock()
	return m.stateEvent(now)
}

// PhaseMsLeft reports milliseconds remaining in the current phase so
// clients that join (or refresh) mid-phase can sync their countdowns.
func (m *Machine) PhaseMsLeft(now time.Time) int64 {
	m.stateMu.RLock()
	defer m.stateMu.RUnlock()
	return m.phaseMsLeftLocked(now)
}

// phaseMsLeftLocked is PhaseMsLeft without locking; callers hold stateMu.
func (m *Machine) phaseMsLeftLocked(now time.Time) int64 {
	var limit time.Duration
	switch m.state {
	case game.PhaseBettingOpen:
		limit = m.cfg.BettingOpen
	case game.PhaseLocked:
		limit = m.cfg.Locked
	case game.PhaseRunning:
		limit = m.runningDur
		if limit > m.cfg.MaxRunning {
			limit = m.cfg.MaxRunning
		}
	case game.PhaseSettled:
		limit = m.cfg.Settled
	}
	left := m.phaseStart.Add(limit).Sub(now)
	if left < 0 {
		left = 0
	}
	return left.Milliseconds()
}

func (m *Machine) resultEvent() Event {
	return Event{Room: m.room, Type: EventResult, Payload: m.result.Payload}
}
