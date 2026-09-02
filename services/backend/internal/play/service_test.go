package play

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
	"github.com/ai-doodoo-slots/services/backend/internal/game/slots"
	"github.com/ai-doodoo-slots/services/backend/internal/testdb"
	"github.com/ai-doodoo-slots/services/backend/internal/wallet"
	"github.com/jackc/pgx/v5/pgxpool"
)

// slotsPayload mirrors the engine's payload for semantic comparison.
type slotsPayload struct {
	Grid         [3][3]int `json:"grid"`
	WinningLines []int     `json:"winningLines"`
}

type fixture struct {
	pool     *pgxpool.Pool
	svc      *Service
	wallet   *wallet.Wallet
	fair     *fair.Service
	registry *game.Registry
	userID   int64
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := testdb.Pool(t)
	ctx := context.Background()

	registry := game.NewRegistry()
	registry.Register(slots.Classic())
	svc := NewService(pool, registry)
	w := wallet.New(pool)
	fsvc := fair.NewService(pool)

	userID := testdb.NewUser(t, pool, 1000)
	if _, err := fsvc.EnsureForUser(ctx, userID); err != nil {
		t.Fatalf("ensure seed: %v", err)
	}

	return &fixture{pool: pool, svc: svc, wallet: w, fair: fsvc, registry: registry, userID: userID}
}

// key scopes an idempotency key to this fixture's user so reruns never
// collide with stale rows from any earlier run.
func (f *fixture) key(s string) string {
	return fmt.Sprintf("%s:%d", s, f.userID)
}

// TestIdempotentReplay is the phase 5 gate: replaying an idempotency key
// returns the original result and creates no second transaction.
func TestIdempotentReplay(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	first, err := f.svc.Play(ctx, f.userID, "slots", 10, "gate-client-seed", f.key("gate-key-1"))
	if err != nil {
		t.Fatalf("first play: %v", err)
	}
	if first.Replay {
		t.Fatal("first play reported Replay")
	}

	second, err := f.svc.Play(ctx, f.userID, "slots", 10, "gate-client-seed", f.key("gate-key-1"))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !second.Replay {
		t.Fatal("replay did not report Replay")
	}
	// The original outcome came from the engine's marshal; the replay came
	// out of jsonb, which normalizes whitespace. Compare semantically.
	var p1, p2 slotsPayload
	if err := json.Unmarshal(first.Outcome, &p1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(second.Outcome, &p2); err != nil {
		t.Fatal(err)
	}
	if second.BetID != first.BetID || second.PayoutCredits != first.PayoutCredits ||
		second.Nonce != first.Nonce || !reflect.DeepEqual(p1, p2) {
		t.Fatalf("replay differs: %+v vs %+v", first, second)
	}

	// Exactly one bet, one synthetic round, and either one or two ledger
	// transactions (payout only when payout > 0).
	var bets, rounds, txns int64
	if err := f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bets WHERE user_id = $1`, f.userID).Scan(&bets); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM rounds r JOIN bets b ON b.round_id = r.id WHERE b.user_id = $1`, f.userID).Scan(&rounds); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM transactions WHERE wallet_id = $1`, f.userID).Scan(&txns); err != nil {
		t.Fatal(err)
	}
	if bets != 1 || rounds != 1 {
		t.Fatalf("bets=%d rounds=%d, want 1/1 (replay duplicated rows)", bets, rounds)
	}
	if txns != 2 {
		t.Fatalf("transactions=%d, want 2 (debit+payout, no duplicates)", txns)
	}

	// Balance equals ledger sum.
	balance, _ := f.wallet.Balance(ctx, f.userID)
	sum, _ := f.wallet.LedgerSum(ctx, f.userID)
	want := int64(1000) - 10 + first.PayoutCredits
	if balance != want || sum != want {
		t.Fatalf("balance=%d sum=%d want=%d", balance, sum, want)
	}
}

func TestIdempotencyConflictOnDifferentBet(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.svc.Play(ctx, f.userID, "slots", 10, "cs", f.key("conflict-key")); err != nil {
		t.Fatalf("first play: %v", err)
	}
	if _, err := f.svc.Play(ctx, f.userID, "slots", 25, "cs", f.key("conflict-key")); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("want ErrIdempotencyConflict, got %v", err)
	}
}

func TestInsufficientFundsRollsBackEverything(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// Drain the wallet to 3 credits.
	if _, err := f.wallet.Apply(ctx, wallet.ApplyRequest{
		UserID: f.userID, Kind: wallet.KindAdminAdjust, Amount: -997,
		IdempotencyKey: fmt.Sprintf("drain:%d", f.userID),
	}); err != nil {
		t.Fatalf("drain: %v", err)
	}
	_, _, beforeNonce, _ := f.fair.Current(ctx, f.userID)

	if _, err := f.svc.Play(ctx, f.userID, "slots", 5, "cs", fmt.Sprintf("broke:%d", f.userID)); !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("want ErrInsufficientFunds, got %v", err)
	}

	// Nothing persisted: no bets, no nonce consumed.
	var bets int64
	if err := f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bets WHERE user_id = $1`, f.userID).Scan(&bets); err != nil {
		t.Fatal(err)
	}
	if bets != 0 {
		t.Fatalf("bets=%d after failed play, want 0", bets)
	}
	_, _, nextNonce, err := f.fair.Current(ctx, f.userID)
	if err != nil {
		t.Fatal(err)
	}
	if nextNonce != beforeNonce {
		t.Fatalf("nonce consumed on failed play: %d -> %d", beforeNonce, nextNonce)
	}
}

func TestSequentialNonces(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	r1, err := f.svc.Play(ctx, f.userID, "slots", 5, "nonce-seed", f.key("n:1"))
	if err != nil {
		t.Fatal(err)
	}
	r2, err := f.svc.Play(ctx, f.userID, "slots", 5, "nonce-seed", f.key("n:2"))
	if err != nil {
		t.Fatal(err)
	}
	if r2.Nonce != r1.Nonce+1 {
		t.Fatalf("nonces %d then %d, want sequential", r1.Nonce, r2.Nonce)
	}
	_, _, next, err := f.fair.Current(ctx, f.userID)
	if err != nil {
		t.Fatal(err)
	}
	if next != r2.Nonce+1 {
		t.Fatalf("fair/current next nonce = %d, want %d", next, r2.Nonce+1)
	}
}

func TestConcurrentPlaysSerialize(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	const n = 10
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := f.svc.Play(ctx, f.userID, "slots", 5, "concurrent", fmt.Sprintf("cc:%d:%d", f.userID, i))
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent play failed: %v", err)
		}
	}

	balance, _ := f.wallet.Balance(ctx, f.userID)
	sum, _ := f.wallet.LedgerSum(ctx, f.userID)
	if balance != sum {
		t.Fatalf("balance %d != ledger sum %d", balance, sum)
	}

	var nonces []int64
	rows, err := f.pool.Query(ctx, `SELECT nonce FROM bets WHERE user_id = $1 ORDER BY nonce`, f.userID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		nonces = append(nonces, n)
	}
	seen := map[int64]bool{}
	for _, n := range nonces {
		if seen[n] {
			t.Fatalf("duplicate nonce %d across concurrent bets", n)
		}
		seen[n] = true
	}
}

func TestUnknownGameAndBadBet(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Play(ctx, f.userID, "nope", 5, "cs", f.key("k1")); !errors.Is(err, ErrUnknownGame) {
		t.Fatalf("want ErrUnknownGame, got %v", err)
	}
	if _, err := f.svc.Play(ctx, f.userID, "slots", 7, "cs", f.key("k2")); !errors.Is(err, ErrInvalidBet) {
		t.Fatalf("want ErrInvalidBet, got %v", err)
	}
	if _, err := f.svc.Play(ctx, f.userID, "slots", 5, "cs", ""); !errors.Is(err, ErrIdempotencyKeyInvalid) {
		t.Fatalf("want ErrIdempotencyKeyInvalid, got %v", err)
	}
}
