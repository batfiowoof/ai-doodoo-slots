// Package ws owns the realtime surface: one authenticated connection per
// player, multiplexed across the lobby and at most one room. The server is
// authoritative — clients send intents, and every inbound message runs the
// same authorization gate as an HTTP request. Send buffers are bounded with
// a drop policy: a slow client is closed, never allowed to block a room.
package ws

import (
	"encoding/json"
	"net/http"

	"github.com/ai-doodoo-slots/services/backend/internal/admin"
)

// Identity is the authenticated caller attached to a connection, resolved
// by the same path as HTTP (session cookie or Keycloak Bearer token).
type Identity struct {
	UserID      int64
	SessionID   int64
	IsGuest     bool
	DisplayName string
	Role        string
	Status      string
	// Avatar mirrors the profile: preset sprite name ("" = none) and the
	// version counter that cache-busts the public avatar URL.
	AvatarPreset  string
	AvatarVersion int64
}

// CanWatch reports whether the identity may connect at all. Banned and
// self-excluded accounts may read, so they can still watch; only their bet
// paths are blocked (enforced server-side at the bet handler).
func (i *Identity) CanWatch() bool {
	return true
}

// IsStaff reports moderator+.
func (i *Identity) IsStaff() bool {
	return i.Role == admin.RoleModerator || i.Role == admin.RoleAdmin
}

// Authenticator resolves the upgrade request to an Identity. It is supplied
// by the httpapi layer so the socket shares the exact auth path of HTTP.
type Authenticator func(r *http.Request) (*Identity, bool)

// BetHandler is the authorized money path for bet messages. Implementations
// re-check status, phase, and idempotency server-side; the socket layer
// never trusts message content beyond shape.
type BetHandler interface {
	PlaceBet(id Identity, credits, autoHundredths int64, idemKey string) (map[string]any, error)
	CashOut(id Identity) (map[string]any, error)
}

// RoomHandler is the authorized path for room-scoped game actions (poker
// buy-ins, folds, raises). The hub routes a game_action message to the
// handler registered for the connection's joined room; implementations
// re-check identity status, table state, and idempotency server-side, and
// return the authoritative ack payload. The socket layer never trusts
// message content beyond shape.
type RoomHandler interface {
	HandleGameAction(id Identity, payload json.RawMessage) (map[string]any, error)
}

// LobbyTopic and session/user/room topics are bus topics the hub listens to.
const (
	TopicLobby   = "lobby"
	TopicSession = "session"
	TopicUser    = "user"
	TopicRooms   = "rooms" // round-loop events; Room names the target room
)
