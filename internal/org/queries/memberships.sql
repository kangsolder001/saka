-- name: InsertMembership :one
INSERT INTO memberships (organization_id, user_id, role, created_at)
VALUES (@organization_id, @user_id, @role, @now::timestamptz)
RETURNING *;
