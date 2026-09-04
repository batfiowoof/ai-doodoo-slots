-- +goose Up
-- Player profiles: display-name editing plus avatars. An avatar is either a
-- curated preset sprite (guests included) or an uploaded image downscaled
-- server-side to 64x64 pixels and stored in Postgres. avatar_version bumps
-- on every change so the public avatar URL cache-busts.
ALTER TABLE users
    ADD COLUMN avatar_preset TEXT,
    ADD COLUMN avatar_version BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN display_name_updated_at TIMESTAMPTZ;

CREATE TABLE user_avatars (
    user_id      BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    content_type TEXT NOT NULL,
    bytes        BYTEA NOT NULL,
    sha256       TEXT NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS user_avatars;
ALTER TABLE users
    DROP COLUMN IF EXISTS display_name_updated_at,
    DROP COLUMN IF EXISTS avatar_version,
    DROP COLUMN IF EXISTS avatar_preset;
