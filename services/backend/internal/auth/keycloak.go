package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// OauthProviderKeycloak is the provider key stored in oauth_identities.
const OauthProviderKeycloak = "keycloak"

// ResolveResult reports which local user a verified Keycloak identity maps
// to and whether that identity provisioned a new user this call.
type ResolveResult struct {
	User    *SessionUser
	Created bool
	// Upgraded is true when an existing guest user was upgraded in place
	// by linking this identity. The guest's session token was revoked.
	Upgraded bool
}

// ResolveKeycloakUser maps a verified Keycloak identity to the local user
// row, provisioning or upgrading as needed, and syncs role/email state from
// the token. With a guest token, the guest is upgraded in place — the same
// user row keeps its wallet, bets, and seeds, and the guest session is
// revoked (the caller rotates to Keycloak-issued credentials).
func (s *Service) ResolveKeycloakUser(ctx context.Context, c *KeycloakClaims, guestToken string) (ResolveResult, error) {
	// Best-effort guest resolution: a stale/invalid cookie is simply no guest.
	var guestSessionID int64
	var guestUserID int64
	if guestToken != "" {
		if row, err := s.q.GetActiveSessionByTokenHash(ctx, HashToken(guestToken)); err == nil && row.IsGuest {
			guestSessionID = row.SessionID
			guestUserID = row.UserID
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ResolveResult{}, err
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)

	identity, idErr := q.GetOauthIdentity(ctx, store.GetOauthIdentityParams{
		Provider:       OauthProviderKeycloak,
		ProviderUserID: c.Subject,
	})

	var user store.User
	var created, upgraded bool
	switch {
	case idErr == nil:
		user, err = q.GetUserByID(ctx, identity.UserID)
		if err != nil {
			return ResolveResult{}, fmt.Errorf("identity points at missing user %d: %w", identity.UserID, err)
		}
		// A stale guest session behind a known identity is revoked; the
		// caller has rotated to Keycloak credentials.
		if guestSessionID != 0 && guestUserID != user.ID {
			_ = q.RevokeSessionByID(ctx, guestSessionID)
		}
		if err := s.syncFromClaims(ctx, q, &user, c); err != nil {
			return ResolveResult{}, err
		}
	case errors.Is(idErr, pgx.ErrNoRows):
		// First login with this identity: upgrade the in-progress guest
		// if there is one, otherwise provision a fresh user.
		if guestUserID != 0 {
			var verified *time.Time
			if c.EmailVerified {
				now := s.clock.Now()
				verified = &now
			}
			upgradedUser, err := q.UpgradeGuestForKeycloak(ctx, store.UpgradeGuestForKeycloakParams{
				ID:              guestUserID,
				DisplayName:     c.PreferredName,
				Email:           strPtr(c.Email),
				EmailVerifiedAt: verified,
			})
			if err != nil {
				return ResolveResult{}, fmt.Errorf("upgrade guest: %w", err)
			}
			user = upgradedUser
			if err := q.InsertOauthIdentity(ctx, store.InsertOauthIdentityParams{
				Provider:       OauthProviderKeycloak,
				ProviderUserID: c.Subject,
				UserID:         user.ID,
			}); err != nil {
				return ResolveResult{}, err
			}
			if guestSessionID != 0 {
				_ = q.RevokeSessionByID(ctx, guestSessionID)
			}
			if err := audit(ctx, q, user.ID, "guest.upgrade", user.ID, map[string]any{
				"provider": OauthProviderKeycloak,
			}); err != nil {
				return ResolveResult{}, err
			}
			upgraded = true
			if err := s.syncFromClaims(ctx, q, &user, c); err != nil {
				return ResolveResult{}, err
			}
		} else {
			var verified *time.Time
			if c.EmailVerified {
				now := s.clock.Now()
				verified = &now
			}
			user, err = q.CreateUserFromKeycloak(ctx, store.CreateUserFromKeycloakParams{
				DisplayName:     displayNameFor(c),
				Email:           strPtr(c.Email),
				EmailVerifiedAt: verified,
			})
			if err != nil {
				return ResolveResult{}, fmt.Errorf("create user from keycloak: %w", err)
			}
			if err := q.InsertOauthIdentity(ctx, store.InsertOauthIdentityParams{
				Provider:       OauthProviderKeycloak,
				ProviderUserID: c.Subject,
				UserID:         user.ID,
			}); err != nil {
				return ResolveResult{}, err
			}
			if err := audit(ctx, q, user.ID, "user.provision", user.ID, map[string]any{
				"provider": OauthProviderKeycloak,
			}); err != nil {
				return ResolveResult{}, err
			}
			created = true
			if err := s.syncFromClaims(ctx, q, &user, c); err != nil {
				return ResolveResult{}, err
			}
		}
	default:
		return ResolveResult{}, fmt.Errorf("lookup identity: %w", idErr)
	}

	if err := tx.Commit(ctx); err != nil {
		return ResolveResult{}, err
	}
	return ResolveResult{User: sessionUserFromStore(&user), Created: created, Upgraded: upgraded}, nil
}

// syncFromClaims updates local role (and verification state) from the
// verified token. Role authority is the IDP; status (banned/self_excluded)
// stays local.
func (s *Service) syncFromClaims(ctx context.Context, q *store.Queries, user *store.User, c *KeycloakClaims) error {
	desired := RoleFromRealmRoles(c.RealmRoles)
	if user.Role != desired {
		if err := q.SetUserRole(ctx, store.SetUserRoleParams{ID: user.ID, Role: desired}); err != nil {
			return err
		}
		user.Role = desired
		if err := audit(ctx, q, user.ID, "role.sync", user.ID, map[string]any{
			"provider": OauthProviderKeycloak,
			"role":     desired,
		}); err != nil {
			return err
		}
	}
	if c.EmailVerified && user.EmailVerifiedAt == nil {
		if err := q.UpdateUserEmailVerified(ctx, user.ID); err != nil {
			return err
		}
		now := s.clock.Now()
		user.EmailVerifiedAt = &now
	}
	return nil
}

func displayNameFor(c *KeycloakClaims) string {
	if c.PreferredName != "" {
		return c.PreferredName
	}
	if c.Email != "" {
		return c.Email
	}
	return "PLAYER-" + NewOpaqueID(4)
}

// strPtr maps an optional claim string to a nullable column value.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func sessionUserFromStore(u *store.User) *SessionUser {
	su := &SessionUser{
		UserID:      u.ID,
		IsGuest:     u.IsGuest,
		DisplayName: u.DisplayName,
		Role:        u.Role,
		Status:      u.Status,
		CreatedAt:   u.CreatedAt,
	}
	if u.Email != nil {
		su.Email = u.Email
	}
	if u.EmailVerifiedAt != nil {
		t := *u.EmailVerifiedAt
		su.EmailVerifiedAt = &t
	}
	return su
}

func audit(ctx context.Context, q *store.Queries, actorID int64, action string, targetID int64, meta map[string]any) error {
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = q.InsertAuditEntry(ctx, store.InsertAuditEntryParams{
		ActorUserID: pgtype.Int8{Int64: actorID, Valid: actorID != 0},
		Action:      action,
		TargetType:  pgtype.Text{String: "user", Valid: targetID != 0},
		TargetID:    pgtype.Int8{Int64: targetID, Valid: targetID != 0},
		Metadata:    metaJSON,
	})
	return err
}
