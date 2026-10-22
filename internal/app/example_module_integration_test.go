//go:build integration

package app_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/app"
	"github.com/kangsolder001/saka/internal/entitlement"
	"github.com/kangsolder001/saka/internal/org"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/httpx"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

// This file is the proof behind docs/panduan-modul-baru.md: it builds the
// "projects" example module of the guide, step by step, on top of the real
// router, and shows that the recipe gives tenant isolation, the plan limit
// (serialized), role checks and feature gating. It lives in a test on purpose:
// nothing here ships in internal/. A real module would use sqlc queries in its
// own package instead of the inline SQL below.

// migrationProjects is the migration of the guide (Step 1).
const migrationProjects = `
CREATE TABLE projects (
    id              uuid        PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    name            text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    created_by      uuid        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    created_at      timestamptz NOT NULL DEFAULT now()
);
-- Leads with organization_id so the policy predicate is an Index Cond (docs/rls.md).
CREATE INDEX projects_organization_created_idx ON projects (organization_id, created_at DESC, id);
ALTER TABLE projects ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON projects
    USING (
        organization_id = (SELECT NULLIF(current_setting('app.current_org', true), '')::uuid)
        OR (SELECT COALESCE(current_setting('app.bypass_rls', true), '') = 'on')
    );`

const projectsLimit = "projects"

// projectsModule is the service of the guide (Steps 3-6).
type projectsModule struct {
	tx     *db.DB
	limits *entitlement.Service
	// slowWork widens the gap between the limit check and the insert, the way
	// real work (more queries, a call to another service) would, so a missing
	// lock shows up reliably in the race test.
	slowWork time.Duration
}

// count is the counter registered with entitlement (Step 4): real rows, no stored counter.
func (m *projectsModule) count(ctx context.Context, q db.DBTX, orgID uuid.UUID) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM projects WHERE organization_id = $1`, orgID).Scan(&n)
	return n, err
}

// create takes the tenant from the context, never from the request (Step 3),
// authorizes by role (Step 5) and checks the limit in the creating transaction (Step 4).
func (m *projectsModule) create(ctx context.Context, name string) (uuid.UUID, error) {
	if err := org.Authorize(ctx, org.ActionUpdateSettings); err != nil {
		return uuid.Nil, err
	}
	orgID, ok := reqctx.OrgID(ctx)
	if !ok {
		return uuid.Nil, apperr.NotFound()
	}
	userID, ok := reqctx.UserID(ctx)
	if !ok {
		return uuid.Nil, apperr.Unauthenticated()
	}
	var id uuid.UUID
	err := m.tx.WithTx(ctx, func(tx pgx.Tx) error {
		// Serialize creators of this organization, then check, then insert.
		if err := org.LockOrganization(ctx, tx, orgID); err != nil {
			return err
		}
		if err := m.limits.CheckLimit(ctx, tx, orgID, projectsLimit, 1); err != nil {
			return err
		}
		time.Sleep(m.slowWork)
		return tx.QueryRow(ctx,
			`INSERT INTO projects (organization_id, name, created_by) VALUES ($1, $2, $3) RETURNING id`,
			orgID, name, userID).Scan(&id)
	})
	return id, err
}

func (m *projectsModule) list(ctx context.Context) ([]string, error) {
	if err := org.Authorize(ctx, org.ActionView); err != nil {
		return nil, err
	}
	orgID, ok := reqctx.OrgID(ctx)
	if !ok {
		return nil, apperr.NotFound()
	}
	names := []string{}
	err := m.tx.WithTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT name FROM projects WHERE organization_id = $1 ORDER BY created_at, id`, orgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				return err
			}
			names = append(names, n)
		}
		return rows.Err()
	})
	return names, err
}

// routes mounts the module on the tenant group (Step 6); the group already ran
// authentication and the tenant middleware.
func (m *projectsModule) routes(rg *gin.RouterGroup, entitlements *entitlement.Service) {
	rg.POST("/projects", func(c *gin.Context) {
		var req struct {
			Name string `json:"name" validate:"required,max=100"`
		}
		if !httpx.BindJSON(c, &req) {
			return
		}
		id, err := m.create(c.Request.Context(), req.Name)
		if err != nil {
			httpx.Error(c, err)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"project": gin.H{"id": id, "name": req.Name}})
	})
	rg.GET("/projects", func(c *gin.Context) {
		names, err := m.list(c.Request.Context())
		if err != nil {
			httpx.Error(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": names})
	})
	// A route that only some plans may use (Step 7).
	rg.GET("/projects/export", entitlements.RequireFeature("export_csv"), func(c *gin.Context) {
		c.String(http.StatusOK, "name\n")
	})
}

// exampleStack builds the stack with the module installed: the migration
// applied, the counter registered, the plan limits seeded, routes mounted.
func exampleStack(t *testing.T) (*stack, *projectsModule) {
	t.Helper()
	mod := &projectsModule{}
	s := newStack(t, func(o *app.Options) {
		o.TenantRoutes = func(rg *gin.RouterGroup, entitlements *entitlement.Service) {
			mod.limits = entitlements
			entitlements.RegisterCounter(projectsLimit, mod.count)
			mod.routes(rg, entitlements)
		}
	})
	mod.tx = s.db
	_, err := s.admin.Exec(context.Background(), migrationProjects)
	require.NoError(t, err)
	// Plans live in the database (Step 4): Free allows two projects, Pro is unlimited.
	_, err = s.admin.Exec(context.Background(), `
		UPDATE plans SET limits = limits || '{"projects": 2}'::jsonb WHERE code = 'free';
		UPDATE plans SET limits = limits || '{"projects": -1}'::jsonb WHERE code = 'pro'`)
	require.NoError(t, err)
	return s, mod
}

func (tm team) projectsPath() string { return orgPath(tm.orgID) + "/projects" }

func (s *stack) createProject(access string, tm team, name string) response {
	s.t.Helper()
	return s.post(tm.projectsPath(), `{"name":"`+name+`"}`, bearer(access))
}

func TestExampleModule_TheMigrationFollowsTheTenantRules(t *testing.T) {
	s, _ := exampleStack(t)

	var rls, hasPolicy bool
	require.NoError(t, s.queryRow(`
		SELECT c.relrowsecurity, EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid)
		FROM pg_class c WHERE c.relname = 'projects'`).Scan(&rls, &hasPolicy))
	assert.True(t, rls, "row-level security is on")
	assert.True(t, hasPolicy, "and there is a policy")
	var ledByOrg int
	require.NoError(t, s.queryRow(`
		SELECT count(*) FROM pg_index i
		JOIN pg_class t ON t.oid = i.indrelid
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = i.indkey[0]
		WHERE t.relname = 'projects' AND a.attname = 'organization_id'`).Scan(&ledByOrg))
	assert.Positive(t, ledByOrg, "an index leads with organization_id")
}

func TestExampleModule_TheFreePlanLimitIsEnforcedInTheCreatingTransaction(t *testing.T) {
	s, _ := exampleStack(t)
	tm := s.newTeam()

	require.Equal(t, http.StatusCreated, s.createProject(tm.owner, tm, "one").Code)
	require.Equal(t, http.StatusCreated, s.createProject(tm.admin, tm, "two").Code)
	third := s.createProject(tm.owner, tm, "three")

	require.Equal(t, http.StatusPaymentRequired, third.Code, third.Body.String())
	assert.Equal(t, "PLAN_LIMIT_REACHED", errorOf(third)["code"])
	assert.Equal(t, map[string]any{"limit": "projects", "max": 2.0, "used": 2.0, "plan": "free", "upgrade_to": "pro"}, detailsOf(third))
	var n int
	require.NoError(t, s.queryRow(`SELECT count(*) FROM projects`).Scan(&n))
	assert.Equal(t, 2, n, "the refused request created nothing")

	s.setPlan(tm.orgID, "pro", "active")
	assert.Equal(t, http.StatusCreated, s.createProject(tm.owner, tm, "three").Code, "upgrading lifts the limit")
	s.setPlan(tm.orgID, "pro", "expired")
	assert.Equal(t, http.StatusPaymentRequired, s.createProject(tm.owner, tm, "four").Code,
		"an expired subscription is back to Free: three projects exist, so nothing new, but nothing is deleted")
	assert.Equal(t, http.StatusOK, s.get(tm.projectsPath(), bearer(tm.owner)).Code)
}

func TestExampleModule_RacingCreatorsOfTheLastSlotLetExactlyOneThrough(t *testing.T) {
	s, mod := exampleStack(t)
	tm := s.newTeam()
	require.Equal(t, http.StatusCreated, s.createProject(tm.owner, tm, "first").Code) // one slot left
	mod.slowWork = 100 * time.Millisecond

	const racers = 6
	codes := make(chan int, racers)
	var wg sync.WaitGroup
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- s.createProject(tm.owner, tm, "racer"+string(rune('a'+i))).Code
		}()
	}
	wg.Wait()
	close(codes)

	created, refused := 0, 0
	for code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusPaymentRequired:
			refused++
		}
	}
	assert.Equal(t, 1, created)
	assert.Equal(t, racers-1, refused)
	var n int
	require.NoError(t, s.queryRow(`SELECT count(*) FROM projects`).Scan(&n))
	assert.Equal(t, 2, n, "the limit held under concurrency")
}

func TestExampleModule_OrganizationsNeverSeeEachOthersProjects(t *testing.T) {
	s, mod := exampleStack(t)
	tm := s.newTeam() // owner, admin, member of A; the outsider owns organization B
	outsider := team{orgID: s.firstOrg("outsider@example.com"), owner: tm.outside}
	require.Equal(t, http.StatusCreated, s.createProject(tm.owner, tm, "alpha").Code)
	require.Equal(t, http.StatusCreated, s.createProject(outsider.owner, outsider, "bravo").Code)

	list := func(path, access string) []any { return s.get(path, bearer(access)).json()["items"].([]any) }
	assert.Equal(t, []any{"alpha"}, list(tm.projectsPath(), tm.member), "a plain member of A sees A's projects")
	assert.Equal(t, []any{"bravo"}, list(outsider.projectsPath(), outsider.owner))

	// B's owner using A's URL, or A's owner using B's: 404, as for every tenant route.
	assert.Equal(t, http.StatusNotFound, s.get(tm.projectsPath(), bearer(outsider.owner)).Code)
	assert.Equal(t, http.StatusNotFound, s.createProject(outsider.owner, tm, "intruder").Code)
	assert.Equal(t, http.StatusNotFound, s.get(outsider.projectsPath(), bearer(tm.owner)).Code)

	// The second layer: even a query that forgot its WHERE only sees the tenant of the transaction.
	aID, err := uuid.Parse(tm.orgID)
	require.NoError(t, err)
	ctxA := reqctx.WithOrgID(context.Background(), aID)
	var seen int
	require.NoError(t, mod.tx.WithTx(ctxA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctxA, `SELECT count(*) FROM projects`).Scan(&seen)
	}))
	assert.Equal(t, 1, seen, "row-level security hides B's row from an unfiltered query inside A's transaction")
	require.NoError(t, mod.tx.WithTx(context.Background(), func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM projects`).Scan(&seen)
	}))
	assert.Zero(t, seen, "and with no tenant at all it sees nothing")
}

func TestExampleModule_RolesAreCheckedInTheServiceAndTheRoute(t *testing.T) {
	s, _ := exampleStack(t)
	tm := s.newTeam()

	assert.Equal(t, http.StatusForbidden, s.createProject(tm.member, tm, "nope").Code, "members read, they do not write")
	assert.Equal(t, http.StatusCreated, s.createProject(tm.admin, tm, "ok").Code)
	assert.Equal(t, http.StatusOK, s.get(tm.projectsPath(), bearer(tm.member)).Code)
	assert.Equal(t, http.StatusUnauthorized, s.get(tm.projectsPath(), nil).Code)
	assert.Equal(t, http.StatusBadRequest, s.post(tm.projectsPath(), `{"name":"","extra":1}`, bearer(tm.owner)).Code)
}

func TestExampleModule_FeatureGatedRoutesFollowThePlan(t *testing.T) {
	s, _ := exampleStack(t)
	tm := s.newTeam()
	path := tm.projectsPath() + "/export"

	free := s.get(path, bearer(tm.owner))
	require.Equal(t, http.StatusForbidden, free.Code)
	assert.Equal(t, "PLAN_FEATURE_UNAVAILABLE", errorOf(free)["code"])

	s.setPlan(tm.orgID, "pro", "active")
	assert.Equal(t, http.StatusOK, s.get(path, bearer(tm.owner)).Code)
}
