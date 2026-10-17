-- +goose Up
-- +goose StatementBegin
CREATE TABLE outbox_jobs (
    id              uuid        PRIMARY KEY DEFAULT uuidv7(),
    type            text        NOT NULL,
    payload         jsonb       NOT NULL DEFAULT '{}',
    status          text        NOT NULL DEFAULT 'pending'
                                CHECK (status IN ('pending', 'processing', 'done', 'failed')),
    attempts        integer     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    locked_at       timestamptz,
    last_error      text,
    trace_id        text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- Claim query: due pending jobs, oldest first.
CREATE INDEX outbox_jobs_pending_idx ON outbox_jobs (next_attempt_at, id)
    WHERE status = 'pending';

-- Reclaim query: processing jobs whose worker died (lease expired).
CREATE INDEX outbox_jobs_processing_idx ON outbox_jobs (locked_at)
    WHERE status = 'processing';

-- Manual review of jobs that exhausted their retries.
CREATE INDEX outbox_jobs_failed_idx ON outbox_jobs (updated_at)
    WHERE status = 'failed';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE outbox_jobs;
-- +goose StatementEnd
