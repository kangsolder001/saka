-- ON CONFLICT DO NOTHING (instead of failing) keeps the surrounding
-- transaction usable when the slug is taken: the caller sees no row and tries
-- another slug.
-- name: InsertOrganization :one
INSERT INTO organizations (id, name, slug, owner_id, created_at, updated_at)
VALUES (@id, @name, @slug, @owner_id, @now::timestamptz, @now::timestamptz)
ON CONFLICT (slug) DO NOTHING
RETURNING *;

-- name: GetOrganization :one
SELECT * FROM organizations WHERE id = @id AND deleted_at IS NULL;

-- Keyset pagination on (membership created_at, membership id), oldest first.
-- name: ListOrganizationsForUser :many
SELECT o.id, o.name, o.slug, o.logo_url, o.owner_id, o.created_at, o.updated_at,
       m.id AS membership_id, m.role, m.created_at AS joined_at
FROM memberships m
JOIN organizations o ON o.id = m.organization_id
WHERE m.user_id = @user_id
  AND o.deleted_at IS NULL
  AND (
    sqlc.narg(after_created_at)::timestamptz IS NULL
    OR (m.created_at, m.id) > (sqlc.narg(after_created_at)::timestamptz, sqlc.narg(after_id)::uuid)
  )
ORDER BY m.created_at, m.id
LIMIT @row_limit;

-- PATCH semantics: a NULL name or slug keeps the current value; the logo is
-- only touched when set_logo is true, so it can be cleared by passing NULL.
-- name: UpdateOrganization :one
UPDATE organizations
SET name       = COALESCE(sqlc.narg(name), name),
    slug       = COALESCE(sqlc.narg(slug), slug),
    logo_url   = CASE WHEN @set_logo::boolean THEN sqlc.narg(logo_url) ELSE logo_url END,
    updated_at = @now::timestamptz
WHERE id = @id AND deleted_at IS NULL
RETURNING *;

-- Soft delete: the row stays so the slug remains reserved (migration 00006).
-- name: SoftDeleteOrganization :execrows
UPDATE organizations
SET deleted_at = @now::timestamptz, updated_at = @now::timestamptz
WHERE id = @id AND deleted_at IS NULL;

-- Compare-and-set on the previous owner, so a transfer that lost a race matches no row.
-- name: SetOrganizationOwner :execrows
UPDATE organizations
SET owner_id = @new_owner_id, updated_at = @now::timestamptz
WHERE id = @id AND owner_id = @old_owner_id AND deleted_at IS NULL;
