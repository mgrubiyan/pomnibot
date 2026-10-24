-- name: CreateFact :one
INSERT INTO facts (id, set_id, name)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetFactsBySetID :many
SELECT f.id, f.set_id, f.name
FROM facts f
WHERE f.set_id = $1
ORDER BY f.id ASC;

-- name: GetSetPlan :many
SELECT
    f.name AS fact_name,
    COALESCE(ufp.next_review_at, CURRENT_TIMESTAMP) AS date
FROM facts f
LEFT JOIN user_fact_progress ufp ON ufp.fact_id = f.id AND ufp.user_id = $2
WHERE f.set_id = $1
  AND EXISTS (
      SELECT 1 FROM sets s
      WHERE s.id = $1
        AND (s.author_id = $2 OR EXISTS (SELECT 1 FROM user_sets us WHERE us.set_id = s.id AND us.user_id = $2))
  )
ORDER BY COALESCE(ufp.next_review_at, CURRENT_TIMESTAMP) ASC;

-- name: InitUserFactProgress :exec
INSERT INTO user_fact_progress (user_id, fact_id, step, stability, difficulty, next_review_at)
SELECT $1, f.id, 0, 0.0, 0.0, CURRENT_TIMESTAMP
FROM facts f
WHERE f.set_id = $2
ON CONFLICT (user_id, fact_id) DO NOTHING;

-- name: GetUserFactProgress :one
SELECT * FROM user_fact_progress
WHERE user_id = $1 AND fact_id = $2;

-- name: IsFactAuthor :one
SELECT EXISTS(
    SELECT 1 FROM facts f
    JOIN sets s ON f.set_id = s.id
    WHERE f.id = $1 AND s.author_id = $2
) AS is_author;

-- name: FactExists :one
SELECT EXISTS(
    SELECT 1 FROM facts WHERE id = $1
) AS exists;

-- name: UpdateFact :one
UPDATE facts
SET name = $2
WHERE id = $1
RETURNING *;

-- name: DeleteFact :execrows
DELETE FROM facts
WHERE id = $1;
