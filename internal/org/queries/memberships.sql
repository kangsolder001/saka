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

-- Keyset pagination on (created_at, id), oldest first: the owner leads.
-- name: ListMemberships :many
SELECT id, organization_id, user_id, role, created_at
FROM memberships
WHERE organization_id = @organization_id
  AND (
    sqlc.narg(after_created_at)::timestamptz IS NULL
    OR (created_at, id) > (sqlc.narg(after_created_at)::timestamptz, sqlc.narg(after_id)::uuid)
  )
ORDER BY created_at, id
LIMIT @row_limit;

-- Row-locks the target so two concurrent role changes or removals serialize.
-- name: GetMembershipForUpdate :one
SELECT id, organization_id, user_id, role, created_at
FROM memberships
WHERE organization_id = @organization_id AND user_id = @user_id
FOR UPDATE;

-- The owner is never touched here: ownership moves only by transfer.
-- name: UpdateMembershipRole :one
UPDATE memberships
SET role = @role
WHERE organization_id = @organization_id AND user_id = @user_id AND role <> 'owner'
RETURNING id, organization_id, user_id, role, created_at;

-- name: DeleteMembership :execrows
DELETE FROM memberships
WHERE organization_id = @organization_id AND user_id = @user_id AND role <> 'owner';

-- Ownership transfer, in three compare-and-set steps (see Service.TransferOwnership).
-- The demotion goes first because the partial unique index allows one owner.
-- name: DemoteOwnerToAdmin :execrows
UPDATE memberships SET role = 'admin'
WHERE organization_id = @organization_id AND user_id = @user_id AND role = 'owner';

-- name: PromoteToOwner :execrows
UPDATE memberships SET role = 'owner'
WHERE organization_id = @organization_id AND user_id = @user_id AND role <> 'owner';

-- Used when accepting an invitation: already being a member is not an error.
-- name: InsertMembershipIfAbsent :execrows
INSERT INTO memberships (organization_id, user_id, role, created_at)
VALUES (@organization_id, @user_id, @role, @now::timestamptz)
ON CONFLICT (organization_id, user_id) DO NOTHING;

-- Every live organization the user belongs to, for account deletion.
-- name: ListUserMemberships :many
SELECT m.organization_id, m.role
FROM memberships m
JOIN organizations o ON o.id = m.organization_id
WHERE m.user_id = @user_id AND o.deleted_at IS NULL
ORDER BY m.organization_id;

-- name: CountOtherMembers :one
SELECT count(*) FROM memberships
WHERE organization_id = @organization_id AND user_id <> @user_id;
