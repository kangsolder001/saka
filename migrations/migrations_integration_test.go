//go:build integration

package migrations_test

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/dbtest"
	"github.com/kangsolder001/saka/migrations"
)

func newConn(t *testing.T) (*pgx.Conn, string) {
	t.Helper()
	dsn := dbtest.NewDatabase(t)
	dbtest.Migrate(t, dsn)
	conn, err := pgx.Connect(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn, dsn
}

func insertUser(conn *pgx.Conn, email string) error {
	_, err := conn.Exec(context.Background(),
		"INSERT INTO users (email, password_hash, name) VALUES ($1, 'hash', 'Test')", email)
	return err
}

func TestUsers_Defaults(t *testing.T) {
	conn, _ := newConn(t)
	require.NoError(t, insertUser(conn, "a@example.com"))

	var (
		version   int
		status    string
		verified  *string
		createdOK bool
	)
	err := conn.QueryRow(context.Background(), `
		SELECT uuid_extract_version(id), status, email_verified_at::text, created_at <= now() AND updated_at <= now()
		FROM users`).Scan(&version, &status, &verified, &createdOK)

	require.NoError(t, err)
	assert.Equal(t, 7, version, "ids must be UUID v7")
	assert.Equal(t, "active", status)
	assert.Nil(t, verified)
	assert.True(t, createdOK)
}

func TestUsers_EmailIsCaseInsensitiveUnique(t *testing.T) {
	conn, _ := newConn(t)
	require.NoError(t, insertUser(conn, "Person@Example.com"))

	err := insertUser(conn, "person@example.COM")

	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23505", pgErr.Code, "unique_violation")
}

func TestUsers_RejectsUnknownStatus(t *testing.T) {
	conn, _ := newConn(t)

	_, err := conn.Exec(context.Background(),
		"INSERT INTO users (email, password_hash, name, status) VALUES ('b@example.com', 'h', 'n', 'banned')")

	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23514", pgErr.Code, "check_violation")
}

func TestUsers_RequiredColumns(t *testing.T) {
	conn, _ := newConn(t)

	_, err := conn.Exec(context.Background(), "INSERT INTO users (email) VALUES ('c@example.com')")

	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23502", pgErr.Code, "not_null_violation")
}

func TestMigrations_AreReversible(t *testing.T) {
	_, dsn := newConn(t)
	sqlDB, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	require.NoError(t, err)
	ctx := context.Background()

	_, err = provider.DownTo(ctx, 0)
	require.NoError(t, err)

	var exists bool
	require.NoError(t, sqlDB.QueryRowContext(ctx, "SELECT to_regclass('public.users') IS NOT NULL").Scan(&exists))
	assert.False(t, exists, "down must drop users")

	_, err = provider.Up(ctx)
	assert.NoError(t, err, "up must work again after down")
}

func TestOutboxJobs_Defaults(t *testing.T) {
	conn, _ := newConn(t)

	var (
		status   string
		attempts int
		version  int
		due      bool
		locked   *string
		trace    *string
	)
	err := conn.QueryRow(context.Background(), `
		INSERT INTO outbox_jobs (type) VALUES ('email.send')
		RETURNING status, attempts, uuid_extract_version(id), next_attempt_at <= now(), locked_at::text, trace_id`).
		Scan(&status, &attempts, &version, &due, &locked, &trace)

	require.NoError(t, err)
	assert.Equal(t, "pending", status)
	assert.Equal(t, 0, attempts)
	assert.Equal(t, 7, version)
	assert.True(t, due, "new jobs are due immediately")
	assert.Nil(t, locked)
	assert.Nil(t, trace)
}

func TestOutboxJobs_RejectsUnknownStatus(t *testing.T) {
	conn, _ := newConn(t)

	_, err := conn.Exec(context.Background(), "INSERT INTO outbox_jobs (type, status) VALUES ('x', 'stuck')")

	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23514", pgErr.Code, "check_violation")
}

func createUser(t *testing.T, conn *pgx.Conn, email string) string {
	t.Helper()
	var id string
	require.NoError(t, conn.QueryRow(context.Background(),
		"INSERT INTO users (email, password_hash, name) VALUES ($1, 'h', 'n') RETURNING id", email).Scan(&id))
	return id
}

func TestUserTokens_Constraints(t *testing.T) {
	conn, _ := newConn(t)
	userID := createUser(t, conn, "t@example.com")
	insert := func(tokenType, hash string) error {
		_, err := conn.Exec(context.Background(),
			"INSERT INTO user_tokens (user_id, type, token_hash, expires_at) VALUES ($1, $2, $3, now() + interval '1 hour')",
			userID, tokenType, hash)
		return err
	}

	require.NoError(t, insert("email_verification", "hash-1"))

	var pgErr *pgconn.PgError
	require.ErrorAs(t, insert("email_verification", "hash-1"), &pgErr)
	assert.Equal(t, "23505", pgErr.Code, "token_hash is unique")
	require.ErrorAs(t, insert("magic_link", "hash-2"), &pgErr)
	assert.Equal(t, "23514", pgErr.Code, "type is restricted")
}

func TestUserTokens_AreRemovedWithTheirUser(t *testing.T) {
	conn, _ := newConn(t)
	userID := createUser(t, conn, "cascade@example.com")
	_, err := conn.Exec(context.Background(),
		"INSERT INTO user_tokens (user_id, type, token_hash, expires_at) VALUES ($1, 'password_reset', 'h', now())", userID)
	require.NoError(t, err)
	_, err = conn.Exec(context.Background(),
		"INSERT INTO refresh_tokens (user_id, family_id, token_hash, expires_at) VALUES ($1, uuidv7(), 'r', now())", userID)
	require.NoError(t, err)

	_, err = conn.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID)
	require.NoError(t, err)

	var tokens, refresh int
	require.NoError(t, conn.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM user_tokens), (SELECT count(*) FROM refresh_tokens)").Scan(&tokens, &refresh))
	assert.Zero(t, tokens)
	assert.Zero(t, refresh)
}

func TestRefreshTokens_Defaults(t *testing.T) {
	conn, _ := newConn(t)
	userID := createUser(t, conn, "r@example.com")

	var (
		version int
		revoked *string
		replace *string
	)
	err := conn.QueryRow(context.Background(), `
		INSERT INTO refresh_tokens (user_id, family_id, token_hash, expires_at)
		VALUES ($1, uuidv7(), 'rt-1', now() + interval '30 days')
		RETURNING uuid_extract_version(id), revoked_at::text, replaced_by::text`, userID).Scan(&version, &revoked, &replace)

	require.NoError(t, err)
	assert.Equal(t, 7, version)
	assert.Nil(t, revoked)
	assert.Nil(t, replace)
}

func TestRefreshTokens_TokenHashIsUniqueAndReplacedByMustExist(t *testing.T) {
	conn, _ := newConn(t)
	userID := createUser(t, conn, "r2@example.com")
	_, err := conn.Exec(context.Background(),
		"INSERT INTO refresh_tokens (user_id, family_id, token_hash, expires_at) VALUES ($1, uuidv7(), 'same', now())", userID)
	require.NoError(t, err)

	var pgErr *pgconn.PgError
	_, err = conn.Exec(context.Background(),
		"INSERT INTO refresh_tokens (user_id, family_id, token_hash, expires_at) VALUES ($1, uuidv7(), 'same', now())", userID)
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23505", pgErr.Code)

	_, err = conn.Exec(context.Background(),
		"INSERT INTO refresh_tokens (user_id, family_id, token_hash, expires_at, replaced_by) VALUES ($1, uuidv7(), 'other', now(), uuidv7())", userID)
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23503", pgErr.Code, "replaced_by must reference an existing token")
}

func TestRateLimitHitsTableIsGoneAfterRateLimitingMovedToRedis(t *testing.T) {
	conn, _ := newConn(t)

	var exists bool
	require.NoError(t, conn.QueryRow(context.Background(), "SELECT to_regclass('public.rate_limit_hits') IS NOT NULL").Scan(&exists))

	assert.False(t, exists)
}

// createOrg inserts an organization and its owner membership in one
// transaction, the way the service does.
func createOrg(t *testing.T, conn *pgx.Conn, slug, ownerID string) string {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	var id string
	require.NoError(t, tx.QueryRow(ctx,
		"INSERT INTO organizations (name, slug, owner_id) VALUES ('Org', $1, $2) RETURNING id", slug, ownerID).Scan(&id))
	_, err = tx.Exec(ctx, "INSERT INTO memberships (organization_id, user_id, role) VALUES ($1, $2, 'owner')", id, ownerID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	return id
}

func requirePgCode(t *testing.T, err error, code string, msgAndArgs ...any) {
	t.Helper()
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr, msgAndArgs...)
	assert.Equal(t, code, pgErr.Code, msgAndArgs...)
}

func TestOrganizations_Defaults(t *testing.T) {
	conn, _ := newConn(t)
	owner := createUser(t, conn, "owner@example.com")

	orgID := createOrg(t, conn, "acme", owner)

	var (
		version int
		deleted *string
		logo    *string
	)
	require.NoError(t, conn.QueryRow(context.Background(),
		"SELECT uuid_extract_version(id), deleted_at::text, logo_url FROM organizations WHERE id = $1", orgID).Scan(&version, &deleted, &logo))
	assert.Equal(t, 7, version)
	assert.Nil(t, deleted)
	assert.Nil(t, logo)
}

func TestOrganizations_SlugIsCaseInsensitiveUnique(t *testing.T) {
	conn, _ := newConn(t)
	owner := createUser(t, conn, "owner@example.com")
	createOrg(t, conn, "Acme", owner)

	_, err := conn.Exec(context.Background(),
		"INSERT INTO organizations (name, slug, owner_id) VALUES ('Other', 'ACME', $1)", owner)

	requirePgCode(t, err, "23505")
}

func TestOrganizations_SlugStaysReservedAfterSoftDelete(t *testing.T) {
	conn, _ := newConn(t)
	owner := createUser(t, conn, "owner@example.com")
	orgID := createOrg(t, conn, "acme", owner)
	_, err := conn.Exec(context.Background(), "UPDATE organizations SET deleted_at = now() WHERE id = $1", orgID)
	require.NoError(t, err)

	_, err = conn.Exec(context.Background(),
		"INSERT INTO organizations (name, slug, owner_id) VALUES ('Squatter', 'acme', $1)", owner)

	requirePgCode(t, err, "23505")
}

func TestOrganizations_OwnerMustBeAMember(t *testing.T) {
	conn, _ := newConn(t)
	stranger := createUser(t, conn, "stranger@example.com")

	// The deferred foreign key only fires at commit.
	_, err := conn.Exec(context.Background(),
		"INSERT INTO organizations (name, slug, owner_id) VALUES ('Org', 'ownerless', $1)", stranger)

	requirePgCode(t, err, "23503", "an organization whose owner has no membership must not commit")
}

func TestOrganizations_OwnerCannotLeaveWithoutTransfer(t *testing.T) {
	conn, _ := newConn(t)
	owner := createUser(t, conn, "owner@example.com")
	orgID := createOrg(t, conn, "acme", owner)

	_, err := conn.Exec(context.Background(),
		"DELETE FROM memberships WHERE organization_id = $1 AND user_id = $2", orgID, owner)

	requirePgCode(t, err, "23503")
}

func TestOrganizations_OwnerIsRestrictedFromUserDeletion(t *testing.T) {
	conn, _ := newConn(t)
	owner := createUser(t, conn, "owner@example.com")
	createOrg(t, conn, "acme", owner)

	_, err := conn.Exec(context.Background(), "DELETE FROM users WHERE id = $1", owner)

	requirePgCode(t, err, "23001", "restrict_violation")
}

func TestMemberships_Constraints(t *testing.T) {
	conn, _ := newConn(t)
	owner := createUser(t, conn, "owner@example.com")
	member := createUser(t, conn, "member@example.com")
	other := createUser(t, conn, "other@example.com")
	orgID := createOrg(t, conn, "acme", owner)
	insert := func(userID, role string) error {
		_, err := conn.Exec(context.Background(),
			"INSERT INTO memberships (organization_id, user_id, role) VALUES ($1, $2, $3)", orgID, userID, role)
		return err
	}

	require.NoError(t, insert(member, "member"))

	requirePgCode(t, insert(member, "admin"), "23505", "a user is a member of an org at most once")
	requirePgCode(t, insert(other, "owner"), "23505", "an org has exactly one owner")
	requirePgCode(t, insert(other, "superuser"), "23514", "role is restricted")
}

func TestMemberships_AreRemovedWithTheirOrganization(t *testing.T) {
	conn, _ := newConn(t)
	owner := createUser(t, conn, "owner@example.com")
	member := createUser(t, conn, "member@example.com")
	orgID := createOrg(t, conn, "acme", owner)
	_, err := conn.Exec(context.Background(),
		"INSERT INTO memberships (organization_id, user_id, role) VALUES ($1, $2, 'member')", orgID, member)
	require.NoError(t, err)
	_, err = conn.Exec(context.Background(),
		"INSERT INTO invitations (organization_id, email, role, token_hash, invited_by, expires_at) VALUES ($1, 'x@example.com', 'member', 'h', $2, now())", orgID, owner)
	require.NoError(t, err)

	_, err = conn.Exec(context.Background(), "DELETE FROM organizations WHERE id = $1", orgID)
	require.NoError(t, err)

	var memberships, invitations int
	require.NoError(t, conn.QueryRow(context.Background(),
		"SELECT (SELECT count(*) FROM memberships), (SELECT count(*) FROM invitations)").Scan(&memberships, &invitations))
	assert.Zero(t, memberships)
	assert.Zero(t, invitations)
}

func TestMemberships_OwnershipTransferCommitsAtomically(t *testing.T) {
	conn, _ := newConn(t)
	ctx := context.Background()
	owner := createUser(t, conn, "owner@example.com")
	member := createUser(t, conn, "member@example.com")
	orgID := createOrg(t, conn, "acme", owner)
	_, err := conn.Exec(ctx, "INSERT INTO memberships (organization_id, user_id, role) VALUES ($1, $2, 'member')", orgID, member)
	require.NoError(t, err)

	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "UPDATE memberships SET role = 'admin' WHERE organization_id = $1 AND user_id = $2", orgID, owner)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "UPDATE memberships SET role = 'owner' WHERE organization_id = $1 AND user_id = $2", orgID, member)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "UPDATE organizations SET owner_id = $2 WHERE id = $1", orgID, member)
	require.NoError(t, err)

	require.NoError(t, tx.Commit(ctx))
}

func TestInvitations_Constraints(t *testing.T) {
	conn, _ := newConn(t)
	owner := createUser(t, conn, "owner@example.com")
	orgID := createOrg(t, conn, "acme", owner)
	insert := func(email, role, hash string) error {
		_, err := conn.Exec(context.Background(),
			"INSERT INTO invitations (organization_id, email, role, token_hash, invited_by, expires_at) VALUES ($1, $2, $3, $4, $5, now() + interval '7 days')",
			orgID, email, role, hash, owner)
		return err
	}

	require.NoError(t, insert("Guest@Example.com", "member", "h1"))

	requirePgCode(t, insert("guest@example.com", "admin", "h2"), "23505", "one open invitation per org and email, case-insensitive")
	requirePgCode(t, insert("new@example.com", "member", "h1"), "23505", "token_hash is unique")
	requirePgCode(t, insert("new@example.com", "owner", "h3"), "23514", "an invitation cannot grant ownership")
}

func TestInvitations_ClosedInvitationDoesNotBlockANewOne(t *testing.T) {
	conn, _ := newConn(t)
	ctx := context.Background()
	owner := createUser(t, conn, "owner@example.com")
	orgID := createOrg(t, conn, "acme", owner)
	insert := func(hash string) error {
		_, err := conn.Exec(ctx,
			"INSERT INTO invitations (organization_id, email, role, token_hash, invited_by, expires_at) VALUES ($1, 'g@example.com', 'member', $2, $3, now())",
			orgID, hash, owner)
		return err
	}
	require.NoError(t, insert("h1"))

	for i, closing := range []string{"accepted_at", "declined_at", "revoked_at"} {
		_, err := conn.Exec(ctx, "UPDATE invitations SET "+closing+" = now() WHERE token_hash = $1", "h"+string(rune('1'+i)))
		require.NoError(t, err)
		require.NoError(t, insert("h"+string(rune('2'+i))), "after %s the same email can be invited again", closing)
	}
}

func TestInvitations_EndExactlyOneWay(t *testing.T) {
	conn, _ := newConn(t)
	owner := createUser(t, conn, "owner@example.com")
	orgID := createOrg(t, conn, "acme", owner)

	_, err := conn.Exec(context.Background(),
		"INSERT INTO invitations (organization_id, email, role, token_hash, invited_by, expires_at, accepted_at, revoked_at) VALUES ($1, 'g@example.com', 'member', 'h', $2, now(), now(), now())",
		orgID, owner)

	requirePgCode(t, err, "23514")
}

// tenantTables lists every table the policy check below must cover: those with
// an organization_id column, plus organizations itself (its id is the tenant).
const tenantTablesSQL = `
	SELECT c.relname, c.relrowsecurity,
	       EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid) AS has_policy
	FROM pg_class c
	JOIN pg_namespace n ON n.oid = c.relnamespace AND n.nspname = 'public'
	WHERE c.relkind = 'r'
	  AND (c.relname = 'organizations'
	       OR EXISTS (SELECT 1 FROM pg_attribute a
	                  WHERE a.attrelid = c.oid AND a.attname = 'organization_id' AND NOT a.attisdropped))
	ORDER BY c.relname`

// A table that gains an organization_id column in a future migration must also
// get row-level security, or tenant isolation would rest on application
// queries alone. This turns "remember to add a policy" into a failing test.
func TestTenantTables_AllHaveRowLevelSecurityAndAPolicy(t *testing.T) {
	conn, _ := newConn(t)

	rows, err := conn.Query(context.Background(), tenantTablesSQL)
	require.NoError(t, err)
	defer rows.Close()

	var seen []string
	for rows.Next() {
		var name string
		var enabled, hasPolicy bool
		require.NoError(t, rows.Scan(&name, &enabled, &hasPolicy))
		seen = append(seen, name)
		assert.True(t, enabled, "%s: row-level security is not enabled", name)
		assert.True(t, hasPolicy, "%s: no policy defined", name)
	}
	require.NoError(t, rows.Err())
	assert.Subset(t, seen, []string{"organizations", "memberships", "invitations", "subscriptions", "invoices"})
}

// Every index on a tenant table that is meant for lookups starts with
// organization_id (CLAUDE.md): the policy predicate then becomes an Index Cond
// instead of a scan (docs/rls.md). Unique indexes that exist to enforce a
// global rule (organizations.slug, invitations.token_hash) are the exceptions.
func TestTenantTables_HaveAnIndexLedByOrganizationID(t *testing.T) {
	conn, _ := newConn(t)

	for table, column := range map[string]string{"memberships": "organization_id", "invitations": "organization_id", "subscriptions": "organization_id", "invoices": "organization_id"} {
		var n int
		err := conn.QueryRow(context.Background(), `
			SELECT count(*)
			FROM pg_index i
			JOIN pg_class t ON t.oid = i.indrelid
			JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = i.indkey[0]
			WHERE t.relname = $1 AND a.attname = $2`, table, column).Scan(&n)

		require.NoError(t, err)
		assert.Positive(t, n, "%s has no index starting with %s", table, column)
	}
}

func TestApplicationRole_ExistsAndIsRestricted(t *testing.T) {
	conn, _ := newConn(t)

	var super, bypass, login bool
	err := conn.QueryRow(context.Background(),
		"SELECT rolsuper, rolbypassrls, rolcanlogin FROM pg_roles WHERE rolname = 'saka_app'").Scan(&super, &bypass, &login)

	require.NoError(t, err)
	assert.False(t, super, "a superuser would ignore every policy")
	assert.False(t, bypass)
	assert.False(t, login, "it is only reachable by SET ROLE from the login user")
}

func TestPlans_SeedMatchesTheDocumentedCatalogue(t *testing.T) {
	conn, _ := newConn(t)

	type plan struct {
		code, name   string
		price        int64
		trial        int
		features     string
		members      int
		active       bool
		sortOrderSet bool
	}
	rows, err := conn.Query(context.Background(), `
		SELECT code, name, price_amount, trial_days, features::text, (limits->>'members')::int, is_active, sort_order > 0
		FROM plans ORDER BY sort_order`)
	require.NoError(t, err)
	got, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (plan, error) {
		var p plan
		err := r.Scan(&p.code, &p.name, &p.price, &p.trial, &p.features, &p.members, &p.active, &p.sortOrderSet)
		return p, err
	})
	require.NoError(t, err)

	assert.Equal(t, []plan{
		{"free", "Free", 0, 0, `[]`, 3, true, true},
		{"pro", "Pro", 99000, 14, `["export_csv"]`, 10, true, true},
		{"business", "Business", 299000, 14, `["export_csv", "audit_log"]`, -1, true, true},
	}, got)
}

func TestPlans_ConstraintsRejectNonsense(t *testing.T) {
	conn, _ := newConn(t)
	insert := func(extra string) error {
		_, err := conn.Exec(context.Background(), `INSERT INTO plans (code, name, price_amount `+extra)
		return err
	}

	assert.Error(t, insert(`) VALUES ('free', 'Duplicate code', 0)`), "codes are unique")
	assert.Error(t, insert(`) VALUES ('neg', 'Negative', -1)`), "no negative prices")
	assert.Error(t, insert(`, interval) VALUES ('wk', 'Weekly', 1, 'week')`), "month or year only")
	assert.Error(t, insert(`, trial_days) VALUES ('t', 'T', 1, -3)`), "no negative trial")
	assert.Error(t, insert(`, features) VALUES ('f', 'F', 1, '{"a":1}')`), "features is an array")
	assert.Error(t, insert(`, limits) VALUES ('l', 'L', 1, '[1]')`), "limits is an object")
	assert.NoError(t, insert(`) VALUES ('ok', 'Fine', 1)`))
}

func planID(t *testing.T, conn *pgx.Conn, code string) string {
	t.Helper()
	var id string
	require.NoError(t, conn.QueryRow(context.Background(), "SELECT id FROM plans WHERE code = $1", code).Scan(&id))
	return id
}

func insertSubscription(conn *pgx.Conn, orgID, planID, status, extraColumns, extraValues string) error {
	_, err := conn.Exec(context.Background(),
		"INSERT INTO subscriptions (organization_id, plan_id, status, current_period_start"+extraColumns+") VALUES ($1, $2, $3, now()"+extraValues+")",
		orgID, planID, status)
	return err
}

func TestSubscriptions_Constraints(t *testing.T) {
	conn, _ := newConn(t)
	owner := createUser(t, conn, "owner@example.com")
	orgID := createOrg(t, conn, "acme", owner)
	other := createOrg(t, conn, "other", createUser(t, conn, "other@example.com"))
	free, pro := planID(t, conn, "free"), planID(t, conn, "pro")

	assert.NoError(t, insertSubscription(conn, orgID, free, "active", "", ""), "an open-ended Free period has no end")
	requirePgCode(t, insertSubscription(conn, orgID, pro, "active", "", ""), "23505", "one subscription per organization")
	requirePgCode(t, insertSubscription(conn, other, pro, "paused", "", ""), "23514", "unknown status")
	requirePgCode(t, insertSubscription(conn, other, pro, "trialing", "", ""), "23514", "a trial needs an end")
	assert.NoError(t, insertSubscription(conn, other, pro, "trialing", ", trial_ends_at", ", now() + interval '14 days'"))
}

func TestSubscriptions_PlansAreReferencedNotDeleted(t *testing.T) {
	conn, _ := newConn(t)
	orgID := createOrg(t, conn, "acme", createUser(t, conn, "owner@example.com"))
	free := planID(t, conn, "free")
	require.NoError(t, insertSubscription(conn, orgID, free, "active", "", ""))

	_, err := conn.Exec(context.Background(), "DELETE FROM plans WHERE id = $1", free)

	requirePgCode(t, err, "23001", "a plan in use is deactivated, not deleted")
}

func TestSubscriptions_AreRemovedWithTheirOrganization(t *testing.T) {
	conn, _ := newConn(t)
	orgID := createOrg(t, conn, "acme", createUser(t, conn, "owner@example.com"))
	require.NoError(t, insertSubscription(conn, orgID, planID(t, conn, "free"), "active", "", ""))

	_, err := conn.Exec(context.Background(), "DELETE FROM organizations WHERE id = $1", orgID)

	require.NoError(t, err)
	var n int
	require.NoError(t, conn.QueryRow(context.Background(), "SELECT count(*) FROM subscriptions").Scan(&n))
	assert.Zero(t, n)
}

// Organizations created before the migration (Fase 4) must not be left without
// a subscription; deleted ones are not worth one.
func TestSubscriptions_ExistingOrganizationsAreBackfilledWithFree(t *testing.T) {
	dsn := dbtest.NewDatabase(t)
	sqlDB, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	require.NoError(t, err)
	ctx := context.Background()
	_, err = provider.UpTo(ctx, 8)
	require.NoError(t, err)
	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()
	live := createOrg(t, conn, "live", createUser(t, conn, "live@example.com"))
	gone := createOrg(t, conn, "gone", createUser(t, conn, "gone@example.com"))
	_, err = conn.Exec(ctx, "UPDATE organizations SET deleted_at = now() WHERE id = $1", gone)
	require.NoError(t, err)

	_, err = provider.Up(ctx)
	require.NoError(t, err)

	var plan, status string
	var start time.Time
	var end *time.Time
	require.NoError(t, conn.QueryRow(ctx, `
		SELECT p.code, s.status, s.current_period_start, s.current_period_end
		FROM subscriptions s JOIN plans p ON p.id = s.plan_id WHERE s.organization_id = $1`, live).Scan(&plan, &status, &start, &end))
	assert.Equal(t, "free", plan)
	assert.Equal(t, "active", status)
	assert.Nil(t, end, "Free never ends")
	var created time.Time
	require.NoError(t, conn.QueryRow(ctx, "SELECT created_at FROM organizations WHERE id = $1", live).Scan(&created))
	assert.True(t, created.Equal(start), "the period starts when the organization did")
	var n int
	require.NoError(t, conn.QueryRow(ctx, "SELECT count(*) FROM subscriptions WHERE organization_id = $1", gone).Scan(&n))
	assert.Zero(t, n)
}

// insertInvoice inserts an invoice with sensible defaults; extra overrides
// columns by name.
func insertInvoice(conn *pgx.Conn, orgID, subID, planID, number string, extra map[string]any) error {
	cols := map[string]any{
		"organization_id": orgID, "subscription_id": subID, "number": number, "kind": "checkout", "plan_id": planID,
		"amount": 99000, "currency": "IDR", "status": "open",
		"period_start": time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "period_end": time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		"due_at": time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC), "provider": "fake",
	}
	for k, v := range extra {
		cols[k] = v
	}
	names, holders, args := "", "", []any{}
	for k, v := range cols {
		if names != "" {
			names, holders = names+", ", holders+", "
		}
		args = append(args, v)
		names += k
		holders += "$" + strconv.Itoa(len(args))
	}
	_, err := conn.Exec(context.Background(), "INSERT INTO invoices ("+names+") VALUES ("+holders+")", args...)
	return err
}

func subscribedOrg(t *testing.T, conn *pgx.Conn, slug, planCode string) (orgID, subID, plan string) {
	t.Helper()
	orgID = createOrg(t, conn, slug, createUser(t, conn, slug+"@example.com"))
	plan = planID(t, conn, planCode)
	require.NoError(t, insertSubscription(conn, orgID, plan, "active", "", ""))
	require.NoError(t, conn.QueryRow(context.Background(), "SELECT id FROM subscriptions WHERE organization_id = $1", orgID).Scan(&subID))
	return orgID, subID, plan
}

func TestInvoices_Constraints(t *testing.T) {
	conn, _ := newConn(t)
	orgID, subID, pro := subscribedOrg(t, conn, "acme", "pro")

	assert.NoError(t, insertInvoice(conn, orgID, subID, pro, "INV-2026-000001", nil))
	requirePgCode(t, insertInvoice(conn, orgID, subID, pro, "INV-2026-000001", map[string]any{"status": "void"}), "23505", "numbers are unique")
	requirePgCode(t, insertInvoice(conn, orgID, subID, pro, "INV-2026-000002", map[string]any{"status": "refunded"}), "23514", "unknown status")
	requirePgCode(t, insertInvoice(conn, orgID, subID, pro, "INV-2026-000003", map[string]any{"kind": "other", "status": "void"}), "23514", "unknown kind")
	requirePgCode(t, insertInvoice(conn, orgID, subID, pro, "INV-2026-000004", map[string]any{"amount": 0, "status": "void"}), "23514", "an invoice bills something")
	requirePgCode(t, insertInvoice(conn, orgID, subID, pro, "INV-2026-000005", map[string]any{
		"status": "void", "period_end": time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}), "23514", "a period has a positive length")
	requirePgCode(t, insertInvoice(conn, orgID, subID, pro, "INV-2026-000006", map[string]any{"status": "paid"}), "23514", "paid needs paid_at")
	requirePgCode(t, insertInvoice(conn, orgID, subID, pro, "INV-2026-000007", map[string]any{"status": "void", "paid_at": time.Now()}), "23514", "only paid invoices have paid_at")
	assert.NoError(t, insertInvoice(conn, orgID, subID, pro, "INV-2026-000008", map[string]any{"status": "paid", "paid_at": time.Now()}))
}

// A subscription has at most one live checkout and one invoice per renewal
// period, so a request or a job that runs twice cannot bill twice.
func TestInvoices_NoDuplicateLiveCheckoutOrRenewalPeriod(t *testing.T) {
	conn, _ := newConn(t)
	orgID, subID, pro := subscribedOrg(t, conn, "acme", "pro")

	require.NoError(t, insertInvoice(conn, orgID, subID, pro, "INV-2026-000001", nil))
	requirePgCode(t, insertInvoice(conn, orgID, subID, pro, "INV-2026-000002", map[string]any{"status": "draft"}), "23505", "second live checkout")
	assert.NoError(t, insertInvoice(conn, orgID, subID, pro, "INV-2026-000003", map[string]any{"status": "void"}), "closed checkouts do not count")
	assert.NoError(t, insertInvoice(conn, orgID, subID, pro, "INV-2026-000004", map[string]any{"status": "paid", "paid_at": time.Now()}))

	require.NoError(t, insertInvoice(conn, orgID, subID, pro, "INV-2026-000005", map[string]any{"kind": "renewal"}))
	requirePgCode(t, insertInvoice(conn, orgID, subID, pro, "INV-2026-000006", map[string]any{"kind": "renewal"}), "23505", "same renewal period twice")
	requirePgCode(t, insertInvoice(conn, orgID, subID, pro, "INV-2026-000007", map[string]any{"kind": "renewal", "status": "expired"}), "23505", "an expired renewal is not issued again")
	assert.NoError(t, insertInvoice(conn, orgID, subID, pro, "INV-2026-000008", map[string]any{"kind": "renewal", "status": "void"}))
	assert.NoError(t, insertInvoice(conn, orgID, subID, pro, "INV-2026-000009", map[string]any{
		"kind": "renewal", "period_start": time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), "period_end": time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)}), "the next period is another invoice")
}

func TestInvoices_AreRemovedWithTheirOrganization(t *testing.T) {
	conn, _ := newConn(t)
	orgID, subID, pro := subscribedOrg(t, conn, "acme", "pro")
	require.NoError(t, insertInvoice(conn, orgID, subID, pro, "INV-2026-000001", nil))

	_, err := conn.Exec(context.Background(), "DELETE FROM organizations WHERE id = $1", orgID)

	require.NoError(t, err)
	var n int
	require.NoError(t, conn.QueryRow(context.Background(), "SELECT count(*) FROM invoices").Scan(&n))
	assert.Zero(t, n)
}

func TestPaymentEvents_AGatewayEventIsStoredOnce(t *testing.T) {
	conn, _ := newConn(t)
	insert := func(provider, eventID string) error {
		_, err := conn.Exec(context.Background(),
			"INSERT INTO payment_events (provider, event_id, payload) VALUES ($1, $2, '{}'::jsonb)", provider, eventID)
		return err
	}

	require.NoError(t, insert("midtrans", "evt-1"))

	requirePgCode(t, insert("midtrans", "evt-1"), "23505", "the same delivery twice")
	assert.NoError(t, insert("midtrans", "evt-2"))
	assert.NoError(t, insert("xendit", "evt-1"), "event ids are per provider")
}

// The application connects as saka_app (SET ROLE), which must be able to take
// invoice numbers: the sequence is created after the grants of migration 00007
// and relies on its default privileges.
func TestInvoiceNumberSequence_IsUsableByTheApplicationRole(t *testing.T) {
	conn, _ := newConn(t)
	ctx := context.Background()
	_, err := conn.Exec(ctx, "SET ROLE saka_app")
	require.NoError(t, err)

	var first, second int64
	require.NoError(t, conn.QueryRow(ctx, "SELECT nextval('invoice_number_seq')").Scan(&first))
	require.NoError(t, conn.QueryRow(ctx, "SELECT nextval('invoice_number_seq')").Scan(&second))

	assert.Equal(t, int64(1), first)
	assert.Equal(t, int64(2), second)
	var n int
	require.NoError(t, conn.QueryRow(ctx, "SELECT count(*) FROM payment_events").Scan(&n), "the application role may use the webhook table")
}

// A rolled back transaction keeps the number it took: numbers are never
// reused, and a failed checkout leaves a gap (documented in the migration).
func TestInvoiceNumberSequence_NumbersAreNeverReusedAfterARollback(t *testing.T) {
	conn, _ := newConn(t)
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	var taken int64
	require.NoError(t, tx.QueryRow(ctx, "SELECT nextval('invoice_number_seq')").Scan(&taken))
	require.NoError(t, tx.Rollback(ctx))

	var next int64
	require.NoError(t, conn.QueryRow(ctx, "SELECT nextval('invoice_number_seq')").Scan(&next))

	assert.Greater(t, next, taken)
}
