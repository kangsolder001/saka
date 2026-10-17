// Package safego is the only sanctioned way to start goroutines in
// application code (spec §11.2). It recovers panics, logs them with the
// request's trace ID and tracks goroutines so shutdown can wait for them.
//
// Recovering a panic only keeps the process alive. Preventing leaks is the
// caller's job: every function passed here must return when its context is
// cancelled.
package safego

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/kangsolder001/saka/internal/platform/logger"
)

// PanicError is the error produced when a goroutine panics inside a Group.
type PanicError struct {
	Name  string
	Value any
	Stack []byte
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("goroutine %q panicked: %v", e.Name, e.Value)
}

// Unwrap returns the panic value when it was an error.
func (e *PanicError) Unwrap() error {
	err, _ := e.Value.(error)
	return err
}

// Runner starts and tracks goroutines. Create one per process in the
// composition root and inject it where concurrency is needed.
type Runner struct {
	log *logger.Logger
	wg  sync.WaitGroup
}

// NewRunner creates a Runner that reports panics to log.
func NewRunner(log *logger.Logger) *Runner {
	return &Runner{log: log.Component("safego")}
}

// Go runs fn in a new goroutine (fire and forget). A panic is recovered and
// logged; it never crashes the process. The goroutine is tracked until it
// returns so Shutdown can wait for it.
func (r *Runner) Go(ctx context.Context, name string, fn func(ctx context.Context)) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer r.recoverAndLog(ctx, name)
		fn(ctx)
	}()
}

// Shutdown waits for every goroutine started with Go to finish, or until ctx
// is done, whichever comes first. If ctx ends first, the tracked goroutines
// are still running; the caller must have cancelled their context.
func (r *Runner) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for goroutines: %w", ctx.Err())
	}
}

func (r *Runner) recoverAndLog(ctx context.Context, name string) {
	if rec := recover(); rec != nil {
		r.logPanic(ctx, name, rec, debug.Stack())
	}
}

func (r *Runner) logPanic(ctx context.Context, name string, value any, stack []byte) {
	zl := r.log.FromContext(ctx)
	zl.Error().
		Str("goroutine", name).
		Str("panic", fmt.Sprint(value)).
		Bytes("stack", stack).
		Msg("goroutine panicked")
}

// Group runs related goroutines and waits for all of them (fan-out/fan-in).
// The first error cancels the context given to the others.
type Group struct {
	runner *Runner
	eg     *errgroup.Group
	ctx    context.Context
}

// NewGroup creates a Group bound to ctx. limit caps concurrently running
// goroutines; a value <= 0 means no limit, which should be rare.
func (r *Runner) NewGroup(ctx context.Context, limit int) *Group {
	eg, gctx := errgroup.WithContext(ctx)
	if limit > 0 {
		eg.SetLimit(limit)
	}
	return &Group{runner: r, eg: eg, ctx: gctx}
}

// Go runs fn in a goroutine of the group. fn receives a context that is
// cancelled when any group member fails. A panic is recovered, logged and
// returned from Wait as a *PanicError. When the limit is reached Go blocks
// until a slot frees up.
func (g *Group) Go(name string, fn func(ctx context.Context) error) {
	g.eg.Go(func() (err error) {
		defer func() {
			if rec := recover(); rec != nil {
				stack := debug.Stack()
				g.runner.logPanic(g.ctx, name, rec, stack)
				err = &PanicError{Name: name, Value: rec, Stack: stack}
			}
		}()
		return fn(g.ctx)
	})
}

// Wait blocks until every goroutine of the group returned and reports the
// first error (or panic).
func (g *Group) Wait() error {
	return g.eg.Wait()
}
