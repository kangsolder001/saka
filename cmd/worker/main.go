// Command worker runs background jobs: it drains the outbox (email delivery).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kangsolder001/saka/internal/mail"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/outbox"
	"github.com/kangsolder001/saka/internal/platform/safego"
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
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log, err := logger.New(os.Stdout, cfg.LogLevel)
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

	renderer, err := mail.NewRenderer(emailtemplates.FS, mail.AppInfo{Name: cfg.AppName})
	if err != nil {
		return err
	}
	mailer, err := newMailer(cfg, log, clk)
	if err != nil {
		return err
	}
	emailHandler := mail.NewJobHandler(renderer, mailer, log)

	worker, err := outbox.NewWorker(outbox.NewStore(database.Pool(), clk), clk, log, runner, outbox.WorkerConfig{
		Concurrency:  cfg.WorkerConcurrency,
		PollInterval: cfg.WorkerPollInterval,
		JobTimeout:   cfg.WorkerJobTimeout,
		Lease:        cfg.WorkerLease,
	})
	if err != nil {
		return err
	}
	worker.Register(mail.JobTypeSendEmail, emailHandler.Handle)

	base.Info().Str("env", cfg.AppEnv).Str("mail_driver", cfg.MailDriver).Msg("worker starting")
	if err := worker.Run(ctx); err != nil { // returns after in-flight jobs finish
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := runner.Shutdown(shutdownCtx); err != nil {
		return err
	}
	base.Info().Msg("worker stopped")
	return nil
}

func newMailer(cfg config.Config, log *logger.Logger, clk clock.Clock) (mail.Mailer, error) {
	switch cfg.MailDriver {
	case "smtp":
		return mail.NewSMTPMailer(mail.SMTPConfig{
			Host:     cfg.SMTPHost,
			Port:     cfg.SMTPPort,
			Username: cfg.SMTPUsername,
			Password: cfg.SMTPPassword,
			From:     cfg.MailFrom,
		}, clk)
	case "log":
		return mail.NewLogMailer(log), nil
	default:
		return nil, fmt.Errorf("unsupported mail driver %q", cfg.MailDriver)
	}
}
