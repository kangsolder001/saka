package user

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/logger"
)

// Summary is the part of a user that is safe and useful to cache: everything
// except the password hash. It is what request authentication needs on every
// call (is the account active? is the email verified?).
type Summary struct {
	ID              uuid.UUID  `json:"id"`
	Email           string     `json:"email"`
	Name            string     `json:"name"`
	AvatarURL       *string    `json:"avatar_url,omitempty"`
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty"`
	Status          Status     `json:"status"`
}

// EmailVerified reports whether the user has confirmed their email address.
func (s Summary) EmailVerified() bool { return s.EmailVerifiedAt != nil }

// Summary returns the cacheable view of the user.
func (u User) Summary() Summary {
	return Summary{
		ID:              u.ID,
		Email:           u.Email,
		Name:            u.Name,
		AvatarURL:       u.AvatarURL,
		EmailVerifiedAt: u.EmailVerifiedAt,
		Status:          u.Status,
	}
}

// Cache is the key-value store the user module caches summaries in. It is
// implemented by redisx.Namespace; keys are relative to the module's namespace.
type Cache interface {
	Get(ctx context.Context, key string) (value []byte, found bool, err error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
}

// Option customizes a Service.
type Option func(*Service)

// WithCache turns on caching of user summaries. The database stays the source
// of truth: entries expire after ttl even if an invalidation is missed, and
// any cache failure falls back to the database.
func WithCache(cache Cache, ttl time.Duration, log *logger.Logger) Option {
	return func(s *Service) {
		s.cache = cache
		s.cacheTTL = ttl
		s.log = log.Component("user")
	}
}

// summaryKey is versioned so the cached shape can change without clearing Redis.
func summaryKey(id uuid.UUID) string { return "summary:v1:" + id.String() }

// GetSummary returns the user's summary, from the cache when possible and from
// the database otherwise (then caching it). Pass the connection pool as q:
// reads inside a transaction must use GetByID so they see their own writes.
// Missing users are not cached.
func (s *Service) GetSummary(ctx context.Context, q db.DBTX, id uuid.UUID) (Summary, error) {
	if s.cache != nil {
		if sum, ok := s.cached(ctx, id); ok {
			return sum, nil
		}
	}

	u, err := s.GetByID(ctx, q, id)
	if err != nil {
		return Summary{}, err
	}
	sum := u.Summary()
	if s.cache != nil {
		s.store(ctx, sum)
	}
	return sum, nil
}

func (s *Service) cached(ctx context.Context, id uuid.UUID) (Summary, bool) {
	raw, found, err := s.cache.Get(ctx, summaryKey(id))
	if err != nil {
		s.warn(ctx, err, "user cache read failed; using the database")
		return Summary{}, false
	}
	if !found {
		return Summary{}, false
	}
	var sum Summary
	if err := json.Unmarshal(raw, &sum); err != nil || sum.ID != id {
		// Corrupt or foreign entry: drop it and treat as a miss.
		_ = s.cache.Delete(ctx, summaryKey(id))
		return Summary{}, false
	}
	return sum, true
}

func (s *Service) store(ctx context.Context, sum Summary) {
	raw, err := json.Marshal(sum)
	if err != nil {
		s.warn(ctx, err, "user cache encode failed")
		return
	}
	if err := s.cache.Set(ctx, summaryKey(sum.ID), raw, s.cacheTTL); err != nil {
		s.warn(ctx, err, "user cache write failed")
	}
}

// InvalidateCache removes the cached summaries of the given users. Call it
// AFTER the transaction that changed a user has committed: invalidating before
// the commit lets a concurrent reader re-cache the old row. If it fails the
// stale entry lives at most one TTL, so callers should log the error loudly
// rather than fail the (already committed) operation.
func (s *Service) InvalidateCache(ctx context.Context, ids ...uuid.UUID) error {
	if s.cache == nil || len(ids) == 0 {
		return nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = summaryKey(id)
	}
	if err := s.cache.Delete(ctx, keys...); err != nil {
		return fmt.Errorf("user: invalidate cache: %w", err)
	}
	return nil
}

// invalidateAfterCommit drops the user's cached summary once the write on q is
// durable (see db.AfterCommit). EVERY method of Service that changes or deletes
// a user must end with it, so no caller (an update or delete endpoint) can
// forget to clear the cache. A failure is logged, not returned: the data change
// already happened, and the stale entry expires within the cache TTL.
func (s *Service) invalidateAfterCommit(ctx context.Context, q db.DBTX, id uuid.UUID) {
	if s.cache == nil {
		return
	}
	// The request may be finished by the time the transaction commits.
	ctx = context.WithoutCancel(ctx)
	db.AfterCommit(q, func() {
		if err := s.InvalidateCache(ctx, id); err != nil {
			s.warn(ctx, err, "user cache invalidation failed; entry stays stale until its TTL")
		}
	})
}

func (s *Service) warn(ctx context.Context, err error, msg string) {
	if s.log == nil {
		return
	}
	zl := s.log.FromContext(ctx)
	zl.Warn().Err(err).Msg(msg)
}
