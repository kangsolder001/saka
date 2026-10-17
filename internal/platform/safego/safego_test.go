package safego_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
	"github.com/kangsolder001/saka/internal/platform/safego"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// syncBuffer lets the test read logs written from other goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newRunner(t *testing.T) (*safego.Runner, *syncBuffer) {
	t.Helper()
	buf := &syncBuffer{}
	log, err := logger.New(buf, "info")
	require.NoError(t, err)
	return safego.NewRunner(log), buf
}

func TestRunnerGo_RunsFunction(t *testing.T) {
	r, _ := newRunner(t)
	var ran atomic.Bool

	r.Go(context.Background(), "job", func(context.Context) { ran.Store(true) })

	require.NoError(t, r.Shutdown(context.Background()))
	assert.True(t, ran.Load())
}

func TestRunnerGo_RecoversPanicAndLogsWithTraceID(t *testing.T) {
	r, logs := newRunner(t)
	ctx := reqctx.WithTraceID(context.Background(), "trace-xyz")

	r.Go(ctx, "explosive", func(context.Context) { panic("boom") })

	require.NoError(t, r.Shutdown(context.Background()))
	out := logs.String()
	assert.Contains(t, out, `"goroutine":"explosive"`)
	assert.Contains(t, out, `"panic":"boom"`)
	assert.Contains(t, out, `"trace_id":"trace-xyz"`)
	assert.Contains(t, out, `"stack":`)
}

func TestRunnerGo_PanicDoesNotStopOtherGoroutines(t *testing.T) {
	r, _ := newRunner(t)
	var done atomic.Int32

	for range 5 {
		r.Go(context.Background(), "ok", func(context.Context) { done.Add(1) })
	}
	r.Go(context.Background(), "bad", func(context.Context) { panic("x") })

	require.NoError(t, r.Shutdown(context.Background()))
	assert.Equal(t, int32(5), done.Load())
}

func TestRunnerShutdown_TimesOutWhileGoroutineRuns(t *testing.T) {
	r, _ := newRunner(t)
	release := make(chan struct{})
	r.Go(context.Background(), "slow", func(context.Context) { <-release })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := r.Shutdown(ctx)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	close(release) // let the goroutine finish so goleak stays quiet
	require.NoError(t, r.Shutdown(context.Background()))
}

func TestRunnerGo_StopsOnContextCancel(t *testing.T) {
	r, _ := newRunner(t)
	ctx, cancel := context.WithCancel(context.Background())

	r.Go(ctx, "waiter", func(ctx context.Context) { <-ctx.Done() })
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	assert.NoError(t, r.Shutdown(shutdownCtx))
}

func TestGroup_WaitReturnsNilWhenAllSucceed(t *testing.T) {
	r, _ := newRunner(t)
	g := r.NewGroup(context.Background(), 0)
	var n atomic.Int32

	for range 10 {
		g.Go("task", func(context.Context) error { n.Add(1); return nil })
	}

	require.NoError(t, g.Wait())
	assert.Equal(t, int32(10), n.Load())
}

func TestGroup_FirstErrorCancelsOthers(t *testing.T) {
	r, _ := newRunner(t)
	g := r.NewGroup(context.Background(), 0)
	wantErr := errors.New("failed")
	var cancelled atomic.Bool

	g.Go("waiter", func(ctx context.Context) error {
		<-ctx.Done()
		cancelled.Store(true)
		return ctx.Err()
	})
	g.Go("failing", func(context.Context) error { return wantErr })

	assert.ErrorIs(t, g.Wait(), wantErr)
	assert.True(t, cancelled.Load())
}

func TestGroup_PanicBecomesError(t *testing.T) {
	r, logs := newRunner(t)
	g := r.NewGroup(context.Background(), 0)

	g.Go("explosive", func(context.Context) error { panic(errors.New("kaboom")) })

	err := g.Wait()
	var pe *safego.PanicError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, "explosive", pe.Name)
	assert.NotEmpty(t, pe.Stack)
	assert.EqualError(t, errors.Unwrap(pe), "kaboom")
	assert.Contains(t, logs.String(), `"goroutine":"explosive"`)
}

func TestGroup_HonoursConcurrencyLimit(t *testing.T) {
	r, _ := newRunner(t)
	const limit = 3
	g := r.NewGroup(context.Background(), limit)
	var running, peak atomic.Int32

	for range 20 {
		g.Go("task", func(context.Context) error {
			cur := running.Add(1)
			for {
				p := peak.Load()
				if cur <= p || peak.CompareAndSwap(p, cur) {
					break
				}
			}
			time.Sleep(2 * time.Millisecond)
			running.Add(-1)
			return nil
		})
	}

	require.NoError(t, g.Wait())
	assert.LessOrEqual(t, peak.Load(), int32(limit))
	assert.Positive(t, peak.Load())
}

func TestGroup_ParentCancelStopsMembers(t *testing.T) {
	r, _ := newRunner(t)
	ctx, cancel := context.WithCancel(context.Background())
	g := r.NewGroup(ctx, 0)

	g.Go("waiter", func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
	cancel()

	assert.ErrorIs(t, g.Wait(), context.Canceled)
}

func TestRunnerDo_ReturnsFunctionError(t *testing.T) {
	r, _ := newRunner(t)
	want := errors.New("plain failure")

	err := r.Do(context.Background(), "job", func(context.Context) error { return want })

	assert.ErrorIs(t, err, want)
}

func TestRunnerDo_ConvertsPanicToErrorAndKeepsCallerAlive(t *testing.T) {
	r, logs := newRunner(t)
	ctx := reqctx.WithTraceID(context.Background(), "trace-do")

	err := r.Do(ctx, "explosive", func(context.Context) error { panic("kaboom") })

	var pe *safego.PanicError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, "explosive", pe.Name)
	assert.Contains(t, logs.String(), `"trace_id":"trace-do"`)
	assert.Contains(t, logs.String(), `"panic":"kaboom"`)
}

func TestRunnerDo_NilOnSuccess(t *testing.T) {
	r, _ := newRunner(t)

	assert.NoError(t, r.Do(context.Background(), "ok", func(context.Context) error { return nil }))
}
