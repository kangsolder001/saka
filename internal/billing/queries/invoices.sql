-- Invoice numbers come from one global sequence (migration 00010): never
-- reused, and not rolled back with the transaction that took one, so a failed
-- checkout leaves a gap.
-- name: NextInvoiceSequence :one
SELECT nextval('invoice_number_seq')::bigint AS seq;

-- name: InsertInvoice :one
INSERT INTO invoices (
    organization_id, subscription_id, number, kind, plan_id, amount, currency, status,
    period_start, period_end, due_at, provider, provider_ref, payment_url,
    reminder_3d_sent_at, reminder_1d_sent_at, created_at, updated_at
) VALUES (
    @organization_id, @subscription_id, @number, @kind, @plan_id, @amount, @currency, @status,
    @period_start, @period_end, @due_at, @provider, sqlc.narg(provider_ref), sqlc.narg(payment_url),
    sqlc.narg(reminder_3d_sent_at)::timestamptz, sqlc.narg(reminder_1d_sent_at)::timestamptz,
    @now::timestamptz, @now::timestamptz
)
RETURNING *;

-- The plan code and name are joined in so callers never see internal plan IDs.
-- name: GetInvoice :one
SELECT i.*, p.code AS plan_code, p.name AS plan_name
FROM invoices i
JOIN plans p ON p.id = i.plan_id
WHERE i.id = @id AND i.organization_id = @organization_id;

-- Newest first; keyset pagination on (created_at, id).
-- name: ListInvoices :many
SELECT i.*, p.code AS plan_code, p.name AS plan_name
FROM invoices i
JOIN plans p ON p.id = i.plan_id
WHERE i.organization_id = @organization_id
  AND (
    sqlc.narg(before_created_at)::timestamptz IS NULL
    OR (i.created_at, i.id) < (sqlc.narg(before_created_at)::timestamptz, sqlc.narg(before_id)::uuid)
  )
ORDER BY i.created_at DESC, i.id DESC
LIMIT @row_limit;

-- The invoices of a subscription that can still be paid, locked. The caller
-- already holds the subscription lock.
-- name: LockLiveInvoicesForSubscription :many
SELECT * FROM invoices
WHERE subscription_id = @subscription_id AND status IN ('draft', 'open')
ORDER BY created_at, id
FOR UPDATE;

-- Closes the live invoices of a subscription: all of them, or only one kind,
-- optionally sparing one. Returns how many were closed.
-- name: VoidLiveInvoices :execrows
UPDATE invoices
SET status = 'void', updated_at = @now::timestamptz
WHERE subscription_id = @subscription_id
  AND status IN ('draft', 'open')
  AND (sqlc.narg(kind)::text IS NULL OR kind = sqlc.narg(kind)::text)
  AND (sqlc.narg(except_id)::uuid IS NULL OR id <> sqlc.narg(except_id)::uuid);

-- The paid renewal for the period that starts when the subscription's current
-- one ends: what the state machine needs to roll into the next period.
-- name: GetPaidRenewalForPeriod :one
SELECT * FROM invoices
WHERE subscription_id = @subscription_id AND kind = 'renewal' AND status = 'paid' AND period_start = @period_start;

-- A webhook knows an invoice only by its number, across tenants (a system
-- transaction): the subscription is found from it and locked first, then the
-- invoice is locked and read again.
-- name: GetInvoiceByNumber :one
SELECT * FROM invoices WHERE number = @number;

-- name: LockInvoice :one
SELECT * FROM invoices WHERE id = @id FOR UPDATE;

-- Marks a live invoice paid. The period is written too: a checkout is paid for
-- the period that starts when it is paid, not when it was issued.
-- name: MarkInvoicePaid :exec
UPDATE invoices
SET status       = 'paid',
    paid_at      = @paid_at::timestamptz,
    period_start = @period_start,
    period_end   = @period_end,
    provider_ref = COALESCE(sqlc.narg(provider_ref), provider_ref),
    updated_at   = @paid_at::timestamptz
WHERE id = @id AND status IN ('draft', 'open');

-- Closes a live invoice as expired or void; a paid or already closed one is left alone.
-- name: CloseInvoice :execrows
UPDATE invoices
SET status = @status, updated_at = @now::timestamptz
WHERE id = @id AND status IN ('draft', 'open');
