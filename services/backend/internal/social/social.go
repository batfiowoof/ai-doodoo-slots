// Package social implements the chat / emote / tip / rain surface behind the
// hub's SocialHandler. Chat lines and moderation are persisted; emotes are
// ephemeral. Money movement (tips, rain) rides the same append-only ledger
// as bets: wallets locked in sorted order inside one transaction, balance
// materialization in the same transaction as the ledger rows.
package social

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/shop"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/ai-doodoo-slots/services/backend/internal/wallet"
	"github.com/ai-doodoo-slots/services/backend/internal/ws"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Bounds for player-to-player money. Tips are small gestures; rain is a
// scene, so its floor is higher.
const (
	maxTipCredits  = 100_000
	minRainTotal   = 10
	maxRainTotal   = 1_000_000
	chatBodyMaxLen = 256
)

// codedError carries a stable client-facing code through the hub's error
// envelope (the same convention the bet paths use).
type codedError struct{ code, msg string }

func (e codedError) Error() string { return e.msg }
func (e codedError) Code() string  { return e.code }

// Service is the gameserver's SocialHandler.
type Service struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
	shop   *shop.Service // emote-pack entitlement gate
	clk    clock.Clock
}

func New(pool *pgxpool.Pool, logger *slog.Logger, clk clock.Clock) *Service {
	return &Service{pool: pool, logger: logger, shop: shop.NewService(pool), clk: clk}
}

// uniqueStamp makes ledger idempotency keys unique per social event (these
// are intentional one-shots, not retried money moves).
func (s *Service) uniqueStamp() string {
	return strconv.FormatInt(s.clk.Now().UnixNano(), 36)
}

// comma renders 1234567 as "1,234,567" for chat lines.
func comma(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var out []byte
	for i, d := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, d)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

// ChatPayload is the chat_message broadcast shape for a freshly inserted row.
func ChatPayload(row store.ChatMessage, id ws.Identity) map[string]any {
	return map[string]any{
		"id":            row.ID,
		"userId":        row.UserID,
		"displayName":   id.DisplayName,
		"avatarPreset":  id.AvatarPreset,
		"avatarVersion": id.AvatarVersion,
		"role":          id.Role,
		"title":         id.Title,
		"nameEffect":    id.NameEffect,
		"kind":          row.Kind,
		"body":          row.Body,
		"createdAt":     row.CreatedAt,
	}
}

// HistoryPayload is the same shape for a history JOIN row (REST replay).
func HistoryPayload(row store.ListRecentChatMessagesRow) map[string]any {
	return map[string]any{
		"id":            row.ID,
		"userId":        row.UserID,
		"displayName":   row.DisplayName,
		"avatarPreset":  row.AvatarPreset,
		"avatarVersion": row.AvatarVersion,
		"role":          row.Role,
		"title":         row.Title,
		"nameEffect":    row.NameEffect,
		"kind":          row.Kind,
		"body":          row.Body,
		"createdAt":     row.CreatedAt,
	}
}

// checkMute returns a coded "muted" error when the user is currently muted.
func (s *Service) checkMute(ctx context.Context, userID int64) error {
	_, err := store.New(s.pool).GetActiveChatMute(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		s.logger.Error("mute lookup", "err", err)
		return codedError{"internal", "mute lookup failed"}
	}
	return codedError{"muted", "you are muted"}
}

// SendChat persists one chat line and returns its broadcast payload.
func (s *Service) SendChat(id ws.Identity, body string) (map[string]any, error) {
	if id.Status != "active" {
		return nil, codedError{"status_forbids_social", "account not active"}
	}
	if len(body) == 0 || len(body) > chatBodyMaxLen {
		return nil, codedError{"bad_request", "invalid chat body"}
	}
	ctx := context.Background()
	if err := s.checkMute(ctx, id.UserID); err != nil {
		return nil, err
	}
	row, err := store.New(s.pool).InsertChatMessage(ctx, store.InsertChatMessageParams{
		UserID: id.UserID,
		Kind:   "chat",
		Body:   body,
	})
	if err != nil {
		s.logger.Error("insert chat", "err", err)
		return nil, codedError{"internal", "chat not stored"}
	}
	return ChatPayload(row, id), nil
}

// SendEmote broadcasts an ephemeral reaction (nothing persisted). Mutes
// cover emotes too — spam is spam in any alphabet. Emote ids gated behind an
// active shop pack require ownership; everything else (including ids with no
// art on any client) is free to send.
func (s *Service) SendEmote(id ws.Identity, emoteID string) (map[string]any, error) {
	if id.Status != "active" {
		return nil, codedError{"status_forbids_social", "account not active"}
	}
	ctx := context.Background()
	if err := s.checkMute(ctx, id.UserID); err != nil {
		return nil, err
	}
	entitled, err := s.shop.EmoteEntitled(ctx, id.UserID, emoteID)
	if err != nil {
		s.logger.Error("emote entitlement", "err", err)
		return nil, codedError{"internal", "emote failed"}
	}
	if !entitled {
		return nil, codedError{"emote_locked", "that emote is vault-only"}
	}
	return map[string]any{
		"userId":        id.UserID,
		"displayName":   id.DisplayName,
		"avatarPreset":  id.AvatarPreset,
		"avatarVersion": id.AvatarVersion,
		"title":         id.Title,
		"nameEffect":    id.NameEffect,
		"emoteId":       emoteID,
	}, nil
}

// SendTip moves credits between wallets (sorted row locks, ledger rows, one
// commit) and returns the tip broadcast plus its system chat line.
func (s *Service) SendTip(id ws.Identity, toUserID, credits int64) (map[string]any, map[string]any, error) {
	if id.Status != "active" {
		return nil, nil, codedError{"status_forbids_social", "account not active"}
	}
	if toUserID == id.UserID {
		return nil, nil, codedError{"tip_self", "cannot tip yourself"}
	}
	if credits < 1 || credits > maxTipCredits {
		return nil, nil, codedError{"bad_request", "tip amount out of range"}
	}
	ctx := context.Background()
	if err := s.checkMute(ctx, id.UserID); err != nil {
		return nil, nil, err
	}
	target, err := store.New(s.pool).GetUserPublicProfile(ctx, toUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, codedError{"unknown_user", "no such player"}
		}
		s.logger.Error("tip target lookup", "err", err)
		return nil, nil, codedError{"internal", "tip failed"}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, codedError{"internal", "tip failed"}
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)
	locked, err := q.LockWalletsSorted(ctx, []int64{id.UserID, toUserID})
	if err != nil {
		s.logger.Error("tip lock wallets", "err", err)
		return nil, nil, codedError{"internal", "tip failed"}
	}
	for _, w := range locked {
		if w.UserID == id.UserID && w.BalanceCredits < credits {
			return nil, nil, codedError{"insufficient_credits", "not enough credits"}
		}
	}
	stamp := s.uniqueStamp()
	if _, err := wallet.ApplyTx(ctx, tx, wallet.ApplyRequest{
		UserID: id.UserID, Kind: wallet.KindTip, Amount: -credits,
		IdempotencyKey: fmt.Sprintf("tip:%d:%d:%s", id.UserID, toUserID, stamp),
	}); err != nil {
		return nil, nil, codedError{"insufficient_credits", "not enough credits"}
	}
	if _, err := wallet.ApplyTx(ctx, tx, wallet.ApplyRequest{
		UserID: toUserID, Kind: wallet.KindTipReceived, Amount: credits,
		IdempotencyKey: fmt.Sprintf("tip_in:%d:%d:%s", id.UserID, toUserID, stamp),
	}); err != nil {
		s.logger.Error("tip credit leg", "err", err)
		return nil, nil, codedError{"internal", "tip failed"}
	}
	body := fmt.Sprintf("tipped %s %s credits", target.DisplayName, comma(credits))
	row, err := q.InsertChatMessage(ctx, store.InsertChatMessageParams{
		UserID: id.UserID, Kind: "system", Body: body,
	})
	if err != nil {
		s.logger.Error("tip chat line", "err", err)
		return nil, nil, codedError{"internal", "tip failed"}
	}
	if err := tx.Commit(ctx); err != nil {
		s.logger.Error("tip commit", "err", err)
		return nil, nil, codedError{"internal", "tip failed"}
	}

	tip := map[string]any{
		"fromUserId": id.UserID, "fromName": id.DisplayName,
		"toUserId": toUserID, "toName": target.DisplayName,
		"credits": credits,
	}
	chat := ChatPayload(row, id)
	return tip, chat, nil
}

// Rain splits totalCredits evenly across recipients (never the rainmaker —
// they already paid). The distributed amount is share×recipients, so a
// non-divisible total leaves the remainder with the rainmaker.
func (s *Service) Rain(id ws.Identity, totalCredits int64, recipients []int64) (map[string]any, map[string]any, error) {
	if id.Status != "active" {
		return nil, nil, codedError{"status_forbids_social", "account not active"}
	}
	if len(recipients) == 0 {
		return nil, nil, codedError{"no_recipients", "nobody to rain on"}
	}
	if totalCredits < minRainTotal || totalCredits > maxRainTotal {
		return nil, nil, codedError{"bad_request", "rain amount out of range"}
	}
	ctx := context.Background()
	if err := s.checkMute(ctx, id.UserID); err != nil {
		return nil, nil, err
	}
	share := totalCredits / int64(len(recipients))
	if share < 1 {
		return nil, nil, codedError{"rain_too_small", "not enough for everyone online"}
	}
	distributed := share * int64(len(recipients))

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, codedError{"internal", "rain failed"}
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)
	ids := append([]int64{id.UserID}, recipients...)
	locked, err := q.LockWalletsSorted(ctx, ids)
	if err != nil {
		s.logger.Error("rain lock wallets", "err", err)
		return nil, nil, codedError{"internal", "rain failed"}
	}
	for _, w := range locked {
		if w.UserID == id.UserID && w.BalanceCredits < distributed {
			return nil, nil, codedError{"insufficient_credits", "not enough credits"}
		}
	}
	stamp := s.uniqueStamp()
	if _, err := wallet.ApplyTx(ctx, tx, wallet.ApplyRequest{
		UserID: id.UserID, Kind: wallet.KindRain, Amount: -distributed,
		IdempotencyKey: fmt.Sprintf("rain:%d:%s", id.UserID, stamp),
	}); err != nil {
		return nil, nil, codedError{"insufficient_credits", "not enough credits"}
	}
	for _, uid := range recipients {
		if _, err := wallet.ApplyTx(ctx, tx, wallet.ApplyRequest{
			UserID: uid, Kind: wallet.KindRainShare, Amount: share,
			IdempotencyKey: fmt.Sprintf("rain_share:%d:%d:%s", id.UserID, uid, stamp),
		}); err != nil {
			s.logger.Error("rain share leg", "user", uid, "err", err)
			return nil, nil, codedError{"internal", "rain failed"}
		}
	}
	body := fmt.Sprintf("made it rain %s credits across %d players (%s each)",
		comma(distributed), len(recipients), comma(share))
	row, err := q.InsertChatMessage(ctx, store.InsertChatMessageParams{
		UserID: id.UserID, Kind: "system", Body: body,
	})
	if err != nil {
		s.logger.Error("rain chat line", "err", err)
		return nil, nil, codedError{"internal", "rain failed"}
	}
	if err := tx.Commit(ctx); err != nil {
		s.logger.Error("rain commit", "err", err)
		return nil, nil, codedError{"internal", "rain failed"}
	}

	rain := map[string]any{
		"fromUserId": id.UserID, "fromName": id.DisplayName,
		"totalCredits": distributed, "shareCredits": share,
		"recipientCount": len(recipients),
	}
	chat := ChatPayload(row, id)
	return rain, chat, nil
}

// DeleteChatMessage soft-deletes one line (staff-gated at the socket layer)
// and returns the chat_deleted payload, or nil when there was nothing to
// delete.
func (s *Service) DeleteChatMessage(id ws.Identity, messageID int64) (map[string]any, error) {
	if !id.IsStaff() {
		return nil, codedError{"forbidden", "staff only"}
	}
	n, err := store.New(s.pool).SoftDeleteChatMessage(context.Background(), messageID)
	if err != nil {
		s.logger.Error("delete chat", "err", err)
		return nil, codedError{"internal", "delete failed"}
	}
	if n == 0 {
		return nil, nil
	}
	return map[string]any{"messageId": messageID}, nil
}

// MuteUser upserts a timed mute (staff-gated at the socket layer).
func (s *Service) MuteUser(id ws.Identity, userID int64, minutes int64, reason string) error {
	if !id.IsStaff() {
		return codedError{"forbidden", "staff only"}
	}
	err := store.New(s.pool).UpsertChatMute(context.Background(), store.UpsertChatMuteParams{
		UserID:  userID,
		Column2: int32(minutes),
		Reason:  reason,
		MutedBy: pgtype.Int8{Int64: id.UserID, Valid: true},
	})
	if err != nil {
		s.logger.Error("mute upsert", "err", err)
		return codedError{"internal", "mute failed"}
	}
	return nil
}

// AnnounceWin turns a raw wins-bus event ({userId, gameId, betCredits,
// payoutCredits, multiplier}) into the big_win broadcast and persists its
// system chat line so history replays it.
func (s *Service) AnnounceWin(ev json.RawMessage) (map[string]any, map[string]any, bool) {
	var p struct {
		UserID        int64   `json:"userId"`
		GameID        string  `json:"gameId"`
		BetCredits    int64   `json:"betCredits"`
		PayoutCredits int64   `json:"payoutCredits"`
		Multiplier    float64 `json:"multiplier"`
	}
	if json.Unmarshal(ev, &p) != nil || p.UserID == 0 {
		return nil, nil, false
	}
	ctx := context.Background()
	prof, err := store.New(s.pool).GetUserPublicProfile(ctx, p.UserID)
	if err != nil {
		s.logger.Error("announce win profile", "err", err)
		return nil, nil, false
	}
	gain := p.PayoutCredits - p.BetCredits
	body := fmt.Sprintf("hit %.2f× on %s for +%s credits", p.Multiplier, p.GameID, comma(gain))
	row, err := store.New(s.pool).InsertChatMessage(ctx, store.InsertChatMessageParams{
		UserID: p.UserID, Kind: "system", Body: body,
	})
	if err != nil {
		s.logger.Error("announce win chat line", "err", err)
		return nil, nil, false
	}
	win := map[string]any{
		"userId":        p.UserID,
		"displayName":   prof.DisplayName,
		"avatarPreset":  prof.AvatarPreset.String,
		"avatarVersion": prof.AvatarVersion,
		"title":         prof.Title,
		"nameEffect":    prof.NameEffect,
		"gameId":        p.GameID,
		"betCredits":    p.BetCredits,
		"payoutCredits": p.PayoutCredits,
		"multiplier":    p.Multiplier,
	}
	chat := ChatPayload(row, ws.Identity{
		UserID:        prof.ID,
		DisplayName:   prof.DisplayName,
		AvatarPreset:  prof.AvatarPreset.String,
		AvatarVersion: prof.AvatarVersion,
		Role:          prof.Role,
		Title:         prof.Title,
		NameEffect:    prof.NameEffect,
	})
	return win, chat, true
}
