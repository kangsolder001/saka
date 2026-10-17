// Package config loads and validates application settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

const (
	EnvDevelopment = "development"
	EnvTest        = "test"
	EnvProduction  = "production"
)

// Config holds every setting the API and worker read at startup.
type Config struct {
	AppEnv          string        `env:"APP_ENV" envDefault:"development"`
	HTTPAddr        string        `env:"HTTP_ADDR" envDefault:":8080"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"15s"`
	LogLevel        string        `env:"LOG_LEVEL" envDefault:"info"`
	CORSOrigins     []string      `env:"CORS_ALLOWED_ORIGINS" envSeparator:","`
	// TrustedProxies are the proxy IPs/CIDRs whose X-Forwarded-For is believed.
	// Empty means the direct peer address is always used as the client IP.
	TrustedProxies []string `env:"TRUSTED_PROXIES" envSeparator:","`

	AppName string `env:"APP_NAME" envDefault:"Saka"`

	// MailDriver picks how emails leave the system: "smtp" or "log" (development only).
	MailDriver   string `env:"MAIL_DRIVER" envDefault:"log"`
	MailFrom     string `env:"MAIL_FROM" envDefault:"Saka <noreply@saka.local>"`
	SMTPHost     string `env:"SMTP_HOST" envDefault:"localhost"`
	SMTPPort     int    `env:"SMTP_PORT" envDefault:"1025"`
	SMTPUsername string `env:"SMTP_USERNAME"`
	SMTPPassword string `env:"SMTP_PASSWORD"`

	// Outbox worker tuning.
	WorkerConcurrency  int           `env:"WORKER_CONCURRENCY" envDefault:"4"`
	WorkerPollInterval time.Duration `env:"WORKER_POLL_INTERVAL" envDefault:"5s"`
	WorkerJobTimeout   time.Duration `env:"WORKER_JOB_TIMEOUT" envDefault:"1m"`
	WorkerLease        time.Duration `env:"WORKER_LEASE" envDefault:"5m"`

	DatabaseURL string `env:"DATABASE_URL"`
	DBMaxConns  int32  `env:"DB_MAX_CONNS" envDefault:"10"`
	DBMinConns  int32  `env:"DB_MIN_CONNS" envDefault:"0"`
}

// Load reads the process environment and validates the result.
func Load() (Config, error) {
	return parse(env.Options{})
}

// LoadFrom reads settings from the given map instead of the process
// environment, which keeps tests independent from the host.
func LoadFrom(environ map[string]string) (Config, error) {
	return parse(env.Options{Environment: environ})
}

func parse(opts env.Options) (Config, error) {
	var cfg Config
	if err := env.ParseWithOptions(&cfg, opts); err != nil {
		return Config{}, fmt.Errorf("parse environment: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// IsProduction reports whether the app runs in the production environment.
func (c Config) IsProduction() bool { return c.AppEnv == EnvProduction }

// Validate checks every setting and reports all problems at once.
func (c Config) Validate() error {
	var errs []error
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	switch c.AppEnv {
	case EnvDevelopment, EnvTest, EnvProduction:
	default:
		add("APP_ENV must be one of development, test, production (got %q)", c.AppEnv)
	}

	if strings.TrimSpace(c.HTTPAddr) == "" {
		add("HTTP_ADDR must not be empty")
	}
	if c.ShutdownTimeout <= 0 {
		add("SHUTDOWN_TIMEOUT must be positive")
	}

	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		add("LOG_LEVEL must be one of debug, info, warn, error (got %q)", c.LogLevel)
	}

	if c.DatabaseURL == "" {
		add("DATABASE_URL is required")
	} else if u, err := url.Parse(c.DatabaseURL); err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		add("DATABASE_URL must be a postgres:// URL")
	}
	if c.DBMaxConns < 1 {
		add("DB_MAX_CONNS must be at least 1")
	}
	if c.DBMinConns < 0 || c.DBMinConns > c.DBMaxConns {
		add("DB_MIN_CONNS must be between 0 and DB_MAX_CONNS")
	}

	switch c.MailDriver {
	case "smtp":
		if strings.TrimSpace(c.SMTPHost) == "" {
			add("SMTP_HOST is required when MAIL_DRIVER=smtp")
		}
		if c.SMTPPort < 1 || c.SMTPPort > 65535 {
			add("SMTP_PORT must be between 1 and 65535")
		}
		if _, err := mail.ParseAddress(c.MailFrom); err != nil {
			add("MAIL_FROM must be a valid address such as \"Name <user@example.com>\"")
		}
	case "log":
		if c.IsProduction() {
			add("MAIL_DRIVER=log is not allowed in production: emails would be dropped")
		}
	default:
		add("MAIL_DRIVER must be one of smtp, log (got %q)", c.MailDriver)
	}

	if c.WorkerConcurrency < 1 {
		add("WORKER_CONCURRENCY must be at least 1")
	}
	if c.WorkerPollInterval <= 0 || c.WorkerJobTimeout <= 0 {
		add("WORKER_POLL_INTERVAL and WORKER_JOB_TIMEOUT must be positive")
	}
	if c.WorkerLease <= c.WorkerJobTimeout {
		add("WORKER_LEASE must be longer than WORKER_JOB_TIMEOUT")
	}

	if c.IsProduction() {
		for _, origin := range c.CORSOrigins {
			if origin == "*" {
				add("CORS_ALLOWED_ORIGINS must not contain * in production")
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	return nil
}
