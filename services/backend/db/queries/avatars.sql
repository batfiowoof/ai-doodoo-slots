-- name: UpsertUserAvatar :exec
INSERT INTO user_avatars (user_id, content_type, bytes, sha256)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id) DO UPDATE
SET content_type = EXCLUDED.content_type,
    bytes        = EXCLUDED.bytes,
    sha256       = EXCLUDED.sha256,
    updated_at   = now();

-- name: GetUserAvatar :one
SELECT content_type, bytes
FROM user_avatars
WHERE user_id = $1;

-- name: DeleteUserAvatar :exec
DELETE FROM user_avatars WHERE user_id = $1;
