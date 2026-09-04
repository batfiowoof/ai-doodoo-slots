// Package httpapi contains the HTTP router, handlers, and middleware.
package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/admin"
	"github.com/ai-doodoo-slots/services/backend/internal/auth"
	"github.com/ai-doodoo-slots/services/backend/internal/bus"
	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
	"github.com/ai-doodoo-slots/services/backend/internal/game/blackjack"
	"github.com/ai-doodoo-slots/services/backend/internal/game/slots"
	"github.com/ai-doodoo-slots/services/backend/internal/hand"
	"github.com/ai-doodoo-slots/services/backend/internal/play"
	"github.com/ai-doodoo-slots/services/backend/internal/theme"
	"github.com/ai-doodoo-slots/services/backend/internal/wallet"
	"github.com/ai-doodoo-slots/services/backend/internal/ws"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Server holds the dependencies shared by all handlers.
type Server struct {
	pool         *pgxpool.Pool
	auth         *auth.Service
	oidc         *auth.OIDCVerifier // nil when Keycloak is not configured
	kcAdmin      *auth.AdminClient  // nil when profile write-back is not configured
	wallet       *wallet.Wallet
	fair         *fair.Service
	registry     *game.Registry
	play         *play.Service
	playLimiter  *rateLimiter
	authLimiter  *rateLimiter
	hand         *hand.Service // blackjack deal/action flow; nil-safe routes
	themes       *theme.Service
	admin        *admin.Service
	clock        clock.Clock
	logger       *slog.Logger
	cookieSecure bool

	bus bus.Bus
	hub *ws.Hub // set by WithHub; enables /api/v1/ws and lobby presence

	roomLive func(slug string) (map[string]any, bool)
}

// Option customizes the server at construction.
type Option func(*Server)

// WithThemeService attaches theme generation. Without it the theme
// endpoints report unavailability.
func WithThemeService(ts *theme.Service) Option {
	return func(s *Server) { s.themes = ts }
}

// WithOIDC attaches Keycloak token verification. Without it the API runs in
// guest-only mode.
func WithOIDC(v *auth.OIDCVerifier) Option {
	return func(s *Server) { s.oidc = v }
}

// WithKeycloakAdmin attaches the profile write-back client. Without it
// profile changes stay local and never reach Keycloak attributes.
func WithKeycloakAdmin(a *auth.AdminClient) Option {
	return func(s *Server) { s.kcAdmin = a }
}

// WithHub enables the realtime surface: an authenticated WebSocket endpoint
// at /api/v1/ws, room subscriptions, and live lobby presence. The hub rides
// the server's in-process bus; cross-process wiring arrives with Redis.
func WithHub() Option {
	return func(s *Server) {
		if s.bus == nil {
			s.bus = bus.NewMemoryBus()
		}
		s.hub = ws.NewHub(s.authIdentity, s.roomSource(), s.bus, s.clock, s.logger)
	}
}

// WithRoomLive attaches a provider for a room's live round state, used by
// the room detail endpoint so deep links show the current round.
func WithRoomLive(f func(slug string) (map[string]any, bool)) Option {
	return func(s *Server) { s.roomLive = f }
}

// Hub exposes the hub for callers that need to drive it (gameserver).
func (s *Server) Hub() *ws.Hub { return s.hub }

// SetRoomLive attaches the live-round provider after construction (used by
// the gameserver, whose runners start after the server is built).
func (s *Server) SetRoomLive(f func(slug string) (map[string]any, bool)) {
	s.roomLive = f
}

// Bus exposes the event bus.
func (s *Server) Bus() bus.Bus { return s.bus }

// Run starts background loops (the hub). Call from a goroutine.
func (s *Server) Run(ctx context.Context) {
	if s.hub != nil {
		s.hub.Run(ctx)
	}
}

// NewServer constructs the HTTP server with its dependency set.
func NewServer(pool *pgxpool.Pool, clk clock.Clock, logger *slog.Logger, cookieSecure bool, opts ...Option) *Server {
	registry := game.NewRegistry()
	registry.Register(slots.Classic())
	registry.Register(slots.FruitSalad())
	registry.Register(slots.Treasure())
	// Blackjack is a stateful multi-request game, not a single-call engine:
	// it registers metadata-only so the arcade floor lists it, while its
	// deal/action endpoints own the flow.
	bjEngine := blackjack.New([]int64{5, 10, 25, 50})
	registry.RegisterListing(game.Listing{
		ID:             blackjack.GameID,
		Name:           "Blackjack",
		TheoreticalRTP: bjEngine.TheoreticalRTP(),
		BetSteps:       bjEngine.BetSteps(),
		Kind:           "stateful",
	})
	s := &Server{
		pool:         pool,
		auth:         auth.NewService(pool, clk, logger),
		wallet:       wallet.New(pool),
		fair:         fair.NewService(pool),
		registry:     registry,
		play:         play.NewService(pool, registry),
		playLimiter:  newRateLimiter(clk, playWindow, playMax),
		authLimiter:  newRateLimiter(clk, time.Minute, 30),
		hand:         hand.NewService(pool, bjEngine, clk),
		admin:        admin.NewService(pool),
		themes:       nil,
		clock:        clk,
		logger:       logger,
		cookieSecure: cookieSecure,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Handler builds the full middleware-wrapped route table.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("POST /api/v1/auth/guest", s.handleAuthGuest)
	mux.HandleFunc("POST /api/v1/auth/keycloak/session", s.handleKeycloakSession)
	mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/v1/auth/sessions", s.handleListSessions)
	mux.HandleFunc("DELETE /api/v1/auth/sessions/{id}", s.handleRevokeSession)
	mux.HandleFunc("GET /api/v1/me", s.handleMe)
	mux.HandleFunc("PATCH /api/v1/me", s.handleUpdateMe)
	mux.HandleFunc("PUT /api/v1/me/avatar", s.handlePutAvatar)
	mux.HandleFunc("DELETE /api/v1/me/avatar", s.handleDeleteAvatar)
	mux.HandleFunc("GET /api/v1/users/{id}/avatar", s.handleUserAvatar)
	mux.HandleFunc("GET /api/v1/users/{id}/profile", s.handleUserPublicProfile)
	mux.HandleFunc("GET /api/v1/games", s.handleListGames)
	mux.HandleFunc("POST /api/v1/games/{id}/play", s.handlePlay)
	mux.HandleFunc("POST /api/v1/games/blackjack/deal", s.handleBlackjackDeal)
	mux.HandleFunc("POST /api/v1/hands/{id}/action", s.handleHandAction)
	mux.HandleFunc("GET /api/v1/hands/active", s.handleActiveHand)
	mux.HandleFunc("GET /api/v1/bets", s.handleListBets)
	mux.HandleFunc("GET /api/v1/fair/current", s.handleFairCurrent)
	mux.HandleFunc("POST /api/v1/fair/rotate", s.handleFairRotate)
	mux.HandleFunc("GET /api/v1/admin/users", s.handleAdminListUsers)
	mux.HandleFunc("POST /api/v1/admin/users/{id}/ban", s.handleBan)
	mux.HandleFunc("POST /api/v1/admin/users/{id}/adjust", s.handleAdminAdjust)
	mux.HandleFunc("GET /api/v1/admin/audit", s.handleAdminAudit)
	mux.HandleFunc("POST /api/v1/me/self-exclude", s.handleSelfExclude)
	mux.HandleFunc("POST /api/v1/me/deposit", s.handleDeposit)
	mux.HandleFunc("POST /api/v1/themes", s.handleCreateTheme)
	mux.HandleFunc("GET /api/v1/themes", s.handleListThemes)
	mux.HandleFunc("GET /api/v1/lobby", s.handleLobby)
	mux.HandleFunc("GET /api/v1/rooms/{slug}", s.handleRoomDetail)
	if s.hub != nil {
		mux.HandleFunc("GET /api/v1/ws", s.hub.ServeHTTP)
	}
	return s.withLogging(s.withRecover(mux))
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiError{Code: code, Message: message})
}

// handleHealthz reports process liveness and database reachability. The gate
// for phase 1: 200 only when the migrated database answers a query.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()

	if err := s.pool.Ping(ctx); err != nil {
		s.logger.Error("healthz: database unreachable", "err", err)
		writeError(w, http.StatusServiceUnavailable, "db_unavailable", "database unreachable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":   "ok",
		"database": "up",
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Hijack forwards the connection takeover so WebSocket upgrades survive the
// logging middleware.
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return h.Hijack()
}

// Flush forwards streaming flushes.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := s.clock.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.logger.Info("http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", s.clock.Now().Sub(start).Milliseconds(),
		)
	})
}

func (s *Server) withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil && !errors.Is(r.Context().Err(), context.Canceled) {
				s.logger.Error("panic recovered", "panic", rec, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
