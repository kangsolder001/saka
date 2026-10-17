-- name: InsertJob :one
INSERT INTO outbox_jobs (type, payload, trace_id, next_attempt_at, created_at, updated_at)
VALUES (@type, @payload, sqlc.narg(trace_id), @next_attempt_at, @now, @now)
RETURNING id;

-- Claims due jobs for one worker. Pending jobs that are due, and processing
-- jobs whose lease expired (their worker died), are picked up. SKIP LOCKED lets
-- concurrent workers claim disjoint sets; attempts is incremented here, so a
-- crashed attempt still counts toward the retry limit.
-- name: ClaimJobs :many
WITH due AS (
    SELECT o.id
    FROM outbox_jobs AS o
    WHERE (o.status = 'pending' AND o.next_attempt_at <= @now::timestamptz)
       OR (o.status = 'processing' AND o.locked_at <= @lease_expired_before::timestamptz)
    ORDER BY o.next_attempt_at, o.id
    LIMIT @batch_size
    FOR UPDATE OF o SKIP LOCKED
)
UPDATE outbox_jobs AS j
SET status = 'processing',
    attempts = j.attempts + 1,
    locked_at = @now::timestamptz,
    updated_at = @now::timestamptz
FROM due
WHERE j.id = due.id
RETURNING j.id, j.type, j.payload, j.attempts, j.trace_id, j.created_at;

-- The three updates below are fenced by attempts: if a slow worker's lease
-- expired and another worker reclaimed the job (attempts + 1), the stale
-- worker's update matches zero rows and cannot overwrite the newer outcome.

-- name: MarkJobDone :execrows
UPDATE outbox_jobs
SET status = 'done', locked_at = NULL, last_error = NULL, updated_at = @now::timestamptz
WHERE id = @id AND status = 'processing' AND attempts = @attempts;

-- name: ScheduleJobRetry :execrows
UPDATE outbox_jobs
SET status = 'pending', locked_at = NULL, next_attempt_at = @next_attempt_at,
    last_error = @last_error, updated_at = @now::timestamptz
WHERE id = @id AND status = 'processing' AND attempts = @attempts;

-- name: MarkJobFailed :execrows
UPDATE outbox_jobs
SET status = 'failed', locked_at = NULL, last_error = @last_error, updated_at = @now::timestamptz
WHERE id = @id AND status = 'processing' AND attempts = @attempts;
