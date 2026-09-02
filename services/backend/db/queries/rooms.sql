-- name: ListActiveRooms :many
SELECT id, game_id, slug, name, min_bet, max_bet, capacity, is_active
FROM rooms
WHERE is_active
ORDER BY id;

-- name: GetRoomBySlug :one
SELECT id, game_id, slug, name, min_bet, max_bet, capacity, is_active
FROM rooms
WHERE slug = $1 AND is_active;
