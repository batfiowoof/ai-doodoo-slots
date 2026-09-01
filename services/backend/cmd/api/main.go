// Command api is the stateless HTTP API. It scales horizontally and never owns
// round state.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/httpapi"
	"github.com/ai-doodoo-slots/services/backend/internal/theme"
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

	addr := envOr("API_ADDR", ":8080")
	dsn := envOr("DATABASE_URL", "postgres://retro:retro@localhost:55432/retrocasino?sslmode=disable")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		logger.Error("connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	// The OpenRouter key lives here, in the Go service — never in the web
	// bundle. Without it, theme endpoints report unavailability.
	var opts []httpapi.Option
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		var models []string
		if m := os.Getenv("OPENROUTER_MODELS"); m != "" {
			for _, id := range strings.Split(m, ",") {
				if id = strings.TrimSpace(id); id != "" {
					models = append(models, id)
				}
			}
		}
		client := theme.NewClient(key, models, clock.Real{})
		opts = append(opts, httpapi.WithThemeService(theme.NewService(pool, client, clock.Real{})))
		logger.Info("theme generation enabled")
	} else {
		logger.Info("OPENROUTER_API_KEY not set; theme generation disabled")
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           httpapi.NewServer(pool, clock.Real{}, logger, envOr("COOKIE_SECURE", "false") == "true", opts...).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("api listening", "addr", addr)
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
