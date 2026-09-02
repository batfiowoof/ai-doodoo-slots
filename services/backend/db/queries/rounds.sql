-- name: CreateRound :one
INSERT INTO rounds (room_id, game_id, chain_seed_id, salt, state)
VALUES ($1, $2, $3, $4, 'betting_open')
RETURNING id;

-- name: SetRoundState :exec
UPDATE rounds SET state = $2 WHERE id = $1;

-- name: SetRoundResult :exec
UPDATE rounds SET state = 'settled', result = $2, settled_at = now() WHERE id = $1;

-- name: GetRoundByID :one
SELECT id, room_id, game_id, chain_seed_id, salt, state, result, opened_at, locked_at, settled_at
FROM rounds
WHERE id = $1;
