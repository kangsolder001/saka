package entitlement_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/entitlement"
	"github.com/kangsolder001/saka/internal/entitlement/mocks"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/db"
)

var orgID = uuid.MustParse("00000000-0000-7000-8000-000000000001")

// The catalogue of migration 00008.
var (
	free = entitlement.Plan{Code: "free", Name: "Free", Limits: map[string]int{"members": 3}}
	pro  = entitlement.Plan{
		Code: "pro", Name: "Pro", PriceAmount: 99000, Features: []string{"export_csv"},
		Limits: map[string]int{"members": 10},
	}
	business = entitlement.Plan{
		Code: "business", Name: "Business", PriceAmount: 299000, Features: []string{"export_csv", "audit_log"},
		Limits: map[string]int{"members": entitlement.Unlimited},
	}
	catalogue = []entitlement.Plan{free, pro, business}
)

type fixture struct {
	plans *mocks.MockPlanSource
	svc   *entitlement.Service
	// counted is how many times the members counter ran.
	counted int
}

// newFixture gives an organization on the plan with a members counter that
// reports used.
func newFixture(t *testing.T, plan entitlement.Plan, used int) *fixture {
	t.Helper()
	f := &fixture{plans: mocks.NewMockPlanSource(t)}
	f.plans.EXPECT().EffectivePlan(mock.Anything, mock.Anything, orgID).Return(plan, nil).Maybe()
	f.plans.EXPECT().ActivePlans(mock.Anything, mock.Anything).Return(catalogue, nil).Maybe()
	f.svc = entitlement.NewService(f.plans)
	f.svc.RegisterCounter("members", func(context.Context, db.DBTX, uuid.UUID) (int, error) {
		f.counted++
		return used, nil
	})
	return f
}

func TestHasFeature_FollowsTheEffectivePlan(t *testing.T) {
	tests := []struct {
		plan    entitlement.Plan
		feature string
		want    bool
	}{
		{free, "export_csv", false},
		{pro, "export_csv", true},
		{pro, "audit_log", false},
		{business, "audit_log", true},
		{business, "unknown_feature", false},
	}
	for _, tt := range tests {
		t.Run(tt.plan.Code+"/"+tt.feature, func(t *testing.T) {
			f := newFixture(t, tt.plan, 0)

			got, err := f.svc.HasFeature(t.Context(), nil, orgID, tt.feature)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHasFeature_APlanLookupFailureIsNotAnAnswer(t *testing.T) {
	plans := mocks.NewMockPlanSource(t)
	plans.EXPECT().EffectivePlan(mock.Anything, mock.Anything, orgID).Return(entitlement.Plan{}, errors.New("db down"))

	got, err := entitlement.NewService(plans).HasFeature(t.Context(), nil, orgID, "export_csv")

	require.Error(t, err)
	assert.False(t, got)
}

func TestCheckFeature_NamesTheCheapestPlanThatHasIt(t *testing.T) {
	tests := []struct {
		plan      entitlement.Plan
		feature   string
		upgradeTo any
	}{
		{free, "export_csv", "pro"},
		{free, "audit_log", "business"},
		{pro, "audit_log", "business"},
		{business, "no_such_feature", nil},
	}
	for _, tt := range tests {
		t.Run(tt.plan.Code+"/"+tt.feature, func(t *testing.T) {
			f := newFixture(t, tt.plan, 0)

			err := f.svc.CheckFeature(t.Context(), nil, orgID, tt.feature)

			var appErr *apperr.Error
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, apperr.CodePlanFeatureUnavailable, appErr.Code)
			assert.Equal(t, http.StatusForbidden, appErr.HTTPStatus())
			assert.Equal(t, tt.feature, appErr.Details["feature"])
			assert.Equal(t, tt.plan.Code, appErr.Details["plan"])
			assert.Equal(t, tt.upgradeTo, appErr.Details["upgrade_to"])
			assert.Equal(t, "Fitur ini tidak tersedia di paket "+tt.plan.Name+".", appErr.Message)
		})
	}
}

func TestCheckFeature_AllowsWhatThePlanIncludes(t *testing.T) {
	f := newFixture(t, business, 0)

	assert.NoError(t, f.svc.CheckFeature(t.Context(), nil, orgID, "audit_log"))
}

func TestCheckLimit_AllowsUpToAndIncludingTheMaximum(t *testing.T) {
	tests := map[string]struct {
		used, delta int
	}{
		"empty":             {0, 1},
		"one below":         {1, 1},
		"exactly the limit": {2, 1},
		"a batch that fits": {1, 2},
		"nothing to add":    {3, 0},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, free, tt.used)

			assert.NoError(t, f.svc.CheckLimit(t.Context(), nil, orgID, "members", tt.delta))
		})
	}
}

func TestCheckLimit_RefusesWhatWouldExceedItAndSaysWhatToUpgradeTo(t *testing.T) {
	tests := map[string]struct {
		plan        entitlement.Plan
		used, delta int
		upgradeTo   any
		message     string
	}{
		"free at the limit":     {free, 3, 1, "pro", "Paket Free hanya mendukung 3 anggota."},
		"free, a batch too big": {free, 2, 2, "pro", "Paket Free hanya mendukung 3 anggota."},
		"free needing more":     {free, 10, 1, "business", "Paket Free hanya mendukung 3 anggota."},
		"pro at the limit":      {pro, 10, 1, "business", "Paket Pro hanya mendukung 10 anggota."},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, tt.plan, tt.used)

			err := f.svc.CheckLimit(t.Context(), nil, orgID, "members", tt.delta)

			var appErr *apperr.Error
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, apperr.CodePlanLimitReached, appErr.Code)
			assert.Equal(t, http.StatusPaymentRequired, appErr.HTTPStatus())
			assert.Equal(t, tt.message, appErr.Message)
			assert.Equal(t, map[string]any{
				"limit": "members", "max": tt.plan.Limits["members"], "used": tt.used,
				"plan": tt.plan.Code, "upgrade_to": tt.upgradeTo,
			}, appErr.Details)
		})
	}
}

func TestCheckLimit_NoPlanOffersEnoughMeansNoUpgrade(t *testing.T) {
	plans := mocks.NewMockPlanSource(t)
	small := entitlement.Plan{Code: "free", Name: "Free", Limits: map[string]int{"members": 3}}
	plans.EXPECT().EffectivePlan(mock.Anything, mock.Anything, orgID).Return(small, nil)
	plans.EXPECT().ActivePlans(mock.Anything, mock.Anything).Return([]entitlement.Plan{small, pro}, nil)
	svc := entitlement.NewService(plans)
	svc.RegisterCounter("members", func(context.Context, db.DBTX, uuid.UUID) (int, error) { return 10, nil })

	err := svc.CheckLimit(t.Context(), nil, orgID, "members", 1)

	var appErr *apperr.Error
	require.ErrorAs(t, err, &appErr)
	assert.Nil(t, appErr.Details["upgrade_to"], "Pro tops out at 10, so it would not fit the 11th")
}

func TestCheckLimit_TheCheapestFittingPlanWinsRegardlessOfCatalogueOrder(t *testing.T) {
	plans := mocks.NewMockPlanSource(t)
	plans.EXPECT().EffectivePlan(mock.Anything, mock.Anything, orgID).Return(free, nil)
	plans.EXPECT().ActivePlans(mock.Anything, mock.Anything).Return([]entitlement.Plan{business, free, pro}, nil)
	svc := entitlement.NewService(plans)
	svc.RegisterCounter("members", func(context.Context, db.DBTX, uuid.UUID) (int, error) { return 3, nil })

	err := svc.CheckLimit(t.Context(), nil, orgID, "members", 1)

	var appErr *apperr.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, "pro", appErr.Details["upgrade_to"])
}

func TestCheckLimit_UnlimitedPlansNeverCount(t *testing.T) {
	f := newFixture(t, business, 1_000_000)

	require.NoError(t, f.svc.CheckLimit(t.Context(), nil, orgID, "members", 1))
	assert.Zero(t, f.counted, "no point counting against no maximum")
}

func TestCheckLimit_RemovingThingsIsNeverRefusedEvenWhenOverTheLimit(t *testing.T) {
	f := newFixture(t, free, 7) // downgraded from a bigger plan

	assert.NoError(t, f.svc.CheckLimit(t.Context(), nil, orgID, "members", 0))
	assert.NoError(t, f.svc.CheckLimit(t.Context(), nil, orgID, "members", -2))
	assert.Error(t, f.svc.CheckLimit(t.Context(), nil, orgID, "members", 1), "but adding is")
}

func TestCheckLimit_ALimitTheCurrentPlanDoesNotListIsZero(t *testing.T) {
	plans := mocks.NewMockPlanSource(t)
	noMembers := entitlement.Plan{Code: "odd", Name: "Odd", Limits: map[string]int{}}
	plans.EXPECT().EffectivePlan(mock.Anything, mock.Anything, orgID).Return(noMembers, nil)
	plans.EXPECT().ActivePlans(mock.Anything, mock.Anything).Return(catalogue, nil)
	svc := entitlement.NewService(plans)
	svc.RegisterCounter("members", func(context.Context, db.DBTX, uuid.UUID) (int, error) { return 0, nil })

	err := svc.CheckLimit(t.Context(), nil, orgID, "members", 1)

	var appErr *apperr.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, apperr.CodePlanLimitReached, appErr.Code, "unconfigured means none allowed, not unlimited")
	assert.Equal(t, "Paket Odd tidak mendukung anggota.", appErr.Message)
	assert.Equal(t, 0, appErr.Details["max"])
	assert.Equal(t, "free", appErr.Details["upgrade_to"])
}

func TestCheckLimit_ALimitWithoutACounterIsAProgrammingError(t *testing.T) {
	f := newFixture(t, free, 0)

	err := f.svc.CheckLimit(t.Context(), nil, orgID, "projects", 1)

	require.Error(t, err)
	assert.False(t, apperr.IsCode(err, apperr.CodePlanLimitReached), "not something to show a user")
	assert.Contains(t, err.Error(), "projects")
	assert.Error(t, f.svc.CheckLimit(t.Context(), nil, orgID, "projects", 0), "even a no-op check on an unknown limit is loud")
}

func TestCheckLimit_FailuresOfTheCollaboratorsPropagate(t *testing.T) {
	t.Run("plan lookup", func(t *testing.T) {
		plans := mocks.NewMockPlanSource(t)
		plans.EXPECT().EffectivePlan(mock.Anything, mock.Anything, orgID).Return(entitlement.Plan{}, errors.New("db down"))
		svc := entitlement.NewService(plans)
		svc.RegisterCounter("members", func(context.Context, db.DBTX, uuid.UUID) (int, error) { return 0, nil })

		err := svc.CheckLimit(t.Context(), nil, orgID, "members", 1)

		require.Error(t, err)
		assert.False(t, apperr.IsCode(err, apperr.CodePlanLimitReached))
	})
	t.Run("counter", func(t *testing.T) {
		plans := mocks.NewMockPlanSource(t)
		plans.EXPECT().EffectivePlan(mock.Anything, mock.Anything, orgID).Return(free, nil)
		svc := entitlement.NewService(plans)
		svc.RegisterCounter("members", func(context.Context, db.DBTX, uuid.UUID) (int, error) { return 0, errors.New("count failed") })

		err := svc.CheckLimit(t.Context(), nil, orgID, "members", 1)

		require.ErrorContains(t, err, "count failed")
	})
	t.Run("catalogue lookup while building the upgrade hint", func(t *testing.T) {
		plans := mocks.NewMockPlanSource(t)
		plans.EXPECT().EffectivePlan(mock.Anything, mock.Anything, orgID).Return(free, nil)
		plans.EXPECT().ActivePlans(mock.Anything, mock.Anything).Return(nil, errors.New("db down"))
		svc := entitlement.NewService(plans)
		svc.RegisterCounter("members", func(context.Context, db.DBTX, uuid.UUID) (int, error) { return 3, nil })

		err := svc.CheckLimit(t.Context(), nil, orgID, "members", 1)

		require.Error(t, err)
		assert.False(t, apperr.IsCode(err, apperr.CodePlanLimitReached))
	})
}

func TestUsage_ReportsEveryCountedLimitAgainstThePlan(t *testing.T) {
	plans := mocks.NewMockPlanSource(t)
	plans.EXPECT().EffectivePlan(mock.Anything, mock.Anything, orgID).Return(pro, nil)
	svc := entitlement.NewService(plans)
	svc.RegisterCounter("members", func(context.Context, db.DBTX, uuid.UUID) (int, error) { return 4, nil })
	svc.RegisterCounter("projects", func(context.Context, db.DBTX, uuid.UUID) (int, error) { return 2, nil })

	got, err := svc.Usage(t.Context(), nil, orgID)

	require.NoError(t, err)
	assert.Equal(t, map[string]entitlement.LimitUsage{
		"members":  {Used: 4, Max: 10},
		"projects": {Used: 2, Max: 0}, // pro does not list it: closed, like CheckLimit
	}, got)
}

func TestUsage_UnlimitedShowsAsMinusOne(t *testing.T) {
	f := newFixture(t, business, 42)

	got, err := f.svc.Usage(t.Context(), nil, orgID)

	require.NoError(t, err)
	assert.Equal(t, map[string]entitlement.LimitUsage{"members": {Used: 42, Max: entitlement.Unlimited}}, got)
}

func TestUsage_ALimitWithoutACounterIsNotReported(t *testing.T) {
	// business lists only "members"; a plan limit nobody counts does not appear.
	plans := mocks.NewMockPlanSource(t)
	extra := entitlement.Plan{Code: "x", Limits: map[string]int{"members": 5, "seats": 9}}
	plans.EXPECT().EffectivePlan(mock.Anything, mock.Anything, orgID).Return(extra, nil)
	svc := entitlement.NewService(plans)
	svc.RegisterCounter("members", func(context.Context, db.DBTX, uuid.UUID) (int, error) { return 1, nil })

	got, err := svc.Usage(t.Context(), nil, orgID)

	require.NoError(t, err)
	assert.Equal(t, map[string]entitlement.LimitUsage{"members": {Used: 1, Max: 5}}, got)
}

func TestUsage_FailuresPropagate(t *testing.T) {
	plans := mocks.NewMockPlanSource(t)
	plans.EXPECT().EffectivePlan(mock.Anything, mock.Anything, orgID).Return(free, nil)
	svc := entitlement.NewService(plans)
	svc.RegisterCounter("members", func(context.Context, db.DBTX, uuid.UUID) (int, error) { return 0, errors.New("count failed") })

	_, err := svc.Usage(t.Context(), nil, orgID)

	require.ErrorContains(t, err, "count failed")
}

func TestRegisterCounter_RejectsDuplicatesAndEmptyRegistrations(t *testing.T) {
	count := func(context.Context, db.DBTX, uuid.UUID) (int, error) { return 0, nil }
	svc := entitlement.NewService(mocks.NewMockPlanSource(t))
	svc.RegisterCounter("members", count)

	assert.Panics(t, func() { svc.RegisterCounter("members", count) }, "two owners for one limit is a wiring bug")
	assert.Panics(t, func() { svc.RegisterCounter("", count) })
	assert.Panics(t, func() { svc.RegisterCounter("projects", nil) })
}
