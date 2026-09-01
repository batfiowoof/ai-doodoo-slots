package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Email token kinds; matches the email_tokens CHECK constraint.
const (
	KindVerifyEmail   = "verify_email"
	KindResetPassword = "reset_password"
)

const (
	EmailTokenTTL = 24 * time.Hour
	ResetTokenTTL = time.Hour
)

var (
	ErrEmailTaken         = errors.New("email already registered")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrNotGuest           = errors.New("account already registered")
	ErrBadEmail           = errors.New("invalid email")
	ErrWeakPassword       = errors.New("password must be 8-128 characters")
	ErrBadToken           = errors.New("invalid or expired token")
)

// RegisterResult is the outcome of a registration or login.
type RegisterResult struct {
	User        store.User
	Token       string // session token (rotated on privilege change)
	Expires     time.Time
	VerifyToken string // dev "email" delivery; empty after plain login
}

// textVal wraps a required string for a nullable citext/text column.
func strPtr(s string) *string {
	return &s
}

func textVal(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: true}
}

// textToPtr converts a nullable text column back to *string.
func textToPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	v := t.String
	return &v
}

// backoffState tracks consecutive login failures per account so delays grow
// progressively. Keyed by hashed email.
type backoffState struct {
	failures int
}

// Accounts extends Service with registration, login, tokens, and session
// management. It shares the pool/clock with the session service.
type Accounts struct {
	pool   *pgxpool.Pool
	q      *store.Queries
	clock  clock.Clock
	logger *slog.Logger

	mu          sync.Mutex
	backoffs    map[string]*backoffState
	adminEmails map[string]bool
}

func NewAccounts(pool *pgxpool.Pool, clk clock.Clock, logger *slog.Logger, adminEmails ...string) *Accounts {
	return &Accounts{
		pool:     pool,
		q:        store.New(pool),
		clock:    clk,
		logger:   logger,
		backoffs: make(map[string]*backoffState),
		adminEmails: adminEmailMap(adminEmails),
	}
}

func adminEmailMap(emails []string) map[string]bool {
	m := make(map[string]bool, len(emails))
	for _, e := range emails {
		m[normalizeEmail(strings.TrimSpace(e))] = true
	}
	return m
}

// promoteIfAdmin grants the admin role when the email is listed in
// ADMIN_EMAILS.
func (a *Accounts) promoteIfAdmin(ctx context.Context, userID int64, email string) {
	if !a.adminEmails[strings.ToLower(strings.TrimSpace(email))] {
		return
	}
	if err := a.q.SetUserRole(ctx, store.SetUserRoleParams{ID: userID, Role: "admin"}); err != nil {
		a.logger.Warn("admin promotion", "err", err)
	}
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validateCredentials(email, password string) (string, error) {
	email = normalizeEmail(email)
	if len(email) < 3 || len(email) > 254 || !strings.Contains(email, "@") {
		return "", ErrBadEmail
	}
	if len(password) < 8 || len(password) > 128 {
		return "", ErrWeakPassword
	}
	return email, nil
}

// issueEmailToken stores only sha256(token); the raw value is returned once
// for "delivery".
func (a *Accounts) issueEmailToken(ctx context.Context, userID int64, kind string, ttl time.Duration) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	raw := base64.RawURLEncoding.EncodeToString(buf)
	_, err := a.q.CreateEmailToken(ctx, store.CreateEmailTokenParams{
		UserID:    userID,
		Kind:      kind,
		TokenHash: HashToken(raw),
		ExpiresAt: a.clock.Now().Add(ttl),
	})
	if err != nil {
		return "", err
	}
	return raw, nil
}

// consumeEmailToken marks a token used inside a transaction and returns the
// owning user ID.
func (a *Accounts) consumeEmailToken(ctx context.Context, tx pgx.Tx, raw, kind string) (int64, error) {
	row, err := store.New(tx).GetValidEmailToken(ctx, store.GetValidEmailTokenParams{
		TokenHash: HashToken(raw),
		Kind:      kind,
	})
	if err != nil {
		return 0, ErrBadToken
	}
	if err := store.New(tx).MarkEmailTokenUsed(ctx, row.ID); err != nil {
		return 0, err
	}
	return row.UserID, nil
}

// Register creates a fresh account, or upgrades a guest in place. On guest
// upgrade the same user row (wallet, bets, seeds) is kept and the session
// token is rotated.
func (a *Accounts) Register(ctx context.Context, su *SessionUser, currentToken, email, password, ip, userAgent string) (RegisterResult, error) {
	email, err := validateCredentials(email, password)
	if err != nil {
		return RegisterResult{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return RegisterResult{}, err
	}

	now := a.clock.Now()

	if su != nil {
		if !su.IsGuest {
			return RegisterResult{}, ErrNotGuest
		}
		// Guest upgrade in place: same row, rotated session, atomic.
		tx, err := a.pool.Begin(ctx)
		if err != nil {
			return RegisterResult{}, err
		}
		defer tx.Rollback(ctx)
		q := store.New(tx)

		user, err := q.UpgradeGuestUser(ctx, store.UpgradeGuestUserParams{
			ID:           su.UserID,
			Email:        strPtr(email),
			PasswordHash: textVal(hash),
		})
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return RegisterResult{}, ErrEmailTaken
			}
			return RegisterResult{}, err
		}
		if err := q.RevokeSessionByTokenHash(ctx, HashToken(currentToken)); err != nil {
			return RegisterResult{}, err
		}
		token, tokenHash, err := NewToken()
		if err != nil {
			return RegisterResult{}, err
		}
		expires := now.Add(SessionTTL)
		if _, err := q.CreateSession(ctx, store.CreateSessionParams{
			UserID:    user.ID,
			TokenHash: tokenHash,
			ExpiresAt: expires,
			Ip:        textPtr(ip),
			UserAgent: textPtr(userAgent),
		}); err != nil {
			return RegisterResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return RegisterResult{}, err
		}

	a.promoteIfAdmin(ctx, user.ID, email)
	verify, err := a.issueEmailToken(ctx, user.ID, KindVerifyEmail, EmailTokenTTL)
		if err != nil {
			a.logger.Warn("issue verify token", "err", err)
		}
		return RegisterResult{User: user, Token: token, Expires: expires, VerifyToken: verify}, nil
	}

	// Fresh account (no session).
	displayName := "PLAYER-" + NewOpaqueID(3)
	user, err := a.q.CreateUserRegistered(ctx, store.CreateUserRegisteredParams{
		DisplayName:  displayName,
		Email:        strPtr(email),
		PasswordHash: textVal(hash),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return RegisterResult{}, ErrEmailTaken
		}
		return RegisterResult{}, err
	}
	token, tokenHash, err := NewToken()
	if err != nil {
		return RegisterResult{}, err
	}
	expires := now.Add(SessionTTL)
	if _, err := a.q.CreateSession(ctx, store.CreateSessionParams{
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: expires,
		Ip:        textPtr(ip),
		UserAgent: textPtr(userAgent),
	}); err != nil {
		return RegisterResult{}, err
	}
	verify, err := a.issueEmailToken(ctx, user.ID, KindVerifyEmail, EmailTokenTTL)
	if err != nil {
		a.logger.Warn("issue verify token", "err", err)
	}
	return RegisterResult{User: user, Token: token, Expires: expires, VerifyToken: verify}, nil
}

// backoffDelay grows with consecutive failures, capped at 5s.
func backoffDelay(failures int) time.Duration {
	if failures <= 0 {
		return 0
	}
	d := 250 * time.Millisecond
	for i := 1; i < failures && d < 5*time.Second; i++ {
		d *= 2
	}
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// Login authenticates with generic failure text and identical timing for
// unknown emails (dummy hash) and wrong passwords.
func (a *Accounts) Login(ctx context.Context, email, password, ip, userAgent string) (RegisterResult, error) {
	email = normalizeEmail(email)
	key := HashToken(email)

	a.mu.Lock()
	b := a.backoffs[key]
	failures := 0
	if b != nil {
	failures = b.failures
	}
	delay := backoffDelay(failures)
	a.mu.Unlock()
	if delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return RegisterResult{}, ctx.Err()
		}
	}

	user, err := a.q.GetUserByEmail(ctx, strPtr(email))
	if err != nil {
		a.logger.Warn("LOGIN-DEBUG lookup-failed", "err", err)
		DummyHash() // same cost as a real verification
		a.recordFailure(key)
		return RegisterResult{}, ErrInvalidCredentials
	}
	if !user.PasswordHash.Valid {
		a.logger.Warn("LOGIN-DEBUG hash-null")
		DummyHash()
		a.recordFailure(key)
		return RegisterResult{}, ErrInvalidCredentials
	}
	ok, err := VerifyPassword(password, user.PasswordHash.String)
	if err != nil || !ok {
		a.logger.Warn("LOGIN-DEBUG verify-failed", "err", err)
		a.recordFailure(key)
		return RegisterResult{}, ErrInvalidCredentials
	}

	a.mu.Lock()
	delete(a.backoffs, key)
	a.mu.Unlock()

	token, tokenHash, err := NewToken()
	if err != nil {
		return RegisterResult{}, err
	}
	expires := a.clock.Now().Add(SessionTTL)
	if _, err := a.q.CreateSession(ctx, store.CreateSessionParams{
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: expires,
		Ip:        textPtr(ip),
		UserAgent: textPtr(userAgent),
	}); err != nil {
		return RegisterResult{}, err
	}
	// ADMIN_EMAILS promotion on login.
	a.promoteIfAdmin(ctx, user.ID, email)
	return RegisterResult{User: user, Token: token, Expires: expires}, nil
}

func (a *Accounts) recordFailure(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.backoffs[key]
	if b == nil {
		b = &backoffState{}
		a.backoffs[key] = b
	}
	b.failures++
}

// ForgotPassword issues a reset token if the account exists. The caller
// delivers it; the response is always 202 either way.
func (a *Accounts) ForgotPassword(ctx context.Context, email string) (string, error) {
	user, err := a.q.GetUserByEmail(ctx, strPtr(normalizeEmail(email)))
	if err != nil {
		return "", nil // no such account: stay silent
	}
	return a.issueEmailToken(ctx, user.ID, KindResetPassword, ResetTokenTTL)
}

// ResetPassword consumes the reset token, sets the new password, and
// revokes every session for the user.
func (a *Accounts) ResetPassword(ctx context.Context, rawToken, password string) error {
	if len(password) < 8 || len(password) > 128 {
		return ErrWeakPassword
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	userID, err := a.consumeEmailToken(ctx, tx, rawToken, KindResetPassword)
	if err != nil {
		return err
	}
	q := store.New(tx)
	if err := q.UpdatePassword(ctx, store.UpdatePasswordParams{ID: userID, PasswordHash: textVal(hash)}); err != nil {
		return err
	}
	if err := q.RevokeAllForUser(ctx, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// VerifyEmail consumes a verification token and marks the email verified.
func (a *Accounts) VerifyEmail(ctx context.Context, rawToken string) error {
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	userID, err := a.consumeEmailToken(ctx, tx, rawToken, KindVerifyEmail)
	if err != nil {
		return err
	}
	if err := store.New(tx).UpdateUserEmailVerified(ctx, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SessionInfo is a session without its token hash.
type SessionInfo struct {
	ID         int64
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	IP         *string
	UserAgent  *string
}

// ListActiveSessions returns the user's active sessions.
func (a *Accounts) ListActiveSessions(ctx context.Context, userID int64) ([]SessionInfo, error) {
	rows, err := a.q.ListActiveSessions(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]SessionInfo, 0, len(rows))
	for _, r := range rows {
		out = append(out, SessionInfo{
			ID:         r.ID,
			CreatedAt:  r.CreatedAt,
			LastSeenAt: r.LastSeenAt,
			ExpiresAt:  r.ExpiresAt,
			IP:         textToPtr(r.Ip),
			UserAgent:  textToPtr(r.UserAgent),
		})
	}
	return out, nil
}

// RevokeOwnSession revokes one of the user's sessions; ownership is
// enforced in the SQL.
func (a *Accounts) RevokeOwnSession(ctx context.Context, userID, sessionID int64) error {
	return a.q.RevokeSessionByIDForUser(ctx, store.RevokeSessionByIDForUserParams{
		ID:     sessionID,
		UserID: userID,
	})
}
