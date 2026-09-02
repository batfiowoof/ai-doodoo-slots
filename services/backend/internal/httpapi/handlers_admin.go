package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/admin"
	"github.com/ai-doodoo-slots/services/backend/internal/auth"
	"github.com/ai-doodoo-slots/services/backend/internal/wallet"
)

// requireRole resolves the session and enforces RBAC. Ownership and status
// checks live in the SQL and the bet path respectively.
func (s *Server) requireRole(w http.ResponseWriter, r *http.Request, required string) *auth.SessionUser {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return nil
	}
	if !admin.RoleAtLeast(su.Role, required) {
		writeError(w, http.StatusForbidden, "forbidden", "insufficient privileges")
		return nil
	}
	return su
}

// handleBan bans or unbans a user (moderator+).
func (s *Server) handleBan(w http.ResponseWriter, r *http.Request) {
	su := s.requireRole(w, r, admin.RoleModerator)
	if su == nil {
		return
	}
	targetID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid user id")
		return
	}
	var body struct {
		Banned bool `json:"banned"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	status := admin.StatusActive
	var until *time.Time
	if body.Banned {
		status = admin.StatusBanned
	}
	if err := s.admin.SetStatus(r.Context(), su.UserID, targetID, status, until); err != nil {
		s.logger.Error("ban", "err", err, "target", targetID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	// Status changes take effect immediately: open sockets of the banned
	// account go down.
	s.publishStatusEvent(targetID, status)
	w.WriteHeader(http.StatusNoContent)
}

// handleAdminAdjust adjusts a balance (admin only) via the ledger.
func (s *Server) handleAdminAdjust(w http.ResponseWriter, r *http.Request) {
	su := s.requireRole(w, r, admin.RoleAdmin)
	if su == nil {
		return
	}
	targetID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid user id")
		return
	}
	var body struct {
		AmountCredits int64  `json:"amountCredits"`
		Reason        string `json:"reason"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if body.AmountCredits == 0 || body.Reason == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "amountCredits (non-zero) and reason are required")
		return
	}

	balance, err := s.admin.AdjustBalance(r.Context(), su.UserID, targetID, body.AmountCredits, body.Reason)
	switch {
	case errors.Is(err, wallet.ErrWalletNotFound), errors.Is(err, wallet.ErrInsufficientFunds):
		writeError(w, http.StatusBadRequest, "bad_request", "adjustment rejected")
		return
	case err != nil:
		s.logger.Error("admin adjust", "err", err, "target", targetID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"balanceCredits": balance})
}

// handleAdminAudit lists the audit log (admin only).
func (s *Server) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	su := s.requireRole(w, r, admin.RoleAdmin)
	if su == nil {
		return
	}
	var cursor *int64
	if cv := r.URL.Query().Get("cursor"); cv != "" {
		v, err := strconv.ParseInt(cv, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid cursor")
			return
		}
		cursor = &v
	}
	rows, err := s.admin.ListAudit(r.Context(), cursor, 50)
	if err != nil {
		s.logger.Error("audit list", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	type entryDTO struct {
		ID          int64           `json:"id"`
		ActorUserID *int64          `json:"actorUserId"`
		Action      string          `json:"action"`
		TargetType  *string         `json:"targetType"`
		TargetID    *int64          `json:"targetId"`
		Metadata    json.RawMessage `json:"metadata"`
		CreatedAt   time.Time       `json:"createdAt"`
	}
	entries := make([]entryDTO, 0, len(rows))
	var nextCursor *int64
	for i, e := range rows {
		if i == 49 {
			nextCursor = &e.ID
		}
		dto := entryDTO{ID: e.ID, Action: e.Action, Metadata: json.RawMessage(e.Metadata), CreatedAt: e.CreatedAt}
		if e.ActorUserID.Valid {
			dto.ActorUserID = &e.ActorUserID.Int64
		}
		if e.TargetType.Valid {
			dto.TargetType = &e.TargetType.String
		}
		if e.TargetID.Valid {
			dto.TargetID = &e.TargetID.Int64
		}
		entries = append(entries, dto)
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "nextCursor": nextCursor})
}

// handleSelfExclude is a user-facing setting: a player can exclude
// themselves from betting.
func (s *Server) handleSelfExclude(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	var body struct {
		Days *int `json:"days"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	var until *time.Time
	if body.Days != nil && *body.Days > 0 {
		t := s.clock.Now().AddDate(0, 0, *body.Days)
		until = &t
	}
	if err := s.admin.SetStatus(r.Context(), su.UserID, su.UserID, admin.StatusSelfExcluded, until); err != nil {
		s.logger.Error("self exclude", "err", err, "user_id", su.UserID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	s.publishStatusEvent(su.UserID, admin.StatusSelfExcluded)
	writeJSON(w, http.StatusOK, map[string]any{"status": admin.StatusSelfExcluded, "statusUntil": until})
}
