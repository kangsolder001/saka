//go:build integration

package outbox_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/dbtest"
	"github.com/kangsolder001/saka/internal/platform/outbox"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

var epoch = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

type env struct {
	db    *db.DB
	clock *clock.Fake
	store *outbox.Store
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dsn := dbtest.NewDatabase(t)
	dbtest.Migrate(t, dsn)
	cfg := config.Default()
	cfg.Database.URL = dsn
	d, err := db.Connect(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(d.Close)

	clk := clock.NewFake(epoch)
	return &env{db: d, clock: clk, store: outbox.NewStore(d.Pool(), clk)}
}

type row struct {
	Type          string
	Payload       string
	Status        string
	Attempts      int
	NextAttemptAt time.Time
	LastError     *string
	TraceID       *string
}

func (e *env) jobs(t *testing.T) []row {
	t.Helper()
	rows, err := e.db.Pool().Query(context.Background(),
		`SELECT type, payload::text, status, attempts, next_attempt_at, last_error, trace_id
		 FROM outbox_jobs ORDER BY created_at, id`)
	require.NoError(t, err)
	defer rows.Close()
	var out []row
	for rows.Next() {
		var r row
		require.NoError(t, rows.Scan(&r.Type, &r.Payload, &r.Status, &r.Attempts, &r.NextAttemptAt, &r.LastError, &r.TraceID))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

func TestEnqueue_InsertsPendingJobWhenTransactionCommits(t *testing.T) {
	e := newEnv(t)
	ctx := reqctx.WithTraceID(context.Background(), "4bf92f3577b34da6a3ce929d0e0e4736")

	var id uuid.UUID
	err := e.db.WithTx(ctx, func(tx pgx.Tx) error {
		var err error
		id, err = e.store.Enqueue(ctx, tx, outbox.NewJob{Type: "email.send", Payload: map[string]string{"to": "a@b.co"}})
		return err
	})

	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, id)
	jobs := e.jobs(t)
	require.Len(t, jobs, 1)
	assert.Equal(t, "email.send", jobs[0].Type)
	assert.JSONEq(t, `{"to":"a@b.co"}`, jobs[0].Payload)
	assert.Equal(t, "pending", jobs[0].Status)
	assert.Equal(t, 0, jobs[0].Attempts)
	assert.True(t, epoch.Equal(jobs[0].NextAttemptAt), "due at the fake clock's now")
	require.NotNil(t, jobs[0].TraceID)
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", *jobs[0].TraceID)
}

func TestEnqueue_RollbackLeavesNoJob(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	err := e.db.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := e.store.Enqueue(ctx, tx, outbox.NewJob{Type: "email.send"}); err != nil {
			return err
		}
		return errors.New("the business change failed afterwards")
	})

	require.Error(t, err)
	assert.Empty(t, e.jobs(t), "a rolled-back transaction must not leave a job behind")
}

func TestEnqueue_WithoutTraceIDStoresNull(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	require.NoError(t, e.db.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := e.store.Enqueue(ctx, tx, outbox.NewJob{Type: "x"})
		return err
	}))

	jobs := e.jobs(t)
	require.Len(t, jobs, 1)
	assert.Nil(t, jobs[0].TraceID)
	assert.JSONEq(t, `{}`, jobs[0].Payload, "nil payload becomes an empty object")
}

func TestEnqueue_RunAtDelaysFirstAttempt(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	runAt := epoch.Add(time.Hour)

	require.NoError(t, e.db.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := e.store.Enqueue(ctx, tx, outbox.NewJob{Type: "x", RunAt: runAt})
		return err
	}))

	assert.True(t, runAt.Equal(e.jobs(t)[0].NextAttemptAt))
}

func TestEnqueue_Validation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	require.NoError(t, e.db.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := e.store.Enqueue(ctx, tx, outbox.NewJob{})
		assert.Error(t, err, "empty type")

		_, err = e.store.Enqueue(ctx, tx, outbox.NewJob{Type: "x", Payload: make(chan int)})
		assert.Error(t, err, "unmarshalable payload")
		return nil
	}))

	assert.Empty(t, e.jobs(t))
}

func traceCtx(id string) context.Context {
	return reqctx.WithTraceID(context.Background(), id)
}
