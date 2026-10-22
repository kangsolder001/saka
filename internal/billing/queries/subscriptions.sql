-- A new organization starts on the Free plan: active, with an open-ended period
-- (current_period_end stays NULL, migration 00009). The plan comes from the
-- catalogue in the database; no row back means that catalogue has no free plan.
-- name: InsertFreeSubscription :one
INSERT INTO subscriptions (organization_id, plan_id, status, current_period_start, created_at, updated_at)
SELECT @organization_id, p.id, 'active', @now::timestamptz, @now::timestamptz, @now::timestamptz
FROM plans p
WHERE p.code = 'free'
RETURNING id;

-- The plan codes are joined in so callers never see internal plan IDs.
-- name: GetSubscriptionByOrganization :one
SELECT s.id, s.organization_id, s.plan_id, p.code AS plan_code, s.status,
       s.trial_ends_at, s.current_period_start, s.current_period_end,
       s.cancel_at_period_end, pp.code AS pending_plan_code, s.grace_until,
       s.created_at, s.updated_at
FROM subscriptions s
JOIN plans p ON p.id = s.plan_id
LEFT JOIN plans pp ON pp.id = s.pending_plan_id
WHERE s.organization_id = @organization_id;

-- The raw row (plan ids, not codes) for the state machine, locked so two
-- requests or jobs cannot change one subscription at the same time. Locks are
-- always taken subscription first, then its invoices, so a webhook and a job
-- touching the same organization cannot deadlock.
-- name: LockSubscriptionByOrganization :one
SELECT * FROM subscriptions WHERE organization_id = @organization_id FOR UPDATE;

-- name: LockSubscriptionByID :one
SELECT * FROM subscriptions WHERE id = @id FOR UPDATE;

-- Writes the fields the state machine owns (billing.Snapshot).
-- name: UpdateSubscriptionState :exec
UPDATE subscriptions
SET status               = @status,
    plan_id              = @plan_id,
    trial_ends_at        = sqlc.narg(trial_ends_at)::timestamptz,
    current_period_start = @current_period_start,
    current_period_end   = sqlc.narg(current_period_end)::timestamptz,
    cancel_at_period_end = @cancel_at_period_end,
    pending_plan_id      = sqlc.narg(pending_plan_id)::uuid,
    grace_until          = sqlc.narg(grace_until)::timestamptz,
    updated_at           = @now::timestamptz
WHERE id = @id;
