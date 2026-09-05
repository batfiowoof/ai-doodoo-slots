-- +goose Up
-- Roulette rooms: the European wheel on the same three stake tiers as the
-- crash floor, so every table has its crowd.
INSERT INTO rooms (game_id, slug, name, min_bet, max_bet, capacity, is_active)
VALUES
  ('roulette', 'roulette-1', 'Roulette — Ground Floor', 5, 1000, 100, true),
  ('roulette', 'roulette-2', 'Roulette — High Floor', 25, 2500, 100, true),
  ('roulette', 'roulette-3', 'Roulette — Whale Room', 100, 10000, 100, true)
ON CONFLICT (slug) DO NOTHING;

-- +goose Down
DELETE FROM rooms WHERE slug IN ('roulette-1', 'roulette-2', 'roulette-3');
