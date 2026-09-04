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

	"github.com/ai-doodoo-slots/services/backend/internal/auth"
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

	// Keycloak OIDC: when an issuer is configured, registered users
	// authenticate with Keycloak access tokens and the BFF forwards them as
	// Bearer credentials. Without it the API runs guest-only.
	if issuer := os.Getenv("KEYCLOAK_ISSUER"); issuer != "" {
		cfg := auth.OIDCConfig{
			Issuer:        issuer,
			ClientID:      envOr("KEYCLOAK_CLIENT_ID", "web"),
			AuthURL:       os.Getenv("KEYCLOAK_AUTH_URL"),
			TokenURL:      os.Getenv("KEYCLOAK_TOKEN_URL"),
			JWKSURL:       os.Getenv("KEYCLOAK_JWKS_URL"),
			EndSessionURL: os.Getenv("KEYCLOAK_END_SESSION_URL"),
		}
		verifier := auth.NewOIDCVerifier(cfg, clock.Real{}, logger)
		opts = append(opts, httpapi.WithOIDC(verifier))
		logger.Info("keycloak auth enabled", "issuer", issuer)

		// Profile write-back: Keycloak attributes mirror the local profile
		// so tokens and future OIDC consumers stay coherent. Optional. The
		// issuer override lets the container reach the token endpoint over
		// the compose network (the public issuer URL is browser-facing).
		if adminClient := auth.NewAdminClient(
			envOr("KEYCLOAK_ADMIN_ISSUER", issuer),
			os.Getenv("KEYCLOAK_ADMIN_URL"),
			envOr("KEYCLOAK_ADMIN_CLIENT_ID", "retro-api"),
			os.Getenv("KEYCLOAK_ADMIN_CLIENT_SECRET"),
			clock.Real{}, logger,
		); adminClient != nil {
			opts = append(opts, httpapi.WithKeycloakAdmin(adminClient))
			logger.Info("keycloak profile write-back enabled")
		} else {
			logger.Info("keycloak admin not configured; profile write-back disabled")
		}
	} else {
		logger.Info("KEYCLOAK_ISSUER not set; guest-only mode")
	}

	// The realtime surface rides the same process in single-node dev; the
	// gameserver is the multi-process home for round loops and sockets.
	opts = append(opts, httpapi.WithHub())

	api := httpapi.NewServer(pool, clock.Real{}, logger,
		envOr("COOKIE_SECURE", "false") == "true",
		opts...,
	)
	go api.Run(ctx)

	srv := &http.Server{
		Addr:              addr,
		Handler:           api.Handler(),
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
