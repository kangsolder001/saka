-- ON CONFLICT DO NOTHING (instead of failing) keeps the surrounding
-- transaction usable when the slug is taken: the caller sees no row and tries
-- another slug.
-- name: InsertOrganization :one
INSERT INTO organizations (id, name, slug, owner_id, created_at, updated_at)
VALUES (@id, @name, @slug, @owner_id, @now::timestamptz, @now::timestamptz)
ON CONFLICT (slug) DO NOTHING
RETURNING *;
