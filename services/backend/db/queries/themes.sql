-- name: CreateTheme :one
INSERT INTO themes (user_id, name, palette, sprites, prompt_hash)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, user_id, name, palette, sprites, prompt_hash, created_at;

-- name: ListThemesByUser :many
SELECT id, user_id, name, palette, sprites, prompt_hash, created_at
FROM themes
WHERE user_id = $1
ORDER BY id DESC
LIMIT 50;

-- name: GetThemeByPromptHash :one
SELECT id, user_id, name, palette, sprites, prompt_hash, created_at
FROM themes
WHERE user_id = $1 AND prompt_hash = $2
ORDER BY id DESC
LIMIT 1;
