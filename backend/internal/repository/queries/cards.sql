-- name: CreateCard :one
INSERT INTO cards (id, fact_id, kind, question, answer_text, explanation, source_quote, source_ref)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: CreateCardOption :one
INSERT INTO card_options (card_id, position, text, is_correct)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateCardTableColumn :one
INSERT INTO card_table_columns (card_id, position, name)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateCardTableItem :one
INSERT INTO card_table_items (card_id, item_text, column_name)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetCardByID :one
SELECT
    c.id,
    f.set_id,
    c.fact_id,
    c.kind,
    ck.difficulty_level,
    c.question,
    c.answer_text,
    c.explanation,
    c.source_quote,
    c.source_ref,
    COALESCE(t.name, '') AS topic,
    c.created_at,
    c.updated_at
FROM cards c
JOIN facts f ON c.fact_id = f.id
JOIN card_kinds ck ON c.kind = ck.kind
LEFT JOIN topics t ON f.topic_id = t.id
WHERE c.id = $1
  AND EXISTS (
      SELECT 1 FROM sets s
      WHERE s.id = f.set_id
        AND (s.author_id = @user_id OR EXISTS (SELECT 1 FROM user_sets us WHERE us.set_id = s.id AND us.user_id = @user_id))
  );

-- name: GetCardByIDForAuthor :one
SELECT
    c.id,
    f.set_id,
    c.fact_id,
    c.kind,
    ck.difficulty_level,
    c.question,
    c.answer_text,
    c.explanation,
    c.source_quote,
    c.source_ref,
    COALESCE(t.name, '') AS topic,
    c.created_at,
    c.updated_at
FROM cards c
JOIN facts f ON c.fact_id = f.id
JOIN card_kinds ck ON c.kind = ck.kind
LEFT JOIN topics t ON f.topic_id = t.id
JOIN sets s ON f.set_id = s.id
WHERE c.id = $1 AND s.author_id = $2;

-- name: GetCardsBySetID :many
SELECT
    c.id,
    f.set_id,
    c.fact_id,
    c.kind,
    ck.difficulty_level,
    c.question,
    c.answer_text,
    c.explanation,
    c.source_quote,
    c.source_ref,
    COALESCE(t.name, '') AS topic,
    c.created_at,
    c.updated_at
FROM cards c
JOIN facts f ON c.fact_id = f.id
JOIN card_kinds ck ON c.kind = ck.kind
LEFT JOIN topics t ON f.topic_id = t.id
WHERE f.set_id = $1
  AND EXISTS (
      SELECT 1 FROM sets s
      WHERE s.id = $1
        AND (s.author_id = @user_id OR EXISTS (SELECT 1 FROM user_sets us WHERE us.set_id = s.id AND us.user_id = @user_id))
  )
ORDER BY c.created_at ASC;

-- name: GetCardOptions :many
SELECT id, card_id, position, text, is_correct
FROM card_options
WHERE card_id = $1
ORDER BY position ASC;

-- name: GetCardTableColumns :many
SELECT id, card_id, position, name
FROM card_table_columns
WHERE card_id = $1
ORDER BY position ASC;

-- name: GetCardTableItems :many
SELECT id, card_id, item_text, column_name
FROM card_table_items
WHERE card_id = $1;

-- name: UpdateCard :one
UPDATE cards
SET
    kind = COALESCE($2, kind),
    question = COALESCE($3, question),
    answer_text = COALESCE($4, answer_text),
    explanation = COALESCE($5, explanation),
    source_quote = COALESCE($6, source_quote),
    source_ref = COALESCE($7, source_ref),
    updated_at = CURRENT_TIMESTAMP
FROM facts f
JOIN sets s ON f.set_id = s.id
WHERE cards.id = $1
  AND cards.fact_id = f.id
  AND s.author_id = $8
RETURNING cards.id, cards.fact_id, cards.kind, cards.question, cards.answer_text, cards.explanation, cards.source_quote, cards.source_ref, cards.created_at, cards.updated_at;

-- name: DeleteCard :execrows
DELETE FROM cards c
USING facts f, sets s
WHERE c.id = $1
  AND c.fact_id = f.id
  AND f.set_id = s.id
  AND s.author_id = $2;

-- name: DeleteCardOptions :exec
DELETE FROM card_options WHERE card_id = $1;

-- name: DeleteCardTableColumns :exec
DELETE FROM card_table_columns WHERE card_id = $1;

-- name: DeleteCardTableItems :exec
DELETE FROM card_table_items WHERE card_id = $1;

-- name: GetFeedCardsForUser :many
WITH user_due_facts AS (
    SELECT
        f.id AS fact_id,
        f.set_id,
        COALESCE(t.name, '') AS topic,
        COALESCE(ufp.elo, 700) AS elo,
        CASE
            WHEN COALESCE(ufp.elo, 700) < 800 THEN 0
            WHEN COALESCE(ufp.elo, 700) < 1000 THEN 1
            WHEN COALESCE(ufp.elo, 700) < 1200 THEN 2
            WHEN COALESCE(ufp.elo, 700) < 1400 THEN 3
            ELSE 4
        END AS target_difficulty
    FROM user_sets us
    JOIN facts f ON us.set_id = f.set_id
    LEFT JOIN topics t ON f.topic_id = t.id
    LEFT JOIN user_fact_progress ufp ON ufp.fact_id = f.id AND ufp.user_id = us.user_id
    WHERE us.user_id = $1
      AND (ufp.next_review_at IS NULL OR ufp.next_review_at <= NOW())
),
ranked_cards AS (
    SELECT
        c.id,
        udf.set_id,
        c.fact_id,
        c.kind,
        ck.difficulty_level,
        c.question,
        c.answer_text,
        c.explanation,
        c.source_quote,
        c.source_ref,
        udf.topic,
        c.created_at,
        c.updated_at,
        ROW_NUMBER() OVER (
            PARTITION BY c.fact_id
            ORDER BY ABS(ck.difficulty_level - udf.target_difficulty) ASC, c.created_at ASC
        ) AS rank
    FROM cards c
    JOIN user_due_facts udf ON c.fact_id = udf.fact_id
    JOIN card_kinds ck ON c.kind = ck.kind
)
SELECT
    id,
    set_id,
    fact_id,
    kind,
    difficulty_level,
    question,
    answer_text,
    explanation,
    source_quote,
    source_ref,
    topic,
    created_at,
    updated_at
FROM ranked_cards
WHERE rank = 1;
