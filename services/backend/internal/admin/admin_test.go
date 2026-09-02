package admin_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/ai-doodoo-slots/services/backend/internal/admin"
	"github.com/ai-doodoo-slots/services/backend/internal/auth"
	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
	"github.com/ai-doodoo-slots/services/backend/internal/game/slots"
	"github.com/ai-doodoo-slots/services/backend/internal/play"
	"github.com/ai-doodoo-slots/services/backend/internal/testdb"
	"github.com/ai-doodoo-slots/services/backend/internal/wallet"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newSvc(t *testing.T) (*pgxpool.Pool, *admin.Service) {
	t.Helper()
	pool := testdb.Pool(t)
	return pool, admin.NewService(pool)
}

func TestRoleAtLeast(t *testing.T) {
	if !admin.RoleAtLeast(admin.RoleAdmin, admin.RoleModerator) {
		t.Fatal("admin should satisfy moderator requirement")
	}
	if !admin.RoleAtLeast(admin.RoleModerator, admin.RoleModerator) {
		t.Fatal("moderator should satisfy moderator requirement")
	}
	if admin.RoleAtLeast(admin.RolePlayer, admin.RoleModerator) {
		t.Fatal("player must not satisfy moderator requirement")
	}
	if admin.RoleAtLeast("player", "admin") {
		t.Fatal("player must not satisfy admin requirement")
	}
}

// TestBannedUserCannotBet is the phase 11 gate: a banned user can read but
// every bet path returns forbidden. The gate lives inside the play service
// so it covers HTTP now and sockets later.
func TestBannedUserCannotBet(t *testing.T) {
	pool, svc := newSvc(t)
	ctx := context.Background()

	sess := auth.NewService(pool, clock.Real{}, testLogger())
	user, token, _, err := sess.CreateGuest(ctx, "127.0.0.1", "gate")
	if err != nil {
		t.Fatal(err)
	}
	w := wallet.New(pool)
	if err := w.EnsureSignup(ctx, user.ID, 1000); err != nil {
		t.Fatal(err)
	}
	if _, err := fair.NewService(pool).EnsureForUser(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	registry := game.NewRegistry()
	registry.Register(slots.Classic())
	ps := play.NewService(pool, registry)

	// Before the ban, betting works.
	if _, err := ps.Play(ctx, user.ID, "slots", 10, "s", fmt.Sprintf("ban:%d:a", user.ID)); err != nil {
		t.Fatalf("pre-ban play: %v", err)
	}

	// Ban (moderator actor).
	if err := svc.SetStatus(ctx, user.ID, user.ID, admin.StatusBanned, nil); err != nil {
		t.Fatalf("ban: %v", err)
	}

	// Bet path returns the status error.
	_, err = ps.Play(ctx, user.ID, "slots", 10, "s", fmt.Sprintf("ban:%d:b", user.ID))
	if !errors.Is(err, play.ErrStatusForbidsBetting) {
		t.Fatalf("want ErrStatusForbidsBetting, got %v", err)
	}

	// Reading still works: session resolves and balance is visible.
	su, err := sess.SessionFromToken(ctx, token)
	if err != nil {
		t.Fatalf("banned user cannot read: %v", err)
	}
	if _, err := w.Balance(ctx, su.UserID); err != nil {
		t.Fatalf("banned user cannot read balance: %v", err)
	}

	// Self-exclusion also blocks betting.
	if err := svc.SetStatus(ctx, user.ID, user.ID, admin.StatusSelfExcluded, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ps.Play(ctx, user.ID, "slots", 10, "s", fmt.Sprintf("ban:%d:c", user.ID)); !errors.Is(err, play.ErrStatusForbidsBetting) {
		t.Fatalf("self-excluded bet allowed: %v", err)
	}
}

// TestAdminAdjustWritesLedgerAndAudit is the phase 11 gate: no code path
// writes a balance without a transaction row; admin actions leave a trail.
func TestAdminAdjustWritesLedgerAndAudit(t *testing.T) {
	pool, svc := newSvc(t)
	ctx := context.Background()

	target := testdb.NewUser(t, pool, 1000)
	actor := testdb.NewUser(t, pool, 0)
	w := wallet.New(pool)

	balance, err := svc.AdjustBalance(ctx, actor, target, 500, "compensation")
	if err != nil {
		t.Fatalf("adjust: %v", err)
	}
	if balance != 1500 {
		t.Fatalf("balance = %d, want 1500", balance)
	}
	sum, _ := w.LedgerSum(ctx, target)
	if sum != 1500 {
		t.Fatalf("ledger sum = %d, want 1500", sum)
	}

	// The adjustment is an ordinary ledger transaction with the actor kind.
	var kind string
	if err := pool.QueryRow(ctx,
		"SELECT kind FROM transactions WHERE wallet_id = $1 AND idempotency_key LIKE 'admin-adjust:%'",
		target,
	).Scan(&kind); err != nil {
		t.Fatalf("no admin_adjust transaction: %v", err)
	}
	if kind != wallet.KindAdminAdjust {
		t.Fatalf("kind = %q", kind)
	}

	// The audit log records actor, target, and metadata.
	var action string
	var actorID int64
	if err := pool.QueryRow(ctx,
		"SELECT action, actor_user_id FROM audit_log WHERE target_id = $1 AND action = 'balance.adjust'",
		target,
	).Scan(&action, &actorID); err != nil {
		t.Fatalf("no audit entry: %v", err)
	}
	if actorID != actor {
		t.Fatalf("audit actor = %d, want %d", actorID, actor)
	}

	// Audit listing works.
	entries, err := svc.ListAudit(ctx, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("audit listing empty")
	}

	// Negative adjustment beyond balance is rejected by the ledger.
	if _, err := svc.AdjustBalance(ctx, actor, target, -99999, "test"); !errors.Is(err, wallet.ErrInsufficientFunds) {
		t.Fatalf("want ErrInsufficientFunds, got %v", err)
	}
}
