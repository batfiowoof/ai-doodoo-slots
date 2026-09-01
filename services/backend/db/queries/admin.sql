-- name: SetUserStatus :exec
UPDATE users
SET status = $2, status_until = $3
WHERE id = $1;

-- name: GetUserStatus :one
SELECT status FROM users WHERE id = $1;

-- name: SetUserRole :exec
UPDATE users SET role = $2 WHERE id = $1;

-- name: InsertAuditEntry :one
INSERT INTO audit_log (actor_user_id, action, target_type, target_id, metadata)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: ListAuditEntries :many
SELECT id, actor_user_id, action, target_type, target_id, metadata, created_at
FROM audit_log
WHERE (sqlc.narg('cursor')::bigint IS NULL OR id < sqlc.narg('cursor'))
ORDER BY id DESC
LIMIT sqlc.arg('lim');
