// Package scheduler runs the periodic jobs of the worker process (the billing
// jobs of spec §8.6). Several workers may run at once; a PostgreSQL advisory
// lock per job name makes sure at most one of them runs a given job at any
// moment. The lock is only a courtesy to avoid duplicate work: every job is
// also idempotent and safe to run concurrently on its own, so a lost or
// bypassed lock never corrupts anything.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
	"github.com/kangsolder001/saka/internal/platform/safego"
)

// Counts is what a run reports, e.g. {"issued": 3, "skipped": 1}: logged with
// the job name so a quiet or a failing job is visible.
type Counts = map[string]int

// Job is a periodic task.
type Job struct {
	// Name identifies the job in logs and in its advisory lock, e.g. "billing.expire".
	Name string
	// Interval is the minimum time between two runs started by this process.
	Interval time.Duration
	// Timeout bounds one run; zero means DefaultTimeout.
	Timeout time.Duration
	// Run does the work. It must be idempotent and safe to run twice and
	// concurrently, and must stop when ctx is cancelled.
	Run func(ctx context.Context) (Counts, error)
}

// DefaultTimeout bounds a run of a job that sets no Timeout.
const DefaultTimeout = 10 * time.Minute

// Locker grants a job name to one process at a time.
type Locker interface {
	// TryLock takes the exclusive lock for key without waiting. It reports
	// false when someone else holds it. release must be called exactly once
	// when the holder is done; it never blocks on the caller's context.
	TryLock(ctx context.Context, key int64) (release func(), ok bool, err error)
}

// Scheduler runs registered jobs on their intervals.
type Scheduler struct {
	locker Locker
	clk    clock.Clock
	runner *safego.Runner
	log    *logger.Logger
	jobs   []*entry
}

type entry struct {
	Job
	lastRun time.Time
	ran     bool
}

// New creates a Scheduler. Register jobs before calling Run.
func New(locker Locker, clk clock.Clock, runner *safego.Runner, log *logger.Logger) *Scheduler {
	return &Scheduler{locker: locker, clk: clk, runner: runner, log: log.Component("scheduler")}
}

// Register adds a job. A job without a name, interval or function, or with a
// name already used, is a wiring bug and panics.
func (s *Scheduler) Register(job Job) {
	switch {
	case job.Name == "":
		panic("scheduler: a job needs a name")
	case job.Interval <= 0:
		panic(fmt.Sprintf("scheduler: job %q needs a positive interval", job.Name))
	case job.Run == nil:
		panic(fmt.Sprintf("scheduler: job %q has no function", job.Name))
	}
	for _, e := range s.jobs {
		if e.Name == job.Name {
			panic(fmt.Sprintf("scheduler: job %q is registered twice", job.Name))
		}
	}
	if job.Timeout <= 0 {
		job.Timeout = DefaultTimeout
	}
	s.jobs = append(s.jobs, &entry{Job: job})
}

// LockKey is the advisory lock key of a job name: a stable hash, so every
// worker computes the same key.
func LockKey(name string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("saka:scheduler:" + name))
	return int64(h.Sum64()) //nolint:gosec // wrapping into the signed range is the point: any 64 bits are a valid key
}

// RunResult is the outcome of one RunOnce.
type RunResult struct {
	// Ran is false when another worker held the job's lock.
	Ran    bool
	Counts Counts
}

// RunOnce runs the named job now if no other worker is running it: it takes
// the job's advisory lock, runs it with its timeout (a panic becomes an
// error), logs the result and releases the lock. The job is not run, and
// nothing fails, when the lock is taken. Run uses it for every due job; tests
// call it directly.
func (s *Scheduler) RunOnce(ctx context.Context, name string) (RunResult, error) {
	var job *entry
	for _, e := range s.jobs {
		if e.Name == name {
			job = e
		}
	}
	if job == nil {
		return RunResult{}, fmt.Errorf("scheduler: no job named %q", name)
	}
	return s.run(ctx, job)
}

func (s *Scheduler) run(ctx context.Context, job *entry) (RunResult, error) {
	// A run is its own unit of work: it gets its own trace, so its log lines
	// (and the outbox jobs it creates) can be followed together.
	ctx = reqctx.WithTraceID(ctx, reqctx.NewTraceID())
	ctx = reqctx.WithRequestID(ctx, reqctx.NewRequestID())
	zl := s.log.FromContext(ctx)
	zl = zl.With().Str("job", job.Name).Logger()

	release, ok, err := s.locker.TryLock(ctx, LockKey(job.Name))
	if err != nil {
		return RunResult{}, fmt.Errorf("scheduler: lock %s: %w", job.Name, err)
	}
	if !ok {
		zl.Debug().Msg("job skipped: another worker is running it")
		return RunResult{}, nil
	}
	defer release()

	runCtx, cancel := context.WithTimeout(ctx, job.Timeout)
	defer cancel()
	started := s.clk.Now()
	var counts Counts
	err = s.runner.Do(runCtx, "job:"+job.Name, func(ctx context.Context) error {
		var err error
		counts, err = job.Run(ctx)
		return err
	})
	took := s.clk.Now().Sub(started)

	event := zl.Info()
	if err != nil {
		event = zl.Error().Err(err)
	}
	for _, k := range sortedKeys(counts) {
		event = event.Int(k, counts[k])
	}
	event.Dur("took_ms", took).Msg("job finished")
	return RunResult{Ran: true, Counts: counts}, err
}

// RunDue runs, in registration order, every job whose interval has elapsed
// since this process last ran it (every job on the first call). A job that
// fails is logged and tried again after its interval; it never stops the
// others. It returns the names of the jobs it ran.
func (s *Scheduler) RunDue(ctx context.Context) []string {
	var ran []string
	for _, job := range s.jobs {
		if ctx.Err() != nil {
			break
		}
		if job.ran && s.clk.Now().Sub(job.lastRun) < job.Interval {
			continue
		}
		job.ran, job.lastRun = true, s.clk.Now()
		res, _ := s.run(ctx, job) // failures are logged by run
		if res.Ran {
			ran = append(ran, job.Name)
		}
	}
	return ran
}

// maxSleep caps the wait between two looks at the schedule.
const maxSleep = time.Minute

// Run runs the jobs on their intervals until ctx is cancelled. Jobs run one
// after another in this single loop, so the scheduler never needs more than
// one database connection at a time besides the one the job itself uses. It
// returns when ctx is done; a job in flight is stopped through its context.
func (s *Scheduler) Run(ctx context.Context) {
	zl := s.log.Base()
	names := make([]string, len(s.jobs))
	for i, j := range s.jobs {
		names[i] = j.Name
	}
	zl.Info().Strs("jobs", names).Msg("scheduler started")
	for ctx.Err() == nil {
		s.RunDue(ctx)
		sleep(ctx, s.untilNextDue())
	}
	zl.Info().Msg("scheduler stopped")
}

// untilNextDue is how long to wait before some job may be due.
func (s *Scheduler) untilNextDue() time.Duration {
	wait := maxSleep
	now := s.clk.Now()
	for _, j := range s.jobs {
		if !j.ran {
			return 0
		}
		if d := j.lastRun.Add(j.Interval).Sub(now); d < wait {
			wait = d
		}
	}
	if wait < 10*time.Millisecond {
		wait = 10 * time.Millisecond
	}
	return wait
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func sortedKeys(m Counts) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Beginner opens transactions; *pgxpool.Pool implements it.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// PostgresLocker locks with transaction-level advisory locks
// (pg_try_advisory_xact_lock) held by an idle transaction for the duration of
// a run: if the process dies the connection closes and PostgreSQL releases the
// lock by itself, and there is no unlock call to forget.
type PostgresLocker struct{ db Beginner }

// NewPostgresLocker creates a Locker on a connection pool.
func NewPostgresLocker(db Beginner) *PostgresLocker { return &PostgresLocker{db: db} }

var _ Locker = (*PostgresLocker)(nil)

// TryLock implements Locker.
func (l *PostgresLocker) TryLock(ctx context.Context, key int64) (func(), bool, error) {
	tx, err := l.db.Begin(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("begin: %w", err)
	}
	rollback := func() {
		// Nothing was written; rolling back ends the transaction and with it the lock.
		if err := tx.Rollback(context.WithoutCancel(ctx)); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			_ = err // the connection is broken; PostgreSQL releases the lock when it notices
		}
	}
	var got bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1)", key).Scan(&got); err != nil {
		rollback()
		return nil, false, fmt.Errorf("try advisory lock: %w", err)
	}
	if !got {
		rollback()
		return nil, false, nil
	}
	return rollback, true, nil
}
