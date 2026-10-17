// Package config loads and validates application settings from environment variables.
package config

import (
	"errors"
	"fmt"
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
