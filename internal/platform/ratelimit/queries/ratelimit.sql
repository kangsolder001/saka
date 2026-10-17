-- Serializes concurrent decisions for one key until the transaction ends.
-- name: LockKey :exec
SELECT pg_advisory_xact_lock(hashtextextended(@key::text, 0));

-- name: CountHits :one
SELECT count(*)::int AS hits, COALESCE(min(created_at), @since::timestamptz)::timestamptz AS oldest
FROM rate_limit_hits
WHERE key = @key AND created_at > @since::timestamptz;

-- name: InsertHit :exec
INSERT INTO rate_limit_hits (key, created_at) VALUES (@key, @now::timestamptz);

-- Hits older than the window no longer count; dropping them keeps the table
-- small without a separate cleanup job.
-- name: DeleteExpiredHits :exec
DELETE FROM rate_limit_hits WHERE key = @key AND created_at <= @since::timestamptz;

-- name: DeleteHits :exec
DELETE FROM rate_limit_hits WHERE key = @key;
