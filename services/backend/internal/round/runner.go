package round

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/bus"
	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/pgtype"
)

// PlaceBetParams carries one stake to the persister. Spot is empty for
// crash-style bets; spot games pass their cell id and per-bet options.
type PlaceBetParams struct {
	UserID         int64
	RoundID        int64
	GameID         string
	Credits        int64
	AutoHundredths int64
	Spot           string
	Options        json.RawMessage
	IdempotencyKey string
}

// Persister records round transitions and moves money for bets. The
// production implementation writes Postgres; tests use the no-op.
type Persister interface {
	OpenRound(ctx context.Context, roomID int64, gameID string, chainSeedID int64, salt string) (int64, error)
	Transition(ctx context.Context, roundID int64, state string) error
	Settle(ctx context.Context, roundID int64, result game.RoundResult) error
	// PlaceBet debits the stake and inserts the bet row in one transaction,
	// returning the bet row id and post-debit balance.
	PlaceBet(ctx context.Context, arg PlaceBetParams) (int64, int64, error)
	// MarkCashout records the receipt-time multiplier on the bet row.
	MarkCashout(ctx context.Context, roundID, userID, hundredths int64) error
	// RefundBets returns a player's cleared stakes in one idempotent
	// transaction, returning the post-refund balance.
	RefundBets(ctx context.Context, roundID, userID, total int64, betIDs []int64) (int64, error)
	// SettleRound pays every winner in one atomic transaction.
	SettleRound(ctx context.Context, roundID int64, settled []SettledBet) error
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
		ChainSeedID: pgtype.Int8{Int64: chainSeedID, Valid: chainSeedID != 0},
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

func (NoopPersister) PlaceBet(context.Context, PlaceBetParams) (int64, int64, error) {
	return 1, 0, nil
}

func (NoopPersister) MarkCashout(context.Context, int64, int64, int64) error { return nil }

func (NoopPersister) RefundBets(context.Context, int64, int64, int64, []int64) (int64, error) {
	return 0, nil
}

func (NoopPersister) SettleRound(context.Context, int64, []SettledBet) error { return nil }

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

	liveMu   sync.RWMutex
	live     *Machine
	liveID   int64
	history  []float64 // last crash multipliers, most recent first
	minBet   int64
	maxBet   int64
	intakeMu sync.Mutex
}

func NewRunner(room string, roomID int64, g game.RoundGame, chain *fair.ChainService, persist Persister, b bus.Bus, clk clock.Clock, cfg Config, pool *pgxpool.Pool, logger *slog.Logger) *Runner {
	return &Runner{
		room: room, roomID: roomID, game: g, chain: chain,
		persist: persist, bus: b, clk: clk, cfg: cfg,
		store: store.New(pool), logger: logger,
	}
}

// Live returns the current round's id and machine, or (0, nil) between
// rounds. The machine pointer stays valid for the round's whole life.
func (r *Runner) Live() (int64, *Machine) {
	r.liveMu.RLock()
	defer r.liveMu.RUnlock()
	return r.liveID, r.live
}

// LiveState renders the live round for HTTP snapshots (deep links).
func (r *Runner) LiveState() map[string]any {
	id, m := r.Live()
	if m == nil {
		return nil
	}
	mult := 0.0
	if m.State() == game.PhaseRunning {
		mult = m.MultiplierAt(r.clk.Now())
	}
	r.liveMu.RLock()
	history := append([]float64(nil), r.history...)
	r.liveMu.RUnlock()
	return map[string]any{
		"roundId":       id,
		"state":         string(m.State()),
		"multiplier":    mult,
		"msLeft":        m.PhaseMsLeft(r.clk.Now()),
		"recentCrashes": history,
		"stakes":        m.StakesView(),
	}
}

// Limits returns the room's enforced bet range.
func (r *Runner) Limits() (min, max int64) { return r.minBet, r.maxBet }

// SetLimits configures the room's bet range (loaded from the rooms table).
func (r *Runner) SetLimits(min, max int64) { r.minBet, r.maxBet = min, max }

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
	r.liveMu.Lock()
	r.live, r.liveID = m, roundID
	r.liveMu.Unlock()
	// Announce betting_open: Step only broadcasts transitions, so without
	// an explicit opening event clients never see the betting phase begin
	// and keep rendering the previous round until locked.
	r.publish(m.InitialEvent(r.clk.Now()))
	defer func() {
		r.liveMu.Lock()
		r.live, r.liveID = nil, 0
		r.liveMu.Unlock()
	}()

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
					// Atomic batch settlement: every payout in one
					// transaction, wallets locked in sorted order.
					settled := make([]SettledBet, 0)
					for _, s := range m.Settlements() {
						settled = append(settled, SettledBet{
							BetID: s.BetID, UserID: s.UserID, Spot: s.Spot,
							PayoutCredits:        s.PayoutCredits,
							MultiplierHundredths: s.MultiplierHundredths,
						})
					}
					if err := r.persist.SettleRound(ctx, roundID, settled); err != nil {
						r.logger.Error("settle round", "room", r.room, "round", roundID, "err", err)
						return err
					}
					// History + per-player payouts for the clients. The
					// history projection is game-specific (crash records the
					// crash multiplier; roulette the winning pocket).
					hv := r.cfg.HistoryValue
					hvValue := m.Result().Multiplier
					if hv != nil {
						hvValue = hv(m.Result())
					}
					r.liveMu.Lock()
					r.history = append([]float64{hvValue}, r.history...)
					if len(r.history) > 10 {
						r.history = r.history[:10]
					}
					r.liveMu.Unlock()
					payouts := make([]map[string]any, 0, len(settled))
					for _, s := range settled {
						if s.PayoutCredits > 0 {
							entry := map[string]any{
								"userId": s.UserID, "payoutCredits": s.PayoutCredits,
							}
							if s.Spot != "" {
								entry["spot"] = s.Spot
							}
							payouts = append(payouts, entry)
						}
					}
					raw, _ := json.Marshal(map[string]any{"payouts": payouts})
					r.publish(Event{Room: r.room, Type: "round_settlements", Payload: raw})
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

