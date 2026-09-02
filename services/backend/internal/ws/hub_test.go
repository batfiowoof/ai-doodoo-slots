package ws

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/bus"
	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/gorilla/websocket"
)

// testSource is a static RoomSource.
type testSource struct{ slugs map[string]bool }

func (s testSource) RoomExists(slug string) bool { return s.slugs[slug] }
func (s testSource) Snapshot(slug string) (json.RawMessage, bool) {
	if !s.slugs[slug] {
		return nil, false
	}
	return json.RawMessage(`{"slug":"` + slug + `","state":"betting_open","seq":42}`), true
}

// cookieAuth authenticates via a test token in the cookie header.
func cookieAuth(valid map[string]Identity) Authenticator {
	return func(r *http.Request) (*Identity, bool) {
		c, err := r.Cookie("retro_session")
		if err != nil {
			return nil, false
		}
		id, ok := valid[c.Value]
		return &id, ok
	}
}

func newTestHub(t *testing.T, auth Authenticator) (*Hub, *bus.MemoryBus, *httptest.Server) {
	t.Helper()
	b := bus.NewMemoryBus()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewHub(auth, testSource{slugs: map[string]bool{"crash-1": true}}, b, clock.Real{}, log)
	srv := httptest.NewServer(http.HandlerFunc(h.ServeHTTP))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { srv.CloseClientConnections() })

	ctx, cancel := context.WithCancel(context.Background())
	go h.Run(ctx)
	t.Cleanup(cancel)
	return h, b, srv
}

func dial(t *testing.T, srv *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	hdr := http.Header{}
	if token != "" {
		hdr.Set("Cookie", "retro_session="+token)
	}
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, hdr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func recv(t *testing.T, c *websocket.Conn, timeout time.Duration) Message {
	t.Helper()
	_ = c.SetReadDeadline((clock.Real{}).Now().Add(timeout))
	_, raw, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m Message
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

func send(t *testing.T, c *websocket.Conn, m Message) {
	t.Helper()
	raw, _ := json.Marshal(m)
	if err := c.WriteMessage(websocket.TextMessage, raw); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestUnauthenticatedSocketRejected(t *testing.T) {
	_, _, srv := newTestHub(t, cookieAuth(map[string]Identity{}))
	hdr := http.Header{}
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, hdr)
	if err == nil {
		t.Fatal("unauthenticated upgrade must be rejected")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestJoinRoomDeliversSnapshot(t *testing.T) {
	_, _, srv := newTestHub(t, cookieAuth(map[string]Identity{
		"tok1": {UserID: 1, SessionID: 1, DisplayName: "p1", Role: "player", Status: "active"},
	}))
	c := dial(t, srv, "tok1")
	send(t, c, Message{Type: "join_room", Payload: json.RawMessage(`{"slug":"crash-1"}`)})
	m := recv(t, c, 2*time.Second)
	if m.Type != "room_snapshot" {
		t.Fatalf("type = %q, want room_snapshot", m.Type)
	}
	var snap struct {
		Slug  string `json:"slug"`
		State string `json:"state"`
		Seq   int    `json:"seq"`
	}
	if json.Unmarshal(m.Payload, &snap) != nil || snap.Slug != "crash-1" || snap.Seq != 42 {
		t.Fatalf("bad snapshot: %s", m.Payload)
	}
}

// TestReconnectGetsCorrectSnapshot is the phase-12 gate: a client that
// disconnects mid-stream lands in the correct visual state on reconnect —
// every join (first connect or reconnect) receives a full room snapshot.
func TestReconnectGetsCorrectSnapshot(t *testing.T) {
	_, _, srv := newTestHub(t, cookieAuth(map[string]Identity{
		"tok1": {UserID: 1, SessionID: 1, DisplayName: "p1", Role: "player", Status: "active"},
	}))

	for round := 0; round < 2; round++ {
		c := dial(t, srv, "tok1")
		send(t, c, Message{Type: "join_room", Payload: json.RawMessage(`{"slug":"crash-1"}`)})
		m := recv(t, c, 2*time.Second)
		if m.Type != "room_snapshot" {
			t.Fatalf("round %d: type = %q", round, m.Type)
		}
		var snap map[string]any
		if json.Unmarshal(m.Payload, &snap) != nil || snap["slug"] != "crash-1" {
			t.Fatalf("round %d: bad snapshot %s", round, m.Payload)
		}
		c.Close()
	}
}

// TestSessionRevocationClosesSocket is the phase-12 gate: revoking a
// session closes its open socket after delivering session_revoked.
func TestSessionRevocationClosesSocket(t *testing.T) {
	h, b, srv := newTestHub(t, cookieAuth(map[string]Identity{
		"tok1": {UserID: 7, SessionID: 11, DisplayName: "p1", Role: "player", Status: "active"},
	}))
	c := dial(t, srv, "tok1")
	send(t, c, Message{Type: "subscribe_lobby"})
	// Wait for the subscription to register.
	deadline := (clock.Real{}).Now().Add(2 * time.Second)
	for {
		_, n := h.presence()
		if n == 1 || (clock.Real{}).Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	payload, _ := json.Marshal(map[string]any{"userId": 7, "sessionId": 11})
	b.Publish(bus.Event{Topic: TopicSession, Type: "session_revoked", Payload: payload})

	// Expect session_revoked (skipping interleaved lobby summaries), then
	// the connection to close.
	_ = c.SetReadDeadline((clock.Real{}).Now().Add(3 * time.Second))
	for {
		m := recv(t, c, 3*time.Second)
		if m.Type == "session_revoked" {
			break
		}
	}
	_ = c.SetReadDeadline((clock.Real{}).Now().Add(2 * time.Second))
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			break // closed as expected
		}
	}

	// Presence drops back to zero.
	time.Sleep(50 * time.Millisecond)
	if _, n := h.presence(); n != 0 {
		t.Fatalf("presence = %d after revocation, want 0", n)
	}
}

func TestLobbySummaryFlowsToSubscribersOnly(t *testing.T) {
	h, _, srv := newTestHub(t, cookieAuth(map[string]Identity{
		"tok1": {UserID: 1, SessionID: 1, DisplayName: "p1", Role: "player", Status: "active"},
		"tok2": {UserID: 2, SessionID: 2, DisplayName: "p2", Role: "player", Status: "active"},
	}))
	watcher := dial(t, srv, "tok1")
	idler := dial(t, srv, "tok2")
	send(t, watcher, Message{Type: "subscribe_lobby"})

	// The 1Hz ticker broadcasts; wait for one summary on the subscriber.
	var got *Message
	deadline := (clock.Real{}).Now().Add(3 * time.Second)
	for got == nil && (clock.Real{}).Now().Before(deadline) {
		m := recv(t, watcher, time.Until(deadline))
		if m.Type == "lobby_summary" {
			got = &m
		}
	}
	if got == nil {
		t.Fatal("no lobby_summary received")
	}
	// The idle (non-subscribed) connection must have received nothing.
	_ = idler.SetReadDeadline((clock.Real{}).Now().Add(300 * time.Millisecond))
	if _, _, err := idler.ReadMessage(); err == nil {
		t.Fatal("idle connection received a message")
	}
	_ = h
}

// fakeBets records calls and returns canned errors.
type fakeBets struct{ err error }

func (f fakeBets) PlaceBet(Identity, int64, int64, string) (map[string]any, error) {
	if f.err != nil {
		return nil, f.err
	}
	return map[string]any{"betCredits": 100}, nil
}
func (f fakeBets) CashOut(Identity) (map[string]any, error) { return nil, f.err }

// TestBannedUserBetRejected is the phase-14 gate: every socket message runs
// the same authorization path as a request -- a banned account cannot reach
// the bet path even with an open connection.
func TestBannedUserBetRejected(t *testing.T) {
	b := bus.NewMemoryBus()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewHub(cookieAuth(map[string]Identity{
		"tok1": {UserID: 9, SessionID: 9, DisplayName: "banned", Role: "player", Status: "banned"},
	}), testSource{slugs: map[string]bool{"crash-1": true}}, b, clock.Real{}, log)
	h.SetBetHandler(fakeBets{})
	srv := httptest.NewServer(http.HandlerFunc(h.ServeHTTP))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithCancel(context.Background())
	go h.Run(ctx)
	t.Cleanup(cancel)

	c := dial(t, srv, "tok1")
	send(t, c, Message{Type: "join_room", Payload: json.RawMessage(`{"slug":"crash-1"}`)})
	recv(t, c, 2*time.Second) // room_snapshot
	send(t, c, Message{Type: "place_bet", Payload: json.RawMessage(`{"credits":100,"autoCashout":2,"idempotencyKey":"k1"}`)})
	m := recv(t, c, 2*time.Second)
	if m.Type != "error" {
		t.Fatalf("type = %q, want error", m.Type)
	}
	var p struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(m.Payload, &p) != nil || p.Code != "status_forbids_betting" {
		t.Fatalf("code = %q", p.Code)
	}
}

// TestUnauthenticatedBetMessageRejected: without a session the upgrade is
// refused outright � no socket, no message path.
func TestUnauthenticatedBetMessageRejected(t *testing.T) {
	_, _, srv := newTestHub(t, cookieAuth(map[string]Identity{}))
	hdr := http.Header{}
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	if _, resp, err := websocket.DefaultDialer.Dial(wsURL, hdr); err == nil {
		t.Fatal("unauthenticated upgrade accepted")
	} else if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
