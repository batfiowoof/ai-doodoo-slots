-- Leaderboard aggregates over settled bets. @span is a Postgres interval
-- literal as text ('24 hours', '7 days', '1000 years' for all-time).
-- sqlc's offline engine cannot analyze CTEs or FROM-subqueries, so each
-- metric is one unbounded aggregate; the handler slices the top 20 and
-- finds the caller's rank from the same rows (the player base is small).

-- name: LeaderboardBiggestWin :many
SELECT b.user_id, u.display_name, u.avatar_preset, u.avatar_version,
       MAX(b.payout_credits - b.bet_credits)::bigint AS value
FROM bets b
JOIN users u ON u.id = b.user_id
WHERE u.status = 'active'
  AND b.created_at >= now() - (@span::text)::interval
GROUP BY b.user_id, u.display_name, u.avatar_preset, u.avatar_version
ORDER BY value DESC, b.user_id ASC;

-- name: LeaderboardNetProfit :many
SELECT b.user_id, u.display_name, u.avatar_preset, u.avatar_version,
       SUM(payout_credits - bet_credits)::bigint AS value
FROM bets b
JOIN users u ON u.id = b.user_id
WHERE u.status = 'active'
  AND b.created_at >= now() - (@span::text)::interval
GROUP BY b.user_id, u.display_name, u.avatar_preset, u.avatar_version
ORDER BY value DESC, b.user_id ASC;

-- name: LeaderboardWagered :many
SELECT b.user_id, u.display_name, u.avatar_preset, u.avatar_version,
       SUM(b.bet_credits)::bigint AS value
FROM bets b
JOIN users u ON u.id = b.user_id
WHERE u.status = 'active'
  AND b.created_at >= now() - (@span::text)::interval
GROUP BY b.user_id, u.display_name, u.avatar_preset, u.avatar_version
ORDER BY value DESC, b.user_id ASC;

-- name: GetUserBiggestWin :one
-- Player-card stat: best single-bet net result, all time.
SELECT COALESCE(MAX(payout_credits - bet_credits), 0)::bigint AS value
FROM bets
WHERE user_id = $1;
