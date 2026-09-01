// Package httpapi contains the HTTP router, handlers, and middleware.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/auth"
	"github.com/ai-doodoo-slots/services/backend/internal/admin"
	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
	"github.com/ai-doodoo-slots/services/backend/internal/game/slots"
	"github.com/ai-doodoo-slots/services/backend/internal/play"
	"github.com/ai-doodoo-slots/services/backend/internal/theme"
	"github.com/ai-doodoo-slots/services/backend/internal/wallet"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Server holds the dependencies shared by all handlers.
type Server struct {
	pool         *pgxpool.Pool
	auth         *auth.Service
	accounts     *auth.Accounts
	wallet       *wallet.Wallet
	fair         *fair.Service
	registry     *game.Registry
	play         *play.Service
	playLimiter  *rateLimiter
	authLimiter  *rateLimiter
	themes       *theme.Service
	admin        *admin.Service
	clock        clock.Clock
	logger       *slog.Logger
	cookieSecure bool
}

// Option customizes the server at construction.
type Option func(*Server)

// WithThemeService attaches theme generation. Without it the theme
// endpoints report unavailability.
func WithThemeService(ts *theme.Service) Option {
	return func(s *Server) { s.themes = ts }
}

// NewServer constructs the HTTP server with its dependency set.
func NewServer(pool *pgxpool.Pool, clk clock.Clock, logger *slog.Logger, cookieSecure bool, opts ...Option) *Server {
	registry := game.NewRegistry()
	registry.Register(slots.New())
	var adminEmails []string
	if v := os.Getenv("ADMIN_EMAILS"); v != "" {
		for _, e := range strings.Split(v, ",") {
			if e = strings.TrimSpace(e); e != "" {
				adminEmails = append(adminEmails, e)
		}
	}
}
	s := &Server{
		pool:         pool,
		auth:         auth.NewService(pool, clk, logger),
		accounts:     auth.NewAccounts(pool, clk, logger, adminEmails...),
		wallet:       wallet.New(pool),
		fair:         fair.NewService(pool),
		registry:     registry,
		play:         play.NewService(pool, registry),
		playLimiter:  newRateLimiter(clk, playWindow, playMax),
		authLimiter:  newRateLimiter(clk, time.Minute, 30),
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
	mux.HandleFunc("POST /api/v1/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	mux.HandleFunc("POST /api/v1/auth/verify", s.handleVerifyEmail)
	mux.HandleFunc("POST /api/v1/auth/forgot", s.handleForgotPassword)
	mux.HandleFunc("POST /api/v1/auth/reset", s.handleResetPassword)
	mux.HandleFunc("GET /api/v1/auth/sessions", s.handleListSessions)
	mux.HandleFunc("DELETE /api/v1/auth/sessions/{id}", s.handleRevokeSession)
	mux.HandleFunc("GET /api/v1/me", s.handleMe)
	mux.HandleFunc("GET /api/v1/games", s.handleListGames)
	mux.HandleFunc("POST /api/v1/games/{id}/play", s.handlePlay)
	mux.HandleFunc("GET /api/v1/bets", s.handleListBets)
	mux.HandleFunc("GET /api/v1/fair/current", s.handleFairCurrent)
	mux.HandleFunc("POST /api/v1/fair/rotate", s.handleFairRotate)
	mux.HandleFunc("POST /api/v1/admin/users/{id}/ban", s.handleBan)
	mux.HandleFunc("POST /api/v1/admin/users/{id}/adjust", s.handleAdminAdjust)
	mux.HandleFunc("GET /api/v1/admin/audit", s.handleAdminAudit)
	mux.HandleFunc("POST /api/v1/me/self-exclude", s.handleSelfExclude)
	mux.HandleFunc("POST /api/v1/themes", s.handleCreateTheme)
	mux.HandleFunc("GET /api/v1/themes", s.handleListThemes)
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
