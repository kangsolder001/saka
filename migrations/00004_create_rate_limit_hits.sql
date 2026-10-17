-- +goose Up
-- +goose StatementBegin
-- Sliding-window rate limiting without extra infrastructure (spec §5.6). One
-- row per counted event; key is a hash, so no email or IP is stored in clear.
CREATE TABLE rate_limit_hits (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    key        text        NOT NULL,
    created_at timestamptz NOT NULL
);

CREATE INDEX rate_limit_hits_key_idx ON rate_limit_hits (key, created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE rate_limit_hits;
-- +goose StatementEnd
