package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/bus"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
)

// queryTimeout bounds DB work in snapshot/lobby paths.
const queryTimeout = 3 * time.Second

type roomDTO struct {
	ID          int64  `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	GameID      string `json:"gameId"`
	MinBet      int64  `json:"minBet"`
	MaxBet      int64  `json:"maxBet"`
	Capacity    int    `json:"capacity"`
	PlayerCount int    `json:"playerCount"`
}

// handleLobby lists rooms with live summaries. Presence counts come from
// live connections, deduplicated by user ID — no persisted room membership
// exists, so a crashed process leaves no ghost players.
func (s *Server) handleLobby(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()
	rooms, err := store.New(s.pool).ListActiveRooms(ctx)
	if err != nil {
		s.logger.Error("list rooms", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	counts, connected := s.presence()
	out := make([]roomDTO, 0, len(rooms))
	for _, room := range rooms {
		out = append(out, roomDTO{
			ID: room.ID, Slug: room.Slug, Name: room.Name, GameID: room.GameID,
			MinBet: room.MinBet, MaxBet: room.MaxBet, Capacity: int(room.Capacity),
			PlayerCount: counts[room.Slug],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rooms":           out,
		"connectedPlayers": connected,
	})
}

// handleRoomDetail serves standalone room state so deep links to a room
// work without visiting the lobby first.
func (s *Server) handleRoomDetail(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if snap, ok := s.roomSource().Snapshot(slug); ok {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(snap)
		return
	}
	writeError(w, http.StatusNotFound, "unknown_room", "no such room")
}

func (s *Server) presence() (map[string]int, int) {
	if s.hub == nil {
		return map[string]int{}, 0
	}
	return s.hub.Presence()
}

// publishSessionEvent notifies the hub (and any other subscribers) that a
// session was revoked; open sockets receive session_revoked and close.
func (s *Server) publishSessionEvent(userID, sessionID int64) {
	if s.bus == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{"userId": userID, "sessionId": sessionID})
	if err != nil {
		return
	}
	s.bus.Publish(bus.Event{Topic: "session", Type: "session_revoked", Payload: payload})
}

// publishStatusEvent closes sockets of banned / self-excluded accounts.
func (s *Server) publishStatusEvent(userID int64, status string) {
	if s.bus == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{"userId": userID, "status": status})
	if err != nil {
		return
	}
	s.bus.Publish(bus.Event{Topic: "user", Type: "status_changed", Payload: payload})
}
