-- +goose Up
-- Social layer: persisted chat (with soft-delete moderation) and mutes.
-- Emotes and presence are ephemeral and never touch the database.

CREATE TABLE chat_messages (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id),
    kind       TEXT NOT NULL DEFAULT 'chat' CHECK (kind IN ('chat', 'system')),
    body       TEXT NOT NULL CHECK (length(body) BETWEEN 1 AND 256),
    deleted    BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- History loads the newest slice; the index also serves the leaderboard-free
-- chat purge path if one is ever added.
CREATE INDEX idx_chat_messages_recent ON chat_messages (created_at DESC) WHERE deleted = false;

CREATE TABLE chat_mutes (
    user_id      BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    muted_until  TIMESTAMPTZ NOT NULL,
    reason       TEXT NOT NULL DEFAULT '',
    muted_by     BIGINT REFERENCES users(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE chat_mutes;
DROP TABLE chat_messages;
