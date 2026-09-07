package ws

import (
	"context"
	"log/slog"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/bus"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RelayProfileNotifications LISTENs on the profile_events channel and fans
// each notification into this process's hub, and on social_events (big-win
// announcements from the api node's instant-game settlement path) onto the
// wins bus topic. The api node publishes profile changes both on its
// in-process bus (for sockets it owns) and via pg_notify (for this process,
// which owns the player sockets) — the pre-Redis stand-in for cross-process
// fan-out. Reconnects with backoff on connection loss; exits when the
// context is cancelled.
func RelayProfileNotifications(ctx context.Context, pool *pgxpool.Pool, h *Hub, log *slog.Logger) {
	for {
		if ctx.Err() != nil {
			return
		}
		conn, err := pool.Acquire(ctx)
		if err != nil {
			log.Warn("profile relay acquire", "err", err)
			sleepCtx(ctx, 5*time.Second)
			continue
		}
		if _, err := conn.Exec(ctx, "LISTEN profile_events"); err != nil {
			conn.Release()
			log.Warn("profile relay listen", "err", err)
			sleepCtx(ctx, 5*time.Second)
			continue
		}
		if _, err := conn.Exec(ctx, "LISTEN social_events"); err != nil {
			conn.Release()
			log.Warn("social relay listen", "err", err)
			sleepCtx(ctx, 5*time.Second)
			continue
		}
		log.Info("profile relay listening")
		for {
			n, err := conn.Conn().WaitForNotification(ctx)
			if err != nil {
				log.Warn("profile relay wait", "err", err)
				conn.Release()
				sleepCtx(ctx, 2*time.Second)
				break
			}
			switch n.Channel {
			case "profile_events":
				h.BroadcastAll(Message{Type: "profile_updated", Payload: []byte(n.Payload)})
			case "social_events":
				// Replay onto the in-process bus so the hub's wins handler
				// announces it like any locally settled big win.
				h.bus.Publish(bus.Event{Topic: TopicWins, Payload: []byte(n.Payload)})
			}
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
