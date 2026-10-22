//go:build integration

package app_test

import (
	"net/http"
	"regexp"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpenAPI_DescribesExactlyTheRegisteredRoutes keeps docs/openapi.yaml and
// the router from drifting: every route app.NewRouter registers (the health
// endpoints included) is documented with the same method, and everything the
// document describes exists.
func TestOpenAPI_DescribesExactlyTheRegisteredRoutes(t *testing.T) {
	s := newStack(t)
	doc := loadOpenAPI(t)

	registered := map[string]bool{}
	for _, r := range s.engine.Routes() {
		registered[r.Method+" "+r.Path] = true
	}
	documented := map[string]bool{}
	for _, op := range doc.operations(t) {
		documented[op.Method+" "+op.Route] = true
	}

	var undocumented, phantom []string
	for route := range registered {
		if !documented[route] {
			undocumented = append(undocumented, route)
		}
	}
	for route := range documented {
		if !registered[route] {
			phantom = append(phantom, route)
		}
	}
	sort.Strings(undocumented)
	sort.Strings(phantom)
	assert.Empty(t, undocumented, "routes the router serves but docs/openapi.yaml does not describe")
	assert.Empty(t, phantom, "operations docs/openapi.yaml describes but the router does not serve")
	assert.NotEmpty(t, registered)
}

// TestOpenAPI_SecuredOperationsRejectARequestWithoutAToken checks the security
// column of the document against the running router: whatever the document
// marks as needing the bearer token must answer 401 without one.
func TestOpenAPI_SecuredOperationsRejectARequestWithoutAToken(t *testing.T) {
	s := newStack(t)
	doc := loadOpenAPI(t)
	param := regexp.MustCompile(`\{[^}]+\}`)

	checked := 0
	for _, op := range doc.operations(t) {
		if !op.Secured {
			continue
		}
		path := "/api/v1" + param.ReplaceAllStringFunc(op.Path, func(string) string { return uuid.NewString() })
		res := s.do(op.Method, path, "", nil)

		assert.Equal(t, http.StatusUnauthorized, res.Code, "%s %s is documented as secured", op.Method, op.Path)
		checked++
	}
	require.Greater(t, checked, 30, "most of the API is behind the token")
}
