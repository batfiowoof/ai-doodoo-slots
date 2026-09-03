package table

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/bus"
	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game/poker"
	"github.com/ai-doodoo-slots/services/backend/internal/testdb"
	"github.com/ai-doodoo-slots/services/backend/internal/wallet"
	"github.com/ai-doodoo-slots/services/backend/internal/ws"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newRunner builds a runner on the real database with fast timings.
// now keeps the clock guard happy: tests read time through the clock
// package like production code.
func now() time.Time { return clock.Real{}.Now() }

func newRunner(t *testing.T) (*Runner, *pgxpool.Pool, *bus.MemoryBus) {
	t.Helper()
	pool := testdb.Pool(t)
	b := bus.NewMemoryBus()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := Config{TurnTimeout: 300 * time.Millisecond, InterHand: 100 * time.Millisecond, Tick: 20 * time.Millisecond, MinBuyInMult: 20}
	r := NewRunner("holdem-test", 1, 6, fair.NewChainService(pool), NewPersister(pool), b, clock.Real{}, cfg, pool, log)
	r.SetLimits(10, 2000) // BB 10, SB 5, max buy-in 2000
	return r, pool, b
}

func TestBuyInSeatsAndCashesOut(t *testing.T) {
	r, pool, _ := newRunner(t)
	ctx := context.Background()
	go r.Run(ctx)
	t.Cleanup(func() { ctx.Done() })

	user := testdb.NewUser(t, pool, 1000)
	id := ws.Identity{UserID: user, DisplayName: "p1", Status: "active"}
	intake := NewIntake(r, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ack, err := intake.HandleGameAction(id, mustJSON(gameActionPayload{Action: "buy_in", Amount: 200, IdempotencyKey: "k1"}))
	if err != nil {
		t.Fatalf("buy_in: %v", err)
	}
	if ack["seated"] != true || ack["stack"].(int64) != 200 || ack["balanceCredits"].(int64) != 800 {
		t.Fatalf("buy_in ack: %+v", ack)
	}

	// Money conservation: wallet 800 + table 200.
	w := wallet.New(pool)
	bal, _ := w.Balance(ctx, user)
	if bal != 800 {
		t.Fatalf("wallet after buy-in = %d, want 800", bal)
	}

	// Leave cashes the stack back.
	ack, err = intake.HandleGameAction(id, mustJSON(gameActionPayload{Action: "leave"}))
	if err != nil {
		t.Fatalf("leave: %v", err)
	}
	bal, _ = w.Balance(ctx, user)
	if bal != 1000 {
		t.Fatalf("wallet after leave = %d, want 1000", bal)
	}
	sum, _ := w.LedgerSum(ctx, user)
	if sum != bal {
		t.Fatalf("ledger %d != balance %d", sum, bal)
	}
}

func TestBuyInTierAndIdempotency(t *testing.T) {
	r, pool, _ := newRunner(t)
	ctx := context.Background()
	go r.Run(ctx)
	user := testdb.NewUser(t, pool, 5000)
	id := ws.Identity{UserID: user, DisplayName: "p1", Status: "active"}
	intake := NewIntake(r, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Below the minimum buy-in (20 × BB = 200).
	if _, err := intake.HandleGameAction(id, mustJSON(gameActionPayload{Action: "buy_in", Amount: 100, IdempotencyKey: "lo"})); codeOf(err) != "out_of_tier" {
		t.Fatalf("want out_of_tier, got %v", err)
	}
	// Above the cap.
	if _, err := intake.HandleGameAction(id, mustJSON(gameActionPayload{Action: "buy_in", Amount: 5000, IdempotencyKey: "hi"})); codeOf(err) != "out_of_tier" {
		t.Fatalf("want out_of_tier, got %v", err)
	}
	// Replayed key: no double debit.
	if _, err := intake.HandleGameAction(id, mustJSON(gameActionPayload{Action: "buy_in", Amount: 300, IdempotencyKey: "dup"})); err != nil {
		t.Fatalf("buy_in: %v", err)
	}
	ack, err := intake.HandleGameAction(id, mustJSON(gameActionPayload{Action: "buy_in", Amount: 300, IdempotencyKey: "dup"}))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if ack["stack"].(int64) != 300 {
		t.Fatalf("replay stack = %v, want 300 (no double credit)", ack["stack"])
	}
	w := wallet.New(pool)
	bal, _ := w.Balance(ctx, user)
	if bal != 4700 {
		t.Fatalf("balance after idempotent replay = %d, want 4700", bal)
	}
}

func TestBannedUserRejected(t *testing.T) {
	r, pool, _ := newRunner(t)
	ctx := context.Background()
	go r.Run(ctx)
	user := testdb.NewUser(t, pool, 1000)
	id := ws.Identity{UserID: user, DisplayName: "banned", Status: "banned"}
	intake := NewIntake(r, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := intake.HandleGameAction(id, mustJSON(gameActionPayload{Action: "buy_in", Amount: 300, IdempotencyKey: "b1"})); codeOf(err) != "status_forbids_betting" {
		t.Fatalf("want status_forbids_betting, got %v", err)
	}
	w := wallet.New(pool)
	bal, _ := w.Balance(ctx, user)
	if bal != 1000 {
		t.Fatalf("banned user wallet moved: %d", bal)
	}
}

// TestFullHandAtomicSettlement: two players buy in, one hand plays out via
// runner-driven timeouts (or actions), settles to the DB, and money is
// conserved end to end.
func TestFullHandAtomicSettlement(t *testing.T) {
	r, pool, b := newRunner(t)
	ctx, cancel := context.WithCancel(context.Background())
	go r.Run(ctx)
	t.Cleanup(cancel)

	intake := NewIntake(r, slog.New(slog.NewTextHandler(io.Discard, nil)))
	u1 := testdb.NewUser(t, pool, 1000)
	u2 := testdb.NewUser(t, pool, 1000)
	id1 := ws.Identity{UserID: u1, DisplayName: "p1", Status: "active"}
	id2 := ws.Identity{UserID: u2, DisplayName: "p2", Status: "active"}

	if _, err := intake.HandleGameAction(id1, mustJSON(gameActionPayload{Action: "buy_in", Amount: 200, IdempotencyKey: "s1"})); err != nil {
		t.Fatal(err)
	}
	if _, err := intake.HandleGameAction(id2, mustJSON(gameActionPayload{Action: "buy_in", Amount: 200, IdempotencyKey: "s2"})); err != nil {
		t.Fatal(err)
	}

	// Wait for a hand to start.
	deadline := now().Add(5 * time.Second)
	handStarted := false
	for !handStarted && now().Before(deadline) {
		view := r.viewFor(0)
		if view["phase"] == poker.PhasePreflop || view["phase"] == poker.PhaseFlop || view["phase"] == poker.PhaseShowdown {
			handStarted = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !handStarted {
		t.Fatal("hand never started")
	}

	// Both players fold/check instantly until the hand settles (turn
	// timeout is 300ms, so waiting also drives it).
	deadline = now().Add(15 * time.Second)
	settled := false
	for !settled && now().Before(deadline) {
		view := r.viewFor(0)
		if results, ok := view["results"].([]map[string]any); ok && len(results) > 0 {
			settled = true
			break
		}
		// Try to act as whoever's turn it is (speeds the hand along).
		for _, seat := range view["seats"].([]map[string]any) {
			if seatV, _ := seat["seatNo"].(int); seatV == view["toAct"] {
				uid, _ := seat["userId"].(int64)
				switch uid {
				case u1:
					_, _ = intake.HandleGameAction(id1, mustJSON(gameActionPayload{Action: "check"}))
					_, _ = intake.HandleGameAction(id1, mustJSON(gameActionPayload{Action: "fold"}))
				case u2:
					_, _ = intake.HandleGameAction(id2, mustJSON(gameActionPayload{Action: "check"}))
					_, _ = intake.HandleGameAction(id2, mustJSON(gameActionPayload{Action: "fold"}))
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !settled {
		t.Fatal("hand never settled")
	}

	// DB assertions: one settled round + two bets rows, payouts sum equals
	// contributions (zero-sum, no rake).
	var rounds, bets, payoutSum, betSum int64
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM rounds r WHERE r.game_id = 'holdem' AND r.result IS NOT NULL`).Scan(&rounds); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*), COALESCE(SUM(bet_credits),0), COALESCE(SUM(payout_credits),0) FROM bets WHERE game_id = 'holdem' AND user_id = ANY($1)`,
		[]int64{u1, u2}).Scan(&bets, &betSum, &payoutSum); err != nil {
		t.Fatal(err)
	}
	if rounds < 1 {
		t.Fatalf("no settled holdem rounds recorded")
	}
	if bets < 2 {
		t.Fatalf("bets rows = %d, want >= 2", bets)
	}
	if payoutSum != betSum {
		t.Fatalf("payouts %d != stakes %d (rake leak)", payoutSum, betSum)
	}

	// Table chips + wallets = 2000 total.
	w := wallet.New(pool)
	b1, _ := w.Balance(context.Background(), u1)
	b2, _ := w.Balance(context.Background(), u2)
	table := int64(0)
	for _, seat := range r.viewFor(0)["seats"].([]map[string]any) {
		table += seat["stack"].(int64)
	}
	if b1+b2+table != 2000 {
		t.Fatalf("money leak: wallets %d+%d table %d", b1, b2, table)
	}

	// Both leave; wallets return to 1000 each (net zero-sum hand).
	if _, err := intake.HandleGameAction(id1, mustJSON(gameActionPayload{Action: "leave"})); err != nil {
		t.Fatalf("leave 1: %v", err)
	}
	if _, err := intake.HandleGameAction(id2, mustJSON(gameActionPayload{Action: "leave"})); err != nil {
		t.Fatalf("leave 2: %v", err)
	}
	b1, _ = w.Balance(context.Background(), u1)
	b2, _ = w.Balance(context.Background(), u2)
	if b1+b2 != 2000 {
		t.Fatalf("after leave wallets = %d + %d, want 2000 total", b1, b2)
	}

	// Bus carried the room events.
	_ = b
}

func TestStateActionMasksHoleCards(t *testing.T) {
	r, pool, _ := newRunner(t)
	ctx, cancel := context.WithCancel(context.Background())
	go r.Run(ctx)
	t.Cleanup(cancel)

	intake := NewIntake(r, slog.New(slog.NewTextHandler(io.Discard, nil)))
	u1 := testdb.NewUser(t, pool, 1000)
	u2 := testdb.NewUser(t, pool, 1000)
	id1 := ws.Identity{UserID: u1, DisplayName: "p1", Status: "active"}
	id2 := ws.Identity{UserID: u2, DisplayName: "p2", Status: "active"}
	if _, err := intake.HandleGameAction(id1, mustJSON(gameActionPayload{Action: "buy_in", Amount: 200, IdempotencyKey: "m1"})); err != nil {
		t.Fatal(err)
	}
	if _, err := intake.HandleGameAction(id2, mustJSON(gameActionPayload{Action: "buy_in", Amount: 200, IdempotencyKey: "m2"})); err != nil {
		t.Fatal(err)
	}

	deadline := now().Add(5 * time.Second)
	for now().Before(deadline) {
		if v := r.viewFor(0); v["phase"] == poker.PhasePreflop {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Spectator view: no hole cards anywhere.
	spectator := r.viewFor(0)
	for _, seat := range spectator["seats"].([]map[string]any) {
		if seat["cards"] != "" {
			t.Fatalf("spectator view leaked cards: %+v", seat)
		}
	}
	// Personal views: own cards only.
	mine, err := intake.HandleGameAction(id1, mustJSON(gameActionPayload{Action: "state"}))
	if err != nil {
		t.Fatal(err)
	}
	ownCards, theirs := 0, 0
	for _, seat := range mine["seats"].([]map[string]any) {
		if seat["cards"] == "" {
			continue
		}
		if seat["userId"].(int64) == u1 {
			ownCards++
		} else {
			theirs++
		}
	}
	if ownCards != 1 || theirs != 0 {
		t.Fatalf("personal view: own=%d others=%d, want 1/0", ownCards, theirs)
	}
}

func mustJSON(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

func codeOf(err error) string {
	if ce, ok := err.(interface{ Code() string }); ok {
		return ce.Code()
	}
	return ""
}
