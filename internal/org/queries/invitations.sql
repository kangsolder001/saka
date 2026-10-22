-- An organization that is deleted takes its open invitations with it.
-- name: RevokeOpenInvitationsForOrganization :execrows
UPDATE invitations
SET revoked_at = @now::timestamptz
WHERE organization_id = @organization_id
  AND accepted_at IS NULL AND declined_at IS NULL AND revoked_at IS NULL;

-- "Open" means not accepted, declined or revoked; expiry is checked separately
-- (the unique index ignores it, migration 00006).
-- name: GetOpenInvitationByEmailForUpdate :one
SELECT id, expires_at FROM invitations
WHERE organization_id = @organization_id AND email = @email
  AND accepted_at IS NULL AND declined_at IS NULL AND revoked_at IS NULL
FOR UPDATE;

-- Inviting an email that already has an open invitation refreshes that row in
-- place (new token, new expiry, spec §7.4) instead of creating a second one.
-- name: UpsertInvitation :one
INSERT INTO invitations (organization_id, email, role, token_hash, invited_by, expires_at, created_at)
VALUES (@organization_id, @email, @role, @token_hash, @invited_by, @expires_at::timestamptz, @now::timestamptz)
ON CONFLICT (organization_id, email) WHERE accepted_at IS NULL AND declined_at IS NULL AND revoked_at IS NULL
DO UPDATE SET role = EXCLUDED.role,
              token_hash = EXCLUDED.token_hash,
              invited_by = EXCLUDED.invited_by,
              expires_at = EXCLUDED.expires_at
RETURNING *;

-- Active = open and not expired. Keyset pagination on (created_at, id).
-- name: ListActiveInvitations :many
SELECT * FROM invitations
WHERE organization_id = @organization_id
  AND accepted_at IS NULL AND declined_at IS NULL AND revoked_at IS NULL
  AND expires_at > @now::timestamptz
  AND (
    sqlc.narg(after_created_at)::timestamptz IS NULL
    OR (created_at, id) > (sqlc.narg(after_created_at)::timestamptz, sqlc.narg(after_id)::uuid)
  )
ORDER BY created_at, id
LIMIT @row_limit;

-- name: RevokeInvitation :execrows
UPDATE invitations SET revoked_at = @now::timestamptz
WHERE id = @id AND organization_id = @organization_id
  AND accepted_at IS NULL AND declined_at IS NULL AND revoked_at IS NULL;

-- An open invitation by ID, locked, with its expiry: resending an expired one
-- makes it count as a seat again, so the caller needs to know which it was.
-- name: GetOpenInvitationForUpdate :one
SELECT id, expires_at FROM invitations
WHERE id = @id AND organization_id = @organization_id
  AND accepted_at IS NULL AND declined_at IS NULL AND revoked_at IS NULL
FOR UPDATE;

-- Resend: a fresh token and expiry for an open invitation, expired or not.
-- name: RenewInvitation :one
UPDATE invitations
SET token_hash = @token_hash, expires_at = @expires_at::timestamptz
WHERE id = @id AND organization_id = @organization_id
  AND accepted_at IS NULL AND declined_at IS NULL AND revoked_at IS NULL
RETURNING *;

-- The public invitation lookup: the token (by hash) is the credential, so this
-- is cross-tenant and runs in a system transaction. The two variants differ
-- only in the row lock the accept/decline paths need.
-- name: GetInvitationByTokenHash :one
SELECT i.id, i.organization_id, i.email, i.role, i.invited_by, i.expires_at,
       i.accepted_at, i.declined_at, i.revoked_at, i.created_at,
       o.name AS organization_name, o.logo_url AS organization_logo_url,
       o.deleted_at AS organization_deleted_at
FROM invitations i
JOIN organizations o ON o.id = i.organization_id
WHERE i.token_hash = @token_hash;

-- name: GetInvitationByTokenHashForUpdate :one
SELECT i.id, i.organization_id, i.email, i.role, i.invited_by, i.expires_at,
       i.accepted_at, i.declined_at, i.revoked_at, i.created_at,
       o.name AS organization_name, o.logo_url AS organization_logo_url,
       o.deleted_at AS organization_deleted_at
FROM invitations i
JOIN organizations o ON o.id = i.organization_id
WHERE i.token_hash = @token_hash
FOR UPDATE OF i FOR SHARE OF o;

-- name: MarkInvitationAccepted :execrows
UPDATE invitations SET accepted_at = @now::timestamptz
WHERE id = @id AND accepted_at IS NULL AND declined_at IS NULL AND revoked_at IS NULL;

-- name: MarkInvitationDeclined :execrows
UPDATE invitations SET declined_at = @now::timestamptz
WHERE id = @id AND accepted_at IS NULL AND declined_at IS NULL AND revoked_at IS NULL;

-- Whether the address has been invited anywhere and the invitation can still be
-- used. Cross-tenant, so callers lift row-level security around it.
-- name: HasLiveInvitationForEmail :one
SELECT EXISTS (
    SELECT 1
    FROM invitations i
    JOIN organizations o ON o.id = i.organization_id
    WHERE i.email = @email
      AND i.accepted_at IS NULL AND i.declined_at IS NULL AND i.revoked_at IS NULL
      AND i.expires_at > @now::timestamptz
      AND o.deleted_at IS NULL
) AS live;

-- The invitations an address can still redeem, locked so a concurrent accept by
-- link and a verification cannot both redeem the same one. The organization row
-- is share-locked too: a user being deleted locks it exclusively while checking
-- for other members, so nobody can join between that check and the cleanup.
-- name: ListLiveInvitationsForEmailForUpdate :many
SELECT i.id, i.organization_id, i.role
FROM invitations i
JOIN organizations o ON o.id = i.organization_id
WHERE i.email = @email
  AND i.accepted_at IS NULL AND i.declined_at IS NULL AND i.revoked_at IS NULL
  AND i.expires_at > @now::timestamptz
  AND o.deleted_at IS NULL
ORDER BY i.created_at, i.id
FOR UPDATE OF i FOR SHARE OF o;
