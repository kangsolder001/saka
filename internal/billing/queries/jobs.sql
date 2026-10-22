-- Queries of the periodic billing jobs (spec §8.6). The jobs list candidate
-- ids in pages (keyset on id, so one run visits each row once), then lock and
-- re-read each row in a transaction of its own with FOR UPDATE SKIP LOCKED: a
-- row another worker is busy with is skipped, and its conditions are checked
-- again after the lock, so two workers never act on one row twice.

-- Subscriptions that need a renewal invoice: paid, still running, not set to
-- cancel, and whose period ends within the lead time (or already ended, when
-- the worker was down). Organizations that were deleted have no renewals.
-- name: ListRenewalCandidates :many
SELECT s.id
FROM subscriptions s
JOIN organizations o ON o.id = s.organization_id AND o.deleted_at IS NULL
WHERE s.status IN ('active', 'past_due')
  AND s.current_period_end IS NOT NULL
  AND NOT s.cancel_at_period_end
  AND s.current_period_end <= @horizon::timestamptz
  AND s.id > @after_id
ORDER BY s.id
LIMIT @row_limit;

-- name: LockSubscriptionSkipLocked :one
SELECT * FROM subscriptions WHERE id = @id FOR UPDATE SKIP LOCKED;

-- The renewal invoice of a period, whatever became of it, unless it was voided.
-- name: GetRenewalInvoice :one
SELECT * FROM invoices
WHERE subscription_id = @subscription_id AND kind = 'renewal' AND period_start = @period_start AND status <> 'void';

-- Subscriptions with a time-driven event due: a trial that ended, a period that
-- ended, a grace period that ended. billing.expire and billing.apply_scheduled
-- both read this list; the state machine decides which of them acts on each.
-- name: ListDueSubscriptions :many
SELECT s.id
FROM subscriptions s
WHERE s.id > @after_id
  AND (
    (s.status = 'trialing' AND s.trial_ends_at <= @now::timestamptz)
    OR (s.status = 'active' AND s.current_period_end IS NOT NULL AND s.current_period_end <= @now::timestamptz)
    OR (s.status = 'past_due' AND s.grace_until <= @now::timestamptz)
  )
ORDER BY s.id
LIMIT @row_limit;

-- Closes the live invoices of a subscription as expired (the time to pay ran
-- out), optionally one kind only.
-- name: ExpireLiveInvoices :execrows
UPDATE invoices
SET status = 'expired', updated_at = @now::timestamptz
WHERE subscription_id = @subscription_id
  AND status IN ('draft', 'open')
  AND (sqlc.narg(kind)::text IS NULL OR kind = sqlc.narg(kind)::text);

-- Invoices nobody can pay any more: a checkout past its due date, a renewal
-- past its due date plus the grace period (the cutoff). Their payment links
-- expire at the same moments, so a late payment is not expected.
-- name: ExpireOverdueInvoices :execrows
UPDATE invoices
SET status = 'expired', updated_at = @now::timestamptz
WHERE status IN ('draft', 'open')
  AND (
    (kind = 'checkout' AND due_at <= @now::timestamptz)
    OR (kind = 'renewal' AND due_at <= @renewal_cutoff::timestamptz)
  );
