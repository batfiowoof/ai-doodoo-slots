package httpapi

import (
	"errors"
	"net/http"

	"github.com/ai-doodoo-slots/services/backend/internal/wallet"
)

// DepositCredits is the fixed top-up granted by the deposit button. The
// server owns the amount; the client cannot choose it.
const DepositCredits int64 = 1000

// handleDeposit tops up the caller's balance. Idempotent per user per UTC
// hour: the idempotency key embeds the hour bucket, so a replay within the
// hour returns the same balance without a second credit.
func (s *Server) handleDeposit(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}

	bucket := s.clock.Now().UTC().Unix() / 3600
	res, err := s.wallet.Apply(r.Context(), wallet.ApplyRequest{
		UserID:         su.UserID,
		Kind:           wallet.KindDailyTopup,
		Amount:         DepositCredits,
		IdempotencyKey: wallet.TopupKey(su.UserID, bucket),
	})
	if errors.Is(err, wallet.ErrWalletNotFound) {
		writeError(w, http.StatusInternalServerError, "internal", "wallet missing")
		return
	}
	if err != nil {
		s.logger.Error("deposit", "err", err, "user_id", su.UserID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"balanceCredits": res.Balance,
		"claimed":        !res.Replayed,
		"amountCredits":  DepositCredits,
	})
}
