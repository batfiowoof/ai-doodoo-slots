-- +goose Up
-- Deduplication: regenerating an identical prompt returns the stored theme
-- without spending provider tokens.
ALTER TABLE themes ADD COLUMN prompt_hash TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_themes_user_prompt ON themes (user_id, prompt_hash);

-- +goose Down
DROP INDEX IF EXISTS idx_themes_user_prompt;
ALTER TABLE themes DROP COLUMN prompt_hash;
