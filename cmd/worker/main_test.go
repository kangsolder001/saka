package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/mail"
	"github.com/kangsolder001/saka/internal/mail/mocks"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/safego"
	"github.com/kangsolder001/saka/internal/platform/scheduler"
)

// The worker builds the billing service and registers its jobs from the
// configuration; a bad wiring (a missing provider, a zero interval) must fail
// at start, not when the first job is due.
func TestNewBilling_RegistersTheBillingJobsFromTheDefaultConfiguration(t *testing.T) {
	cfg := config.Default()
	log := logger.Nop()
	service, err := newBilling(cfg, log, clock.Real{}, nil, mocks.NewMockSender(t))
	require.NoError(t, err)
	jobs := scheduler.New(nil, clock.Real{}, safego.NewRunner(log), log)

	require.NoError(t, service.RegisterJobs(jobs))

	// Registering twice is a wiring bug and panics: proof that the five jobs are in.
	assert.Panics(t, func() { _ = service.RegisterJobs(jobs) })
}

func TestNewMailer_BuildsTheConfiguredDriver(t *testing.T) {
	cfg := config.Default()
	cfg.Mail.From = "Saka <noreply@example.com>"

	for driver, wantType := range map[string]any{"log": &mail.LogMailer{}, "smtp": &mail.SMTPMailer{}, "resend": &mail.ResendMailer{}} {
		cfg.Mail.Driver = driver
		cfg.Mail.Resend.APIKey = "re_test"

		mailer, err := newMailer(cfg, logger.Nop(), clock.Real{})

		require.NoError(t, err, driver)
		assert.IsType(t, wantType, mailer, driver)
	}
}

func TestNewMailer_RefusesAnUnknownDriverAndAResendWithoutAKey(t *testing.T) {
	cfg := config.Default()
	cfg.Mail.Driver = "pigeon"
	_, err := newMailer(cfg, logger.Nop(), clock.Real{})
	require.Error(t, err)

	cfg.Mail.Driver = "resend"
	cfg.Mail.Resend.APIKey = ""
	_, err = newMailer(cfg, logger.Nop(), clock.Real{})
	require.Error(t, err)
}

func TestNewBilling_RefusesAnUnknownProvider(t *testing.T) {
	cfg := config.Default()
	cfg.Billing.Provider = "stripe"

	_, err := newBilling(cfg, logger.Nop(), clock.Real{}, nil, mocks.NewMockSender(t))

	require.Error(t, err)
}
