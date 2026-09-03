package hand

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game/blackjack"
	"github.com/ai-doodoo-slots/services/backend/internal/testdb"
	"github.com/ai-doodoo-slots/services/backend/internal/wallet"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixture struct {
	pool   *pgxpool.Pool
	svc    *Service
	wallet *wallet.Wallet
	fair   *fair.Service
	userID int64
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := testdb.Pool(t)
	ctx := context.Background()

	svc := NewService(pool, blackjack.New([]int64{5, 10, 25, 50}), clock.Real{})
	w := wallet.New(pool)
	fsvc := fair.NewService(pool)

	userID := testdb.NewUser(t, pool, 1000)
	if _, err := fsvc.EnsureForUser(ctx, userID); err != nil {
		t.Fatalf("ensure seed: %v", err)
	}
	return &fixture{pool: pool, svc: svc, wallet: w, fair: fsvc, userID: userID}
}

func (f *fixture) key(s string) string {
	return fmt.Sprintf("%s:%d", s, f.userID)
}

// driveToCompletion stands or hits the active hand to completion, tracking
// the expected stake and payout. Returns the final view.
func (f *fixture) driveToCompletion(t *testing.T, ctx context.Context, res DealResult, hitTo int) DealResult {
	t.Helper()
	for i := 0; i < 12; i++ {
		if res.View.Status == blackjack.StatusComplete {
			return res
		}
		action := "stand"
		if res.View.PlayerTotal < hitTo {
			action = "hit"
		}
		next, err := f.svc.Action(ctx, f.userID, res.HandID, action, f.key(fmt.Sprintf("act-%d-%d", res.HandID, i)))
		if err != nil {
			t.Fatalf("action %s: %v", action, err)
		}
		res = next
	}
	t.Fatal("hand never completed")
	return res
}

func TestDealDebitsAndMasksHoleCard(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	res, err := f.svc.Deal(ctx, f.userID, 10, "bj-seed", f.key("deal-1"))
	if err != nil {
		t.Fatalf("deal: %v", err)
	}
	if res.View.BetCredits != 10 || res.View.Status == "" {
		t.Fatalf("bad deal view: %+v", res.View)
	}
	if res.BalanceCredits != 990 {
		t.Fatalf("balance after deal = %d, want 990", res.BalanceCredits)
	}
	if res.View.Status == blackjack.StatusActive {
		// Hole card withheld: exactly one dealer card (2 chars) visible.
		if len(res.View.DealerCards) != 2 {
			t.Fatalf("active hand leaked hole card: %q", res.View.DealerCards)
		}
		if res.View.DealerTotal != nil {
			t.Fatal("active hand exposed dealer total")
		}
	} else if len(res.View.DealerCards) != 4 {
		t.Fatalf("completed deal should reveal both dealer cards: %q", res.View.DealerCards)
	}

	// The bets row exists with the seed triple and a masked outcome.
	var rawOutcome []byte
	if err := f.pool.QueryRow(ctx,
		`SELECT outcome FROM bets WHERE id = $1`, res.View.BetID,
	).Scan(&rawOutcome); err != nil {
		t.Fatal(err)
	}
	var betOutcome struct {
		DealerCards string `json:"dealerCards"`
	}
	if err := json.Unmarshal(rawOutcome, &betOutcome); err != nil {
		t.Fatal(err)
	}
	if res.View.Status == blackjack.StatusActive && len(betOutcome.DealerCards) != 2 {
		t.Fatalf("bet outcome leaked hole card: %q", betOutcome.DealerCards)
	}

	balance, _ := f.wallet.Balance(ctx, f.userID)
	sum, _ := f.wallet.LedgerSum(ctx, f.userID)
	want := int64(990)
	if res.View.Status == blackjack.StatusComplete {
		want = 990 - 10 + res.View.PayoutCredits
	}
	if balance != want || sum != want {
		t.Fatalf("balance=%d sum=%d want=%d", balance, sum, want)
	}
}

func TestFullHandLifecycleAndLedger(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	res, err := f.svc.Deal(ctx, f.userID, 10, "bj-seed", f.key("life-1"))
	if err != nil {
		t.Fatalf("deal: %v", err)
	}
	handID := res.HandID
	stake := int64(10)

	if res.View.Status == blackjack.StatusActive && res.View.CanDouble {
		next, err := f.svc.Action(ctx, f.userID, handID, "double", f.key("dbl"))
		if err != nil {
			t.Fatalf("double: %v", err)
		}
		stake = 20
		if next.View.BetCredits != 20 {
			t.Fatalf("doubled stake = %d, want 20", next.View.BetCredits)
		}
		res = next
	}

	final := f.driveToCompletion(t, ctx, res, 17)

	if final.View.Status != blackjack.StatusComplete {
		t.Fatal("hand not complete")
	}
	if len(final.View.DealerCards) < 4 {
		t.Fatalf("completed hand should reveal full dealer hand: %q", final.View.DealerCards)
	}
	if final.View.DealerTotal == nil {
		t.Fatal("completed hand missing dealer total")
	}
	// Payout conventions.
	switch final.View.Outcome {
	case blackjack.OutcomeBlackjack, blackjack.OutcomeWin:
		if final.View.PayoutCredits <= stake {
			t.Fatalf("%s payout %d not above stake %d", final.View.Outcome, final.View.PayoutCredits, stake)
		}
	case blackjack.OutcomePush:
		if final.View.PayoutCredits != stake {
			t.Fatalf("push payout %d != stake %d", final.View.PayoutCredits, stake)
		}
	case blackjack.OutcomeLose, blackjack.OutcomeBust:
		if final.View.PayoutCredits != 0 {
			t.Fatalf("%s payout %d, want 0", final.View.Outcome, final.View.PayoutCredits)
		}
	default:
		t.Fatalf("unexpected outcome %q", final.View.Outcome)
	}

	// Ledger conservation: 1000 - stake + payout, and balance == sum.
	balance, _ := f.wallet.Balance(ctx, f.userID)
	sum, _ := f.wallet.LedgerSum(ctx, f.userID)
	want := int64(1000) - stake + final.View.PayoutCredits
	if balance != want || sum != want {
		t.Fatalf("balance=%d sum=%d want=%d", balance, sum, want)
	}

	// The bets row settled to the final stake/payout.
	var betCredits, payout int64
	if err := f.pool.QueryRow(ctx,
		`SELECT bet_credits, payout_credits FROM bets WHERE id = $1`, final.View.BetID,
	).Scan(&betCredits, &payout); err != nil {
		t.Fatal(err)
	}
	if betCredits != stake || payout != final.View.PayoutCredits {
		t.Fatalf("bet row (%d,%d) != expected (%d,%d)", betCredits, payout, stake, final.View.PayoutCredits)
	}

	// Actions after completion are rejected.
	if _, err := f.svc.Action(ctx, f.userID, handID, "hit", f.key("post-1")); !errors.Is(err, ErrHandComplete) {
		t.Fatalf("want ErrHandComplete, got %v", err)
	}

	// A new deal is possible once the previous hand completed.
	if _, err := f.svc.Deal(ctx, f.userID, 5, "bj-seed", f.key("life-2")); err != nil {
		t.Fatalf("deal after completion: %v", err)
	}
}

func TestOneActiveHandPerUser(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	first, err := f.svc.Deal(ctx, f.userID, 10, "cs", f.key("one-1"))
	if err != nil {
		t.Fatalf("deal: %v", err)
	}
	if first.View.Status == blackjack.StatusComplete {
		t.Skip("hand completed at deal; nothing to block a second deal")
	}
	if _, err := f.svc.Deal(ctx, f.userID, 10, "cs", f.key("one-2")); !errors.Is(err, ErrHandActive) {
		t.Fatalf("want ErrHandActive, got %v", err)
	}
}

func TestActionIdempotency(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	res, err := f.svc.Deal(ctx, f.userID, 10, "cs", f.key("idem-1"))
	if err != nil {
		t.Fatalf("deal: %v", err)
	}
	if res.View.Status == blackjack.StatusComplete {
		t.Skip("hand completed at deal")
	}
	key := f.key("hit-once")
	a, err := f.svc.Action(ctx, f.userID, res.HandID, "hit", key)
	if err != nil {
		t.Fatalf("hit: %v", err)
	}
	if a.Replay {
		t.Fatal("first hit reported Replay")
	}
	b, err := f.svc.Action(ctx, f.userID, res.HandID, "hit", key)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if !b.Replay {
		t.Fatal("retry did not report Replay")
	}
	if a.View.PlayerCards != b.View.PlayerCards {
		t.Fatalf("retry drew a card: %q vs %q", a.View.PlayerCards, b.View.PlayerCards)
	}
}

func TestDealIdempotentReplay(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	first, err := f.svc.Deal(ctx, f.userID, 10, "cs", f.key("replay-1"))
	if err != nil {
		t.Fatalf("deal: %v", err)
	}
	second, err := f.svc.Deal(ctx, f.userID, 10, "cs", f.key("replay-1"))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !second.Replay || second.HandID != first.HandID {
		t.Fatalf("replay mismatch: %+v vs %+v", first, second)
	}
	var hands, bets int64
	if err := f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM blackjack_hands WHERE user_id = $1`, f.userID).Scan(&hands); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bets WHERE user_id = $1`, f.userID).Scan(&bets); err != nil {
		t.Fatal(err)
	}
	if hands != 1 || bets != 1 {
		t.Fatalf("replay duplicated rows: hands=%d bets=%d", hands, bets)
	}
}

func TestInsufficientFundsRollsBackDeal(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.wallet.Apply(ctx, wallet.ApplyRequest{
		UserID: f.userID, Kind: wallet.KindAdminAdjust, Amount: -995,
		IdempotencyKey: fmt.Sprintf("drain:%d", f.userID),
	}); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if _, err := f.svc.Deal(ctx, f.userID, 10, "cs", f.key("broke")); !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("want ErrInsufficientFunds, got %v", err)
	}
	var hands int64
	if err := f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM blackjack_hands WHERE user_id = $1`, f.userID).Scan(&hands); err != nil {
		t.Fatal(err)
	}
	if hands != 0 {
		t.Fatalf("failed deal persisted %d hands", hands)
	}
}

func TestStaleHandAutoStands(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	res, err := f.svc.Deal(ctx, f.userID, 10, "cs", f.key("stale-1"))
	if err != nil {
		t.Fatalf("deal: %v", err)
	}
	if res.View.Status == blackjack.StatusComplete {
		t.Skip("hand completed at deal")
	}
	// Age the hand past the stale window behind the service's back.
	if _, err := f.pool.Exec(ctx,
		`UPDATE blackjack_hands SET updated_at = now() - interval '10 minutes' WHERE id = $1`,
		res.HandID); err != nil {
		t.Fatal(err)
	}
	next, err := f.svc.Deal(ctx, f.userID, 10, "cs", f.key("stale-2"))
	if err != nil {
		t.Fatalf("deal over stale hand: %v", err)
	}
	// The stale hand completed with a stand and paid out; the new deal went
	// through. Ledger conservation across both hands.
	var staleStatus string
	var stalePayout int64
	if err := f.pool.QueryRow(ctx,
		`SELECT status, payout_credits FROM blackjack_hands WHERE id = $1`, res.HandID,
	).Scan(&staleStatus, &stalePayout); err != nil {
		t.Fatal(err)
	}
	if staleStatus != blackjack.StatusComplete {
		t.Fatalf("stale hand status = %q, want complete", staleStatus)
	}
	balance, _ := f.wallet.Balance(ctx, f.userID)
	sum, _ := f.wallet.LedgerSum(ctx, f.userID)
	want := int64(1000) - 10 + stalePayout - 10 + next.View.PayoutCredits
	if balance != want || sum != want {
		t.Fatalf("balance=%d sum=%d want=%d", balance, sum, want)
	}
}

func TestValidationErrors(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Deal(ctx, f.userID, 7, "cs", f.key("v1")); !errors.Is(err, ErrInvalidBet) {
		t.Fatalf("want ErrInvalidBet, got %v", err)
	}
	if _, err := f.svc.Deal(ctx, f.userID, 10, "cs", ""); !errors.Is(err, ErrIdempotencyKeyInvalid) {
		t.Fatalf("want ErrIdempotencyKeyInvalid, got %v", err)
	}
	if _, err := f.svc.Action(ctx, f.userID, 1, "split", f.key("v3")); !errors.Is(err, ErrInvalidActionName) {
		t.Fatalf("want ErrInvalidActionName, got %v", err)
	}
	if _, err := f.svc.Action(ctx, f.userID, 999999, "hit", f.key("v4")); !errors.Is(err, ErrHandNotFound) {
		t.Fatalf("want ErrHandNotFound, got %v", err)
	}
}
