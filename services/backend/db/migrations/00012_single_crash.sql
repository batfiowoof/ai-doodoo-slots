-- +goose Up
-- One crash room: the two extra stake tiers just duplicated the same curve
-- behind separate slugs. Deactivate them (history keeps its FK targets) and
-- widen crash-1 so every stake still fits.
UPDATE rooms SET is_active = false WHERE slug IN ('crash-2', 'crash-3');
UPDATE rooms SET min_bet = 5, max_bet = 10000 WHERE slug = 'crash-1';

-- +goose Down
UPDATE rooms SET min_bet = 5, max_bet = 1000 WHERE slug = 'crash-1';
UPDATE rooms SET is_active = true WHERE slug IN ('crash-2', 'crash-3');
