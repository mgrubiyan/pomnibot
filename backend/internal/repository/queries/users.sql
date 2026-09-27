-- name: UpsertUser :one
INSERT INTO users (id, username, first_name, last_name, updated_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (id) DO UPDATE
SET username = EXCLUDED.username,
    first_name = EXCLUDED.first_name,
    last_name = EXCLUDED.last_name,
    updated_at = now()
RETURNING id, username, first_name, last_name, created_at, updated_at;

-- name: GetUserByID :one
SELECT id, username, first_name, last_name, created_at, updated_at
FROM users
WHERE id = $1;
