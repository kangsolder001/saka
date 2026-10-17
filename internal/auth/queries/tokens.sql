-- name: InsertUserToken :one
INSERT INTO user_tokens (user_id, type, token_hash, payload, expires_at, created_at)
VALUES (@user_id, @type, @token_hash, sqlc.narg(payload), @expires_at::timestamptz, @now::timestamptz)
RETURNING id;

-- A new token replaces any unused one of the same kind, so only the latest
-- link in the user's inbox works.
-- name: DeleteOpenUserTokens :exec
DELETE FROM user_tokens
WHERE user_id = @user_id AND type = @type AND used_at IS NULL;

-- Consuming is one atomic statement: of any number of concurrent callers with
-- the same token exactly one gets a row back.
-- name: ConsumeUserToken :one
UPDATE user_tokens
SET used_at = @now::timestamptz
WHERE token_hash = @token_hash
  AND type = @type
  AND used_at IS NULL
  AND expires_at > @now::timestamptz
RETURNING id, user_id, type, payload, expires_at, created_at;
