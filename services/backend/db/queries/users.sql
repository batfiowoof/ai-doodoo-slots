-- name: CreateUserGuest :one
INSERT INTO users (display_name, is_guest)
VALUES ($1, true)
RETURNING id, created_at, is_guest, display_name, email, password_hash,
          email_verified_at, role, status, status_until;

-- name: GetUserByID :one
SELECT id, created_at, is_guest, display_name, email, password_hash,
       email_verified_at, role, status, status_until
FROM users
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT id, created_at, is_guest, display_name, email, password_hash,
       email_verified_at, role, status, status_until
FROM users
WHERE email = $1;

-- name: CreateUserRegistered :one
INSERT INTO users (display_name, email, password_hash, is_guest)
VALUES ($1, $2, $3, false)
RETURNING id, created_at, is_guest, display_name, email, password_hash,
          email_verified_at, role, status, status_until;

-- name: UpgradeGuestUser :one
-- Guest upgrade in place: the same row keeps its wallet, bets, and seeds.
UPDATE users
SET email = $2, password_hash = $3, is_guest = false
WHERE id = $1 AND is_guest = true
RETURNING id, created_at, is_guest, display_name, email, password_hash,
          email_verified_at, role, status, status_until;

-- name: UpdateUserEmailVerified :exec
UPDATE users SET email_verified_at = now() WHERE id = $1;

-- name: UpdatePassword :exec
UPDATE users SET password_hash = $2 WHERE id = $1;
