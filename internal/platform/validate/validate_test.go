package validate_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/validate"
)

type item struct {
	Name string `json:"name" validate:"required"`
}

type request struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8,max=72"`
	Role     string `json:"role" validate:"oneof=admin member"`
	Age      int    `json:"age" validate:"min=18"`
	Items    []item `json:"items" validate:"dive"`
	NoTag    string `validate:"required"`
}

func valid() request {
	return request{Email: "a@b.co", Password: "longenough", Role: "admin", Age: 20, NoTag: "x"}
}

func fieldsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	require.Error(t, err)
	appErr := apperr.From(err)
	require.Equal(t, apperr.CodeValidation, appErr.Code)
	return appErr.Details["fields"].(map[string]string)
}

func TestStruct_Valid(t *testing.T) {
	assert.NoError(t, validate.Struct(valid()))
}

func TestStruct_UsesJSONFieldNamesAndMessages(t *testing.T) {
	r := request{Email: "not-an-email", Password: "short", Role: "root", Age: 10}

	fields := fieldsOf(t, validate.Struct(r))

	assert.Equal(t, "must be a valid email address", fields["email"])
	assert.Equal(t, "must be at least 8 characters", fields["password"])
	assert.Equal(t, "must be one of: admin, member", fields["role"])
	assert.Equal(t, "must be at least 18", fields["age"])
	assert.Equal(t, "is required", fields["NoTag"], "fields without json tag fall back to the Go name")
}

func TestStruct_ReportsRequiredAndTooLong(t *testing.T) {
	r := valid()
	r.Email = ""
	r.Password = strings.Repeat("a", 73)

	fields := fieldsOf(t, validate.Struct(r))

	assert.Equal(t, "is required", fields["email"])
	assert.Equal(t, "must be at most 72 characters", fields["password"])
}

func TestStruct_NestedPathsDropRootName(t *testing.T) {
	r := valid()
	r.Items = []item{{Name: "ok"}, {Name: ""}}

	fields := fieldsOf(t, validate.Struct(r))

	assert.Equal(t, "is required", fields["items[1].name"])
}

func TestStruct_PointerAndNonStructInput(t *testing.T) {
	r := valid()
	assert.NoError(t, validate.Struct(&r))

	err := validate.Struct(42)
	require.Error(t, err)
	assert.Equal(t, apperr.CodeInternal, apperr.From(err).Code)
}

func TestHTTPURLProblem(t *testing.T) {
	assert.Empty(t, validate.HTTPURLProblem("https://cdn.example.com/a.png", 100))
	assert.Empty(t, validate.HTTPURLProblem("http://localhost:3000/a.png", 100))
	for _, bad := range []string{"", "javascript:alert(1)", "data:image/png;base64,AAAA", "/relative/a.png", "ftp://example.com/a", "https://", "ht tp://x"} {
		assert.Equal(t, "must be a valid http(s) URL", validate.HTTPURLProblem(bad, 100), bad)
	}
	assert.Equal(t, "must be at most 10 characters", validate.HTTPURLProblem("https://example.com/long", 10))
}
