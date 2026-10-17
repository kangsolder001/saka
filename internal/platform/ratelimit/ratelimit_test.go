package ratelimit_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kangsolder001/saka/internal/platform/ratelimit"
)

func TestKey_HashesItsPartsAndIsDeterministic(t *testing.T) {
	a := ratelimit.Key("login", "ann@example.com", "203.0.113.9")
	b := ratelimit.Key("login", "ann@example.com", "203.0.113.9")

	assert.Equal(t, a, b)
	assert.True(t, strings.HasPrefix(a, "login:"))
	assert.NotContains(t, a, "ann@example.com", "emails must never be stored in clear")
	assert.NotContains(t, a, "203.0.113.9", "IPs must never be stored in clear")
	assert.Regexp(t, `^login:[0-9a-f]{64}$`, a)
}

func TestKey_DifferentInputsDoNotCollide(t *testing.T) {
	assert.NotEqual(t, ratelimit.Key("login", "a", "b"), ratelimit.Key("login", "a", "c"))
	assert.NotEqual(t, ratelimit.Key("login", "a"), ratelimit.Key("register", "a"), "rule names namespace keys")
	assert.NotEqual(t, ratelimit.Key("x", "ab", "c"), ratelimit.Key("x", "a", "bc"), "part boundaries matter")
}
