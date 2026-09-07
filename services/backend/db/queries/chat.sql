-- name: InsertChatMessage :one
INSERT INTO chat_messages (user_id, kind, body)
VALUES ($1, $2, $3)
RETURNING id, user_id, kind, body, deleted, created_at;

-- name: ListRecentChatMessages :many
-- Newest-first slice; the client renders bottom-up. System lines carry the
-- acting user (winner, rainmaker, tipper) so the join fills their identity.
SELECT m.id, m.user_id, m.kind, m.body, m.created_at,
       u.display_name, u.avatar_preset, u.avatar_version, u.role
FROM chat_messages m
JOIN users u ON u.id = m.user_id
WHERE m.deleted = false
ORDER BY m.id DESC
LIMIT $1;

-- name: SoftDeleteChatMessage :execrows
UPDATE chat_messages SET deleted = true
WHERE id = $1 AND deleted = false;

-- name: GetActiveChatMute :one
SELECT user_id, muted_until, reason
FROM chat_mutes
WHERE user_id = $1 AND muted_until > now();

-- name: UpsertChatMute :exec
INSERT INTO chat_mutes (user_id, muted_until, reason, muted_by)
VALUES ($1, now() + make_interval(mins => $2::int), $3, $4)
ON CONFLICT (user_id) DO UPDATE
SET muted_until = EXCLUDED.muted_until,
    reason      = EXCLUDED.reason,
    muted_by    = EXCLUDED.muted_by,
    created_at  = now();
