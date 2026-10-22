-- name: CreateCardIssue :execrows
-- Inserts nothing when the card is gone or the user can't see its set.
INSERT INTO card_issues (card_id, set_id, user_id, reason, kind, question, answer_text, explanation, source_quote)
SELECT c.id, f.set_id, @user_id, @reason, c.kind, c.question, c.answer_text, c.explanation, c.source_quote
FROM cards c
JOIN facts f ON c.fact_id = f.id
WHERE c.id = @card_id
  AND EXISTS (
      SELECT 1 FROM sets s
      WHERE s.id = f.set_id
        AND (s.author_id = @user_id OR EXISTS (SELECT 1 FROM user_sets us WHERE us.set_id = s.id AND us.user_id = @user_id))
  );
