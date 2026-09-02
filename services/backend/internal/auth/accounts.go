package auth

import (
	"context"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// SessionInfo is a session without its token hash.
type SessionInfo struct {
	ID         int64
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	IP         *string
	UserAgent  *string
}

// ListActiveSessions returns the user's active sessions. Registered users
// authenticate via Keycloak and hold no local session rows, so this only
// reports guest sessions.
func (s *Service) ListActiveSessions(ctx context.Context, userID int64) ([]SessionInfo, error) {
	rows, err := s.q.ListActiveSessions(ctx, userID)
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
func (s *Service) RevokeOwnSession(ctx context.Context, userID, sessionID int64) error {
	return s.q.RevokeSessionByIDForUser(ctx, store.RevokeSessionByIDForUserParams{
		ID:     sessionID,
		UserID: userID,
	})
}

// textToPtr converts a nullable text column to *string.
func textToPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	v := t.String
	return &v
}
