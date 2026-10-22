package entitlement_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kangsolder001/saka/internal/entitlement"
)

func TestPlan_HasFeature(t *testing.T) {
	p := entitlement.Plan{Features: []string{"export_csv"}}

	assert.True(t, p.HasFeature("export_csv"))
	assert.False(t, p.HasFeature("audit_log"))
	assert.False(t, entitlement.Plan{}.HasFeature("export_csv"))
}

func TestPlan_ALimitTheMissingPlanDoesNotMentionIsZeroNotUnlimited(t *testing.T) {
	p := entitlement.Plan{Limits: map[string]int{"members": 3, "projects": entitlement.Unlimited}}

	assert.Equal(t, 3, p.Limit("members"))
	assert.Equal(t, entitlement.Unlimited, p.Limit("projects"))
	assert.Zero(t, p.Limit("seats"), "an unconfigured limit fails closed")
}
