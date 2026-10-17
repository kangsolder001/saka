//go:build integration

package redisx_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/redistest"
	"github.com/kangsolder001/saka/internal/platform/redisx"
)

func TestConnect_PingsRedis(t *testing.T) {
	c := redistest.NewClient(t)

	assert.NoError(t, c.Ping(context.Background()))
}

func TestConnect_FailsWhenUnreachable(t *testing.T) {
	cfg := config.Default().Redis
	cfg.Addr = "127.0.0.1:1"

	_, err := redisx.Connect(context.Background(), cfg)

	assert.Error(t, err)
}

func TestNamespace_KeysCarryPrefixAndModule(t *testing.T) {
	c := redistest.NewClient(t)

	key := c.Namespace("user").Key("summary", "v1", "abc")

	assert.Regexp(t, `^t[0-9a-f]{20}:user:summary:v1:abc$`, key)
}

func TestNamespace_SetGetDelete(t *testing.T) {
	ns := redistest.NewClient(t).Namespace("demo")
	ctx := context.Background()

	_, found, err := ns.Get(ctx, "k")
	require.NoError(t, err)
	assert.False(t, found, "a missing key is not an error")

	require.NoError(t, ns.Set(ctx, "k", []byte("value"), time.Minute))
	got, found, err := ns.Get(ctx, "k")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, []byte("value"), got)

	require.NoError(t, ns.Delete(ctx, "k", "never-existed"))
	_, found, err = ns.Get(ctx, "k")
	require.NoError(t, err)
	assert.False(t, found)
	assert.NoError(t, ns.Delete(ctx), "deleting nothing is fine")
}

func TestNamespace_ValuesExpire(t *testing.T) {
	c := redistest.NewClient(t)
	ns := c.Namespace("demo")
	ctx := context.Background()
	require.NoError(t, ns.Set(ctx, "short", []byte("v"), 150*time.Millisecond))

	require.Eventually(t, func() bool {
		_, found, err := ns.Get(ctx, "short")
		return err == nil && !found
	}, 3*time.Second, 50*time.Millisecond, "keys must expire on their own")
}

func TestNamespace_RejectsNonExpiringValues(t *testing.T) {
	ns := redistest.NewClient(t).Namespace("demo")

	assert.Error(t, ns.Set(context.Background(), "k", []byte("v"), 0))
	assert.Error(t, ns.Set(context.Background(), "k", []byte("v"), -time.Second))
}

func TestNamespace_ModulesAndPrefixesDoNotCollide(t *testing.T) {
	a := redistest.NewClient(t) // each test client has its own prefix
	b := redistest.NewClient(t)
	ctx := context.Background()
	require.NoError(t, a.Namespace("user").Set(ctx, "k", []byte("a-user"), time.Minute))

	_, foundOtherModule, err := a.Namespace("billing").Get(ctx, "k")
	require.NoError(t, err)
	_, foundOtherPrefix, err := b.Namespace("user").Get(ctx, "k")
	require.NoError(t, err)

	assert.False(t, foundOtherModule, "modules have separate key spaces")
	assert.False(t, foundOtherPrefix, "services/environments with another key_prefix are separate too")
}
