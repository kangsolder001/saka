-- +goose Up
-- +goose StatementBegin
-- Row-level security for the tenant tables (spec §7.3, docs/rls.md).
--
-- The application connects and then runs SET ROLE saka_app, a role that owns
-- nothing and has no BYPASSRLS, so the policies below always apply to it. The
-- table owner (migrations) is not subject to them.
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'saka_app') THEN
        CREATE ROLE saka_app NOLOGIN;
    END IF;
END
$$;

GRANT USAGE ON SCHEMA public TO saka_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO saka_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO saka_app;
-- Tables created by later migrations get the same grants automatically.
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO saka_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO saka_app;

-- The tenant of a transaction is app.current_org (db.WithTx / db.PinTenant);
-- app.bypass_rls = 'on' marks a system transaction (db.WithSystemTx). Both are
-- transaction-local. NULLIF turns the '' a connection reports after the setting
-- ended into NULL, and the scalar sub-select makes PostgreSQL evaluate the
-- setting once per query instead of once per row (both from the spike).
--
-- One policy covers every command: USING doubles as WITH CHECK, so inserting a
-- row for another tenant is rejected too.
ALTER TABLE organizations ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON organizations
    USING (
        id = (SELECT NULLIF(current_setting('app.current_org', true), '')::uuid)
        OR (SELECT COALESCE(current_setting('app.bypass_rls', true), '') = 'on')
    );

ALTER TABLE memberships ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON memberships
    USING (
        organization_id = (SELECT NULLIF(current_setting('app.current_org', true), '')::uuid)
        OR (SELECT COALESCE(current_setting('app.bypass_rls', true), '') = 'on')
    );

ALTER TABLE invitations ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON invitations
    USING (
        organization_id = (SELECT NULLIF(current_setting('app.current_org', true), '')::uuid)
        OR (SELECT COALESCE(current_setting('app.bypass_rls', true), '') = 'on')
    );
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP POLICY tenant_isolation ON invitations;
ALTER TABLE invitations DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON memberships;
ALTER TABLE memberships DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON organizations;
ALTER TABLE organizations DISABLE ROW LEVEL SECURITY;
-- The saka_app role and its grants stay: roles are cluster-wide and other
-- databases on the same server may still use it.
-- +goose StatementEnd
