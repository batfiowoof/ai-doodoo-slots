package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/bus"
	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/gorilla/websocket"
)

const (
	// sendBuffer caps per-connection outbound events. When it fills, the
	// client is dropped: a slow client must never block a room broadcast.
	sendBuffer = 64
	// writeTimeout bounds each frame write.
	writeTimeout = 5 * time.Second
	// pongWait is how long a connection may stay silent; pings every
	// pongWait/2 keep it alive.
	pongWait = 60 * time.Second
	// maxInboundPerSecond caps inbound message rate per connection.
	maxInboundPerSecond = 20
	// maxRoomsPerConnection: one room per connection for now (the lobby is
	// a separate pseudo-room and does not count).
	maxRoomsPerConnection = 1
	// LobbyTopicName is the pseudo-room for lobby subscriptions.
	LobbyTopicName = "lobby"
)

// RoomSource supplies room state snapshots for join and reconnect. The
// gameserver registers live providers; static rooms fall back to their
// configured definition.
type RoomSource interface {
	// Snapshot returns the full room state for a (re)connecting client.
	Snapshot(slug string) (json.RawMessage, bool)
	// RoomExists reports whether the slug names an active room.
	RoomExists(slug string) bool
}

// Hub owns connections and room subscriptions.
type Hub struct {
	auth Authenticator
	src  RoomSource
	bus  bus.Bus
	clk  clock.Clock
	log  *slog.Logger
	bets BetHandler

	roomInfo     func() map[string]map[string]any
	roomHandlers map[string]RoomHandler

	allowedOrigins map[string]bool

	betsMu sync.RWMutex

	mu      sync.RWMutex
	clients map[*Client]bool
	rooms   map[string]map[*Client]bool // slug -> subscribed clients
}

// SetBetHandler attaches the money path (wired after the round runners
// start). Calls before that answer with an unavailable error.
func (h *Hub) SetBetHandler(b BetHandler) {
	h.betsMu.Lock()
	h.bets = b
	h.betsMu.Unlock()
}

// SetRoomHandler attaches the game-action path for one room (wired after
// the room's table runner starts). game_action messages from connections
// joined to that room route through it; the BetHandler (crash) is untouched.
func (h *Hub) SetRoomHandler(slug string, rh RoomHandler) {
	h.betsMu.Lock()
	if h.roomHandlers == nil {
		h.roomHandlers = make(map[string]RoomHandler)
	}
	h.roomHandlers[slug] = rh
	h.betsMu.Unlock()
}

// RoomHandlerFor returns the game-action handler for a room, if any.
func (h *Hub) RoomHandlerFor(slug string) RoomHandler {
	h.betsMu.RLock()
	defer h.betsMu.RUnlock()
	return h.roomHandlers[slug]
}

// SetRoomInfo attaches a live-round catalog used to enrich lobby summaries:
// it returns info for every active room (not just occupied ones), so an idle
// lobby still shows each cabinet. The lobby never receives raw round ticks —
// only this coarse, 1Hz digest.
func (h *Hub) SetRoomInfo(f func() map[string]map[string]any) {
	h.betsMu.Lock()
	h.roomInfo = f
	h.betsMu.Unlock()
}

func (h *Hub) betHandler() BetHandler {
	h.betsMu.RLock()
	defer h.betsMu.RUnlock()
	return h.bets
}

func NewHub(auth Authenticator, src RoomSource, b bus.Bus, clk clock.Clock, log *slog.Logger) *Hub {
	allowed := make(map[string]bool)
	if v := os.Getenv("WS_ALLOWED_ORIGINS"); v != "" {
		for _, o := range strings.Split(v, ",") {
			if o = strings.TrimSpace(o); o != "" {
				allowed[o] = true
			}
		}
	}
	return &Hub{
		auth:           auth,
		src:            src,
		bus:            b,
		clk:            clk,
		log:            log,
		allowedOrigins: allowed,
		clients:        make(map[*Client]bool),
		rooms:          make(map[string]map[*Client]bool),
	}
}

// Run drives the hub until the context is cancelled: bus events (session
// revocations, status changes, room events from round loops) and 1Hz lobby
// summaries.
func (h *Hub) Run(ctx context.Context) {
	sessionSub := h.bus.Subscribe(TopicSession)
	userSub := h.bus.Subscribe(TopicUser)
	roomSub := h.bus.Subscribe(TopicRooms)
	lobbyTick := time.NewTicker(1 * time.Second)
	defer lobbyTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-sessionSub.Chan():
			h.handleSessionEvent(ev)
		case ev := <-userSub.Chan():
			h.handleUserEvent(ev)
		case ev := <-roomSub.Chan():
			// Round-loop events fan out to the room's subscribers only;
			// the lobby never receives round ticks.
			h.BroadcastRoom(ev.Room, Message{Type: ev.Type, Payload: ev.Payload})
		case <-lobbyTick.C:
			h.broadcastLobbySummary()
		}
	}
}

// ServeHTTP upgrades and registers a connection. Unauthenticated upgrades
// are rejected before any socket exists. Cross-origin upgrades are allowed
// only for origins listed in WS_ALLOWED_ORIGINS (the BFF origin).
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, ok := h.auth(r)
	if !ok || id == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	origin := r.Header.Get("Origin")
	if origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
		if !h.allowedOrigins[origin] {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
	}
	// The origin gate above already made the allowlist decision (same-host
	// always passes; cross-origin only from WS_ALLOWED_ORIGINS), so the
	// upgrader's own CheckOrigin defers to it.
	up := websocket.Upgrader{
		ReadBufferSize: 1024, WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		h.log.Warn("ws upgrade failed", "err", err)
		return
	}
	c := &Client{
		hub:   h,
		conn:  conn,
		send:  make(chan []byte, sendBuffer),
		id:    *id,
		rooms: make(map[string]bool),
	}
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
	go c.writePump()
	go c.readPump()
}

// --- client lifecycle -------------------------------------------------

func (h *Hub) remove(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	for slug := range c.rooms {
		if room := h.rooms[slug]; room != nil {
			delete(room, c)
			if len(room) == 0 {
				delete(h.rooms, slug)
			}
		}
	}
	h.mu.Unlock()
}

// handleSessionEvent closes every open connection whose session was
// revoked, after delivering the session_revoked message.
func (h *Hub) handleSessionEvent(ev bus.Event) {
	var p struct {
		UserID    int64 `json:"userId"`
		SessionID int64 `json:"sessionId"`
	}
	if json.Unmarshal(ev.Payload, &p) != nil {
		return
	}
	h.mu.RLock()
	var targets []*Client
	for c := range h.clients {
		if c.id.UserID == p.UserID && (p.SessionID == 0 || c.id.SessionID == p.SessionID) {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range targets {
		c.sendJSON(Message{Type: "session_revoked"})
		c.close()
	}
}

// handleUserEvent closes connections of accounts whose status now forbids
// anything further (banned, self_excluded) — they may read over HTTP but
// their live sockets go down on status change.
func (h *Hub) handleUserEvent(ev bus.Event) {
	var p struct {
		UserID int64  `json:"userId"`
		Status string `json:"status"`
	}
	if json.Unmarshal(ev.Payload, &p) != nil {
		return
	}
	if p.Status == "active" {
		return
	}
	h.mu.RLock()
	var targets []*Client
	for c := range h.clients {
		if c.id.UserID == p.UserID {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range targets {
		c.sendJSON(Message{Type: "session_revoked"})
		c.close()
	}
}

// presence returns per-room connection counts with user-ID deduplication
// (two tabs are one player) plus the lobby count.
func (h *Hub) presence() (map[string]int, int) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	roomCounts := make(map[string]int, len(h.rooms))
	for slug, room := range h.rooms {
		seen := make(map[int64]bool, len(room))
		for c := range room {
			if !seen[c.id.UserID] {
				seen[c.id.UserID] = true
				roomCounts[slug]++
			}
		}
	}
	seen := make(map[int64]bool, len(h.clients))
	lobby := 0
	for c := range h.clients {
		if !seen[c.id.UserID] {
			seen[c.id.UserID] = true
			lobby++
		}
	}
	return roomCounts, lobby
}

// Presence is the exported view for the lobby HTTP surface.
func (h *Hub) Presence() (map[string]int, int) { return h.presence() }

func (h *Hub) broadcastLobbySummary() {
	rooms, players := h.presence()
	h.betsMu.RLock()
	catalog := h.roomInfo
	h.betsMu.RUnlock()

	detail := map[string]any{}
	if catalog != nil {
		for slug, live := range catalog() {
			entry := map[string]any{"players": rooms[slug]}
			entry["state"] = live["state"]
			entry["multiplier"] = live["multiplier"]
			entry["recentCrashes"] = live["recentCrashes"]
			detail[slug] = entry
		}
	} else {
		for slug, count := range rooms {
			detail[slug] = map[string]any{"players": count}
		}
	}
	payload, err := json.Marshal(map[string]any{
		"rooms":            detail,
		"connectedPlayers": players,
	})
	if err != nil {
		return
	}
	h.mu.RLock()
	targets := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		if c.rooms[LobbyTopicName] {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range targets {
		c.sendJSON(Message{Type: "lobby_summary", Payload: payload})
	}
}

// broadcastRoom fans an event out to a room's subscribers. Delivery is
// fire-and-forget into bounded buffers.
func (h *Hub) BroadcastRoom(slug string, m Message) {
	payload, _ := json.Marshal(m)
	h.mu.RLock()
	room := h.rooms[slug]
	targets := make([]*Client, 0, len(room))
	for c := range room {
		targets = append(targets, c)
	}
	h.mu.RUnlock()
	for _, c := range targets {
		select {
		case c.send <- payload:
		default:
			c.close() // slow client dropped
		}
	}
}


