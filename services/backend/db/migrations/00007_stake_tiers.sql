-- +goose Up
-- Stake tiers: three cabinets, one curve. Min/max bets are enforced
-- server-side by the room's runner.
INSERT INTO rooms (game_id, slug, name, min_bet, max_bet, capacity, is_active)
VALUES ('crash', 'crash-2', 'Crash — High Floor', 25, 2500, 150, true),
       ('crash', 'crash-3', 'Crash — Whale Room', 100, 10000, 80, true)
ON CONFLICT (slug) DO NOTHING;

-- +goose Down
DELETE FROM rooms WHERE slug IN ('crash-2', 'crash-3');
