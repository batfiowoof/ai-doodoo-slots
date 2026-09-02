-- +goose Up
-- Static rooms are long-lived and always running; their round loops start
-- with the gameserver. One crash room is plenty for phase 12-14.
INSERT INTO rooms (game_id, slug, name, min_bet, max_bet, capacity, is_active)
VALUES ('crash', 'crash-1', 'Crash — Ground Floor', 5, 1000, 200, true)
ON CONFLICT (slug) DO NOTHING;

-- +goose Down
DELETE FROM rooms WHERE slug = 'crash-1';
