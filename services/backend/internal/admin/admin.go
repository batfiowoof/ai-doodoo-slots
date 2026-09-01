// Package admin implements moderation and admin actions with a full audit
// trail. Balance adjustments are ordinary ledger transactions (kind
// admin_adjust) — nothing anywhere writes wallets.balance_credits without a
// matching transaction row.
package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/ai-doodoo-slots/services/backend/internal/wallet"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Roles, ordered by privilege.
const (
	RolePlayer    = "player"
	RoleModerator = "moderator"
	RoleAdmin     = "admin"
)

// Account statuses.
const (
	StatusActive       = "active"
	StatusBanned       = "banned"
	StatusSelfExcluded = "self_excluded"
)

// StatusForbidsBetting: banned and self-excluded accounts can read but
// cannot reach any bet path.
func StatusForbidsBetting(status string) bool {
	return status == StatusBanned || status == StatusSelfExcluded
}

// RoleAtLeast reports whether role meets the required privilege level.
func RoleAtLeast(role, required string) bool {
	rank := map[string]int{RolePlayer: 1, RoleModerator: 2, RoleAdmin: 3}
	return rank[role] >= rank[required]
}

// Service performs moderation and admin actions with an audit trail.
type Service struct {
	pool *pgxpool.Pool
	q    *store.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: store.New(pool)}
}

// uniq produces a random idempotency key suffix; each admin adjustment is a
// distinct ledger action.
func uniq() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// SetStatus bans, unbans, or self-excludes a user and writes the audit row
// in the same transaction.
func (s *Service) SetStatus(ctx context.Context, actorID int64, targetID int64, status string, statusUntil *time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)

	if err := q.SetUserStatus(ctx, store.SetUserStatusParams{ID: targetID, Status: status, StatusUntil: statusUntil}); err != nil {
		return err
	}
	meta := map[string]any{"status": status}
	if statusUntil != nil {
		meta["status_until"] = statusUntil
	}
	if err := insertAudit(ctx, q, actorID, "user."+status, "user", targetID, meta); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AdjustBalance writes an ordinary ledger transaction with kind
// admin_adjust and records the actor in the audit log.
func (s *Service) AdjustBalance(ctx context.Context, actorID, targetID, amount int64, reason string) (int64, error) {
	if amount == 0 {
		return 0, wallet.ErrIdempotencyConflict // zero adjustments are meaningless; reject
	}
	if reason == "" {
		return 0, fmt.Errorf("reason is required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	res, err := wallet.ApplyTx(ctx, tx, wallet.ApplyRequest{
		UserID:         targetID,
		Kind:           wallet.KindAdminAdjust,
		Amount:         amount,
		IdempotencyKey: fmt.Sprintf("admin-adjust:%d:%s", actorID, uniq()),
	})
	if err != nil {
		return 0, err
	}
	if err := insertAudit(ctx, store.New(tx), actorID, "balance.adjust", "user", targetID, map[string]any{
		"amount_credits": amount,
		"reason":         reason,
		"transaction_id": res.TransactionID,
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return res.Balance, nil
}

// AuditEntry is one audit log row for display.
type AuditEntry struct {
	ID          int64
	ActorUserID pgtype.Int8
	Action      string
	TargetType  pgtype.Text
	TargetID    pgtype.Int8
	Metadata    []byte
	CreatedAt   time.Time
}

// ListAudit returns paginated audit entries (admin only, enforced by the
// handler's RBAC).
func (s *Service) ListAudit(ctx context.Context, cursor *int64, limit int32) ([]AuditEntry, error) {
	rows, err := s.q.ListAuditEntries(ctx, store.ListAuditEntriesParams{
		Cursor: pgInt8(cursor),
		Lim:    limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]AuditEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, AuditEntry(r))
	}
	return out, nil
}

func insertAudit(ctx context.Context, q *store.Queries, actorID int64, action, targetType string, targetID int64, meta map[string]any) error {
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = q.InsertAuditEntry(ctx, store.InsertAuditEntryParams{
		ActorUserID: pgtype.Int8{Int64: actorID, Valid: actorID != 0},
		Action:      action,
		TargetType:  pgtype.Text{String: targetType, Valid: targetType != ""},
		TargetID:    pgtype.Int8{Int64: targetID, Valid: targetID != 0},
		Metadata:    metaJSON,
	})
	return err
}


func pgInt8(v *int64) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *v, Valid: true}
}
