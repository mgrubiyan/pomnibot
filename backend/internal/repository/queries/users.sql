-- name: UpsertUser :one
INSERT INTO users (id, first_name, last_name, username, is_bot, last_active_at)
VALUES ($1, $2, $3, $4, $5, CURRENT_TIMESTAMP)
ON CONFLICT (id) DO UPDATE
SET first_name = CASE WHEN EXCLUDED.first_name <> '' THEN EXCLUDED.first_name ELSE users.first_name END,
    last_name = COALESCE(EXCLUDED.last_name, users.last_name),
    username = COALESCE(EXCLUDED.username, users.username),
    is_bot = EXCLUDED.is_bot,
    last_active_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: EnsureUser :one
INSERT INTO users (id, first_name, last_name, username, is_bot, last_active_at)
VALUES ($1, '', NULL, NULL, FALSE, CURRENT_TIMESTAMP)
ON CONFLICT (id) DO UPDATE
SET last_active_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: CountUserActiveDays :one
SELECT COUNT(DISTINCT DATE(answered_at))::INT AS active_days
FROM answer_results
WHERE user_id = $1 AND answered_at >= NOW() - INTERVAL '7 days';

-- name: GetUsersWithDueFacts :many
SELECT DISTINCT u.id
FROM users u
JOIN user_sets us ON us.user_id = u.id
JOIN facts f ON f.set_id = us.set_id
LEFT JOIN user_fact_progress ufp ON ufp.fact_id = f.id AND ufp.user_id = u.id
WHERE u.is_bot = FALSE
  AND u.id NOT IN (0, 100001)
  AND (
    ufp.next_review_at IS NULL
    OR (ufp.next_review_at AT TIME ZONE sqlc.arg('tz')::text)::date <= (sqlc.arg('now')::timestamptz AT TIME ZONE sqlc.arg('tz')::text)::date
  );
