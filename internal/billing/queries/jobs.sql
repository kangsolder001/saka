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
