-- +goose Up
-- +goose StatementBegin
-- Plans (spec §4.7). They live in the database, not in code: the seed below is
-- what a fresh install ships with, and changing a plan is a migration (or an
-- admin tool later), never a deploy of constants. Plans are global, not tenant
-- data, so there is no row-level security here.
CREATE TABLE plans (
    id           uuid        PRIMARY KEY DEFAULT uuidv7(),
    code         text        NOT NULL UNIQUE,
    name         text        NOT NULL,
    -- Smallest currency unit; IDR has no decimals, so rupiah. 0 for Free.
    price_amount bigint      NOT NULL CHECK (price_amount >= 0),
    currency     text        NOT NULL DEFAULT 'IDR',
    interval     text        NOT NULL DEFAULT 'month' CHECK (interval IN ('month', 'year')),
    trial_days   integer     NOT NULL DEFAULT 0 CHECK (trial_days >= 0),
    -- Feature codes, e.g. ["export_csv", "audit_log"].
    features     jsonb       NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(features) = 'array'),
    -- Limit name -> maximum, e.g. {"members": 3}. -1 means unlimited.
    limits       jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(limits) = 'object'),
    -- An old plan is deactivated rather than deleted: subscriptions still point at it.
    is_active    boolean     NOT NULL DEFAULT true,
    sort_order   integer     NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- The initial catalogue (spec §9.3, example numbers).
INSERT INTO plans (code, name, price_amount, interval, trial_days, features, limits, sort_order) VALUES
    ('free',     'Free',     0,      'month', 0,  '[]'::jsonb,                          '{"members": 3}'::jsonb,  1),
    ('pro',      'Pro',      99000,  'month', 14, '["export_csv"]'::jsonb,              '{"members": 10}'::jsonb, 2),
    ('business', 'Business', 299000, 'month', 14, '["export_csv", "audit_log"]'::jsonb, '{"members": -1}'::jsonb, 3);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE plans;
-- +goose StatementEnd
