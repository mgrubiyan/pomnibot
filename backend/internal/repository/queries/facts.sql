-- name: UpsertTopic :one
INSERT INTO topics (name)
VALUES ($1)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: CreateFact :one
INSERT INTO facts (id, set_id, topic_id, name)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetFactsBySetID :many
SELECT f.id, f.set_id, f.name, f.created_at, COALESCE(t.name, '') AS topic_name
FROM facts f
LEFT JOIN topics t ON f.topic_id = t.id
WHERE f.set_id = $1
ORDER BY f.created_at ASC;

-- name: GetSetPlan :many
SELECT
    f.name AS fact_name,
    COALESCE(ufp.next_review_at, CURRENT_TIMESTAMP) AS date
FROM facts f
LEFT JOIN user_fact_progress ufp ON ufp.fact_id = f.id AND ufp.user_id = $2
WHERE f.set_id = $1
ORDER BY COALESCE(ufp.next_review_at, CURRENT_TIMESTAMP) ASC;

-- name: InitUserFactProgress :exec
INSERT INTO user_fact_progress (user_id, fact_id, elo, ceiling_found, next_review_at)
SELECT $1, f.id, 700, FALSE, CURRENT_TIMESTAMP
FROM facts f
WHERE f.set_id = $2
ON CONFLICT (user_id, fact_id) DO NOTHING;

-- name: GetUserFactProgress :one
SELECT * FROM user_fact_progress
WHERE user_id = $1 AND fact_id = $2;
