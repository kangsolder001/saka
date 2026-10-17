-- +goose Up
-- +goose StatementBegin
-- Rate limiting moved to Redis (shared by all modules); the PostgreSQL table
-- from migration 00004 is no longer used.
DROP TABLE rate_limit_hits;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TABLE rate_limit_hits (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    key        text        NOT NULL,
    created_at timestamptz NOT NULL
);

CREATE INDEX rate_limit_hits_key_idx ON rate_limit_hits (key, created_at);
-- +goose StatementEnd
