-- name: CreateSet :one
INSERT INTO sets (title, author_id, share_code, warnings)
VALUES ($1, $2, $3, COALESCE(sqlc.arg(warnings)::TEXT[], '{}'::TEXT[]))
RETURNING *;

-- name: GetSetByID :one
SELECT
    s.id,
    s.title,
    s.author_id,
    u.first_name AS author_first_name,
    u.last_name AS author_last_name,
    u.username AS author_username,
    s.share_code,
    s.updated_at,
    CASE WHEN s.author_id = sqlc.arg('user_id') THEN s.warnings ELSE '{}'::TEXT[] END::TEXT[] AS warnings,
    (SELECT COUNT(*)::INT FROM cards c JOIN facts f ON c.fact_id = f.id WHERE f.set_id = s.id) AS cards_total,
    (SELECT COUNT(*)::INT
     FROM facts f
     LEFT JOIN user_fact_progress ufp ON ufp.fact_id = f.id AND ufp.user_id = sqlc.arg('user_id')
     WHERE f.set_id = s.id
       AND (ufp.next_review_at IS NULL OR ufp.next_review_at <= NOW())) AS cards_due
FROM sets s
JOIN users u ON s.author_id = u.id
WHERE s.id = $1
  AND (s.author_id = sqlc.arg('user_id') OR EXISTS (SELECT 1 FROM user_sets us WHERE us.set_id = s.id AND us.user_id = sqlc.arg('user_id')));

-- name: GetSetByShareCode :one
SELECT
    s.id,
    s.title,
    s.author_id,
    u.first_name AS author_first_name,
    u.last_name AS author_last_name,
    u.username AS author_username,
    s.share_code,
    s.updated_at,
    (SELECT COUNT(*)::INT FROM cards c JOIN facts f ON c.fact_id = f.id WHERE f.set_id = s.id) AS cards_total
FROM sets s
JOIN users u ON s.author_id = u.id
WHERE s.share_code = $1;

-- name: JoinSet :one
INSERT INTO user_sets (user_id, set_id)
VALUES ($1, $2)
ON CONFLICT (user_id, set_id) DO UPDATE SET joined_at = user_sets.joined_at
RETURNING *;

-- name: GetUserSets :many
SELECT
    s.id,
    s.title,
    s.author_id,
    u.first_name AS author_first_name,
    u.last_name AS author_last_name,
    u.username AS author_username,
    s.share_code,
    s.updated_at,
    (SELECT COUNT(*)::INT FROM cards c JOIN facts f ON c.fact_id = f.id WHERE f.set_id = s.id) AS cards_total,
    (SELECT COUNT(*)::INT
     FROM facts f
     LEFT JOIN user_fact_progress ufp ON ufp.fact_id = f.id AND ufp.user_id = $1
     WHERE f.set_id = s.id
       AND (ufp.next_review_at IS NULL OR ufp.next_review_at <= NOW())) AS cards_due
FROM user_sets us
JOIN sets s ON us.set_id = s.id
JOIN users u ON s.author_id = u.id
WHERE us.user_id = $1
ORDER BY us.joined_at DESC;

-- name: IsSetAuthor :one
SELECT EXISTS(
    SELECT 1 FROM sets WHERE id = $1 AND author_id = $2
) AS is_author;

-- name: DeleteSet :execrows
DELETE FROM sets WHERE id = $1 AND author_id = $2;

-- name: LeaveSet :execrows
DELETE FROM user_sets WHERE set_id = $1 AND user_id = $2;

-- name: CountTotalDueCardsForUser :one
SELECT COUNT(*)::INT AS due_count
FROM facts f
JOIN user_sets us ON us.set_id = f.set_id AND us.user_id = $1
LEFT JOIN user_fact_progress ufp ON ufp.fact_id = f.id AND ufp.user_id = $1
WHERE ufp.next_review_at IS NULL OR ufp.next_review_at <= NOW();

-- name: GetNextReviewDateForUser :one
SELECT MIN(ufp.next_review_at)::TIMESTAMPTZ AS next_review_at
FROM facts f
JOIN user_sets us ON us.set_id = f.set_id AND us.user_id = $1
JOIN user_fact_progress ufp ON ufp.fact_id = f.id AND ufp.user_id = $1
WHERE ufp.next_review_at > NOW();

-- name: GetSetLeaderboard :many
WITH set_facts AS (
    SELECT f.id FROM facts f WHERE f.set_id = sqlc.arg('set_id')::UUID
),
facts_count AS (
    SELECT COUNT(*)::INT AS total FROM set_facts
),
fact_cards AS (
    SELECT c.fact_id, COUNT(*)::INT AS total_cards
    FROM cards c
    JOIN set_facts sf ON sf.id = c.fact_id
    GROUP BY c.fact_id
),
participants AS (
    SELECT us.user_id FROM user_sets us WHERE us.set_id = sqlc.arg('set_id')::UUID
    UNION
    SELECT s.author_id AS user_id FROM sets s WHERE s.id = sqlc.arg('set_id')::UUID
),
progress_by_user AS (
    SELECT
        ufp.user_id,
        SUM(
            LEAST(ufp.step, GREATEST(1, COALESCE(fc.total_cards, 1)))::FLOAT / GREATEST(1, COALESCE(fc.total_cards, 1))::FLOAT
        )::FLOAT AS total_mastery
    FROM user_fact_progress ufp
    JOIN set_facts sf ON sf.id = ufp.fact_id
    LEFT JOIN fact_cards fc ON fc.fact_id = ufp.fact_id
    GROUP BY ufp.user_id
),
user_scores AS (
    SELECT
        p.user_id,
        fc.total AS facts_count,
        CASE
            WHEN fc.total = 0 THEN 0::FLOAT
            ELSE COALESCE(pu.total_mastery, 0.0) / fc.total::FLOAT
        END AS avg_mastery
    FROM participants p
    CROSS JOIN facts_count fc
    LEFT JOIN progress_by_user pu ON pu.user_id = p.user_id
),
ranked AS (
    SELECT
        us.user_id,
        u.first_name,
        u.last_name,
        u.username,
        CAST(LEAST(100, GREATEST(0, ROUND(us.avg_mastery * 100.0))) AS INT) AS percentile,
        DENSE_RANK() OVER (ORDER BY us.avg_mastery DESC)::INT AS rank
    FROM user_scores us
    JOIN users u ON u.id = us.user_id
)
SELECT user_id, first_name, last_name, username, percentile, rank
FROM ranked
ORDER BY rank ASC, user_id ASC;

-- name: GetUserSetRating :one
WITH set_facts AS (
    SELECT f.id FROM facts f WHERE f.set_id = sqlc.arg('set_id')::UUID
),
facts_count AS (
    SELECT COUNT(*)::INT AS total FROM set_facts
),
fact_cards AS (
    SELECT c.fact_id, COUNT(*)::INT AS total_cards
    FROM cards c
    JOIN set_facts sf ON sf.id = c.fact_id
    GROUP BY c.fact_id
),
participants AS (
    SELECT us.user_id FROM user_sets us WHERE us.set_id = sqlc.arg('set_id')::UUID
    UNION
    SELECT s.author_id AS user_id FROM sets s WHERE s.id = sqlc.arg('set_id')::UUID
    UNION
    SELECT sqlc.arg('user_id')::UUID AS user_id
),
progress_by_user AS (
    SELECT
        ufp.user_id,
        SUM(
            LEAST(ufp.step, GREATEST(1, COALESCE(fc.total_cards, 1)))::FLOAT / GREATEST(1, COALESCE(fc.total_cards, 1))::FLOAT
        )::FLOAT AS total_mastery
    FROM user_fact_progress ufp
    JOIN set_facts sf ON sf.id = ufp.fact_id
    LEFT JOIN fact_cards fc ON fc.fact_id = ufp.fact_id
    GROUP BY ufp.user_id
),
user_scores AS (
    SELECT
        p.user_id,
        fc.total AS facts_count,
        CASE
            WHEN fc.total = 0 THEN 0::FLOAT
            ELSE COALESCE(pu.total_mastery, 0.0) / fc.total::FLOAT
        END AS avg_mastery
    FROM participants p
    CROSS JOIN facts_count fc
    LEFT JOIN progress_by_user pu ON pu.user_id = p.user_id
),
ranked AS (
    SELECT
        us.user_id,
        CAST(LEAST(100, GREATEST(0, ROUND(us.avg_mastery * 100.0))) AS INT) AS percentile,
        DENSE_RANK() OVER (ORDER BY us.avg_mastery DESC)::INT AS rank
    FROM user_scores us
)
SELECT percentile, rank
FROM ranked
WHERE user_id = sqlc.arg('user_id')::UUID;

-- name: LockSetForAuthor :one
SELECT id FROM sets WHERE id = $1 AND author_id = $2 FOR UPDATE;
