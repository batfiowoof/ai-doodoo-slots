package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/auth"
	"github.com/ai-doodoo-slots/services/backend/internal/wallet"
)

// meFromUser builds the Me payload for a freshly registered/logged-in user.
func (s *Server) meFromUser(w http.ResponseWriter, r *http.Request, u auth.SessionUser) {
	balance, err := s.wallet.Balance(r.Context(), u.UserID)
	if errors.Is(err, wallet.ErrWalletNotFound) {
		balance = 0
	} else if err != nil {
		s.logger.Error("read balance", "err", err, "user_id", u.UserID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, meDTO{User: toUserDTO(&u), BalanceCredits: balance})
}

// handleRegister upgrades a guest in place, or creates a fresh account when
// called without a session.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !s.authLimiter.allow(r.RemoteAddr) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts")
		return
	}

	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	su := s.currentUser(r)
	currentToken, _ := auth.TokenFromRequest(r)
	ip, ua := r.RemoteAddr, r.UserAgent()

	reg, err := s.accounts.Register(r.Context(), su, currentToken, body.Email, body.Password, ip, ua)
	switch {
	case errors.Is(err, auth.ErrEmailTaken):
		writeError(w, http.StatusConflict, "email_taken", "email already registered")
		return
	case errors.Is(err, auth.ErrNotGuest):
		writeError(w, http.StatusConflict, "already_registered", "this session already has an account")
		return
	case errors.Is(err, auth.ErrBadEmail), errors.Is(err, auth.ErrWeakPassword):
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	case err != nil:
		s.logger.Error("register", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}

	// Fresh accounts get the wallet and seed pair; upgraded guests keep
	// theirs, so provisioning is idempotent and only runs on the fresh path.
	if su == nil {
		if err := s.wallet.EnsureSignup(r.Context(), reg.User.ID, SignupBonusCredits); err != nil {
			s.logger.Error("provision wallet", "err", err, "user_id", reg.User.ID)
			writeError(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
		if _, err := s.fair.EnsureForUser(r.Context(), reg.User.ID); err != nil {
			s.logger.Error("provision seed", "err", err, "user_id", reg.User.ID)
			writeError(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
	}

	// Dev "email" delivery: verification tokens are logged, not sent.
	if reg.VerifyToken != "" {
		s.logger.Info("verification email (dev delivery)",
			"user_id", reg.User.ID, "verify_token", reg.VerifyToken)
	}

	auth.SetCookie(w, reg.Token, reg.Expires, s.cookieSecure)
	newSu := &auth.SessionUser{
		UserID:      reg.User.ID,
		IsGuest:     reg.User.IsGuest,
		DisplayName: reg.User.DisplayName,
		Email:       reg.User.Email,
		Role:        reg.User.Role,
		Status:      reg.User.Status,
		CreatedAt:   reg.User.CreatedAt,
	}
	s.meFromUser(w, r, *newSu)
}

// handleLogin authenticates with generic failure text.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := r.RemoteAddr
	if !s.authLimiter.allow(ip) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts")
		return
	}

	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	reg, err := s.accounts.Login(r.Context(), body.Email, body.Password, ip, r.UserAgent())
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			// Identical text for unknown email and wrong password.
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
			return
		}
		s.logger.Error("login", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}

	auth.SetCookie(w, reg.Token, reg.Expires, s.cookieSecure)
	newSu := &auth.SessionUser{
		UserID:      reg.User.ID,
		IsGuest:     reg.User.IsGuest,
		DisplayName: reg.User.DisplayName,
		Email:       reg.User.Email,
		Role:        reg.User.Role,
		Status:      reg.User.Status,
		CreatedAt:   reg.User.CreatedAt,
	}
	s.meFromUser(w, r, *newSu)
}

// handleVerifyEmail consumes a verification token.
func (s *Server) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if err := s.accounts.VerifyEmail(r.Context(), body.Token); err != nil {
		writeError(w, http.StatusBadRequest, "bad_token", "invalid or expired verification token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleForgotPassword always returns 202 whether or not the account exists.
func (s *Server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	if !s.authLimiter.allow(r.RemoteAddr) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts")
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	token, err := s.accounts.ForgotPassword(r.Context(), body.Email)
	if err != nil {
		s.logger.Error("forgot password", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	if token != "" {
		// Dev "email" delivery: reset tokens are logged, not sent.
		s.logger.Info("password reset email (dev delivery)", "reset_token", token)
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

// handleResetPassword consumes the reset token and revokes all sessions.
func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if err := s.accounts.ResetPassword(r.Context(), body.Token, body.Password); err != nil {
		if errors.Is(err, auth.ErrWeakPassword) {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "bad_token", "invalid or expired reset token")
		return
	}
	// Every session for this user was revoked, including the caller's.
	auth.ClearCookie(w, s.cookieSecure)
	w.WriteHeader(http.StatusNoContent)
}

// handleListSessions lists the current user's active sessions.
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	sessions, err := s.accounts.ListActiveSessions(r.Context(), su.UserID)
	if err != nil {
		s.logger.Error("list sessions", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	type sessionDTO struct {
		ID         int64    `json:"id"`
		CreatedAt  string   `json:"createdAt"`
		LastSeenAt string   `json:"lastSeenAt"`
		ExpiresAt  string   `json:"expiresAt"`
		IP         *string  `json:"ip"`
		UserAgent  *string  `json:"userAgent"`
	}
	out := make([]sessionDTO, 0, len(sessions))
	for _, si := range sessions {
		out = append(out, sessionDTO{
			ID:         si.ID,
			CreatedAt:  si.CreatedAt.Format(time.RFC3339Nano),
			LastSeenAt: si.LastSeenAt.Format(time.RFC3339Nano),
			ExpiresAt:  si.ExpiresAt.Format(time.RFC3339Nano),
			IP:         si.IP,
			UserAgent:  si.UserAgent,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRevokeSession revokes one of the user's sessions.
func (s *Server) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid session id")
		return
	}
	if err := s.accounts.RevokeOwnSession(r.Context(), su.UserID, id); err != nil {
		s.logger.Error("revoke session", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	if id == su.SessionID {
		auth.ClearCookie(w, s.cookieSecure)
	}
	w.WriteHeader(http.StatusNoContent)
}
