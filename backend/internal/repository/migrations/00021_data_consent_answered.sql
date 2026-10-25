-- +goose Up
-- When the user first answered the consent question, either way; NULL means
-- the mini-app has yet to ask. Whoever has already agreed has answered.
ALTER TABLE users ADD COLUMN data_consent_answered_at TIMESTAMPTZ;
UPDATE users SET data_consent_answered_at = data_consent_at WHERE data_consent_at IS NOT NULL;

-- +goose Down
ALTER TABLE users DROP COLUMN data_consent_answered_at;
