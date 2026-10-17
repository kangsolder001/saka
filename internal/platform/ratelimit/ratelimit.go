// Package ratelimit is a sliding-window rate limiter stored in Redis, so every
// API instance shares the same counters and decisions are fast. Each key is a
// sorted set of event timestamps; Lua scripts make every decision atomic.
package ratelimit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/redisx"
)

// Module is the Redis namespace of this package: <key_prefix>:ratelimit:.
const Module = "ratelimit"

// Rule is "at most Limit events per Window".
type Rule struct {
	// Name namespaces keys, e.g. "login".
	Name   string
	Limit  int
	Window time.Duration
}

// Decision is the outcome of a rate limit check.
type Decision struct {
	Allowed bool
	// Remaining events left in the current window.
	Remaining int
	// RetryAfter is how long to wait until an event is allowed again (zero when Allowed).
	RetryAfter time.Duration
}

// Key builds a storage key from a rule name and identifying parts (email,
// IP, user id, ...). The parts are hashed so personal data is never stored.
func Key(name string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return name + ":" + hex.EncodeToString(sum[:])
}

// take drops expired events, then counts one more if the limit allows it.
// KEYS[1] key; ARGV: now_ms, since_ms (events at or before it have expired),
// window_ms, limit, member. Returns {allowed, count, oldest_ms}.
var takeScript = redis.NewScript(`
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[2])
local count = redis.call('ZCARD', KEYS[1])
if count >= tonumber(ARGV[4]) then
  local oldest = redis.call('ZRANGE', KEYS[1], 0, 0, 'WITHSCORES')
  return {0, count, oldest[2]}
end
redis.call('ZADD', KEYS[1], ARGV[1], ARGV[5])
redis.call('PEXPIRE', KEYS[1], ARGV[3])
return {1, count + 1, ARGV[1]}
`)

// check counts the events inside the window without adding one.
// KEYS[1] key; ARGV: since_ms. Returns {count, oldest_ms}.
var checkScript = redis.NewScript(`
local count = redis.call('ZCOUNT', KEYS[1], '(' .. ARGV[1], '+inf')
local oldest = ARGV[1]
if count > 0 then
  local first = redis.call('ZRANGEBYSCORE', KEYS[1], '(' .. ARGV[1], '+inf', 'WITHSCORES', 'LIMIT', 0, 1)
  oldest = first[2]
end
return {count, oldest}
`)

// Limiter applies Rules using Redis.
type Limiter struct {
	ns  *redisx.Namespace
	rdb *redis.Client
	clk clock.Clock
}

// New creates a Limiter on the shared Redis connection.
func New(client *redisx.Client, clk clock.Clock) *Limiter {
	return &Limiter{ns: client.Namespace(Module), rdb: client.Raw(), clk: clk}
}

// Take atomically counts one event and reports whether it is allowed. Of any
// number of concurrent callers on the same key, no more than Rule.Limit are
// allowed per window. Denied attempts are not counted, so a client that keeps
// hammering does not extend its own lockout. If Redis is unreachable the call
// fails (the caller must not skip the limit).
func (l *Limiter) Take(ctx context.Context, key string, rule Rule) (Decision, error) {
	now := l.clk.Now()
	res, err := takeScript.Run(ctx, l.rdb, []string{l.ns.Key(key)},
		ms(now), ms(now.Add(-rule.Window)), rule.Window.Milliseconds(), rule.Limit, uuid.NewString()).Int64Slice()
	if err != nil {
		return Decision{}, fmt.Errorf("ratelimit: take %q: %w", rule.Name, err)
	}
	if res[0] == 1 {
		return Decision{Allowed: true, Remaining: rule.Limit - int(res[1])}, nil
	}
	return denied(rule, time.UnixMilli(res[2]), now), nil
}

// Check reports whether another event would be allowed, without counting one.
// Pair it with Record to count only some events, such as failed logins.
func (l *Limiter) Check(ctx context.Context, key string, rule Rule) (Decision, error) {
	now := l.clk.Now()
	res, err := checkScript.Run(ctx, l.rdb, []string{l.ns.Key(key)}, ms(now.Add(-rule.Window))).Int64Slice()
	if err != nil {
		return Decision{}, fmt.Errorf("ratelimit: check %q: %w", rule.Name, err)
	}
	hits := int(res[0])
	if hits < rule.Limit {
		return Decision{Allowed: true, Remaining: rule.Limit - hits}, nil
	}
	return denied(rule, time.UnixMilli(res[1]), now), nil
}

// Record counts one event without checking the limit.
func (l *Limiter) Record(ctx context.Context, key string, rule Rule) error {
	now := l.clk.Now()
	fullKey := l.ns.Key(key)
	_, err := l.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.ZRemRangeByScore(ctx, fullKey, "-inf", ms(now.Add(-rule.Window)))
		p.ZAdd(ctx, fullKey, redis.Z{Score: float64(now.UnixMilli()), Member: uuid.NewString()})
		p.PExpire(ctx, fullKey, rule.Window)
		return nil
	})
	if err != nil {
		return fmt.Errorf("ratelimit: record %q: %w", rule.Name, err)
	}
	return nil
}

// Reset forgets all events of a key (for example after a successful login).
func (l *Limiter) Reset(ctx context.Context, key string) error {
	if err := l.ns.Delete(ctx, key); err != nil {
		return fmt.Errorf("ratelimit: reset: %w", err)
	}
	return nil
}

func ms(t time.Time) string { return fmt.Sprint(t.UnixMilli()) }

// denied builds the Decision for a blocked event. oldest is the time of the
// oldest event inside the window; once it leaves the window a slot frees up.
func denied(rule Rule, oldest, now time.Time) Decision {
	retry := oldest.Add(rule.Window).Sub(now)
	if retry < time.Second {
		retry = time.Second
	}
	return Decision{Allowed: false, RetryAfter: retry}
}
