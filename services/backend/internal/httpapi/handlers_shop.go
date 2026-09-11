// Shop handlers: catalog, inventory, purchase, and cosmetics equip. The
// purchase is a money path — the service runs it as one ledger transaction —
// so it is rate-limited and idempotency-keyed like a bet. Equipping is free
// but ownership-checked, and republishes the profile event so every connected
// client re-renders the player's cosmetics live.
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/auth"
	"github.com/ai-doodoo-slots/services/backend/internal/shop"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
)

// shopWindow / shopMax: 30 purchases per 10 seconds per user.
var (
	shopWindow = 10 * time.Second
	shopMax    = 30
)

// handleShopItems lists the active catalog.
func (s *Server) handleShopItems(w http.ResponseWriter, r *http.Request) {
	if s.currentUser(r) == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	rows, err := store.New(s.pool).ListActiveShopItems(r.Context())
	if err != nil {
		s.logger.Error("shop catalog", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, it := range rows {
		items = append(items, map[string]any{
			"id":           it.ID,
			"kind":         it.Kind,
			"name":         it.Name,
			"blurb":        it.Blurb,
			"priceCredits": it.PriceCredits,
			"rarity":       it.Rarity,
			"payload":      json.RawMessage(it.Payload),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleShopInventory lists the caller's owned items.
func (s *Server) handleShopInventory(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	rows, err := s.shop.Inventory(r.Context(), su.UserID)
	if err != nil {
		s.logger.Error("shop inventory", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{
			"itemId":     row.ItemID,
			"acquiredAt": row.AcquiredAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleShopPurchase debits credits and grants the item in one transaction.
func (s *Server) handleShopPurchase(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	if !su.CanBet() {
		writeError(w, http.StatusForbidden, "status_forbids_purchase", "account status does not permit purchases")
		return
	}
	if !s.shopLimiter.allowUserID(su.UserID) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many purchases, slow down")
		return
	}
	var body struct {
		ItemId         string `json:"itemId"`
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil || body.ItemId == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "itemId and idempotencyKey are required")
		return
	}

	res, err := s.shop.Purchase(r.Context(), su.UserID, body.ItemId, body.IdempotencyKey)
	switch {
	case errors.Is(err, shop.ErrUnknownItem):
		writeError(w, http.StatusNotFound, "unknown_item", "no such shop item")
		return
	case errors.Is(err, shop.ErrIdempotencyKeyInvalid):
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	case errors.Is(err, shop.ErrInsufficientFunds):
		writeError(w, http.StatusPaymentRequired, "insufficient_funds", "not enough credits")
		return
	case errors.Is(err, shop.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key reused with a different purchase")
		return
	case errors.Is(err, shop.ErrStatusForbidsPurchase):
		writeError(w, http.StatusForbidden, "status_forbids_purchase", "account status does not permit purchases")
		return
	case err != nil:
		s.logger.Error("shop purchase", "err", err, "user_id", su.UserID, "item", body.ItemId)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"itemId":         res.ItemID,
		"balanceCredits": res.Balance,
		"replay":         res.Replay,
		"alreadyOwned":   res.AlreadyOwned,
	})
}

// handleUpdateCosmetics equips or clears cosmetic slots (free, ownership
// checked), then republishes the profile event so every surface — chat names,
// roster, poker seats — reflects the change without a reconnect.
func (s *Server) handleUpdateCosmetics(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	var body struct {
		Title        *string `json:"title"`
		NameEffect   *string `json:"nameEffect"`
		CardSkin     *string `json:"cardSkin"`
		AvatarFrame  *string `json:"avatarFrame"`
		PlinkoBall   *string `json:"plinkoBall"`
		ProfileTheme *string `json:"profileTheme"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if body.Title == nil && body.NameEffect == nil && body.CardSkin == nil &&
		body.AvatarFrame == nil && body.PlinkoBall == nil && body.ProfileTheme == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "nothing to update")
		return
	}

	req := shop.EquipRequest{
		Title:        body.Title,
		NameEffect:   body.NameEffect,
		CardSkin:     body.CardSkin,
		AvatarFrame:  body.AvatarFrame,
		PlinkoBall:   body.PlinkoBall,
		ProfileTheme: body.ProfileTheme,
	}
	if err := s.shop.Equip(r.Context(), su.UserID, req); err != nil {
		switch {
		case errors.Is(err, shop.ErrUnknownItem):
			writeError(w, http.StatusNotFound, "unknown_item", "no such shop item")
		case errors.Is(err, shop.ErrNotOwned):
			writeError(w, http.StatusForbidden, "not_owned", "buy the item in the vault first")
		case errors.Is(err, shop.ErrKindMismatch), errors.Is(err, shop.ErrNotEquippable):
			writeError(w, http.StatusBadRequest, "kind_mismatch", "item does not fit that slot")
		default:
			s.logger.Error("cosmetics equip", "err", err, "user_id", su.UserID)
			writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		}
		return
	}

	// Re-read so the response and event carry the full new loadout.
	user, err := store.New(s.pool).GetUserByID(r.Context(), su.UserID)
	if err != nil {
		s.logger.Error("cosmetics reread", "err", err, "user_id", su.UserID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	s.auditProfile(r.Context(), su.UserID, "profile.cosmetics", map[string]any{
		"title": user.Title, "nameEffect": user.NameEffect, "cardSkin": user.CardSkin,
		"avatarFrame": user.AvatarFrame, "plinkoBall": user.PlinkoBall, "profileTheme": user.ProfileTheme,
	})
	s.publishProfileEvent(su.UserID, user.DisplayName, user.AvatarPreset.String, user.AvatarVersion,
		user.Title, user.NameEffect, user.CardSkin)

	// Same re-read → SessionUser path as the profile handlers, so the
	// response carries every applied cosmetic in one authoritative payload.
	su2 := auth.SessionUserFromStore(&user)
	su2.SessionID = su.SessionID
	su2.Subject = su.Subject
	s.writeMe(w, r, su2)
}
