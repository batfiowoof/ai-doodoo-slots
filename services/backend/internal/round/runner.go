package round

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/bus"
	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Persister records round transitions. The production implementation writes
// the rounds table; tests use a no-op.
type Persister interface {
	OpenRound(ctx context.Context, roomID int64, gameID string, chainSeedID int64, salt string) (int64, error)
	Transition(ctx context.Context, roundID int64, state string) error
	Settle(ctx context.Context, roundID int64, result game.RoundResult) error
}

// pgPersister persists rounds to Postgres.
type pgPersister struct {
	pool *pgxpool.Pool
}

// NewPersister returns the Postgres-backed Persister.
func NewPersister(pool *pgxpool.Pool) Persister { return &pgPersister{pool: pool} }

func (p *pgPersister) OpenRound(ctx context.Context, roomID int64, gameID string, chainSeedID int64, salt string) (int64, error) {
	return store.New(p.pool).CreateRound(ctx, store.CreateRoundParams{
		RoomID:      pgtype.Int8{Int64: roomID, Valid: true},
		GameID:      gameID,
		ChainSeedID: pgtype.Int8{Int64: chainSeedID, Valid: true},
		Salt:        salt,
	})
}

func (p *pgPersister) Transition(ctx context.Context, roundID int64, state string) error {
	return store.New(p.pool).SetRoundState(ctx, store.SetRoundStateParams{ID: roundID, State: state})
}

func (p *pgPersister) Settle(ctx context.Context, roundID int64, result game.RoundResult) error {
	return store.New(p.pool).SetRoundResult(ctx, store.SetRoundResultParams{ID: roundID, Result: result.Payload})
}

// NoopPersister is the test double.
type NoopPersister struct{}

func (NoopPersister) OpenRound(context.Context, int64, string, int64, string) (int64, error) {
	return 1, nil
}

func (NoopPersister) Transition(context.Context, int64, string) error { return nil }

func (NoopPersister) Settle(context.Context, int64, game.RoundResult) error { return nil }

// Runner drives one room's round loop forever: it commits the next chain
// link at round open, runs the state machine, reveals and verifies the seed
// at settle, persists transitions, and publishes every event on the bus for
// the hub to fan out to the room's sockets. One runner per room is the
// single writer for that room's rounds.
type Runner struct {
	room    string
	roomID  int64
	game    game.RoundGame
	chain   *fair.ChainService
	persist Persister
	bus     bus.Bus
	clk     clock.Clock
	cfg     Config
	store   *store.Queries
	logger  *slog.Logger
}

func NewRunner(room string, roomID int64, g game.RoundGame, chain *fair.ChainService, persist Persister, b bus.Bus, clk clock.Clock, cfg Config, pool *pgxpool.Pool, logger *slog.Logger) *Runner {
	return &Runner{
		room: room, roomID: roomID, game: g, chain: chain,
		persist: persist, bus: b, clk: clk, cfg: cfg,
		store: store.New(pool), logger: logger,
	}
}

// Run loops rounds until the context is cancelled.
func (r *Runner) Run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		if err := r.runRound(ctx); err != nil {
			r.logger.Error("round loop", "room", r.room, "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}
}

func (r *Runner) runRound(ctx context.Context) error {
	if err := r.chain.EnsureChain(ctx); err != nil {
		return err
	}
	// Commit: the next unrevealed link binds to this round at open. Only
	// its hash is public; the plain seed steers the machine server-side.
	link, err := r.store.GetNextUnrevealedChainSeed(ctx)
	if err != nil {
		return err
	}
	seedPlain, err := hex.DecodeString(link.SeedPlain.String)
	if err != nil {
		return err
	}
	roundID, err := r.persist.OpenRound(ctx, r.roomID, r.game.ID(), link.ID, "")
	if err != nil {
		return err
	}

	m, err := Start(r.room, r.game, fair.NewChainStream(seedPlain, ""), r.cfg, r.clk.Now())
	if err != nil {
		return err
	}

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-ticker.C:
			for _, ev := range m.Step(now) {
				if ev.Type == EventStateChanged {
					var p struct {
						State string `json:"state"`
					}
					_ = json.Unmarshal(ev.Payload, &p)
					if err := r.persist.Transition(ctx, roundID, p.State); err != nil {
						r.logger.Warn("persist transition", "err", err)
					}
				}
				if ev.Type == EventResult {
					// Reveal + verify the committed link before the result
					// leaves the server.
					if _, _, rerr := r.chain.RevealNext(ctx); rerr != nil {
						return rerr
					}
					if serr := r.persist.Settle(ctx, roundID, m.Result()); serr != nil {
						return serr
					}
				}
				r.publish(ev)
			}
			if m.Done() {
				return nil
			}
		}
	}
}

func (r *Runner) publish(ev Event) {
	r.bus.Publish(bus.Event{Topic: "rooms", Room: ev.Room, Type: ev.Type, Payload: ev.Payload})
}
