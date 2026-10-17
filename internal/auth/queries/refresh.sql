-- name: InsertRefreshToken :one
INSERT INTO refresh_tokens (user_id, family_id, token_hash, user_agent, ip, expires_at, created_at)
VALUES (@user_id, @family_id, @token_hash, sqlc.narg(user_agent), sqlc.narg(ip), @expires_at::timestamptz, @now::timestamptz)
RETURNING id;

-- A session is alive while its family still has a refresh token that is
-- neither revoked nor expired. The access-token middleware checks this on
-- every request, so logout and password reset take effect immediately.
-- name: SessionActive :one
SELECT EXISTS (
    SELECT 1
    FROM refresh_tokens
    WHERE family_id = @family_id
      AND user_id = @user_id
      AND revoked_at IS NULL
      AND expires_at > @now::timestamptz
) AS active;

-- Row-locks the token so concurrent refreshes with the same token serialize.
-- name: GetRefreshTokenForUpdate :one
SELECT id, user_id, family_id, expires_at, revoked_at
FROM refresh_tokens
WHERE token_hash = @token_hash
FOR UPDATE;

-- name: MarkRefreshTokenRotated :execrows
UPDATE refresh_tokens
SET revoked_at = @now::timestamptz, replaced_by = @replaced_by
WHERE id = @id AND revoked_at IS NULL;

-- name: RevokeSession :execrows
UPDATE refresh_tokens
SET revoked_at = @now::timestamptz
WHERE family_id = @family_id AND revoked_at IS NULL;

-- name: RevokeAllSessionsOfUser :execrows
UPDATE refresh_tokens
SET revoked_at = @now::timestamptz
WHERE user_id = @user_id AND revoked_at IS NULL;
