//go:build integration

package app_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mePath = "/api/v1/me"

func (s *stack) get(path string, headers map[string]string) response {
	s.t.Helper()
	return s.do(http.MethodGet, path, "", headers)
}

func (s *stack) patch(path, body string, headers map[string]string) response {
	s.t.Helper()
	return s.do(http.MethodPatch, path, body, headers)
}

func TestMe_EndToEnd(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")

	res := s.get(mePath, bearer(access))

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	body := res.json()
	profile := body["user"].(map[string]any)
	assert.Equal(t, "ann@example.com", profile["email"])
	assert.Equal(t, "Ann", profile["name"])
	assert.Equal(t, false, profile["email_verified"])
	assert.Nil(t, profile["avatar_url"])
	assert.Empty(t, body["organizations"])
	assert.NotContains(t, res.Body.String(), "argon2id", "the password hash never leaves the server")
}

func TestMe_RequiresAuthentication(t *testing.T) {
	s := newStack(t)

	assert.Equal(t, http.StatusUnauthorized, s.get(mePath, nil).Code)
	assert.Equal(t, http.StatusUnauthorized, s.get(mePath, bearer("garbage")).Code)
	assert.Equal(t, http.StatusUnauthorized, s.patch(mePath, `{"name":"X"}`, nil).Code)
}

func TestPatchMe_UpdatesTheProfileAndIsVisibleImmediately(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")
	require.Equal(t, "Ann", s.get(mePath, bearer(access)).json()["user"].(map[string]any)["name"], "fills the cache")

	res := s.patch(mePath, `{"name":"  Ann Lee ","avatar_url":"https://cdn.example.com/ann.png"}`, bearer(access))

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	updated := res.json()["user"].(map[string]any)
	assert.Equal(t, "Ann Lee", updated["name"])
	assert.Equal(t, "https://cdn.example.com/ann.png", updated["avatar_url"])

	again := s.get(mePath, bearer(access)).json()["user"].(map[string]any)
	assert.Equal(t, "Ann Lee", again["name"], "the cached profile was dropped by the update")
	assert.Equal(t, "https://cdn.example.com/ann.png", again["avatar_url"])

	removed := s.patch(mePath, `{"avatar_url":null}`, bearer(access))
	require.Equal(t, http.StatusOK, removed.Code, removed.Body.String())
	assert.Nil(t, s.get(mePath, bearer(access)).json()["user"].(map[string]any)["avatar_url"])
	assert.Equal(t, "Ann Lee", s.get(mePath, bearer(access)).json()["user"].(map[string]any)["name"], "name untouched")
}

func TestPatchMe_RejectsInvalidInput(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")

	for name, body := range map[string]string{
		"empty object":        `{}`,
		"blank name":          `{"name":"  "}`,
		"javascript avatar":   `{"avatar_url":"javascript:alert(1)"}`,
		"cannot set email":    `{"email":"evil@example.com"}`,
		"cannot set verified": `{"email_verified":true}`,
	} {
		res := s.patch(mePath, body, bearer(access))

		assert.Equal(t, http.StatusBadRequest, res.Code, "%s: %s", name, res.Body.String())
	}
	assert.Equal(t, "Ann", s.get(mePath, bearer(access)).json()["user"].(map[string]any)["name"], "nothing changed")
}

func TestPatchMe_OnlyChangesTheCallersOwnProfile(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	s.registered("bob@example.com")
	ann := s.loggedIn("ann@example.com")
	bob := s.loggedIn("bob@example.com")

	require.Equal(t, http.StatusOK, s.patch(mePath, `{"name":"Ann Lee"}`, bearer(ann)).Code)

	assert.Equal(t, "Ann", s.get(mePath, bearer(bob)).json()["user"].(map[string]any)["name"], "bob's profile is his own")
}
