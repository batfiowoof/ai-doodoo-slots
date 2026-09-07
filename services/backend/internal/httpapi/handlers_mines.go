package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/ai-doodoo-slots/services/backend/internal/bigwin"
	"github.com/ai-doodoo-slots/services/backend/internal/mines"
)

// handleMinesStart opens a mines round: debit, draw the mine layout from
// the personal stream, persist. Same rate-limit budget as the play path.
func (s *Server) handleMinesStart(w http.ResponseWriter, r *http.Request) {
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
		MineCount      int    `json:"mineCount"`
		ClientSeed     string `json:"clientSeed"`
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	res, err := s.mines.Start(r.Context(), su.UserID, body.BetCredits, body.MineCount, body.ClientSeed, body.IdempotencyKey)
	s.writeMinesResult(w, r, err, res)
}

// handleMinesReveal uncovers one tile on the active round.
func (s *Server) handleMinesReveal(w http.ResponseWriter, r *http.Request) {
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
		Tile           int    `json:"tile"`
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	res, err := s.mines.Reveal(r.Context(), su.UserID, roundID, body.Tile, body.IdempotencyKey)
	s.writeMinesResult(w, r, err, res)
}

// handleMinesCashOut settles the active round at the current multiplier.
func (s *Server) handleMinesCashOut(w http.ResponseWriter, r *http.Request) {
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

	res, err := s.mines.CashOut(r.Context(), su.UserID, roundID, body.IdempotencyKey)
	if err == nil && !res.Replay && res.View.PayoutCredits > 0 && res.View.BetCredits > 0 {
		bigwin.Notify(r.Context(), s.pool, su.UserID, "mines", res.View.BetCredits,
			res.View.PayoutCredits, res.View.Multiplier)
	}
	s.writeMinesResult(w, r, err, res)
}

// handleActiveMinesRound returns the caller's in-progress round, if any.
func (s *Server) handleActiveMinesRound(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	view, ok, err := s.mines.ActiveRound(r.Context(), su.UserID)
	if err != nil {
		s.logger.Error("active mines round", "err", err, "user_id", su.UserID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"round": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"round": view})
}

func (s *Server) writeMinesResult(w http.ResponseWriter, r *http.Request, err error, res mines.StartResult) {
	switch {
	case err == nil:
	case errors.Is(err, mines.ErrInvalidBet):
		writeError(w, http.StatusBadRequest, "invalid_bet", err.Error())
		return
	case errors.Is(err, mines.ErrMineCountInvalid):
		writeError(w, http.StatusBadRequest, "invalid_params", err.Error())
		return
	case errors.Is(err, mines.ErrIdempotencyKeyInvalid):
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	case errors.Is(err, mines.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key reused with a different bet")
		return
	case errors.Is(err, mines.ErrInsufficientFunds):
		writeError(w, http.StatusPaymentRequired, "insufficient_funds", "insufficient credits")
		return
	case errors.Is(err, mines.ErrStatusForbidsBetting):
		writeError(w, http.StatusForbidden, "status_forbids_betting", "account status does not permit betting")
		return
	case errors.Is(err, mines.ErrRoundActive):
		writeError(w, http.StatusConflict, "round_active", "a mines round is already in progress")
		return
	case errors.Is(err, mines.ErrRoundNotFound):
		writeError(w, http.StatusNotFound, "round_not_found", "no such mines round")
		return
	case errors.Is(err, mines.ErrRoundComplete):
		writeError(w, http.StatusConflict, "round_complete", "mines round is already complete")
		return
	case errors.Is(err, mines.ErrInvalidTile):
		writeError(w, http.StatusBadRequest, "invalid_tile", err.Error())
		return
	case errors.Is(err, mines.ErrCannotCash):
		writeError(w, http.StatusBadRequest, "cannot_cash_out", err.Error())
		return
	default:
		s.logger.Error("mines", "err", err, "user_id", r.URL.Path)
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
