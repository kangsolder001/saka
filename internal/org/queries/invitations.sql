-- An organization that is deleted takes its open invitations with it.
-- name: RevokeOpenInvitationsForOrganization :execrows
UPDATE invitations
SET revoked_at = @now::timestamptz
WHERE organization_id = @organization_id
  AND accepted_at IS NULL AND declined_at IS NULL AND revoked_at IS NULL;
