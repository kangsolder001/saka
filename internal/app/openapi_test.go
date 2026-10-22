package app_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/kangsolder001/saka/internal/platform/apperr"
)

// docs/openapi.yaml is written by hand. These tests keep it honest: it must be
// well formed, and (in openapi_routes_integration_test.go) it must describe
// exactly the routes the router registers.

var openAPIMethods = []string{"get", "put", "post", "delete", "patch", "options", "head"}

// openAPIDoc is the parsed docs/openapi.yaml.
type openAPIDoc map[string]any

func loadOpenAPI(t *testing.T) openAPIDoc {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	raw, err := os.ReadFile(filepath.Join(root, "docs", "openapi.yaml"))
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, yaml.Unmarshal(raw, &doc), "docs/openapi.yaml is not valid YAML")
	return openAPIDoc(doc)
}

func asMap(t *testing.T, v any, what string) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	require.True(t, ok, "%s must be a mapping, got %T", what, v)
	return m
}

// operation is one method on one path.
type operation struct {
	Method string // upper case
	// Route is the gin route template the operation must match, for example
	// /api/v1/orgs/:orgId.
	Route string
	// Path is the template as written in the document.
	Path string
	Spec map[string]any
	// Secured is true when the effective security requires the bearer token.
	Secured bool
}

func (d openAPIDoc) operations(t *testing.T) []operation {
	t.Helper()
	servers, _ := d["servers"].([]any)
	require.NotEmpty(t, servers, "the document needs a servers entry")
	defaultBase := asMap(t, servers[0], "servers[0]")["url"].(string)
	globalSecurity := d["security"]

	var ops []operation
	paths := asMap(t, d["paths"], "paths")
	for path, item := range paths {
		pathItem := asMap(t, item, path)
		for _, method := range openAPIMethods {
			spec, ok := pathItem[method].(map[string]any)
			if !ok {
				continue
			}
			// A server list on the operation overrides the one on the path, which
			// overrides the document's (the health endpoints live outside the API prefix).
			base := defaultBase
			for _, scope := range []map[string]any{pathItem, spec} {
				if own, ok := scope["servers"].([]any); ok && len(own) > 0 {
					base = asMap(t, own[0], path+" servers[0]")["url"].(string)
				}
			}
			route := strings.TrimRight(base, "/") + path
			route = regexp.MustCompile(`\{([^}]+)\}`).ReplaceAllString(route, ":$1")
			security := globalSecurity
			if own, ok := spec["security"]; ok {
				security = own
			}
			ops = append(ops, operation{
				Method: strings.ToUpper(method), Route: route, Path: path, Spec: spec, Secured: requiresBearer(security),
			})
		}
	}
	sort.Slice(ops, func(i, j int) bool {
		return ops[i].Route+ops[i].Method < ops[j].Route+ops[j].Method
	})
	return ops
}

func requiresBearer(security any) bool {
	reqs, _ := security.([]any)
	for _, r := range reqs {
		if m, ok := r.(map[string]any); ok {
			if _, has := m["bearerAuth"]; has {
				return true
			}
		}
	}
	return false
}

func TestOpenAPI_IsWellFormed(t *testing.T) {
	doc := loadOpenAPI(t)

	assert.True(t, strings.HasPrefix(doc["openapi"].(string), "3.1."), "OpenAPI 3.1")
	info := asMap(t, doc["info"], "info")
	assert.NotEmpty(t, info["title"])
	assert.NotEmpty(t, info["version"])

	ops := doc.operations(t)
	require.Greater(t, len(ops), 40, "the document describes the whole API")

	components := asMap(t, doc["components"], "components")
	schemes := asMap(t, components["securitySchemes"], "securitySchemes")
	require.Contains(t, schemes, "bearerAuth")

	// Every $ref points at something that exists.
	var refs []string
	collectRefs(doc, &refs)
	for _, ref := range refs {
		assert.NotNil(t, resolveRef(doc, ref), "unresolved $ref %s", ref)
	}

	seenIDs := map[string]string{}
	for _, op := range ops {
		name := op.Method + " " + op.Path
		id, _ := op.Spec["operationId"].(string)
		assert.NotEmpty(t, id, "%s needs an operationId", name)
		if prev, dup := seenIDs[id]; dup {
			t.Errorf("operationId %q is used by %s and %s", id, prev, name)
		}
		seenIDs[id] = name
		assert.NotEmpty(t, op.Spec["summary"], "%s needs a summary", name)
		assert.NotEmpty(t, op.Spec["tags"], "%s needs a tag", name)

		responses := asMap(t, op.Spec["responses"], name+" responses")
		hasSuccess := false
		for status := range responses {
			if strings.HasPrefix(status, "2") {
				hasSuccess = true
			}
		}
		assert.True(t, hasSuccess, "%s needs a 2xx response", name)
		if op.Secured {
			assert.Contains(t, responses, "401", "%s requires a token, so it can answer 401", name)
		}

		// Every {param} in the path is declared (on the path or the operation).
		declared := declaredPathParams(t, doc, op)
		for _, m := range regexp.MustCompile(`\{([^}]+)\}`).FindAllStringSubmatch(op.Path, -1) {
			assert.Contains(t, declared, m[1], "%s uses {%s} without declaring it", name, m[1])
		}
	}
}

func TestOpenAPI_ErrorCodesMatchTheCodeBase(t *testing.T) {
	doc := loadOpenAPI(t)
	schemas := asMap(t, asMap(t, doc["components"], "components")["schemas"], "schemas")
	enum, _ := asMap(t, schemas["ErrorCode"], "ErrorCode")["enum"].([]any)

	var documented []string
	for _, c := range enum {
		documented = append(documented, c.(string))
	}
	code := []apperr.Code{
		apperr.CodeValidation, apperr.CodeUnauthenticated, apperr.CodeTokenExpired, apperr.CodeForbidden,
		apperr.CodeEmailNotVerified, apperr.CodePlanFeatureUnavailable, apperr.CodePlanLimitReached,
		apperr.CodeSubscriptionInactive, apperr.CodeNotFound, apperr.CodeConflict, apperr.CodeRateLimited,
		apperr.CodePaymentProvider, apperr.CodeInternal,
	}
	var implemented []string
	for _, c := range code {
		implemented = append(implemented, string(c))
	}
	assert.ElementsMatch(t, implemented, documented, "ErrorCode in docs/openapi.yaml must list every code of internal/platform/apperr")
}

func declaredPathParams(t *testing.T, doc openAPIDoc, op operation) map[string]bool {
	t.Helper()
	declared := map[string]bool{}
	add := func(list any) {
		items, _ := list.([]any)
		for _, p := range items {
			param := asMap(t, p, "parameter")
			if ref, ok := param["$ref"].(string); ok {
				param = asMap(t, resolveRef(doc, ref), ref)
			}
			if param["in"] == "path" {
				declared[param["name"].(string)] = true
			}
		}
	}
	paths := asMap(t, doc["paths"], "paths")
	add(asMap(t, paths[op.Path], op.Path)["parameters"])
	add(op.Spec["parameters"])
	return declared
}

func collectRefs(v any, out *[]string) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			if k == "$ref" {
				if s, ok := child.(string); ok {
					*out = append(*out, s)
				}
				continue
			}
			collectRefs(child, out)
		}
	case []any:
		for _, child := range x {
			collectRefs(child, out)
		}
	}
}

// resolveRef follows a local JSON pointer such as #/components/schemas/Error.
func resolveRef(doc openAPIDoc, ref string) any {
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}
	var cur any = map[string]any(doc)
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[part]
	}
	return cur
}
