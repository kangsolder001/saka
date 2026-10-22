//go:build integration

package app_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (tm team) subscriptionActionPath(action string) string {
	return orgPath(tm.orgID) + "/subscription/" + action
}

// payingTeam is a billingTeam on an active Pro subscription whose period ends in 10 days.
func (s *stack) payingTeam() team {
	s.t.Helper()
	tm := s.billingTeam()
	s.setPlan(tm.orgID, "pro", "active")
	_, err := s.admin.Exec(s.ctx(), `UPDATE subscriptions SET current_period_start = $2::timestamptz - interval '20 days',
		current_period_end = $2::timestamptz + interval '10 days' WHERE organization_id = $1`, tm.orgID, s.clock.Now())
	require.NoError(s.t, err)
	return tm
}

func TestChangePlan_EndToEnd_AnUpgradeReturnsAnInvoiceToPay(t *testing.T) {
	s := newStack(t)
	tm := s.payingTeam()

	res := s.post(tm.subscriptionActionPath("change-plan"), `{"plan":"business"}`, bearer(tm.owner))

	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	body := res.json()
	assert.Equal(t, "business", body["invoice"].(map[string]any)["plan"])
	assert.Equal(t, false, body["scheduled"])
	assert.Equal(t, "pro", body["subscription"].(map[string]any)["plan"], "the plan only changes once the invoice is paid")

	// Paying it moves the organization to Business from the payment time.
	number := body["invoice"].(map[string]any)["number"].(string)
	require.Equal(t, http.StatusOK, s.paymentWebhook(paid("evt-1", number, 299000)).Code)
	plan, status := s.subscriptionOf(tm.orgID)
	assert.Equal(t, "business", plan)
	assert.Equal(t, "active", status)
}

func TestChangePlan_EndToEnd_ADowngradeIsScheduledAndShowsInTheSubscription(t *testing.T) {
	s := newStack(t)
	tm := s.payingTeam()
	s.setPlan(tm.orgID, "business", "active")
	// Fill the seats beyond what Pro allows (10): the warning names the limit.
	s.fillMembers(tm.orgID, 12)

	res := s.post(tm.subscriptionActionPath("change-plan"), `{"plan":"pro"}`, bearer(tm.owner))

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	body := res.json()
	assert.Equal(t, true, body["scheduled"])
	assert.Nil(t, body["invoice"])
	assert.Equal(t, []any{map[string]any{"limit": "members", "used": 12.0, "max": 10.0}}, body["warnings"])
	sub := body["subscription"].(map[string]any)
	assert.Equal(t, "business", sub["plan"])
	assert.Equal(t, "pro", sub["pending_plan"])
	assert.Equal(t, "business", sub["effective_plan"], "the bigger plan stays until the period ends")
	assert.Zero(t, s.emailsTo("owner@example.com", "invoice_created"), "a downgrade has nothing to pay")
}

func TestChangePlan_EndToEnd_ValidationConflictsAndRoles(t *testing.T) {
	s := newStack(t)
	tm := s.payingTeam()

	assert.Equal(t, http.StatusBadRequest, s.post(tm.subscriptionActionPath("change-plan"), `{"plan":"free"}`, bearer(tm.owner)).Code)
	assert.Equal(t, http.StatusBadRequest, s.post(tm.subscriptionActionPath("change-plan"), `{}`, bearer(tm.owner)).Code)
	assert.Equal(t, http.StatusConflict, s.post(tm.subscriptionActionPath("change-plan"), `{"plan":"pro"}`, bearer(tm.owner)).Code, "already on it")
	assert.Equal(t, http.StatusForbidden, s.post(tm.subscriptionActionPath("change-plan"), `{"plan":"business"}`, bearer(tm.admin)).Code)
	assert.Equal(t, http.StatusForbidden, s.post(tm.subscriptionActionPath("change-plan"), `{"plan":"business"}`, bearer(tm.member)).Code)
	assert.Equal(t, http.StatusNotFound, s.post(tm.subscriptionActionPath("change-plan"), `{"plan":"business"}`, bearer(tm.outside)).Code)
	assert.Empty(t, s.gateway.Created())
}

func TestChangePlan_EndToEnd_NeedsAVerifiedEmail(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam() // nobody verified
	s.setPlan(tm.orgID, "pro", "active")

	res := s.post(tm.subscriptionActionPath("change-plan"), `{"plan":"business"}`, bearer(tm.owner))

	assert.Equal(t, http.StatusForbidden, res.Code)
	assert.Equal(t, "EMAIL_NOT_VERIFIED", errorOf(res)["code"])
}

func TestCancelAndResume_EndToEnd(t *testing.T) {
	s := newStack(t)
	tm := s.payingTeam()

	canceled := s.post(tm.subscriptionActionPath("cancel"), "", bearer(tm.owner))
	require.Equal(t, http.StatusOK, canceled.Code, canceled.Body.String())
	sub := canceled.json()["subscription"].(map[string]any)
	assert.Equal(t, true, sub["cancel_at_period_end"])
	assert.Equal(t, "active", sub["status"])
	assert.Equal(t, "pro", sub["effective_plan"], "the plan keeps working until the period ends")

	assert.Equal(t, http.StatusOK, s.post(tm.subscriptionActionPath("cancel"), "", bearer(tm.owner)).Code, "idempotent")

	resumed := s.post(tm.subscriptionActionPath("resume"), "", bearer(tm.owner))
	require.Equal(t, http.StatusOK, resumed.Code, resumed.Body.String())
	assert.Equal(t, false, resumed.json()["subscription"].(map[string]any)["cancel_at_period_end"])
}

func TestCancelAndResume_EndToEnd_OnlyTheOwnerAndOnlyAPaidSubscription(t *testing.T) {
	s := newStack(t)
	tm := s.payingTeam()

	for _, action := range []string{"cancel", "resume"} {
		assert.Equal(t, http.StatusForbidden, s.post(tm.subscriptionActionPath(action), "", bearer(tm.admin)).Code, action)
		assert.Equal(t, http.StatusForbidden, s.post(tm.subscriptionActionPath(action), "", bearer(tm.member)).Code, action)
		assert.Equal(t, http.StatusNotFound, s.post(tm.subscriptionActionPath(action), "", bearer(tm.outside)).Code, action)
		assert.Equal(t, http.StatusUnauthorized, s.post(tm.subscriptionActionPath(action), "").Code, action)
	}

	free := s.billingTeamNamed("solo")
	for _, action := range []string{"cancel", "resume"} {
		res := s.post(free.subscriptionActionPath(action), "", bearer(free.owner))
		assert.Equal(t, http.StatusConflict, res.Code, action)
	}
}

// billingTeamNamed is a verified owner alone in a Free organization, whose
// email starts with name.
func (s *stack) billingTeamNamed(name string) team {
	s.t.Helper()
	email := name + "@example.com"
	s.registered(email)
	s.verify(email)
	return team{orgID: s.firstOrg(email), owner: s.loggedIn(email)}
}

// fillMembers makes the organization have n members by inserting users directly.
func (s *stack) fillMembers(orgID string, n int) {
	s.t.Helper()
	_, err := s.admin.Exec(s.ctx(), `
		WITH existing AS (SELECT count(*) AS c FROM memberships WHERE organization_id = $1),
		     new_users AS (
		       INSERT INTO users (email, password_hash, name)
		       SELECT 'filler' || g || '@example.com', 'h', 'Filler' FROM generate_series(1, GREATEST($2 - (SELECT c FROM existing), 0)) g
		       RETURNING id)
		INSERT INTO memberships (organization_id, user_id, role) SELECT $1, id, 'member' FROM new_users`, orgID, n)
	require.NoError(s.t, err)
}
