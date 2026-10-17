-- name: CreateUser :one
INSERT INTO users (email, password_hash, name, created_at, updated_at)
VALUES (@email, @password_hash, @name, @now::timestamptz, @now::timestamptz)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = @id AND deleted_at IS NULL;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = @email AND deleted_at IS NULL;

-- Idempotent: only the first call sets the timestamp.
-- name: MarkEmailVerified :execrows
UPDATE users
SET email_verified_at = @now::timestamptz, updated_at = @now::timestamptz
WHERE id = @id AND deleted_at IS NULL AND email_verified_at IS NULL;

-- name: UpdatePasswordHash :execrows
UPDATE users
SET password_hash = @password_hash, updated_at = @now::timestamptz
WHERE id = @id AND deleted_at IS NULL;
