package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// textPtr converts an optional string to pgtype.Text.
func textPtr(v string) pgtype.Text {
	if v == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: v, Valid: true}
}

const (
	// SessionTTL is the sliding expiry applied on activity.
	SessionTTL = 30 * 24 * time.Hour
	// SessionAbsoluteCap bounds total session lifetime regardless of activity.
	SessionAbsoluteCap = 90 * 24 * time.Hour
	// touchInterval throttles last_seen writes.
	touchInterval = time.Hour
)

var ErrNoSession = errors.New("no active session")

// Service issues and resolves sessions.
type Service struct {
	pool   *pgxpool.Pool
	q      *store.Queries
	clock  clock.Clock
	logger *slog.Logger
}

func NewService(pool *pgxpool.Pool, clk clock.Clock, logger *slog.Logger) *Service {
	return &Service{pool: pool, q: store.New(pool), clock: clk, logger: logger}
}

// SessionUser is the authenticated identity attached to a request.
type SessionUser struct {
	SessionID       int64
	UserID          int64
	IsGuest         bool
	DisplayName     string
	Email           *string
	EmailVerifiedAt *time.Time
	Role            string
	Status          string
	CreatedAt       time.Time
	ExpiresAt       time.Time
	LastSeenAt      time.Time
	// Avatar is a preset sprite name; empty means no preset (an uploaded
	// image may still exist, keyed by AvatarVersion > 0).
	AvatarPreset  string
	AvatarVersion int64
	// Subject is the Keycloak sub (empty for guests). It addresses the
	// Keycloak user for profile write-back.
	Subject string
	// DisplayNameUpdatedAt gates the rename cooldown; nil = never renamed.
	DisplayNameUpdatedAt *time.Time
}

// CanBet reports whether the account status permits betting. Banned and
// self-excluded accounts may read but never bet.
func (u *SessionUser) CanBet() bool {
	return u.Status == "active"
}

// CreateGuest creates a guest user and its session, returning the user, the
// raw token (client gets it via cookie), and the expiry.
func (s *Service) CreateGuest(ctx context.Context, ip, userAgent string) (store.User, string, time.Time, error) {
	displayName := "GUEST-" + NewOpaqueID(3)

	token, tokenHash, err := NewToken()
	if err != nil {
		return store.User{}, "", time.Time{}, fmt.Errorf("generate token: %w", err)
	}

	now := s.clock.Now()
	user, err := s.q.CreateUserGuest(ctx, displayName)
	if err != nil {
		return store.User{}, "", time.Time{}, fmt.Errorf("create guest user: %w", err)
	}
	_, err = s.q.CreateSession(ctx, store.CreateSessionParams{
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: now.Add(SessionTTL),
		Ip:        textPtr(ip),
		UserAgent: textPtr(userAgent),
	})
	if err != nil {
		return store.User{}, "", time.Time{}, fmt.Errorf("create session: %w", err)
	}
	return user, token, now.Add(SessionTTL), nil
}

// SessionFromToken resolves a raw token to an active session user, sliding
// last_seen_at forward at most once per touchInterval.
func (s *Service) SessionFromToken(ctx context.Context, rawToken string) (*SessionUser, error) {
	row, err := s.q.GetActiveSessionByTokenHash(ctx, HashToken(rawToken))
	if err != nil {
		return nil, ErrNoSession
	}

	su := &SessionUser{
		SessionID:       row.SessionID,
		UserID:          row.UserID,
		IsGuest:         row.IsGuest,
		DisplayName:     row.DisplayName,
		Email:           row.Email,
		EmailVerifiedAt: row.EmailVerifiedAt,
		Role:            row.Role,
		Status:          row.Status,
		CreatedAt:       row.UserCreatedAt,
		ExpiresAt:       row.ExpiresAt,
		LastSeenAt:      row.LastSeenAt,
		AvatarPreset:    row.AvatarPreset.String,
		AvatarVersion:   row.AvatarVersion,
		DisplayNameUpdatedAt: row.DisplayNameUpdatedAt,
	}

	if s.clock.Now().Sub(row.LastSeenAt) > touchInterval {
		if err := s.q.TouchSession(ctx, store.TouchSessionParams{ID: row.SessionID, LastSeenAt: s.clock.Now()}); err != nil {
			s.logger.Warn("touch session", "err", err)
		}
	}
	return su, nil
}

// Logout revokes the session behind a raw token.
func (s *Service) Logout(ctx context.Context, rawToken string) error {
	return s.q.RevokeSessionByTokenHash(ctx, HashToken(rawToken))
}
