package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/play"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// playWindow / playMax: 20 plays per 10 seconds per user.
var (
	playWindow = 10 * time.Second
	playMax    = 20
)

// handlePlay runs a server-authoritative bet.
func (s *Server) handlePlay(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	if !su.CanBet() {
		writeError(w, http.StatusForbidden, "status_forbids_betting", "account status does not permit betting")
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

	res, err := s.play.Play(r.Context(), su.UserID, r.PathValue("id"), body.BetCredits, body.ClientSeed, body.IdempotencyKey)
	switch {
	case errors.Is(err, play.ErrUnknownGame):
		writeError(w, http.StatusNotFound, "unknown_game", "no such game")
		return
	case errors.Is(err, play.ErrInvalidBet):
		writeError(w, http.StatusBadRequest, "invalid_bet", err.Error())
		return
	case errors.Is(err, play.ErrIdempotencyKeyInvalid):
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	case errors.Is(err, play.ErrInsufficientFunds):
		writeError(w, http.StatusPaymentRequired, "insufficient_funds", "insufficient credits")
		return
	case errors.Is(err, play.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key reused with a different bet")
		return
	case errors.Is(err, play.ErrStatusForbidsBetting):
		writeError(w, http.StatusForbidden, "status_forbids_betting", "account status does not permit betting")
		return
	case err != nil:
		s.logger.Error("play", "err", err, "user_id", su.UserID, "game", r.PathValue("id"))
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"betId":          res.BetID,
		"gameId":         res.GameID,
		"payoutCredits":  res.PayoutCredits,
		"balanceCredits": res.BalanceCredits,
		"outcome":        json.RawMessage(res.Outcome),
		"fairness": map[string]any{
			"serverSeedHash": res.ServerSeedHash,
			"clientSeed":     res.ClientSeed,
			"nonce":          res.Nonce,
		},
		"replay": res.Replay,
	})
}

// handleListGames exposes the registry for display only.
func (s *Server) handleListGames(w http.ResponseWriter, r *http.Request) {
	games := make([]map[string]any, 0)
	for _, g := range s.registry.List() {
		name := g.ID()
		if d, ok := g.(interface{ DisplayName() string }); ok {
			name = d.DisplayName()
		}
		var paytable any
		if p, ok := g.(interface{ Paytable() any }); ok {
			paytable = p.Paytable()
		}
		games = append(games, map[string]any{
			"id":             g.ID(),
			"name":           name,
			"theoreticalRtp": g.TheoreticalRTP(),
			"paytable":       paytable,
		})
	}
	writeJSON(w, http.StatusOK, games)
}

// handleListBets returns the caller's paginated history. Ownership is
// enforced in the SQL, never by filtering in the handler.
func (s *Server) handleListBets(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}

	var cursor pgtype.Int8
	if c := r.URL.Query().Get("cursor"); c != "" {
		v, err := strconv.ParseInt(c, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid cursor")
			return
		}
		cursor = pgtype.Int8{Int64: v, Valid: true}
	}

	const limit = 50
	rows, err := store.New(s.pool).ListBetsByUser(r.Context(), store.ListBetsByUserParams{
		UserID: su.UserID,
		Cursor: cursor,
		Lim:    limit,
	})
	if err != nil {
		s.logger.Error("list bets", "err", err, "user_id", su.UserID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}

	bets := make([]map[string]any, 0, len(rows))
	var nextCursor *int64
	for i, b := range rows {
		if i == limit-1 {
			nextCursor = &b.ID
		}
		bets = append(bets, map[string]any{
			"id":            b.ID,
			"gameId":        b.GameID,
			"roundId":       b.RoundID,
			"betCredits":    b.BetCredits,
			"payoutCredits": b.PayoutCredits,
			"clientSeed":    b.ClientSeed,
			"nonce":         b.Nonce,
			"outcome":       json.RawMessage(b.Outcome),
			"createdAt":     b.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"bets":       bets,
		"nextCursor": nextCursor,
	})
}
