// Package shop implements the cosmetics store: catalog reads, the purchase
// transaction, equip validation, and the emote-pack entitlement gate.
// Purchases move credits with the same discipline as every other money path
// (wallet row lock, ledger debit, balance materialization, one commit) but
// are render-only: nothing here touches the fair stream or a payout.
package shop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ai-doodoo-slots/services/backend/internal/admin"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/ai-doodoo-slots/services/backend/internal/wallet"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Cosmetic slots: the users column each item kind equips into. emote_pack is
// ownership-only — packs unlock wheel emotes, there is nothing to equip.
var slotByKind = map[string]string{
	"title":         "title",
	"name_effect":   "nameEffect",
	"card_skin":     "cardSkin",
	"avatar_frame":  "avatarFrame",
	"plinko_ball":   "plinkoBall",
	"profile_theme": "profileTheme",
}

var (
	// ErrUnknownItem means the item id is not in the active catalog.
	ErrUnknownItem = errors.New("unknown shop item")
	// ErrAlreadyOwned means the inventory row already exists; no charge.
	ErrAlreadyOwned = errors.New("item already owned")
	// ErrNotOwned means equip was asked for an item not in the inventory.
	ErrNotOwned = errors.New("item not owned")
	// ErrKindMismatch means the item's kind does not fit the equip slot.
	ErrKindMismatch = errors.New("item kind does not fit that slot")
	// ErrNotEquippable means the kind has no equip slot (emote packs).
	ErrNotEquippable = errors.New("item kind is not equippable")
	// ErrIdempotencyKeyInvalid covers missing or oversized keys.
	ErrIdempotencyKeyInvalid = errors.New("idempotency key must be 1-64 characters")
	// ErrStatusForbidsPurchase is returned for banned/self-excluded accounts.
	ErrStatusForbidsPurchase = errors.New("account status does not permit purchases")
	// Re-exported for handler mapping.
	ErrInsufficientFunds   = wallet.ErrInsufficientFunds
	ErrIdempotencyConflict = wallet.ErrIdempotencyConflict
)

// Service runs catalog reads and the purchase transaction.
type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// PurchaseResult is what the purchase endpoint returns.
type PurchaseResult struct {
	ItemID       string
	Balance      int64
	Replay       bool
	AlreadyOwned bool
}

// Purchase debits the wallet and grants the item atomically:
//
//	lock wallet (FOR UPDATE) → status gate → catalog → ownership check →
//	ledger debit (idempotency-checked) → inventory grant → commit.
//
// A replayed idempotency key returns the original result; a lost key that
// hits the already-owned inventory row returns without a second charge.
func (s *Service) Purchase(ctx context.Context, userID int64, itemID, idempotencyKey string) (PurchaseResult, error) {
	if len(idempotencyKey) == 0 || len(idempotencyKey) > 64 {
		return PurchaseResult{}, ErrIdempotencyKeyInvalid
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PurchaseResult{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)

	// Serialize on the wallet row first, mirroring the play path's lock order.
	lockRow, err := wallet.LockWallet(ctx, tx, userID)
	if err != nil {
		return PurchaseResult{}, err
	}

	status, err := q.GetUserStatus(ctx, userID)
	if err != nil {
		return PurchaseResult{}, fmt.Errorf("load status: %w", err)
	}
	if admin.StatusForbidsBetting(status) {
		return PurchaseResult{}, ErrStatusForbidsPurchase
	}

	item, err := q.GetActiveShopItem(ctx, itemID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PurchaseResult{}, ErrUnknownItem
		}
		return PurchaseResult{}, fmt.Errorf("load item: %w", err)
	}

	// Already owned (including a retry whose idempotency key was lost):
	// grant nothing, charge nothing.
	owned, err := q.HasInventoryItem(ctx, store.HasInventoryItemParams{UserID: userID, ItemID: item.ID})
	if err != nil {
		return PurchaseResult{}, fmt.Errorf("ownership check: %w", err)
	}
	if owned {
		return PurchaseResult{ItemID: item.ID, Balance: lockRow.BalanceCredits, AlreadyOwned: true}, nil
	}

	res, err := wallet.ApplyTx(ctx, tx, wallet.ApplyRequest{
		UserID:         userID,
		Kind:           wallet.KindShopBuy,
		Amount:         -item.PriceCredits,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return PurchaseResult{}, err
	}
	if _, err := q.InsertInventoryItem(ctx, store.InsertInventoryItemParams{UserID: userID, ItemID: item.ID}); err != nil {
		return PurchaseResult{}, fmt.Errorf("grant item: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PurchaseResult{}, fmt.Errorf("commit: %w", err)
	}
	return PurchaseResult{ItemID: item.ID, Balance: res.Balance, Replay: res.Replayed}, nil
}

// EquipRequest carries one equip slot per field; nil leaves the slot
// untouched, "" clears it. Field names mirror the JSON body.
type EquipRequest struct {
	Title        *string
	NameEffect   *string
	CardSkin     *string
	AvatarFrame  *string
	PlinkoBall   *string
	ProfileTheme *string
}

// validateEquipSlot checks one requested value against the catalog: the item
// must be active, equippable, the right kind, and owned. "" (clear) passes.
func (s *Service) validateEquipSlot(ctx context.Context, userID int64, kind, value string) error {
	if value == "" {
		return nil
	}
	item, err := store.New(s.pool).GetActiveShopItem(ctx, value)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnknownItem
		}
		return fmt.Errorf("load item: %w", err)
	}
	if slotByKind[item.Kind] != kind {
		return ErrKindMismatch
	}
	owned, err := store.New(s.pool).HasInventoryItem(ctx, store.HasInventoryItemParams{UserID: userID, ItemID: item.ID})
	if err != nil {
		return fmt.Errorf("ownership check: %w", err)
	}
	if !owned {
		return ErrNotOwned
	}
	return nil
}

// Equip validates every requested slot (active catalog, kind match,
// ownership) and persists the changes. Validation happens before any write,
// so the users row only ever names owned cosmetics.
func (s *Service) Equip(ctx context.Context, userID int64, req EquipRequest) error {
	type slotUpdate struct {
		column string
		value  string
	}
	updates := make([]slotUpdate, 0, 6)
	if req.Title != nil {
		updates = append(updates, slotUpdate{"title", *req.Title})
	}
	if req.NameEffect != nil {
		updates = append(updates, slotUpdate{"nameEffect", *req.NameEffect})
	}
	if req.CardSkin != nil {
		updates = append(updates, slotUpdate{"cardSkin", *req.CardSkin})
	}
	if req.AvatarFrame != nil {
		updates = append(updates, slotUpdate{"avatarFrame", *req.AvatarFrame})
	}
	if req.PlinkoBall != nil {
		updates = append(updates, slotUpdate{"plinkoBall", *req.PlinkoBall})
	}
	if req.ProfileTheme != nil {
		updates = append(updates, slotUpdate{"profileTheme", *req.ProfileTheme})
	}
	if len(updates) == 0 {
		return nil
	}

	for _, u := range updates {
		if err := s.validateEquipSlot(ctx, userID, u.column, u.value); err != nil {
			return fmt.Errorf("%s: %w", u.column, err)
		}
	}

	q := store.New(s.pool)
	if req.Title != nil {
		if _, err := q.SetTitle(ctx, store.SetTitleParams{ID: userID, Title: *req.Title}); err != nil {
			return err
		}
	}
	if req.NameEffect != nil {
		if _, err := q.SetNameEffect(ctx, store.SetNameEffectParams{ID: userID, NameEffect: *req.NameEffect}); err != nil {
			return err
		}
	}
	if req.CardSkin != nil {
		if _, err := q.SetCardSkin(ctx, store.SetCardSkinParams{ID: userID, CardSkin: *req.CardSkin}); err != nil {
			return err
		}
	}
	if req.AvatarFrame != nil {
		if _, err := q.SetAvatarFrame(ctx, store.SetAvatarFrameParams{ID: userID, AvatarFrame: *req.AvatarFrame}); err != nil {
			return err
		}
	}
	if req.PlinkoBall != nil {
		if _, err := q.SetPlinkoBall(ctx, store.SetPlinkoBallParams{ID: userID, PlinkoBall: *req.PlinkoBall}); err != nil {
			return err
		}
	}
	if req.ProfileTheme != nil {
		if _, err := q.SetProfileTheme(ctx, store.SetProfileThemeParams{ID: userID, ProfileTheme: *req.ProfileTheme}); err != nil {
			return err
		}
	}
	return nil
}

// Inventory lists the caller's owned items.
func (s *Service) Inventory(ctx context.Context, userID int64) ([]store.ListInventoryRow, error) {
	return store.New(s.pool).ListInventory(ctx, userID)
}

// EmotePackEntry maps one active pack item to the emote ids it unlocks.
type EmotePackEntry struct {
	ItemID string
	Emotes []string
}

// EmotePacks lists the pack-gated emote ids. The emote art registry lives on
// the client; the server only needs to know which ids cost money.
func (s *Service) EmotePacks(ctx context.Context) ([]EmotePackEntry, error) {
	rows, err := store.New(s.pool).ListActiveEmotePacks(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]EmotePackEntry, 0, len(rows))
	for _, row := range rows {
		var payload struct {
			Emotes []string `json:"emotes"`
		}
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			continue
		}
		out = append(out, EmotePackEntry{ItemID: row.ID, Emotes: payload.Emotes})
	}
	return out, nil
}

// EmoteEntitled reports whether userID may send emoteID: anything not gated
// behind an active pack is free.
func (s *Service) EmoteEntitled(ctx context.Context, userID int64, emoteID string) (bool, error) {
	packs, err := s.EmotePacks(ctx)
	if err != nil {
		return false, err
	}
	gatingPack := ""
	for _, p := range packs {
		for _, e := range p.Emotes {
			if e == emoteID {
				gatingPack = p.ItemID
				break
			}
		}
		if gatingPack != "" {
			break
		}
	}
	if gatingPack == "" {
		return true, nil
	}
	owned, err := store.New(s.pool).HasInventoryItem(ctx, store.HasInventoryItemParams{UserID: userID, ItemID: gatingPack})
	if err != nil {
		return false, err
	}
	return owned, nil
}
