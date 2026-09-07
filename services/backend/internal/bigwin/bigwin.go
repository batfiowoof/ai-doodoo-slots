// Package bigwin decides which settlements are lobby-wide news and pushes
// them onto the wins bus topic. The threshold is a multiplier of the stake
// (payout ≥ threshold×bet); BIG_WIN_MULTIPLIER tunes it (default 15×).
package bigwin

import (
	"context"
	"encoding/json"
	"os"
	"strconv"

	"github.com/ai-doodoo-slots/services/backend/internal/bus"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Topic is the bus topic the hub subscribes to for announcements.
const Topic = "wins"

var threshold = func() float64 {
	if v := os.Getenv("BIG_WIN_MULTIPLIER"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	return 15
}()

// Threshold reports the configured payout multiple that qualifies a win.
func Threshold() float64 { return threshold }

// Publish checks a settled bet against the threshold and, when it
// qualifies, pushes the announcement onto the bus for the hub to fan out.
// multiplier is payout/bet (or the game's own settlement multiplier). Used
// by the round runner, which lives in the hub's process.
func Publish(b bus.Bus, userID int64, gameID string, betCredits, payoutCredits int64, multiplier float64) {
	if b == nil || betCredits <= 0 || payoutCredits <= 0 {
		return
	}
	if multiplier < threshold {
		return
	}
	raw, err := json.Marshal(map[string]any{
		"userId":        userID,
		"gameId":        gameID,
		"betCredits":    betCredits,
		"payoutCredits": payoutCredits,
		"multiplier":    multiplier,
	})
	if err != nil {
		return
	}
	b.Publish(bus.Event{Topic: Topic, Payload: raw})
}

// Notify is the instant-game path (slots/dice/plinko/mines), which runs in
// the api process that owns no sockets: announce via pg_notify so the
// gameserver's relay replays it onto its wins bus topic.
func Notify(ctx context.Context, pool *pgxpool.Pool, userID int64, gameID string, betCredits, payoutCredits int64, multiplier float64) {
	if pool == nil || betCredits <= 0 || payoutCredits <= 0 {
		return
	}
	if multiplier < threshold {
		return
	}
	raw, err := json.Marshal(map[string]any{
		"userId":        userID,
		"gameId":        gameID,
		"betCredits":    betCredits,
		"payoutCredits": payoutCredits,
		"multiplier":    multiplier,
	})
	if err != nil {
		return
	}
	_, _ = pool.Exec(ctx, "SELECT pg_notify('social_events', $1)", string(raw))
}
