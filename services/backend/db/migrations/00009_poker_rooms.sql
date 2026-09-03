-- +goose Up
-- Poker tables: min_bet is the big blind, max_bet the maximum buy-in
-- (stack cap). Stakes mirror the crash tiers.
INSERT INTO rooms (game_id, slug, name, min_bet, max_bet, capacity, is_active)
VALUES ('holdem', 'holdem-1', 'Hold''em — Ground Floor', 10, 2000, 6, true),
       ('holdem', 'holdem-2', 'Hold''em — High Floor', 50, 10000, 6, true),
       ('holdem', 'holdem-3', 'Hold''em — Whale Room', 200, 50000, 9, true)
ON CONFLICT (slug) DO NOTHING;

-- +goose Down
DELETE FROM rooms WHERE slug IN ('holdem-1', 'holdem-2', 'holdem-3');
