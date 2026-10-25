-- +goose Up
-- Photos live apart from users, so that reading a user never loads one. A
-- new photo gets a new random ID: it is the address the photo is served at
-- without authorization, so it must be unguessable, and caches keep it.
CREATE TABLE avatars (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    image BYTEA NOT NULL
);
ALTER TABLE users ADD COLUMN avatar_id UUID;

-- +goose Down
ALTER TABLE users DROP COLUMN avatar_id;
DROP TABLE avatars;
