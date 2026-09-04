-- name: GetOauthIdentity :one
SELECT provider, provider_user_id, user_id
FROM oauth_identities
WHERE provider = $1 AND provider_user_id = $2;

-- name: InsertOauthIdentity :exec
INSERT INTO oauth_identities (provider, provider_user_id, user_id)
VALUES ($1, $2, $3)
ON CONFLICT (provider, provider_user_id) DO NOTHING;

-- name: CreateUserFromKeycloak :one
INSERT INTO users (display_name, email, email_verified_at, is_guest)
VALUES ($1, $2, $3, false)
RETURNING id, created_at, is_guest, display_name, email, password_hash,
          email_verified_at, role, status, status_until,
          avatar_preset, avatar_version, display_name_updated_at;

-- name: UpgradeGuestForKeycloak :one
-- Guest upgrade in place: the same row keeps its wallet, bets, and seeds.
UPDATE users
SET display_name      = COALESCE(NULLIF(sqlc.arg('display_name')::text, ''), display_name),
    email             = COALESCE(sqlc.narg('email')::citext, email),
    email_verified_at = COALESCE(sqlc.narg('email_verified_at'), email_verified_at),
    is_guest          = false
WHERE id = $1 AND is_guest = true
RETURNING id, created_at, is_guest, display_name, email, password_hash,
          email_verified_at, role, status, status_until,
          avatar_preset, avatar_version, display_name_updated_at;
