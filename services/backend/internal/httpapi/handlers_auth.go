package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/auth"
	"github.com/ai-doodoo-slots/services/backend/internal/wallet"
	"github.com/ai-doodoo-slots/services/backend/internal/ws"
)

// SignupBonusCredits is the starting balance for new guests.
const SignupBonusCredits int64 = 1000

// bearerPrefix is the Authorization scheme for Keycloak access tokens.
const bearerPrefix = "Bearer "

// currentUser resolves the request identity. A Keycloak access token
// (Authorization: Bearer) wins; then the BFF's token cookie (which lets
// WebSocket upgrades from the browser authenticate as a Keycloak user);
// the guest session cookie is the final fallback.
func (s *Server) currentUser(r *http.Request) *auth.SessionUser {
	ctx := r.Context()
	if h := r.Header.Get("Authorization"); len(h) > len(bearerPrefix) && h[:len(bearerPrefix)] == bearerPrefix {
		return s.userFromKeycloakToken(ctx, h[len(bearerPrefix):])
	}
	if s.oidc != nil {
		if c, err := r.Cookie("retro_kc"); err == nil && c.Value != "" {
			if raw, derr := base64.RawURLEncoding.DecodeString(c.Value); derr == nil {
				var t struct {
					AccessToken string `json:"accessToken"`
				}
				if json.Unmarshal(raw, &t) == nil && t.AccessToken != "" {
					if su := s.userFromKeycloakToken(ctx, t.AccessToken); su != nil {
						return su
					}
				}
			}
		}
	}
	token, ok := auth.TokenFromRequest(r)
	if !ok {
		return nil
	}
	su, err := s.auth.SessionFromToken(ctx, token)
	if err != nil {
		return nil
	}
	return su
}

// userFromKeycloakToken verifies the access token and resolves the local
// user row, provisioning it on first sight.
func (s *Server) userFromKeycloakToken(ctx context.Context, raw string) *auth.SessionUser {
	if s.oidc == nil {
		return nil
	}
	claims, err := s.oidc.Verify(ctx, raw)
	if err != nil {
		return nil
	}
	res, err := s.auth.ResolveKeycloakUser(ctx, claims, "")
	if err != nil {
		s.logger.Error("resolve keycloak user", "err", err)
		return nil
	}
	if res.Created {
		// Fresh Keycloak users get the same signup provisioning as guests.
		if err := s.wallet.EnsureSignup(ctx, res.User.UserID, SignupBonusCredits); err != nil {
			s.logger.Error("provision wallet", "err", err, "user_id", res.User.UserID)
			return nil
		}
		if _, err := s.fair.EnsureForUser(ctx, res.User.UserID); err != nil {
			s.logger.Error("provision seed", "err", err, "user_id", res.User.UserID)
			return nil
		}
	}
	return res.User
}

// authIdentity adapts the shared HTTP auth path for WebSocket upgrades so
// sockets never grow a second authorization mechanism.
func (s *Server) authIdentity(r *http.Request) (*ws.Identity, bool) {
	su := s.currentUser(r)
	if su == nil {
		return nil, false
	}
	return &ws.Identity{
		UserID:      su.UserID,
		SessionID:   su.SessionID,
		IsGuest:     su.IsGuest,
		DisplayName: su.DisplayName,
		Role:        su.Role,
		Status:      su.Status,
	}, true
}

// handleKeycloakSession establishes the app-side identity after an OIDC
// login. The BFF posts the verified access token; any guest session cookie
// present is upgraded in place so wallet, bets, history, and seeds survive.
func (s *Server) handleKeycloakSession(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		writeError(w, http.StatusServiceUnavailable, "keycloak_unconfigured", "Keycloak is not configured")
		return
	}
	var body struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil || body.AccessToken == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "accessToken is required")
		return
	}

	claims, err := s.oidc.Verify(r.Context(), body.AccessToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_token", "invalid or expired token")
		return
	}

	guestToken, _ := auth.TokenFromRequest(r)
	res, err := s.auth.ResolveKeycloakUser(r.Context(), claims, guestToken)
	if err != nil {
		s.logger.Error("resolve keycloak user", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	if res.Created {
		if err := s.wallet.EnsureSignup(r.Context(), res.User.UserID, SignupBonusCredits); err != nil {
			s.logger.Error("provision wallet", "err", err, "user_id", res.User.UserID)
			writeError(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
		if _, err := s.fair.EnsureForUser(r.Context(), res.User.UserID); err != nil {
			s.logger.Error("provision seed", "err", err, "user_id", res.User.UserID)
			writeError(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
	}
	// The guest cookie has been consumed by the upgrade; clear it.
	if res.Upgraded {
		auth.ClearCookie(w, s.cookieSecure)
	}
	s.writeMe(w, r, res.User)
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
	s.writeMe(w, r, su)
}

// handleLogout revokes the guest session, clears its cookie, and closes any
// open socket for that session.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token, ok := auth.TokenFromRequest(r)
	if ok {
		if row, err := s.auth.SessionFromToken(r.Context(), token); err == nil {
			s.publishSessionEvent(row.UserID, row.SessionID)
		}
		if err := s.auth.Logout(r.Context(), token); err != nil {
			s.logger.Error("logout", "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
	}
	auth.ClearCookie(w, s.cookieSecure)
	w.WriteHeader(http.StatusNoContent)
}

// writeMe renders the meDTO for a resolved user.
func (s *Server) writeMe(w http.ResponseWriter, r *http.Request, su *auth.SessionUser) {
	balance, err := s.wallet.Balance(r.Context(), su.UserID)
	if err != nil {
		if errors.Is(err, wallet.ErrWalletNotFound) {
			balance = 0
		} else {
			s.logger.Error("read balance", "err", err, "user_id", su.UserID)
			writeError(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
	}
	writeJSON(w, http.StatusOK, meDTO{User: toUserDTO(su), BalanceCredits: balance})
}

// handleMe returns the current user, balance, and role.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	s.writeMe(w, r, su)
}

// handleListSessions lists the current user's active (guest) sessions.
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	sessions, err := s.auth.ListActiveSessions(r.Context(), su.UserID)
	if err != nil {
		s.logger.Error("list sessions", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	type sessionDTO struct {
		ID         int64   `json:"id"`
		CreatedAt  string  `json:"createdAt"`
		LastSeenAt string  `json:"lastSeenAt"`
		ExpiresAt  string  `json:"expiresAt"`
		IP         *string `json:"ip"`
		UserAgent  *string `json:"userAgent"`
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
	if err := s.auth.RevokeOwnSession(r.Context(), su.UserID, id); err != nil {
		s.logger.Error("revoke session", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	s.publishSessionEvent(su.UserID, id)
	if id == su.SessionID {
		auth.ClearCookie(w, s.cookieSecure)
	}
	w.WriteHeader(http.StatusNoContent)
}



