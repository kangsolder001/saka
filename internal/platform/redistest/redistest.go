// Package redistest starts a real Redis with testcontainers-go for
// integration tests. One container is shared per test process; every test gets
// its own key prefix, so tests stay isolated without flushing the server.
package redistest

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/redisx"
)

const image = "redis:7.4-alpine"

var (
	once     sync.Once
	addr     string
	startErr error
)

func start(ctx context.Context) {
	c, err := tcredis.Run(ctx, image)
	if err != nil {
		startErr = err
		return
	}
	endpoint, err := c.Endpoint(ctx, "")
	if err != nil {
		startErr = err
		return
	}
	addr = endpoint
}

// Config returns a RedisConfig pointing at the shared container with a unique
// key prefix, for tests that build the application stack themselves.
func Config(t testing.TB) config.RedisConfig {
	t.Helper()
	once.Do(func() { start(context.Background()) })
	if startErr != nil {
		t.Fatalf("start redis container: %v", startErr)
	}
	cfg := config.Default().Redis
	cfg.Addr = addr
	cfg.KeyPrefix = "t" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
	return cfg
}

// NewClient connects to the shared container with a unique key prefix.
func NewClient(t testing.TB) *redisx.Client {
	t.Helper()
	c, err := redisx.Connect(context.Background(), Config(t))
	if err != nil {
		t.Fatalf("connect to redis: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}
