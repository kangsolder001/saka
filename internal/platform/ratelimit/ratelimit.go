// Package ratelimit is a sliding-window rate limiter stored in PostgreSQL, so
// every API instance shares the same counters without extra infrastructure.
package ratelimit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/ratelimit/ratelimitdb"
)

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

// Transactor runs a function in a transaction; *db.DB implements it.
type Transactor interface {
	WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error
}

var _ Transactor = (*db.DB)(nil)

// Limiter applies Rules using the database.
type Limiter struct {
	tx   Transactor
	pool db.DBTX
	clk  clock.Clock
}

// New creates a Limiter. pool is used for read-only checks and writes that
// need no locking; tx for the atomic Take.
func New(tx Transactor, pool db.DBTX, clk clock.Clock) *Limiter {
	return &Limiter{tx: tx, pool: pool, clk: clk}
}

// Take atomically counts one event and reports whether it is allowed. Of any
// number of concurrent callers on the same key, no more than Rule.Limit are
// allowed per window. Denied attempts are not counted, so a client that keeps
// hammering does not extend its own lockout.
func (l *Limiter) Take(ctx context.Context, key string, rule Rule) (Decision, error) {
	var decision Decision
	err := l.tx.WithTx(ctx, func(tx pgx.Tx) error {
		q := ratelimitdb.New(tx)
		if err := q.LockKey(ctx, key); err != nil {
			return fmt.Errorf("lock key: %w", err)
		}
		now := l.clk.Now()
		since := now.Add(-rule.Window)
		if err := q.DeleteExpiredHits(ctx, ratelimitdb.DeleteExpiredHitsParams{Key: key, Since: since}); err != nil {
			return fmt.Errorf("drop expired hits: %w", err)
		}
		count, err := q.CountHits(ctx, ratelimitdb.CountHitsParams{Key: key, Since: since})
		if err != nil {
			return fmt.Errorf("count hits: %w", err)
		}
		decision = decide(rule, int(count.Hits), count.Oldest, now)
		if !decision.Allowed {
			return nil
		}
		if err := q.InsertHit(ctx, ratelimitdb.InsertHitParams{Key: key, Now: now}); err != nil {
			return fmt.Errorf("record hit: %w", err)
		}
		decision.Remaining--
		return nil
	})
	if err != nil {
		return Decision{}, fmt.Errorf("ratelimit: take %q: %w", rule.Name, err)
	}
	return decision, nil
}

// Check reports whether another event would be allowed, without counting one.
// Pair it with Record to count only some events, such as failed logins.
func (l *Limiter) Check(ctx context.Context, key string, rule Rule) (Decision, error) {
	now := l.clk.Now()
	count, err := ratelimitdb.New(l.pool).CountHits(ctx, ratelimitdb.CountHitsParams{Key: key, Since: now.Add(-rule.Window)})
	if err != nil {
		return Decision{}, fmt.Errorf("ratelimit: check %q: %w", rule.Name, err)
	}
	return decide(rule, int(count.Hits), count.Oldest, now), nil
}

// Record counts one event without checking the limit.
func (l *Limiter) Record(ctx context.Context, key string, rule Rule) error {
	q := ratelimitdb.New(l.pool)
	now := l.clk.Now()
	if err := q.DeleteExpiredHits(ctx, ratelimitdb.DeleteExpiredHitsParams{Key: key, Since: now.Add(-rule.Window)}); err != nil {
		return fmt.Errorf("ratelimit: drop expired hits: %w", err)
	}
	if err := q.InsertHit(ctx, ratelimitdb.InsertHitParams{Key: key, Now: now}); err != nil {
		return fmt.Errorf("ratelimit: record %q: %w", rule.Name, err)
	}
	return nil
}

// Reset forgets all events of a key (for example after a successful login).
func (l *Limiter) Reset(ctx context.Context, key string) error {
	if err := ratelimitdb.New(l.pool).DeleteHits(ctx, key); err != nil {
		return fmt.Errorf("ratelimit: reset: %w", err)
	}
	return nil
}

// decide turns the current count into a Decision. oldest is the time of the
// oldest event inside the window.
func decide(rule Rule, hits int, oldest, now time.Time) Decision {
	if hits < rule.Limit {
		return Decision{Allowed: true, Remaining: rule.Limit - hits}
	}
	retry := oldest.Add(rule.Window).Sub(now)
	if retry < time.Second {
		retry = time.Second
	}
	return Decision{Allowed: false, RetryAfter: retry}
}
