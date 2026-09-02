package auth

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/ai-doodoo-slots/services/backend/internal/testdb"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestService builds a Service against the test database.
func newTestService(t *testing.T) *Service {
	t.Helper()
	pool := testdb.Pool(t)
	return NewService(pool, clock.Real{}, testLogger())
}

// cleanupUser removes a user provisioned by the code under test (which has
// no testdb-registered cleanup of its own).
func cleanupUser(t *testing.T, pool *pgxpool.Pool, userID int64) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Errorf("test cleanup failed: %v", err)
		}
	})
}

// newGuestWithSession creates a guest user plus an active session, the way
// POST /api/v1/auth/guest does.
func newGuestWithSession(t *testing.T, s *Service) (int64, string) {
	t.Helper()
	user, token, _, err := s.CreateGuest(context.Background(), "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("create guest: %v", err)
	}
	return user.ID, token
}

func TestKeycloakProvisionsNewUser(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	email := fmt.Sprintf("fan-%s@example.test", NewOpaqueID(6))

	res, err := s.ResolveKeycloakUser(ctx, &KeycloakClaims{
		Subject:       "kc-sub-" + NewOpaqueID(6),
		PreferredName: "arcade_fan",
		Email:         email,
		EmailVerified: true,
		RealmRoles:    []string{"player"},
	}, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	cleanupUser(t, testdb.Pool(t), res.User.UserID)
	if !res.Created {
		t.Fatal("expected a newly provisioned user")
	}
	if res.User.IsGuest {
		t.Error("provisioned user must not be a guest")
	}
	if res.User.DisplayName != "arcade_fan" {
		t.Errorf("displayName = %q", res.User.DisplayName)
	}
	if res.User.Email == nil || *res.User.Email != email {
		t.Errorf("email = %v, want %s", res.User.Email, email)
	}
	if res.User.EmailVerifiedAt == nil {
		t.Error("email_verified_at should be set for a verified claim")
	}
}

func TestKeycloakSameSubResolvesSameUser(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	sub := "kc-sub-" + NewOpaqueID(6)

	res1, err := s.ResolveKeycloakUser(ctx, &KeycloakClaims{Subject: sub}, "")
	if err != nil {
		t.Fatalf("resolve 1: %v", err)
	}
	cleanupUser(t, testdb.Pool(t), res1.User.UserID)
	res2, err := s.ResolveKeycloakUser(ctx, &KeycloakClaims{Subject: sub}, "")
	if err != nil {
		t.Fatalf("resolve 2: %v", err)
	}
	if res2.Created {
		t.Error("second resolve must not create a new user")
	}
	if res1.User.UserID != res2.User.UserID {
		t.Errorf("user ids differ: %d vs %d", res1.User.UserID, res2.User.UserID)
	}
}

func TestKeycloakRoleSyncFromToken(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	sub := "kc-sub-" + NewOpaqueID(6)

	res, err := s.ResolveKeycloakUser(ctx, &KeycloakClaims{Subject: sub, RealmRoles: []string{"moderator"}}, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	cleanupUser(t, testdb.Pool(t), res.User.UserID)
	if res.User.Role != "moderator" {
		t.Fatalf("role = %q, want moderator", res.User.Role)
	}
	// Role promoted in the token flows through on the next resolve.
	res2, err := s.ResolveKeycloakUser(ctx, &KeycloakClaims{Subject: sub, RealmRoles: []string{"admin"}}, "")
	if err != nil {
		t.Fatalf("resolve 2: %v", err)
	}
	if res2.User.Role != "admin" {
		t.Errorf("role = %q, want admin", res2.User.Role)
	}
}

// TestKeycloakUpgradeGuestPreservesUserRow is the phase-10 gate carried over
// to the Keycloak path: a guest with a wallet, bets, and seeds registers and
// retains every row, with the guest session revoked.
func TestKeycloakUpgradeGuestPreservesUserRow(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()

	guestID, guestToken := newGuestWithSession(t, s)

	// Give the guest a seed pair (wallet/bet linkage is covered by the
	// wallet and play tests; this gate pins user-row preservation).
	if _, err := s.q.CreateServerSeed(ctx, store.CreateServerSeedParams{
		UserID:     guestID,
		SeedHash:   "hash-" + guestToken,
		SeedPlain:  pgtype.Text{String: "plain", Valid: true},
		ClientSeed: "client-seed",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	sub := "kc-sub-" + NewOpaqueID(6)
	res, err := s.ResolveKeycloakUser(ctx, &KeycloakClaims{
		Subject:       sub,
		PreferredName: "upgraded_gamer",
		Email:         fmt.Sprintf("gamer-%s@example.test", NewOpaqueID(6)),
		EmailVerified: true,
		RealmRoles:    []string{"player"},
	}, guestToken)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !res.Upgraded {
		t.Fatal("expected in-place upgrade")
	}
	if res.User.UserID != guestID {
		t.Fatalf("upgraded user id = %d, want guest id %d", res.User.UserID, guestID)
	}
	if res.User.IsGuest {
		t.Error("user must no longer be a guest")
	}

	// The old session is revoked.
	if _, err := s.SessionFromToken(ctx, guestToken); err == nil {
		t.Error("guest session should be revoked after upgrade")
	}

	// The seed pair survived: the same row still owns it.
	active, err := s.q.GetActiveServerSeed(ctx, guestID)
	if err != nil {
		t.Fatalf("active seed: %v", err)
	}
	if active.UserID != guestID {
		t.Errorf("seed owner = %d, want %d", active.UserID, guestID)
	}
}

func TestKeycloakUnknownSubWithoutGuestCreatesUser(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()

	res, err := s.ResolveKeycloakUser(ctx, &KeycloakClaims{
		Subject:       "kc-sub-" + NewOpaqueID(6),
		PreferredName: "no_guest",
	}, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	cleanupUser(t, testdb.Pool(t), res.User.UserID)
	if !res.Created || res.Upgraded {
		t.Fatalf("created=%v upgraded=%v, want created only", res.Created, res.Upgraded)
	}
}
