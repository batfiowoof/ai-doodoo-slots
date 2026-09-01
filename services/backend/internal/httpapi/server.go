// Package httpapi contains the HTTP router, handlers, and middleware.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/auth"
	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/wallet"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Server holds the dependencies shared by all handlers.
type Server struct {
	pool         *pgxpool.Pool
	auth         *auth.Service
	wallet       *wallet.Wallet
	fair         *fair.Service
	clock        clock.Clock
	logger       *slog.Logger
	cookieSecure bool
}

// NewServer constructs the HTTP server with its dependency set.
func NewServer(pool *pgxpool.Pool, clk clock.Clock, logger *slog.Logger, cookieSecure bool) *Server {
	return &Server{
		pool:         pool,
		auth:         auth.NewService(pool, clk, logger),
		wallet:       wallet.New(pool),
		fair:         fair.NewService(pool),
		clock:        clk,
		logger:       logger,
		cookieSecure: cookieSecure,
	}
}

// Handler builds the full middleware-wrapped route table.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("POST /api/v1/auth/guest", s.handleAuthGuest)
	mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/v1/me", s.handleMe)
	mux.HandleFunc("GET /api/v1/fair/current", s.handleFairCurrent)
	mux.HandleFunc("POST /api/v1/fair/rotate", s.handleFairRotate)
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
