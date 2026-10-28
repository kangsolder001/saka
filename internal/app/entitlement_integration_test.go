//go:build integration

package app_test

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// soloTeam is a verified owner alone in a new organization, which is on Free
// (three seats).
func (s *stack) soloTeam() team {
	s.t.Helper()
	s.registered("owner@example.com")
	s.verify("owner@example.com")
	return team{orgID: s.firstOrg("owner@example.com"), owner: s.loggedIn("owner@example.com")}
}

// inviteAs invites email as the owner and returns the response.
func (s *stack) inviteAs(token string, tm team, email string) response {
	s.t.Helper()
	return s.post(tm.invitationsPath(), inviteBody(email, "member"), bearer(token))
}

func errorOf(res response) map[string]any {
	return res.json()["error"].(map[string]any)
}

func detailsOf(res response) map[string]any {
	return errorOf(res)["details"].(map[string]any)
}

// seats counts members plus live invitations of the organization at the
// stack's current (fake) time, straight from the tables.
func (s *stack) seats(orgID string) int {
	s.t.Helper()
	var n int
	require.NoError(s.t, s.queryRow(`
		SELECT (SELECT count(*) FROM memberships WHERE organization_id = $1)
		     + (SELECT count(*) FROM invitations WHERE organization_id = $1
		        AND accepted_at IS NULL AND declined_at IS NULL AND revoked_at IS NULL AND expires_at > $2)`,
		orgID, s.clock.Now()).Scan(&n))
	return n
}

// fillFree invites guests until the three Free seats are used.
func (s *stack) fillFree(tm team, guests ...string) {
	s.t.Helper()
	for _, g := range guests {
		require.Equal(s.t, http.StatusCreated, s.inviteAs(tm.owner, tm, g).Code, g)
	}
	require.Equal(s.t, 3, s.seats(tm.orgID))
}

func TestInvite_BeyondTheFreeLimitIsRefusedWithUpgradeDetails(t *testing.T) {
	s := newStack(t)
	tm := s.soloTeam()
	s.fillFree(tm, "a@example.com", "b@example.com")

	res := s.inviteAs(tm.owner, tm, "c@example.com")

	require.Equal(t, http.StatusPaymentRequired, res.Code, res.Body.String())
	assert.Equal(t, "PLAN_LIMIT_REACHED", errorOf(res)["code"])
	assert.Equal(t, "The Free plan allows up to 3 members.", errorOf(res)["message"])
	assert.Equal(t, map[string]any{
		"limit": "members", "max": 3.0, "used": 3.0, "plan": "free", "upgrade_to": "pro",
	}, detailsOf(res))
	assert.Equal(t, 2, s.openInvitations(tm.orgID), "nothing was created")
	assert.Zero(t, s.emailsTo("c@example.com", "invitation"), "and nothing was sent")
}

func TestInvite_MembersAndPendingInvitationsShareTheLimit(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam() // owner, admin and member: Free is already full
	s.verify("owner@example.com")

	res := s.inviteAs(tm.owner, tm, "a@example.com")

	require.Equal(t, http.StatusPaymentRequired, res.Code, res.Body.String())
	assert.EqualValues(t, 3, detailsOf(res)["used"])
}

func TestInvite_RefreshingALiveInvitationAtTheLimitIsNotANewSeat(t *testing.T) {
	s := newStack(t)
	tm := s.soloTeam()
	s.fillFree(tm, "a@example.com", "b@example.com")

	res := s.inviteAs(tm.owner, tm, "A@example.com")

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	assert.Equal(t, 3, s.seats(tm.orgID))
}

func TestInvite_ResendingALiveInvitationAtTheLimitIsNotANewSeat(t *testing.T) {
	s := newStack(t)
	tm := s.soloTeam()
	s.fillFree(tm, "a@example.com", "b@example.com")
	id := s.openInvitationID(tm.orgID, "a@example.com")

	res := s.post(tm.invitationPath(id)+"/resend", ``, bearer(tm.owner))

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	assert.Equal(t, 3, s.seats(tm.orgID))
}

func (s *stack) openInvitationID(orgID, email string) string {
	s.t.Helper()
	var id string
	require.NoError(s.t, s.queryRow(`SELECT id::text FROM invitations WHERE organization_id = $1 AND email = $2
		AND accepted_at IS NULL AND declined_at IS NULL AND revoked_at IS NULL`, orgID, email).Scan(&id))
	return id
}

func TestInvite_SeatsComeBackWhenInvitationsAreRevokedDeclinedOrExpire(t *testing.T) {
	t.Run("revoked", func(t *testing.T) {
		s := newStack(t)
		tm := s.soloTeam()
		s.fillFree(tm, "a@example.com", "b@example.com")
		require.Equal(t, http.StatusPaymentRequired, s.inviteAs(tm.owner, tm, "c@example.com").Code)

		id := s.openInvitationID(tm.orgID, "a@example.com")
		require.Equal(t, http.StatusNoContent, s.do(http.MethodDelete, tm.invitationPath(id), "", bearer(tm.owner)).Code)

		assert.Equal(t, http.StatusCreated, s.inviteAs(tm.owner, tm, "c@example.com").Code)
	})
	t.Run("declined", func(t *testing.T) {
		s := newStack(t)
		tm := s.soloTeam()
		_, token := s.invite(tm, "a@example.com", "member")
		s.fillFree(tm, "b@example.com")
		require.Equal(t, http.StatusPaymentRequired, s.inviteAs(tm.owner, tm, "c@example.com").Code)

		require.Equal(t, http.StatusNoContent, s.post(invitationLinkPath(token)+"/decline", ``).Code)

		assert.Equal(t, http.StatusCreated, s.inviteAs(tm.owner, tm, "c@example.com").Code)
	})
	t.Run("expired", func(t *testing.T) {
		s := newStack(t)
		tm := s.soloTeam()
		s.fillFree(tm, "a@example.com", "b@example.com")

		s.clock.Advance(InvitationLifetime)
		tm.owner = s.loggedIn("owner@example.com")

		assert.Equal(t, 1, s.seats(tm.orgID), "only the owner is left")
		assert.Equal(t, http.StatusCreated, s.inviteAs(tm.owner, tm, "c@example.com").Code)
		assert.Equal(t, http.StatusCreated, s.inviteAs(tm.owner, tm, "d@example.com").Code)
		assert.Equal(t, http.StatusPaymentRequired, s.inviteAs(tm.owner, tm, "e@example.com").Code)
	})
}

func TestInvite_AnExpiredInvitationTakesASeatAgainWhenRefreshedOrResent(t *testing.T) {
	s := newStack(t)
	tm := s.soloTeam()
	s.fillFree(tm, "a@example.com", "b@example.com")
	expiredResend := s.openInvitationID(tm.orgID, "b@example.com")
	s.clock.Advance(InvitationLifetime)
	tm.owner = s.loggedIn("owner@example.com")
	// Two fresh invitations take the seats the expired ones gave up.
	require.Equal(t, http.StatusCreated, s.inviteAs(tm.owner, tm, "c@example.com").Code)
	require.Equal(t, http.StatusCreated, s.inviteAs(tm.owner, tm, "d@example.com").Code)

	refresh := s.inviteAs(tm.owner, tm, "a@example.com")
	resend := s.post(tm.invitationPath(expiredResend)+"/resend", ``, bearer(tm.owner))

	assert.Equal(t, http.StatusPaymentRequired, refresh.Code, "re-inviting an address whose invitation expired needs a seat")
	assert.Equal(t, http.StatusPaymentRequired, resend.Code, "so does resending it")
	assert.Equal(t, 3, s.seats(tm.orgID))
	assert.Equal(t, 1, s.emailsTo("a@example.com", "invitation"), "no new email went to a: the first one was the only one")

	// Once a seat is free the same request works.
	require.Equal(t, http.StatusNoContent, s.do(http.MethodDelete, tm.invitationPath(s.openInvitationID(tm.orgID, "c@example.com")), "", bearer(tm.owner)).Code)
	assert.Equal(t, http.StatusOK, s.inviteAs(tm.owner, tm, "a@example.com").Code)
	assert.Equal(t, 3, s.seats(tm.orgID))
}

func TestInvite_AcceptingAnInvitationAtTheLimitStillWorks(t *testing.T) {
	s := newStack(t)
	tm := s.soloTeam()
	s.registered("guest@example.com")
	s.verify("guest@example.com")
	_, token := s.invite(tm, "guest@example.com", "member")
	s.fillFree(tm, "b@example.com")

	res := s.post(invitationLinkPath(token)+"/accept", ``, bearer(s.loggedIn("guest@example.com")))

	require.Equal(t, http.StatusOK, res.Code, "the invitation already held the seat: "+res.Body.String())
	assert.Equal(t, 3, s.seats(tm.orgID), "a seat turned from invitation into membership")
	assert.Equal(t, http.StatusPaymentRequired, s.inviteAs(tm.owner, tm, "c@example.com").Code)
}

func TestInvite_UpgradingThePlanLiftsTheLimit(t *testing.T) {
	s := newStack(t)
	tm := s.soloTeam()
	s.fillFree(tm, "a@example.com", "b@example.com")
	require.Equal(t, http.StatusPaymentRequired, s.inviteAs(tm.owner, tm, "c@example.com").Code)

	s.setPlan(tm.orgID, "pro", "active")
	for i := 0; i < 7; i++ { // up to Pro's 10
		require.Equal(t, http.StatusCreated, s.inviteAs(tm.owner, tm, fmt.Sprintf("pro%d@example.com", i)).Code)
	}
	res := s.inviteAs(tm.owner, tm, "eleventh@example.com")

	require.Equal(t, http.StatusPaymentRequired, res.Code)
	assert.Equal(t, "The Pro plan allows up to 10 members.", errorOf(res)["message"])
	assert.Equal(t, "business", detailsOf(res)["upgrade_to"])

	s.setPlan(tm.orgID, "business", "active")
	assert.Equal(t, http.StatusCreated, s.inviteAs(tm.owner, tm, "eleventh@example.com").Code, "Business has no member limit")
}

func TestInvite_ASubscriptionThatNoLongerGrantsAccessFallsBackToTheFreeLimit(t *testing.T) {
	for _, status := range []string{"expired", "canceled"} {
		t.Run(status, func(t *testing.T) {
			s := newStack(t)
			tm := s.soloTeam()
			s.setPlan(tm.orgID, "pro", "active")
			for i := 0; i < 4; i++ { // five seats: fine on Pro, over Free's three
				require.Equal(t, http.StatusCreated, s.inviteAs(tm.owner, tm, fmt.Sprintf("g%d@example.com", i)).Code)
			}

			s.setPlan(tm.orgID, "pro", status)
			res := s.inviteAs(tm.owner, tm, "one-more@example.com")

			require.Equal(t, http.StatusPaymentRequired, res.Code, res.Body.String())
			assert.Equal(t, map[string]any{
				"limit": "members", "max": 3.0, "used": 5.0, "plan": "free", "upgrade_to": "pro",
			}, detailsOf(res), "the limits are Free's now")
			assert.Equal(t, 5, s.seats(tm.orgID), "nothing is deleted when access lapses")

			s.setPlan(tm.orgID, "pro", "active")
			assert.Equal(t, http.StatusCreated, s.inviteAs(tm.owner, tm, "one-more@example.com").Code, "paying again brings Pro back")
		})
	}
}

func TestInvite_PastDueKeepsThePlanOnlyDuringTheGracePeriod(t *testing.T) {
	s := newStack(t)
	tm := s.soloTeam()
	s.setPlan(tm.orgID, "pro", "active")
	for i := 0; i < 4; i++ {
		require.Equal(t, http.StatusCreated, s.inviteAs(tm.owner, tm, fmt.Sprintf("g%d@example.com", i)).Code)
	}
	graceEnd := s.clock.Now().Add(3 * 24 * time.Hour)
	s.setPlan(tm.orgID, "pro", "past_due")
	_, err := s.admin.Exec(s.ctx(), "UPDATE subscriptions SET grace_until = $2 WHERE organization_id = $1", tm.orgID, graceEnd)
	require.NoError(t, err)

	assert.Equal(t, http.StatusCreated, s.inviteAs(tm.owner, tm, "in-grace@example.com").Code, "still Pro inside the grace period")

	s.clock.Set(graceEnd)
	tm.owner = s.loggedIn("owner@example.com")
	res := s.inviteAs(tm.owner, tm, "after-grace@example.com")

	require.Equal(t, http.StatusPaymentRequired, res.Code, res.Body.String())
	assert.Equal(t, "free", detailsOf(res)["plan"])
}

func TestInvite_ParallelInvitationsAtTheLastSeatLetExactlyOneThrough(t *testing.T) {
	s := newStack(t)
	tm := s.soloTeam()
	s.registered("member@example.com")
	s.addMember(tm.orgID, "member@example.com", "member") // two of three seats

	codes := make([]int, 8)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = s.inviteAs(tm.owner, tm, fmt.Sprintf("racer%d@example.com", i)).Code
		}()
	}
	wg.Wait()

	created, refused := 0, 0
	for _, c := range codes {
		switch c {
		case http.StatusCreated:
			created++
		case http.StatusPaymentRequired:
			refused++
		default:
			t.Errorf("unexpected status %d", c)
		}
	}
	assert.Equal(t, 1, created, "one seat, one invitation")
	assert.Equal(t, len(codes)-1, refused)
	assert.Equal(t, 3, s.seats(tm.orgID), "the plan is never overshot")
}

func TestInvite_ParallelInvitationsToOneAddressAtTheLimitDoNotDoubleCount(t *testing.T) {
	s := newStack(t)
	tm := s.soloTeam()
	require.Equal(t, http.StatusCreated, s.inviteAs(tm.owner, tm, "a@example.com").Code) // two of three seats

	codes := make([]int, 6)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = s.inviteAs(tm.owner, tm, "same@example.com").Code
		}()
	}
	wg.Wait()

	created, refreshed := 0, 0
	for _, c := range codes {
		switch c {
		case http.StatusCreated:
			created++
		case http.StatusOK:
			refreshed++
		default:
			t.Errorf("unexpected status %d", c)
		}
	}
	assert.Equal(t, 1, created, "the first one creates the invitation")
	assert.Equal(t, len(codes)-1, refreshed, "the others refresh it instead of asking for more seats")
	assert.Equal(t, 3, s.seats(tm.orgID))
}
