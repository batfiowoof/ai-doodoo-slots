-- name: CreateUserGuest :one
INSERT INTO users (display_name, is_guest)
VALUES ($1, true)
RETURNING id, created_at, is_guest, display_name, email, password_hash,
          email_verified_at, role, status, status_until,
          avatar_preset, avatar_version, display_name_updated_at;

-- name: GetUserByID :one
SELECT id, created_at, is_guest, display_name, email, password_hash,
       email_verified_at, role, status, status_until,
       avatar_preset, avatar_version, display_name_updated_at
FROM users
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT id, created_at, is_guest, display_name, email, password_hash,
       email_verified_at, role, status, status_until,
       avatar_preset, avatar_version, display_name_updated_at
FROM users
WHERE email = $1;

-- name: CreateUserRegistered :one
INSERT INTO users (display_name, email, password_hash, is_guest)
VALUES ($1, $2, $3, false)
RETURNING id, created_at, is_guest, display_name, email, password_hash,
          email_verified_at, role, status, status_until,
          avatar_preset, avatar_version, display_name_updated_at;

-- name: UpgradeGuestUser :one
-- Guest upgrade in place: the same row keeps its wallet, bets, and seeds.
UPDATE users
SET email = $2, password_hash = $3, is_guest = false
WHERE id = $1 AND is_guest = true
RETURNING id, created_at, is_guest, display_name, email, password_hash,
          email_verified_at, role, status, status_until,
          avatar_preset, avatar_version, display_name_updated_at;

-- name: UpdateUserEmailVerified :exec
UPDATE users SET email_verified_at = now() WHERE id = $1;

-- name: UpdatePassword :exec
UPDATE users SET password_hash = $2 WHERE id = $1;

-- name: UpdateDisplayName :one
UPDATE users
SET display_name = $2, display_name_updated_at = now()
WHERE id = $1
RETURNING id, created_at, is_guest, display_name, email, password_hash,
          email_verified_at, role, status, status_until,
          avatar_preset, avatar_version, display_name_updated_at;

-- name: DisplayNameTaken :one
-- Case-insensitive uniqueness check; ownership excluded by caller id.
SELECT count(*) > 0 AS taken
FROM users
WHERE lower(display_name) = lower($1) AND id <> $2;

-- name: SetAvatarPreset :execrows
UPDATE users
SET avatar_preset = $2, avatar_version = avatar_version + 1
WHERE id = $1;

-- name: SetUploadedAvatar :one
-- An upload clears any preset; the version bump moves the public URL.
UPDATE users
SET avatar_preset = NULL, avatar_version = avatar_version + 1
WHERE id = $1
RETURNING avatar_version;

-- name: ClearAvatar :execrows
UPDATE users
SET avatar_preset = NULL, avatar_version = avatar_version + 1
WHERE id = $1;

-- name: GetUserPublicProfile :one
SELECT id, display_name, avatar_preset, avatar_version, role, created_at
FROM users
WHERE id = $1;

-- name: AdminListUsers :many
-- Search by display name or email substring; empty term lists newest first.
SELECT id, created_at, is_guest, display_name, email, email_verified_at,
       role, status, status_until
FROM users
WHERE @term::text = ''
   OR display_name ILIKE '%' || @term::text || '%'
   OR email::text ILIKE '%' || @term::text || '%'
ORDER BY id DESC
LIMIT @max_rows;
