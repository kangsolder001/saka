package midtrans_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/billing/payment"
	"github.com/kangsolder001/saka/internal/billing/payment/midtrans"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/config"
)

// findConfig walks up from the package directory to the repository's
// config.json (gitignored, so it only exists on a developer machine).
func findConfig(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		candidate := filepath.Join(dir, config.DefaultPath)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// TestSandbox_CreatePaymentReturnsAValidPaymentLink is the optional manual
// check behind the first billing acceptance criterion (spec §12): it creates
// a real Snap transaction in the Midtrans SANDBOX and verifies the payment
// link. It only runs when config.json (never an environment variable) sets
// billing.provider to "midtrans" with a sandbox server key ("SB-" prefix);
// otherwise it is skipped, so CI and fresh checkouts are unaffected.
//
// It never touches production: a key that does not start with "SB-" or a
// host that is not the sandbox makes the test skip.
func TestSandbox_CreatePaymentReturnsAValidPaymentLink(t *testing.T) {
	path := findConfig(t)
	if path == "" {
		t.Skip("no config.json: set billing.provider=midtrans and a sandbox server key to run this test")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Skipf("config.json is not usable: %v", err)
	}
	m := cfg.Billing.Midtrans
	if cfg.Billing.Provider != config.BillingProviderMidtrans || !strings.HasPrefix(m.ServerKey, "SB-") {
		t.Skip("config.json does not select the midtrans provider with a sandbox (SB-) server key")
	}
	if !strings.Contains(m.SnapBaseURL, "sandbox") || !strings.Contains(m.APIBaseURL, "sandbox") {
		t.Skip("config.json does not point at the Midtrans sandbox hosts")
	}

	clk := clock.Real{}
	provider, err := midtrans.New(midtrans.Config{
		ServerKey: m.ServerKey, SnapBaseURL: m.SnapBaseURL, APIBaseURL: m.APIBaseURL, Timeout: m.RequestTimeout.Std(),
	}, clk)
	require.NoError(t, err)

	// A unique order id per run: Midtrans refuses a reused order_id.
	number := fmt.Sprintf("INV-SANDBOX-%d", clk.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	session, err := provider.CreatePayment(ctx, payment.PaymentRequest{
		InvoiceNumber: number,
		Amount:        99000,
		Currency:      "IDR",
		Description:   "Saka sandbox check",
		Customer:      payment.Customer{Name: "Sandbox Check", Email: "sandbox-check@example.com"},
		ExpiresAt:     clk.Now().Add(time.Hour),
	})
	require.NoError(t, err)
	require.Equal(t, number, session.Ref)

	link, err := url.Parse(session.URL)
	require.NoError(t, err)
	require.Equal(t, "https", link.Scheme)
	require.Contains(t, link.Host, "midtrans", "the payment link must point at Midtrans")

	// Nobody has opened the link, so Midtrans either has no such payment yet
	// or reports it as pending; both prove the status call round-trips.
	status, err := provider.GetPaymentStatus(ctx, number)
	if err != nil {
		require.True(t, errors.Is(err, payment.ErrNotFound), "unexpected error: %v", err)
		return
	}
	require.Equal(t, payment.StatePending, status.State)
}
