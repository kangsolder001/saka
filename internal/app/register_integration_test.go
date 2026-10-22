//go:build integration

package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/auth"
)

const (
	registerPath = "/api/v1/auth/register"
	traceparent  = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	traceID      = "4bf92f3577b34da6a3ce929d0e0e4736"
)

func registerBody(email string) string {
	return `{"email":"` + email + `","password":"correct-horse","name":"Ann","organization_name":"Acme"}`
}

func TestRegister_EndToEnd(t *testing.T) {
	s := newStack(t)

	res := s.post(registerPath, registerBody("Ann@Example.com"), map[string]string{"traceparent": traceparent})

	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	assert.Equal(t, traceID, res.Header().Get("X-Trace-Id"))
	body := res.json()["user"].(map[string]any)
	assert.Equal(t, "ann@example.com", body["email"])
	assert.Equal(t, false, body["email_verified"])

	// The user was stored with an Argon2id hash, never the password.
	var hash string
	require.NoError(t, s.queryRow("SELECT password_hash FROM users WHERE email = 'ann@example.com'").Scan(&hash))
	assert.Contains(t, hash, "$argon2id$")
	assert.NotContains(t, hash, "correct-horse")

	// One verification email job exists, carrying the request's trace id and
	// a link whose token matches the stored hash.
	var payload []byte
	var jobTrace *string
	require.NoError(t, s.queryRow("SELECT payload, trace_id FROM outbox_jobs WHERE type = 'email.send'").Scan(&payload, &jobTrace))
	require.NotNil(t, jobTrace)
	assert.Equal(t, traceID, *jobTrace)

	var job struct {
		Template string `json:"template"`
		To       string `json:"to"`
		Data     struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(payload, &job))
	assert.Equal(t, "verify_email", job.Template)
	assert.Equal(t, "ann@example.com", job.To)
	assert.Equal(t, "Ann", job.Data.Name)

	link, err := url.Parse(job.Data.URL)
	require.NoError(t, err)
	assert.Equal(t, "https://app.example.com/verify-email", link.Scheme+"://"+link.Host+link.Path)
	var tokenRows int
	require.NoError(t, s.queryRow("SELECT count(*) FROM user_tokens WHERE type = 'email_verification' AND token_hash = $1",
		auth.HashToken(link.Query().Get("token"))).Scan(&tokenRows))
	assert.Equal(t, 1, tokenRows, "the emailed token is the one whose hash is stored")
}

func TestRegister_DuplicateEmailIsConflictIgnoringCaseAndLeavesNoExtraRows(t *testing.T) {
	s := newStack(t)
	require.Equal(t, http.StatusCreated, s.post(registerPath, registerBody("ann@example.com")).Code)

	res := s.post(registerPath, registerBody("ANN@example.com"))

	assert.Equal(t, http.StatusConflict, res.Code)
	assert.Contains(t, res.Body.String(), `"code":"CONFLICT"`)
	var users, tokens, jobs int
	require.NoError(t, s.queryRow(`SELECT (SELECT count(*) FROM users), (SELECT count(*) FROM user_tokens),
		(SELECT count(*) FROM outbox_jobs)`).Scan(&users, &tokens, &jobs))
	assert.Equal(t, []int{1, 1, 1}, []int{users, tokens, jobs}, "the failed attempt created nothing")
}

func TestRegister_InvalidInputCreatesNothing(t *testing.T) {
	s := newStack(t)

	res := s.post(registerPath, `{"email":"ann@example.com","password":"short","name":"Ann","organization_name":"Acme"}`)

	assert.Equal(t, http.StatusBadRequest, res.Code)
	var users int
	require.NoError(t, s.queryRow("SELECT count(*) FROM users").Scan(&users))
	assert.Zero(t, users)
}

func TestRegister_RegistrationIsAtomicWhenTheEmailStepFails(t *testing.T) {
	s := newStack(t)
	// Make the outbox insert fail: the user created earlier in the same
	// transaction must disappear with it.
	_, err := s.admin.Exec(context.Background(), "DROP TABLE outbox_jobs")
	require.NoError(t, err)

	res := s.post(registerPath, registerBody("ann@example.com"))

	assert.Equal(t, http.StatusInternalServerError, res.Code)
	var users, tokens, orgs, members int
	require.NoError(t, s.queryRow(`SELECT (SELECT count(*) FROM users), (SELECT count(*) FROM user_tokens),
		(SELECT count(*) FROM organizations), (SELECT count(*) FROM memberships)`).Scan(&users, &tokens, &orgs, &members))
	assert.Zero(t, users, "no user without its verification email")
	assert.Zero(t, tokens)
	assert.Zero(t, orgs, "nor an organization without its user")
	assert.Zero(t, members)
}

func TestRegister_CreatesTheUsersOwnOrganizationAsOwner(t *testing.T) {
	s := newStack(t)

	require.Equal(t, http.StatusCreated, s.post(registerPath, registerBody("ann@example.com")).Code)

	var name, slug, role string
	var ownerMatches bool
	require.NoError(t, s.queryRow(`
		SELECT o.name, o.slug, m.role, o.owner_id = u.id
		FROM users u
		JOIN memberships m ON m.user_id = u.id
		JOIN organizations o ON o.id = m.organization_id
		WHERE u.email = 'ann@example.com'`).Scan(&name, &slug, &role, &ownerMatches))
	assert.Equal(t, "Acme", name)
	assert.Equal(t, "acme", slug)
	assert.Equal(t, "owner", role)
	assert.True(t, ownerMatches)
}

func TestRegister_TwoUsersWithTheSameOrganizationNameGetDistinctSlugs(t *testing.T) {
	s := newStack(t)

	require.Equal(t, http.StatusCreated, s.post(registerPath, registerBody("ann@example.com")).Code)
	require.Equal(t, http.StatusCreated, s.post(registerPath, registerBody("bob@example.com")).Code)

	var distinct int
	require.NoError(t, s.queryRow("SELECT count(DISTINCT slug) FROM organizations").Scan(&distinct))
	assert.Equal(t, 2, distinct)
}

func TestRegister_DuplicateEmailCreatesNoOrganization(t *testing.T) {
	s := newStack(t)
	require.Equal(t, http.StatusCreated, s.post(registerPath, registerBody("ann@example.com")).Code)

	require.Equal(t, http.StatusConflict, s.post(registerPath, registerBody("ann@example.com")).Code)

	var orgs, members int
	require.NoError(t, s.queryRow("SELECT (SELECT count(*) FROM organizations), (SELECT count(*) FROM memberships)").Scan(&orgs, &members))
	assert.Equal(t, []int{1, 1}, []int{orgs, members})
}
