// Package outbox implements the transactional outbox: services insert jobs in
// the same transaction as their data change, and a worker delivers them later
// (spec §10.1). Job types are generic; email is just one handler.
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/outbox/outboxdb"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

// NewJob describes a job to enqueue.
type NewJob struct {
	// Type selects the worker handler, e.g. "email.send".
	Type string
	// Payload is marshalled to JSON; nil becomes {}.
	Payload any
	// RunAt delays the first attempt; zero means as soon as possible.
	RunAt time.Time
}

// Enqueuer is what services depend on to schedule background work.
type Enqueuer interface {
	// Enqueue inserts the job using the caller's transaction, so the job
	// exists if and only if the caller's data change commits.
	Enqueue(ctx context.Context, tx db.DBTX, job NewJob) (uuid.UUID, error)
}

// Store persists outbox jobs. The pool is used by the worker; Enqueue always
// uses the transaction handed in by the caller.
type Store struct {
	pool db.DBTX
	clk  clock.Clock
}

var _ Enqueuer = (*Store)(nil)

// NewStore creates a Store backed by pool (used for claiming and finishing jobs).
func NewStore(pool db.DBTX, clk clock.Clock) *Store {
	return &Store{pool: pool, clk: clk}
}

// Enqueue implements Enqueuer. The trace ID in ctx is saved with the job so
// the worker's logs can be joined with the originating request.
func (s *Store) Enqueue(ctx context.Context, tx db.DBTX, job NewJob) (uuid.UUID, error) {
	if job.Type == "" {
		return uuid.Nil, errors.New("outbox: job type is required")
	}
	payload, err := marshalPayload(job.Payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("outbox: marshal payload for %q: %w", job.Type, err)
	}

	now := s.clk.Now()
	runAt := job.RunAt
	if runAt.IsZero() {
		runAt = now
	}

	var traceID *string
	if id := reqctx.TraceID(ctx); id != "" {
		traceID = &id
	}

	id, err := outboxdb.New(tx).InsertJob(ctx, outboxdb.InsertJobParams{
		Type:          job.Type,
		Payload:       payload,
		TraceID:       traceID,
		NextAttemptAt: runAt.UTC(),
		Now:           now,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("outbox: insert job %q: %w", job.Type, err)
	}
	return id, nil
}

func marshalPayload(p any) ([]byte, error) {
	if p == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(p)
}
