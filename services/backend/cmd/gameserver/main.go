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
	"github.com/ai-doodoo-slots/services/backend/internal/game/poker"
	"github.com/ai-doodoo-slots/services/backend/internal/httpapi"
	"github.com/ai-doodoo-slots/services/backend/internal/round"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/ai-doodoo-slots/services/backend/internal/table"
	"github.com/ai-doodoo-slots/services/backend/internal/ws"
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

	// Profile changes are published by the api node over Postgres
	// NOTIFY/LISTEN; relay them onto our sockets.
	go ws.RelayProfileNotifications(ctx, pool, api.Hub(), logger)

	// One runner per active room; each runner is the single writer for its
	// room's rounds. Round games (crash) use the phase-loop runner; table
	// games (poker) use the table runner and register their intake as the
	// room's game-action handler on the hub.
	crashChain := fair.NewChainService(pool)
	persist := round.NewPersister(pool)
	tablePersist := table.NewPersister(pool)
	rooms, err := store.New(pool).ListActiveRooms(ctx)
	if err != nil {
		logger.Error("list rooms", "err", err)
		os.Exit(1)
	}
	runners := make(map[string]*round.Runner, len(rooms))
	tableRunners := make(map[string]*table.Runner, len(rooms))
	for _, room := range rooms {
		if g, ok := roundRegistry[room.GameID]; ok {
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
			runner.SetLimits(room.MinBet, room.MaxBet)
			// The intake is the authorized money path for socket bet messages;
			// the hub relays this room's place_bet/cash_out through it
			// (per-room limits, not a global handler).
			intake := round.NewIntake(runner, persist, clock.Real{}, api.Bus(), logger)
			api.Hub().SetRoomHandler(room.Slug, intake)
			logger.Info("round runner started", "room", room.Slug, "game", room.GameID)
			continue
		}
		if room.GameID == poker.GameID {
			tr := table.NewRunner(room.Slug, room.ID, int(room.Capacity), crashChain,
				tablePersist, api.Bus(), clock.Real{}, table.DefaultConfig(), pool, logger)
			tr.SetLimits(room.MinBet, room.MaxBet)
			go tr.Run(ctx)
			tableRunners[room.Slug] = tr
			// The table intake handles buy_in/act/leave/state game_action
			// messages routed by the hub for this room.
			api.Hub().SetRoomHandler(room.Slug, table.NewIntake(tr, logger))
			logger.Info("table runner started", "room", room.Slug, "game", room.GameID)
		}
	}
	// Lobby summaries carry coarse per-room state — never round ticks.
	api.Hub().SetRoomInfo(func() map[string]map[string]any {
		out := make(map[string]map[string]any, len(runners)+len(tableRunners))
		for slug, r := range runners {
			out[slug] = r.LiveState()
		}
		for slug, tr := range tableRunners {
			out[slug] = tr.LiveState()
		}
		return out
	})
	api.SetRoomLive(func(slug string) (map[string]any, bool) {
		if r, ok := runners[slug]; ok && r != nil {
			state := r.LiveState()
			return state, state != nil
		}
		if tr, ok := tableRunners[slug]; ok && tr != nil {
			return tr.LiveState(), true
		}
		return nil, false
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
