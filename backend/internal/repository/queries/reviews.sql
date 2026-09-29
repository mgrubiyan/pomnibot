-- name: RecordAnswerResult :one
INSERT INTO answer_results (user_id, card_id, correct, answered_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdateFactProgressOnAnswer :one
INSERT INTO user_fact_progress (
    user_id,
    fact_id,
    elo,
    ceiling_found,
    consecutive_correct,
    consecutive_incorrect,
    interval_days,
    ease_factor,
    next_review_at,
    last_reviewed_at
)
VALUES (
    $1,
    $2,
    CASE WHEN $3::BOOLEAN THEN 700 + 201 ELSE GREATEST(400, 700 - 201) END,
    CASE WHEN NOT $3::BOOLEAN THEN TRUE ELSE FALSE END,
    CASE WHEN $3::BOOLEAN THEN 1 ELSE 0 END,
    CASE WHEN NOT $3::BOOLEAN THEN 1 ELSE 0 END,
    0,
    2.50,
    CASE WHEN NOT $3::BOOLEAN THEN CURRENT_TIMESTAMP + INTERVAL '1 day' ELSE CURRENT_TIMESTAMP END,
    CURRENT_TIMESTAMP
)
ON CONFLICT (user_id, fact_id) DO UPDATE SET
    elo = CASE
        WHEN $3::BOOLEAN THEN user_fact_progress.elo + 201
        ELSE GREATEST(400, user_fact_progress.elo - 201)
    END,
    ceiling_found = CASE
        WHEN NOT $3::BOOLEAN OR user_fact_progress.ceiling_found OR (user_fact_progress.elo + 201 >= 1400) THEN TRUE
        ELSE FALSE
    END,
    consecutive_correct = CASE
        WHEN $3::BOOLEAN THEN user_fact_progress.consecutive_correct + 1
        ELSE 0
    END,
    consecutive_incorrect = CASE
        WHEN NOT $3::BOOLEAN THEN user_fact_progress.consecutive_incorrect + 1
        ELSE 0
    END,
    ease_factor = CASE
        WHEN NOT $3::BOOLEAN THEN GREATEST(1.30, user_fact_progress.ease_factor - 0.20)
        ELSE LEAST(2.50, user_fact_progress.ease_factor + 0.20)
    END,
    interval_days = CASE
        WHEN $3::BOOLEAN AND NOT (NOT $3::BOOLEAN OR user_fact_progress.ceiling_found OR (user_fact_progress.elo + 201 >= 1400)) THEN 0
        WHEN NOT $3::BOOLEAN THEN 1
        WHEN user_fact_progress.interval_days = 0 THEN 1
        WHEN user_fact_progress.interval_days = 1 THEN 3
        ELSE LEAST(30, (user_fact_progress.interval_days * user_fact_progress.ease_factor)::INT)
    END,
    next_review_at = CASE
        WHEN $3::BOOLEAN AND NOT (NOT $3::BOOLEAN OR user_fact_progress.ceiling_found OR (user_fact_progress.elo + 201 >= 1400)) THEN CURRENT_TIMESTAMP
        WHEN NOT $3::BOOLEAN THEN CURRENT_TIMESTAMP + INTERVAL '1 day'
        WHEN user_fact_progress.interval_days = 0 THEN CURRENT_TIMESTAMP + INTERVAL '1 day'
        WHEN user_fact_progress.interval_days = 1 THEN CURRENT_TIMESTAMP + INTERVAL '3 days'
        ELSE CURRENT_TIMESTAMP + (LEAST(30, (user_fact_progress.interval_days * user_fact_progress.ease_factor)::INT) || ' days')::INTERVAL
    END,
    last_reviewed_at = CURRENT_TIMESTAMP
RETURNING *;
