-- name: CreateSet :one
INSERT INTO sets (title, author_id, share_code)
VALUES ($1, $2, $3)
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
    s.created_at,
    s.updated_at,
    (SELECT COUNT(*)::INT FROM cards c JOIN facts f ON c.fact_id = f.id WHERE f.set_id = s.id) AS cards_total,
    (SELECT COUNT(*)::INT
     FROM facts f
     LEFT JOIN user_fact_progress ufp ON ufp.fact_id = f.id AND ufp.user_id = $2
     WHERE f.set_id = s.id
       AND (ufp.next_review_at IS NULL OR ufp.next_review_at <= NOW())) AS cards_due
FROM sets s
JOIN users u ON s.author_id = u.id
WHERE s.id = $1
  AND (s.author_id = $2 OR EXISTS (SELECT 1 FROM user_sets us WHERE us.set_id = s.id AND us.user_id = $2));

-- name: GetSetByShareCode :one
SELECT
    s.id,
    s.title,
    s.author_id,
    u.first_name AS author_first_name,
    u.last_name AS author_last_name,
    u.username AS author_username,
    s.share_code,
    s.created_at,
    s.updated_at,
    (SELECT COUNT(*)::INT FROM cards c JOIN facts f ON c.fact_id = f.id WHERE f.set_id = s.id) AS cards_total,
    (SELECT COUNT(*)::INT
     FROM facts f
     LEFT JOIN user_fact_progress ufp ON ufp.fact_id = f.id AND ufp.user_id = $2
     WHERE f.set_id = s.id
       AND (ufp.next_review_at IS NULL OR ufp.next_review_at <= NOW())) AS cards_due
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
    s.created_at,
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
