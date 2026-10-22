-- name: InsertMembership :one
INSERT INTO memberships (organization_id, user_id, role, created_at)
VALUES (@organization_id, @user_id, @role, @now::timestamptz)
RETURNING *;

-- The tenant lookup. Joining organizations hides memberships of a deleted
-- organization, so a deleted tenant is indistinguishable from an unknown one.
-- name: GetMembership :one
SELECT m.id, m.organization_id, m.user_id, m.role, m.created_at
FROM memberships m
JOIN organizations o ON o.id = m.organization_id
WHERE m.organization_id = @organization_id
  AND m.user_id = @user_id
  AND o.deleted_at IS NULL;
