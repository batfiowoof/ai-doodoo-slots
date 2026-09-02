// Command gameserver is the stateful process that owns round loops. Round
// state lives here in memory; API nodes fan its events out to their own
// connected sockets. Round loops arrive in phase 13 (crash engine); this
// binary already carries the realtime surface (phase 12).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	addr := envOr("GAMESERVER_ADDR", ":8082")
	dsn := envOr("DATABASE_URL", "postgres://retro:retro@localhost:55432/retrocasino?sslmode=disable")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		logger.Error("connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	// The gameserver owns round loops (phase 13) and the realtime surface:
	// hub, rooms, and lobby presence ride the in-process bus.
	api := httpapi.NewServer(pool, clock.Real{}, logger,
		envOr("COOKIE_SECURE", "false") == "true",
		httpapi.WithHub(),
	)
	go api.Run(ctx)

	srv := &http.Server{
		Addr:              addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("gameserver listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "err", err)
	}
}
