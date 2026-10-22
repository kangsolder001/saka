-- +goose Up
-- +goose StatementBegin
-- Organizations (spec §4.4). The organization is the tenant: it is what
-- subscribes and what owns every piece of business data.
CREATE TABLE organizations (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    name       text        NOT NULL,
    -- Not partial on deleted_at: a deleted organization keeps its slug reserved
    -- so the old URL cannot be taken over by someone else.
    slug       citext      NOT NULL UNIQUE,
    logo_url   text,
    owner_id   uuid        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);

-- Memberships (spec §4.5).
CREATE TABLE memberships (
    id              uuid        PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    user_id         uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role            text        NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    -- Also the index for "is this user a member of this org", the tenant
    -- middleware lookup, and the target of organizations.owner_id below.
    UNIQUE (organization_id, user_id)
);

-- Exactly one owner per organization ("tepat satu owner", spec §4.4).
CREATE UNIQUE INDEX memberships_one_owner_idx ON memberships (organization_id) WHERE role = 'owner';
-- Listing the organizations of a user (GET /orgs, GET /me).
CREATE INDEX memberships_user_idx ON memberships (user_id);

-- The owner must be a member of the organization. Deferred because creating
-- an organization inserts the organization and its owner membership in the
-- same transaction, in either order, and ownership transfer rewrites both.
ALTER TABLE organizations
    ADD CONSTRAINT organizations_owner_membership_fkey
    FOREIGN KEY (id, owner_id) REFERENCES memberships (organization_id, user_id)
    DEFERRABLE INITIALLY DEFERRED;

-- Invitations (spec §4.6). Only the SHA-256 of the token is stored.
CREATE TABLE invitations (
    id              uuid        PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    email           citext      NOT NULL,
    role            text        NOT NULL CHECK (role IN ('admin', 'member')),
    token_hash      text        NOT NULL UNIQUE,
    invited_by      uuid        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    expires_at      timestamptz NOT NULL,
    accepted_at     timestamptz,
    declined_at     timestamptz,
    revoked_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    -- An invitation ends exactly one way.
    CHECK (num_nonnulls(accepted_at, declined_at, revoked_at) <= 1)
);

-- One open invitation per (organization, email). "Open" ignores expiry on
-- purpose (now() is not allowed in an index predicate): inviting again
-- refreshes the existing row in place, expired or not (spec §7.4).
CREATE UNIQUE INDEX invitations_open_idx ON invitations (organization_id, email)
    WHERE accepted_at IS NULL AND declined_at IS NULL AND revoked_at IS NULL;
-- Finding the open invitations of an email, for auto-accept at registration.
CREATE INDEX invitations_email_idx ON invitations (email)
    WHERE accepted_at IS NULL AND declined_at IS NULL AND revoked_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE invitations;
ALTER TABLE organizations DROP CONSTRAINT organizations_owner_membership_fkey;
DROP TABLE memberships;
DROP TABLE organizations;
-- +goose StatementEnd
