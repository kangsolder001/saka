-- +goose Up
-- +goose StatementBegin
-- Invoice numbers (spec §4.9, §14 #5): one global sequence, not per organization.
-- A sequence never hands out a number twice, and it is not rolled back with the
-- transaction that took the number, so a failed checkout may leave a gap in the
-- numbering. Gaps are harmless; reusing a number would not be.
CREATE SEQUENCE invoice_number_seq AS bigint START WITH 1 MINVALUE 1;

-- Invoices (spec §4.9): one per billing period, paid through the payment
-- gateway. Money is an integer in the smallest unit of the currency.
CREATE TABLE invoices (
    id                  uuid        PRIMARY KEY DEFAULT uuidv7(),
    organization_id     uuid        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    subscription_id     uuid        NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,
    number              text        NOT NULL UNIQUE,
    -- 'checkout': a purchase the organization asked for (first subscription,
    -- trial conversion, upgrade, subscribing again); the paid period starts when
    -- it is paid. 'renewal': the invoice for the next period, issued by the
    -- renewals job; the paid period follows the current one.
    kind                text        NOT NULL CHECK (kind IN ('checkout', 'renewal')),
    -- Snapshot of the plan being billed; price and currency are copied below
    -- and never read back from the catalogue.
    plan_id             uuid        NOT NULL REFERENCES plans (id) ON DELETE RESTRICT,
    amount              bigint      NOT NULL CHECK (amount > 0),
    currency            text        NOT NULL,
    status              text        NOT NULL CHECK (status IN ('draft', 'open', 'paid', 'expired', 'void')),
    period_start        timestamptz NOT NULL,
    period_end          timestamptz NOT NULL,
    due_at              timestamptz NOT NULL,
    paid_at             timestamptz,
    provider            text        NOT NULL,
    -- How the gateway knows the payment: Midtrans' order id at first, its
    -- transaction id once the gateway has reported one.
    provider_ref        text,
    payment_url         text,
    -- Reminders already sent (billing.reminders), so a job that runs again
    -- never sends the same one twice.
    reminder_3d_sent_at timestamptz,
    reminder_1d_sent_at timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CHECK (period_end > period_start),
    CHECK ((status = 'paid') = (paid_at IS NOT NULL))
);

-- Listing an organization's invoices, newest first; leads with organization_id
-- as every tenant index does (docs/rls.md).
CREATE INDEX invoices_organization_created_idx ON invoices (organization_id, created_at DESC, id DESC);
CREATE INDEX invoices_subscription_idx ON invoices (subscription_id);
-- The jobs that sweep open invoices (reminders, expiry, reconcile).
CREATE INDEX invoices_open_due_idx ON invoices (due_at) WHERE status IN ('draft', 'open');

-- A subscription never has two live checkouts, nor two invoices for the same
-- renewal period, so a request or a job that runs twice cannot bill twice. A
-- voided invoice does not count: the renewals job can issue it again.
CREATE UNIQUE INDEX invoices_one_live_checkout_idx ON invoices (subscription_id)
    WHERE kind = 'checkout' AND status IN ('draft', 'open');
CREATE UNIQUE INDEX invoices_one_renewal_per_period_idx ON invoices (subscription_id, period_start)
    WHERE kind = 'renewal' AND status <> 'void';

-- Same policy as the other tenant tables (migration 00007).
ALTER TABLE invoices ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON invoices
    USING (
        organization_id = (SELECT NULLIF(current_setting('app.current_org', true), '')::uuid)
        OR (SELECT COALESCE(current_setting('app.bypass_rls', true), '') = 'on')
    );

-- Webhook plumbing (spec §4.10): every notification the gateway sends, stored
-- before it is processed. UNIQUE (provider, event_id) makes a delivery that
-- arrives twice (gateways retry) one event. Global, not tenant data: a webhook
-- carries no tenant until it is matched to an invoice, so no organization_id
-- and no row-level security.
--
-- processed_at NULL: not processed yet, or it failed (error says why; the
-- gateway's retry processes it again). processed_at set with error set: it was
-- processed but needs a human (e.g. a payment for an invoice that was already
-- void).
CREATE TABLE payment_events (
    id           uuid        PRIMARY KEY DEFAULT uuidv7(),
    provider     text        NOT NULL,
    event_id     text        NOT NULL,
    payload      jsonb       NOT NULL,
    received_at  timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz,
    error        text,
    UNIQUE (provider, event_id)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE payment_events;
DROP POLICY tenant_isolation ON invoices;
DROP TABLE invoices;
DROP SEQUENCE invoice_number_seq;
-- +goose StatementEnd
