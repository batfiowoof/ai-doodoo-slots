package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
)

// handleFairCurrent returns the active seed hash, client seed, and next nonce.
func (s *Server) handleFairCurrent(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	seedHash, clientSeed, nextNonce, err := s.fair.Current(r.Context(), su.UserID)
	if err != nil {
		s.logger.Error("fair current", "err", err, "user_id", su.UserID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"serverSeedHash": seedHash,
		"clientSeed":     clientSeed,
		"nonce":          nextNonce,
	})
}

// handleFairRotate reveals the old server seed and issues a new pair.
func (s *Server) handleFairRotate(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}

	var body struct {
		ClientSeed string `json:"clientSeed"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil && !errors.Is(err, http.ErrBodyReadAfterClose) {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if len(body.ClientSeed) > fair.MaxClientSeedLen {
		writeError(w, http.StatusBadRequest, "bad_request", "clientSeed too long")
		return
	}

	revealed, newHash, clientSeed, err := s.fair.Rotate(r.Context(), su.UserID, body.ClientSeed)
	if err != nil {
		s.logger.Error("fair rotate", "err", err, "user_id", su.UserID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"revealedServerSeed": revealed,
		"serverSeedHash":     newHash,
		"clientSeed":         clientSeed,
		"nonce":              0,
	})
}
