-- +goose Up
-- The messenger photo the avatar was taken from: MAX or Telegram sends its
-- address on every sign-in, and a new one means the user changed the photo.
-- NULL for a photo the user uploaded and for none.
ALTER TABLE users ADD COLUMN avatar_source TEXT;

-- +goose Down
ALTER TABLE users DROP COLUMN avatar_source;
