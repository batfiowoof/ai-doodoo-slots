package ws

import (
	"encoding/json"
	"strings"
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
	lastChat    time.Time   // social cooldowns (per connection)
	lastEmote   time.Time
}

// social cooldown floors. The 20/s inbound cap is the backstop; these keep
// the chat readable without client-side cooperation.
const (
	chatCooldown  = 500 * time.Millisecond
	emoteCooldown = 400 * time.Millisecond
)

// identity snapshots the connection identity. The hub patches identities on
// profile_updated events, so readers copy under the client lock instead of
// racing the writer.
func (c *Client) identity() Identity {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.id
}

// patchIdentity applies the present fields of a profile_updated payload.
func (c *Client) patchIdentity(p profilePatch) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p.DisplayName != nil {
		c.id.DisplayName = *p.DisplayName
	}
	if p.AvatarPreset != nil {
		c.id.AvatarPreset = *p.AvatarPreset
	}
	if p.AvatarVersion != nil {
		c.id.AvatarVersion = *p.AvatarVersion
	}
	if p.Title != nil {
		c.id.Title = *p.Title
	}
	if p.NameEffect != nil {
		c.id.NameEffect = *p.NameEffect
	}
	if p.CardSkin != nil {
		c.id.CardSkin = *p.CardSkin
	}
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

// inbound message types. Every money-moving type is re-authorized per
// message: identity status (banned/self-excluded), phase, and idempotency
// are all checked server-side by the BetHandler or the room's RoomHandler.
var inboundTypes = map[string]bool{
	"subscribe_lobby":   true,
	"unsubscribe_lobby": true,
	"join_room":         true,
	"leave_room":        true,
	"place_bet":         true,
	"cash_out":          true,
	"game_action":       true,
	"send_chat":         true,
	"send_emote":        true,
	"send_tip":          true,
	"make_it_rain":      true,
	"chat_delete":       true,
	"chat_mute":         true,
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
		c.hub.broadcastLobbySummary()

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

	case "place_bet":
		c.handleBet("place_bet", m.Payload)

	case "cash_out":
		c.handleBet("cash_out", m.Payload)

	case "game_action":
		c.handleGameAction(m.Payload)

	case "send_chat":
		c.handleSocialChat(m.Payload)

	case "send_emote":
		c.handleSocialEmote(m.Payload)

	case "send_tip":
		c.handleSocialTip(m.Payload)

	case "make_it_rain":
		c.handleSocialRain(m.Payload)

	case "chat_delete":
		c.handleSocialDelete(m.Payload)

	case "chat_mute":
		c.handleSocialMute(m.Payload)
	}
}

// socialError answers a social message with the shared error envelope.
func (c *Client) socialError(code string) {
	raw, _ := json.Marshal(map[string]any{"code": code})
	c.sendJSON(Message{Type: "error", Payload: raw})
}

// socialGate is the shared pre-flight for every social message: an active
// account, a wired handler, and a per-connection cooldown floor.
func (c *Client) socialGate(cooldown time.Duration, last *time.Time) (SocialHandler, bool) {
	if c.identity().Status != "active" {
		c.socialError("status_forbids_social")
		return nil, false
	}
	h := c.hub.socialHandler()
	if h == nil {
		c.socialError("social_unavailable")
		return nil, false
	}
	now := c.hub.clk.Now()
	c.mu.Lock()
	ready := now.Sub(*last) >= cooldown
	if ready {
		*last = now
	}
	c.mu.Unlock()
	if !ready {
		c.socialError("social_cooldown")
		return nil, false
	}
	return h, true
}

// socialFail maps a handler error (coded when the handler says so) onto the
// error envelope.
func (c *Client) socialFail(err error) {
	code := "social_rejected"
	if ce, ok := err.(interface{ Code() string }); ok {
		code = ce.Code()
	}
	c.socialError(code)
}

// handleSocialChat validates, persists (handler), and broadcasts one line.
func (c *Client) handleSocialChat(payload json.RawMessage) {
	var p struct {
		Body string `json:"body"`
	}
	if json.Unmarshal(payload, &p) != nil {
		c.socialError("bad_request")
		return
	}
	body := strings.TrimSpace(p.Body)
	if len(body) == 0 || len(body) > 256 {
		c.socialError("bad_request")
		return
	}
	h, ok := c.socialGate(chatCooldown, &c.lastChat)
	if !ok {
		return
	}
	msg, err := h.SendChat(c.identity(), body)
	if err != nil {
		c.socialFail(err)
		return
	}
	c.hub.broadcastMapAll("chat_message", msg)
}

// handleSocialEmote validates the id shape and broadcasts an ephemeral
// reaction. Clients silently ignore ids they have no art for.
func (c *Client) handleSocialEmote(payload json.RawMessage) {
	var p struct {
		EmoteID string `json:"emoteId"`
	}
	if json.Unmarshal(payload, &p) != nil {
		c.socialError("bad_request")
		return
	}
	if !validEmoteID(p.EmoteID) {
		c.socialError("bad_request")
		return
	}
	h, ok := c.socialGate(emoteCooldown, &c.lastEmote)
	if !ok {
		return
	}
	msg, err := h.SendEmote(c.identity(), p.EmoteID)
	if err != nil {
		c.socialFail(err)
		return
	}
	c.hub.broadcastMapAll("emote", msg)
}

// handleSocialTip moves credits to another player and announces it.
func (c *Client) handleSocialTip(payload json.RawMessage) {
	var p struct {
		ToUserID int64 `json:"toUserId"`
		Credits  int64 `json:"credits"`
	}
	if json.Unmarshal(payload, &p) != nil || p.ToUserID == 0 || p.Credits <= 0 {
		c.socialError("bad_request")
		return
	}
	h, ok := c.socialGate(chatCooldown, &c.lastChat)
	if !ok {
		return
	}
	tip, chat, err := h.SendTip(c.identity(), p.ToUserID, p.Credits)
	if err != nil {
		c.socialFail(err)
		return
	}
	c.hub.broadcastMapAll("tip", tip)
	if chat != nil {
		c.hub.broadcastMapAll("chat_message", chat)
	}
}

// handleSocialRain splits the pot across everyone currently online.
func (c *Client) handleSocialRain(payload json.RawMessage) {
	var p struct {
		Credits int64 `json:"credits"`
	}
	if json.Unmarshal(payload, &p) != nil || p.Credits <= 0 {
		c.socialError("bad_request")
		return
	}
	h, ok := c.socialGate(chatCooldown, &c.lastChat)
	if !ok {
		return
	}
	id := c.identity()
	recipients := c.hub.OnlineUserIDs(id.UserID)
	if len(recipients) == 0 {
		c.socialError("no_recipients")
		return
	}
	rain, chat, err := h.Rain(id, p.Credits, recipients)
	if err != nil {
		c.socialFail(err)
		return
	}
	c.hub.broadcastMapAll("rain", rain)
	if chat != nil {
		c.hub.broadcastMapAll("chat_message", chat)
	}
}

// handleSocialDelete soft-deletes one chat line (staff only).
func (c *Client) handleSocialDelete(payload json.RawMessage) {
	var p struct {
		MessageID int64 `json:"messageId"`
	}
	if json.Unmarshal(payload, &p) != nil || p.MessageID <= 0 {
		c.socialError("bad_request")
		return
	}
	h := c.hub.socialHandler()
	if h == nil {
		c.socialError("social_unavailable")
		return
	}
	if !c.identity().IsStaff() {
		c.socialError("forbidden")
		return
	}
	msg, err := h.DeleteChatMessage(c.identity(), p.MessageID)
	if err != nil {
		c.socialFail(err)
		return
	}
	if msg != nil {
		c.hub.broadcastMapAll("chat_deleted", msg)
	}
}

// handleSocialMute disables chat for a user (staff only).
func (c *Client) handleSocialMute(payload json.RawMessage) {
	var p struct {
		UserID  int64  `json:"userId"`
		Minutes int64  `json:"minutes"`
		Reason  string `json:"reason"`
	}
	if json.Unmarshal(payload, &p) != nil || p.UserID <= 0 || p.Minutes <= 0 || p.Minutes > 60*24*30 {
		c.socialError("bad_request")
		return
	}
	h := c.hub.socialHandler()
	if h == nil {
		c.socialError("social_unavailable")
		return
	}
	if !c.identity().IsStaff() {
		c.socialError("forbidden")
		return
	}
	if err := h.MuteUser(c.identity(), p.UserID, p.Minutes, p.Reason); err != nil {
		c.socialFail(err)
		return
	}
	// The offender learns about it directly; staff gets an ack-free silence.
	c.hub.sendToUser(p.UserID, Message{
		Type: "error",
		Payload: json.RawMessage(`{"code":"muted","message":"you are muted"}`),
	})
}

// validEmoteID guards the ephemeral reaction id shape; the id set itself is
// a frontend registry concern and may grow without a server change.
func validEmoteID(id string) bool {
	if len(id) == 0 || len(id) > 32 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

// handleGameAction routes a room-scoped game action (poker buy-in, fold,
// raise…) through the joined room's RoomHandler — the same authorization
// path as an HTTP request — and answers with game_ack or error. The
// response payload is authoritative state from the server.
func (c *Client) handleGameAction(payload json.RawMessage) {
	// Per-message authorization: a connection authenticated before a ban
	// must not reach money.
	if c.identity().Status != "active" {
		c.sendJSON(Message{Type: "error", Payload: json.RawMessage(`{"code":"status_forbids_betting"}`)})
		return
	}
	var p struct {
		Action string `json:"action"`
	}
	if json.Unmarshal(payload, &p) != nil || p.Action == "" {
		c.sendJSON(Message{Type: "error", Payload: json.RawMessage(`{"code":"bad_request"}`)})
		return
	}
	// The message routes to the handler of the room this connection joined.
	room := c.joinedRoom()
	if room == "" {
		c.sendJSON(Message{Type: "error", Payload: json.RawMessage(`{"code":"not_in_room"}`)})
		return
	}
	h := c.hub.RoomHandlerFor(room)
	if h == nil {
		c.sendJSON(Message{Type: "error", Payload: json.RawMessage(`{"code":"no_game_in_room"}`)})
		return
	}
	resp, err := h.HandleGameAction(c.identity(), payload)
	if err != nil {
		code := "action_rejected"
		if ce, ok := err.(interface{ Code() string }); ok {
			code = ce.Code()
		}
		raw, _ := json.Marshal(map[string]any{"code": code, "message": err.Error(), "room": room})
		c.sendJSON(Message{Type: "error", Payload: raw})
		return
	}
	raw, _ := json.Marshal(resp)
	c.sendJSON(Message{Type: "game_ack", Payload: raw})
}

// handleBet routes a money message through the joined room's RoomHandler
// when one is registered (per-room limits and rounds), falling back to the
// global BetHandler. Either way the authorization path is the same as an
// HTTP request, and the bet_ack payload is authoritative server state.
func (c *Client) handleBet(kind string, payload json.RawMessage) {
	h := c.hub.betHandler()
	if h == nil && c.hub.RoomHandlerFor(c.joinedRoom()) == nil {
		c.sendJSON(Message{Type: "error", Payload: json.RawMessage(`{"code":"bets_unavailable"}`)})
		return
	}
	// Per-message authorization: a connection authenticated before a ban
	// must not bet.
	if c.identity().Status != "active" {
		c.sendJSON(Message{Type: "error", Payload: json.RawMessage(`{"code":"status_forbids_betting"}`)})
		return
	}
	// The message must target the room this connection joined.
	room := c.joinedRoom()
	if room == "" {
		c.sendJSON(Message{Type: "error", Payload: json.RawMessage(`{"code":"not_in_room"}`)})
		return
	}

	// Room-scoped routing: the wire verb rides in as the action.
	if rh := c.hub.RoomHandlerFor(room); rh != nil {
		resp, err := rh.HandleGameAction(c.identity(), injectAction(payload, kind))
		if err != nil {
			code := "bet_rejected"
			if ce, ok := err.(interface{ Code() string }); ok {
				code = ce.Code()
			}
			raw, _ := json.Marshal(map[string]any{"code": code, "message": err.Error(), "room": room})
			c.sendJSON(Message{Type: "error", Payload: raw})
			return
		}
		raw, _ := json.Marshal(resp)
		c.sendJSON(Message{Type: "bet_ack", Payload: raw})
		return
	}

	var resp map[string]any
	var err error
	id := c.identity()
	switch kind {
	case "place_bet":
		var p struct {
			Credits       int64   `json:"credits"`
			AutoCashout   float64 `json:"autoCashout"`
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if json.Unmarshal(payload, &p) != nil {
			c.sendJSON(Message{Type: "error", Payload: json.RawMessage(`{"code":"bad_request"}`)})
			return
		}
		hundredths := int64(p.AutoCashout * 100) // server rounds the target
		resp, err = h.PlaceBet(id, p.Credits, hundredths, p.IdempotencyKey)
	case "cash_out":
		resp, err = h.CashOut(id)
	}
	if err != nil {
		code := "bet_rejected"
		if ce, ok := err.(interface{ Code() string }); ok {
			code = ce.Code()
		}
		payload, _ := json.Marshal(map[string]any{"code": code, "message": err.Error(), "room": room})
		c.sendJSON(Message{Type: "error", Payload: payload})
		return
	}
	raw, _ := json.Marshal(resp)
	c.sendJSON(Message{Type: "bet_ack", Payload: raw})
}

// injectAction folds the wire verb into the payload so a RoomHandler can
// dispatch place_bet/cash_out like any other game action.
func injectAction(payload json.RawMessage, kind string) json.RawMessage {
	var m map[string]any
	if json.Unmarshal(payload, &m) != nil {
		m = map[string]any{}
	}
	m["action"] = kind
	raw, err := json.Marshal(m)
	if err != nil {
		return payload
	}
	return raw
}

// joinedRoom returns the connection's current (non-lobby) room, if any.
func (c *Client) joinedRoom() string {
	c.hub.mu.RLock()
	defer c.hub.mu.RUnlock()
	for slug := range c.rooms {
		if slug != LobbyTopicName {
			return slug
		}
	}
	return ""
}
