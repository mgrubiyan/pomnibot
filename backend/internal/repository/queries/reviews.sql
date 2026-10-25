-- name: RecordAnswer :one
INSERT INTO answers (user_id, card_id, correct, attempt_id)
VALUES ($1, $2, $3, $4)
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

-- name: GetLastAnswer :one
-- The verdict recorded last for the user's answer to the card, if it was
-- recorded after since_id: ids are UUIDv7 and grow with the time of the
-- answer, so the bound reads only the user's latest answers by their index.
SELECT correct FROM answers
WHERE user_id = @user_id AND card_id = @card_id
  AND id >= @since_id::uuid
ORDER BY id DESC
LIMIT 1;

-- name: GetAnswerByAttempt :one
SELECT correct FROM answers
WHERE user_id = @user_id AND card_id = @card_id AND attempt_id = @attempt_id;
