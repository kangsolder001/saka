package scheduler_test

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

	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/safego"
	"github.com/kangsolder001/saka/internal/platform/scheduler"
)

// memoryLocker is a Locker whose locks live in this process.
type memoryLocker struct {
	mu   sync.Mutex
	held map[int64]bool
	fail error
}

func newMemoryLocker() *memoryLocker { return &memoryLocker{held: map[int64]bool{}} }

func (l *memoryLocker) TryLock(_ context.Context, key int64) (func(), bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fail != nil {
		return nil, false, l.fail
	}
	if l.held[key] {
		return nil, false, nil
	}
	l.held[key] = true
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		delete(l.held, key)
	}, true, nil
}

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

type fixture struct {
	s      *scheduler.Scheduler
	clk    *clock.Fake
	locker *memoryLocker
	logs   *syncBuffer
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	logs := &syncBuffer{}
	log, err := logger.New(logs, "debug")
	require.NoError(t, err)
	clk := clock.NewFake(time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC))
	locker := newMemoryLocker()
	return &fixture{s: scheduler.New(locker, clk, safego.NewRunner(log), log), clk: clk, locker: locker, logs: logs}
}

func counting(counter *atomic.Int32) func(context.Context) (scheduler.Counts, error) {
	return func(context.Context) (scheduler.Counts, error) {
		counter.Add(1)
		return scheduler.Counts{"done": 1}, nil
	}
}

func TestLockKey_IsStableAndDistinctPerJob(t *testing.T) {
	assert.Equal(t, scheduler.LockKey("billing.expire"), scheduler.LockKey("billing.expire"))
	names := []string{"billing.renewals", "billing.expire", "billing.apply_scheduled", "billing.reminders", "billing.reconcile"}
	seen := map[int64]string{}
	for _, n := range names {
		k := scheduler.LockKey(n)
		assert.NotContains(t, seen, k, "%s collides with %s", n, seen[k])
		seen[k] = n
	}
}

func TestRegister_RejectsWiringBugs(t *testing.T) {
	f := newFixture(t)
	ok := func(context.Context) (scheduler.Counts, error) { return nil, nil }

	assert.Panics(t, func() { f.s.Register(scheduler.Job{Interval: time.Hour, Run: ok}) }, "no name")
	assert.Panics(t, func() { f.s.Register(scheduler.Job{Name: "a", Run: ok}) }, "no interval")
	assert.Panics(t, func() { f.s.Register(scheduler.Job{Name: "a", Interval: time.Hour}) }, "no function")
	f.s.Register(scheduler.Job{Name: "a", Interval: time.Hour, Run: ok})
	assert.Panics(t, func() { f.s.Register(scheduler.Job{Name: "a", Interval: time.Hour, Run: ok}) }, "a name twice")
}

func TestRunOnce_RunsTheJobAndLogsItsCounts(t *testing.T) {
	f := newFixture(t)
	var n atomic.Int32
	f.s.Register(scheduler.Job{Name: "demo.job", Interval: time.Hour, Run: func(context.Context) (scheduler.Counts, error) {
		n.Add(1)
		return scheduler.Counts{"issued": 3, "skipped": 1}, nil
	}})

	res, err := f.s.RunOnce(context.Background(), "demo.job")

	require.NoError(t, err)
	assert.True(t, res.Ran)
	assert.Equal(t, scheduler.Counts{"issued": 3, "skipped": 1}, res.Counts)
	assert.Equal(t, int32(1), n.Load())
	out := f.logs.String()
	assert.Contains(t, out, `"job":"demo.job"`)
	assert.Contains(t, out, `"issued":3`)
	assert.Contains(t, out, `"message":"job finished"`)
	assert.Contains(t, out, `"trace_id"`, "a run is a traceable unit of work")
}

func TestRunOnce_UnknownJob(t *testing.T) {
	_, err := newFixture(t).s.RunOnce(context.Background(), "nope")

	require.Error(t, err)
}

func TestRunOnce_AJobAnotherWorkerIsRunningIsSkippedNotFailed(t *testing.T) {
	f := newFixture(t)
	var n atomic.Int32
	f.s.Register(scheduler.Job{Name: "demo.job", Interval: time.Hour, Run: counting(&n)})
	release, ok, err := f.locker.TryLock(context.Background(), scheduler.LockKey("demo.job")) // the other worker
	require.NoError(t, err)
	require.True(t, ok)

	res, err := f.s.RunOnce(context.Background(), "demo.job")

	require.NoError(t, err)
	assert.False(t, res.Ran)
	assert.Zero(t, n.Load())

	release()
	res, err = f.s.RunOnce(context.Background(), "demo.job")
	require.NoError(t, err)
	assert.True(t, res.Ran, "it runs once the other worker is done")
}

func TestRunOnce_TheLockIsReleasedWhenTheJobFailsOrPanics(t *testing.T) {
	f := newFixture(t)
	f.s.Register(scheduler.Job{Name: "fails", Interval: time.Hour, Run: func(context.Context) (scheduler.Counts, error) {
		return scheduler.Counts{"failed": 1}, errors.New("gateway down")
	}})
	f.s.Register(scheduler.Job{Name: "panics", Interval: time.Hour, Run: func(context.Context) (scheduler.Counts, error) {
		panic("boom")
	}})

	_, err := f.s.RunOnce(context.Background(), "fails")
	require.ErrorContains(t, err, "gateway down")
	_, err = f.s.RunOnce(context.Background(), "panics")
	require.Error(t, err, "a panic is an error, not a crash")

	assert.Empty(t, f.locker.held, "neither left its lock behind")
	assert.Contains(t, f.logs.String(), `"level":"error"`)
}

func TestRunOnce_ALockFailureIsReported(t *testing.T) {
	f := newFixture(t)
	f.s.Register(scheduler.Job{Name: "demo.job", Interval: time.Hour, Run: counting(new(atomic.Int32))})
	f.locker.fail = errors.New("database down")

	_, err := f.s.RunOnce(context.Background(), "demo.job")

	require.ErrorContains(t, err, "database down")
}

func TestRunOnce_TheTimeoutStopsALongJob(t *testing.T) {
	f := newFixture(t)
	f.s.Register(scheduler.Job{Name: "slow", Interval: time.Hour, Timeout: 20 * time.Millisecond, Run: func(ctx context.Context) (scheduler.Counts, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}})

	_, err := f.s.RunOnce(context.Background(), "slow")

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Empty(t, f.locker.held)
}

func TestRunDue_RunsEveryJobFirstThenEachWhenItsIntervalHasPassed(t *testing.T) {
	f := newFixture(t)
	var hourly, daily atomic.Int32
	f.s.Register(scheduler.Job{Name: "hourly", Interval: time.Hour, Run: counting(&hourly)})
	f.s.Register(scheduler.Job{Name: "daily", Interval: 24 * time.Hour, Run: counting(&daily)})

	assert.Equal(t, []string{"hourly", "daily"}, f.s.RunDue(context.Background()), "first call: everything, in registration order")
	assert.Empty(t, f.s.RunDue(context.Background()), "nothing is due straight away")

	f.clk.Advance(59 * time.Minute)
	assert.Empty(t, f.s.RunDue(context.Background()))
	f.clk.Advance(time.Minute)
	assert.Equal(t, []string{"hourly"}, f.s.RunDue(context.Background()))
	f.clk.Advance(23 * time.Hour)
	assert.Equal(t, []string{"hourly", "daily"}, f.s.RunDue(context.Background()))

	assert.Equal(t, int32(3), hourly.Load())
	assert.Equal(t, int32(2), daily.Load())
}

func TestRunDue_AFailingJobDoesNotStopTheOthersAndIsRetriedAfterItsInterval(t *testing.T) {
	f := newFixture(t)
	var after atomic.Int32
	var attempts atomic.Int32
	f.s.Register(scheduler.Job{Name: "flaky", Interval: time.Hour, Run: func(context.Context) (scheduler.Counts, error) {
		attempts.Add(1)
		return nil, errors.New("boom")
	}})
	f.s.Register(scheduler.Job{Name: "after", Interval: time.Hour, Run: counting(&after)})

	f.s.RunDue(context.Background())
	assert.Equal(t, int32(1), after.Load())

	f.s.RunDue(context.Background())
	assert.Equal(t, int32(1), attempts.Load(), "not hammered: it waits for its interval")
	f.clk.Advance(time.Hour)
	f.s.RunDue(context.Background())
	assert.Equal(t, int32(2), attempts.Load())
}

func TestRunDue_StopsWhenTheContextIsCancelled(t *testing.T) {
	f := newFixture(t)
	var n atomic.Int32
	f.s.Register(scheduler.Job{Name: "demo.job", Interval: time.Hour, Run: counting(&n)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.Empty(t, f.s.RunDue(ctx))
	assert.Zero(t, n.Load())
}

func TestRun_RunsJobsUntilTheContextIsCancelled(t *testing.T) {
	logs := &syncBuffer{}
	log, err := logger.New(logs, "info")
	require.NoError(t, err)
	runner := safego.NewRunner(log)
	s := scheduler.New(newMemoryLocker(), clock.Real{}, runner, log)
	var n atomic.Int32
	s.Register(scheduler.Job{Name: "tick", Interval: 20 * time.Millisecond, Run: counting(&n)})
	ctx, cancel := context.WithCancel(context.Background())

	runner.Go(ctx, "scheduler", s.Run)
	require.Eventually(t, func() bool { return n.Load() >= 3 }, 5*time.Second, 10*time.Millisecond, "it keeps running on its interval")
	cancel()

	shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	require.NoError(t, runner.Shutdown(shutdown), "the scheduler goroutine ends with the context")
	after := n.Load()
	time.Sleep(60 * time.Millisecond)
	assert.Equal(t, after, n.Load(), "and runs nothing afterwards")
}
