-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE users (
    id                uuid        PRIMARY KEY DEFAULT uuidv7(),
    email             citext      NOT NULL UNIQUE,
    password_hash     text        NOT NULL,
    name              text        NOT NULL,
    avatar_url        text,
    email_verified_at timestamptz,
    status            text        NOT NULL DEFAULT 'active'
                                  CHECK (status IN ('active', 'suspended', 'deleted')),
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    deleted_at        timestamptz
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE users;
DROP EXTENSION IF EXISTS citext;
-- +goose StatementEnd
