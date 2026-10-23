-- name: RecordAnswer :one
INSERT INTO answers (user_id, card_id, correct)
VALUES ($1, $2, $3)
RETURNING *;

-- name: UpsertFactProgress :one
INSERT INTO user_fact_progress (
    user_id,
    fact_id,
    step,
    stability,
    difficulty,
    reps,
    lapses,
    state,
    last_reviewed_at,
    next_review_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (user_id, fact_id) DO UPDATE SET
    step = EXCLUDED.step,
    stability = EXCLUDED.stability,
    difficulty = EXCLUDED.difficulty,
    reps = EXCLUDED.reps,
    lapses = EXCLUDED.lapses,
    state = EXCLUDED.state,
    last_reviewed_at = EXCLUDED.last_reviewed_at,
    next_review_at = EXCLUDED.next_review_at
RETURNING *;
