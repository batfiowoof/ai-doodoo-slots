-- Personalization: catalog metadata, the launch-event sink, the lobby
-- preference, and the affinity aggregates behind the recommendation scorer.

-- name: ListGameMetadata :many
SELECT game_id, category, collection, tags, is_new_until, blurb
FROM game_metadata
ORDER BY game_id;

-- name: InsertPlayerEvent :exec
INSERT INTO player_events (user_id, session_id, event_type, game_id, context)
VALUES ($1, $2, $3, $4, $5);

-- name: UserGameAffinity :many
SELECT game_id,
       COUNT(*)::bigint AS bet_count,
       COALESCE(SUM(bet_credits), 0)::bigint AS wagered,
       COALESCE(SUM(payout_credits - bet_credits), 0)::bigint AS net,
       MAX(created_at) AS last_played
FROM bets
WHERE user_id = $1
  AND created_at >= now() - (@span::text)::interval
GROUP BY game_id;

-- Launches carry signal for players (guests especially) who look around
-- without settling a bet.
-- name: UserRecentLaunches :many
SELECT game_id, MAX(created_at) AS last_at
FROM player_events
WHERE user_id = $1
  AND event_type = 'launch'
  AND created_at >= now() - (@span::text)::interval
GROUP BY game_id;

-- Stake pattern for the safer-play suppression gate. CASE, not FILTER:
-- the sqlc offline engine analyzes it reliably.
-- name: UserStakeStats :one
SELECT COALESCE(AVG(CASE WHEN created_at >= now() - interval '1 hour' THEN bet_credits END), 0)::bigint AS avg_bet_1h,
       COALESCE(AVG(CASE WHEN created_at >= now() - interval '7 days' THEN bet_credits END), 0)::bigint AS avg_bet_7d,
       COALESCE(SUM(CASE WHEN created_at >= now() - interval '24 hours' THEN payout_credits - bet_credits ELSE 0 END), 0)::bigint AS net_24h
FROM bets
WHERE user_id = $1;

-- name: TrendingLaunches :many
SELECT game_id, COUNT(*)::bigint AS hits
FROM player_events
WHERE event_type = 'launch'
  AND created_at >= now() - interval '24 hours'
GROUP BY game_id;

-- name: TrendingBets :many
SELECT game_id, COUNT(*)::bigint AS hits
FROM bets
WHERE created_at >= now() - interval '24 hours'
GROUP BY game_id;

-- name: GetUserPrefs :one
SELECT personalize_enabled
FROM user_prefs
WHERE user_id = $1;

-- name: UpsertUserPrefs :one
INSERT INTO user_prefs (user_id, personalize_enabled, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (user_id) DO UPDATE
SET personalize_enabled = EXCLUDED.personalize_enabled,
    updated_at = now()
RETURNING personalize_enabled;
