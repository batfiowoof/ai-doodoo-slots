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
	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
	"github.com/ai-doodoo-slots/services/backend/internal/game/crash"
	"github.com/ai-doodoo-slots/services/backend/internal/httpapi"
	"github.com/ai-doodoo-slots/services/backend/internal/round"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// roundRegistry maps game IDs to shared-round engines. Adding a game:
// implement game.RoundGame, register here, seed a room.
var roundRegistry = map[string]game.RoundGame{
	crash.GameID: crash.New(),
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

	// The gameserver owns round loops (crash) and the realtime surface:
	// hub, rooms, and lobby presence ride the in-process bus.
	api := httpapi.NewServer(pool, clock.Real{}, logger,
		envOr("COOKIE_SECURE", "false") == "true",
		httpapi.WithHub(),
	)
	go api.Run(ctx)

	// One runner per active round-game room; each runner is the single
	// writer for its room's rounds.
	crashChain := fair.NewChainService(pool)
	persist := round.NewPersister(pool)
	rooms, err := store.New(pool).ListActiveRooms(ctx)
	if err != nil {
		logger.Error("list rooms", "err", err)
		os.Exit(1)
	}
	runners := make(map[string]*round.Runner, len(rooms))
	for _, room := range rooms {
		g, ok := roundRegistry[room.GameID]
		if !ok {
			continue // round engine not shipped yet
		}
		runner := round.NewRunner(room.Slug, room.ID, g, crashChain, persist,
			api.Bus(), clock.Real{}, round.Config{
				BettingOpen: 7 * time.Second,
				Locked:      1 * time.Second,
				Settled:     4 * time.Second,
				Tick:        100 * time.Millisecond,
				MaxRunning:  90 * time.Second,
				Curve:       crash.MultiplierAt,
				RunningFor:  crash.RunningFor,
			}, pool, logger)
		go runner.Run(ctx)
		runners[room.Slug] = runner
		// The intake is the authorized money path for socket bet messages;
		// the hub relays place_bet/cash_out through it.
		intake := round.NewIntake(runner, persist, clock.Real{}, logger)
		api.Hub().SetBetHandler(intake)
		logger.Info("round runner started", "room", room.Slug, "game", room.GameID)
	}
	api.SetRoomLive(func(slug string) (map[string]any, bool) {
		r, ok := runners[slug]
		if !ok || r == nil {
			return nil, false
		}
		state := r.LiveState()
		return state, state != nil
	})

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
