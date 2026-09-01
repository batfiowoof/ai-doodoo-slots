-- name: CreateSession :one
INSERT INTO sessions (user_id, token_hash, expires_at, ip, user_agent)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, user_id, token_hash, created_at, expires_at, last_seen_at, ip, user_agent, revoked_at;

-- name: GetActiveSessionByTokenHash :one
SELECT s.id AS session_id, s.expires_at, s.last_seen_at,
       u.id AS user_id, u.is_guest, u.display_name, u.email,
       u.email_verified_at, u.role, u.status, u.created_at AS user_created_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = $1
  AND s.revoked_at IS NULL
  AND s.expires_at > now();

-- name: TouchSession :exec
-- Sliding expiry: extend by the TTL on activity, capped by an absolute
-- lifetime measured from session creation.
UPDATE sessions
SET last_seen_at = $2,
    expires_at = LEAST($2 + interval '30 days', created_at + interval '90 days')
WHERE id = $1;

-- name: RevokeSessionByTokenHash :exec
UPDATE sessions
SET revoked_at = now()
WHERE token_hash = $1 AND revoked_at IS NULL;

-- name: RevokeSessionByID :exec
UPDATE sessions
SET revoked_at = now()
WHERE id = $1 AND revoked_at IS NULL;
