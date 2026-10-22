// Command worker runs background jobs: it drains the outbox (email delivery)
// and runs the periodic billing jobs.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kangsolder001/saka/internal/billing"
	"github.com/kangsolder001/saka/internal/mail"
	"github.com/kangsolder001/saka/internal/org"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/outbox"
	"github.com/kangsolder001/saka/internal/platform/safego"
	"github.com/kangsolder001/saka/internal/platform/scheduler"
	"github.com/kangsolder001/saka/internal/user"
	emailtemplates "github.com/kangsolder001/saka/templates/email"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "worker:", err)
		os.Exit(1)
	}
}

// run is the composition root of the worker process.
func run() error {
	configPath := flag.String("config", config.DefaultPath, "path to the JSON config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	log, err := logger.New(os.Stdout, cfg.App.LogLevel)
	if err != nil {
		return err
	}
	base := log.Base()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.Connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer database.Close()

	clk := clock.Real{}
	runner := safego.NewRunner(log)

	renderer, err := mail.NewRenderer(emailtemplates.FS, mail.AppInfo{Name: cfg.App.Name})
	if err != nil {
		return err
	}
	mailer, err := newMailer(cfg, log, clk)
	if err != nil {
		return err
	}
	emailHandler := mail.NewJobHandler(renderer, mailer, log)

	worker, err := outbox.NewWorker(outbox.NewStore(database.Pool(), clk), clk, log, runner, outbox.WorkerConfig{
		Concurrency:  cfg.Worker.Concurrency,
		PollInterval: cfg.Worker.PollInterval.Std(),
		JobTimeout:   cfg.Worker.JobTimeout.Std(),
		Lease:        cfg.Worker.Lease.Std(),
	})
	if err != nil {
		return err
	}
	worker.Register(mail.JobTypeSendEmail, emailHandler.Handle)

	// Periodic jobs (spec §8.6) run beside the outbox worker, one at a time
	// across all workers (advisory locks) and stopped by the same context.
	jobs := scheduler.New(scheduler.NewPostgresLocker(database.Pool()), clk, runner, log)
	billingService, err := newBilling(cfg, log, clk, database, mail.NewService(renderer, outbox.NewStore(database.Pool(), clk)))
	if err != nil {
		return err
	}
	if err := billingService.RegisterJobs(jobs); err != nil {
		return err
	}
	runner.Go(ctx, "scheduler", jobs.Run)

	base.Info().Str("env", cfg.App.Env).Str("mail_driver", cfg.Mail.Driver).Msg("worker starting")
	if err := worker.Run(ctx); err != nil { // returns after in-flight jobs finish
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.App.ShutdownTimeout.Std())
	defer cancel()
	if err := runner.Shutdown(shutdownCtx); err != nil {
		return err
	}
	base.Info().Msg("worker stopped")
	return nil
}

// newBilling builds the billing service the jobs run on. The organization
// service is only the contact lookup for emails here (it never creates or
// deletes an organization in the worker), so it gets the dependencies of that.
func newBilling(cfg config.Config, log *logger.Logger, clk clock.Clock, database *db.DB, mailer mail.Sender) (*billing.Service, error) {
	providers, err := billing.NewProviders(cfg.Billing, clk)
	if err != nil {
		return nil, fmt.Errorf("payment providers: %w", err)
	}
	organizations := org.NewService(org.Dependencies{Clock: clk, Tx: database, Users: user.NewService(clk)})
	service := billing.NewService(clk)
	service.EnablePayments(billing.PaymentDependencies{
		Tx: database, Mail: mailer, Contacts: organizations, Providers: providers,
		Settings: billing.SettingsFrom(cfg.Billing), Log: log.Component("billing"),
	})
	return service, nil
}

func newMailer(cfg config.Config, log *logger.Logger, clk clock.Clock) (mail.Mailer, error) {
	switch cfg.Mail.Driver {
	case "smtp":
		return mail.NewSMTPMailer(mail.SMTPConfig{
			Host:     cfg.Mail.SMTP.Host,
			Port:     cfg.Mail.SMTP.Port,
			Username: cfg.Mail.SMTP.Username,
			Password: cfg.Mail.SMTP.Password,
			From:     cfg.Mail.From,
		}, clk)
	case "log":
		return mail.NewLogMailer(log), nil
	default:
		return nil, fmt.Errorf("unsupported mail driver %q", cfg.Mail.Driver)
	}
}
