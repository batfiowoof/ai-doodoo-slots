package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/ai-doodoo-slots/services/backend/internal/bigwin"
	"github.com/ai-doodoo-slots/services/backend/internal/chicken"
)

// handleChickenStart opens a chicken run: debit, draw the fatal lane from
// the personal stream, persist. Same rate-limit budget as the play path.
func (s *Server) handleChickenStart(w http.ResponseWriter, r *http.Request) {
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
		Difficulty     string `json:"difficulty"`
		ClientSeed     string `json:"clientSeed"`
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	res, err := s.chicken.Start(r.Context(), su.UserID, body.BetCredits, body.Difficulty, body.ClientSeed, body.IdempotencyKey)
	s.writeChickenResult(w, r, err, res)
}

// handleChickenHop advances the chicken one lane on the active round.
func (s *Server) handleChickenHop(w http.ResponseWriter, r *http.Request) {
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

	roundID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || roundID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid round id")
		return
	}
	var body struct {
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	res, err := s.chicken.Hop(r.Context(), su.UserID, roundID, body.IdempotencyKey)
	s.writeChickenResult(w, r, err, res)
}

// handleChickenCashOut settles the active round at the current multiplier.
func (s *Server) handleChickenCashOut(w http.ResponseWriter, r *http.Request) {
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

	roundID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || roundID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid round id")
		return
	}
	var body struct {
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	res, err := s.chicken.CashOut(r.Context(), su.UserID, roundID, body.IdempotencyKey)
	if err == nil && !res.Replay && res.View.PayoutCredits > 0 && res.View.BetCredits > 0 {
		bigwin.Notify(r.Context(), s.pool, su.UserID, "chicken", res.View.BetCredits,
			res.View.PayoutCredits, res.View.Multiplier)
	}
	s.writeChickenResult(w, r, err, res)
}

// handleActiveChickenRound returns the caller's in-progress round, if any.
func (s *Server) handleActiveChickenRound(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	view, ok, err := s.chicken.ActiveRound(r.Context(), su.UserID)
	if err != nil {
		s.logger.Error("active chicken round", "err", err, "user_id", su.UserID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"round": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"round": view})
}

func (s *Server) writeChickenResult(w http.ResponseWriter, r *http.Request, err error, res chicken.StartResult) {
	switch {
	case err == nil:
	case errors.Is(err, chicken.ErrInvalidBet):
		writeError(w, http.StatusBadRequest, "invalid_bet", err.Error())
		return
	case errors.Is(err, chicken.ErrDifficultyInvalid):
		writeError(w, http.StatusBadRequest, "invalid_params", err.Error())
		return
	case errors.Is(err, chicken.ErrIdempotencyKeyInvalid):
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	case errors.Is(err, chicken.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key reused with a different bet")
		return
	case errors.Is(err, chicken.ErrInsufficientFunds):
		writeError(w, http.StatusPaymentRequired, "insufficient_funds", "insufficient credits")
		return
	case errors.Is(err, chicken.ErrStatusForbidsBetting):
		writeError(w, http.StatusForbidden, "status_forbids_betting", "account status does not permit betting")
		return
	case errors.Is(err, chicken.ErrRoundActive):
		writeError(w, http.StatusConflict, "round_active", "a chicken run is already in progress")
		return
	case errors.Is(err, chicken.ErrRoundNotFound):
		writeError(w, http.StatusNotFound, "round_not_found", "no such chicken run")
		return
	case errors.Is(err, chicken.ErrRoundComplete):
		writeError(w, http.StatusConflict, "round_complete", "chicken run is already complete")
		return
	case errors.Is(err, chicken.ErrCannotCash):
		writeError(w, http.StatusBadRequest, "cannot_cash_out", err.Error())
		return
	default:
		s.logger.Error("chicken", "err", err, "user_id", r.URL.Path)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"round":          res.View,
		"balanceCredits": res.BalanceCredits,
		"fairness": map[string]any{
			"serverSeedHash": res.ServerSeedHash,
			"clientSeed":     res.ClientSeed,
			"nonce":          res.Nonce,
		},
		"replay": res.Replay,
	})
}
