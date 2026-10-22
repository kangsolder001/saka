-- An organization that is deleted takes its open invitations with it.
-- name: RevokeOpenInvitationsForOrganization :execrows
UPDATE invitations
SET revoked_at = @now::timestamptz
WHERE organization_id = @organization_id
  AND accepted_at IS NULL AND declined_at IS NULL AND revoked_at IS NULL;

-- "Open" means not accepted, declined or revoked; expiry is checked separately
-- (the unique index ignores it, migration 00006).
-- name: GetOpenInvitationByEmailForUpdate :one
SELECT id FROM invitations
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

-- Resend: a fresh token and expiry for an open invitation, expired or not.
-- name: RenewInvitation :one
UPDATE invitations
SET token_hash = @token_hash, expires_at = @expires_at::timestamptz
WHERE id = @id AND organization_id = @organization_id
  AND accepted_at IS NULL AND declined_at IS NULL AND revoked_at IS NULL
RETURNING *;
