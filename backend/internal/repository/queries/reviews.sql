-- name: RecordCardReview :one
INSERT INTO card_reviews (user_id, card_id, correct, answered_at)
VALUES ($1, $2, $3, $4)
RETURNING id, user_id, card_id, correct, answered_at;

-- name: UpsertCardProgress :one
INSERT INTO user_card_progress (
    user_id,
    card_id,
    next_review_at,
    last_reviewed_at,
    repetitions,
    interval_days
) VALUES (
    $1, $2, $3, $4, $5, $6
)
ON CONFLICT (user_id, card_id) DO UPDATE
SET next_review_at = EXCLUDED.next_review_at,
    last_reviewed_at = EXCLUDED.last_reviewed_at,
    repetitions = EXCLUDED.repetitions,
    interval_days = EXCLUDED.interval_days
RETURNING user_id, card_id, next_review_at, last_reviewed_at, repetitions, interval_days;

-- name: GetDueCardsCountByUserID :one
SELECT COUNT(DISTINCT c.id)::int AS due_count
FROM cards c
JOIN card_sets s ON s.id = c.set_id
LEFT JOIN user_card_sets ucs ON ucs.set_id = s.id AND ucs.user_id = $1
JOIN user_card_progress p ON p.card_id = c.id AND p.user_id = $1
WHERE (s.creator_id = $1 OR ucs.user_id = $1)
  AND p.next_review_at <= now();

-- name: GetFeedCardsByUserID :many
SELECT c.*
FROM cards c
JOIN card_sets s ON s.id = c.set_id
LEFT JOIN user_card_sets ucs ON ucs.set_id = s.id AND ucs.user_id = $1
LEFT JOIN user_card_progress p ON p.card_id = c.id AND p.user_id = $1
WHERE (s.creator_id = $1 OR ucs.user_id = $1)
  AND (p.next_review_at IS NULL OR p.next_review_at <= now())
ORDER BY COALESCE(p.next_review_at, now()) ASC
LIMIT $2;

-- name: GetSetPlanBySetID :many
SELECT 
    c.topic AS fact_name,
    COALESCE(p.next_review_at, now()) AS review_date
FROM cards c
LEFT JOIN user_card_progress p ON p.card_id = c.id AND p.user_id = $2
WHERE c.set_id = $1
ORDER BY review_date ASC;
