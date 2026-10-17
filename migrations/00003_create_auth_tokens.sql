-- +goose Up
-- +goose StatementBegin
-- One table for every single-use token (spec §4.2). Only the SHA-256 of the
-- raw token is stored, so a database leak does not expose usable tokens.
CREATE TABLE user_tokens (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    type       text        NOT NULL
                           CHECK (type IN ('email_verification', 'password_reset', 'email_change')),
    token_hash text        NOT NULL UNIQUE,
    payload    jsonb,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Finds the still-usable tokens of a user, e.g. to invalidate older ones.
CREATE INDEX user_tokens_open_idx ON user_tokens (user_id, type) WHERE used_at IS NULL;

-- Refresh tokens (spec §4.3). One login = one family; rotation links a token
-- to its successor through replaced_by.
CREATE TABLE refresh_tokens (
    id          uuid        PRIMARY KEY DEFAULT uuidv7(),
    user_id     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    family_id   uuid        NOT NULL,
    token_hash  text        NOT NULL UNIQUE,
    user_agent  text,
    ip          text,
    expires_at  timestamptz NOT NULL,
    revoked_at  timestamptz,
    replaced_by uuid        REFERENCES refresh_tokens (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Revoking a whole session and checking that a session is alive.
CREATE INDEX refresh_tokens_family_idx ON refresh_tokens (family_id);
-- Revoking every session of a user (password reset) and listing sessions.
CREATE INDEX refresh_tokens_user_idx ON refresh_tokens (user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE refresh_tokens;
DROP TABLE user_tokens;
-- +goose StatementEnd
