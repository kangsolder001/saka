package user_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kangsolder001/saka/internal/user"
)

// cacheWriters are the Service methods that change a user the cache may hold.
// Each one must end with invalidateAfterCommit, and the integration test
// TestCacheWriters_* runs every name listed here against a cached user.
var cacheWriters = []string{"ChangeEmail", "Delete", "MarkEmailVerified", "UpdateProfile"}

// notCacheWriters are the other exported methods, each with the reason it
// does not need to invalidate.
var notCacheWriters = map[string]string{
	"Create":          "inserts a new user: nothing can be cached for an ID that did not exist",
	"SetPasswordHash": "the cached summary holds no password data (TestSetPasswordHash_LeavesTheSummaryCacheAlone)",
	"GetByID":         "read",
	"GetByEmail":      "read",
	"GetSummary":      "read (fills the cache)",
	"GetSummaries":    "read",
	"InvalidateCache": "cache control itself",
}

// TestServiceMethods_AreAllClassified fails when someone adds an exported
// method to user.Service without deciding whether it writes cached data. A new
// write method must end with invalidateAfterCommit and be added to
// cacheWriters (and to writeCalls in the integration test) so its cache
// behaviour is proven; anything else needs a reason in notCacheWriters.
func TestServiceMethods_AreAllClassified(t *testing.T) {
	var actual []string
	typ := reflect.TypeOf(&user.Service{})
	for i := range typ.NumMethod() {
		actual = append(actual, typ.Method(i).Name)
	}

	var classified []string
	classified = append(classified, cacheWriters...)
	for name := range notCacheWriters {
		classified = append(classified, name)
	}
	sort.Strings(actual)
	sort.Strings(classified)

	assert.Equal(t, classified, actual,
		"classify every exported user.Service method: write methods in cacheWriters (with invalidateAfterCommit and a cache test), the rest in notCacheWriters with a reason")
}
