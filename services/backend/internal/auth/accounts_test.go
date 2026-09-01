package auth_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"os"

	"github.com/ai-doodoo-slots/services/backend/internal/auth"
	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"fmt"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
	"github.com/ai-doodoo-slots/services/backend/internal/game/slots"
	"github.com/ai-doodoo-slots/services/backend/internal/play"
	"strings"
	"github.com/ai-doodoo-slots/services/backend/internal/testdb"
	"github.com/ai-doodoo-slots/services/backend/internal/wallet"
	"github.com/jackc/pgx/v5/pgxpool"
)

var testRunID = auth.NewOpaqueID(4)

func testEmail(prefix string) string {
	return fmt.Sprintf("%s-%s@example.com", prefix, testRunID)
}

func newAuth(t *testing.T, pool *pgxpool.Pool) (*auth.Service, *auth.Accounts) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(testDiscard{}, nil))
	return auth.NewService(pool, clock.Real{}, logger), auth.NewAccounts(pool, clock.Real{}, logger)
}

type testDiscard struct{}

func (testDiscard) Write(p []byte) (int, error) { return os.Stderr.Write(p) }

// TestGuestUpgradePreservesEverything is the phase 10 gate: a guest with
// bets and a wallet registers and retains every row, with a rotated session
// token.
func TestGuestUpgradePreservesEverything(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()
	svcS, svc := newAuth(t, pool)

	// 1. Guest with a wallet, a bet, and a seed pair.
	user, token, _, err := svcS.CreateGuest(ctx, "127.0.0.1", "gate-test")
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
	registry.Register(slots.New())
	betRes, err := play.NewService(pool, registry).Play(ctx, user.ID, "slots", 10, "upgrade-seed", fmt.Sprintf("upgrade-bet:%d", user.ID))
	if err != nil {
		t.Fatalf("guest bet: %v", err)
	}
	balanceBefore, err := w.Balance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	var seedBefore int64
	if err := pool.QueryRow(ctx,
		`SELECT id FROM server_seeds WHERE user_id = $1 AND is_active`, user.ID,
	).Scan(&seedBefore); err != nil {
		t.Fatal(err)
	}

	// 2. Register against the guest session.
	su, err := svcS.SessionFromToken(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := svc.Register(ctx, su, token, testEmail("gate"), "hunter2hunter2", "127.0.0.1", "gate-test")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// Same row: wallet, bets, seeds intact.
	balanceAfter, _ := w.Balance(ctx, user.ID)
	if balanceAfter != balanceBefore {
		t.Fatalf("balance %d -> %d, want unchanged", balanceBefore, balanceAfter)
	}
	sum, _ := w.LedgerSum(ctx, user.ID)
	if sum != balanceAfter {
		t.Fatalf("ledger sum %d != balance %d", sum, balanceAfter)
	}
	var bets int64
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM bets WHERE user_id = $1`, user.ID).Scan(&bets); err != nil {
		t.Fatal(err)
	}
	if bets != 1 || betRes.BetID == 0 {
		t.Fatalf("bets=%d after upgrade, want 1 (same user row)", bets)
	}
	var seedAfter int64
	if err := pool.QueryRow(ctx,
		`SELECT id FROM server_seeds WHERE user_id = $1 AND is_active`, user.ID,
	).Scan(&seedAfter); err != nil {
		t.Fatal(err)
	}
	if seedAfter != seedBefore {
		t.Fatalf("seed row %d -> %d, want same seed pair", seedBefore, seedAfter)
	}

	// Session rotated: old token dead, new token live and non-guest.
	if _, err := svcS.SessionFromToken(ctx, token); !errors.Is(err, auth.ErrNoSession) {
		t.Fatalf("old session still active: %v", err)
	}
	su2, err := svcS.SessionFromToken(ctx, reg.Token)
	if err != nil {
		t.Fatalf("new session dead: %v", err)
	}
	if su2.IsGuest || su2.UserID != user.ID {
		t.Fatalf("new session wrong identity: isGuest=%v userID=%d", su2.IsGuest, su2.UserID)
	}
	if reg.Token == token {
		t.Fatal("session token was not rotated")
	}
}

func TestRegisterFreshAccountAndLogin(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()
	svcS, svc := newAuth(t, pool)
	w := wallet.New(pool)

	// Fresh registration without a session.
	reg, err := svc.Register(ctx, nil, "", testEmail("fresh"), "password123", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	if reg.User.IsGuest {
		t.Fatal("fresh registration still a guest")
	}
	if reg.VerifyToken == "" {
		t.Fatal("no verification token issued")
	}
	// Handler provisions wallet + seed on the fresh path.
	if err := w.EnsureSignup(ctx, reg.User.ID, 1000); err != nil {
		t.Fatal(err)
	}

	// Login works.
	login, err := svc.Login(ctx, strings.ToUpper(testEmail("fresh")), "password123", "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if login.User.ID != reg.User.ID {
		t.Fatal("login resolved a different user")
	}

	// Wrong password and unknown email: identical generic error.
	if _, err := svc.Login(ctx, testEmail("fresh"), "wrong-password", "127.0.0.1", "test"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("wrong password: want ErrInvalidCredentials, got %v", err)
	}
	if _, err := svc.Login(ctx, testEmail("nobody"), "password123", "127.0.0.1", "test"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("unknown email: want ErrInvalidCredentials, got %v", err)
	}

	// Duplicate registration is rejected.
	su, _ := svcS.SessionFromToken(ctx, login.Token)
	if _, err := svc.Register(ctx, nil, "", testEmail("fresh"), "password123", "127.0.0.1", "test"); !errors.Is(err, auth.ErrEmailTaken) {
		t.Fatalf("want ErrEmailTaken, got %v", err)
	}
	_ = su
}

func TestEmailVerification(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()
	_, svc := newAuth(t, pool)

	reg, err := svc.Register(ctx, nil, "", testEmail("verify"), "password123", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.VerifyEmail(ctx, reg.VerifyToken); err != nil {
		t.Fatalf("verify: %v", err)
	}
	// Token is single-use.
	if err := svc.VerifyEmail(ctx, reg.VerifyToken); !errors.Is(err, auth.ErrBadToken) {
		t.Fatalf("reused token accepted: %v", err)
	}
	// Email is now marked verified.
	var verified bool
	if err := pool.QueryRow(ctx,
		`SELECT email_verified_at IS NOT NULL FROM users WHERE id = $1`, reg.User.ID,
	).Scan(&verified); err != nil {
		t.Fatal(err)
	}
	if !verified {
		t.Fatal("email_verified_at not set")
	}
}

// TestPasswordResetRevokesAllSessions is the phase 10 gate: consuming a
// reset token revokes every other session.
func TestPasswordResetRevokesAllSessions(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()
	svcS, svc := newAuth(t, pool)
	w := wallet.New(pool)

	resetEmail := testEmail("reset")
	reg, err := svc.Register(ctx, nil, "", resetEmail, "oldpassword1", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.EnsureSignup(ctx, reg.User.ID, 1000); err != nil {
		t.Fatal(err)
	}
	// A second session (login from "another device").
	second, err := svc.Login(ctx, testEmail("reset"), "oldpassword1", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}

	// Forgot returns a token; reset consumes it and kills both sessions.
	resetToken, err := svc.ForgotPassword(ctx, testEmail("reset"))
	if err != nil || resetToken == "" {
		t.Fatalf("forgot: token=%q err=%v", resetToken, err)
	}
	if err := svc.ResetPassword(ctx, resetToken, "newpassword9"); err != nil {
		t.Fatalf("reset: %v", err)
	}

	for name, tok := range map[string]string{"first": reg.Token, "second": second.Token} {
		if _, err := svcS.SessionFromToken(ctx, tok); !errors.Is(err, auth.ErrNoSession) {
			t.Fatalf("%s session survived reset: %v", name, err)
		}
	}

	// Old password no longer works; new one does; token reuse rejected.
	if _, err := svc.Login(ctx, testEmail("reset"), "oldpassword1", "127.0.0.1", "test"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatal("old password still works")
	}
	if _, err := svc.Login(ctx, testEmail("reset"), "newpassword9", "127.0.0.1", "test"); err != nil {
		t.Fatalf("new password rejected: %v", err)
	}
	if err := svc.ResetPassword(ctx, resetToken, "another-password"); !errors.Is(err, auth.ErrBadToken) {
		t.Fatal("reset token reused")
	}
}

func TestSessionListAndRevoke(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()
	svcS, svc := newAuth(t, pool)

	reg, err := svc.Register(ctx, nil, "", testEmail("sessions"), "password123", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	// Session tokens from Register: reg.Token is session #1.
	if _, err := svcS.SessionFromToken(ctx, reg.Token); err != nil {
		t.Fatal(err)
	}
	sessions, err := svc.ListActiveSessions(ctx, reg.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions=%d, want 1", len(sessions))
	}
	// Another user cannot revoke someone else's session — ownership in SQL.
	other, _, _, err := svcS.CreateGuest(ctx, "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RevokeOwnSession(ctx, other.ID, sessions[0].ID); err != nil {
		t.Fatalf("cross-user revoke errored (it should just no-op): %v", err)
	}
	if _, err := svcS.SessionFromToken(ctx, reg.Token); err != nil {
		t.Fatal("cross-user revoke killed another user's session")
	}
	// Owner revokes their own session.
	if err := svc.RevokeOwnSession(ctx, reg.User.ID, sessions[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svcS.SessionFromToken(ctx, reg.Token); !errors.Is(err, auth.ErrNoSession) {
		t.Fatal("own session survived revocation")
	}
}
