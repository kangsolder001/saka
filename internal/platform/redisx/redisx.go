// Package redisx wraps the shared Redis connection. One Redis serves many
// modules, so every module works through a Namespace that prefixes its keys
// with <key_prefix>:<module>: and keeps modules from colliding. Redis holds
// temporary state (rate limits, caches); PostgreSQL stays the source of truth.
package redisx

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kangsolder001/saka/internal/platform/config"
)

const (
	dialTimeout  = 3 * time.Second
	ioTimeout    = 2 * time.Second
	connectLimit = 5 * time.Second
)

// Client is the shared Redis connection.
type Client struct {
	rdb    *redis.Client
	prefix string
}

// Connect opens the connection described by cfg and verifies it with a ping.
func Connect(ctx context.Context, cfg config.RedisConfig) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  dialTimeout,
		ReadTimeout:  ioTimeout,
		WriteTimeout: ioTimeout,
	})
	c := &Client{rdb: rdb, prefix: cfg.KeyPrefix}

	pingCtx, cancel := context.WithTimeout(ctx, connectLimit)
	defer cancel()
	if err := c.Ping(pingCtx); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("connect to redis: %w", err)
	}
	return c, nil
}

// Ping checks that Redis is reachable (used by /readyz).
func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

// Close releases the connection pool.
func (c *Client) Close() error { return c.rdb.Close() }

// Raw returns the underlying go-redis client for operations Namespace does not
// cover (Lua scripts, sorted sets). Callers must still build keys with
// Namespace.Key.
func (c *Client) Raw() *redis.Client { return c.rdb }

// Namespace returns the key space of one module.
func (c *Client) Namespace(module string) *Namespace {
	return &Namespace{rdb: c.rdb, prefix: c.prefix + ":" + module + ":"}
}

// Namespace is a module's slice of Redis: all its keys start with
// <key_prefix>:<module>:.
type Namespace struct {
	rdb    *redis.Client
	prefix string
}

// Key builds a full key from parts, e.g. Key("summary", "v1", id).
func (n *Namespace) Key(parts ...string) string {
	return n.prefix + strings.Join(parts, ":")
}

// Get returns the value of key; found is false when it does not exist.
func (n *Namespace) Get(ctx context.Context, key string) (value []byte, found bool, err error) {
	v, err := n.rdb.Get(ctx, n.Key(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("redis get: %w", err)
	}
	return v, true, nil
}

// Set stores value under key and expires it after ttl. A ttl of zero or less
// is rejected: nothing in this system should live in Redis forever.
func (n *Namespace) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl <= 0 {
		return errors.New("redis set: ttl must be positive")
	}
	if err := n.rdb.Set(ctx, n.Key(key), value, ttl).Err(); err != nil {
		return fmt.Errorf("redis set: %w", err)
	}
	return nil
}

// Delete removes the keys; missing keys are not an error.
func (n *Namespace) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	full := make([]string, len(keys))
	for i, k := range keys {
		full[i] = n.Key(k)
	}
	if err := n.rdb.Del(ctx, full...).Err(); err != nil {
		return fmt.Errorf("redis delete: %w", err)
	}
	return nil
}
