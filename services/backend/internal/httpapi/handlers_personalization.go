package httpapi

import (
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/ai-doodoo-slots/services/backend/internal/recs"
)

var eventGameIDPattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

// handlePlayerEvents accepts fire-and-forget launch events. Guests included:
// browsing is the only signal a guest ever produces.
func (s *Server) handlePlayerEvents(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	var body struct {
		Events []recs.Event `json:"events"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if len(body.Events) == 0 || len(body.Events) > 20 {
		writeError(w, http.StatusBadRequest, "bad_request", "events must contain 1-20 items")
		return
	}
	for _, e := range body.Events {
		if e.Type != "launch" || !eventGameIDPattern.MatchString(e.GameID) {
			writeError(w, http.StatusBadRequest, "bad_request", "each event needs type=launch and a game id")
			return
		}
		if len(e.Context) > 512 {
			writeError(w, http.StatusBadRequest, "bad_request", "event context too large")
			return
		}
	}
	accepted, err := s.recs.RecordLaunch(r.Context(), su.UserID, su.SessionID, body.Events)
	if err != nil {
		s.logger.Error("player events", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": accepted})
}

// handlePersonalizedLobby serves the caller's ranked lobby payload. RG
// gating happens inside the engine; the handler never overrides it.
func (s *Server) handlePersonalizedLobby(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	res, err := s.recs.For(r.Context(), recs.Player{ID: su.UserID, Status: su.Status})
	if err != nil {
		s.logger.Error("personalized lobby", "err", err, "user_id", su.UserID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleUpdatePreferences flips the personalization toggle. Audited like
// every other profile mutation; takes effect immediately.
func (s *Server) handleUpdatePreferences(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	var body struct {
		PersonalizeEnabled *bool `json:"personalizeEnabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if body.PersonalizeEnabled == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "personalizeEnabled is required")
		return
	}
	enabled, err := s.recs.SetPersonalize(r.Context(), su.UserID, *body.PersonalizeEnabled)
	if err != nil {
		s.logger.Error("update preferences", "err", err, "user_id", su.UserID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	s.auditProfile(r.Context(), su.UserID, "prefs.update", map[string]any{"personalize_enabled": enabled})
	writeJSON(w, http.StatusOK, map[string]any{"personalizeEnabled": enabled})
}
