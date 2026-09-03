package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/ai-doodoo-slots/services/backend/internal/hand"
)

// handleBlackjackDeal opens a hand: debit, shuffle from the personal stream,
// persist. Same rate-limit budget as the instant play path.
func (s *Server) handleBlackjackDeal(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	if !su.CanBet() {
		writeError(w, http.StatusForbidden, "status_forbids_betting", "account status does not permit betting")
		return
	}
	if !s.playLimiter.allowUserID(su.UserID) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many plays, slow down")
		return
	}

	var body struct {
		BetCredits     int64  `json:"betCredits"`
		ClientSeed     string `json:"clientSeed"`
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	res, err := s.hand.Deal(r.Context(), su.UserID, body.BetCredits, body.ClientSeed, body.IdempotencyKey)
	s.writeHandResult(w, r, err, res)
}

// handleHandAction applies hit/stand/double to the active hand.
func (s *Server) handleHandAction(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	if !su.CanBet() {
		writeError(w, http.StatusForbidden, "status_forbids_betting", "account status does not permit betting")
		return
	}
	if !s.playLimiter.allowUserID(su.UserID) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many actions, slow down")
		return
	}

	handID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || handID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid hand id")
		return
	}
	var body struct {
		Action         string `json:"action"`
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	res, err := s.hand.Action(r.Context(), su.UserID, handID, body.Action, body.IdempotencyKey)
	s.writeHandResult(w, r, err, res)
}

// handleActiveHand returns the caller's in-progress hand, if any.
func (s *Server) handleActiveHand(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	view, ok, err := s.hand.ActiveHand(r.Context(), su.UserID)
	if err != nil {
		s.logger.Error("active hand", "err", err, "user_id", su.UserID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"hand": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hand": view})
}

func (s *Server) writeHandResult(w http.ResponseWriter, r *http.Request, err error, res hand.DealResult) {
	switch {
	case err == nil:
	case errors.Is(err, hand.ErrInvalidBet):
		writeError(w, http.StatusBadRequest, "invalid_bet", err.Error())
		return
	case errors.Is(err, hand.ErrIdempotencyKeyInvalid):
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	case errors.Is(err, hand.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key reused with a different bet")
		return
	case errors.Is(err, hand.ErrInsufficientFunds):
		writeError(w, http.StatusPaymentRequired, "insufficient_funds", "insufficient credits")
		return
	case errors.Is(err, hand.ErrStatusForbidsBetting):
		writeError(w, http.StatusForbidden, "status_forbids_betting", "account status does not permit betting")
		return
	case errors.Is(err, hand.ErrHandActive):
		writeError(w, http.StatusConflict, "hand_active", "a hand is already in progress")
		return
	case errors.Is(err, hand.ErrHandNotFound):
		writeError(w, http.StatusNotFound, "hand_not_found", "no such hand")
		return
	case errors.Is(err, hand.ErrHandComplete):
		writeError(w, http.StatusConflict, "hand_complete", "hand is already complete")
		return
	case errors.Is(err, hand.ErrInvalidAction):
		writeError(w, http.StatusBadRequest, "invalid_action", "action not allowed at this point")
		return
	case errors.Is(err, hand.ErrInvalidActionName):
		writeError(w, http.StatusBadRequest, "invalid_action", err.Error())
		return
	default:
		s.logger.Error("hand", "err", err, "user_id", r.PathValue("id"))
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"hand":           res.View,
		"balanceCredits": res.BalanceCredits,
		"fairness": map[string]any{
			"serverSeedHash": res.ServerSeedHash,
			"clientSeed":     res.ClientSeed,
			"nonce":          res.Nonce,
		},
		"replay": res.Replay,
	})
}
