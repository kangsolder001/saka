// Package config loads and validates application settings from a JSON file
// (config.json by default). Unset keys keep the defaults from Default, and
// unknown keys are rejected so typos fail at startup instead of being ignored.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// DefaultPath is the config file used when none is given.
const DefaultPath = "config.json"

var rolePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

var keyPrefixPattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

const (
	minJWTSecretLen   = 32
	placeholderMarker = "change-me"
)

const (
	EnvDevelopment = "development"
	EnvTest        = "test"
	EnvProduction  = "production"
)

// Duration is a time.Duration written as a string in JSON, e.g. "15s", "5m".
type Duration time.Duration

// Std returns the value as a time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// UnmarshalJSON parses a Go duration string such as "1m30s".
func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return errors.New(`duration must be a string such as "15s" or "5m"`)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalJSON writes the duration back as a string.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// Config holds every setting the API, worker and migrate commands read.
type Config struct {
	App      AppConfig      `json:"app"`
	Database DatabaseConfig `json:"database"`
	Auth     AuthConfig     `json:"auth"`
	Redis    RedisConfig    `json:"redis"`
	Mail     MailConfig     `json:"mail"`
	Worker   WorkerConfig   `json:"worker"`
	Billing  BillingConfig  `json:"billing"`
}

// AppConfig covers the HTTP server and general behaviour.
type AppConfig struct {
	Name            string   `json:"name"`
	Env             string   `json:"env"`
	HTTPAddr        string   `json:"http_addr"`
	ShutdownTimeout Duration `json:"shutdown_timeout"`
	LogLevel        string   `json:"log_level"`
	// CORSAllowedOrigins lists the browser origins allowed to call the API.
	CORSAllowedOrigins []string `json:"cors_allowed_origins"`
	// FrontendURL is the base of links sent by email, e.g. <url>/verify-email?token=...
	FrontendURL string `json:"frontend_url"`
	// TrustedProxies are the proxy IPs/CIDRs whose X-Forwarded-For is believed.
	// Empty means the direct peer address is always used as the client IP.
	TrustedProxies []string `json:"trusted_proxies"`
}

// DatabaseConfig covers the PostgreSQL connection pool.
type DatabaseConfig struct {
	URL      string `json:"url"`
	MaxConns int32  `json:"max_conns"`
	MinConns int32  `json:"min_conns"`
	// AppRole is the non-owner PostgreSQL role every connection switches to
	// (SET ROLE), so row-level security applies even when the login user owns
	// the tables or is a superuser. The migrations create saka_app. Empty
	// disables the switch (tests of the pool itself); never do that in production.
	AppRole string `json:"app_role"`
}

// RedisConfig covers the shared Redis instance (rate limits, caches). Redis is
// never the source of truth: PostgreSQL is.
type RedisConfig struct {
	Addr     string `json:"addr"`
	Password string `json:"password"`
	DB       int    `json:"db"`
	// KeyPrefix starts every key (<prefix>:<module>:...), so modules, services
	// and environments sharing one Redis never collide.
	KeyPrefix string `json:"key_prefix"`
	// UserCacheTTL bounds how long cached member data may be stale if an
	// invalidation is ever missed.
	UserCacheTTL Duration `json:"user_cache_ttl"`
}

// AuthConfig covers token signing and lifetimes.
type AuthConfig struct {
	// JWTSecret signs access tokens (HS256). Required, at least 32 characters.
	JWTSecret       string   `json:"jwt_secret"`
	AccessTokenTTL  Duration `json:"access_token_ttl"`
	RefreshTokenTTL Duration `json:"refresh_token_ttl"`
}

// MailConfig covers outgoing email.
type MailConfig struct {
	// Driver is "smtp" or "log" (development only: emails are only logged).
	Driver string     `json:"driver"`
	From   string     `json:"from"`
	SMTP   SMTPConfig `json:"smtp"`
}

// SMTPConfig is the SMTP relay used when the mail driver is "smtp".
type SMTPConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// WorkerConfig tunes the outbox worker.
type WorkerConfig struct {
	Concurrency  int      `json:"concurrency"`
	PollInterval Duration `json:"poll_interval"`
	JobTimeout   Duration `json:"job_timeout"`
	Lease        Duration `json:"lease"`
}

// Payment providers a deployment can choose (billing.provider).
const (
	BillingProviderMidtrans = "midtrans"
	// BillingProviderFake is the in-memory gateway for development and tests.
	BillingProviderFake = "fake"
)

// BillingConfig covers invoices, the payment gateway and the billing jobs
// (spec §8).
type BillingConfig struct {
	// Provider is the gateway new invoices are created with: "midtrans", or
	// "fake" (development and tests only, refused in production). Webhooks of
	// every configured provider are accepted.
	Provider string `json:"provider"`
	// InvoiceDueDays is how long a checkout invoice stays payable.
	InvoiceDueDays int `json:"invoice_due_days"`
	// RenewalLeadDays is how long before a period ends its renewal invoice is issued.
	RenewalLeadDays int `json:"renewal_lead_days"`
	// GraceDays is how long a subscription keeps its access after the period
	// ended without payment (past_due), before it expires.
	GraceDays int `json:"grace_days"`
	// HourlyJobsInterval is how often the worker runs billing.renewals,
	// billing.expire and billing.apply_scheduled (spec §8.6).
	HourlyJobsInterval Duration `json:"hourly_jobs_interval"`
	// DailyJobsInterval is how often it runs billing.reminders and billing.reconcile.
	DailyJobsInterval Duration `json:"daily_jobs_interval"`
	// ReconcileMinAge is how old an open invoice must be before billing.reconcile
	// asks the gateway about it (a fresh invoice is still being paid).
	ReconcileMinAge Duration       `json:"reconcile_min_age"`
	Midtrans        MidtransConfig `json:"midtrans"`
}

// MidtransConfig is the Midtrans account. The server key is a secret: it lives
// in config.json only and is never logged.
type MidtransConfig struct {
	ServerKey string `json:"server_key"`
	// SnapBaseURL and APIBaseURL default to the sandbox; production uses
	// https://app.midtrans.com and https://api.midtrans.com.
	SnapBaseURL    string   `json:"snap_base_url"`
	APIBaseURL     string   `json:"api_base_url"`
	RequestTimeout Duration `json:"request_timeout"`
}

// Default returns the built-in defaults. It is not valid on its own because
// database.url has no default; tests set it and call Validate when needed.
func Default() Config {
	return Config{
		App: AppConfig{
			Name:            "Saka",
			Env:             EnvDevelopment,
			HTTPAddr:        ":8080",
			ShutdownTimeout: Duration(15 * time.Second),
			LogLevel:        "info",
			FrontendURL:     "http://localhost:3000",
		},
		Auth: AuthConfig{
			AccessTokenTTL:  Duration(15 * time.Minute),
			RefreshTokenTTL: Duration(30 * 24 * time.Hour),
		},
		Database: DatabaseConfig{MaxConns: 10, MinConns: 0, AppRole: "saka_app"},
		Redis: RedisConfig{
			Addr:         "localhost:6379",
			KeyPrefix:    "saka",
			UserCacheTTL: Duration(5 * time.Minute),
		},
		Mail: MailConfig{
			Driver: "log",
			From:   "Saka <noreply@saka.local>",
			SMTP:   SMTPConfig{Host: "localhost", Port: 1025},
		},
		Worker: WorkerConfig{
			Concurrency:  4,
			PollInterval: Duration(5 * time.Second),
			JobTimeout:   Duration(time.Minute),
			Lease:        Duration(5 * time.Minute),
		},
		Billing: BillingConfig{
			Provider:           BillingProviderFake,
			InvoiceDueDays:     7,
			RenewalLeadDays:    3,
			GraceDays:          3,
			HourlyJobsInterval: Duration(time.Hour),
			DailyJobsInterval:  Duration(24 * time.Hour),
			ReconcileMinAge:    Duration(time.Hour),
			Midtrans: MidtransConfig{
				SnapBaseURL:    "https://app.sandbox.midtrans.com",
				APIBaseURL:     "https://api.sandbox.midtrans.com",
				RequestTimeout: Duration(15 * time.Second),
			},
		},
	}
}

// Load reads and validates the config file at path.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the path is an operator-supplied flag
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("config file %q not found: copy config.example.json to %s and edit it", path, DefaultPath)
		}
		return Config{}, fmt.Errorf("read config file: %w", err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return Config{}, fmt.Errorf("config file %q: %w", path, err)
	}
	return cfg, nil
}

// Parse decodes JSON on top of the defaults and validates the result.
func Parse(data []byte) (Config, error) {
	cfg := Default()

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse json: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("parse json: unexpected data after the config object")
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// IsProduction reports whether the app runs in the production environment.
func (c Config) IsProduction() bool { return c.App.Env == EnvProduction }

// Validate checks every setting and reports all problems at once, naming
// each by its JSON path.
func (c Config) Validate() error {
	var errs []error
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	switch c.App.Env {
	case EnvDevelopment, EnvTest, EnvProduction:
	default:
		add("app.env must be one of development, test, production (got %q)", c.App.Env)
	}
	if strings.TrimSpace(c.App.Name) == "" {
		add("app.name must not be empty")
	}
	if strings.TrimSpace(c.App.HTTPAddr) == "" {
		add("app.http_addr must not be empty")
	}
	if c.App.ShutdownTimeout <= 0 {
		add("app.shutdown_timeout must be positive")
	}
	switch strings.ToLower(c.App.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		add("app.log_level must be one of debug, info, warn, error (got %q)", c.App.LogLevel)
	}
	// The API answers CORS requests with credentials (the refresh cookie), and
	// browsers refuse a wildcard origin together with credentials, so every
	// origin must be listed explicitly in every environment.
	for _, origin := range c.App.CORSAllowedOrigins {
		if origin == "*" {
			add("app.cors_allowed_origins must not contain *: list each origin explicitly")
		}
	}

	if u, err := url.Parse(c.App.FrontendURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		add("app.frontend_url must be an http(s) URL such as https://app.example.com")
	}

	// The JWT secret is never echoed in errors.
	switch {
	case len(c.Auth.JWTSecret) < minJWTSecretLen:
		add("auth.jwt_secret is required and must be at least %d characters", minJWTSecretLen)
	case c.IsProduction() && strings.Contains(strings.ToLower(c.Auth.JWTSecret), placeholderMarker):
		add("auth.jwt_secret still contains the example placeholder; generate a random secret")
	}
	if c.Auth.AccessTokenTTL <= 0 || c.Auth.RefreshTokenTTL <= 0 {
		add("auth.access_token_ttl and auth.refresh_token_ttl must be positive")
	} else if c.Auth.AccessTokenTTL >= c.Auth.RefreshTokenTTL {
		add("auth.access_token_ttl must be shorter than auth.refresh_token_ttl")
	}

	// The database URL contains credentials, so it is never echoed in errors.
	if c.Database.URL == "" {
		add("database.url is required")
	} else if u, err := url.Parse(c.Database.URL); err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		add("database.url must be a postgres:// URL")
	}
	if c.Database.AppRole != "" && !rolePattern.MatchString(c.Database.AppRole) {
		add("database.app_role must be a lowercase PostgreSQL role name such as saka_app")
	}
	if c.IsProduction() && c.Database.AppRole == "" {
		add("database.app_role must be set in production: without it row-level security does not protect tenants")
	}
	if c.Database.MaxConns < 1 {
		add("database.max_conns must be at least 1")
	}
	if c.Database.MinConns < 0 || c.Database.MinConns > c.Database.MaxConns {
		add("database.min_conns must be between 0 and database.max_conns")
	}

	if host, port, err := net.SplitHostPort(c.Redis.Addr); err != nil || host == "" || port == "" {
		add("redis.addr must be host:port such as localhost:6379")
	}
	if c.Redis.DB < 0 || c.Redis.DB > 15 {
		add("redis.db must be between 0 and 15")
	}
	if !keyPrefixPattern.MatchString(c.Redis.KeyPrefix) {
		add("redis.key_prefix must be 1-32 characters of a-z, 0-9, _ or -")
	}
	if c.Redis.UserCacheTTL <= 0 || c.Redis.UserCacheTTL > Duration(24*time.Hour) {
		add("redis.user_cache_ttl must be between 1s and 24h")
	}

	switch c.Mail.Driver {
	case "smtp":
		if strings.TrimSpace(c.Mail.SMTP.Host) == "" {
			add("mail.smtp.host is required when mail.driver is smtp")
		}
		if c.Mail.SMTP.Port < 1 || c.Mail.SMTP.Port > 65535 {
			add("mail.smtp.port must be between 1 and 65535")
		}
		if _, err := mail.ParseAddress(c.Mail.From); err != nil {
			add(`mail.from must be a valid address such as "Name <user@example.com>"`)
		}
	case "log":
		if c.IsProduction() {
			add("mail.driver \"log\" is not allowed in production: emails would be dropped")
		}
	default:
		add("mail.driver must be one of smtp, log (got %q)", c.Mail.Driver)
	}

	if c.Worker.Concurrency < 1 {
		add("worker.concurrency must be at least 1")
	}
	if c.Worker.PollInterval <= 0 || c.Worker.JobTimeout <= 0 {
		add("worker.poll_interval and worker.job_timeout must be positive")
	}
	if c.Worker.Lease <= c.Worker.JobTimeout {
		add("worker.lease must be longer than worker.job_timeout")
	}

	c.validateBilling(add)

	if len(errs) > 0 {
		return fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	return nil
}

// validateBilling checks the billing section. Secrets are never echoed.
func (c Config) validateBilling(add func(format string, args ...any)) {
	b := c.Billing
	switch b.Provider {
	case BillingProviderMidtrans:
		if strings.TrimSpace(b.Midtrans.ServerKey) == "" {
			add("billing.midtrans.server_key is required when billing.provider is midtrans")
		}
	case BillingProviderFake:
		if c.IsProduction() {
			add("billing.provider \"fake\" is not allowed in production: payments would never reach a real gateway")
		}
	default:
		add("billing.provider must be one of midtrans, fake (got %q)", b.Provider)
	}
	if b.InvoiceDueDays < 1 || b.InvoiceDueDays > 60 {
		add("billing.invoice_due_days must be between 1 and 60")
	}
	if b.RenewalLeadDays < 1 || b.RenewalLeadDays > 30 {
		add("billing.renewal_lead_days must be between 1 and 30")
	}
	if b.GraceDays < 0 || b.GraceDays > 30 {
		add("billing.grace_days must be between 0 and 30")
	}
	if b.HourlyJobsInterval <= 0 || b.DailyJobsInterval <= 0 {
		add("billing.hourly_jobs_interval and billing.daily_jobs_interval must be positive")
	}
	if b.ReconcileMinAge < 0 {
		add("billing.reconcile_min_age must not be negative")
	}

	m := b.Midtrans
	for name, raw := range map[string]string{"billing.midtrans.snap_base_url": m.SnapBaseURL, "billing.midtrans.api_base_url": m.APIBaseURL} {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			add("%s must be an http(s) URL", name)
			continue
		}
		if c.IsProduction() && b.Provider == BillingProviderMidtrans && strings.Contains(u.Host, "sandbox") {
			add("%s points at the Midtrans sandbox, which is not allowed in production", name)
		}
	}
	if m.RequestTimeout <= 0 {
		add("billing.midtrans.request_timeout must be positive")
	}
	if c.IsProduction() && b.Provider == BillingProviderMidtrans {
		if strings.HasPrefix(m.ServerKey, "SB-") {
			add("billing.midtrans.server_key is a sandbox key, which is not allowed in production")
		}
		if strings.Contains(strings.ToLower(m.ServerKey), placeholderMarker) {
			add("billing.midtrans.server_key still contains the example placeholder")
		}
	}
}
