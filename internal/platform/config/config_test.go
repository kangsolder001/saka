package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/config"
)

const validDBURL = "postgres://saka:saka@localhost:5433/saka?sslmode=disable"

// minimal is the smallest valid file: only the required database url.
const minimal = `{"database": {"url": "` + validDBURL + `"}}`

func TestParse_MinimalFileGetsDefaults(t *testing.T) {
	cfg, err := config.Parse([]byte(minimal))

	require.NoError(t, err)
	assert.Equal(t, "Saka", cfg.App.Name)
	assert.Equal(t, config.EnvDevelopment, cfg.App.Env)
	assert.Equal(t, ":8080", cfg.App.HTTPAddr)
	assert.Equal(t, 15*time.Second, cfg.App.ShutdownTimeout.Std())
	assert.Equal(t, "info", cfg.App.LogLevel)
	assert.Empty(t, cfg.App.CORSAllowedOrigins)
	assert.Equal(t, int32(10), cfg.Database.MaxConns)
	assert.Equal(t, "log", cfg.Mail.Driver)
	assert.Equal(t, "localhost", cfg.Mail.SMTP.Host)
	assert.Equal(t, 1025, cfg.Mail.SMTP.Port)
	assert.Equal(t, 4, cfg.Worker.Concurrency)
	assert.Equal(t, 5*time.Second, cfg.Worker.PollInterval.Std())
	assert.Equal(t, time.Minute, cfg.Worker.JobTimeout.Std())
	assert.Equal(t, 5*time.Minute, cfg.Worker.Lease.Std())
	assert.False(t, cfg.IsProduction())
}

func TestParse_OverridesAndKeepsUntouchedDefaults(t *testing.T) {
	cfg, err := config.Parse([]byte(`{
		"app": {"env": "production", "http_addr": ":9000", "shutdown_timeout": "30s",
		        "cors_allowed_origins": ["https://a.example", "https://b.example"],
		        "trusted_proxies": ["10.0.0.0/8"]},
		"database": {"url": "` + validDBURL + `", "max_conns": 25, "min_conns": 2},
		"mail": {"driver": "smtp", "from": "Saka <noreply@example.com>",
		         "smtp": {"host": "smtp.example.com", "port": 587, "username": "u", "password": "p"}},
		"worker": {"concurrency": 8, "lease": "10m"}
	}`))

	require.NoError(t, err)
	assert.True(t, cfg.IsProduction())
	assert.Equal(t, ":9000", cfg.App.HTTPAddr)
	assert.Equal(t, 30*time.Second, cfg.App.ShutdownTimeout.Std())
	assert.Equal(t, []string{"https://a.example", "https://b.example"}, cfg.App.CORSAllowedOrigins)
	assert.Equal(t, []string{"10.0.0.0/8"}, cfg.App.TrustedProxies)
	assert.Equal(t, int32(25), cfg.Database.MaxConns)
	assert.Equal(t, int32(2), cfg.Database.MinConns)
	assert.Equal(t, "smtp.example.com", cfg.Mail.SMTP.Host)
	assert.Equal(t, 587, cfg.Mail.SMTP.Port)
	assert.Equal(t, 8, cfg.Worker.Concurrency)
	assert.Equal(t, 10*time.Minute, cfg.Worker.Lease.Std())
	assert.Equal(t, "Saka", cfg.App.Name, "keys not in the file keep their default")
	assert.Equal(t, time.Minute, cfg.Worker.JobTimeout.Std(), "partial sections keep sibling defaults")
}

func TestParse_RejectsBadJSON(t *testing.T) {
	tests := map[string]struct {
		input   string
		wantMsg string
	}{
		"empty file":         {``, "parse json"},
		"not json":           {`hello`, "parse json"},
		"unknown top key":    {`{"databse": {"url": "` + validDBURL + `"}}`, "unknown field"},
		"unknown nested key": {`{"database": {"url": "` + validDBURL + `", "pool": 3}}`, "unknown field"},
		"wrong type":         {`{"database": {"url": "` + validDBURL + `", "max_conns": "ten"}}`, "parse json"},
		"duration as number": {`{"database": {"url": "` + validDBURL + `"}, "app": {"shutdown_timeout": 15}}`, "duration must be a string"},
		"bad duration":       {`{"database": {"url": "` + validDBURL + `"}, "app": {"shutdown_timeout": "soon"}}`, "invalid duration"},
		"trailing data":      {minimal + ` {}`, "unexpected data"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := config.Parse([]byte(tt.input))

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestParse_Validation(t *testing.T) {
	with := func(app, db, mail, worker string) string {
		if db == "" {
			db = `{"url": "` + validDBURL + `"}`
		}
		field := func(name, v string) string {
			if v == "" {
				return ""
			}
			return `"` + name + `": ` + v + `,`
		}
		return `{` + field("app", app) + field("mail", mail) + field("worker", worker) + `"database": ` + db + `}`
	}
	tests := []struct {
		name    string
		input   string
		wantMsg string
	}{
		{"missing database url", `{}`, "database.url is required"},
		{"non postgres url", with("", `{"url": "mysql://x"}`, "", ""), "database.url must be a postgres:// URL"},
		{"unknown env", with(`{"env": "staging"}`, "", "", ""), "app.env must be one of"},
		{"blank app name", with(`{"name": " "}`, "", "", ""), "app.name must not be empty"},
		{"unknown log level", with(`{"log_level": "trace"}`, "", "", ""), "app.log_level must be one of"},
		{"zero shutdown timeout", with(`{"shutdown_timeout": "0s"}`, "", "", ""), "app.shutdown_timeout must be positive"},
		{"min conns above max", with("", `{"url": "`+validDBURL+`", "max_conns": 2, "min_conns": 5}`, "", ""), "database.min_conns"},
		{"zero max conns", with("", `{"url": "`+validDBURL+`", "max_conns": 0}`, "", ""), "database.max_conns"},
		{"wildcard cors in production", with(`{"env": "production", "cors_allowed_origins": ["*"]}`, "", `{"driver": "smtp"}`, ""), "must not contain *"},
		{"unknown mail driver", with("", "", `{"driver": "pigeon"}`, ""), "mail.driver must be one of"},
		{"log mailer in production", with(`{"env": "production"}`, "", `{"driver": "log"}`, ""), "not allowed in production"},
		{"smtp without host", with("", "", `{"driver": "smtp", "smtp": {"host": " "}}`, ""), "mail.smtp.host is required"},
		{"smtp bad port", with("", "", `{"driver": "smtp", "smtp": {"port": 70000}}`, ""), "mail.smtp.port"},
		{"smtp bad from", with("", "", `{"driver": "smtp", "from": "nope"}`, ""), "mail.from"},
		{"zero worker concurrency", with("", "", "", `{"concurrency": 0}`), "worker.concurrency"},
		{"lease not above timeout", with("", "", "", `{"lease": "30s", "job_timeout": "1m"}`), "worker.lease must be longer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Parse([]byte(tt.input))

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestParse_ReportsAllProblemsAtOnce(t *testing.T) {
	_, err := config.Parse([]byte(`{"app": {"env": "staging", "log_level": "trace"}}`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "app.env")
	assert.Contains(t, err.Error(), "app.log_level")
	assert.Contains(t, err.Error(), "database.url")
}

func TestParse_NeverEchoesDatabaseCredentials(t *testing.T) {
	_, err := config.Parse([]byte(`{"database": {"url": "mysql://admin:s3cretpass@host/db"}}`))

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cretpass")
}

func TestDefault_IsValidOnceTheDatabaseURLIsSet(t *testing.T) {
	cfg := config.Default()
	assert.Error(t, cfg.Validate(), "database.url has no default")

	cfg.Database.URL = validDBURL
	assert.NoError(t, cfg.Validate())
}

func TestLoad(t *testing.T) {
	t.Run("reads a file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		require.NoError(t, os.WriteFile(path, []byte(minimal), 0o600))

		cfg, err := config.Load(path)

		require.NoError(t, err)
		assert.Equal(t, validDBURL, cfg.Database.URL)
	})

	t.Run("missing file explains how to create it", func(t *testing.T) {
		_, err := config.Load(filepath.Join(t.TempDir(), "nope.json"))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "config.example.json")
	})

	t.Run("errors mention the file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad.json")
		require.NoError(t, os.WriteFile(path, []byte(`{}`), 0o600))

		_, err := config.Load(path)

		require.Error(t, err)
		assert.True(t, strings.Contains(err.Error(), "bad.json"))
	})
}

// The example file is what people copy; it must always be a valid config.
func TestExampleFileIsValid(t *testing.T) {
	cfg, err := config.Load(filepath.Join("..", "..", "..", "config.example.json"))

	require.NoError(t, err)
	assert.Equal(t, "smtp", cfg.Mail.Driver)
}
