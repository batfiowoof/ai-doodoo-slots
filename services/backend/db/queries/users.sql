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
