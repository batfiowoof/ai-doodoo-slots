-- name: CreateEmailToken :one
INSERT INTO email_tokens (user_id, kind, token_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING id, user_id, kind, token_hash, expires_at, used_at;

-- name: GetValidEmailToken :one
SELECT id, user_id FROM email_tokens
WHERE token_hash = $1 AND kind = $2
  AND used_at IS NULL AND expires_at > now();

-- name: MarkEmailTokenUsed :exec
UPDATE email_tokens SET used_at = now() WHERE id = $1;
