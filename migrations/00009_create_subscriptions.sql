-- +goose Up
-- +goose StatementBegin
-- Subscriptions (spec §4.8): one per organization, pointing at a plan. The
-- organization is what subscribes, never a user.
CREATE TABLE subscriptions (
    id                   uuid        PRIMARY KEY DEFAULT uuidv7(),
    -- UNIQUE doubles as the lookup index and leads with organization_id, as the
    -- row-level security predicate wants (docs/rls.md).
    organization_id      uuid        NOT NULL UNIQUE REFERENCES organizations (id) ON DELETE CASCADE,
    plan_id              uuid        NOT NULL REFERENCES plans (id) ON DELETE RESTRICT,
    status               text        NOT NULL CHECK (status IN ('trialing', 'active', 'past_due', 'expired', 'canceled')),
    trial_ends_at        timestamptz,
    current_period_start timestamptz NOT NULL,
    -- NULL means the period never ends: the Free plan, which is never invoiced.
    -- Billing jobs select on current_period_end IS NOT NULL.
    current_period_end   timestamptz,
    cancel_at_period_end boolean     NOT NULL DEFAULT false,
    -- Scheduled downgrade, applied when the current period ends.
    pending_plan_id      uuid        REFERENCES plans (id) ON DELETE RESTRICT,
    grace_until          timestamptz,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    -- A trial always has an end, or it would never expire.
    CHECK (status <> 'trialing' OR trial_ends_at IS NOT NULL)
);

-- Every organization that exists already starts on Free (new ones get theirs
-- when they are created). Deleted organizations are skipped: nothing reads them.
INSERT INTO subscriptions (organization_id, plan_id, status, current_period_start, created_at, updated_at)
SELECT o.id, p.id, 'active', o.created_at, now(), now()
FROM organizations o
JOIN plans p ON p.code = 'free'
WHERE o.deleted_at IS NULL;

-- Same policy as the other tenant tables (migration 00007).
ALTER TABLE subscriptions ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON subscriptions
    USING (
        organization_id = (SELECT NULLIF(current_setting('app.current_org', true), '')::uuid)
        OR (SELECT COALESCE(current_setting('app.bypass_rls', true), '') = 'on')
    );
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP POLICY tenant_isolation ON subscriptions;
DROP TABLE subscriptions;
-- +goose StatementEnd
