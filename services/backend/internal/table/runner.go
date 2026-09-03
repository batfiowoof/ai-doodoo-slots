package table

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/bus"
	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game/poker"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Config tunes the table loop.
type Config struct {
	TurnTimeout   time.Duration // auto check/fold when a turn idles
	InterHand     time.Duration // pause between hands
	Tick          time.Duration // loop resolution
	MinBuyInMult  int64         // min buy-in as a multiple of the big blind
}

// DefaultConfig carries the house timings.
func DefaultConfig() Config {
	return Config{TurnTimeout: 20 * time.Second, InterHand: 5 * time.Second, Tick: 100 * time.Millisecond, MinBuyInMult: 20}
}

// request is one serialized ask routed into the runner goroutine. The resp
// channel (buffered) carries the authoritative ack; the runner never blocks
// on it.
type request struct {
	userID int64
	name   string
	kind   string // buy_in | rebuy | leave | act | state
	action string // poker action for kind == "act"
	amount int64
	seatNo int
	idemKey string
	resp   chan ack
}

type ack struct {
	payload map[string]any
	err     error
}

// Runner owns one poker room: engine state, hand lifecycle, chain seeds,
// persistence, and room events. One goroutine is the single writer; every
// mutation goes through the request queue.
type Runner struct {
	room    string
	roomID  int64
	cap     int
	minBet  int64 // big blind
	maxBet  int64 // max buy-in (stack cap)
	cfg     Config
	chain   *fair.ChainService
	persist Persister
	bus     bus.Bus
	clk     clock.Clock
	pool    *pgxpool.Pool
	logger  *slog.Logger
	store   *store.Queries

	reqs chan request

	mu          sync.RWMutex
	state       *poker.State
	roundID     int64
	deadline    time.Time // current turn's cutoff
	nextHandAt  time.Time // earliest next hand start
	lastResults []poker.PlayerResult
}

// NewRunner builds the room's table runner.
func NewRunner(room string, roomID int64, capacity int, chain *fair.ChainService, persist Persister, b bus.Bus, clk clock.Clock, cfg Config, pool *pgxpool.Pool, logger *slog.Logger) *Runner {
	return &Runner{
		room: room, roomID: roomID, cap: capacity,
		cfg: cfg, chain: chain, persist: persist, bus: b, clk: clk,
		pool: pool, logger: logger, store: store.New(pool),
		reqs: make(chan request, 64),
	}
}

// SetLimits configures the room's stake tier: minBet is the big blind,
// maxBet the maximum buy-in.
func (r *Runner) SetLimits(min, max int64) {
	r.mu.Lock()
	r.minBet, r.maxBet = min, max
	if r.state == nil {
		r.state = poker.NewState(r.cap, min/2, min)
	} else {
		r.state.SB, r.state.BB = min/2, min
	}
	r.mu.Unlock()
}

// Submit routes a request into the runner loop and waits for the ack.
func (r *Runner) Submit(ctx context.Context, req request) (map[string]any, error) {
	req.resp = make(chan ack, 1)
	select {
	case r.reqs <- req:
	case <-ctx.Done():
		return nil, coded("table_busy", fmt.Errorf("table busy"))
	}
	select {
	case a := <-req.resp:
		return a.payload, a.err
	case <-time.After(5 * time.Second):
		return nil, coded("table_busy", fmt.Errorf("table busy"))
	}
}

// Run drives the table until the context is cancelled.
func (r *Runner) Run(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.Tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-r.reqs:
			r.handle(ctx, req)
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

// tick advances time-driven work: turn timeouts and the next hand start.
func (r *Runner) tick(ctx context.Context) {
	r.mu.Lock()
	st := r.state
	if st == nil {
		r.mu.Unlock()
		return
	}
	now := r.clk.Now()

	// Turn timeout.
	if st.ToAct >= 0 && now.After(r.deadline) {
		if err := st.Timeout(); err == nil {
			wasSettled := st.Phase == poker.PhaseShowdown && st.Results != nil
			view := r.viewLocked(st, 0)
			r.afterMutation(ctx, st, true)
			r.mu.Unlock()
			if !wasSettled {
				// Auto check/fold is a visible table change; settlement
				// publishes its own state.
				r.publish("table_state", view)
			}
		} else {
			r.mu.Unlock()
		}
		return
	}

	// Next hand.
	ready := st.Phase == poker.PhaseWaiting || st.Phase == poker.PhaseShowdown
	if ready && now.After(r.nextHandAt) && r.eligible(st) >= 2 {
		r.mu.Unlock()
		r.startHand(ctx)
		return
	}
	r.mu.Unlock()
}

func (r *Runner) eligible(st *poker.State) int {
	n := 0
	for _, s := range st.Seats {
		if s.State != poker.SeatEmpty && s.Stack+s.Rebuy > 0 {
			n++
		}
	}
	return n
}

// startHand commits the next chain link, opens the round row, and deals.
func (r *Runner) startHand(ctx context.Context) {
	if err := r.chain.EnsureChain(ctx); err != nil {
		r.logger.Error("table chain", "room", r.room, "err", err)
		r.delayNextHand()
		return
	}
	link, err := r.store.GetNextUnrevealedChainSeed(ctx)
	if err != nil {
		r.logger.Error("table chain link", "room", r.room, "err", err)
		r.delayNextHand()
		return
	}
	seedPlain, err := hex.DecodeString(link.SeedPlain.String)
	if err != nil {
		r.logger.Error("table chain decode", "room", r.room, "err", err)
		r.delayNextHand()
		return
	}

	r.mu.Lock()
	st := r.state
	r.mu.Unlock()
	if st == nil {
		return
	}

	// The round id binds the chain link; the salt binds the stream to this
	// exact hand, so replay needs only public material.
	roundID, err := r.persist.OpenHand(ctx, r.roomID, link.ID, poker.GameID, "")
	if err != nil {
		r.logger.Error("open hand", "room", r.room, "err", err)
		r.delayNextHand()
		return
	}
	salt := fmt.Sprintf("round:%d", roundID)
	stream := fair.NewChainStream(seedPlain, salt)

	r.mu.Lock()
	if err := st.StartHand(stream); err != nil {
		r.mu.Unlock()
		r.logger.Warn("start hand", "room", r.room, "err", err)
		r.delayNextHand()
		return
	}
	r.roundID = roundID
	r.lastResults = nil
	r.deadline = r.clk.Now().Add(r.cfg.TurnTimeout)
	phase, handNo := st.Phase, st.HandNo
	view := r.viewLocked(st, 0)
	r.mu.Unlock()

	r.publish("hand_started", map[string]any{
		"handNo": handNo,
		"phase":  phase,
		"roundId": roundID,
	})
	r.publish("table_state", view)
}

// afterMutation is called (under lock) after any engine change: refresh the
// turn deadline and settle if the hand ended.
func (r *Runner) afterMutation(ctx context.Context, st *poker.State, holdLock bool) {
	if !holdLock {
		r.mu.Lock()
		defer r.mu.Unlock()
	}
	if st.ToAct >= 0 {
		r.deadline = r.clk.Now().Add(r.cfg.TurnTimeout)
	}
	if st.Phase == poker.PhaseShowdown && st.Results != nil && r.lastResults == nil {
		r.lastResults = st.Results
		view := r.viewLocked(st, 0)
		record, settled := r.recordLocked(st)
		roundID := r.roundID
		r.mu.Unlock()
		r.settle(ctx, roundID, record, settled, view, st)
		r.mu.Lock()
	}
}

// settle reveals the seed, records the hand, publishes the result, and
// cashes out leavers. Called with the lock released (re-acquired inside
// afterMutation's caller pattern).
func (r *Runner) settle(ctx context.Context, roundID int64, record HandRecord, settled []SettledPlayer, view map[string]any, st *poker.State) {
	// Reveal + verify the committed link before results leave the server.
	if _, _, err := r.chain.RevealNext(ctx); err != nil {
		r.logger.Error("reveal chain", "room", r.room, "err", err)
	}
	if err := r.persist.SettleHand(ctx, roundID, poker.GameID, record, settled); err != nil {
		r.logger.Error("settle hand", "room", r.room, "round", roundID, "err", err)
	}

	// Full reveal to the room.
	raw, _ := json.Marshal(record)
	r.publish("hand_result", map[string]any{
		"roundId": roundID,
		"record":  json.RawMessage(raw),
	})
	r.publish("table_state", view)

	// Cash out leavers now that chips are settled.
	for _, seat := range st.LeavingSeats() {
		r.cashOutSeat(ctx, st, seat)
	}

	r.mu.Lock()
	r.nextHandAt = r.clk.Now().Add(r.cfg.InterHand)
	r.mu.Unlock()
}

func (r *Runner) cashOutSeat(ctx context.Context, st *poker.State, seat *poker.Seat) {
	userID := seat.UserID // CashOut frees the seat and zeroes it
	// Engine mutation under the write lock: HTTP readers may be rendering.
	r.mu.Lock()
	chips, err := st.CashOut(userID)
	r.mu.Unlock()
	if err != nil || chips <= 0 {
		return
	}
	bal, err := r.persist.CashOut(ctx, userID, chips, cashOutKey(r.room, userID, r.roundID))
	if err != nil {
		r.logger.Error("cash out", "room", r.room, "user", userID, "err", err)
		return
	}
	r.publish("cash_out", map[string]any{
		"userId": userID, "credits": chips, "balanceCredits": bal,
	})
}

func (r *Runner) delayNextHand() {
	r.mu.Lock()
	r.nextHandAt = r.clk.Now().Add(2 * time.Second)
	r.mu.Unlock()
}

// handle applies one request to the table.
func (r *Runner) handle(ctx context.Context, req request) {
	r.mu.Lock()
	st := r.state
	if st == nil {
		r.mu.Unlock()
		req.resp <- ack{err: coded("table_unavailable", fmt.Errorf("table unavailable"))}
		return
	}

	switch req.kind {
	case "state":
		view := r.viewLocked(st, req.userID)
		r.mu.Unlock()
		req.resp <- ack{payload: view}
		return

	case "buy_in", "rebuy":
		r.mu.Unlock()
		r.handleBuyIn(ctx, req)
		return

	case "leave":
		seat, err := st.LeaveSeat(req.userID)
		if err != nil {
			r.mu.Unlock()
			req.resp <- ack{err: coded("not_seated", err)}
			return
		}
		if st.Phase == poker.PhaseWaiting || st.Phase == poker.PhaseShowdown {
			// Out of hand: cash out immediately (lock released inside).
			r.mu.Unlock()
			r.cashOutSeat(ctx, st, seat)
			r.publish("table_state", r.viewFor(0))
			req.resp <- ack{payload: map[string]any{"left": true}}
			return
		}
		r.afterMutation(ctx, st, true)
		view := r.viewLocked(st, 0)
		r.mu.Unlock()
		r.publish("table_state", view)
		req.resp <- ack{payload: map[string]any{"leaving": true}}
		return

	case "act":
		err := st.Act(poker.Action{UserID: req.userID, Kind: req.action, Amount: req.amount})
		if err != nil {
			r.mu.Unlock()
			req.resp <- ack{err: r.codeAction(err)}
			return
		}
		view := r.viewLocked(st, req.userID)
		r.afterMutation(ctx, st, true)
		r.mu.Unlock()
		req.resp <- ack{payload: view}
		return

	default:
		r.mu.Unlock()
		req.resp <- ack{err: coded("bad_request", fmt.Errorf("unknown action"))}
	}
}

// handleBuyIn validates tier + seat, debits, and seats the player. Runs
// outside the lock (money tx), re-locking to seat.
func (r *Runner) handleBuyIn(ctx context.Context, req request) {
	r.mu.RLock()
	bb, maxBuy := r.state.BB, r.maxBet
	seat := r.state.SeatOf(req.userID)
	freeSeat := -1
	for _, s := range r.state.Seats {
		if s.State == poker.SeatEmpty {
			freeSeat = s.SeatNo
			break
		}
	}
	r.mu.RUnlock()

	if req.amount <= 0 {
		req.resp <- ack{err: coded("invalid_amount", ErrInvalidAmount)}
		return
	}
	if req.amount < bb*r.cfg.MinBuyInMult || req.amount > maxBuy {
		req.resp <- ack{err: coded("out_of_tier", fmt.Errorf("buy-in must be between %d and %d", bb*r.cfg.MinBuyInMult, maxBuy))}
		return
	}

	chips := req.amount
	if seat != nil {
		// Rebuy tops the stack up to the cap; joining takes a new seat.
		r.mu.RLock()
		stack := seat.Stack + seat.Rebuy
		r.mu.RUnlock()
		if stack+chips > maxBuy {
			req.resp <- ack{err: coded("out_of_tier", fmt.Errorf("stack would exceed the %d cap", maxBuy))}
			return
		}
	} else if freeSeat < 0 {
		req.resp <- ack{err: coded("table_full", poker.ErrTableFull)}
		return
	}

	balance, replayed, err := r.persist.BuyIn(ctx, req.userID, poker.GameID, chips, buyInKey(r.room, req.userID, req.idemKey))
	if err != nil {
		req.resp <- ack{err: codeMoney(err)}
		return
	}
	if replayed {
		// This key already paid: no chips are owed, just return the table.
		r.mu.RLock()
		stack := int64(0)
		if s := r.state.SeatOf(req.userID); s != nil {
			stack = s.Stack + s.Rebuy
		}
		r.mu.RUnlock()
		req.resp <- ack{payload: map[string]any{
			"seated": stack > 0, "stack": stack, "balanceCredits": balance, "replay": true,
		}}
		return
	}

	r.mu.Lock()
	st := r.state
	var seated bool
	if existing := st.SeatOf(req.userID); existing != nil {
		if err := st.AddChips(req.userID, chips); err == nil {
			seated = true
		}
	} else {
		if _, err := st.SeatPlayer(req.userID, req.name, chips, req.seatNo); err == nil {
			seated = true
		}
	}
	stack := int64(0)
	if s := st.SeatOf(req.userID); s != nil {
		stack = s.Stack + s.Rebuy
	}
	// A hand can start as soon as two stacks exist.
	if st.Phase == poker.PhaseWaiting && r.nextHandAt.IsZero() {
		r.nextHandAt = r.clk.Now().Add(2 * time.Second)
	}
	r.mu.Unlock()

	if !seated {
		// Lost the race (seat taken): refund immediately with a derived key.
		if _, err := r.persist.CashOut(ctx, req.userID, chips, buyInKey(r.room, req.userID, req.idemKey)+":refund"); err != nil {
			r.logger.Error("buyin refund", "room", r.room, "user", req.userID, "err", err)
		}
		req.resp <- ack{err: coded("table_full", poker.ErrTableFull)}
		return
	}

	r.publish("table_state", r.viewFor(0))
	req.resp <- ack{payload: map[string]any{
		"seated": true, "stack": stack, "balanceCredits": balance,
	}}
}

// codeAction maps engine errors to wire codes.
func (r *Runner) codeAction(err error) error {
	switch err {
	case poker.ErrNoHand:
		return coded("no_hand", err)
	case poker.ErrNotYourTurn:
		return coded("not_your_turn", err)
	case poker.ErrNotSeated:
		return coded("not_seated", err)
	case poker.ErrIllegalAction:
		return coded("illegal_action", err)
	case poker.ErrBadAmount:
		return coded("invalid_amount", err)
	default:
		return coded("action_rejected", err)
	}
}

// codeMoney maps wallet errors to wire codes.
func codeMoney(err error) error {
	switch {
	case err == nil:
		return nil
	case err == ErrForbiddenStatus:
		return coded("status_forbids_betting", err)
	case err == ErrInvalidAmount:
		return coded("invalid_amount", err)
	default:
		return coded("bet_rejected", err)
	}
}

func (r *Runner) publish(evType string, payload map[string]any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	r.bus.Publish(bus.Event{Topic: "rooms", Room: r.room, Type: evType, Payload: raw})
}

// LiveState renders the room's live snapshot for HTTP deep links and lobby
// summaries (masked: no hole cards).
func (r *Runner) LiveState() map[string]any {
	return r.viewFor(0)
}
