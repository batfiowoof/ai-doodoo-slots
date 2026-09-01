// Package testdb provides database-backed test fixtures. Cleanup removes
// test rows in dependency order — the ledger is append-only in production,
// so no production table carries ON DELETE CASCADE into transactions.
package testdb

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool connects to the test database, skipping the test when unavailable.
func Pool(t *testing.T) *pgxpool.Pool {
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

// NewUser creates an isolated guest user with a wallet funded to
// startingCredits and registers ordered cleanup.
func NewUser(t *testing.T, pool *pgxpool.Pool, startingCredits int64) int64 {
	t.Helper()
	ctx := context.Background()

	var userID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (display_name, is_guest) VALUES ($1, true) RETURNING id`,
		fmt.Sprintf("TEST-%d-%d", clock.Real{}.Now().UnixNano(), os.Getpid()),
	).Scan(&userID); err != nil {
		t.Fatalf("create test user: %v", err)
	}
	t.Cleanup(func() { cleanupUser(context.Background(), t, pool, userID) })

	if startingCredits > 0 {
		if _, err := pool.Exec(ctx,
			`INSERT INTO wallets (user_id, balance_credits) VALUES ($1, $2)`,
			userID, startingCredits,
		); err != nil {
			t.Fatalf("create wallet: %v", err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO transactions (wallet_id, kind, amount_credits, idempotency_key)
			 VALUES ($1, 'signup_bonus', $2, $3)`,
			userID, startingCredits, fmt.Sprintf("testdb:signup:%d", userID),
		); err != nil {
			t.Fatalf("fund wallet: %v", err)
		}
	} else if _, err := pool.Exec(ctx,
		`INSERT INTO wallets (user_id, balance_credits) VALUES ($1, 0)`, userID,
	); err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	return userID
}

// cleanupUser removes a test user and all dependent rows in dependency
// order. The ledger has no cascades into transactions by design.
func cleanupUser(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userID int64) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`DELETE FROM transactions WHERE wallet_id = $1`, userID,
	); err != nil {
		t.Errorf("test cleanup failed: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM users WHERE id = $1`, userID,
	); err != nil {
		t.Errorf("test cleanup failed: %v", err)
	}
	// Synthetic rounds orphaned when the cascade removed this user's bets.
	if _, err := pool.Exec(ctx,
		`DELETE FROM rounds WHERE NOT EXISTS (SELECT 1 FROM bets WHERE bets.round_id = rounds.id)`,
	); err != nil {
		t.Errorf("test cleanup failed: %v", err)
	}
}
