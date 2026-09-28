-- name: UpsertUser :one
INSERT INTO users (id, name, last_active_at)
VALUES ($1, $2, CURRENT_TIMESTAMP)
ON CONFLICT (id) DO UPDATE
SET name = EXCLUDED.name,
    last_active_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: CountUserActiveDays :one
SELECT COUNT(DISTINCT DATE(answered_at))::INT AS active_days
FROM answer_results
WHERE user_id = $1 AND answered_at >= NOW() - INTERVAL '7 days';
