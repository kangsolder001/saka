//go:build integration

package app_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/app"
	"github.com/kangsolder001/saka/internal/auth"
	"github.com/kangsolder001/saka/internal/platform/db"
)

func (s *stack) deleteMe(access, password string) response {
	s.t.Helper()
	return s.do(http.MethodDelete, mePath, `{"current_password":"`+password+`"}`, bearer(access))
}

func (s *stack) userRow(email string) (status string, deleted bool, found bool) {
	s.t.Helper()
	var deletedAt *string
	err := s.queryRow("SELECT status, deleted_at::text FROM users WHERE email = $1", email).Scan(&status, &deletedAt)
	if err != nil {
		return "", false, false
	}
	return status, deletedAt != nil, true
}

// refusingGuard stands in for the organization module saying "this user still
// owns an organization with other members".
type refusingGuard struct{ calls int }

func (g *refusingGuard) BeforeUserDeleted(context.Context, db.DBTX, uuid.UUID) error {
	g.calls++
	return auth.ErrOwnsSharedOrganization()
}

func TestDeleteAccount_EndToEnd(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	s.registered("bob@example.com")
	laptop := s.cookieLogin("ann@example.com")
	phone := s.cookieLogin("ann@example.com")
	bob := s.cookieLogin("bob@example.com")
	require.Equal(t, "ann@example.com", s.currentEmail(laptop.access), "primes the user cache")
	annID := s.userID("ann@example.com")

	res := s.deleteMe(laptop.access, "correct-horse")

	require.Equal(t, http.StatusNoContent, res.Code, res.Body.String())
	assert.False(t, s.accessWorks(laptop.access), "the deleting session is over")
	assert.False(t, s.accessWorks(phone.access), "so is every other session")
	assert.Equal(t, http.StatusUnauthorized, s.get(mePath, bearer(laptop.access)).Code, "the cached profile cannot keep it alive")
	refreshed, _ := s.refreshWithCookie(phone.refresh)
	assert.Equal(t, http.StatusUnauthorized, refreshed.Code)
	assert.Equal(t, http.StatusUnauthorized, s.loginStatus("ann@example.com", "correct-horse"), "the account cannot log in any more")
	status, deleted, found := s.userRow("deleted+" + annID + "@invalid")
	require.True(t, found, "the row is kept with an anonymized email")
	assert.Equal(t, "deleted", status)
	assert.True(t, deleted)
	assert.True(t, s.accessWorks(bob.access), "other users are untouched")
}

func (s *stack) userID(email string) string {
	s.t.Helper()
	var id string
	require.NoError(s.t, s.queryRow("SELECT id::text FROM users WHERE email = $1", email).Scan(&id))
	return id
}

func TestDeleteAccount_TheEmailCanBeUsedToSignUpAgain(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")
	require.Equal(t, http.StatusNoContent, s.deleteMe(access, "correct-horse").Code)

	res := s.post(registerPath, registerBody("ann@example.com"))

	assert.Equal(t, http.StatusCreated, res.Code, "no CONFLICT: the old account no longer holds the address")
	assert.Equal(t, http.StatusOK, s.loginStatus("ann@example.com", "correct-horse"))
}

func TestDeleteAccount_WrongPasswordDeletesNothing(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	laptop := s.cookieLogin("ann@example.com")

	res := s.deleteMe(laptop.access, "wrong-password")

	require.Equal(t, http.StatusBadRequest, res.Code, res.Body.String())
	assert.Contains(t, res.Body.String(), "current_password")
	assert.True(t, s.accessWorks(laptop.access))
	status, deleted, found := s.userRow("ann@example.com")
	assert.True(t, found)
	assert.Equal(t, "active", status)
	assert.False(t, deleted)
}

func TestDeleteAccount_GuessingThePasswordIsRateLimited(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")
	for range 5 {
		require.Equal(t, http.StatusBadRequest, s.deleteMe(access, "wrong-password").Code)
	}

	assert.Equal(t, http.StatusTooManyRequests, s.deleteMe(access, "correct-horse").Code, "even the right password is refused while locked")
	_, deleted, _ := s.userRow("ann@example.com")
	assert.False(t, deleted)
}

func TestDeleteAccount_RefusedByTheOrganizationModuleLeavesEverythingIntact(t *testing.T) {
	guard := &refusingGuard{}
	s := newStack(t, func(o *app.Options) { o.Organizations = guard })
	s.registered("ann@example.com")
	laptop := s.cookieLogin("ann@example.com")
	phone := s.cookieLogin("ann@example.com")

	res := s.deleteMe(laptop.access, "correct-horse")

	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	assert.Contains(t, res.Body.String(), "owns_shared_organization")
	assert.Equal(t, 1, guard.calls)
	status, deleted, _ := s.userRow("ann@example.com")
	assert.Equal(t, "active", status, "the user was not touched")
	assert.False(t, deleted)
	assert.True(t, s.accessWorks(laptop.access), "no session was revoked")
	assert.True(t, s.accessWorks(phone.access))
	assert.Equal(t, http.StatusOK, s.loginStatus("ann@example.com", "correct-horse"))
}

func TestDeleteAccount_RequiresAuthentication(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")

	res := s.do(http.MethodDelete, mePath, `{"current_password":"correct-horse"}`, nil)

	assert.Equal(t, http.StatusUnauthorized, res.Code)
	_, deleted, _ := s.userRow("ann@example.com")
	assert.False(t, deleted)
}

func (s *stack) orgDeleted(orgID string) bool {
	s.t.Helper()
	var deleted bool
	require.NoError(s.t, s.queryRow("SELECT deleted_at IS NOT NULL FROM organizations WHERE id = $1", orgID).Scan(&deleted))
	return deleted
}

func TestDeleteAccount_RealOrganizations_OwnerOfASharedOneIsRefusedAndNothingChanges(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam()

	res := s.deleteMe(tm.owner, "correct-horse")

	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	assert.Contains(t, res.Body.String(), "owns_shared_organization")
	status, deleted, _ := s.userRow("owner@example.com")
	assert.Equal(t, "active", status)
	assert.False(t, deleted)
	assert.False(t, s.orgDeleted(tm.orgID))
	assert.True(t, s.accessWorks(tm.owner), "the session survives a refused deletion")
	role, member := s.roleOf(tm.orgID, "owner@example.com")
	assert.True(t, member)
	assert.Equal(t, "owner", role)
}

func TestDeleteAccount_RealOrganizations_ASoleOwnerTakesTheirOrganizationAlong(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	orgID := s.firstOrg("ann@example.com")
	require.Equal(t, http.StatusNoContent, s.deleteMe(s.loggedIn("ann@example.com"), "correct-horse").Code)

	assert.True(t, s.orgDeleted(orgID))
}

func TestDeleteAccount_RealOrganizations_AMemberLeavesQuietly(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam()

	require.Equal(t, http.StatusNoContent, s.deleteMe(tm.member, "correct-horse").Code)

	assert.False(t, s.orgDeleted(tm.orgID))
	_, member := s.roleOf(tm.orgID, "member@example.com")
	assert.False(t, member)
	assert.Equal(t, http.StatusOK, s.get(orgPath(tm.orgID), bearer(tm.owner)).Code, "the others are undisturbed")
	assert.Len(t, s.get(tm.membersPath(), bearer(tm.owner)).json()["items"], 2)
}

func TestDeleteAccount_RealOrganizations_TransferringOwnershipUnblocksIt(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam()
	require.Equal(t, http.StatusConflict, s.deleteMe(tm.owner, "correct-horse").Code)
	require.Equal(t, http.StatusNoContent,
		s.post(tm.transferPath(), transferBody(s.userID("admin@example.com"), "correct-horse"), bearer(tm.owner)).Code)

	res := s.deleteMe(tm.owner, "correct-horse")

	require.Equal(t, http.StatusNoContent, res.Code, res.Body.String())
	assert.False(t, s.orgDeleted(tm.orgID), "the organization lives on under its new owner")
	assert.Equal(t, http.StatusOK, s.get(orgPath(tm.orgID), bearer(tm.admin)).Code)
}
