package wallet

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The gate for phase 2: 50 concurrent debits on one wallet leave the balance
// correct and equal to the ledger sum. Requires the dockerized Postgres
// (docker compose up -d); DSN overridable via TEST_DATABASE_URL.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://retro:retro@localhost:55432/retrocasino?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("database unavailable, skipping integration test: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("database unavailable, skipping integration test: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// testUser creates an isolated user + wallet funded with startingCredits and
// registers cleanup.
func testUser(t *testing.T, pool *pgxpool.Pool, startingCredits int64) int64 {
	t.Helper()
	ctx := context.Background()
	var userID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (display_name, is_guest) VALUES ($1, true) RETURNING id`,
		"TEST-" + fmt.Sprintf("%d-%d", clock.Real{}.Now().UnixNano(), os.Getpid()),
	).Scan(&userID); err != nil {
		t.Fatalf("create test user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})
	if err := store.New(pool).CreateWallet(ctx, store.CreateWalletParams{UserID: userID, BalanceCredits: 0}); err != nil {
		t.Fatalf("create test wallet: %v", err)
	}
	if startingCredits > 0 {
		w := New(pool)
		if _, err := w.Apply(ctx, ApplyRequest{
			UserID:         userID,
			Kind:           KindSignupBonus,
			Amount:         startingCredits,
			IdempotencyKey: fmt.Sprintf("test-fund:%d:%d", userID, clock.Real{}.Now().UnixNano()),
		}); err != nil {
			t.Fatalf("fund test wallet: %v", err)
		}
	}
	return userID
}

func TestConcurrentDebitsSerializes(t *testing.T) {
	pool := testPool(t)
	w := New(pool)
	ctx := context.Background()

	const (
		concurrency = 50
		debit       = 100
		starting    = concurrency * debit // 5000
	)
	userID := testUser(t, pool, starting)

	errs := make(chan error, concurrency)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := w.Apply(ctx, ApplyRequest{
				UserID:         userID,
				Kind:           KindBet,
				Amount:         -debit,
				IdempotencyKey: fmt.Sprintf("gate-debit:%d:%d", userID, i),
			})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)

	failed := 0
	for err := range errs {
		if err != nil {
			failed++
			t.Logf("debit error: %v", err)
		}
	}
	if failed != 0 {
		t.Fatalf("%d/%d concurrent debits failed", failed, concurrency)
	}

	balance, err := w.Balance(ctx, userID)
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	if balance != 0 {
		t.Fatalf("balance = %d, want 0", balance)
	}
	sum, err := w.LedgerSum(ctx, userID)
	if err != nil {
		t.Fatalf("ledger sum: %v", err)
	}
	if sum != balance {
		t.Fatalf("ledger sum %d != cached balance %d", sum, balance)
	}
}

func TestConcurrentSameIdempotencyKeyChargesOnce(t *testing.T) {
	pool := testPool(t)
	w := New(pool)
	ctx := context.Background()

	const starting = 5000
	userID := testUser(t, pool, starting)
	key := fmt.Sprintf("same-key:%d", userID)

	results := make(chan ApplyResult, 50)
	errs := make(chan error, 50)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := w.Apply(ctx, ApplyRequest{
				UserID:         userID,
				Kind:           KindBet,
				Amount:         -100,
				IdempotencyKey: key,
			})
			if err != nil {
				errs <- err
			} else {
				results <- res
			}
		}()
	}
	wg.Wait()
	close(errs)
	close(results)

	for err := range errs {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 50 {
		t.Fatalf("got %d results, want 50", len(results))
	}
	first := <-results
	for res := range results {
		if res.TransactionID != first.TransactionID {
			t.Fatal("replays returned different transaction IDs")
		}
		if !res.Replayed {
			t.Fatal("replay did not report Replayed")
		}
	}

	balance, err := w.Balance(ctx, userID)
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	if balance != starting-100 {
		t.Fatalf("balance = %d, want %d (single charge)", balance, starting-100)
	}
	sum, _ := w.LedgerSum(ctx, userID)
	if sum != balance {
		t.Fatalf("ledger sum %d != cached balance %d", sum, balance)
	}
}

func TestIdempotencyConflictOnDifferingPayload(t *testing.T) {
	pool := testPool(t)
	w := New(pool)
	ctx := context.Background()
	userID := testUser(t, pool, 1000)
	key := fmt.Sprintf("conflict:%d", userID)

	if _, err := w.Apply(ctx, ApplyRequest{UserID: userID, Kind: KindBet, Amount: -100, IdempotencyKey: key}); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	// Same key, different amount → conflict, no second transaction.
	if _, err := w.Apply(ctx, ApplyRequest{UserID: userID, Kind: KindBet, Amount: -200, IdempotencyKey: key}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("want ErrIdempotencyConflict, got %v", err)
	}
	// Same key, different kind → conflict.
	if _, err := w.Apply(ctx, ApplyRequest{UserID: userID, Kind: KindPayout, Amount: -100, IdempotencyKey: key}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("want ErrIdempotencyConflict, got %v", err)
	}

	sum, _ := w.LedgerSum(ctx, userID)
	if sum != 900 {
		t.Fatalf("ledger sum = %d, want 900 (exactly one debit)", sum)
	}
}

func TestInsufficientFunds(t *testing.T) {
	pool := testPool(t)
	w := New(pool)
	ctx := context.Background()
	userID := testUser(t, pool, 50)

	if _, err := w.Apply(ctx, ApplyRequest{UserID: userID, Kind: KindBet, Amount: -100, IdempotencyKey: fmt.Sprintf("broke:%d", userID)}); !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("want ErrInsufficientFunds, got %v", err)
	}
	// The failed debit must not have written anything.
	balance, _ := w.Balance(ctx, userID)
	if balance != 50 {
		t.Fatalf("balance = %d, want 50", balance)
	}
}

func TestCreditsAndMixedOps(t *testing.T) {
	pool := testPool(t)
	w := New(pool)
	ctx := context.Background()
	userID := testUser(t, pool, 0)

	if _, err := w.Apply(ctx, ApplyRequest{UserID: userID, Kind: KindDailyTopup, Amount: 1000, IdempotencyKey: fmt.Sprintf("topup:%d", userID)}); err != nil {
		t.Fatalf("credit: %v", err)
	}
	if _, err := w.Apply(ctx, ApplyRequest{UserID: userID, Kind: KindBet, Amount: -250, IdempotencyKey: fmt.Sprintf("bet:%d", userID)}); err != nil {
		t.Fatalf("debit: %v", err)
	}

	balance, _ := w.Balance(ctx, userID)
	sum, _ := w.LedgerSum(ctx, userID)
	if balance != 750 || sum != 750 {
		t.Fatalf("balance=%d sum=%d, want 750/750", balance, sum)
	}
}

func TestEnsureSignupIdempotent(t *testing.T) {
	pool := testPool(t)
	w := New(pool)
	ctx := context.Background()
	userID := testUser(t, pool, 0)

	if err := w.EnsureSignup(ctx, userID, 1000); err != nil {
		t.Fatalf("first EnsureSignup: %v", err)
	}
	if err := w.EnsureSignup(ctx, userID, 1000); err != nil {
		t.Fatalf("retry EnsureSignup: %v", err)
	}
	balance, _ := w.Balance(ctx, userID)
	if balance != 1000 {
		t.Fatalf("balance = %d, want 1000 (signup applied once)", balance)
	}
}

func TestLockWalletsSorted(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	idA := testUser(t, pool, 10)
	idB := testUser(t, pool, 10)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	// Deliberately pass unordered; the query must acquire locks ascending.
	if err := LockWalletsSorted(ctx, tx, []int64{idB, idA}); err != nil {
		t.Fatalf("LockWalletsSorted: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}
