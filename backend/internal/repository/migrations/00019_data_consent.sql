-- +goose Up
-- When the user let set authors see their name in the leaderboard and the
-- mistakes of a set; NULL means they have not, and authors see an anonymous
-- participant.
ALTER TABLE users ADD COLUMN data_consent_at TIMESTAMPTZ;

-- The shipped demonstration accounts cannot sign in and belong to nobody, so
-- the demo sets keep their names. Real accounts always carry a platform ID.
UPDATE users SET data_consent_at = CURRENT_TIMESTAMP WHERE max_id IS NULL AND tg_id IS NULL;

-- +goose Down
ALTER TABLE users DROP COLUMN data_consent_at;
