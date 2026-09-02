package ws

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Message is the single envelope for both directions.
type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Client is one authenticated connection.
type Client struct {
	hub   *Hub
	conn  *websocket.Conn
	send  chan []byte
	id    Identity
	rooms map[string]bool

	mu          sync.Mutex
	closed      bool
	tearingDown bool
	inbound     []time.Time // inbound message timestamps for the rate cap
}

// sendJSON queues a message; drops silently (and schedules teardown) when
// the connection is closed or the buffer is full.
func (c *Client) sendJSON(m Message) {
	payload, err := json.Marshal(m)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	select {
	case c.send <- payload:
	default:
		c.closed = true
		go c.teardown() // slow client dropped, never blocks a broadcast
	}
}

// teardown idempotently closes the connection. It runs on a goroutine so
// the write pump can flush already-queued messages (e.g. a session_revoked
// notice) before the socket dies.
func (c *Client) teardown() {
	c.mu.Lock()
	if c.tearingDown {
		c.mu.Unlock()
		return
	}
	c.tearingDown = true
	c.mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	_ = c.conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		c.hub.clk.Now().Add(writeTimeout),
	)
	_ = c.conn.Close()
	c.hub.remove(c)
}

// close marks the connection closed, removes it from presence immediately,
// and schedules teardown so queued messages (e.g. session_revoked) flush.
func (c *Client) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.tearingDown = true
	c.mu.Unlock()
	c.hub.remove(c)
	go c.teardown()
}

func (c *Client) writePump() {
	for payload := range c.send {
		_ = c.conn.SetWriteDeadline(c.hub.clk.Now().Add(writeTimeout))
		if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			return
		}
	}
}

// inbound message types. place_bet/cash_out arrive with the crash engine
// (phase 14); the guard rejects them explicitly until then.
var inboundTypes = map[string]bool{
	"subscribe_lobby":   true,
	"unsubscribe_lobby": true,
	"join_room":         true,
	"leave_room":        true,
}

func (c *Client) readPump() {
	defer c.close()
	c.conn.SetReadLimit(4096)
	_ = c.conn.SetReadDeadline(c.hub.clk.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(c.hub.clk.Now().Add(pongWait))
	})
	ping := time.NewTicker(pongWait / 2)
	defer ping.Stop()
	go func() {
		for range ping.C {
			_ = c.conn.SetWriteDeadline(c.hub.clk.Now().Add(writeTimeout))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}()

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		if !c.allowInbound() {
			c.sendJSON(Message{Type: "error", Payload: json.RawMessage(`{"code":"rate_limited"}`)})
			return
		}
		var m Message
		if json.Unmarshal(raw, &m) != nil || !inboundTypes[m.Type] {
			c.sendJSON(Message{Type: "error", Payload: json.RawMessage(`{"code":"unknown_or_forbidden_type"}`)})
			continue
		}
		c.handle(m)
	}
}

// allowInbound enforces the per-connection message cap.
func (c *Client) allowInbound() bool {
	now := c.hub.clk.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	kept := c.inbound[:0]
	for _, t := range c.inbound {
		if now.Sub(t) < time.Second {
			kept = append(kept, t)
		}
	}
	c.inbound = kept
	if len(c.inbound) >= maxInboundPerSecond {
		return false
	}
	c.inbound = append(c.inbound, now)
	return true
}

// handle dispatches an authorized inbound message. Every branch re-checks
// state that could have changed since connect (status, caps).
func (c *Client) handle(m Message) {
	switch m.Type {
	case "subscribe_lobby":
		c.hub.mu.Lock()
		c.rooms[LobbyTopicName] = true
		c.hub.mu.Unlock()
		rooms, players := c.hub.presence()
		payload, _ := json.Marshal(map[string]any{"rooms": rooms, "connectedPlayers": players})
		c.sendJSON(Message{Type: "lobby_summary", Payload: payload})

	case "unsubscribe_lobby":
		c.hub.mu.Lock()
		delete(c.rooms, LobbyTopicName)
		c.hub.mu.Unlock()

	case "join_room":
		var p struct{ Slug string }
		if json.Unmarshal(m.Payload, &p) != nil || p.Slug == "" {
			c.sendJSON(Message{Type: "error", Payload: json.RawMessage(`{"code":"bad_request"}`)})
			return
		}
		// Authorization: the room must exist and be active.
		if c.hub.src == nil || !c.hub.src.RoomExists(p.Slug) {
			c.sendJSON(Message{Type: "error", Payload: json.RawMessage(`{"code":"unknown_room"}`)})
			return
		}
		c.hub.mu.Lock()
		// Cap: one room per connection — leaving any current room first.
		for slug := range c.rooms {
			if slug != LobbyTopicName {
				delete(c.rooms, slug)
				if room := c.hub.rooms[slug]; room != nil {
					delete(room, c)
					if len(room) == 0 {
						delete(c.hub.rooms, slug)
					}
				}
			}
		}
		room := c.hub.rooms[p.Slug]
		if room == nil {
			room = make(map[*Client]bool)
			c.hub.rooms[p.Slug] = room
		}
		room[c] = true
		c.rooms[p.Slug] = true
		c.hub.mu.Unlock()
		// Full state snapshot on join and reconnect.
		if snap, ok := c.hub.src.Snapshot(p.Slug); ok {
			c.sendJSON(Message{Type: "room_snapshot", Payload: snap})
		}

	case "leave_room":
		c.hub.mu.Lock()
		for slug := range c.rooms {
			if slug == LobbyTopicName {
				continue
			}
			delete(c.rooms, slug)
			if room := c.hub.rooms[slug]; room != nil {
				delete(room, c)
				if len(room) == 0 {
					delete(c.hub.rooms, slug)
				}
			}
		}
		c.hub.mu.Unlock()
	}
}
