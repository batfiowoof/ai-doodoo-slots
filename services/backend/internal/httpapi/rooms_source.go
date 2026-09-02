package httpapi

import (
	"context"
	"encoding/json"

	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/ai-doodoo-slots/services/backend/internal/ws"
)

// roomsSource serves room existence and snapshots from the rooms table.
// Live round state plugs in at phase 13, when the gameserver's round loops
// register a richer source; until then the snapshot carries the room
// definition and a null round.
type roomsSource struct {
	s *Server
}

var _ ws.RoomSource = (*roomsSource)(nil)

func (s *Server) roomSource() ws.RoomSource { return &roomsSource{s: s} }

func (r *roomsSource) RoomExists(slug string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	_, err := store.New(r.s.pool).GetRoomBySlug(ctx, slug)
	return err == nil
}

func (r *roomsSource) Snapshot(slug string) (json.RawMessage, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	room, err := store.New(r.s.pool).GetRoomBySlug(ctx, slug)
	if err != nil {
		return nil, false
	}
	counts, _ := r.s.presence()
	round := any(nil)
	if r.s.roomLive != nil {
		if live, ok := r.s.roomLive(slug); ok {
			round = live
		}
	}
	payload, err := json.Marshal(map[string]any{
		"room": roomDTO{
			ID: room.ID, Slug: room.Slug, Name: room.Name, GameID: room.GameID,
			MinBet: room.MinBet, MaxBet: room.MaxBet, Capacity: int(room.Capacity),
			PlayerCount: counts[slug],
		},
		"round": round,
	})
	if err != nil {
		return nil, false
	}
	return payload, true
}
