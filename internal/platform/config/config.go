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
	"net/mail"
	"net/url"
	"os"
	"strings"
	"time"
)

// DefaultPath is the config file used when none is given.
const DefaultPath = "config.json"

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
	Mail     MailConfig     `json:"mail"`
	Worker   WorkerConfig   `json:"worker"`
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
	// TrustedProxies are the proxy IPs/CIDRs whose X-Forwarded-For is believed.
	// Empty means the direct peer address is always used as the client IP.
	TrustedProxies []string `json:"trusted_proxies"`
}

// DatabaseConfig covers the PostgreSQL connection pool.
type DatabaseConfig struct {
	URL      string `json:"url"`
	MaxConns int32  `json:"max_conns"`
	MinConns int32  `json:"min_conns"`
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
		},
		Database: DatabaseConfig{MaxConns: 10, MinConns: 0},
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
	if c.IsProduction() {
		for _, origin := range c.App.CORSAllowedOrigins {
			if origin == "*" {
				add("app.cors_allowed_origins must not contain * in production")
			}
		}
	}

	// The database URL contains credentials, so it is never echoed in errors.
	if c.Database.URL == "" {
		add("database.url is required")
	} else if u, err := url.Parse(c.Database.URL); err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		add("database.url must be a postgres:// URL")
	}
	if c.Database.MaxConns < 1 {
		add("database.max_conns must be at least 1")
	}
	if c.Database.MinConns < 0 || c.Database.MinConns > c.Database.MaxConns {
		add("database.min_conns must be between 0 and database.max_conns")
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

	if len(errs) > 0 {
		return fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	return nil
}
