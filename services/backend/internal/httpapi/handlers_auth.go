package httpapi

import (
	"errors"
	"net/http"

	"github.com/ai-doodoo-slots/services/backend/internal/auth"
	"github.com/ai-doodoo-slots/services/backend/internal/wallet"
)

// SignupBonusCredits is the starting balance for new guests.
const SignupBonusCredits int64 = 1000

// currentUser resolves the session behind the cookie. Returns nil when there
// is no active session; handlers decide whether that is an error.
func (s *Server) currentUser(r *http.Request) *auth.SessionUser {
	token, ok := auth.TokenFromRequest(r)
	if !ok {
		return nil
	}
	su, err := s.auth.SessionFromToken(r.Context(), token)
	if err != nil {
		return nil
	}
	return su
}

// handleAuthGuest creates a guest with 1000 credits.
func (s *Server) handleAuthGuest(w http.ResponseWriter, r *http.Request) {
	if s.currentUser(r) != nil {
		writeError(w, http.StatusConflict, "already_authenticated", "a session already exists")
		return
	}

	ip, ua := r.RemoteAddr, r.UserAgent()
	user, token, expires, err := s.auth.CreateGuest(r.Context(), ip, ua)
	if err != nil {
		s.logger.Error("create guest", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	if err := s.wallet.EnsureSignup(r.Context(), user.ID, SignupBonusCredits); err != nil {
		s.logger.Error("provision wallet", "err", err, "user_id", user.ID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	// Issue the fairness seed pair at signup; the player sees only the hash.
	if _, err := s.fair.EnsureForUser(r.Context(), user.ID); err != nil {
		s.logger.Error("provision seed", "err", err, "user_id", user.ID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}

	auth.SetCookie(w, token, expires, s.cookieSecure)
	su := &auth.SessionUser{
		UserID:      user.ID,
		IsGuest:     user.IsGuest,
		DisplayName: user.DisplayName,
		Role:        user.Role,
		Status:      user.Status,
		CreatedAt:   user.CreatedAt,
	}
	balance, err := s.wallet.Balance(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("read balance", "err", err, "user_id", user.ID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, meDTO{User: toUserDTO(su), BalanceCredits: balance})
}

// handleLogout revokes the session and clears its cookie.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token, ok := auth.TokenFromRequest(r)
	if ok {
		if err := s.auth.Logout(r.Context(), token); err != nil {
			s.logger.Error("logout", "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
	}
	auth.ClearCookie(w, s.cookieSecure)
	w.WriteHeader(http.StatusNoContent)
}

// handleMe returns the current user, balance, and role.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	balance, err := s.wallet.Balance(r.Context(), su.UserID)
	if err != nil {
		if errors.Is(err, wallet.ErrWalletNotFound) {
			writeError(w, http.StatusInternalServerError, "internal", "wallet missing")
			return
		}
		s.logger.Error("read balance", "err", err, "user_id", su.UserID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, meDTO{User: toUserDTO(su), BalanceCredits: balance})
}
