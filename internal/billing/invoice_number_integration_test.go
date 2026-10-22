//go:build integration

package billing_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNextInvoiceNumber_IsGlobalAndFollowsTheClockYear(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	first, err := e.svc.NextInvoiceNumber(ctx, e.db.Pool())
	require.NoError(t, err)
	e.clock.Set(time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC))
	second, err := e.svc.NextInvoiceNumber(ctx, e.db.Pool())
	require.NoError(t, err)

	assert.Equal(t, "INV-2026-000001", first)
	assert.Equal(t, "INV-2027-000002", second, "the sequence does not restart with the year")
}

// The application role needs USAGE on the sequence; the default privileges of
// migration 00007 must cover a sequence created later (verified here through
// the real application pool, which runs as saka_app).
func TestNextInvoiceNumber_WorksForTheApplicationRoleInATenantTransaction(t *testing.T) {
	e := newEnv(t)
	orgID := e.newOrg()

	e.inTenant(orgID, func(ctx context.Context, tx pgx.Tx) {
		number, err := e.svc.NextInvoiceNumber(ctx, tx)
		require.NoError(t, err)
		assert.Equal(t, "INV-2026-000001", number)
	})
}

func TestNextInvoiceNumber_ParallelCallersNeverShareANumber(t *testing.T) {
	e := newEnv(t)
	const callers = 20
	numbers := make([]string, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			numbers[i], errs[i] = e.svc.NextInvoiceNumber(context.Background(), e.db.Pool())
		}()
	}
	wg.Wait()

	seen := map[string]bool{}
	for i, n := range numbers {
		require.NoError(t, errs[i])
		assert.False(t, seen[n], "number %s was handed out twice", n)
		seen[n] = true
	}
	assert.Len(t, seen, callers)
}

// A rolled back checkout still consumed its number: the next invoice skips it.
func TestNextInvoiceNumber_ARolledBackTransactionLeavesAGapNotAReuse(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	errRollback := errors.New("rollback")
	var lost string
	err := e.db.WithTx(ctx, func(tx pgx.Tx) error {
		var err error
		lost, err = e.svc.NextInvoiceNumber(ctx, tx)
		require.NoError(t, err)
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)

	next, err := e.svc.NextInvoiceNumber(ctx, e.db.Pool())

	require.NoError(t, err)
	assert.Equal(t, "INV-2026-000001", lost)
	assert.Equal(t, "INV-2026-000002", next)
}
